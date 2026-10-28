package health

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// computedCheck raises KGWUI-012 for each cross-check the engine matured into
// state disagrees (computed-rules §9.3). It is optional and always OK.
type computedCheck struct{}

// Name implements Check.
func (computedCheck) Name() string { return "computed views" }

// Run implements Check.
func (c computedCheck) Run(in Input) ([]Finding, CheckResult) {
	var fs []Finding
	if v := in.Computed; v != nil {
		n := 0
		for i := range v.Checks {
			if v.Checks[i].State == computed.StateDisagrees {
				n++
			}
		}
		fs = make([]Finding, 0, n)
		var owned map[objects.Ref]*unstructured.Unstructured
		if n > 0 {
			owned = ownedResources(in.Owned)
		}
		for i := range v.Checks {
			ch := &v.Checks[i]
			if ch.State != computed.StateDisagrees {
				continue
			}
			on, since := ch.On, ch.Since
			f := Finding{Code: CodeComputedDisagrees, Slug: "computed-view-disagrees", Severity: SeverityWarning,
				Install: in.Install.ID(), Resource: &on, Message: ComputedDisagreesMessage, Detail: disagreeDetail(*ch, v.RuleSet, v.Delegation.Rows),
				Since: &since, Evidence: evidence(ch, v.Delegation.Rows)}
			// A caller who cannot view every dep gets the resource's own first-seen (design §7.4 rule 7).
			if u := ownedObject(in.Owned, owned, on); u != nil {
				own := in.firstSeen(u)
				f.ResourceSince = &own
			}
			fs = append(fs, f)
		}
	}
	return fs, ok(c.Name(), false, in.Now)
}

// evidence is what a KGWUI-012 detail depends on. A views B and C check
// leaves out the routes of its deps: they are the subject and the ancestors
// on its paths, whose names the detail may quote (design §7.4 rule 7,
// D-024). Descendants still count: a parent-resolvedrefs check is decided by
// the children its rows under the key select, so it adds them; rows are in
// RowKey order (fix round 3).
func evidence(c *computed.CheckResult, rows []computed.Row) []objects.Ref {
	switch c.ID.Kind {
	case computed.KindChildEntry:
		return withoutRoutes(c.Deps, c.Contributors)
	case computed.KindParentResolvedRefs:
	default:
		return c.Deps
	}
	ev := slices.Clone(withoutRoutes(c.Deps, c.Contributors))
	i, _ := slices.BinarySearchFunc(rows, c.On, func(r computed.Row, p objects.Ref) int { return r.Key.Parent.Compare(p) })
	for ; i < len(rows) && rows[i].Key.Parent == c.On; i++ {
		r := &rows[i]
		if ch := r.Key.Child; ch != (objects.Ref{}) && slices.Contains(r.Reporters, c.Key) && !slices.Contains(ev, ch) {
			ev = append(ev, ch)
		}
	}
	return ev
}

// ownedResources maps the owned Gateways, ListenerSets and routes, and the
// declared and no-parent candidates, by ref; the first of duplicates wins.
// A computed-only child is a candidate, so its KGWUI-012 keeps its first-seen
// (design §7.4 rule 9).
func ownedResources(o *install.Owned) map[objects.Ref]*unstructured.Unstructured {
	if o == nil {
		return nil
	}
	m := make(map[objects.Ref]*unstructured.Unstructured, len(o.Gateways)+len(o.ListenerSets)+len(o.Routes)+len(o.Declared)+len(o.NoParent))
	add := func(u *unstructured.Unstructured) {
		if r := objects.RefOf(u); m[r] == nil {
			m[r] = u
		}
	}
	for _, us := range [][]*unstructured.Unstructured{o.Gateways, o.ListenerSets} {
		for _, u := range us {
			add(u)
		}
	}
	for _, r := range o.Routes {
		add(r.Obj)
	}
	for _, d := range o.Declared {
		add(d.Route.Obj)
	}
	for _, r := range o.NoParent {
		add(r.Obj)
	}
	return m
}

// ownedObject returns the owned Gateway, ListenerSet, route or candidate ref from resources, or nil.
func ownedObject(o *install.Owned, resources map[objects.Ref]*unstructured.Unstructured, ref objects.Ref) *unstructured.Unstructured {
	if o == nil {
		return nil
	}
	return resources[ref]
}

