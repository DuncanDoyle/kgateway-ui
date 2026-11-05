package api

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/DuncanDoyle/kgateway-ui/pkg/diagnose"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Findings list parameters (design §13.1); findingsSingle may appear once. The
// grouped view (view=groups, design §17.1) takes only groupsParams.
var (
	findingsFilters = []string{"severity", "code", "kind", "namespace", "group", "q"}
	findingsParams  = append(slices.Clone(findingsFilters), "sort", "limit", "cursor", "view")
	findingsSingle  = []string{"group", "q", "sort", "limit", "cursor", "view"}
	groupsParams    = []string{"severity", "code", "q", "limit", "cursor", "view"}
)

// viewGroups is the view parameter of the grouped findings view.
const viewGroups = "groups"

// affectedLimit is the finding detail's affected page size (design §13.1).
const affectedLimit = 50

var (
	codeFormat    = regexp.MustCompile(`^[A-Z][A-Z0-9]*-[0-9]{3}$`)
	groupIDFormat = regexp.MustCompile(`^f1-[a-z2-7]{26}$`)
)

// findingsQuery is a parsed GET …/findings query.
type findingsQuery struct {
	severities []health.Severity
	codes      []string
	kinds      []objects.GroupKind
	namespaces []string
	group, q   string // q is lower-cased
	groups     bool   // view=groups
	sort       diagnose.Sort
	limit      int
	cursor     string
}

// parseFindingsQuery parses r's query; any bad parameter or value is
// explore.ErrBadQuery. The index checks cursors.
func parseFindingsQuery(r *http.Request) (findingsQuery, error) {
	q := findingsQuery{sort: diagnose.Sort{Key: diagnose.SortRank}, limit: explore.DefaultLimit}
	vals, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, badParam("malformed query: %v", err)
	}
	for _, name := range slices.Sorted(maps.Keys(vals)) {
		if !slices.Contains(findingsParams, name) {
			return q, badParam("unknown parameter %q", name)
		}
		if len(vals[name]) > 1 && slices.Contains(findingsSingle, name) {
			return q, badParam("parameter %q given more than once", name)
		}
	}
	if vals.Has("view") {
		if v := vals.Get("view"); v != viewGroups {
			return q, badParam("view %q: want groups", v)
		}
		q.groups = true
		for _, name := range slices.Sorted(maps.Keys(vals)) {
			if !slices.Contains(groupsParams, name) {
				return q, badParam("parameter %q: the groups view takes severity, code, q, limit and cursor", name)
			}
		}
	}
	for _, v := range vals["severity"] {
		s := health.Severity(v)
		if s != health.SeverityError && s != health.SeverityWarning && s != health.SeverityInfo {
			return q, badParam("severity %q: want error, warning or info", v)
		}
		q.severities = append(q.severities, s)
	}
	for _, v := range vals["code"] {
		if !codeFormat.MatchString(v) {
			return q, badParam("code %q: want a finding code such as KGWUI-002", v)
		}
		q.codes = append(q.codes, v)
	}
	for _, v := range vals["kind"] {
		s, err := segments(v, 2, "kind")
		if err != nil {
			return q, err
		}
		q.kinds = append(q.kinds, objects.GroupKind{Group: s[0], Kind: s[1]})
	}
	for _, v := range vals["namespace"] {
		if v == "" {
			return q, badParam("namespace: want a namespace name")
		}
		q.namespaces = append(q.namespaces, v)
	}
	if vals.Has("group") {
		if q.group = vals.Get("group"); !groupIDFormat.MatchString(q.group) {
			return q, badParam("group %q: want a group id", q.group)
		}
	}
	q.q, q.cursor = strings.ToLower(vals.Get("q")), vals.Get("cursor")
	if v := vals.Get("limit"); vals.Has("limit") {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > explore.MaxLimit {
			return q, badParam("limit %q: want 1–%d", v, explore.MaxLimit)
		}
		q.limit = n
	}
	if vals.Has("sort") {
		v := vals.Get("sort")
		// Phase 2 syntax: a key with an optional .asc or .desc; rank has one direction.
		key, dir, _ := strings.Cut(v, ".")
		desc := dir == "desc"
		if key != diagnose.SortRank && key != diagnose.SortSince && key != diagnose.SortCode || dir != "" && dir != "asc" && !desc || desc && key == diagnose.SortRank {
			return q, badParam("sort %q: want rank, since or code, with .asc or .desc (rank: .asc only)", v)
		}
		q.sort = diagnose.Sort{Key: key, Desc: desc}
	}
	return q, nil
}

