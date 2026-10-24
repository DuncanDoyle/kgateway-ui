package kgw23

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Rule names of view A: the stage that decided an outcome (rules §4.6).
const (
	ruleAllowed  = "allowed-routes"
	ruleSection  = "section-and-port"
	ruleHostname = "hostname-intersection"
)

// candidate is one parentRef of a route that names a resource.
type candidate struct{ route, ref int }

// listenerState accumulates one listener's count and members.
type listenerState struct {
	kinds    []objects.GroupKind
	kindText string
	// selErr is the listener error of a nil or invalid selector; selText the selector as written.
	selErr, selText string
	verdicts        map[string]nsVerdict
	count           int
	unknown         computed.UnknownReason
	members         []computed.Member
	nsDeps          []objects.Ref
}

// attach computes view A (rules §4). Routes are indexed by the resource their
// parentRefs name, so each resource sees only its candidates.
func attach(in computed.AttachmentInput) computed.AttachmentResult {
	expOn, expKnown := in.Experimental.Bool()

	// A denied ListenerSet is never evaluated (rules §3.1).
	order := make([]int, 0, len(in.Resources))
	for i, r := range in.Resources {
		if r.Acceptance != computed.Denied {
			order = append(order, i)
		}
	}
	slices.SortStableFunc(order, func(a, b int) int { return compareRef(in.Resources[a].Ref, in.Resources[b].Ref) })
	byRef := make(map[objects.Ref]int, len(order))
	for k, i := range order {
		if _, dup := byRef[in.Resources[i].Ref]; !dup {
			byRef[in.Resources[i].Ref] = k
		}
	}

	routes := make([]int, len(in.Routes))
	for i := range routes {
		routes[i] = i
	}
	slices.SortStableFunc(routes, func(a, b int) int { return compareRef(in.Routes[a].Ref, in.Routes[b].Ref) })
	cands := make([][]candidate, len(order))
	for _, ri := range routes {
		rt := &in.Routes[ri]
		// With the setting false, an experimental-only kind is not watched (rules §4.2).
		if rt.ExperimentalOnly && expKnown && !expOn {
			continue
		}
		for p, pr := range rt.ParentRefs {
			if k, ok := byRef[pr.Ref()]; ok {
				cands[k] = append(cands[k], candidate{ri, p})
			}
		}
	}

	// Preallocate: the result slices dominate allocation at scale.
	nCands, nListeners := 0, 0
	for k, i := range order {
		nCands += len(cands[k])
		nListeners += len(in.Resources[i].Listeners)
	}
	res := computed.AttachmentResult{
		Outcomes: make([]computed.RefOutcome, 0, nCands),
		Counts:   make([]computed.ListenerCount, 0, nListeners),
	}
	states := make([][]listenerState, 0, len(order))
	nMembers := 0
	for k, i := range order {
		if byRef[in.Resources[i].Ref] == k {
			ls := evalResource(&res, in, &in.Resources[i], cands[k], !expKnown)
			for j := range ls {
				nMembers += len(ls[j].members)
			}
			states = append(states, ls)
		}
	}
	// Members are grouped by resource, then listener as written.
	res.Members = make([]computed.Member, 0, nMembers)
	for _, ls := range states {
		for j := range ls {
			res.Members = append(res.Members, ls[j].members...)
		}
	}
	slices.SortFunc(res.Outcomes, func(a, b computed.RefOutcome) int {
		return cmp.Or(compareRef(a.Route, b.Route), cmp.Compare(a.ParentRef, b.ParentRef))
	})
	return res
}