// disagreeDetail says what was computed and what the controller reports, and
// under which rule set (wording passes, Tasks 48 and 67). A views B check
// quotes the match its ancestor delegated under, from rows (design §7.4 rule 7).
func disagreeDetail(c computed.CheckResult, ruleSet string, rows []computed.Row) string {
	var d string
	key := "`" + c.Key.Name + "`"
	if c.Key.SectionName != "" {
		key += " (section `" + c.Key.SectionName + "`)"
	}
	switch c.ID.Kind {
	case computed.KindListenerCount:
		d = fmt.Sprintf(countDisagrees, c.Listener, routes(c.Expected), c.Observed)
	case computed.KindChildEntry:
		parent, _ := c.Parent()
		d = fmt.Sprintf(childDisagrees, parent.String(), delegatedAt(c, parent, rows), entryText(c.Expected), entryText(c.Observed))
	case computed.KindParentResolvedRefs:
		d = fmt.Sprintf(resolvedDisagrees, key, delegatedAt(c, c.On, rows), c.Expected, c.Observed)
	default:
		d = fmt.Sprintf(parentDisagrees, key, c.Expected, c.Observed)
	}
	d += fmt.Sprintf(ruleSetDetail, ruleSet)
	if c.Note != "" {
		d += " " + c.Note + "."
	}
	return d
}

// entryText words a child-entry expectation or observation: an entry, no
// entry, or its Accepted condition.
func entryText(s string) string {
	switch s {
	case "entry":
		return "an entry"
	case "none":
		return "no entry"
	}
	return "Accepted=" + s
}

// maxMatches is how many distinct delegated matches a detail quotes.
const maxMatches = 3

// delegatedAt is atMatch with the distinct matches parent's ancestor
// delegated under, from the retained contexts of parent's rows; "" when none
// is known. For child-entry the ancestor is parent itself, selecting c.On;
// for parent-resolvedrefs it is the HTTPRoute that reports under c.Key, the
// step before parent on each path; under a Gateway or ListenerSet key parent
// is a root, its paths have one step and there is none.
func delegatedAt(c computed.CheckResult, parent objects.Ref, rows []computed.Row) string {
	resolved := c.ID.Kind == computed.KindParentResolvedRefs
	var seen []string
	more := false
	i, _ := slices.BinarySearchFunc(rows, parent, func(r computed.Row, p objects.Ref) int { return r.Key.Parent.Compare(p) })
	for ; i < len(rows) && rows[i].Key.Parent == parent; i++ {
		r := &rows[i]
		if !resolved && r.Key.Child != c.On {
			continue
		}
		for j := range r.Contexts {
			x := &r.Contexts[j]
			m, ok := x.ParentMatch, true
			if resolved {
				// The step before parent is the ancestor writing under c.Key.
				ok = x.Reporter == c.Key && x.Path.Len() >= 2
				if ok {
					m = x.Path.Parent().Last().Match
				}
			}
			if !ok {
				continue
			}
			t := "`" + matchText(m) + "`"
			switch {
			case slices.Contains(seen, t):
			case len(seen) == maxMatches:
				more = true
			default:
				seen = append(seen, t)
			}
		}
	}
	if len(seen) == 0 {
		return ""
	}
	out := strings.Join(seen, matchOr)
	if more {
		out += matchMore
	}
	return fmt.Sprintf(atMatch, out)
}

// matchText words a match as the SPA's MatchLine does, e.g. "PathPrefix /api
// Header x-env Exact prod", cut to maxMatchText bytes and "…" when longer.
func matchText(m objects.Match) string {
	var b strings.Builder
	add := func(s ...string) bool {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		for _, x := range s {
			b.WriteString(x)
		}
		return b.Len() <= maxMatchText
	}
	done := m.Path != nil && !add(m.Path.Type, " ", m.Path.Value)
	for _, ks := range [...]struct {
		label string
		ks    []objects.KeyMatch
	}{{"Header ", m.Headers}, {"Query ", m.QueryParams}} {
		for _, k := range ks.ks {
			if !done {
				done = !add(ks.label, k.Name, " ", k.Type, " ", k.Value)
			}
		}
	}
	if !done && m.Method != "" {
		add("Method ", m.Method)
	}
	out := b.String()
	if len(out) <= maxMatchText {
		return out
	}
	cut := maxMatchText
	for cut > 0 && !utf8.RuneStart(out[cut]) {
		cut--
	}
	return out[:cut] + "…"
}

// maxMatchText bounds one match's text in a finding (our call, 2.4).
const maxMatchText = 512

// routes is "1 route" or "n routes"; a non-number is kept as is.
func routes(n string) string {
	switch v, err := strconv.Atoi(n); {
	case err != nil:
		return n
	case v == 1:
		return "1 route"
	}
	return n + " routes"
}
