package api

import (
	"fmt"
	"net/http"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

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
}

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
	if a.Reason == explore.ReasonReferenced {
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
	out := make([]MatchDTO, 0, len(ms))
	for _, m := range ms {
		dto := MatchDTO{Headers: keyMatches(m.Headers), QueryParams: keyMatches(m.QueryParams), Method: m.Method}
		if m.Path != nil {
			dto.Path = &PathMatchDTO{Type: m.Path.Type, Value: m.Path.Value}
		}
		out = append(out, dto)
	}
	return out
}

func (d *detail) rule(route objects.Ref, r objects.Rule) RuleDTO {
	out := RuleDTO{Index: r.Index, Name: r.Name, Matches: matchDTOs(r.Matches), Filters: d.filters(r.Filters), BackendRefs: make([]BackendRefDTO, 0, len(r.BackendRefs))}
	for _, b := range r.BackendRefs {
		out.BackendRefs = append(out.BackendRefs, d.backendRef(route, b))
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

// backendRef is b of route on the wire. A name delegation ref links its
// child; wildcard and label refs select, so they never link.
func (d *detail) backendRef(route objects.Ref, b objects.BackendRef) BackendRefDTO {
	out := BackendRefDTO{Ref: LinkedRef{Ref: b.Ref, Link: LinkNone}, Port: b.Port, Weight: b.Weight, WeightDefaulted: b.WeightDefaulted, Filters: d.filters(b.Filters)}
	if dl := b.Delegation; dl != nil {
		out.Delegation = &DelegationRefDTO{Mode: dl.Mode, Namespace: dl.Namespace, Name: dl.Name, Label: dl.Label}
		if dl.Mode == objects.DelegationName {
			child := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: dl.Namespace, Name: dl.Name}
			out.Ref = d.l.linked(child)
			out.Delegation.State = d.delegationState(route, child)
		}
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

// ancestors is the up-chain of route; hidden steps carry no identity.
func (d *detail) ancestors(route objects.Ref) ([]AncestorPathDTO, bool) {
	paths, truncated := d.ix.Ancestors(route, d.v.can)
	out := make([]AncestorPathDTO, 0, len(paths))
	for _, p := range paths {
		dto := AncestorPathDTO{Steps: make([]AncestorStepDTO, 0, len(p.Steps)), Cycle: p.Cycle}
		for _, s := range p.Steps {
			if s.Restricted {
				dto.Steps = append(dto.Steps, AncestorStepDTO{Restricted: true})
				continue
			}
			lr := d.l.linked(s.Ref)
			dto.Steps = append(dto.Steps, AncestorStepDTO{Ref: &lr})
		}
		out = append(out, dto)
	}
	return out, truncated
}
