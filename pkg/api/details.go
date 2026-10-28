package api

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Detail 404 messages. A denied, absent or unadmitted object gets the same
// one as the resource endpoint (D-013).
const (
	notFoundInScope = "resource not found in this install's scope"
	noDetailView    = "no detail view for this kind"
)

// detail is one detail request after its install was found.
type detail struct {
	h  *handler
	r  *http.Request
	p  *engine.Published
	i  *engine.InstallModel
	ix *explore.Index
	v  *view
	l  linker
	f  facts
	// links memoises link per ref for the many path steps of one delegation page.
	links map[objects.Ref]Link
	// steps counts the path steps the page has materialized (maxPagePathSteps).
	steps int
}

// maxPagePathSteps bounds the path steps one delegation page materializes.
// Without it a page costs rows × contexts × path depth.
var maxPagePathSteps = 100_000

// startDetail finds the install, or writes 503 or 404.
func (h *handler) startDetail(w http.ResponseWriter, r *http.Request) *detail {
	p, i := h.find(w, r)
	if i == nil {
		return nil
	}
	ix := i.Index
	if ix == nil {
		ix = &explore.Index{}
	}
	v := h.view(r)
	l := linkerFor(v, p.Model, i)
	return &detail{h: h, r: r, p: p, i: i, ix: ix, v: v, l: l, f: facts{ix: ix, v: v, l: l}}
}

// open returns the object ref names and its candidate block when the
// caller may open its detail page (design §7.1), or writes 404 and returns
// nil. Nothing else about ref is read before this check.
func (d *detail) open(w http.ResponseWriter, ref objects.Ref) (*unstructured.Unstructured, *Candidate) {
	var u *unstructured.Unstructured
	if d.l.link(ref) == LinkDetail {
		u, _ = stored(d.p.Model, ref)
	}
	if u == nil {
		writeErr(w, http.StatusNotFound, notFoundInScope)
		return nil, nil
	}
	a, _ := d.ix.Admission(ref)
	if a.Owned {
		return u, nil
	}
	c := &Candidate{Reason: a.Reason}
	if a.Reason == explore.ReasonReferenced || a.Reason == explore.ReasonSelected {
		// The first visible referrer; link() admitted ref only because one exists.
		for _, by := range a.Referrers {
			if d.v.can(by) {
				lr := d.l.linked(by)
				c.By = &lr
				break
			}
		}
	}
	return u, c
}

// count is the number of route rows among ids the caller may view (rule 5).
func (d *detail) count(ids []explore.RowID) int {
	return d.v.recount(ids, func(id explore.RowID) objects.Ref { return d.ix.Routes[id].Ref })
}

// routeCounts counts the visible routes under parent per attachment, as
// the routes list totals them; links through a hidden ListenerSet do not
// count (rule 3).
func (d *detail) routeCounts(parent objects.Ref) RouteCounts {
	var c RouteCounts
	for _, id := range d.ix.ByParent[parent] {
		if !d.v.can(d.ix.Routes[id].Ref) {
			continue
		}
		for _, x := range []struct {
			a explore.Attachment
			n *int
		}{{explore.AttachmentAccepted, &c.Accepted}, {explore.AttachmentNotAccepted, &c.NotAccepted},
			{explore.AttachmentPending, &c.Pending}, {explore.AttachmentUnwatched, &c.Unwatched}} {
			if d.ix.Under(id, parent, x.a, d.v.visible()) {
				*x.n++
			}
		}
	}
	return c
}

// condition is c of object u on the wire.
func condition(u *unstructured.Unstructured, c metav1.Condition) Condition {
	return Condition{Type: c.Type, Status: string(c.Status), Reason: c.Reason, Message: c.Message, ObservedGeneration: c.ObservedGeneration,
		LastTransitionTime: wireTime(c.LastTransitionTime.Time), Stale: objects.IsStale(u, c)}
}

func conditions(u *unstructured.Unstructured, cs []metav1.Condition) []Condition {
	out := make([]Condition, 0, len(cs))
	for _, c := range cs {
		out = append(out, condition(u, c))
	}
	return out
}

func groupKinds(gks []objects.GroupKind) []objects.GroupKind {
	if gks == nil {
		return []objects.GroupKind{}
	}
	return gks
}

