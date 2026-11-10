// Package scale builds deterministic worlds of the scale tiers of design §9.6
// for benchmarks and scale tests. World returns unstructured objects only;
// typed Namespaces and Deployments come from package testutil. The Scale*
// functions return the typed Phase 3 shapes.
package scale

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// Tier is the shape of one scale tier (design §9.6).
type Tier struct {
	Name                          string
	Gateways, ListenersPerGateway int
	HTTPRoutes                    int
	// ParentRefsMean is the mean parentRef count of non-delegated routes.
	ParentRefsMean float64
	ListenerSets   int
	// DelegatedPct is the percentage of HTTPRoutes that are delegated children.
	DelegatedPct float64
	// MaxDepth and MaxFanOut bound the delegation trees; the lattice is exempt.
	MaxDepth, MaxFanOut int
	// ParentMatches is the number of path matches of a top-level delegating
	// rule; a child has one per incoming match of each delegator.
	ParentMatches        int
	Namespaces, Backends int
	// LatticeDepth is the number of layers of the repeated diamond; 0 means none.
	LatticeDepth int
	// Contradict inverts every route's Accepted status and adds one to every
	// attachedRoutes, so every view A check contradicts. It also drops every
	// child entry and makes delegating parents ResolvedRefs=False, so view B's
	// checks contradict too (PF2).
	Contradict bool

	// Phase 3 shapes. World ignores them; the Scale* functions use them.
	// Services is the total and ReferencedServices those LivePhase3's routes name
	// in backendRefs; every Service has at least one EndpointSlice.
	Services, ReferencedServices, EndpointSlices int
	// ProxyPodsPerGateway is the proxy Pod count of each Gateway.
	ProxyPodsPerGateway int
	// WarningEvents is the number of Warning Events on proxy Pods.
	WarningEvents int
	// GatewayAPICRDs is the number of Gateway API CRDs with metadata.
	GatewayAPICRDs int
	// IsWorst adds long-named Gateways, makes every Gateway reject, 5 % of
	// referenced Services lack a ready endpoint and every proxy Pod restart.
	// World adds the analyzer's codes 013–018 (see IssueEvery).
	IsWorst bool
	// MultiRulePct is the percentage of leaf routes with three rules, and
	// MultiRefPct that of rules with three weighted backendRefs (0 in every tier;
	// see Paths). One in ten multi-ref rules carries a weight-0 ref and one in
	// twenty delegating rules also names an ordinary Backend.
	MultiRulePct, MultiRefPct int
}

// Shape is the part of t the typed Phase 3 generators in package testutil need.
func (t Tier) Shape() testutil.ScaleShape {
	return testutil.ScaleShape{Gateways: t.Gateways, Namespaces: t.Namespaces, Services: t.Services, ReferencedServices: t.ReferencedServices,
		EndpointSlices: t.EndpointSlices, ProxyPodsPerGateway: t.ProxyPodsPerGateway, WarningEvents: t.WarningEvents,
		GatewayAPICRDs: t.GatewayAPICRDs, Worst: t.IsWorst}
}

// Worst is t with every Gateway rejecting, 5 % of referenced Services without
// a ready endpoint and every proxy Pod restarted once, plus long-named Gateways.
func (t Tier) Worst() Tier {
	t.Name += "-worst"
	t.IsWorst = true
	return t
}

// Paths is t with multi-rule, multi-ref and mixed delegation routes, which
// multiply the path model's rows (rules × refs). The rest of the world is unchanged.
func (t Tier) Paths() Tier {
	t.Name += "-paths"
	t.MultiRulePct, t.MultiRefPct = 30, 30
	return t
}

// Contradicting is t with every view A check contradicting its status.
func (t Tier) Contradicting() Tier {
	t.Name += "-contradicting"
	t.Contradict = true
	return t
}

// The tiers of design §9.6, at their benchmark sizes.
var (
	Typical = Tier{Name: "typical", Gateways: 30, ListenersPerGateway: 3, HTTPRoutes: 500, ParentRefsMean: 1.2, ListenerSets: 10,
		DelegatedPct: 10, MaxDepth: 2, MaxFanOut: 10, ParentMatches: 1, Namespaces: 20, Backends: 50,
		Services: 600, ReferencedServices: 300, EndpointSlices: 700, ProxyPodsPerGateway: 2, WarningEvents: 50, GatewayAPICRDs: 10}
	Large = Tier{Name: "large", Gateways: 100, ListenersPerGateway: 4, HTTPRoutes: 2000, ParentRefsMean: 1.3, ListenerSets: 200,
		DelegatedPct: 10, MaxDepth: 3, MaxFanOut: 50, ParentMatches: 2, Namespaces: 100, Backends: 200,
		Services: 5000, ReferencedServices: 2000, EndpointSlices: 6000, ProxyPodsPerGateway: 2, WarningEvents: 1000, GatewayAPICRDs: 10}
	Extreme = Tier{Name: "extreme", Gateways: 500, ListenersPerGateway: 4, HTTPRoutes: 5000, ParentRefsMean: 1.5, ListenerSets: 2000,
		DelegatedPct: 10, MaxDepth: 4, MaxFanOut: 100, ParentMatches: 4, Namespaces: 500, Backends: 1000, LatticeDepth: 8,
		Services: 25000, ReferencedServices: 10000, EndpointSlices: 30000, ProxyPodsPerGateway: 3, WarningEvents: 10000, GatewayAPICRDs: 10}
)

