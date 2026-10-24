package kgw24

import (
	"cmp"
	"slices"
	"sort"
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

// listenerState holds one listener's parsed rules and accumulates its count.
type listenerState struct {
	kinds    []objects.GroupKind
	kindText string
	sel      labels.Selector
	selErr   error
	// selText is the selector as written, for facts.
	selText string
	count   int
	unknown computed.UnknownReason
	// nsNames are the namespaces its Selector evaluated, with duplicates.
	nsNames []string
}

// newListenerStates parses each listener's allowed kinds and selector once.
func newListenerStates(r *computed.Resource) []listenerState {
	ls := make([]listenerState, len(r.Listeners))
	for j, l := range r.Listeners {
		st := &ls[j]
		st.kinds = allowedKinds(l)
		names := make([]string, len(st.kinds))
		for i, k := range st.kinds {
			names[i] = k.Kind
		}
		st.kindText = strings.Join(names, ", ")
		st.sel, st.selErr = parseSelector(l)
		if st.sel != nil {
			st.selText = metav1.FormatLabelSelector(l.AllowedRoutes.Namespaces.Selector)
		}
	}
	return ls
}

// memberBuf collects Members in candidate order with their listener index, so
// each resource's segment can be reordered by listener as written.
type memberBuf struct {
	m   []computed.Member
	idx []int32
	// facts is a scratch buffer reused across refs.
	facts []computed.Fact
}

func (s *memberBuf) Len() int           { return len(s.m) }
func (s *memberBuf) Less(i, j int) bool { return s.idx[i] < s.idx[j] }
func (s *memberBuf) Swap(i, j int) {
	s.m[i], s.m[j] = s.m[j], s.m[i]
	s.idx[i], s.idx[j] = s.idx[j], s.idx[i]
}

// attach computes view A (rules §4). Routes are indexed by the resource their
// parentRefs name, so each resource sees only its candidates.
//
// Deps slices are shared between results and are never mutated after sharing.
func attach(in computed.AttachmentInput) computed.AttachmentResult {
	expOn, expKnown := in.Experimental.Bool()

	// A denied ListenerSet is never evaluated (rules §3.1). On a duplicate
	// Ref, which the API cannot produce, the first in input order wins.
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

	// Size the results up front: they dominate allocation at scale. The
	// member bound counts listeners that pass the kind, port and section checks.
	states := make([][]listenerState, len(order))
	nCands, nListeners, nMembers := 0, 0, 0
	for k, i := range order {
		r := &in.Resources[i]
		if byRef[r.Ref] != k {
			continue
		}
		states[k] = newListenerStates(r)
		nCands += len(cands[k])
		nListeners += len(r.Listeners)
		if blanketUnknown(in, r) == "" {
			nMembers += memberBound(in, r, states[k], cands[k])
		}
	}
	res := computed.AttachmentResult{
		Outcomes: make([]computed.RefOutcome, 0, nCands),
		Counts:   make([]computed.ListenerCount, 0, nListeners),
	}
	ms := &memberBuf{m: make([]computed.Member, 0, nMembers), idx: make([]int32, 0, nMembers)}
	for k, i := range order {
		if byRef[in.Resources[i].Ref] != k {
			continue
		}
		start := len(ms.m)
		evalResource(&res, ms, in, &in.Resources[i], states[k], cands[k], !expKnown)
		// Members are grouped by resource, then listener as written.
		sort.Stable(&memberBuf{m: ms.m[start:], idx: ms.idx[start:]})
	}
	res.Members = ms.m
	slices.SortFunc(res.Outcomes, func(a, b computed.RefOutcome) int {
		return cmp.Or(compareRef(a.Route, b.Route), cmp.Compare(a.ParentRef, b.ParentRef))
	})
	return res
}

// memberBound is an upper bound on a resource's members: every check but hostnames, none of which allocate.
func memberBound(in computed.AttachmentInput, r *computed.Resource, ls []listenerState, cands []candidate) int {
	n := 0
	for _, c := range cands {
		rt := &in.Routes[c.route]
		for j := range r.Listeners {
			l, st := &r.Listeners[j], &ls[j]
			if st.selErr != nil || !kindAllowed(rt.Ref.Kind, st.kinds) {
				continue
			}
			v := namespacesParsed(*l, st.sel, st.selErr, r.Ref.Namespace, rt.Ref.Namespace, in.NamespaceLabels, in.NamespacesOK)
			if _, ok := listenerMatch(rt.ParentRefs[c.ref], l); ok && v.OK {
				n++
			}
		}
	}
	return n
}

// blanketUnknown is why every listener and ref of r is unknown, or "".
// Unknown scope ranks first: it affects every candidate (rules §3.3).
func blanketUnknown(in computed.AttachmentInput, r *computed.Resource) computed.UnknownReason {
	switch {
	case in.ScopeUnknown:
		return computed.UnknownScope
	case r.Acceptance == computed.NoStatus:
		return computed.UnknownListenerSetStatus
	}
	return ""
}

// evalResource runs processRoute for every candidate of one resource and
// appends its members, outcomes and counts (rules §4.1, §4.4).
func evalResource(res *computed.AttachmentResult, ms *memberBuf, in computed.AttachmentInput,
	r *computed.Resource, ls []listenerState, cands []candidate, expUnknown bool,
) {
	countDeps := make([]objects.Ref, 0, len(cands)+1)
	countDeps = append(countDeps, r.Ref)
	for _, c := range cands {
		countDeps = append(countDeps, in.Routes[c.route].Ref)
	}
	// Sorted once and shared by the counts without Namespace deps; full cap forces a copy on append.
	countDeps = sortRefs(countDeps)
	countDeps = countDeps[:len(countDeps):len(countDeps)]

	if why := blanketUnknown(in, r); why != "" {
		// Its listeners are unknown, and so is every ref naming it (rules §3.3).
		for _, c := range cands {
			res.Outcomes = append(res.Outcomes, computed.RefOutcome{
				Route: in.Routes[c.route].Ref, ParentRef: c.ref, Resource: r.Ref,
				Unknown: why, Rule: ruleAllowed,
				Deps: sortRefs([]objects.Ref{in.Routes[c.route].Ref, r.Ref}),
			})
		}
		for _, l := range r.Listeners {
			res.Counts = append(res.Counts, computed.ListenerCount{
				Resource: r.Ref, Listener: l.Name, Unknown: why, Deps: countDeps,
			})
		}
		return
	}

	for _, c := range cands {
		res.Outcomes = append(res.Outcomes, evalRef(ms, in, r, ls, c, expUnknown))
	}

	for j, l := range r.Listeners {
		st := &ls[j]
		lc := computed.ListenerCount{Resource: r.Ref, Listener: l.Name, Count: st.count, Unknown: st.unknown, Deps: countDeps}
		if st.selErr != nil {
			lc.Error = st.selErr.Error()
		}
		if lc.Unknown != "" {
			lc.Count = 0
		}
		if len(st.nsNames) > 0 {
			d := make([]objects.Ref, len(countDeps), len(countDeps)+len(st.nsNames))
			copy(d, countDeps)
			for _, ns := range st.nsNames {
				d = append(d, nsRef(ns))
			}
			lc.Deps = sortRefs(d)
		}
		res.Counts = append(res.Counts, lc)
	}
}

// evalRef is processRoute's loop for one parentRef, the `allowed` quirk as
// written: allowed is set before the section and port check (rules §4.1).
func evalRef(ms *memberBuf, in computed.AttachmentInput, r *computed.Resource, ls []listenerState,
	c candidate, expUnknown bool,
) computed.RefOutcome {
	rt := &in.Routes[c.route]
	pr := rt.ParentRefs[c.ref]
	expUnknown = expUnknown && rt.ExperimentalOnly
	hostKind := rt.Ref.Kind == "HTTPRoute" || rt.Ref.Kind == "GRPCRoute" || rt.Ref.Kind == "TLSRoute"
	var (
		allowed, matched, hosts bool
		unknown                 computed.UnknownReason
		facts                   = ms.facts[:0]
		passed                  []string
		// deps and nsDeps are shared by this ref's Members and its outcome.
		deps, nsDeps []objects.Ref
	)
	addFact := func(f computed.Fact) { facts = append(facts, f) }
	withNS := func() []objects.Ref {
		if nsDeps == nil {
			nsDeps = sortRefs([]objects.Ref{rt.Ref, r.Ref, nsRef(rt.Ref.Namespace)})
		}
		return nsDeps
	}
	// A known Selector verdict on any kind-matching listener makes the
	// Namespace a dep; deps then shares nsDeps, whose Namespace sorts first.
	for j := range ls {
		if ls[j].sel != nil && in.NamespacesOK && kindAllowed(rt.Ref.Kind, ls[j].kinds) {
			if withNS(); nsDeps[0] == nsRef(rt.Ref.Namespace) {
				deps = nsDeps[1:3:3]
			}
			break
		}
	}
	if expUnknown {
		unknown = computed.UnknownSetting
	}
	for j := range r.Listeners {
		l, st := &r.Listeners[j], &ls[j]
		if st.selErr != nil {
			addFact(computed.Fact{Listener: l.Name, Code: computed.FactSelectorError, Got: st.selErr.Error()})
			continue
		}
		if !kindAllowed(rt.Ref.Kind, st.kinds) {
			addFact(computed.Fact{Listener: l.Name, Code: computed.FactKind, Want: st.kindText, Got: rt.Ref.Kind})
			continue
		}
		v := namespacesParsed(*l, st.sel, st.selErr, r.Ref.Namespace, rt.Ref.Namespace, in.NamespaceLabels, in.NamespacesOK)
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
			withNS()
			// Consecutive candidates often share a namespace; sortRefs removes the rest.
			if ns := rt.Ref.Namespace; len(st.nsNames) == 0 || st.nsNames[len(st.nsNames)-1] != ns {
				st.nsNames = append(st.nsNames, ns)
			}
		}
		if !v.OK {
			addFact(nsFact(l.Name, st, r.Ref.Namespace, rt.Ref.Namespace, v, in))
			continue
		}
		allowed = true
		if f, ok := listenerMatch(pr, l); !ok {
			addFact(f)
			continue
		}
		matched = true
		eff, ok := hostnames(hostKind, l, rt)
		if !ok {
			addFact(computed.Fact{Listener: l.Name, Code: computed.FactHostname, Want: l.Hostname, Got: strings.Join(rt.Hostnames, ", ")})
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
		var md []objects.Ref
		if v.UsesSelector {
			md = withNS()
		} else {
			if deps == nil {
				deps = sortRefs([]objects.Ref{rt.Ref, r.Ref})
			}
			md = deps
		}
		ms.m = append(ms.m, computed.Member{
			Resource: r.Ref, Listener: l.Name, Route: rt.Ref, ParentRef: c.ref, Hostnames: eff, Deps: md,
		})
		ms.idx = append(ms.idx, int32(j)) // #nosec G115 -- a listener index fits in int32
	}

	// Facts go to a scratch buffer; only a rejection keeps a copy.
	ms.facts = facts[:0]
	if len(facts) == 0 {
		facts = nil
	}
	o := computed.RefOutcome{Route: rt.Ref, ParentRef: c.ref, Resource: r.Ref, Deps: nsDeps}
	if nsDeps == nil {
		if deps == nil {
			deps = sortRefs([]objects.Ref{rt.Ref, r.Ref})
		}
		o.Deps = deps
	}
	switch {
	case unknown != "":
		o.Unknown, o.Rule = unknown, ruleAllowed
	case !allowed:
		o.Outcome, o.Rule, o.Facts = computed.OutcomeNotAllowed, ruleAllowed, slices.Clip(slices.Clone(facts))
	case !matched:
		o.Outcome, o.Rule, o.Facts = computed.OutcomeNoParent, ruleSection, slices.Clip(slices.Clone(facts))
	case hostKind && !hosts:
		o.Outcome, o.Rule, o.Facts = computed.OutcomeNoHostname, ruleHostname, slices.Clip(slices.Clone(facts))
	default:
		o.Outcome, o.Listeners, o.Rule = computed.OutcomeAccepted, passed, ruleSection
		if hostKind {
			o.Rule = ruleHostname
		}
	}
	return o
}

// nsFact explains a namespace rejection: the selector and the namespace's
// labels, or the resource's namespace and the route's under Same.
func nsFact(listener string, st *listenerState, resourceNS, routeNS string, v nsVerdict,
	in computed.AttachmentInput,
) computed.Fact {
	if v.UsesSelector {
		got := labels.Set(in.NamespaceLabels[routeNS]).String()
		return computed.Fact{Listener: listener, Code: computed.FactNamespace, Want: st.selText, Got: got}
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
		want, got := strconv.Itoa(int(pr.Port)), strconv.Itoa(int(l.Port))
		return computed.Fact{Listener: l.Name, Code: computed.FactPort, Want: want, Got: got}, false
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

// nsRef is the dependency on a Namespace (rules §4.6).
func nsRef(ns string) objects.Ref { return objects.Ref{Kind: "Namespace", Name: ns} }

// sortRefs sorts and deduplicates refs in place.
func sortRefs(refs []objects.Ref) []objects.Ref {
	slices.SortFunc(refs, compareRef)
	return slices.Compact(refs)
}

// compareRef orders refs by group, kind, namespace and name.
func compareRef(a, b objects.Ref) int {
	return cmp.Or(cmp.Compare(a.Group, b.Group), cmp.Compare(a.Kind, b.Kind),
		cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
}