// listeners joins resource u's spec listeners with their status blocks by
// name and adds view A's counts.
func (d *detail) listeners(u *unstructured.Unstructured, ls []objects.Listener, st []objects.ListenerStatus) []ListenerDTO {
	out := make([]ListenerDTO, 0, len(ls))
	for _, l := range ls {
		dto := ListenerDTO{Name: l.Name, Port: l.Port, Protocol: l.Protocol, Hostname: l.Hostname, AllowedRoutes: AllowedRoutesDTO{
			Namespaces: RouteNamespacesDTO{From: l.AllowedRoutes.Namespaces.From, Selector: l.AllowedRoutes.Namespaces.Selector},
			Kinds:      groupKinds(l.AllowedRoutes.Kinds)}}
		if t := l.TLS; t != nil {
			dto.TLS = &ListenerTLSDTO{Mode: t.Mode, CertificateRefs: make([]LinkedRef, 0, len(t.CertificateRefs))}
			for _, c := range t.CertificateRefs {
				dto.TLS.CertificateRefs = append(dto.TLS.CertificateRefs, LinkedRef{Ref: c, Link: LinkNone})
			}
		}
		if j := slices.IndexFunc(st, func(s objects.ListenerStatus) bool { return s.Name == l.Name }); j >= 0 {
			s := st[j]
			dto.Status = &ListenerStatusDTO{AttachedRoutes: s.AttachedRoutes, SupportedKinds: groupKinds(s.SupportedKinds), Conditions: conditions(u, s.Conditions)}
		}
		d.f.listener(objects.RefOf(u), &dto)
		out = append(out, dto)
	}
	return out
}

func (h *handler) gatewayDetail(w http.ResponseWriter, r *http.Request) {
	d := h.startDetail(w, r)
	if d == nil {
		return
	}
	ref := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: r.PathValue("ns"), Name: r.PathValue("name")}
	u, cand := d.open(w, ref)
	if u == nil {
		return
	}
	var v *objects.GatewayView
	var st health.State
	if id, ok := d.ix.FindGateway(ref); ok {
		v, st = d.ix.Gateways[id].View, d.ix.Gateways[id].State
	} else {
		v, st = objects.DecodeGateway(u), explore.RowState(ref, d.i.Health)
	}
	writeJSON(w, GatewayDetail{Install: d.i.Resolved.ID(), Ref: LinkedRef{Ref: ref, Link: LinkDetail}, Generation: u.GetGeneration(), Class: v.Class,
		Health: st, Candidate: cand, RuleSet: d.ix.RuleSet, Conditions: conditions(u, v.Conditions), Addresses: append([]string{}, v.StatusAddresses...),
		Listeners: d.listeners(u, v.Listeners, v.ListenerStatus), AttachedListenerSets: v.AttachedListenerSets,
		ListenerSets: d.listenerSetCount(ref), RouteCounts: d.routeCounts(ref), CollectedAt: wireTime(d.p.ConfirmedAt)})
}

// listenerSetCount is the total of the ListenerSets list for parent gw:
// visible ones only; a Gateway the install does not own is scanned for.
func (d *detail) listenerSetCount(gw objects.Ref) int {
	if id, ok := d.ix.FindGateway(gw); ok {
		return d.v.recount(d.ix.Gateways[id].ListenerSets, func(id explore.RowID) objects.Ref { return d.ix.ListenerSets[id].Ref })
	}
	n := 0
	for i := range d.ix.ListenerSets {
		if l := &d.ix.ListenerSets[i]; l.Parent == gw && d.v.can(l.Ref) {
			n++
		}
	}
	return n
}

// servedListenerSet returns the served ListenerSet kind group/kind.
func servedListenerSet(m *engine.Model, group, kind string) (kinds.Served, bool) {
	for _, k := range m.Served {
		if k.Category == kinds.CategoryListenerSet && k.Group == group && k.Kind == kind {
			return k, true
		}
	}
	return kinds.Served{}, false
}

// experimentalHint is D-018's hint as a sentence, for a ListenerSet with no
// status whose kind the controller watches only with experimental features.
func experimentalHint(k kinds.Served) string {
	return fmt.Sprintf(health.ExperimentalSentence, k.Kind+" "+k.Version)
}