// Tiers lists Typical, Large and Extreme.
var Tiers = []Tier{Typical, Large, Extreme}

// Class is the GatewayClass of every scale Gateway.
const Class = "kgateway"

// SelectorListener is the listener of each Gateway that admits only
// namespaces labelled testutil.ScaleTeamLabel.
const SelectorListener = "web"

// FailedEvery makes every FailedEvery-th parentRef, in generation order, one
// the listeners reject (Accepted=False/NotAllowedByListeners): 5 %. An
// HTTPRoute or GRPCRoute names a ListenerSet in another namespace; a TCPRoute
// or TLSRoute a Gateway without a listener for its kind.
const FailedEvery = 20

// MixedEvery makes every MixedEvery-th Gateway carry a TCP and a TLS
// listener, the parents of TCPRoutes and TLSRoutes.
const MixedEvery = 10

// The worst case's analyzer findings (sub-phase 3.3). Top-level leaf
// HTTPRoutes are counted in generation order: the 0th of every IssueEvery
// names a missing Backend (013), the 10th one in another namespace without a
// grant (014), the 5th of every StaleEvery has stale status (003, which feeds
// 018) and the 15th of every ReplacedEvery a dropped rule naming a policy (015).
// Gateway ConflictAt of every ConflictEvery has a conflicted listener (017), and
// every PolicyEvery-th namespace a TrafficPolicy whose target is missing (016).
const (
	IssueEvery    = 20
	StaleEvery    = 40
	ReplacedEvery = 100
	ConflictEvery = 10
	ConflictAt    = 4
	PolicyEvery   = 5
	// MissingBackend is the Backend a 013 route names; GonePolicy the policy in each
	// PolicyEvery-th namespace, whose only target is Gateway GoneGateway.
	MissingBackend = "be-missing"
	GonePolicy     = "tp-gone"
	GoneGateway    = "gw-gone"
)

// issue is a worst-case route's analyzer shape; zero is none.
type issue int

const (
	issueMissing issue = iota + 1
	issueNotPermitted
	issueStale
	issueReplaced
)

const (
	gwAPI      = "gateway.networking.k8s.io/v1"
	backendAPI = "gateway.kgateway.dev/v1alpha1"
	sideKind   = 0.05 // share of HTTPRoutes added as each of GRPC, TCP and TLS routes
)

// route is one generated route before it becomes an object.
type route struct {
	kind, ns, name string
	parents        []objects.ParentRef // Gateway and ListenerSet parentRefs
	failed         []bool              // per parent: Accepted=False
	sections       []string            // per parent: sectionName or ""
	delegators     []int               // indices of HTTPRoutes delegating to this one
	children       []int               // indices of HTTPRoutes this one delegates to
	explicit       bool                // a child naming its parents in spec.parentRefs
	segment        string              // path segment of a lattice node, shared by its layer; else its name
	backend        string              // Backend name of a leaf rule
	backendNS      string              // its namespace when not the route's
	issue          issue               // the worst case's analyzer shape
}

// world carries generation state.
type world struct {
	t        Tier
	rng      *rand.Rand
	routes   []*route
	gwNS     []string
	lsNS     []string
	lsByNS   [][]int    // namespace index → its ListenerSets
	nsBE     [][]string // namespace index → its Backends
	beNext   []int      // namespace index → next Backend to use
	refCount int        // parentRefs generated so far
	paths    map[*route][]string
}

func gwName(i int) string { return testutil.ScaleGateway(i) }
func lsName(i int) string { return fmt.Sprintf("ls-%04d", i) }
func beName(i int) string { return fmt.Sprintf("be-%04d", i) }