// evalResource runs processRoute for every candidate of one resource and
// appends its outcomes and counts; it returns the listener states that hold its members (rules §4.1, §4.4).
func evalResource(res *computed.AttachmentResult, in computed.AttachmentInput, r *computed.Resource, cands []candidate, expUnknown bool) []listenerState {
	ls := make([]listenerState, len(r.Listeners))
	for j, l := range r.Listeners {
		ls[j].kinds = allowedKinds(l)
		names := make([]string, len(ls[j].kinds))
		for i, k := range ls[j].kinds {
			names[i] = k.Kind
		}
		ls[j].kindText = strings.Join(names, ", ")
		ls[j].selErr, ls[j].selText = selectorInfo(l)
	}
	countDeps := make([]objects.Ref, 0, len(cands)+1)
	countDeps = append(countDeps, r.Ref)
	for _, c := range cands {
		countDeps = append(countDeps, in.Routes[c.route].Ref)
	}

	if r.Acceptance == computed.NoStatus {
		// Its listeners are unknown, and so is every ref naming it (rules §3.3).
		for _, c := range cands {
			res.Outcomes = append(res.Outcomes, computed.RefOutcome{
				Route: in.Routes[c.route].Ref, ParentRef: c.ref, Resource: r.Ref,
				Unknown: computed.UnknownListenerSetStatus, Rule: ruleAllowed,
				Deps: sortRefs([]objects.Ref{in.Routes[c.route].Ref, r.Ref}),
			})
		}
		for _, l := range r.Listeners {
			res.Counts = append(res.Counts, computed.ListenerCount{
				Resource: r.Ref, Listener: l.Name, Unknown: computed.UnknownListenerSetStatus,
				Deps: sortRefs(slices.Clone(countDeps)),
			})
		}
		return nil
	}

	for _, c := range cands {
		res.Outcomes = append(res.Outcomes, evalRef(in, r, ls, c, expUnknown))
	}

	for j, l := range r.Listeners {
		st := &ls[j]
		lc := computed.ListenerCount{Resource: r.Ref, Listener: l.Name, Count: st.count, Unknown: st.unknown, Error: st.selErr}
		if lc.Unknown != "" {
			lc.Count = 0
		}
		lc.Deps = sortRefs(append(slices.Clone(countDeps), st.nsDeps...))
		res.Counts = append(res.Counts, lc)
	}
	return ls
}

// evalRef is processRoute's loop for one parentRef, the `allowed` quirk as
// written: allowed is set before the section and port check (rules §4.1).
func evalRef(in computed.AttachmentInput, r *computed.Resource, ls []listenerState, c candidate, expUnknown bool) computed.RefOutcome {
	rt := &in.Routes[c.route]
	pr := rt.ParentRefs[c.ref]
	expUnknown = expUnknown && rt.ExperimentalOnly
	hostKind := rt.Ref.Kind == "HTTPRoute" || rt.Ref.Kind == "GRPCRoute" || rt.Ref.Kind == "TLSRoute"
	var (
		allowed, matched, hosts, nsDep bool
		unknown                        computed.UnknownReason
		facts                          []computed.Fact
		passed                         []string
	)
	if expUnknown {
		unknown = computed.UnknownSetting
	}
	for j := range r.Listeners {
		l, st := &r.Listeners[j], &ls[j]
		if st.selErr != "" {
			facts = append(facts, computed.Fact{Listener: l.Name, Code: computed.FactSelectorError, Got: st.selErr})
			continue
		}
		if !kindAllowed(rt.Ref.Kind, st.kinds) {
			facts = append(facts, computed.Fact{Listener: l.Name, Code: computed.FactKind, Want: st.kindText, Got: rt.Ref.Kind})
			continue
		}
		v := st.verdict(*l, r.Ref.Namespace, rt.Ref.Namespace, in)
		if v.Unknown != "" {
			// The ref is unknown; the count only when the route would otherwise pass (rules §3.3).
			if unknown == "" {
				unknown = v.Unknown
			}
			if _, ok := listenerMatch(pr, l); ok {
				if _, ok := hostnames(hostKind, l, rt); ok && st.unknown == "" {
					st.unknown = v.Unknown
				}
			}
			continue
		}
		if v.UsesSelector {
			nsDep = true
			st.nsDeps = append(st.nsDeps, nsRef(rt.Ref.Namespace))
		}
		if !v.OK {
			facts = append(facts, nsFact(l.Name, st, r.Ref.Namespace, rt.Ref.Namespace, v, in))
			continue
		}
		allowed = true
		if f, ok := listenerMatch(pr, l); !ok {
			facts = append(facts, f)
			continue
		}
		matched = true
		eff, ok := hostnames(hostKind, l, rt)
		if !ok {
			facts = append(facts, computed.Fact{Listener: l.Name, Code: computed.FactHostname, Want: l.Hostname, Got: strings.Join(rt.Hostnames, ", ")})
			continue
		}
		hosts = true
		passed = append(passed, l.Name)
		if expUnknown {
			if st.unknown == "" {
				st.unknown = computed.UnknownSetting
			}
			continue
		}
		st.count++
		deps := []objects.Ref{rt.Ref, r.Ref}
		if v.UsesSelector {
			deps = append(deps, nsRef(rt.Ref.Namespace))
		}
		st.members = append(st.members, computed.Member{Resource: r.Ref, Listener: l.Name, Route: rt.Ref, ParentRef: c.ref, Hostnames: eff, Deps: sortRefs(deps)})
	}

	deps := []objects.Ref{rt.Ref, r.Ref}
	if nsDep {
		deps = append(deps, nsRef(rt.Ref.Namespace))
	}
	o := computed.RefOutcome{Route: rt.Ref, ParentRef: c.ref, Resource: r.Ref, Deps: sortRefs(deps)}
	switch {
	case unknown != "":
		o.Unknown, o.Rule = unknown, ruleAllowed
	case !allowed:
		o.Outcome, o.Rule, o.Facts = computed.OutcomeNotAllowed, ruleAllowed, facts
	case !matched:
		o.Outcome, o.Rule, o.Facts = computed.OutcomeNoParent, ruleSection, facts
	case hostKind && !hosts:
		o.Outcome, o.Rule, o.Facts = computed.OutcomeNoHostname, ruleHostname, facts
	default:
		o.Outcome, o.Listeners, o.Rule = computed.OutcomeAccepted, passed, ruleSection
		if hostKind {
			o.Rule = ruleHostname
		}
	}
	return o
}