func (h *handler) listenerSetDetail(w http.ResponseWriter, r *http.Request) {
	d := h.startDetail(w, r)
	if d == nil {
		return
	}
	k, ok := servedListenerSet(d.p.Model, r.PathValue("group"), r.PathValue("kind"))
	if !ok {
		writeErr(w, http.StatusNotFound, noDetailView)
		return
	}
	ref := objects.Ref{Group: k.Group, Kind: k.Kind, Namespace: r.PathValue("ns"), Name: r.PathValue("name")}
	u, cand := d.open(w, ref)
	if u == nil {
		return
	}
	var v *objects.ListenerSetView
	var st health.State
	if id, ok := d.ix.FindListenerSet(ref); ok {
		v, st = d.ix.ListenerSets[id].View, d.ix.ListenerSets[id].State
	} else {
		v, st = objects.DecodeListenerSet(u), explore.RowState(ref, d.i.Health)
	}
	parent := v.Parent.Ref()
	out := ListenerSetDetail{Install: d.i.Resolved.ID(), Ref: LinkedRef{Ref: ref, Link: LinkDetail}, Generation: u.GetGeneration(), Health: st,
		Candidate: cand, RuleSet: d.ix.RuleSet, Parent: d.l.linked(parent), Conditions: conditions(u, v.Conditions), Listeners: d.listeners(u, v.Listeners, v.ListenerStatus),
		RouteCounts: d.routeCounts(ref), CollectedAt: wireTime(d.p.ConfirmedAt)}
	if len(v.Conditions) == 0 && len(v.ListenerStatus) == 0 && k.ExperimentalOnly(d.i.Resolved.KgatewayVersion()) {
		out.ExperimentalHint = experimentalHint(k)
	}
	// The parent is read only when it opens: a hidden, absent or unadmitted
	// parent reads as not found, and admission never follows chains (D-013).
	var pu *unstructured.Unstructured
	if out.Parent.Link != LinkNone {
		pu, _ = stored(d.p.Model, parent)
	}
	out.ParentFound = pu != nil
	if acc := objects.FindCondition(v.Conditions, "Accepted"); pu != nil && acc != nil && acc.Status == metav1.ConditionFalse &&
		acc.Reason == "NotAllowed" && !objects.IsStale(u, *acc) {
		var al map[string]any
		if id, ok := d.ix.FindGateway(parent); ok {
			al = d.ix.Gateways[id].View.AllowedListeners
		} else {
			al = objects.DecodeGateway(pu).AllowedListeners
		}
		out.ParentAllowedListeners = &AllowedListenersDTO{Set: al != nil, Value: al, CRDHint: al == nil}
	}
	writeJSON(w, out)
}

func (h *handler) routeDetail(w http.ResponseWriter, r *http.Request) {
	d := h.startDetail(w, r)
	if d == nil {
		return
	}
	if r.PathValue("group") != objects.GatewayAPIGroup || r.PathValue("kind") != "HTTPRoute" {
		writeErr(w, http.StatusNotFound, noDetailView)
		return
	}
	ref := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: r.PathValue("ns"), Name: r.PathValue("name")}
	// Admission and the Authorizer run before the up-chain walk (Task 20).
	u, cand := d.open(w, ref)
	if u == nil {
		return
	}
	var v *objects.HTTPRouteView
	var st health.State
	id, row := d.ix.Row(ref)
	if row && d.ix.Routes[id].View != nil {
		v, st = d.ix.Routes[id].View, d.ix.Routes[id].State
	} else {
		v, st = objects.DecodeHTTPRoute(u), explore.RowState(ref, d.i.Health)
	}
	out := RouteDetail{Install: d.i.Resolved.ID(), Ref: LinkedRef{Ref: ref, Link: LinkDetail}, Generation: u.GetGeneration(), Health: st,
		Candidate: cand, RuleSet: d.ix.RuleSet, Hostnames: append([]string{}, v.Hostnames...), ParentRefs: d.specParents(v.ParentRefs),
		Parents: d.parents(u, v.Parents), Rules: make([]RuleDTO, 0, len(v.Rules)),
		ChildCount: d.count(d.ix.ByParentRoute[ref]), DeclaredCount: d.count(d.ix.Declared[ref]), CollectedAt: wireTime(d.p.ConfirmedAt)}
	if _, ok := u.GetAnnotations()[objects.InheritParentMatcherAnnotation]; ok {
		b := v.InheritParentMatcher
		out.Annotations.InheritParentMatcher = &b
	}
	if row {
		out.Attachments = d.attachments(id, v.ParentRefs)
	}
	for _, rule := range v.Rules {
		out.Rules = append(out.Rules, d.rule(ref, rule))
	}
	out.Effective, out.EffectiveTruncated = d.effective(ref, v.InheritParentMatcher)
	out.Ancestors, out.AncestorsTruncated = d.ancestors(ref)
	writeJSON(w, out)
}