// World returns tier t's GatewayClass, Gateways, ListenerSets, Backends
// and routes, identical for equal seeds. Every route parentRef has a status
// entry, Accepted=True except every FailedEvery-th one, as view A computes
// them (inverted under Contradict). Delegated children fit every incoming
// match and have an entry under the parent route's ref for every delegating
// parent, as view B computes them (PF2); leaves reference Backends.
// Namespaces come from testutil.ScaleNamespaces.
func World(t Tier, seed int64) []*unstructured.Unstructured {
	w := &world{t: t, rng: rand.New(rand.NewPCG(uint64(seed), 0x5ca1e)), paths: map[*route][]string{}} //nolint:gosec // deterministic test data
	for i := range t.Gateways {
		w.gwNS = append(w.gwNS, testutil.ScaleNamespace(i%t.Namespaces))
	}
	w.lsByNS = make([][]int, t.Namespaces)
	for i := range t.ListenerSets {
		w.lsNS = append(w.lsNS, w.gwNS[i%t.Gateways])
		ni := nsIndex(w.lsNS[i])
		w.lsByNS[ni] = append(w.lsByNS[ni], i)
	}
	w.nsBE = make([][]string, t.Namespaces)
	w.beNext = make([]int, t.Namespaces)
	for i := range t.Backends {
		w.nsBE[i%t.Namespaces] = append(w.nsBE[i%t.Namespaces], beName(i))
	}
	w.genRoutes()
	if t.IsWorst {
		w.issues()
	}
	return w.objects()
}

// issues gives the worst case's top-level leaf HTTPRoutes their analyzer shapes (IssueEvery).
func (w *world) issues() {
	k := 0
	for _, r := range w.routes {
		if r.kind != "HTTPRoute" || len(r.parents) == 0 || len(r.children) > 0 {
			continue
		}
		switch {
		case k%IssueEvery == 0:
			r.issue, r.backend = issueMissing, MissingBackend
		case k%IssueEvery == IssueEvery/2:
			r.issue, r.backendNS = issueNotPermitted, testutil.ScaleNamespace((nsIndex(r.ns)+1)%w.t.Namespaces)
		case k%StaleEvery == 5:
			r.issue = issueStale
		case k%ReplacedEvery == 15:
			r.issue = issueReplaced
		}
		k++
	}
}

// genRoutes draws every route: top-level HTTPRoutes, delegated children,
// then the GRPC, TCP and TLS routes.
func (w *world) genRoutes() {
	t := w.t
	children := int(math.Round(float64(t.HTTPRoutes) * t.DelegatedPct / 100))
	top := t.HTTPRoutes - children
	for i := range t.HTTPRoutes {
		r := &route{kind: "HTTPRoute", name: fmt.Sprintf("r-%05d", i)}
		if i < top {
			r.ns = testutil.ScaleNamespace(w.rng.IntN(t.Namespaces))
			w.attach(r)
		}
		w.routes = append(w.routes, r)
	}
	w.delegate(top, children)
	for _, kind := range []string{"GRPCRoute", "TCPRoute", "TLSRoute"} {
		for range int(math.Round(float64(t.HTTPRoutes) * sideKind)) {
			r := &route{kind: kind, name: fmt.Sprintf("r-%05d", len(w.routes)), ns: testutil.ScaleNamespace(w.rng.IntN(t.Namespaces))}
			w.attach(r)
			w.routes = append(w.routes, r)
		}
	}
	for _, r := range w.routes {
		if len(r.children) == 0 {
			ni := nsIndex(r.ns)
			bes := w.nsBE[ni]
			r.backend = bes[w.beNext[ni]%len(bes)]
			w.beNext[ni]++
		}
	}
}

// attach draws r's parentRefs: 1 + Binomial(3, (mean−1)/3) distinct parents
// that the listeners admit, but for every FailedEvery-th. An HTTPRoute or
// GRPCRoute names a ListenerSet of its namespace a tenth of the time, else a
// Gateway; a TCPRoute or TLSRoute names a mixed Gateway. A fifth of the
// Gateway refs carry a sectionName of a listener for the kind.
func (w *world) attach(r *route) {
	p := (w.t.ParentRefsMean - 1) / 3
	n := 1
	for range 3 {
		if w.rng.Float64() < p {
			n++
		}
	}
	labelled := testutil.ScaleLabelled(nsIndex(r.ns))
	http := r.kind == "HTTPRoute" || r.kind == "GRPCRoute"
	seen := map[objects.ParentRef]bool{}
	for len(r.parents) < n {
		failed := (w.refCount+1)%FailedEvery == 0
		var ref objects.ParentRef
		section := ""
		switch {
		case failed && http:
			i := w.rng.IntN(w.t.ListenerSets)
			if w.lsNS[i] == r.ns {
				continue
			}
			ref = w.lsRef(i)
		case failed:
			i := w.rng.IntN(w.t.Gateways)
			if mixed(i) {
				continue
			}
			ref = testutil.GwRef(w.gwNS[i], gwName(i))
		case http && w.rng.Float64() < 0.1 && len(w.lsByNS[nsIndex(r.ns)]) > 0:
			own := w.lsByNS[nsIndex(r.ns)]
			ref = w.lsRef(own[w.rng.IntN(len(own))])
		case http:
			i := w.rng.IntN(w.t.Gateways)
			if len(w.httpSections(i)) == 0 && !labelled {
				continue // only SelectorListener would admit it
			}
			ref = testutil.GwRef(w.gwNS[i], gwName(i))
			if w.rng.Float64() < 0.2 {
				if s := w.httpSections(i); len(s) > 0 {
					section = s[w.rng.IntN(len(s))]
				}
				if labelled && w.rng.Float64() < 0.5 {
					section = SelectorListener
				}
			}
		default:
			i := w.rng.IntN(w.t.Gateways/MixedEvery)*MixedEvery + MixedEvery - 1
			ref = testutil.GwRef(w.gwNS[i], gwName(i))
			if w.rng.Float64() < 0.2 {
				for j := range w.t.ListenersPerGateway {
					if allows(w.protocol(i, j), r.kind) {
						section = listenerName(j, w.t.ListenersPerGateway)
					}
				}
			}
		}
		key := ref
		key.SectionName = section
		if seen[key] {
			continue
		}
		seen[key] = true
		w.refCount++
		r.parents = append(r.parents, key)
		r.sections = append(r.sections, section)
		r.failed = append(r.failed, failed)
	}
}

