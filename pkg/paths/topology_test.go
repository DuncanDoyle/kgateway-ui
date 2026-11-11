package paths_test

import (
	"slices"
	"testing"
	"time"
	"unsafe"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/internal/testutil/scale"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
	"github.com/DuncanDoyle/snellius/pkg/scopeverify"
)

type staticSnap struct{ s *cache.Snapshot }

func (s staticSnap) Snapshot() *cache.Snapshot { return s.s }

// build runs the engine over snap and builds the topology of its only install.
func build(t testing.TB, snap *cache.Snapshot) *paths.Topology {
	t.Helper()
	top, _ := buildWith(t, snap)
	return top
}

// buildWith is build that also returns the install's model.
func buildWith(t testing.TB, snap *cache.Snapshot) (*paths.Topology, *engine.InstallModel) {
	t.Helper()
	e := engine.New(engine.Config{Snapshots: staticSnap{snap}, Served: testutil.ServedUpstream(), Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour), Checks: health.Upstream(),
		Now: func() time.Time { return testutil.Now }})
	m := e.Compute()
	if len(m.Installs) != 1 {
		t.Fatalf("installs: %d", len(m.Installs))
	}
	i := &m.Installs[0]
	return paths.Build(paths.Input{Index: i.Index, Views: i.Computed, Decode: objects.Direct{},
		Delegates: i.Rules.Delegator() != nil}), i
}

// world builds a topology over one kgateway install with experimental
// features on, the GatewayClass and objs.
func world(t *testing.T, objs ...*unstructured.Unstructured) *paths.Topology {
	t.Helper()
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1,
		testutil.Env(install.ExperimentalEnv, "true"))
	all := append([]*unstructured.Unstructured{testutil.GatewayClass("kgateway", testutil.Controller, true)}, objs...)
	access := map[schema.GroupVersionResource]cache.Access{}
	for _, k := range testutil.ServedUpstream() {
		access[k.GVR()] = cache.Access{OK: true}
	}
	return build(t, &cache.Snapshot{Objects: testutil.ObjectsByGVR(t, all...), Access: access,
		Namespaces:  []*corev1.Namespace{testutil.Namespace("kgw", nil), testutil.Namespace("infra", nil), testutil.Namespace("shop", nil)},
		Deployments: []*appsv1.Deployment{d, testutil.ProxyDeployment("infra", "edge", 1, 1)}, Pods: []*corev1.Pod{testutil.ControllerPod(d, "p", true)},
		Synced: true, TakenAt: testutil.Now})
}

var ready = []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}

// edge is infra/edge with HTTP listeners named names on port 8080.
func edge(names ...string) *unstructured.Unstructured {
	ls := make([]testutil.Listener, 0, len(names))
	for _, n := range names {
		ls = append(ls, testutil.Listener{Name: n, Conds: ready})
	}
	return testutil.Gateway("infra", "edge", "kgateway", nil, ls...)
}

// svc is a backendRef to Service shop/name on port 8080.
func svc(name string) map[string]any { return map[string]any{"name": name, "port": int64(8080)} }

// rule is an HTTPRoute rule with backendRefs refs and filters fs.
func rule(refs []any, fs ...any) map[string]any {
	r := map[string]any{}
	if refs != nil {
		r["backendRefs"] = refs
	}
	if fs != nil {
		r["filters"] = fs
	}
	return r
}

func ref(ns, name string) objects.Ref { return objects.Ref{Kind: "Service", Namespace: ns, Name: name} }