// match reports whether f passes q's filters for the caller of v; q sees only
// what f's row shows (design §14 rule 17). t is f's lowered text, nil without q.
func (q *findingsQuery) match(v *view, f *health.Finding, t *diagnose.Text) bool {
	if len(q.severities) > 0 && !slices.Contains(q.severities, f.Severity) ||
		len(q.codes) > 0 && !slices.Contains(q.codes, f.Code) ||
		q.group != "" && f.Group != q.group {
		return false
	}
	if len(q.kinds) > 0 && (f.Resource == nil || !slices.Contains(q.kinds, objects.GroupKind{Group: f.Resource.Group, Kind: f.Resource.Kind})) ||
		len(q.namespaces) > 0 && (f.Resource == nil || !slices.Contains(q.namespaces, f.Resource.Namespace)) {
		return false
	}
	if q.q == "" {
		return true
	}
	if f.Resource != nil && strings.Contains(t.Identity, q.q) {
		return true
	}
	if f.Resource != nil && !v.can(*f.Resource) {
		return false
	}
	return strings.Contains(t.Message, q.q) || v.allOf(f.Evidence) && strings.Contains(t.Detail, q.q)
}

// restrictedDetail is f's detail for a caller who may not view all of its
// evidence: restrictedEvidence, plus a 015's restart hint, which names no object (R108).
func restrictedDetail(f *health.Finding) string {
	if f.Code == health.CodeRuleReplaced && strings.HasSuffix(f.Detail, health.RuleReplacedHint) {
		return restrictedEvidence + ". " + health.RuleReplacedHint
	}
	return restrictedEvidence
}

// findingRow is f on the wire for the caller of v, restricted per D-013 and
// design §14 rules 11 and 12. Times are UTC.
func findingRow(v *view, l linker, f *health.Finding) FindingRow {
	row := FindingRow{ID: f.ID, Code: f.Code, Slug: f.Slug, Severity: f.Severity, Group: f.Group}
	if f.Resource != nil {
		lr := l.linked(*f.Resource)
		row.Resource = &lr
		if !v.can(*f.Resource) {
			row.Restricted = true
			return row
		}
	}
	row.Message, row.Detail, row.Since = f.Message, f.Detail, f.Since
	if !v.allOf(f.Evidence) {
		row.Detail, row.EvidenceRestricted, row.Since = restrictedDetail(f), true, f.ResourceSince
	}
	if row.Since != nil {
		at := wireTime(*row.Since)
		row.Since = &at
	}
	return row
}

// diagnosis returns i's diagnosis, or an empty one.
func diagnosis(i *engine.InstallModel) *diagnose.Diagnosis {
	if i.Diagnosis == nil || i.Diagnosis.Index == nil {
		return &diagnose.Diagnosis{Index: diagnose.NewIndex(nil, nil, nil)}
	}
	return i.Diagnosis
}