func (w *world) lsRef(i int) objects.ParentRef {
	return objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: w.lsNS[i], Name: lsName(i)}
}

// mixed reports whether Gateway i has a TCP and a TLS listener.
func mixed(i int) bool { return i%MixedEvery == MixedEvery-1 }

// protocol is the protocol of Gateway i's listener j: the two before
// SelectorListener are TCP and TLS on a mixed Gateway, every other is HTTP.
func (w *world) protocol(i, j int) string {
	n := w.t.ListenersPerGateway
	switch {
	case !mixed(i) || j == n-1:
		return "HTTP"
	case j == n-3:
		return "TCP"
	case j == n-2:
		return "TLS"
	}
	return "HTTP"
}

// httpSections names Gateway i's HTTP listeners but SelectorListener.
func (w *world) httpSections(i int) []string {
	var out []string
	for j := range w.t.ListenersPerGateway - 1 {
		if w.protocol(i, j) == "HTTP" {
			out = append(out, listenerName(j, w.t.ListenersPerGateway))
		}
	}
	return out
}

// allows reports whether a listener of protocol admits kind by default.
func allows(protocol, kind string) bool {
	switch protocol {
	case "TCP":
		return kind == "TCPRoute"
	case "TLS":
		return kind == "TLSRoute"
	}
	return kind == "HTTPRoute" || kind == "GRPCRoute"
}

// delegate turns HTTPRoutes [top, top+children) into delegated children: the
// lattice first, then trees whose roots are accepted top-level routes in random order.
// The first tree reaches MaxFanOut at its root and MaxDepth along its first child.
func (w *world) delegate(top, children int) {
	t := w.t
	// A route whose every parentRef fails is no root: nothing would reach its children (rules §5.1).
	roots := slices.DeleteFunc(w.rng.Perm(top), func(i int) bool { return !slices.Contains(w.routes[i].failed, false) })
	next := top
	end := top + children
	link := func(p, c int) {
		w.routes[p].children = append(w.routes[p].children, c)
		w.routes[c].delegators = append(w.routes[c].delegators, p)
	}
	// newChild places child next in parent p's namespace, or a random one for a fifth of them.
	newChild := func(p int) int {
		c := next
		next++
		cr := w.routes[c]
		cr.ns = w.routes[p].ns
		if w.rng.Float64() < 0.2 {
			cr.ns = testutil.ScaleNamespace(w.rng.IntN(t.Namespaces))
		}
		cr.explicit = w.rng.IntN(2) == 0
		link(p, c)
		return c
	}
	if t.LatticeDepth > 0 && end-next >= 2*t.LatticeDepth {
		prev := []int{roots[0]}
		roots = roots[1:]
		ns := w.routes[prev[0]].ns
		for l := range t.LatticeDepth {
			layer := []int{next, next + 1}
			next += 2
			for _, c := range layer {
				w.routes[c].ns = ns
				w.routes[c].segment = fmt.Sprintf("layer-%d", l)
				for _, p := range prev {
					link(p, c)
				}
			}
			prev = layer
		}
	}
	type node struct{ i, depth int }
	for first := true; next < end; first = false {
		root := roots[0]
		roots = roots[1:]
		fan := min(t.MaxFanOut, end-next)
		if !first {
			fan = 1 + w.rng.IntN(fan)
		}
		queue := []node{}
		for range fan {
			queue = append(queue, node{newChild(root), 1})
		}
		for chain := first; len(queue) > 0 && next < end; {
			n := queue[0]
			queue = queue[1:]
			if n.depth >= t.MaxDepth || !(chain || w.rng.Float64() < 0.25) {
				continue
			}
			chain = false
			sub := 1 + w.rng.IntN(min(t.MaxFanOut, 5, end-next))
			added := []node{}
			for range sub {
				added = append(added, node{newChild(n.i), n.depth + 1})
			}
			if first && n.depth+1 < t.MaxDepth {
				chain = true // keep descending along the first new child
				queue = append(added, queue...)
				continue
			}
			queue = append(queue, added...)
		}
	}
}