// attachments is view A for each Gateway or ListenerSet parentRef of route
// row id, in parentRef order; nil when none was computed.
func (d *detail) attachments(id explore.RowID, refs []objects.ParentRef) []RouteAttachment {
	fs := d.ix.Routes[id].Computed
	if len(fs) == 0 {
		return nil
	}
	out := make([]RouteAttachment, 0, len(fs))
	for i := range fs {
		f := &fs[i]
		if f.Out.ParentRef >= len(refs) {
			continue
		}
		out = append(out, RouteAttachment{ParentRef: refs[f.Out.ParentRef], Index: f.Out.ParentRef, Target: d.l.linked(f.Out.Resource), Result: d.f.attachment(f)})
	}
	return out
}

// specParents is a route's spec parentRefs, each linked like a status target.
func (d *detail) specParents(ps []objects.ParentRef) []SpecParentRef {
	out := make([]SpecParentRef, 0, len(ps))
	for _, p := range ps {
		out = append(out, SpecParentRef{ParentRef: p, Target: d.l.linked(p.Ref())})
	}
	return out
}

// parents is route u's status.parents, this install's controllers' entries
// first, each group in written order.
func (d *detail) parents(u *unstructured.Unstructured, ps []objects.RouteParentStatus) []ParentStatus {
	out := make([]ParentStatus, 0, len(ps))
	for _, ours := range []bool{true, false} {
		for _, p := range ps {
			if slices.Contains(d.i.Resolved.ControllerNames, p.ControllerName) != ours {
				continue
			}
			out = append(out, ParentStatus{ParentRef: p.ParentRef, Target: d.l.linked(p.ParentRef.Ref()), ControllerName: p.ControllerName,
				Ours: ours, Conditions: conditions(u, p.Conditions)})
		}
	}
	return out
}

// defaultMatches is the CRD default for omitted rule matches.
var defaultMatches = []objects.Match{{Path: &objects.PathMatch{Type: "PathPrefix", Value: "/"}}}

func keyMatches(ks []objects.KeyMatch) []KeyMatchDTO {
	if ks == nil {
		return nil
	}
	out := make([]KeyMatchDTO, 0, len(ks))
	for _, k := range ks {
		out = append(out, KeyMatchDTO(k))
	}
	return out
}

// matchDTOs is ms on the wire; nil (omitted) gets the CRD default.
func matchDTOs(ms []objects.Match) []MatchDTO {
	if ms == nil {
		ms = defaultMatches
	}
	return matchList(ms)
}

// matchList is ms on the wire as computed: never nil, no default.
func matchList(ms []objects.Match) []MatchDTO {
	out := make([]MatchDTO, 0, len(ms))
	for _, m := range ms {
		out = append(out, matchDTO(m))
	}
	return out
}

// matchDTO is one match on the wire.
func matchDTO(m objects.Match) MatchDTO {
	dto := MatchDTO{Headers: keyMatches(m.Headers), QueryParams: keyMatches(m.QueryParams), Method: m.Method}
	if m.Path != nil {
		dto.Path = &PathMatchDTO{Type: m.Path.Type, Value: m.Path.Value}
	}
	return dto
}

func (d *detail) rule(route objects.Ref, r objects.Rule) RuleDTO {
	out := RuleDTO{Index: r.Index, Name: r.Name, Matches: matchDTOs(r.Matches), Filters: d.filters(r.Filters), BackendRefs: make([]BackendRefDTO, 0, len(r.BackendRefs))}
	for i, b := range r.BackendRefs {
		out.BackendRefs = append(out.BackendRefs, d.backendRef(route, r.Index, i, b))
	}
	return out
}

// filters keeps the written order; each ref carries its link.
func (d *detail) filters(fs []objects.Filter) []FilterDTO {
	out := make([]FilterDTO, 0, len(fs))
	for _, f := range fs {
		dto := FilterDTO{Type: f.Type, Config: f.Config}
		if e := f.ExtensionRef; e != nil {
			lr := d.l.linked(*e)
			dto.ExtensionRef = &lr
		}
		if m := f.Mirror; m != nil {
			dto.Mirror = &MirrorDTO{BackendRef: d.l.linked(m.Backend), Port: m.Port, Percent: m.Percent}
			if m.Fraction != nil {
				dto.Mirror.Fraction = &FractionDTO{Numerator: m.Fraction.Numerator, Denominator: m.Fraction.Denominator}
			}
		}
		out = append(out, dto)
	}
	return out
}

