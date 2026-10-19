package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/overview"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

const img = "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7"

type fakeMetrics map[string]ctrlmetrics.Result

func (f fakeMetrics) Get(id string) (ctrlmetrics.Result, bool) { r, ok := f[id]; return r, ok }

// okMetrics are fresh, reject-free metrics. ManagedPresent lets the scope
// verifier run; with the default 1 h persistence no mismatch is reported.
func okMetrics(ids ...string) fakeMetrics {
	f := fakeMetrics{}
	for _, id := range ids {
		f[id] = ctrlmetrics.Result{CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{}, Managed: map[ctrlmetrics.ManagedKey]float64{}, ManagedPresent: true}}
	}
	return f
}

// setup configures startWith. Zero values are the defaults.
type setup struct {
	served    []kinds.Served     // nil: every upstream kind
	pinned    string             // pinned install ID
	metrics   ctrlmetrics.Source // nil: okMetrics("kgw/kgateway")
	immediate bool               // report scope mismatches on the second computation (persistence 0, not 1 h)
	authz     Authorizer         // nil: AllowAll
	// prepare runs on the fake clients before the cache starts (failure injection).
	prepare func(dyn *dynamicfake.FakeDynamicClient, kube *kubefake.Clientset)
	// late objects are created after the engine has first seen the rest, so their
	// generation is first seen at testutil.Now: inside the grace period.
	late []*unstructured.Unstructured
}

// harness is a running cache, engine and handler over fake clients, with the
// clients exposed so a test can inject failures after the initial sync.
type harness struct {
	ctx   context.Context
	cache *cache.Cache
	e     *engine.Engine
	srv   *httptest.Server
	dyn   *dynamicfake.FakeDynamicClient
	kube  *kubefake.Clientset
}

// clock is the engine clock of a harness.
type clock struct{ t atomic.Pointer[time.Time] }

func (c *clock) set(t time.Time) { c.t.Store(&t) }
func (c *clock) now() time.Time  { return *c.t.Load() }

// newHarness runs the real cache, engine and handler over fake clients seeded
// with objs. The engine first computes at testutil.Now minus Grace and 1 s, so
// the seeded objects' generations are past the grace period at testutil.Now,
// the time every API response is computed at (D-006: unprocessed means warning).
func newHarness(t *testing.T, s setup, objs ...runtime.Object) *harness {
	t.Helper()
	if s.served == nil {
		s.served = testutil.ServedUpstream()
	}
	if s.metrics == nil {
		s.metrics = okMetrics("kgw/kgateway")
	}
	persist := time.Hour
	if s.immediate {
		persist = 0
	}
	dyn, kube := testutil.Clients(t, objs...)
	fd := dyn.(*dynamicfake.FakeDynamicClient)
	if s.prepare != nil {
		s.prepare(fd, kube)
	}
	det := install.UpstreamDetector{}
	c := cache.New(cache.Config{Dynamic: dyn, Kube: kube, Served: s.served,
		PodSelectors: []labels.Selector{det.ControllerPodSelector()}, DeploymentSelectors: []labels.Selector{det.ControllerSelector(), det.ProxySelector()}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Start(ctx)
	clk := &clock{}
	clk.set(testutil.Now.Add(-health.Grace - time.Second))
	e := engine.New(engine.Config{Snapshots: c, Served: s.served, Detectors: []install.Detector{det}, Scope: install.NamespaceSelectorScope{},
		Verifier: scopeverify.NewManagedCountVerifier(persist), Metrics: s.metrics, Checks: health.Upstream(), Pinned: s.pinned,
		MetricsMaxAge: 30 * time.Second, Now: clk.now})
	deadline := time.Now().Add(5 * time.Second)
	for !e.Compute().Snapshot.Synced {
		if time.Now().After(deadline) {
			t.Fatal("not synced")
		}
		time.Sleep(20 * time.Millisecond)
	}
	clk.set(testutil.Now)
	for _, u := range s.late {
		createAndWait(ctx, t, fd, c, s.served, u)
	}
	e.Compute()
	srv := httptest.NewServer(NewHandler(e, fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}}, s.authz))
	t.Cleanup(srv.Close)
	return &harness{ctx: ctx, cache: c, e: e, srv: srv, dyn: fd, kube: kube}
}