// objects renders the world, counting attachedRoutes per listener as the
// controller would for accepted, non-delegated parentRefs; Contradict adds one.
func (w *world) objects() []*unstructured.Unstructured {
	t := w.t
	gwAttached := map[string][]int{} // ns/name → per listener
	lsAttached := map[string]int{}
	listeners := make([]string, t.ListenersPerGateway)
	for j := range listeners {
		listeners[j] = listenerName(j, t.ListenersPerGateway)
	}
	for i := range t.Gateways {
		gwAttached[w.gwNS[i]+"/"+gwName(i)] = make([]int, t.ListenersPerGateway)
	}
	for _, r := range w.routes {
		labelled := testutil.ScaleLabelled(nsIndex(r.ns))
		for k, p := range r.parents {
			if r.failed[k] {
				continue
			}
			if p.Kind == "ListenerSet" {
				lsAttached[p.Namespace+"/"+p.Name]++
				continue
			}
			counts := gwAttached[p.Namespace+"/"+p.Name]
			gi := gwIndex(p.Name)
			for j, l := range listeners {
				if allows(w.protocol(gi, j), r.kind) && (p.SectionName == "" || p.SectionName == l) && (l != SelectorListener || labelled) {
					counts[j]++
				}
			}
		}
	}

	extra := 0
	if t.Contradict {
		extra = 1
	}
	out := []*unstructured.Unstructured{testutil.GatewayClass(Class, testutil.Controller, true)}
	ok := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}
	for i := range t.Gateways {
		var ls []testutil.Listener
		var spec []any
		var gwConds []metav1.Condition
		counts := gwAttached[w.gwNS[i]+"/"+gwName(i)]
		// A conflicted Gateway's l1 shares l0's port and hostname, and kgateway rejects it.
		conflicted := t.IsWorst && i%ConflictEvery == ConflictAt
		if conflicted {
			gwConds = []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "ListenersNotValid"), ok[1]}
		}
		for j, l := range listeners {
			lc, port := ok, int64(8080+j)
			if conflicted && j == 1 {
				lc, port = []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "HostnameConflict"),
					testutil.Cond("Programmed", metav1.ConditionFalse, "HostnameConflict"), testutil.Cond("Conflicted", metav1.ConditionTrue, "HostnameConflict")}, 8080
			}
			ls = append(ls, testutil.Listener{Name: l, Attached: int32(counts[j] + extra), Conds: lc}) //nolint:gosec // small counts
			from := map[string]any{"namespaces": map[string]any{"from": "All"}}
			if l == SelectorListener {
				from = map[string]any{"namespaces": map[string]any{"from": "Selector",
					"selector": map[string]any{"matchLabels": map[string]any{"team": testutil.ScaleTeamLabel["team"]}}}}
			}
			ln := map[string]any{"name": l, "port": port, "protocol": w.protocol(i, j), "allowedRoutes": from}
			if ln["protocol"] == "TLS" {
				ln["tls"] = map[string]any{"mode": "Passthrough"}
			}
			spec = append(spec, ln)
		}
		gw := testutil.Gateway(w.gwNS[i], gwName(i), Class, gwConds, ls...)
		gw.Object["spec"].(map[string]any)["listeners"] = spec
		gw.Object["spec"].(map[string]any)["allowedListeners"] = map[string]any{"namespaces": map[string]any{"from": "Same"}}
		out = append(out, gw)
	}
	for i := range t.ListenerSets {
		g := i % t.Gateways
		n := lsAttached[w.lsNS[i]+"/"+lsName(i)] + extra
		out = append(out, testutil.ListenerSet(w.lsNS[i], lsName(i), testutil.GwRef(w.gwNS[g], gwName(g)), nil,
			testutil.Listener{Name: "extra", Attached: int32(n), Conds: ok})) //nolint:gosec // small counts
	}
	for i := range t.Backends {
		out = append(out, backend(testutil.ScaleNamespace(i%t.Namespaces), beName(i)))
	}
	for _, r := range w.routes {
		out = append(out, w.render(r))
	}
	if t.IsWorst {
		for i := 0; i < t.Namespaces; i += PolicyEvery {
			tp := testutil.TrafficPolicy(testutil.ScaleNamespace(i), GonePolicy)
			tp.Object["spec"] = map[string]any{"targetRefs": []any{map[string]any{"group": objects.GatewayAPIGroup, "kind": "Gateway", "name": GoneGateway}}}
			out = append(out, tp)
		}
	}
	for _, o := range out {
		o.SetResourceVersion("1")
	}
	return out
}