// backendRef is b, ref i of rule of route, on the wire. A name delegation
// ref links its child; wildcard and label refs select, so they never link.
func (d *detail) backendRef(route objects.Ref, rule, i int, b objects.BackendRef) BackendRefDTO {
	out := BackendRefDTO{Ref: LinkedRef{Ref: b.Ref, Link: LinkNone}, Port: b.Port, Weight: b.Weight, WeightDefaulted: b.WeightDefaulted, Filters: d.filters(b.Filters)}
	if dl := b.Delegation; dl != nil {
		out.Delegation = &DelegationRefDTO{Mode: dl.Mode, Namespace: dl.Namespace, Name: dl.Name, Label: dl.Label}
		if dl.Mode == objects.DelegationName {
			child := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: dl.Namespace, Name: dl.Name}
			out.Ref = d.l.linked(child)
			out.Delegation.State = d.delegationState(route, child)
		}
		d.refCounts(out.Delegation, explore.RefKey{Parent: route, Rule: rule, Ref: i})
		return out
	}
	out.Ref = d.l.linked(b.Ref)
	if b.Ref.Group == kinds.KgatewayGroup && b.Ref.Kind == "Backend" {
		out.Backend = d.backendState(b.Ref, out.Ref.Link)
	}
	return out
}

// delegationState is a name ref's state from status (design §7.3). The
// Authorizer decides on the ref alone, so a denied child and an absent
// denied one are both restricted.
func (d *detail) delegationState(parent, child objects.Ref) string {
	switch {
	case !d.v.can(child):
		return DelegationRestricted
	case slices.Contains(d.ix.ParentRoutes[child], parent):
		return DelegationReported
	}
	if u, _ := stored(d.p.Model, child); u != nil {
		return DelegationNotReported
	}
	return DelegationNotFound
}

// backendState is a Backend's state and Accepted condition: restricted when
// the caller may not view it, omitted when it does not open (absent or not
// admitted; admission never follows chains).
func (d *detail) backendState(ref objects.Ref, link Link) *BackendStateDTO {
	if !d.v.can(ref) {
		return &BackendStateDTO{Restricted: true}
	}
	if link == LinkNone {
		return nil
	}
	u, _ := stored(d.p.Model, ref)
	st, c := health.BackendState(u)
	out := &BackendStateDTO{State: st}
	if c != nil {
		cc := condition(u, *c)
		out.Accepted = &cc
	}
	return out
}

// ancestors is the up-chain of route. A hidden Gateway or ListenerSet step
// carries no identity; a hidden ancestor route its name and the matches it
// delegated under (design §7.4 rule 3, D-024).
func (d *detail) ancestors(route objects.Ref) ([]AncestorPathDTO, bool) {
	paths, truncated := d.ix.Ancestors(route, d.v.can)
	out := make([]AncestorPathDTO, 0, len(paths))
	for _, p := range paths {
		dto := AncestorPathDTO{Steps: make([]AncestorStepDTO, 0, len(p.Steps)), Cycle: p.Cycle}
		for k, s := range p.Steps {
			if s.Restricted {
				dto.Steps = append(dto.Steps, AncestorStepDTO{Restricted: true})
				continue
			}
			lr := d.l.linked(s.Ref)
			step := AncestorStepDTO{Ref: &lr}
			if s.Hidden && k+1 < len(p.Steps) {
				step.Matches = d.delegatedMatches(s.Ref, p.Steps[k+1].Ref)
			}
			dto.Steps = append(dto.Steps, step)
		}
		out = append(out, dto)
	}
	return out, truncated
}

// delegatedMatches are the distinct matches under which the hidden route
// parent delegated to child, from view B's retained contexts whose other
// dependencies the caller may view (design §7.4 rules 3, 4b).
func (d *detail) delegatedMatches(parent, child objects.Ref) []MatchDTO {
	var ms []objects.Match
	seen := map[string]bool{}
	for _, i := range d.ix.Delegation[parent] {
		r := &d.ix.DelegRows[i]
		if r.Key.Child != child {
			continue
		}
		for j := range r.Contexts {
			m := r.Contexts[j].ParentMatch
			if key := matchKey(m); d.v.allowDeps(d.ix, r.ContextDeps[j]) && !seen[key] {
				seen[key] = true
				ms = append(ms, m)
			}
		}
	}
	if ms == nil {
		return nil
	}
	return matchList(ms)
}

// matchKey is m as a string, equal for equal matches.
func matchKey(m objects.Match) string {
	var b strings.Builder
	if m.Path != nil {
		b.WriteString(m.Path.Type + "\x00" + m.Path.Value)
	}
	for _, ks := range [][]objects.KeyMatch{m.Headers, m.QueryParams} {
		b.WriteByte(1)
		for _, k := range ks {
			b.WriteString(k.Type + "\x00" + k.Name + "\x00" + k.Value + "\x00")
		}
	}
	b.WriteString("\x01" + m.Method)
	return b.String()
}