// createAndWait creates u through the fake dynamic client and waits until the cache holds it.
func createAndWait(ctx context.Context, t *testing.T, dyn *dynamicfake.FakeDynamicClient, c *cache.Cache, served []kinds.Served, u *unstructured.Unstructured) {
	t.Helper()
	gv, _ := schema.ParseGroupVersion(u.GetAPIVersion())
	for _, k := range served {
		if k.Group != gv.Group || k.Kind != u.GetKind() {
			continue
		}
		if _, err := dyn.Resource(k.GVR()).Namespace(u.GetNamespace()).Create(ctx, u, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			for _, o := range c.Snapshot().Objects[k.GVR()] {
				if o.GetNamespace() == u.GetNamespace() && o.GetName() == u.GetName() {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s %s/%s never reached the cache", u.GetKind(), u.GetNamespace(), u.GetName())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	t.Fatalf("kind %s is not served", u.GetKind())
}

// startWith is newHarness for tests that only need the engine, the server and namespace updates.
func startWith(t *testing.T, s setup, objs ...runtime.Object) (*engine.Engine, *httptest.Server, func(*corev1.Namespace)) {
	t.Helper()
	h := newHarness(t, s, objs...)
	update := func(n *corev1.Namespace) {
		if _, err := h.kube.CoreV1().Namespaces().Update(h.ctx, n, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	return h.e, h.srv, update
}

// start is startWith with the common settings.
func start(t *testing.T, served []kinds.Served, pinned string, m ctrlmetrics.Source, objs ...runtime.Object) (*engine.Engine, *httptest.Server, func(*corev1.Namespace)) {
	t.Helper()
	return startWith(t, setup{served: served, pinned: pinned, metrics: m}, objs...)
}

func get[T any](t *testing.T, srv *httptest.Server, path string, wantStatus int) T {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s: status %d, want %d", path, resp.StatusCode, wantStatus)
	}
	var v T
	if wantStatus == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
			t.Fatal(err)
		}
	}
	return v
}

// world: install kgw/kgateway scoped to team=a; infra and shop are watched, blog is not.
func world(extra ...runtime.Object) []runtime.Object { return worldWith(1, 1, extra...) }

// worldWith is world with the number of ready controller and proxy replicas (of 1).
func worldWith(ctrlReady, proxyReady int32, extra ...runtime.Object) []runtime.Object {
	d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, ctrlReady, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"a"}}]`))
	gw := testutil.GwRef("infra", "edge")
	return append([]runtime.Object{
		testutil.Namespace("kgw", nil), testutil.Namespace("infra", map[string]string{"team": "a"}),
		testutil.Namespace("shop", map[string]string{"team": "a"}), testutil.Namespace("blog", nil),
		d, testutil.ControllerPod(d, "kgateway-0", ctrlReady > 0), testutil.ProxyDeployment("infra", "edge", 1, proxyReady),
		testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.GatewayClass("other", "example.com/other", true),
		testutil.Gateway("infra", "edge", "kgateway", nil), testutil.Gateway("infra", "foreign", "other", nil),
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.HTTPRoute("blog", "posts", []objects.ParentRef{gw}),
	}, extra...)
}

// stale bumps u's generation past its fixture conditions (observed generation 1).
func stale(u *unstructured.Unstructured) *unstructured.Unstructured {
	u.SetGeneration(2)
	return u
}

func count(o OverviewResponse, kind string) (int, bool) {
	for _, c := range o.Counts {
		if c.Kind == kind {
			return c.Count, true
		}
	}
	return 0, false
}

func hasFinding(h HealthResponse, code, res string) bool {
	for _, f := range h.Findings {
		if f.Code == code && (res == "" || (f.Resource != nil && f.Resource.String() == res)) {
			return true
		}
	}
	return false
}

func TestOverviewCountsOnlyWhatTheInstallManages(t *testing.T) {
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw/kgateway"), world()...)
	insts := get[[]InstallSummary](t, srv, "/api/v1/installs", 200)
	if len(insts) != 1 || insts[0].ID != "kgw/kgateway" || insts[0].Version != "2.4.7" {
		t.Fatalf("installs: %+v", insts)
	}
	if insts[0].Controller != (overview.ReadyCount{Ready: 1, Desired: 1}) {
		t.Errorf("controller readiness: %+v", insts[0].Controller)
	}
	if !insts[0].ComputedAt.Equal(testutil.Now) {
		t.Errorf("installs must carry the model's computation time, got %s", insts[0].ComputedAt)
	}
	o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw/kgateway/overview", 200)
	if o.Header.ID != "kgw/kgateway" {
		t.Errorf("overview must carry the install id, got %q", o.Header.ID)
	}
	if n, ok := count(o, "GatewayClass"); !ok || n != 1 {
		t.Errorf("GatewayClasses: want a tile counting 1 (class 'other' excluded), got %d (tile %v)", n, ok)
	}
	if n, _ := count(o, "Gateway"); n != 1 {
		t.Errorf("Gateways: want 1 (foreign class excluded), got %d", n)
	}
	if n, _ := count(o, "HTTPRoute"); n != 1 {
		t.Errorf("HTTPRoutes: want 1 (blog/posts is out of scope), got %d", n)
	}
	if o.Header.Scope.Watched != 2 || o.Header.Scope.Total != 4 {
		t.Errorf("scope watched/total: %+v", o.Header.Scope)
	}
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw/kgateway/health", 200)
	if !hasFinding(h, health.CodeNamespaceNotWatched, "blog/posts") || h.State != health.StateWarning || h.Install != "kgw/kgateway" {
		t.Fatalf("health: %+v", h)
	}
}

func TestMetricsUnreachableIsNeverHealthy(t *testing.T) {
	m := fakeMetrics{"kgw/kgateway": {CollectedAt: testutil.Now, Err: "dial tcp: connection refused"}}
	_, srv, _ := start(t, testutil.ServedUpstream(), "", m, world()[:len(world())-1]...) // without blog/posts
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw/kgateway/health", 200)
	if h.State != health.StateUnknown || !hasFinding(h, health.CodeSignalUnavailable, "") {
		t.Fatalf("want unknown with signal-unavailable, got %s %+v", h.State, h.Findings)
	}
}

func TestUnservedKindHasNoTileAndNoFinding(t *testing.T) {
	var served []kinds.Served
	for _, s := range testutil.ServedUpstream() {
		if s.Kind != "TCPRoute" {
			served = append(served, s)
		}
	}
	// Without status this route would be KGWUI-003 if TCPRoute were served.
	tcp := testutil.HTTPRoute("shop", "db", []objects.ParentRef{testutil.GwRef("infra", "edge")})
	tcp.SetKind("TCPRoute")
	_, srv, _ := start(t, served, "", okMetrics("kgw/kgateway"), world(tcp)...)
	o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw/kgateway/overview", 200)
	if _, ok := count(o, "TCPRoute"); ok {
		t.Fatal("an unserved kind must not have a tile")
	}
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw/kgateway/health", 200)
	for _, f := range h.Findings {
		if f.Resource != nil && f.Resource.Kind == "TCPRoute" {
			t.Fatalf("an unserved kind must not produce a finding: %+v", f)
		}
	}
	get[map[string]any](t, srv, "/api/v1/installs/kgw/kgateway/resources/gateway.networking.k8s.io/TCPRoute/shop/db", 404)
}

func TestOverlappingInstallsAndPinning(t *testing.T) {
	d2 := testutil.ControllerDeployment("kgw2", "kgateway2", img, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, "[]"))
	objs := world(testutil.Namespace("kgw2", nil), d2, testutil.ControllerPod(d2, "kgateway2-0", true))
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw/kgateway", "kgw2/kgateway2"), objs...)
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw2/kgateway2/health", 200)
	if !hasFinding(h, health.CodeAmbiguousOwnership, "infra/edge") {
		t.Fatalf("overlapping scopes must report ambiguous ownership: %+v", h.Findings)
	}
	_, pinned, _ := start(t, testutil.ServedUpstream(), "kgw/kgateway", okMetrics("kgw/kgateway", "kgw2/kgateway2"), objs...)
	if insts := get[[]InstallSummary](t, pinned, "/api/v1/installs", 200); len(insts) != 1 || insts[0].ID != "kgw/kgateway" {
		t.Fatalf("pinned: %+v", insts)
	}
	get[HealthResponse](t, pinned, "/api/v1/installs/kgw2/kgateway2/health", 404)
}

func TestNamespaceLabelChangeUpdatesScopeWithoutRestart(t *testing.T) {
	e, srv, update := start(t, testutil.ServedUpstream(), "", okMetrics("kgw/kgateway"), world()...)
	update(testutil.Namespace("blog", map[string]string{"team": "a"}))
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.Compute()
		o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw/kgateway/overview", 200)
		if n, _ := count(o, "HTTPRoute"); n == 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("blog/posts did not come into scope after labelling the namespace")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Owned objects and objects referenced by a current finding are
// served (only managedFields stripped); anything else is 404.
func TestResourceEndpoint(t *testing.T) {
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw/kgateway"), world()...)
	const base = "/api/v1/installs/kgw/kgateway/resources/"
	r := get[map[string]any](t, srv, base+"gateway.networking.k8s.io/HTTPRoute/shop/cart", 200)
	if md, _ := r["metadata"].(map[string]any); md["managedFields"] != nil || md["name"] != "cart" {
		t.Fatalf("unexpected object: %+v", r["metadata"])
	}
	get[map[string]any](t, srv, base+"gateway.networking.k8s.io/HTTPRoute/blog/posts", 200)  // unwatched, referenced by KGWUI-004
	get[map[string]any](t, srv, base+"gateway.networking.k8s.io/Gateway/infra/foreign", 404) // another controller's Gateway
	get[map[string]any](t, srv, base+"-/Secret/shop/x", 404)                                 // never served
	get[map[string]any](t, srv, base+"apps/Deployment/kgw/kgateway", 404)                    // ready controller: no finding references it
	get[map[string]any](t, srv, base+"gateway.networking.k8s.io/GatewayClass/-/kgateway", 200)
	meta := get[MetaResponse](t, srv, "/api/v1/meta", 200)
	found := false
	for _, k := range meta.Kinds {
		found = found || (k.Group == "apps" && k.Kind == "Deployment")
	}
	if !found {
		t.Fatal("/api/v1/meta must list apps/Deployment, which finding links can reach")
	}
}

// The controller Deployment referenced by KGWUI-005 is served.
func TestResourceEndpointServesReferencedDeployment(t *testing.T) {
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw/kgateway"), worldWith(0, 1)...)
	r := get[map[string]any](t, srv, "/api/v1/installs/kgw/kgateway/resources/apps/Deployment/kgw/kgateway", 200)
	md, _ := r["metadata"].(map[string]any)
	if r["kind"] != "Deployment" || r["apiVersion"] != "apps/v1" || md["name"] != "kgateway" || md["managedFields"] != nil {
		t.Fatalf("unexpected object: kind=%v apiVersion=%v metadata=%+v", r["kind"], r["apiVersion"], md)
	}
}

// Every finding code at the API boundary, once where it must appear and once
// where it must not (design §9), including stale True, absent conditions,
// policies without ancestors, stale metrics and routes to a missing Gateway.
func TestEveryFindingCodeAtTheAPI(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	rejects := fakeMetrics{"kgw/kgateway": {CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{
		Rejects: map[ctrlmetrics.GatewayKey]float64{{Namespace: "infra", Name: "edge"}: 1}, Managed: map[ctrlmetrics.ManagedKey]float64{}, ManagedPresent: true}}}
	mismatch := fakeMetrics{"kgw/kgateway": {CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{
		Rejects: map[ctrlmetrics.GatewayKey]float64{}, Managed: map[ctrlmetrics.ManagedKey]float64{{Namespace: "shop", Resource: "HTTPRoute"}: 5}, ManagedPresent: true}}}
	old := fakeMetrics{"kgw/kgateway": {CollectedAt: testutil.Now.Add(-10 * time.Minute), Metrics: ctrlmetrics.Metrics{
		Rejects: map[ctrlmetrics.GatewayKey]float64{}, Managed: map[ctrlmetrics.ManagedKey]float64{}, ManagedPresent: true}}}
	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	partial := testutil.HTTPRoute("shop", "partial", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")}})
	d2 := testutil.ControllerDeployment("kgw2", "kgateway2", img, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, "[]"))
	overlap := world(testutil.Namespace("kgw2", nil), d2, testutil.ControllerPod(d2, "kgateway2-0", true))

	cases := []struct {
		name      string
		s         setup
		objs      []runtime.Object
		recompute bool // compute once more, for findings that must persist (007)
		code, res string
		present   bool
		state     health.State // "" = not checked
	}{
		{"001 rejects", setup{metrics: rejects}, world(), false, health.CodeXDSRejected, "infra/edge", true, health.StateError},
		{"001 no rejects", setup{}, world(), false, health.CodeXDSRejected, "infra/edge", false, ""},
		{"002 backend not found", setup{}, world(broken), false, health.CodeConditionFalse, "shop/broken", true, health.StateError},
		{"002 accepted route", setup{}, world(), false, health.CodeConditionFalse, "shop/cart", false, ""},
		{"003 route without status", setup{}, world(testutil.HTTPRoute("shop", "new", []objects.ParentRef{gw})), false, health.CodeRouteUnattached, "shop/new", true, ""},
		{"003 stale True", setup{}, world(stale(testutil.HTTPRoute("shop", "old", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)))), false, health.CodeRouteUnattached, "shop/old", true, ""},
		{"003 required condition absent", setup{}, world(partial), false, health.CodeRouteUnattached, "shop/partial", true, ""},
		{"003 policy without ancestors", setup{}, world(testutil.TrafficPolicy("shop", "tp-new")), false, health.CodeRouteUnattached, "shop/tp-new", true, ""},
		{"003 parent gateway missing", setup{}, world(testutil.HTTPRoute("shop", "orphan", []objects.ParentRef{testutil.GwRef("infra", "gone")})), false, health.CodeRouteUnattached, "shop/orphan", true, ""},
		{"003 being processed inside grace", setup{late: []*unstructured.Unstructured{testutil.HTTPRoute("shop", "young", []objects.ParentRef{gw})}}, world(), false, health.CodeRouteUnattached, "shop/young", true, health.StateUnknown},
		{"003 accepted route", setup{}, world(), false, health.CodeRouteUnattached, "shop/cart", false, ""},
		{"004 unwatched namespace", setup{}, world(), false, health.CodeNamespaceNotWatched, "blog/posts", true, health.StateWarning},
		{"004 watched namespace", setup{}, world(), false, health.CodeNamespaceNotWatched, "shop/cart", false, ""},
		{"005 controller not ready", setup{}, worldWith(0, 1), false, health.CodeControllerNotReady, "kgw/kgateway", true, health.StateError},
		{"005 controller ready", setup{}, world(), false, health.CodeControllerNotReady, "", false, ""},
		{"006 proxy not ready", setup{}, worldWith(1, 0), false, health.CodeProxyNotReady, "infra/edge", true, health.StateError},
		{"006 proxy ready", setup{}, world(), false, health.CodeProxyNotReady, "", false, ""},
		{"007 persistent mismatch", setup{metrics: mismatch, immediate: true}, world(), true, health.CodeScopeMismatch, "", true, ""},
		{"007 fresh mismatch", setup{metrics: mismatch}, world(), true, health.CodeScopeMismatch, "", false, ""},
		{"008 metrics unreachable", setup{metrics: fakeMetrics{"kgw/kgateway": {CollectedAt: testutil.Now, Err: "connection refused"}}}, world(), false, health.CodeSignalUnavailable, "", true, health.StateUnknown},
		{"008 stale metrics", setup{metrics: old}, world(), false, health.CodeSignalUnavailable, "", true, health.StateUnknown},
		{"008 every signal available", setup{}, world(), false, health.CodeSignalUnavailable, "", false, ""},
		{"009 overlapping installs", setup{metrics: okMetrics("kgw/kgateway", "kgw2/kgateway2")}, overlap, false, health.CodeAmbiguousOwnership, "infra/edge", true, ""},
		{"009 single install", setup{}, world(), false, health.CodeAmbiguousOwnership, "", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, srv, _ := startWith(t, tc.s, tc.objs...)
			if tc.recompute {
				e.Compute()
			}
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw/kgateway/health", 200)
			if hasFinding(h, tc.code, tc.res) != tc.present {
				t.Fatalf("%s on %q: want present=%v; findings: %+v", tc.code, tc.res, tc.present, h.Findings)
			}
			if tc.state != "" && h.State != tc.state {
				t.Fatalf("state: want %s, got %s", tc.state, h.State)
			}
		})
	}
}

// isolated is world without the unwatched blog/posts route, so its baseline is healthy.
func isolated(extra ...runtime.Object) []runtime.Object {
	objs := world()
	return append(objs[:len(objs)-1:len(objs)-1], extra...)
}

// hasSev is hasFinding with a severity; an empty sev matches any.
func hasSev(h HealthResponse, code string, sev health.Severity, res string) bool {
	for _, f := range h.Findings {
		if f.Code == code && (sev == "" || f.Severity == sev) && (res == "" || (f.Resource != nil && f.Resource.String() == res)) {
			return true
		}
	}
	return false
}

type expect struct {
	code string
	sev  health.Severity
	res  string
}

// Status rules at the HTTP boundary on an isolated world.
// Each case asserts the state, the expected finding and severity, the absence
// of wrong findings, and the inventory counts.
func TestStatusRulesAtTheAPI(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	gw443 := gw
	gw443.Port = 443
	cond := testutil.Cond
	noGen := testutil.AcceptedBy(gw, testutil.Controller)
	for i := range noGen.Conds {
		noGen.Conds[i].ObservedGeneration = 0
	}
	edge2 := testutil.Gateway("infra", "edge2", "kgateway", nil)
	testutil.AddSpecListener(edge2, "https")
	// Foreign install: kgw2 watches team=b and owns infra-b/edge; shop/both is attached to both installs' Gateways.
	gwB := testutil.GwRef("infra-b", "edge")
	d2 := testutil.ControllerDeployment("kgw2", "kgateway2", img, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"b"}}]`))
	twoInstalls := isolated(testutil.Namespace("kgw2", nil), testutil.Namespace("infra-b", map[string]string{"team": "b"}), d2,
		testutil.ControllerPod(d2, "kgateway2-0", true), testutil.Gateway("infra-b", "edge", "kgateway", nil), testutil.ProxyDeployment("infra-b", "edge", 1, 1),
		testutil.HTTPRoute("shop", "both", []objects.ParentRef{gw, gwB}, testutil.AcceptedBy(gw, testutil.Controller),
			testutil.RouteParent{Ref: gwB, Controller: testutil.Controller, Conds: []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners"), cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}}))

	cases := []struct {
		name   string
		s      setup
		objs   []runtime.Object
		state  health.State
		want   *expect
		forbid []expect
		counts map[string]int
	}{
		{"baseline", setup{}, isolated(), health.StateHealthy, nil, nil, map[string]int{"GatewayClass": 1, "Gateway": 1, "HTTPRoute": 1}},
		{"stale True", setup{}, isolated(stale(testutil.HTTPRoute("shop", "old", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)))),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/old"}, []expect{{health.CodeConditionFalse, "", "shop/old"}}, map[string]int{"HTTPRoute": 2}},
		{"stale False", setup{}, isolated(stale(testutil.HTTPRoute("shop", "oldfail", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
			Conds: []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners"), cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}}))),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/oldfail"}, []expect{{health.CodeConditionFalse, "", "shop/oldfail"}}, map[string]int{"HTTPRoute": 2}},
		{"required condition absent", setup{}, isolated(testutil.HTTPRoute("shop", "partial", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
			Conds: []metav1.Condition{cond("Accepted", metav1.ConditionTrue, "Accepted")}})),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/partial"}, []expect{{health.CodeConditionFalse, "", "shop/partial"}}, map[string]int{"HTTPRoute": 2}},
		{"observedGeneration 0", setup{}, isolated(testutil.HTTPRoute("shop", "nogen", []objects.ParentRef{gw}, noGen)),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/nogen"}, nil, map[string]int{"HTTPRoute": 2}},
		{"policy Pending", setup{}, isolated(testutil.TrafficPolicy("shop", "tp-pending", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
			Conds: []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "Pending"), cond("Attached", metav1.ConditionFalse, "Pending")}})),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/tp-pending"}, []expect{{health.CodeConditionFalse, "", "shop/tp-pending"}}, map[string]int{"TrafficPolicy": 1}},
		{"policy without ancestors", setup{}, isolated(testutil.TrafficPolicy("shop", "tp-new")),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/tp-new"}, nil, map[string]int{"TrafficPolicy": 1}},
		{"new generic policy", setup{}, isolated(testutil.BackendTLSPolicy("shop", "btls-new")),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/btls-new"}, nil, map[string]int{"BackendTLSPolicy": 1}},
		{"being processed (first seen inside the grace period)", setup{late: []*unstructured.Unstructured{testutil.HTTPRoute("shop", "young", []objects.ParentRef{gw})}}, isolated(),
			health.StateUnknown, &expect{health.CodeRouteUnattached, health.SeverityInfo, "shop/young"}, []expect{{health.CodeRouteUnattached, health.SeverityWarning, "shop/young"}}, map[string]int{"HTTPRoute": 2}},
		{"status for a removed parent", setup{}, isolated(testutil.HTTPRoute("shop", "moved", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller),
			testutil.RouteParent{Ref: testutil.GwRef("infra", "old"), Controller: testutil.Controller, Conds: []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "NoMatchingParent"), cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}})),
			health.StateHealthy, nil, []expect{{health.CodeConditionFalse, "", "shop/moved"}, {health.CodeRouteUnattached, "", "shop/moved"}}, map[string]int{"HTTPRoute": 2}},
		{"status only for another port", setup{}, isolated(testutil.HTTPRoute("shop", "ported", []objects.ParentRef{gw443}, testutil.AcceptedBy(gw, testutil.Controller))),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/ported"}, []expect{{health.CodeConditionFalse, "", "shop/ported"}}, map[string]int{"HTTPRoute": 2}},
		{"foreign-group parentRef", setup{}, isolated(testutil.HTTPRoute("shop", "foreign", []objects.ParentRef{{Group: "example.com", Kind: "Gateway", Namespace: "infra", Name: "edge"}})),
			health.StateHealthy, nil, []expect{{health.CodeRouteUnattached, "", "shop/foreign"}, {health.CodeConditionFalse, "", "shop/foreign"}}, map[string]int{"HTTPRoute": 1}},
		{"listener without a status block", setup{}, isolated(edge2, testutil.ProxyDeployment("infra", "edge2", 1, 1)),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "infra/edge2"}, nil, map[string]int{"Gateway": 2}},
		// A route owned through one parent counts; its missing parent still raises KGWUI-003.
		{"one parent owned, one missing", setup{}, isolated(testutil.HTTPRoute("shop", "half", []objects.ParentRef{gw, testutil.GwRef("infra", "gone")}, testutil.AcceptedBy(gw, testutil.Controller))),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/half"}, []expect{{health.CodeConditionFalse, "", "shop/half"}}, map[string]int{"HTTPRoute": 2}},
		{"parent status of another install", setup{metrics: okMetrics("kgw/kgateway", "kgw2/kgateway2")}, twoInstalls,
			health.StateHealthy, nil, []expect{{health.CodeConditionFalse, "", "shop/both"}, {health.CodeRouteUnattached, "", "shop/both"}}, map[string]int{"HTTPRoute": 2, "Gateway": 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, tc.s, tc.objs...)
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw/kgateway/health", 200)
			if h.State != tc.state {
				t.Fatalf("state: want %s, got %s; findings: %+v", tc.state, h.State, h.Findings)
			}
			if tc.want != nil && !hasSev(h, tc.want.code, tc.want.sev, tc.want.res) {
				t.Fatalf("missing %s/%s on %s; findings: %+v", tc.want.code, tc.want.sev, tc.want.res, h.Findings)
			}
			for _, f := range tc.forbid {
				if hasSev(h, f.code, f.sev, f.res) {
					t.Fatalf("unexpected %s/%s on %s; findings: %+v", f.code, f.sev, f.res, h.Findings)
				}
			}
			o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw/kgateway/overview", 200)
			for kind, n := range tc.counts {
				if got, _ := count(o, kind); got != n {
					t.Errorf("%s count: want %d, got %d", kind, n, got)
				}
			}
		})
	}
}

// Failures after the initial sync, through
// the fake dynamic and typed clients. A failed Watch call or an error event
// inside the stream makes the install unknown; further failed attempts keep it
// so; the next successful Watch call recovers it, whether the reflector resumed
// a classic watch or relisted first.
func TestWatchFailuresAfterSyncAtTheAPI(t *testing.T) {
	refused := fmt.Errorf("dial tcp 10.96.0.1:443: connect: %w", syscall.ECONNREFUSED)
	cases := []struct {
		name     string
		resource string
		typed    bool
		mode     string // "call": the Watch call fails; "event": an error event in the stream
		err      error
	}{
		{"httproutes watch call refused", "httproutes", false, "call", refused},
		{"httproutes watch call throttled", "httproutes", false, "call", apierrors.NewTooManyRequests("slow down", 1)},
		{"httproutes watch call forbidden", "httproutes", false, "call", apierrors.NewForbidden(schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: "httproutes"}, "", nil)},
		{"httproutes error event", "httproutes", false, "event", apierrors.NewTooManyRequests("slow down", 1)},
		{"namespaces error event", "namespaces", true, "event", apierrors.NewInternalError(errors.New("etcd timeout"))},
		{"pods watch call throttled", "pods", true, "call", apierrors.NewTooManyRequests("slow down", 1)},
		{"deployments watch call refused", "deployments", true, "call", refused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var fail atomic.Bool
			var failedCalls atomic.Int32 // failed Watch and List calls while fail is set
			watchers := make(chan *watch.FakeWatcher, 64)
			onWatch := func(clienttesting.Action) (bool, watch.Interface, error) {
				if fail.Load() && tc.mode == "call" {
					failedCalls.Add(1)
					return true, nil, tc.err
				}
				w := watch.NewFake()
				watchers <- w
				return true, w, nil
			}
			onList := func(clienttesting.Action) (bool, runtime.Object, error) {
				if fail.Load() {
					failedCalls.Add(1)
					return true, nil, apierrors.NewServiceUnavailable("apiserver restarting")
				}
				return false, nil, nil
			}
			hn := newHarness(t, setup{prepare: func(dyn *dynamicfake.FakeDynamicClient, kube *kubefake.Clientset) {
				if tc.typed {
					kube.PrependWatchReactor(tc.resource, onWatch)
					kube.PrependReactor("list", tc.resource, onList)
				} else {
					dyn.PrependWatchReactor(tc.resource, onWatch)
					dyn.PrependReactor("list", tc.resource, onList)
				}
			}}, isolated()...)
			state := func() health.State {
				hn.e.Compute()
				return get[HealthResponse](t, hn.srv, "/api/v1/installs/kgw/kgateway/health", 200).State
			}
			if s := state(); s != health.StateHealthy {
				t.Fatalf("baseline must be healthy, got %s", s)
			}
			w := <-watchers
			fail.Store(true)
			if tc.mode == "event" {
				w.Error(&tc.err.(*apierrors.StatusError).ErrStatus)
			} else {
				w.Stop() // the established watch ends; the next Watch call fails
			}
			waitUntil(t, "the install to become unknown", func() bool { return state() == health.StateUnknown })
			h := get[HealthResponse](t, hn.srv, "/api/v1/installs/kgw/kgateway/health", 200)
			if !hasSev(h, health.CodeSignalUnavailable, health.SeverityInfo, "") {
				t.Fatalf("KGWUI-008 missing: %+v", h.Findings)
			}
			if tc.mode == "call" {
				n := failedCalls.Load()
				waitUntil(t, "another failed attempt", func() bool { return failedCalls.Load() > n })
				if s := state(); s != health.StateUnknown {
					t.Fatalf("further failed attempts must keep the install unknown, got %s", s)
				}
			}
			fail.Store(false)
			<-watchers // the next successful Watch call: a resumed classic watch, or a new one after a relist
			waitUntil(t, "recovery on the next successful watch", func() bool { return state() == health.StateHealthy })
		})
	}
}