// TestRowsPerAttachmentRuleRef: 2 attachments × 2 rules with {2, 0} refs are 6
// rows; the rule with no backendRef is one row with its written action.
func TestRowsPerAttachmentRuleRef(t *testing.T) {
	route := testutil.WithRules(testutil.HTTPRoute("shop", "r", []objects.ParentRef{testutil.GwRef("infra", "edge")}),
		rule([]any{svc("a"), svc("b")}),
		rule(nil, map[string]any{"type": "RequestRedirect", "requestRedirect": map[string]any{"scheme": "https"}}))
	top := world(t, edge("one", "two"), route)
	if len(top.Rows) != 6 {
		t.Fatalf("rows: %d", len(top.Rows))
	}
	type key struct {
		listener string
		rule     int
		backend  string
		action   paths.Action
	}
	var got []key
	for _, r := range top.Rows {
		k := key{listener: top.Listener(r).Name, rule: top.Rule(r).Index, action: top.Action(r)}
		if b, ok := top.Backend(r); ok {
			k.backend = b.Ref.Name
		}
		got = append(got, k)
	}
	want := []key{{"one", 0, "a", paths.ActionBackend}, {"one", 0, "b", paths.ActionBackend}, {"one", 1, "", paths.ActionRedirect},
		{"two", 0, "a", paths.ActionBackend}, {"two", 0, "b", paths.ActionBackend}, {"two", 1, "", paths.ActionRedirect}}
	if !slices.Equal(got, want) {
		t.Fatalf("rows:\n got %v\nwant %v", got, want)
	}
	if l := top.Listener(top.Rows[0]); l.Protocol != "HTTP" || l.Port != 8080 {
		t.Fatalf("listener: %+v", l)
	}
}

// TestWrittenAction: a rule with no backendRef shows its action as written:
// redirect, direct response through an extensionRef, else none.
func TestWrittenAction(t *testing.T) {
	route := testutil.WithRules(testutil.HTTPRoute("shop", "r", []objects.ParentRef{testutil.GwRef("infra", "edge")}),
		rule(nil, map[string]any{"type": "ExtensionRef", "extensionRef": map[string]any{"group": "gateway.kgateway.dev", "kind": "DirectResponse", "name": "dr"}}),
		rule(nil, map[string]any{"type": "ExtensionRef", "extensionRef": map[string]any{"group": "gateway.kgateway.dev", "kind": "TrafficPolicy", "name": "tp"}}),
		rule(nil))
	top := world(t, edge("http"), route)
	var got []paths.Action
	for _, r := range top.Rows {
		got = append(got, top.Action(r))
	}
	if want := []paths.Action{paths.ActionDirectResponse, paths.ActionNone, paths.ActionNone}; !slices.Equal(got, want) {
		t.Fatalf("actions: %v, want %v", got, want)
	}
}

// TestListenerSetProjects: a ListenerSet attachment shows the ListenerSet and its listener, and projects to the parent Gateway.
func TestListenerSetProjects(t *testing.T) {
	lsRef := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	ls := testutil.ListenerSet("infra", "ls", testutil.GwRef("infra", "edge"), nil)
	// A ListenerSet listener admits routes from its own namespace.
	route := testutil.WithRules(testutil.HTTPRoute("infra", "r", []objects.ParentRef{lsRef}), rule([]any{svc("a")}))
	top := world(t, edge("http"), ls, route)
	if len(top.Rows) != 1 {
		t.Fatalf("rows: %d", len(top.Rows))
	}
	r := top.Rows[0]
	gw := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}
	set := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	if top.Gateway(r) != gw || top.ListenerSet(r) == nil || *top.ListenerSet(r) != set {
		t.Fatalf("gateway %v, listener set %v", top.Gateway(r), top.ListenerSet(r))
	}
	if l := top.Listener(r); l.Name != "extra" || l.Port != 8081 {
		t.Fatalf("listener: %+v", l)
	}
	deps := depRefs(top, r)
	if !slices.Contains(deps, gw) || !slices.Contains(deps, set) {
		t.Fatalf("deps: %v", deps)
	}
}

// TestMirrorRow: a RequestMirror is its own row with the mirror and its percent, not a backend.
func TestMirrorRow(t *testing.T) {
	mirror := map[string]any{"type": "RequestMirror", "requestMirror": map[string]any{"backendRef": map[string]any{"name": "shadow", "port": int64(9090)}, "percent": int64(10)}}
	route := testutil.WithRules(testutil.HTTPRoute("shop", "r", []objects.ParentRef{testutil.GwRef("infra", "edge")}),
		rule([]any{svc("a")}, mirror))
	top := world(t, edge("http"), route)
	if len(top.Rows) != 2 {
		t.Fatalf("rows: %d", len(top.Rows))
	}
	main, m := top.Rows[0], top.Rows[1]
	if b, ok := top.Backend(main); !ok || b.Ref != ref("shop", "a") {
		t.Fatalf("backend row: %+v", b)
	}
	if _, ok := top.Mirror(main); ok {
		t.Fatal("backend row is a mirror")
	}
	mf, ok := top.Mirror(m)
	if !ok || mf.Backend != ref("shop", "shadow") || mf.Port != 9090 || mf.Percent == nil || *mf.Percent != 10 {
		t.Fatalf("mirror: %+v", mf)
	}
	if _, ok := top.Backend(m); ok || top.Action(m) != paths.ActionBackend {
		t.Fatal("mirror row has a backend or a written action")
	}
	if !slices.Contains(depRefs(top, m), ref("shop", "shadow")) {
		t.Fatalf("mirror deps: %v", depRefs(top, m))
	}
}