// render builds route r. Under Contradict a child has no entry under its
// delegators and a delegating parent reports ResolvedRefs=False/BackendNotFound,
// so view B's child-entry and parent-resolvedrefs checks contradict too (PF2).
func (w *world) render(r *route) *unstructured.Unstructured {
	var spec []objects.ParentRef
	var status []testutil.RouteParent
	resolved := testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")
	if w.t.Contradict && len(r.children) > 0 {
		resolved = testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")
	}
	var extra []metav1.Condition
	switch r.issue {
	case issueMissing:
		resolved = testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")
		resolved.Message = fmt.Sprintf("Backend %s/%s not found", r.ns, r.backend)
	case issueNotPermitted:
		resolved = testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "RefNotPermitted")
		resolved.Message = fmt.Sprintf("backendRef %s/%s not permitted by any ReferenceGrant", r.backendNS, r.backend)
	case issueReplaced:
		c := testutil.Cond("kgateway.dev/Programmed", metav1.ConditionFalse, "RouteRuleDropped")
		pns := testutil.ScaleNamespace(nsIndex(r.ns) / PolicyEvery * PolicyEvery)
		c.Message = fmt.Sprintf("Dropped Rule (0): %s/TrafficPolicy/%s/%s: invalid", kinds.KgatewayGroup, pns, GonePolicy)
		extra = []metav1.Condition{c}
	}
	for k, p := range r.parents {
		spec = append(spec, p)
		if r.failed[k] != w.t.Contradict {
			status = append(status, testutil.RouteParent{Ref: p, Controller: testutil.Controller, Conds: append([]metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners"), resolved}, extra...)})
			continue
		}
		st := testutil.AcceptedBy(p, testutil.Controller)
		for i := range st.Conds {
			if st.Conds[i].Type == resolved.Type {
				st.Conds[i] = resolved
			}
		}
		st.Conds = append(st.Conds, extra...)
		status = append(status, st)
	}
	for _, d := range r.delegators {
		p := w.routes[d]
		ref := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: p.ns, Name: p.name}
		if r.explicit {
			spec = append(spec, ref)
		}
		if !w.t.Contradict {
			status = append(status, testutil.AcceptedBy(ref, testutil.Controller))
		}
	}
	u := testutil.HTTPRoute(r.ns, r.name, spec, status...)
	if r.issue == issueStale {
		u.SetGeneration(2) // the status observed generation 1
	}
	u.SetAPIVersion(gwAPI)
	u.SetKind(r.kind)
	u.SetUID(types.UID(r.kind + "/" + r.ns + "/" + r.name))
	u.Object["spec"].(map[string]any)["rules"] = w.rules(r)
	if r.kind == "TLSRoute" || r.kind == "GRPCRoute" {
		u.Object["spec"].(map[string]any)["hostnames"] = []any{r.name + ".example.com"}
	}
	return u
}

// pick is a deterministic share of routes: those whose name and salt hash into the lowest pct of 100.
func pick(name, salt string, pct int) bool {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name + salt))
	return int(h.Sum32()%100) < pct
}

// multiRefs is r's backendRefs for one rule: one, or three weighted ones for a
// MultiRefPct share of rules, one in ten with a weight-0 ref. Extra refs name
// other Backends of r's namespace, or other ports for a TCPRoute or TLSRoute.
// Weights are written only where MultiRefPct is set.
func (w *world) multiRefs(r *route, rule int) []any {
	salt := fmt.Sprintf("/ref%d", rule)
	side := r.kind == "TCPRoute" || r.kind == "TLSRoute"
	base := backendRef(r)
	if side {
		base = map[string]any{"name": r.name, "port": int64(443)}
	}
	if w.t.MultiRefPct == 0 {
		return []any{base}
	}
	if !pick(r.name, salt, w.t.MultiRefPct) {
		base["weight"] = int64(1)
		return []any{base}
	}
	ws := []int64{60, 30, 10}
	if pick(r.name, salt+"/zero", 10) {
		ws = []int64{70, 30, 0}
	}
	bes := w.nsBE[nsIndex(r.ns)]
	refs := make([]any, 3)
	for k := range refs {
		ref := maps.Clone(base)
		switch {
		case k > 0 && side:
			ref["port"] = int64(443 + 8000*k)
		case k > 0 && r.issue == 0:
			ref["name"] = bes[(slices.Index(bes, r.backend)+k)%len(bes)]
		}
		ref["weight"] = ws[k]
		refs[k] = ref
	}
	return refs
}