// waitUntil polls cond until it holds or 15 s pass (reflector backoff starts at 0.8 s).
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestSPAFallbackAndProbes(t *testing.T) {
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw/kgateway"), world()...)
	for _, p := range []string{"/", "/i/kgw/kgateway", "/i/kgw/kgateway/findings", "/healthz", "/readyz"} {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
	}
	get[map[string]any](t, srv, "/api/v1/nope", 404)
}

// D-013: a denied object is 404 and leaves the tiles and gateway rows; its
// findings stay, in the same order, with code, severity and resource but no
// message or evidence; the roll-up and severity counts do not change.
func TestAuthorizerRestrictsDeniedObjects(t *testing.T) {
	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{testutil.GwRef("infra", "edge")}, testutil.RouteParent{Ref: testutil.GwRef("infra", "edge"),
		Controller: testutil.Controller, Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	denied := func(ref objects.Ref) bool {
		return ref.Namespace == "shop" || (ref.Kind == "Gateway" && ref.Name == "edge")
	}
	authz := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return !denied(ref) })
	_, all, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw/kgateway"), world(broken)...)
	_, some, _ := startWith(t, setup{authz: authz}, world(broken)...)
	const base = "/api/v1/installs/kgw/kgateway"

	full := get[HealthResponse](t, all, base+"/health", 200)
	part := get[HealthResponse](t, some, base+"/health", 200)
	if part.State != full.State || part.State != health.StateError || len(part.Findings) != len(full.Findings) {
		t.Fatalf("state and findings must cover everything: full %s %d, partial %s %d", full.State, len(full.Findings), part.State, len(part.Findings))
	}
	for sev, n := range full.Counts {
		if part.Counts[sev] != n {
			t.Errorf("%s count: want %d, got %d", sev, n, part.Counts[sev])
		}
	}
	restricted := 0
	for i, f := range part.Findings {
		w := full.Findings[i]
		if f.Code != w.Code || f.Severity != w.Severity || (f.Resource == nil) != (w.Resource == nil) || (f.Resource != nil && *f.Resource != *w.Resource) {
			t.Fatalf("finding %d: order, code, severity or resource changed: %+v vs %+v", i, f, w)
		}
		if f.Resource != nil && denied(*f.Resource) {
			restricted++
			if !f.Restricted || f.Message != "" || f.Detail != "" || f.Since != nil {
				t.Errorf("finding on a denied resource must be restricted without evidence: %+v", f)
			}
			continue
		}
		if f.Restricted || f.Message != w.Message || f.Detail != w.Detail {
			t.Errorf("finding on a visible resource must be unchanged: %+v vs %+v", f, w)
		}
	}
	if restricted == 0 {
		t.Fatal("expected at least one finding on a denied resource")
	}
	insts := get[[]InstallSummary](t, some, "/api/v1/installs", 200)
	if len(insts) != 1 || insts[0].Health.State != full.State || insts[0].Health.Counts[health.SeverityError] != full.Counts[health.SeverityError] {
		t.Fatalf("install summary must keep the full roll-up: %+v", insts)
	}

	o := get[OverviewResponse](t, some, base+"/overview", 200)
	for kind, want := range map[string]int{"GatewayClass": 1, "Gateway": 0, "HTTPRoute": 0} {
		if n, _ := count(o, kind); n != want {
			t.Errorf("%s tile: want %d, got %d", kind, want, n)
		}
	}
	if len(o.Gateways) != 0 {
		t.Errorf("a denied Gateway must leave the gateway rows: %+v", o.Gateways)
	}
	if fo := get[OverviewResponse](t, all, base+"/overview", 200); len(fo.Gateways) != 1 {
		t.Fatalf("allow-all baseline must show the Gateway: %+v", fo.Gateways)
	}

	for _, p := range []string{"gateway.networking.k8s.io/HTTPRoute/shop/cart", "gateway.networking.k8s.io/HTTPRoute/shop/broken", "gateway.networking.k8s.io/Gateway/infra/edge"} {
		get[map[string]any](t, all, base+"/resources/"+p, 200)
		get[map[string]any](t, some, base+"/resources/"+p, 404)
	}
	get[map[string]any](t, some, base+"/resources/gateway.networking.k8s.io/HTTPRoute/blog/posts", 200)
}

// Before the cache has synced every data endpoint is 503 and readiness fails.
func TestNotSyncedIsServiceUnavailable(t *testing.T) {
	srv := httptest.NewServer(NewHandler(staticModel{&engine.Model{Snapshot: &cache.Snapshot{}}}, fstest.MapFS{}, nil))
	t.Cleanup(srv.Close)
	for _, p := range []string{"/api/v1/installs", "/api/v1/installs/kgw/kgateway/health", "/readyz"} {
		get[map[string]any](t, srv, p, http.StatusServiceUnavailable)
	}
	nilSrv := httptest.NewServer(NewHandler(staticModel{}, fstest.MapFS{}, nil))
	t.Cleanup(nilSrv.Close)
	get[map[string]any](t, nilSrv, "/api/v1/installs", http.StatusServiceUnavailable)
	if m := get[MetaResponse](t, nilSrv, "/api/v1/meta", 200); m.Synced || len(m.Kinds) != 1 {
		t.Fatalf("meta before the first computation: %+v", m)
	}
}

type staticModel struct{ m *engine.Model }

func (s staticModel) Current() *engine.Model { return s.m }