// refCounts adds view B's counts and merged check to the delegation ref k
// (design §7.3, §7.4 rule 5). Rows of hidden children are left out. Selected
// is unknown when the selection is (a partial selection or an unknown
// scope); Attached also when a counted row is unknown or restricted. A ref
// with no rows, such as one of a parent no root reaches, gets neither. A
// selection-restricted ref's check is restricted and its zero-child rows
// are left out; it still counts its visible children (our call, 2.4).
func (d *detail) refCounts(dto *DelegationRefDTO, k explore.RefKey) {
	ids := d.ix.RefRows[k]
	if len(ids) == 0 {
		// The edge cap cut it before selection: too large, not empty (rules §5.7, D-025).
		if d.ix.TooLarge[k] {
			c := d.f.unknown(ruleDelegation, computed.UnknownTooLarge)
			if d.ix.SelectionHidden(k, d.v.visible()) {
				c = d.f.restricted()
			}
			dto.Computed = &c
		}
		return
	}
	hidden := d.ix.SelectionHidden(k, d.v.visible())
	var c *Computed
	if hidden {
		rc := d.f.restricted()
		c = &rc
	}
	selected, attached, selKnown, attKnown := 0, 0, true, true
	for _, i := range ids {
		r := &d.ix.DelegRows[i]
		child := r.Key.Child != (objects.Ref{})
		if child && !d.v.can(r.Key.Child) || hidden && !child {
			continue
		}
		rc := d.f.delegation(r)
		if c == nil {
			c = &rc
		} else {
			m := mergeComputed(*c, rc)
			c = &m
		}
		if r.Outcome == computed.OutcomeUnknown && (!child || r.Unknown == computed.UnknownScope) {
			selKnown = false
		}
		if !child {
			continue
		}
		selected++
		switch {
		case rc.State == string(computed.StateRestricted) || r.Outcome == computed.OutcomeUnknown:
			attKnown = false
		case r.Outcome == computed.OutcomeAttached || r.Outcome == computed.OutcomePartial:
			attached++
		}
	}
	dto.Computed = c
	if selKnown {
		dto.Selected = &selected
		if attKnown {
			dto.Attached = &attached
		}
	}
}