// TestHostnamesAsList: an attachment with several effective hostnames is one row carrying them all.
func TestHostnamesAsList(t *testing.T) {
	route := testutil.WithHostnames(testutil.WithRules(testutil.HTTPRoute("shop", "r", []objects.ParentRef{testutil.GwRef("infra", "edge")}),
		rule([]any{svc("a")})), "a.example.com", "b.example.com")
	top := world(t, edge("http"), route)
	if len(top.Rows) != 1 {
		t.Fatalf("rows: %d", len(top.Rows))
	}
	if h := top.Hostnames(top.Rows[0]); !slices.Equal(h, []string{"a.example.com", "b.example.com"}) {
		t.Fatalf("hostnames: %v", h)
	}
}

// kindRoute is r as a route of kind at apiVersion.
func kindRoute(r *unstructured.Unstructured, apiVersion, kind string) *unstructured.Unstructured {
	r.SetAPIVersion(apiVersion)
	r.SetKind(kind)
	r.SetUID(types.UID("uid-" + kind))
	return r
}

// TestAllRouteKinds: GRPCRoute, TCPRoute and TLSRoute rows carry their
// backendRef with port and weight, on a listener of their protocol.
func TestAllRouteKinds(t *testing.T) {
	gw := testutil.GatewayWithListeners("infra", "edge", "kgateway", []objects.Listener{
		{Name: "grpc", Port: 80, Protocol: "HTTP"}, {Name: "tcp", Port: 5432, Protocol: "TCP"},
		{Name: "tls", Port: 443, Protocol: "TLS", TLS: &objects.ListenerTLS{Mode: "Passthrough"}},
	}, testutil.Listener{Name: "grpc", Conds: ready}, testutil.Listener{Name: "tcp", Conds: ready}, testutil.Listener{Name: "tls", Conds: ready})
	to := func(section string) []objects.ParentRef {
		p := testutil.GwRef("infra", "edge")
		p.SectionName = section
		return []objects.ParentRef{p}
	}
	weighted := func(name string, port, weight int64) map[string]any {
		return map[string]any{"name": name, "port": port, "weight": weight}
	}
	routes := []*unstructured.Unstructured{
		kindRoute(testutil.WithRules(testutil.HTTPRoute("infra", "g", to("grpc")), rule([]any{weighted("g1", 50051, 3), weighted("g2", 50052, 1)})),
			"gateway.networking.k8s.io/v1", "GRPCRoute"),
		kindRoute(testutil.WithRules(testutil.HTTPRoute("infra", "t", to("tcp")), rule([]any{weighted("db", 5432, 2)})),
			"gateway.networking.k8s.io/v1alpha2", "TCPRoute"),
		kindRoute(testutil.WithRules(testutil.HTTPRoute("infra", "s", to("tls")), rule([]any{weighted("tls", 8443, 5)})),
			"gateway.networking.k8s.io/v1alpha2", "TLSRoute"),
	}
	top := world(t, append([]*unstructured.Unstructured{gw}, routes...)...)
	type key struct {
		kind, listener, backend string
		port                    int32
		listenerPort, weight    int32
	}
	var got []key
	for _, r := range top.Rows {
		b, ok := top.Backend(r)
		if !ok {
			t.Fatalf("row without backend: %+v", r)
		}
		got = append(got, key{top.Chain(r)[0].Kind, top.Listener(r).Name, b.Ref.Name, b.Port, top.Listener(r).Port, b.Weight})
	}
	slices.SortFunc(got, func(a, b key) int {
		if a.kind != b.kind {
			if a.kind < b.kind {
				return -1
			}
			return 1
		}
		if a.backend < b.backend {
			return -1
		}
		return 1
	})
	want := []key{{"GRPCRoute", "grpc", "g1", 50051, 80, 3}, {"GRPCRoute", "grpc", "g2", 50052, 80, 1},
		{"TCPRoute", "tcp", "db", 5432, 5432, 2}, {"TLSRoute", "tls", "tls", 8443, 443, 5}}
	if !slices.Equal(got, want) {
		t.Fatalf("rows:\n got %v\nwant %v", got, want)
	}
}