// rules is one delegating rule for a parent, or one rule to a Backend for a
// leaf, with r's path prefixes as matches. A MultiRulePct share of leaves has three rules.
func (w *world) rules(r *route) []any {
	match := func(path string) map[string]any {
		return map[string]any{"path": map[string]any{"type": "PathPrefix", "value": path}}
	}
	nRules := 1
	if r.issue == 0 && len(r.children) == 0 && pick(r.name, "/rules", w.t.MultiRulePct) {
		nRules = 3
	}
	switch r.kind {
	case "TCPRoute", "TLSRoute", "GRPCRoute":
		var rs []any
		for k := range nRules {
			rs = append(rs, map[string]any{"backendRefs": w.multiRefs(r, k)})
		}
		return rs
	}
	var ms []any
	for _, p := range w.pathsOf(r) {
		ms = append(ms, match(p))
	}
	if len(r.children) == 0 {
		var rs []any
		for k := range nRules {
			rms := ms
			if k > 0 { // each extra rule has its own sub-paths
				rms = nil
				for _, p := range w.pathsOf(r) {
					rms = append(rms, match(fmt.Sprintf("%s/rule%d", p, k)))
				}
			}
			rs = append(rs, map[string]any{"matches": rms, "backendRefs": w.multiRefs(r, k)})
		}
		return rs
	}
	var refs []any
	for _, c := range r.children {
		cr := w.routes[c]
		refs = append(refs, map[string]any{"group": objects.GatewayAPIGroup, "kind": "HTTPRoute", "name": cr.name, "namespace": cr.ns})
	}
	if w.t.MultiRefPct > 0 && pick(r.name, "/mixed", 5) {
		bes := w.nsBE[nsIndex(r.ns)]
		refs = append(refs, map[string]any{"group": kinds.KgatewayGroup, "kind": "Backend", "name": bes[0], "namespace": r.ns, "weight": int64(1)})
	}
	return []any{map[string]any{"matches": ms, "backendRefs": refs}}
}

// pathsOf is r's path prefixes. A top-level parent has ParentMatches: /name,
// then /altK/name, so none is a raw prefix of another. A child has one per
// distinct incoming match of its delegators, that match plus its segment, so
// every context attaches and each match fits exactly one incoming match
// (PF2). A lattice layer shares one segment, so its paths do not double per layer.
func (w *world) pathsOf(r *route) []string {
	if ps, ok := w.paths[r]; ok {
		return ps
	}
	var ps []string
	switch {
	case len(r.delegators) > 0:
		seg := cmp.Or(r.segment, r.name)
		for _, d := range r.delegators {
			for _, p := range w.pathsOf(w.routes[d]) {
				if !slices.Contains(ps, p+"/"+seg) {
					ps = append(ps, p+"/"+seg)
				}
			}
		}
	case len(r.children) > 0:
		for k := range w.t.ParentMatches {
			if k == 0 {
				ps = append(ps, "/"+r.name)
			} else {
				ps = append(ps, fmt.Sprintf("/alt%d/%s", k, r.name))
			}
		}
	default:
		ps = []string{"/" + r.name}
	}
	w.paths[r] = ps
	return ps
}

func backendRef(r *route) map[string]any {
	return map[string]any{"group": kinds.KgatewayGroup, "kind": "Backend", "name": r.backend, "namespace": cmp.Or(r.backendNS, r.ns)}
}

// backend builds a static kgateway Backend.
func backend(ns, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{"apiVersion": backendAPI, "kind": "Backend",
		"spec": map[string]any{"type": "Static", "static": map[string]any{"hosts": []any{map[string]any{"host": name + ".example.com", "port": int64(8080)}}}},
		"status": map[string]any{"conditions": []any{map[string]any{"type": "Accepted", "status": "True", "reason": "Accepted",
			"observedGeneration": int64(1), "lastTransitionTime": testutil.Old.UTC().Format(time.RFC3339)}}}}}
	u.SetNamespace(ns)
	u.SetName(name)
	u.SetGeneration(1)
	u.SetUID(types.UID("Backend/" + ns + "/" + name))
	u.SetCreationTimestamp(testutil.Old)
	return u
}

// listenerName names listener j of n: l0 … and SelectorListener last.
func listenerName(j, n int) string {
	if j == n-1 {
		return SelectorListener
	}
	return fmt.Sprintf("l%d", j)
}

func gwIndex(name string) int {
	var i int
	_, _ = fmt.Sscanf(name, "gw-%d", &i)
	return i
}

func nsIndex(ns string) int {
	var i int
	_, _ = fmt.Sscanf(ns, "ns-%d", &i)
	return i
}