// effective is view C of route per parent edge, and whether edges were cut
// (rules §5.7, §6). A hidden parent is named with link none (D-024). A
// restricted edge's hidden contexts are one trailing {restricted: true}, as
// on a delegation row (design §7.4 rule 4). A parent no root reached has no
// edge; when whether one could reach it is hidden, the caller gets the same
// restricted edge as a reached one would give. inherit is the route's
// inherit-parent-matcher annotation.
func (d *detail) effective(route objects.Ref, inherit bool) ([]EffectiveDTO, bool) {
	e := d.ix.Effective[route]
	out := make([]EffectiveDTO, 0, len(e.Edges))
	for j := range e.Edges {
		ed := &e.Edges[j]
		dto := EffectiveDTO{Parent: d.l.linked(ed.Parent), Mode: ed.Mode, ContextsTruncated: ed.ContextsTruncated,
			Contexts: make([]EffectiveContext, 0, len(ed.Contexts)), Computed: d.f.effective(ed, e.EdgeDeps[j])}
		for x := range ed.Contexts {
			if !d.v.allowDeps(d.ix, e.ContextDeps[j][x]) {
				continue
			}
			c := &ed.Contexts[x]
			pm := matchDTO(c.ParentMatch)
			ec := EffectiveContext{ParentMatch: &pm, Rules: make([]EffectiveRuleDTO, 0, len(c.Rules))}
			for _, er := range c.Rules {
				ec.Rules = append(ec.Rules, EffectiveRuleDTO{Index: er.Index, Kept: er.Kept, Matches: matchList(er.Matches), Reason: explainFit(er.Reason)})
			}
			dto.Contexts = append(dto.Contexts, ec)
		}
		if dto.Computed.State == string(computed.StateRestricted) {
			dto.Contexts, dto.ContextsTruncated = append(dto.Contexts, EffectiveContext{Restricted: true}), false
		}
		out = append(out, dto)
	}
	truncated := e.Truncated
	if n := len(out); d.unreachedEdges(route, inherit, &out) && len(out) > n {
		// Every parent is an HTTPRoute: edges sort by (namespace, name), as view C's do.
		slices.SortStableFunc(out, func(a, b EffectiveDTO) int {
			return cmp.Or(strings.Compare(a.Parent.Namespace, b.Parent.Namespace), strings.Compare(a.Parent.Name, b.Parent.Name))
		})
		if limit := computed.DefaultBounds.EffectiveEdges; len(out) > limit {
			out, truncated = out[:limit], true
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, truncated
}

// unreachedEdges appends a restricted edge to out for each parent of route
// whose rows no context reached and whose root eligibility the caller may not
// view; it reports whether route has such rows at all.
func (d *detail) unreachedEdges(route objects.Ref, inherit bool, out *[]EffectiveDTO) bool {
	ids := d.ix.Unreached[route]
	mode := computed.ModeDefault
	if inherit {
		mode = computed.ModeInherit
	}
	for _, i := range ids {
		r := &d.ix.DelegRows[i]
		p := r.Key.Parent
		if d.v.allowDeps(d.ix, r.Deps) || slices.ContainsFunc(*out, func(e EffectiveDTO) bool { return e.Parent.Ref == p }) {
			continue
		}
		*out = append(*out, EffectiveDTO{Parent: d.l.linked(p), Mode: mode, Contexts: []EffectiveContext{{Restricted: true}},
			Computed: d.f.restricted()})
	}
	return len(ids) > 0
}

// routeDelegation is GET …/routes/{group}/{kind}/{ns}/{name}/delegation:
// the route's view B rows, filtered and paged in their fixed order (design
// §7.1, §7.2). It opens like the route's detail page; rows of hidden
// children are left out before the total (design §7.4 rule 3).
func (h *handler) routeDelegation(w http.ResponseWriter, r *http.Request) {
	d := h.startDetail(w, r)
	if d == nil {
		return
	}
	if r.PathValue("group") != objects.GatewayAPIGroup || r.PathValue("kind") != "HTTPRoute" {
		writeErr(w, http.StatusNotFound, noDetailView)
		return
	}
	q, err := parseDelegationQuery(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ref := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: r.PathValue("ns"), Name: r.PathValue("name")}
	if u, _ := d.open(w, ref); u == nil {
		return
	}
	pg, err := d.ix.QueryDelegation(ref, q, d.v.visible())
	if err != nil {
		fail(w, err)
		return
	}
	pu, pv := d.routeView(ref)
	items := make([]DelegationRow, 0, len(pg.IDs))
	for _, id := range pg.IDs {
		if id < 0 {
			items = append(items, d.selectionRow(&d.ix.DelegRows[-1-id]))
			continue
		}
		items = append(items, d.delegationRow(&d.ix.DelegRows[id], pu, pv))
	}
	writeJSON(w, Page[DelegationRow]{Install: d.i.Resolved.ID(), Items: items, Total: pg.Total, Next: pg.Next, Prev: pg.Prev,
		CollectedAt: wireTime(d.p.ConfirmedAt)})
}

// delegationRow is view B row r of the parent stored as pu, pv for the
// caller. A restricted row has no outcome, reason or parent condition, and
// its hidden contexts are one trailing {restricted: true}: their number and
// the parent's reporter keys can depend on hidden objects (design §7.4 rule 4).
func (d *detail) delegationRow(r *explore.DelegRow, pu *unstructured.Unstructured, pv *objects.HTTPRouteView) DelegationRow {
	as := r.RefAsWritten
	out := DelegationRow{Rule: r.Key.Rule, Ref: r.Key.Ref, RefAsWritten: DelegationRefDTO{Mode: as.Mode, Namespace: as.Namespace, Name: as.Name, Label: as.Label},
		Allows: string(r.Allows), Contexts: make([]DelegationContext, 0, len(r.Contexts)), ContextsTruncated: r.ContextsTruncated, Computed: d.f.delegation(r)}
	shown := out.Computed.State != string(computed.StateRestricted)
	if shown {
		out.Outcome, out.Reason = string(r.Outcome), explainRow(r.Outcome, r.Reason, r.Unresolved)
		out.Observed.ParentCondition = d.parentCondition(pu, pv, r.Reporters)
	}
	if child := r.Key.Child; child != (objects.Ref{}) {
		lr := d.cachedLink(child)
		out.Child = &lr
		out.Observed.Entry = d.childEntry(child, r.ChildKey)
	}
	for j := range r.Contexts {
		if !d.v.allowDeps(d.ix, r.ContextDeps[j]) {
			continue
		}
		if d.steps += r.Contexts[j].Path.Len(); d.steps > maxPagePathSteps {
			out.ContextsTruncated = true
			break
		}
		out.Contexts = append(out.Contexts, d.delegationContext(r, j))
	}
	if !shown {
		out.Contexts, out.ContextsTruncated = append(out.Contexts, DelegationContext{Restricted: true}), false
	}
	return out
}

// selectionRow is the one row of a selection-restricted ref, the ref of r:
// it stands for hidden children and for "selects nothing" alike, so it has
// no child, outcome, reason or context (our call, 2.4).
func (d *detail) selectionRow(r *explore.DelegRow) DelegationRow {
	as := r.RefAsWritten
	return DelegationRow{Rule: r.Key.Rule, Ref: r.Key.Ref, RefAsWritten: DelegationRefDTO{Mode: as.Mode, Namespace: as.Namespace, Name: as.Name, Label: as.Label},
		Contexts: []DelegationContext{}, Computed: d.f.restricted()}
}

// delegationContext is retained context j of row r, whose dependencies the
// caller may view: routes on its path are named with their links, which is
// none for a hidden one, and never restrict it (design §7.4 rule 4b, D-024).
func (d *detail) delegationContext(r *explore.DelegRow, j int) DelegationContext {
	c := &r.Contexts[j]
	o := c.Outcome
	if o == "" {
		o = computed.OutcomeUnknown // an empty outcome counts as unknown
	}
	pm := matchDTO(c.ParentMatch)
	out := DelegationContext{ParentMatch: &pm, Path: make([]PathStepDTO, c.Path.Len()), Outcome: string(o)}
	// An unknown context's reason is its unknown code, as Computed.Reason is.
	if o == computed.OutcomeUnknown {
		out.Reason = string(c.Unknown)
	} else {
		out.Reason = explainRow(o, c.Reason, r.Unresolved)
	}
	i := len(out.Path)
	for s := range c.Path.All() { // last step first
		i--
		out.Path[i] = PathStepDTO{Ref: d.cachedLink(s.Route), Match: matchDTO(s.Match)}
	}
	return out
}

// cachedLink is d.l.linked memoised per request.
func (d *detail) cachedLink(ref objects.Ref) LinkedRef {
	if d.links == nil {
		d.links = map[objects.Ref]Link{}
	}
	l, ok := d.links[ref]
	if !ok {
		l = d.l.link(ref)
		d.links[ref] = l
	}
	return LinkedRef{Ref: ref, Link: l}
}

// routeView is route ref's object and decoded view, from its row when it has one.
func (d *detail) routeView(ref objects.Ref) (*unstructured.Unstructured, *objects.HTTPRouteView) {
	if id, ok := d.ix.Row(ref); ok && d.ix.Routes[id].View != nil {
		return d.ix.Routes[id].Obj, d.ix.Routes[id].View
	}
	if u, _ := stored(d.p.Model, ref); u != nil {
		return u, objects.DecodeHTTPRoute(u)
	}
	return nil, nil
}

// childEntry is child's status entry from the install's controllers under
// report key, as written (rules §7); nil when there is none.
func (d *detail) childEntry(child objects.Ref, key computed.ReportKey) *ParentStatus {
	if key == (computed.ReportKey{}) {
		return nil
	}
	u, v := d.routeView(child)
	if v == nil {
		return nil
	}
	for _, p := range v.Parents {
		if computed.KeyOf(p.Written) == key && slices.Contains(d.i.Resolved.ControllerNames, p.ControllerName) {
			return &ParentStatus{ParentRef: p.ParentRef, Target: d.cachedLink(p.ParentRef.Ref()), ControllerName: p.ControllerName,
				Ours: true, Conditions: conditions(u, p.Conditions)}
		}
	}
	return nil
}

// parentCondition is the parent's ResolvedRefs condition from its first
// entry of the install's controllers under one of keys, its reporter keys.
func (d *detail) parentCondition(u *unstructured.Unstructured, v *objects.HTTPRouteView, keys []computed.ReportKey) *Condition {
	if v == nil {
		return nil
	}
	for _, p := range v.Parents {
		if !slices.Contains(keys, computed.KeyOf(p.Written)) || !slices.Contains(d.i.Resolved.ControllerNames, p.ControllerName) {
			continue
		}
		if c := objects.FindCondition(p.Conditions, "ResolvedRefs"); c != nil {
			cc := condition(u, *c)
			return &cc
		}
	}
	return nil
}