func (h *handler) findings(w http.ResponseWriter, r *http.Request) {
	p, i := h.find(w, r)
	if i == nil {
		return
	}
	q, err := parseFindingsQuery(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	v, fs, ix := h.view(r), i.Health.Findings, diagnosis(i).Index
	match := func(k int) bool { return q.match(v, &fs[k], nil) }
	if q.q != "" {
		// q asks about every hidden-identity row, like a route list.
		v.borrow()
		defer v.release()
		ts := ix.Text(fs)
		match = func(k int) bool { return q.match(v, &fs[k], &ts[k]) }
	}
	if q.groups {
		h.findingGroups(w, p, i, v, &q, match)
		return
	}
	pg, err := ix.Page(fs, i.Resolved.ID(), q.sort, q.limit, q.cursor, match)
	if err != nil {
		fail(w, err)
		return
	}
	l := linkerFor(v, p.Model, i)
	items := make([]FindingRow, 0, len(pg.Items))
	for _, k := range pg.Items {
		items = append(items, findingRow(v, l, &fs[k]))
	}
	writeJSON(w, Page[FindingRow]{Install: i.Resolved.ID(), Items: items, Total: pg.Total, Next: pg.Next, Prev: pg.Prev,
		CollectedAt: wireTime(p.ConfirmedAt)})
}

// findingGroups writes the page of i's groups for q (design §5.2, §17.1): a group matches
// when one member passes match (R98). A group whose cause the caller cannot view matches q
// by its members' identity only (§14 rule 17).
func (h *handler) findingGroups(w http.ResponseWriter, p *engine.Published, i *engine.InstallModel, v *view, q *findingsQuery, match func(k int) bool) {
	d, fs := diagnosis(i), i.Health.Findings
	// d.Rank lists each group's members in group order, so a group's first is its top member.
	tops := make([]int, len(d.Groups))
	for k, off := 0, 0; k < len(d.Groups); k++ {
		tops[k], off = off, off+len(d.Groups[k].Members)
	}
	noQ := *q
	noQ.q = ""
	pass := func(m int) bool { return noQ.match(v, &fs[m], nil) }
	matchGroup := func(k int) bool {
		g := &d.Groups[k]
		if g.Cause != nil && !v.can(*g.Cause) && q.q != "" {
			ts := d.Index.Text(fs)
			return slices.ContainsFunc(g.Members, func(m int) bool { return pass(m) && strings.Contains(ts[m].Identity, q.q) })
		}
		return slices.ContainsFunc(g.Members, match)
	}
	pg, err := d.Index.PageGroups(i.Resolved.ID(), q.limit, q.cursor, matchGroup)
	if err != nil {
		fail(w, err)
		return
	}
	l, in := linkerFor(v, p.Model, i), stepsInput(p.Model, i)
	items := make([]GroupRow, 0, len(pg.Items))
	for _, k := range pg.Items {
		items = append(items, h.groupRow(v, l, &d.Groups[k], fs, d.Rank[tops[k]], in))
	}
	writeJSON(w, Page[GroupRow]{Install: i.Resolved.ID(), Items: items, Total: pg.Total, Next: pg.Next, Prev: pg.Prev,
		CollectedAt: wireTime(p.ConfirmedAt)})
}

func (h *handler) findingDetail(w http.ResponseWriter, r *http.Request) {
	p, i := h.find(w, r)
	if i == nil {
		return
	}
	vals, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeErr(w, http.StatusBadRequest, badParam("malformed query: %v", err).Error())
		return
	}
	for name, vs := range vals {
		if name != "affectedCursor" || len(vs) > 1 {
			writeErr(w, http.StatusBadRequest, badParam("parameter %q unknown or given more than once", name).Error())
			return
		}
	}
	d, fs := diagnosis(i), i.Health.Findings
	k, ok := d.Index.Find(fs, r.PathValue("findingId"))
	if !ok {
		writeErr(w, http.StatusNotFound, "finding "+r.PathValue("findingId")+" not found")
		return
	}
	v := h.view(r)
	l := linkerFor(v, p.Model, i)
	f := &fs[k]
	row := findingRow(v, l, f)
	var refs []objects.Ref
	if !row.Restricted {
		refs = affectedOf(v, d, f.Group)
	}
	affected, err := pageAffected(refs, vals.Get("affectedCursor"))
	if err != nil {
		fail(w, err)
		return
	}
	items := make([]LinkedRef, 0, len(affected.refs))
	for _, ref := range affected.refs {
		items = append(items, l.linked(ref))
	}
	at := wireTime(p.ConfirmedAt)
	out := FindingDetail{Install: i.Resolved.ID(), Finding: row, CollectedAt: at}
	if !row.Restricted {
		if !row.EvidenceRestricted && f.Facts != nil {
			out.Evidence = wireEvidence(v, l, f.Facts)
			out.SuggestedFix = wireFix(v, l, diagnose.Fix(f))
		}
		out.NextSteps = nextSteps(diagnose.Steps(diagnose.Group{Members: []int{k}}, fs, stepsInput(p.Model, i), h.commands, v.visible()))
	}
	out.Affected = Page[LinkedRef]{Install: i.Resolved.ID(), Items: items, Total: len(refs), Next: affected.next, Prev: affected.prev, CollectedAt: at}
	writeJSON(w, out)
}

// wireFix is fx on the wire; nil when fx is nil or names an object the caller
// of v may not view (design §14 rule 15).
func wireFix(v *view, l linker, fx *diagnose.SuggestedFix) *SuggestedFix {
	if fx == nil || !v.allOf(fx.Names) {
		return nil
	}
	return &SuggestedFix{Title: fx.Title, Target: l.linked(fx.Target), Create: fx.Create, Path: fx.Path, YAML: fx.YAML, Basis: fx.Basis}
}