// Snapshot is a synced snapshot of tier t's world with every kind
// readable, one kgateway v2.4.7 controller in namespace kgw with a ready Pod,
// and a ready proxy per Gateway.
func Snapshot(t Tier, seed int64) *cache.Snapshot {
	byKind := map[schema.GroupKind]schema.GroupVersionResource{}
	access := map[schema.GroupVersionResource]cache.Access{}
	for _, s := range testutil.ServedUpstream() {
		byKind[schema.GroupKind{Group: s.Group, Kind: s.Kind}] = s.GVR()
		access[s.GVR()] = cache.Access{OK: true}
	}
	objs := map[schema.GroupVersionResource][]*unstructured.Unstructured{}
	for _, o := range World(t, seed) {
		gvr, ok := byKind[o.GroupVersionKind().GroupKind()]
		if !ok {
			panic("scale: unserved kind " + o.GetKind())
		}
		objs[gvr] = append(objs[gvr], o)
	}
	for _, g := range []schema.GroupVersionResource{cache.NamespacesGVR, cache.PodsGVR, cache.DeploymentsGVR} {
		access[g] = cache.Access{OK: true}
	}
	s := &cache.Snapshot{Objects: objs, Access: access, Synced: true, TakenAt: testutil.Now}
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	s.Namespaces = append(testutil.ScaleNamespaces(t.Namespaces), testutil.Namespace("kgw", nil))
	s.Deployments = append(s.Deployments, d)
	s.Pods = append(s.Pods, testutil.ControllerPod(d, "kgateway-0", true))
	for i := range t.Gateways {
		s.Deployments = append(s.Deployments, testutil.ProxyDeployment(testutil.ScaleNamespace(i%t.Namespaces), gwName(i), 1, 1))
	}
	return s
}

// MaxRouteParentStatuses is the Gateway API MaxItems of a route's status.parents.
const MaxRouteParentStatuses = 32

// DenseNamespace holds DenseSelection's selected routes; DenseParents its parents and Gateway.
var (
	DenseNamespace = testutil.ScaleNamespace(1)
	DenseParents   = testutil.ScaleNamespace(0)
)

// DenseSelection is the dense-selection shape of design §9.6: parents
// HTTPRoutes on one Gateway, each with one wildcard ref to DenseNamespace,
// whose routes HTTPRoutes have no parentRefs and match under every parent's
// /dense, each to one Backend. So every parent selects, permits and attaches every route. Status
// is what the controller writes: each parent Accepted on the Gateway, each
// route Accepted under the first MaxRouteParentStatuses parents, as the API
// server admits no more. Namespaces come from
// testutil.ScaleNamespaces(2).
func DenseSelection(parents, routes int) []*unstructured.Unstructured {
	ok := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}
	gw := testutil.Gateway(DenseParents, "dense", Class, nil, testutil.Listener{Name: "http", Attached: int32(parents), Conds: ok}) //nolint:gosec // small counts
	gw.Object["spec"].(map[string]any)["listeners"] = []any{map[string]any{"name": "http", "port": int64(8080), "protocol": "HTTP",
		"allowedRoutes": map[string]any{"namespaces": map[string]any{"from": "All"}}}}
	out := []*unstructured.Unstructured{testutil.GatewayClass(Class, testutil.Controller, true), gw, backend(DenseNamespace, "be")}
	gwRef := testutil.GwRef(DenseParents, "dense")
	match := func(path string) []any {
		return []any{map[string]any{"path": map[string]any{"type": "PathPrefix", "value": path}}}
	}
	var under []testutil.RouteParent // each route's entries, one per parent
	for i := range parents {
		name := fmt.Sprintf("p-%03d", i)
		u := testutil.HTTPRoute(DenseParents, name, []objects.ParentRef{gwRef}, testutil.AcceptedBy(gwRef, testutil.Controller))
		u.Object["spec"].(map[string]any)["rules"] = []any{map[string]any{"matches": match("/dense"),
			"backendRefs": []any{map[string]any{"group": objects.GatewayAPIGroup, "kind": "HTTPRoute", "name": "*", "namespace": DenseNamespace}}}}
		out = append(out, u)
		under = append(under, testutil.AcceptedBy(testutil.HTTPRouteRef(DenseParents, name), testutil.Controller))
	}
	for i := range routes {
		name := fmt.Sprintf("r-%05d", i)
		u := testutil.HTTPRoute(DenseNamespace, name, nil, under[:min(len(under), MaxRouteParentStatuses)]...)
		u.Object["spec"].(map[string]any)["rules"] = []any{map[string]any{"matches": match("/dense/" + name),
			"backendRefs": []any{map[string]any{"group": kinds.KgatewayGroup, "kind": "Backend", "name": "be", "namespace": DenseNamespace}}}}
		out = append(out, u)
	}
	for _, o := range out {
		o.SetResourceVersion("1")
		o.SetUID(types.UID(o.GetKind() + "/" + o.GetNamespace() + "/" + o.GetName()))
	}
	return out
}