// verdict caches the namespace verdict per route namespace, so a selector is parsed once per namespace.
func (st *listenerState) verdict(l objects.Listener, resourceNS, routeNS string, in computed.AttachmentInput) nsVerdict {
	if v, ok := st.verdicts[routeNS]; ok {
		return v
	}
	if st.verdicts == nil {
		st.verdicts = map[string]nsVerdict{}
	}
	v := namespaces(l, resourceNS, routeNS, in.NamespaceLabels, in.NamespacesOK)
	st.verdicts[routeNS] = v
	return v
}

// selectorInfo returns the listener error of a Selector listener, with the
// controller's text for a nil selector, or the selector as written.
func selectorInfo(l objects.Listener) (errMsg, text string) {
	ns := l.AllowedRoutes.Namespaces
	if ns.From != "Selector" {
		return "", ""
	}
	if ns.Selector == nil {
		return "selector must be set", ""
	}
	if _, err := metav1.LabelSelectorAsSelector(ns.Selector); err != nil {
		return err.Error(), ""
	}
	return "", metav1.FormatLabelSelector(ns.Selector)
}

// nsFact explains a namespace rejection: the selector and the namespace's
// labels, or the resource's namespace and the route's under Same.
func nsFact(listener string, st *listenerState, resourceNS, routeNS string, v nsVerdict, in computed.AttachmentInput) computed.Fact {
	if v.UsesSelector {
		return computed.Fact{Listener: listener, Code: computed.FactNamespace, Want: st.selText, Got: labels.Set(in.NamespaceLabels[routeNS]).String()}
	}
	return computed.Fact{Listener: listener, Code: computed.FactNamespace, Want: resourceNS, Got: routeNS}
}

// kindAllowed compares the kind only, as the controller does.
func kindAllowed(kind string, allowed []objects.GroupKind) bool {
	for _, k := range allowed {
		if k.Kind == kind {
			return true
		}
	}
	return false
}

// listenerMatch is parentRefMatchListener: port first, then sectionName. A zero port is unset.
func listenerMatch(pr objects.ParentRef, l *objects.Listener) (computed.Fact, bool) {
	if pr.Port != 0 && pr.Port != l.Port {
		return computed.Fact{Listener: l.Name, Code: computed.FactPort, Want: strconv.Itoa(int(pr.Port)), Got: strconv.Itoa(int(l.Port))}, false
	}
	if pr.SectionName != "" && pr.SectionName != l.Name {
		return computed.Fact{Listener: l.Name, Code: computed.FactSection, Want: pr.SectionName, Got: l.Name}, false
	}
	return computed.Fact{}, true
}

// hostnames intersects only for HTTPRoute, GRPCRoute and TLSRoute (rules §4.1).
func hostnames(hostKind bool, l *objects.Listener, rt *computed.Route) ([]string, bool) {
	if !hostKind {
		return nil, true
	}
	ok, eff := intersect(l.Hostname, rt.Hostnames)
	return eff, ok
}

func nsRef(ns string) objects.Ref { return objects.Ref{Kind: "Namespace", Name: ns} }

// sortRefs sorts and deduplicates refs in place.
func sortRefs(refs []objects.Ref) []objects.Ref {
	slices.SortFunc(refs, compareRef)
	return slices.Compact(refs)
}

func compareRef(a, b objects.Ref) int {
	return cmp.Or(cmp.Compare(a.Group, b.Group), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
}