// TestRowSize: a row stays within the memo's 48-byte budget (design §4.3).
func TestRowSize(t *testing.T) {
	if n := unsafe.Sizeof(paths.Row{}); n > 48 {
		t.Fatalf("Row is %d bytes", n)
	}
}

func depRefs(top *paths.Topology, r paths.Row) []objects.Ref {
	var out []objects.Ref
	for _, i := range top.Deps(r) {
		out = append(out, top.Refs[i])
	}
	return out
}

// TestDepsCoverChain: a row's deps hold its Gateway, ListenerSet, every route
// of its chain and its backend; a delegation row's backend is the delegation.
func TestDepsCoverChain(t *testing.T) {
	lsRef := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	ls := testutil.ListenerSet("infra", "ls", testutil.GwRef("infra", "edge"), nil)
	route := testutil.WithRules(testutil.HTTPRoute("infra", "r", []objects.ParentRef{lsRef}),
		rule([]any{svc("a"), testutil.DelegateTo("infra", "child")}))
	child := testutil.WithRules(testutil.HTTPRoute("infra", "child", nil), rule([]any{svc("c")}))
	top := world(t, edge("http"), ls, route, child)
	if len(top.Rows) != 2 {
		t.Fatalf("rows: %d", len(top.Rows))
	}
	gw := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}
	set := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	root := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "infra", Name: "r"}
	plain, deleg := top.Rows[0], top.Rows[1]
	if chain := top.Chain(plain); !slices.Equal(chain, []objects.Ref{root}) {
		t.Fatalf("chain: %v", chain)
	}
	for _, r := range top.Rows {
		deps := depRefs(top, r)
		for _, want := range append(top.Chain(r), gw, set) {
			if !slices.Contains(deps, want) {
				t.Fatalf("deps %v lack %v", deps, want)
			}
		}
	}
	if !slices.Contains(depRefs(top, plain), ref("infra", "a")) {
		t.Fatalf("deps lack the backend: %v", depRefs(top, plain))
	}
	b, ok := top.Backend(deleg)
	if !ok || b.Delegation == nil || b.Delegation.Name != "child" {
		t.Fatalf("delegation row: %+v", b)
	}
	if child := (objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "infra", Name: "child"}); !slices.Contains(depRefs(top, deleg), child) {
		t.Fatalf("delegation deps lack the named child: %v", depRefs(top, deleg))
	}
}

// TestPathsWorldRows builds the topology over the large-paths world: one row
// per view A attachment, rule and ref, with each route's rules decoded here.
// Delegated children's rows come from view C and are not counted.
func TestPathsWorldRows(t *testing.T) {
	tier := scale.Large.Paths()
	per := map[objects.Ref]int{}
	for _, o := range scale.World(tier, 1) {
		var rules []objects.Rule
		switch o.GetKind() {
		case "HTTPRoute":
			rules = objects.DecodeHTTPRoute(o).Rules
		case "GRPCRoute", "TCPRoute", "TLSRoute":
			rules = objects.DecodeRouteRules(o)
		default:
			continue
		}
		n := 0
		for _, r := range rules {
			n += max(1, len(r.BackendRefs))
		}
		per[objects.RefOf(o)] = n
	}
	top, i := buildWith(t, scale.Snapshot(tier, 1))
	want := 0
	for _, m := range i.Computed.Attachment.Members {
		want += per[m.Route]
	}
	if len(top.Rows) != want || want == 0 {
		t.Fatalf("rows %d, want %d", len(top.Rows), want)
	}
	t.Logf("%s: %d rows over %d attachments, %d refs, %d bytes", tier.Name, len(top.Rows), len(i.Computed.Attachment.Members), len(top.Refs), top.Bytes())
}