// affectedOf returns the visible affected objects of group, sorted; none
// when the group's cause is hidden (design §14 rule 13).
func affectedOf(v *view, d *diagnose.Diagnosis, group string) []objects.Ref {
	j := slices.IndexFunc(d.Groups, func(g diagnose.Group) bool { return g.ID == group })
	if j < 0 {
		return nil
	}
	return visibleAffected(v, &d.Groups[j])
}

// visibleAffected returns g's affected objects the caller of v may view,
// sorted and deduplicated; none when g's cause is hidden.
func visibleAffected(v *view, g *diagnose.Group) []objects.Ref {
	if len(g.Affected) == 0 || g.Cause != nil && !v.can(*g.Cause) {
		return nil
	}
	out := make([]objects.Ref, 0, len(g.Affected))
	for _, ref := range g.Affected {
		if v.can(ref) {
			out = append(out, ref)
		}
	}
	slices.SortFunc(out, compareAffected)
	return slices.CompactFunc(out, func(a, b objects.Ref) bool { return a == b })
}

// compareAffected orders affected objects by namespace, name, group and kind.
func compareAffected(a, b objects.Ref) int {
	return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Group, b.Group), cmp.Compare(a.Kind, b.Kind))
}

// affectedCursor is the JSON inside an affected cursor, in the explorer's
// shape: T is the anchor's (namespace, name, group, kind).
type affectedCursor struct {
	S string   `json:"s"`
	D string   `json:"d"`
	B bool     `json:"b"`
	T []string `json:"t"`
}

// sortAffected keeps affected cursors apart from every list's.
const sortAffected = "affected"

// encodeRefCursor is the cursor of list tag before or after r.
func encodeRefCursor(tag string, before bool, r objects.Ref) string {
	b, _ := json.Marshal(affectedCursor{S: tag, D: "asc", B: before, T: []string{r.Namespace, r.Name, r.Group, r.Kind}}) // strings cannot fail
	return base64.RawURLEncoding.EncodeToString(b)
}

// affectedPage is one page of affected objects with its cursors.
type affectedPage struct {
	refs       []objects.Ref
	next, prev string
}

// pageAffected returns the page of sorted affected refs after or before c.
func pageAffected(refs []objects.Ref, c string) (affectedPage, error) {
	return pageRefs(refs, c, affectedLimit, sortAffected)
}

// pageRefs returns the limit refs of sorted refs after or before c, a cursor
// of list tag. A cursor that does not decode is explore.ErrBadCursor.
func pageRefs(refs []objects.Ref, c string, limit int, tag string) (affectedPage, error) {
	start, end := 0, min(len(refs), limit)
	var anchor *affectedCursor
	if c != "" {
		var w affectedCursor
		raw, err := base64.RawURLEncoding.DecodeString(c)
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if len(c) > 1024 || err != nil || dec.Decode(&w) != nil || dec.More() || w.S != tag || w.D != "asc" || len(w.T) != 4 || w.T[1] == "" || w.T[3] == "" {
			return affectedPage{}, badCursorErr("not a cursor of the " + tag + " list")
		}
		anchor = &w
		a := objects.Ref{Namespace: w.T[0], Name: w.T[1], Group: w.T[2], Kind: w.T[3]}
		if w.B {
			end, _ = slices.BinarySearchFunc(refs, a, compareAffected)
			start = max(0, end-limit)
		} else {
			k, found := slices.BinarySearchFunc(refs, a, compareAffected)
			if found {
				k++
			}
			start = k
			end = min(len(refs), start+limit)
		}
	}
	p := affectedPage{refs: refs[start:end]}
	switch {
	case end > start:
		if end < len(refs) {
			p.next = encodeRefCursor(tag, false, refs[end-1])
		}
		if start > 0 {
			p.prev = encodeRefCursor(tag, true, refs[start])
		}
	case anchor != nil && !anchor.B && start > 0:
		p.prev = encodeRefCursor(tag, true, objects.Ref{Namespace: anchor.T[0], Name: anchor.T[1], Group: anchor.T[2], Kind: anchor.T[3]})
	case anchor != nil && anchor.B && end < len(refs):
		p.next = encodeRefCursor(tag, false, objects.Ref{Namespace: anchor.T[0], Name: anchor.T[1], Group: anchor.T[2], Kind: anchor.T[3]})
	}
	return p, nil
}

// badCursorErr is explore.ErrBadCursor with why.
func badCursorErr(why string) error { return fmt.Errorf("%w: %s", explore.ErrBadCursor, why) }
