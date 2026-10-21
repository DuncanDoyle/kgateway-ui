package api

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	appsv1 "k8s.io/api/apps/v1"
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
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
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
	metrics   ctrlmetrics.Source // nil: okMetrics("kgw.kgateway")
	immediate bool               // report scope mismatches on the second computation (persistence 0, not 1 h)
	authz     Authorizer         // nil: AllowAll
	// prepare runs on the fake clients before the cache starts (failure injection).
	prepare func(dyn *dynamicfake.FakeDynamicClient, kube *kubefake.Clientset)
	// late objects are created after the engine has first seen the rest, so their
	// generation is first seen at testutil.Now: inside the grace period.
	late []*unstructured.Unstructured
	// detectors replace the upstream detector; nil keeps it.
	detectors []install.Detector
	// platform is the engine's platform; zero is Kubernetes.
	platform platform.Info
	// logger receives install rejections; nil is slog.Default().
	logger *slog.Logger
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
		s.metrics = okMetrics("kgw.kgateway")
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
	dets := s.detectors
	if dets == nil {
		dets = []install.Detector{install.UpstreamDetector{}}
	}
	var podSel, depSel []labels.Selector
	for _, d := range dets {
		podSel = append(podSel, d.ControllerPodSelector())
		depSel = append(depSel, d.ControllerSelector(), d.ProxySelector())
	}
	c := cache.New(cache.Config{Dynamic: dyn, Kube: kube, Served: s.served, PodSelectors: podSel, DeploymentSelectors: depSel})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Start(ctx)
	clk := &clock{}
	clk.set(testutil.Now.Add(-health.Grace - time.Second))
	e := engine.New(engine.Config{Snapshots: c, Served: s.served, Detectors: dets, Scope: install.NamespaceSelectorScope{},
		Verifier: scopeverify.NewManagedCountVerifier(persist), Metrics: s.metrics, Checks: health.Upstream(), Pinned: s.pinned,
		MetricsMaxAge: 30 * time.Second, Now: clk.now, Platform: s.platform, Logger: s.logger})
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

// world: install kgw.kgateway scoped to team=a; infra and shop are watched, blog is not.
func world(extra ...runtime.Object) []runtime.Object { return worldWith(1, 1, extra...) }

// worldWith is world with the number of ready controller and proxy replicas (of 1).
func worldWith(ctrlReady, proxyReady int32, extra ...runtime.Object) []runtime.Object {
	return replicaWorld(1, ctrlReady, 1, proxyReady, extra...)
}

// replicaWorld is world with desired and ready replicas for the controller and
// the proxy. A controller with 0 desired replicas has no Pod.
func replicaWorld(ctrlDesired, ctrlReady, proxyDesired, proxyReady int32, extra ...runtime.Object) []runtime.Object {
	d := testutil.ControllerDeployment("kgw", "kgateway", img, ctrlDesired, ctrlReady, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"a"}}]`))
	gw := testutil.GwRef("infra", "edge")
	objs := []runtime.Object{testutil.Namespace("kgw", nil), testutil.Namespace("infra", map[string]string{"team": "a"}),
		testutil.Namespace("shop", map[string]string{"team": "a"}), testutil.Namespace("blog", nil), d}
	if ctrlDesired > 0 {
		objs = append(objs, testutil.ControllerPod(d, "kgateway-0", ctrlReady > 0))
	}
	// blog/posts stays last: isolated() drops it.
	return append(append(objs,
		testutil.ProxyDeployment("infra", "edge", proxyDesired, proxyReady),
		testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.GatewayClass("other", "example.com/other", true),
		testutil.Gateway("infra", "edge", "kgateway", nil), testutil.Gateway("infra", "foreign", "other", nil),
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.HTTPRoute("blog", "posts", []objects.ParentRef{gw}),
	), extra...)
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
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw.kgateway"), world()...)
	insts := get[[]InstallSummary](t, srv, "/api/v1/installs", 200)
	if len(insts) != 1 || insts[0].ID != "kgw.kgateway" || insts[0].Version != "2.4.7" {
		t.Fatalf("installs: %+v", insts)
	}
	if r := insts[0].Readiness; r.State != overview.ReadinessReady || *r.Ready != 1 || *r.Desired != 1 {
		t.Errorf("controller readiness: %+v", r)
	}
	if !insts[0].ComputedAt.Equal(testutil.Now) {
		t.Errorf("installs must carry the model's computation time, got %s", insts[0].ComputedAt)
	}
	o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", 200)
	if o.Header.ID != "kgw.kgateway" {
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
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
	if !hasFinding(h, health.CodeNamespaceNotWatched, "blog/posts") || h.State != health.StateWarning || h.Install != "kgw.kgateway" {
		t.Fatalf("health: %+v", h)
	}
}

func TestMetricsUnreachableIsNeverHealthy(t *testing.T) {
	m := fakeMetrics{"kgw.kgateway": {CollectedAt: testutil.Now, Err: "dial tcp: connection refused"}}
	_, srv, _ := start(t, testutil.ServedUpstream(), "", m, world()[:len(world())-1]...) // without blog/posts
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
	if h.State != health.StateUnknown || !hasFinding(h, health.CodeSignalUnavailable, "") {
		t.Fatalf("want unknown with signal-unavailable, got %s %+v", h.State, h.Findings)
	}
}

// D-012: every owned Gateway yields a kgateway_resources_managed series, so a
// scrape without that family while the install owns a Gateway is the wrong
// endpoint or a renamed metric: xDS rejects are unavailable, not zero.
func TestScrapeWithoutManagedFamilyIsNeverHealthy(t *testing.T) {
	empty := fakeMetrics{"kgw.kgateway": {CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{
		Rejects: map[ctrlmetrics.GatewayKey]float64{}, Managed: map[ctrlmetrics.ManagedKey]float64{}}}}
	xds := func(h HealthResponse) *health.CheckResult {
		for i := range h.Checks {
			if h.Checks[i].Name == "xDS rejects" {
				return &h.Checks[i]
			}
		}
		t.Fatalf("no xDS check: %+v", h.Checks)
		return nil
	}
	_, srv, _ := start(t, testutil.ServedUpstream(), "", empty, isolated()...)
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
	if c := xds(h); h.State != health.StateUnknown || c.OK || !c.Required || !hasSev(h, health.CodeSignalUnavailable, health.SeverityInfo, "") {
		t.Fatalf("an owned Gateway without a managed series must make xDS unavailable: %s %+v %+v", h.State, c, h.Findings)
	}

	// Without owned Gateways an absent family is expected: no series yet.
	d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, 1)
	_, bare, _ := start(t, testutil.ServedUpstream(), "", empty, testutil.Namespace("kgw", nil), d, testutil.ControllerPod(d, "kgateway-0", true))
	h = get[HealthResponse](t, bare, "/api/v1/installs/kgw.kgateway/health", 200)
	if c := xds(h); h.State != health.StateHealthy || !c.OK {
		t.Fatalf("no owned Gateway: an absent family is not an error: %s %+v %+v", h.State, c, h.Findings)
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
	_, srv, _ := start(t, served, "", okMetrics("kgw.kgateway"), world(tcp)...)
	o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", 200)
	if _, ok := count(o, "TCPRoute"); ok {
		t.Fatal("an unserved kind must not have a tile")
	}
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
	for _, f := range h.Findings {
		if f.Resource != nil && f.Resource.Kind == "TCPRoute" {
			t.Fatalf("an unserved kind must not produce a finding: %+v", f)
		}
	}
	get[map[string]any](t, srv, "/api/v1/installs/kgw.kgateway/resources/gateway.networking.k8s.io/TCPRoute/shop/db", 404)
}

func TestOverlappingInstallsAndPinning(t *testing.T) {
	d2 := testutil.ControllerDeployment("kgw2", "kgateway2", img, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, "[]"))
	objs := world(testutil.Namespace("kgw2", nil), d2, testutil.ControllerPod(d2, "kgateway2-0", true))
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw.kgateway", "kgw2.kgateway2"), objs...)
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw2.kgateway2/health", 200)
	if !hasFinding(h, health.CodeAmbiguousOwnership, "infra/edge") {
		t.Fatalf("overlapping scopes must report ambiguous ownership: %+v", h.Findings)
	}
	_, pinned, _ := start(t, testutil.ServedUpstream(), "kgw.kgateway", okMetrics("kgw.kgateway", "kgw2.kgateway2"), objs...)
	if insts := get[[]InstallSummary](t, pinned, "/api/v1/installs", 200); len(insts) != 1 || insts[0].ID != "kgw.kgateway" {
		t.Fatalf("pinned: %+v", insts)
	}
	get[HealthResponse](t, pinned, "/api/v1/installs/kgw2.kgateway2/health", 404)
}

func TestNamespaceLabelChangeUpdatesScopeWithoutRestart(t *testing.T) {
	e, srv, update := start(t, testutil.ServedUpstream(), "", okMetrics("kgw.kgateway"), world()...)
	update(testutil.Namespace("blog", map[string]string{"team": "a"}))
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.Compute()
		o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", 200)
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
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw.kgateway"), world()...)
	const base = "/api/v1/installs/kgw.kgateway/resources/"
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
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw.kgateway"), worldWith(0, 1)...)
	r := get[map[string]any](t, srv, "/api/v1/installs/kgw.kgateway/resources/apps/Deployment/kgw/kgateway", 200)
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
	rejects := fakeMetrics{"kgw.kgateway": {CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{
		Rejects: map[ctrlmetrics.GatewayKey]float64{{Namespace: "infra", Name: "edge"}: 1}, Managed: map[ctrlmetrics.ManagedKey]float64{}, ManagedPresent: true}}}
	mismatch := fakeMetrics{"kgw.kgateway": {CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{
		Rejects: map[ctrlmetrics.GatewayKey]float64{}, Managed: map[ctrlmetrics.ManagedKey]float64{{Namespace: "shop", Resource: "HTTPRoute"}: 5}, ManagedPresent: true}}}
	old := fakeMetrics{"kgw.kgateway": {CollectedAt: testutil.Now.Add(-10 * time.Minute), Metrics: ctrlmetrics.Metrics{
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
		{"008 metrics unreachable", setup{metrics: fakeMetrics{"kgw.kgateway": {CollectedAt: testutil.Now, Err: "connection refused"}}}, world(), false, health.CodeSignalUnavailable, "", true, health.StateUnknown},
		{"008 stale metrics", setup{metrics: old}, world(), false, health.CodeSignalUnavailable, "", true, health.StateUnknown},
		{"008 every signal available", setup{}, world(), false, health.CodeSignalUnavailable, "", false, ""},
		{"009 overlapping installs", setup{metrics: okMetrics("kgw.kgateway", "kgw2.kgateway2")}, overlap, false, health.CodeAmbiguousOwnership, "infra/edge", true, ""},
		{"009 single install", setup{}, world(), false, health.CodeAmbiguousOwnership, "", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, srv, _ := startWith(t, tc.s, tc.objs...)
			if tc.recompute {
				e.Compute()
			}
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
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
		{"parent status of another install", setup{metrics: okMetrics("kgw.kgateway", "kgw2.kgateway2")}, twoInstalls,
			health.StateHealthy, nil, []expect{{health.CodeConditionFalse, "", "shop/both"}, {health.CodeRouteUnattached, "", "shop/both"}}, map[string]int{"HTTPRoute": 2, "Gateway": 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, tc.s, tc.objs...)
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
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
			o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", 200)
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
			t.Parallel()
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
				return get[HealthResponse](t, hn.srv, "/api/v1/installs/kgw.kgateway/health", 200).State
			}
			if s := state(); s != health.StateHealthy {
				t.Fatalf("baseline must be healthy, got %s", s)
			}
			w := testutil.Recv(t, watchers, "the established watch")
			fail.Store(true)
			if tc.mode == "event" {
				w.Error(&tc.err.(*apierrors.StatusError).ErrStatus)
			} else {
				w.Stop() // the established watch ends; the next Watch call fails
			}
			waitUntil(t, "the install to become unknown", func() bool { return state() == health.StateUnknown })
			h := get[HealthResponse](t, hn.srv, "/api/v1/installs/kgw.kgateway/health", 200)
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
			// Watchers opened before or during the failure must not stand in for the recovery one.
			testutil.Drain(watchers)
			fail.Store(false)
			testutil.Recv(t, watchers, "the next successful Watch call") // a resumed classic watch, or a new one after a relist
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
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw.kgateway"), world()...)
	for _, p := range []string{"/", "/i/kgw.kgateway", "/i/kgw.kgateway/findings", "/healthz", "/readyz"} {
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
	_, all, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw.kgateway"), world(broken)...)
	_, some, _ := startWith(t, setup{authz: authz}, world(broken)...)
	const base = "/api/v1/installs/kgw.kgateway"

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
	for _, p := range []string{"/api/v1/installs", "/api/v1/installs/kgw.kgateway/health", "/readyz"} {
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

func (staticModel) Platform() platform.Info { return platform.Kubernetes() }

// A controller scaled to zero has no Pod; the scope comes from the Deployment
// template, its Gateways stay owned and KGWUI-005 reports it is not ready.
func TestScaledToZeroControllerKeepsScopeFromTemplate(t *testing.T) {
	var objs []runtime.Object
	for _, o := range worldWith(0, 1) {
		if _, isPod := o.(*corev1.Pod); !isPod {
			objs = append(objs, o)
		}
	}
	_, srv, _ := start(t, testutil.ServedUpstream(), "", fakeMetrics{"kgw.kgateway": {CollectedAt: testutil.Now, Err: "no ready controller pod to scrape"}}, objs...)
	o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", 200)
	sc := o.Header.Scope
	if sc.Mode != string(install.ScopeSelectors) || sc.Source != install.SourceDeploymentTemplate || sc.Watched != 2 {
		t.Fatalf("scope must come from the template: %+v", sc)
	}
	if n, _ := count(o, "Gateway"); n != 1 {
		t.Errorf("Gateways: want 1 still owned, got %d", n)
	}
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
	if !hasFinding(h, health.CodeControllerNotReady, "kgw/kgateway") || h.State != health.StateError {
		t.Fatalf("want KGWUI-005 and error state, not a false green: %s %+v", h.State, h.Findings)
	}
	if hasFinding(h, health.CodeScopeMismatch, "") {
		t.Fatalf("no KGWUI-007 without metrics to verify against: %+v", h.Findings)
	}
	detail := ""
	for _, c := range h.Checks {
		if c.Name == "controller scope" {
			detail = c.Detail
		}
	}
	if !strings.Contains(detail, "Deployment template") {
		t.Errorf("scope check must say where the scope came from, got %q", detail)
	}
}

// idDetector is the upstream detector reporting a provider-chosen install ID
// and no namespace or name, as a non-Kubernetes provider would (D-016).
type idDetector struct {
	install.UpstreamDetector
	id string
}

// Detect implements install.Detector.
func (d idDetector) Detect(dep *appsv1.Deployment) (install.Install, bool) {
	in, ok := d.UpstreamDetector.Detect(dep)
	in.CustomID, in.DisplayName, in.Namespace, in.Name = d.id, "Edge one", "", ""
	return in, ok
}

func isDeployment(k kinds.Served) bool { return k.Group == "apps" && k.Kind == "Deployment" }

// H1: an install ID is one opaque path segment. The Kubernetes ID and a
// provider ID with ~ and _ resolve on every endpoint, a percent-encoded ID
// reaches the same install, and the old two-segment path resolves to nothing.
func TestOpaqueInstallIDsAtTheAPI(t *testing.T) {
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw.kgateway"), world()...)
	insts := get[[]InstallSummary](t, srv, "/api/v1/installs", 200)
	if len(insts) != 1 || insts[0].ID != "kgw.kgateway" || insts[0].DisplayName != "kgw/kgateway" || insts[0].Namespace != "kgw" || insts[0].Name != "kgateway" {
		t.Fatalf("installs: %+v", insts)
	}
	for _, p := range []string{"/overview", "/health", "/resources/gateway.networking.k8s.io/Gateway/infra/edge"} {
		get[map[string]any](t, srv, "/api/v1/installs/kgw.kgateway"+p, 200)
		get[map[string]any](t, srv, "/api/v1/installs/kgw/kgateway"+p, 404)
	}

	_, csrv, _ := startWith(t, setup{detectors: []install.Detector{idDetector{id: "acme~edge_1"}}, metrics: okMetrics("acme~edge_1")}, world()...)
	h := get[HealthResponse](t, csrv, "/api/v1/installs/acme~edge_1/health", 200)
	if h.Install != "acme~edge_1" || !hasFinding(h, health.CodeNamespaceNotWatched, "blog/posts") {
		t.Fatalf("custom ID health: %+v", h)
	}
	if o := get[OverviewResponse](t, csrv, "/api/v1/installs/acme%7Eedge_1/overview", 200); o.Header.ID != "acme~edge_1" {
		t.Fatalf("a percent-encoded ID must reach the same install: %+v", o.Header)
	}
	get[map[string]any](t, csrv, "/api/v1/installs/acme~edge_1/resources/gateway.networking.k8s.io/Gateway/infra/edge", 200)
}

// H1: a provider ID outside the URL-unreserved set, or a dot segment, is
// rejected. Installs are discovered at runtime, so it cannot fail startup: the
// install is left out, the rejection is logged once (not on every recompute),
// and /readyz is 503 naming the detector and the raw ID.
func TestInvalidInstallIDsAreRejected(t *testing.T) {
	for _, id := range []string{"a/b", "50%", ".", ".."} {
		t.Run(id, func(t *testing.T) {
			var logs bytes.Buffer
			e, srv, _ := startWith(t, setup{detectors: []install.Detector{idDetector{id: id}}, logger: slog.New(slog.NewTextHandler(&logs, nil))}, world()...)
			e.Compute()
			if insts := get[[]InstallSummary](t, srv, "/api/v1/installs", 200); len(insts) != 0 {
				t.Fatalf("an invalid ID must not be served: %+v", insts)
			}
			if n := strings.Count(logs.String(), "install rejected"); n != 1 {
				t.Fatalf("want exactly one rejection log across repeated computations, got %d:\n%s", n, logs.String())
			}
			if !strings.Contains(logs.String(), "Deployment kgw/kgateway") {
				t.Fatalf("the rejection must name the Deployment:\n%s", logs.String())
			}
			resp, err := http.Get(srv.URL + "/readyz")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			var body ErrorResponse
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("/readyz: status %d, body %+v, %v", resp.StatusCode, body, err)
			}
			if !strings.Contains(body.Error, "detector kgateway") || !strings.Contains(body.Error, strconv.Quote(id)) {
				t.Fatalf("/readyz must name the detector and the raw ID %s: %q", strconv.Quote(id), body.Error)
			}
		})
	}
}

// H2 and H3: a platform without scope or workloads, and an install without a
// namespace or name, give JSON without those fields and a readiness that says
// "not observed" (D-016).
func TestPlatformWithoutScopeOrWorkloadsAtTheAPI(t *testing.T) {
	s := setup{detectors: []install.Detector{idDetector{id: "acme~edge_1"}}, metrics: okMetrics("acme~edge_1"), platform: platform.Info{Name: "Test"}}
	_, srv, _ := startWith(t, s, world()...)
	insts := get[[]map[string]any](t, srv, "/api/v1/installs", 200)
	if len(insts) != 1 {
		t.Fatalf("installs: %+v", insts)
	}
	hd := get[map[string]any](t, srv, "/api/v1/installs/acme~edge_1/overview", 200)["header"].(map[string]any)
	for what, obj := range map[string]map[string]any{"install": insts[0], "header": hd} {
		for _, k := range []string{"namespace", "name", "scope"} {
			if v, ok := obj[k]; ok {
				t.Errorf("%s: %s must be absent, got %v", what, k, v)
			}
		}
		if obj["displayName"] != "Edge one" {
			t.Errorf("%s: displayName %v", what, obj["displayName"])
		}
		if r, _ := obj["readiness"].(map[string]any); len(r) != 1 || r["state"] != "not-observed" {
			t.Errorf("%s: readiness must be only {state: not-observed}, got %v", what, obj["readiness"])
		}
	}
	meta := get[MetaResponse](t, srv, "/api/v1/meta", 200)
	if meta.Platform != "Test" || meta.Capabilities == nil || len(meta.Capabilities) != 0 || slices.ContainsFunc(meta.Kinds, isDeployment) {
		t.Fatalf("meta must report no capabilities and no Deployment kind: %+v", meta)
	}
	// Without workloads, a not-ready finding carries no replica wording either.
	_, down, _ := startWith(t, s, worldWith(0, 1)...)
	h := get[HealthResponse](t, down, "/api/v1/installs/acme~edge_1/health", 200)
	if !hasFinding(h, health.CodeControllerNotReady, "kgw/kgateway") {
		t.Fatalf("a controller with 0 of 1 ready replicas must give KGWUI-005: %+v", h.Findings)
	}
	for _, f := range h.Findings {
		if f.Code == health.CodeControllerNotReady && f.Message != "Controller not ready" {
			t.Fatalf("KGWUI-005 without workloads: %q", f.Message)
		}
	}
}

// H3: Kubernetes reports both capabilities, Deployments are linkable, and the
// install carries replica counts and its scope.
func TestKubernetesPlatformAtTheAPI(t *testing.T) {
	_, srv, _ := start(t, testutil.ServedUpstream(), "", nil, world()...)
	meta := get[MetaResponse](t, srv, "/api/v1/meta", 200)
	if meta.Platform != "Kubernetes" || !slices.Equal(meta.Capabilities, []string{platform.Scope, platform.Workloads}) || !slices.ContainsFunc(meta.Kinds, isDeployment) {
		t.Fatalf("meta: %+v", meta)
	}
	in := get[[]InstallSummary](t, srv, "/api/v1/installs", 200)[0]
	r := in.Readiness
	if r.State != overview.ReadinessReady || r.Ready == nil || *r.Ready != 1 || r.Desired == nil || *r.Desired != 1 {
		t.Fatalf("readiness: %+v", r)
	}
	if in.Scope == nil || in.Scope.Watched != 2 || in.Scope.Total != 4 {
		t.Fatalf("scope: %+v", in.Scope)
	}
	_, down, _ := start(t, testutil.ServedUpstream(), "", nil, worldWith(0, 1)...)
	if r := get[[]InstallSummary](t, down, "/api/v1/installs", 200)[0].Readiness; r.State != overview.ReadinessNotReady || *r.Ready != 0 {
		t.Fatalf("a controller with 0 of 1 ready replicas must be not-ready: %+v", r)
	}
}

// proxySetup shapes proxyWorld. The zero value is a managed Gateway with a ready proxy.
type proxySetup struct {
	classParams, gwParams string             // GatewayParameters names in infra; "" = no reference
	classRefKind          string             // group "example.com" and this kind on the class reference; "" = GatewayParameters
	gwRefKind             string             // kind of the Gateway's reference; "" = GatewayParameters
	noProxy               bool               // no proxy Deployment
	proxyNotReady         bool               // the proxy Deployment has 0 of 1 ready replicas
	gwConds               []metav1.Condition // nil: Accepted and Programmed
}

// proxyWorld is isolated() with the parameters and proxy Deployment of p.
func proxyWorld(p proxySetup, extra ...runtime.Object) []runtime.Object {
	var out []runtime.Object
	for _, o := range isolated() {
		switch v := o.(type) {
		case *appsv1.Deployment:
			if v.Namespace == "infra" && v.Name == "edge" {
				if p.noProxy {
					continue
				}
				ready := int32(1)
				if p.proxyNotReady {
					ready = 0
				}
				o = testutil.ProxyDeployment("infra", "edge", 1, ready)
			}
		case *unstructured.Unstructured:
			if v.GetKind() == "GatewayClass" && v.GetName() == "kgateway" && p.classParams != "" {
				group, kind := kinds.KgatewayGroup, "GatewayParameters"
				if p.classRefKind != "" {
					group, kind = "example.com", p.classRefKind
				}
				testutil.SetClassParameters(v, group, kind, "infra", p.classParams)
			}
			if v.GetKind() == "Gateway" && v.GetName() == "edge" {
				if p.gwConds != nil {
					v = testutil.Gateway("infra", "edge", "kgateway", p.gwConds)
					o = v
				}
				if p.gwParams != "" {
					kind := cmp.Or(p.gwRefKind, "GatewayParameters")
					testutil.SetGatewayParameters(v, kinds.KgatewayGroup, kind, p.gwParams)
				}
			}
		}
		out = append(out, o)
	}
	return append(out, extra...)
}

const selfManagedMsg = "Self-managed proxy: the UI cannot see its readiness"

// H5 acceptance (design §14.3) at the health endpoint: a self-managed Gateway
// without a proxy Deployment gets KGWUI-010 (info) instead of KGWUI-006, and
// nothing else about its health changes.
func TestSelfManagedProxiesAtTheAPI(t *testing.T) {
	sm, kube := testutil.GatewayParameters("infra", "sm", true), testutil.GatewayParameters("infra", "kube", false)
	cond := testutil.Cond
	notProgrammed := []metav1.Condition{cond("Accepted", metav1.ConditionTrue, "Accepted"), cond("Programmed", metav1.ConditionFalse, "AddressNotAssigned")}
	invalidParams := []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "InvalidParameters"), cond("Programmed", metav1.ConditionFalse, "Invalid")}
	rejects := fakeMetrics{"kgw.kgateway": {CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{
		Rejects: map[ctrlmetrics.GatewayKey]float64{{Namespace: "infra", Name: "edge"}: 1}, Managed: map[ctrlmetrics.ManagedKey]float64{}, ManagedPresent: true}}}
	forbidParams := func(dyn *dynamicfake.FakeDynamicClient, _ *kubefake.Clientset) {
		dyn.PrependReactor("list", "gatewayparameters", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: kinds.KgatewayGroup, Resource: "gatewayparameters"}, "", nil)
		})
	}
	noParamsKind := slices.DeleteFunc(testutil.ServedUpstream(), func(k kinds.Served) bool { return k.Kind == "GatewayParameters" })
	selfManaged := expect{health.CodeSelfManagedProxy, health.SeverityInfo, "infra/edge"}
	missing := expect{health.CodeProxyNotReady, health.SeverityWarning, "infra/edge"}
	anySelf, anyProxy := expect{health.CodeSelfManagedProxy, "", ""}, expect{health.CodeProxyNotReady, "", ""}

	cases := []struct {
		name   string
		s      setup
		objs   []runtime.Object
		state  health.State
		want   []expect
		forbid []expect
	}{
		{"class selfManaged, Gateway without parameters, no proxy", setup{}, proxyWorld(proxySetup{classParams: "sm", noProxy: true}, sm),
			health.StateHealthy, []expect{selfManaged}, []expect{anyProxy}},
		{"class selfManaged, Gateway parameters kube", setup{}, proxyWorld(proxySetup{classParams: "sm", gwParams: "kube", noProxy: true}, sm, kube),
			health.StateHealthy, []expect{selfManaged}, []expect{anyProxy}},
		{"class kube, Gateway parameters selfManaged", setup{}, proxyWorld(proxySetup{classParams: "kube", gwParams: "sm", noProxy: true}, sm, kube),
			health.StateHealthy, []expect{selfManaged}, []expect{anyProxy}},
		{"class selfManaged, Gateway references missing parameters", setup{}, proxyWorld(proxySetup{classParams: "sm", gwParams: "nope", noProxy: true, gwConds: invalidParams}, sm),
			health.StateError, []expect{missing, {health.CodeConditionFalse, health.SeverityError, "infra/edge"}}, []expect{anySelf}},
		{"self-managed with a proxy Deployment that is not ready", setup{}, proxyWorld(proxySetup{classParams: "sm", proxyNotReady: true}, sm),
			health.StateError, []expect{{health.CodeProxyNotReady, health.SeverityError, "infra/edge"}}, []expect{anySelf}},
		{"self-managed and Programmed=False", setup{}, proxyWorld(proxySetup{classParams: "sm", noProxy: true, gwConds: notProgrammed}, sm),
			health.StateError, []expect{{health.CodeConditionFalse, health.SeverityError, "infra/edge"}, selfManaged}, nil},
		{"managed and Programmed=False", setup{}, proxyWorld(proxySetup{gwConds: notProgrammed}),
			health.StateError, []expect{{health.CodeConditionFalse, health.SeverityError, "infra/edge"}}, []expect{anySelf}},
		{"self-managed with an active xDS reject", setup{metrics: rejects}, proxyWorld(proxySetup{classParams: "sm", noProxy: true}, sm),
			health.StateError, []expect{{health.CodeXDSRejected, health.SeverityError, "infra/edge"}, selfManaged}, nil},
		{"managed with an active xDS reject", setup{metrics: rejects}, proxyWorld(proxySetup{}),
			health.StateError, []expect{{health.CodeXDSRejected, health.SeverityError, "infra/edge"}}, []expect{anySelf}},
		{"managed Gateway without a proxy", setup{}, proxyWorld(proxySetup{noProxy: true}),
			health.StateWarning, []expect{missing}, []expect{anySelf}},
		// kgateway does not check a class reference's group or kind; it looks up GatewayParameters by namespace and name.
		{"class reference of another group and kind still resolves", setup{}, proxyWorld(proxySetup{classParams: "sm", classRefKind: "OtherParameters", noProxy: true}, sm),
			health.StateHealthy, []expect{selfManaged}, []expect{anyProxy}},
		{"Gateway parameters of another kind", setup{}, proxyWorld(proxySetup{classParams: "sm", gwParams: "sm", gwRefKind: "OtherParameters", noProxy: true}, sm),
			health.StateWarning, []expect{missing}, []expect{anySelf}},
		{"GatewayParameters not readable", setup{prepare: forbidParams}, proxyWorld(proxySetup{classParams: "sm", noProxy: true}, sm),
			health.StateUnknown, []expect{missing, {health.CodeSignalUnavailable, health.SeverityInfo, ""}}, []expect{anySelf}},
		{"GatewayParameters not served", setup{served: noParamsKind}, proxyWorld(proxySetup{classParams: "sm", noProxy: true}),
			health.StateWarning, []expect{missing}, []expect{anySelf}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, tc.s, tc.objs...)
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
			if h.State != tc.state {
				t.Fatalf("state: want %s, got %s; findings: %+v", tc.state, h.State, h.Findings)
			}
			for _, w := range tc.want {
				if !hasSev(h, w.code, w.sev, w.res) {
					t.Fatalf("missing %s/%s on %s; findings: %+v", w.code, w.sev, w.res, h.Findings)
				}
			}
			for _, f := range tc.forbid {
				if hasSev(h, f.code, f.sev, f.res) {
					t.Fatalf("unexpected %s on %q; findings: %+v", f.code, f.res, h.Findings)
				}
			}
			for _, f := range h.Findings {
				if f.Code == health.CodeSelfManagedProxy && f.Message != selfManagedMsg {
					t.Errorf("KGWUI-010 message: %q", f.Message)
				}
			}
			// GatewayParameters are watched, never counted.
			if _, tile := count(get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", 200), "GatewayParameters"); tile {
				t.Error("GatewayParameters must not get a count tile")
			}
		})
	}
}

// D-017 §1: a workload with 0 desired replicas is not ready. A controller at
// zero is red (KGWUI-005); a managed proxy at zero is a warning (KGWUI-006).
func TestWorkloadsAtZeroReplicasAtTheAPI(t *testing.T) {
	// withoutBlog drops the unwatched blog/posts route, so the baseline is healthy.
	withoutBlog := func(objs []runtime.Object) []runtime.Object { return objs[: len(objs)-1 : len(objs)-1] }
	cases := []struct {
		name      string
		objs      []runtime.Object
		state     health.State
		want      *expect
		msg       string
		readiness overview.ReadinessState
	}{
		{"controller at zero", withoutBlog(replicaWorld(0, 0, 1, 1)), health.StateError,
			&expect{health.CodeControllerNotReady, health.SeverityError, "kgw/kgateway"}, "Controller scaled to 0 replicas", overview.ReadinessNotReady},
		{"proxy at zero", withoutBlog(replicaWorld(1, 1, 0, 0)), health.StateWarning,
			&expect{health.CodeProxyNotReady, health.SeverityWarning, "infra/edge"}, "Proxy scaled to 0 replicas", overview.ReadinessReady},
		{"proxy 1 of 2 ready stays an error", withoutBlog(replicaWorld(1, 1, 2, 1)), health.StateError,
			&expect{health.CodeProxyNotReady, health.SeverityError, "infra/edge"}, "Proxy not ready (1 of 2 replicas)", overview.ReadinessReady},
		{"1 of 1 ready stays green", withoutBlog(replicaWorld(1, 1, 1, 1)), health.StateHealthy, nil, "", overview.ReadinessReady},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw.kgateway"), tc.objs...)
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
			if h.State != tc.state {
				t.Fatalf("state: want %s, got %s; findings: %+v", tc.state, h.State, h.Findings)
			}
			if tc.want != nil && !hasSev(h, tc.want.code, tc.want.sev, tc.want.res) {
				t.Fatalf("missing %s/%s on %s; findings: %+v", tc.want.code, tc.want.sev, tc.want.res, h.Findings)
			}
			if tc.want != nil && h.Findings[0].Message != tc.msg {
				t.Errorf("message: want %q, got %q", tc.msg, h.Findings[0].Message)
			}
			if tc.want == nil && len(h.Findings) != 0 {
				t.Fatalf("want no findings, got %+v", h.Findings)
			}
			o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", 200)
			if o.Header.Readiness.State != tc.readiness {
				t.Fatalf("header readiness: want %s, got %+v", tc.readiness, o.Header.Readiness)
			}
			insts := get[[]InstallSummary](t, srv, "/api/v1/installs", 200)
			if len(insts) != 1 || insts[0].Readiness.State != tc.readiness {
				t.Fatalf("installs readiness: want %s, got %+v", tc.readiness, insts)
			}
		})
	}
}

// D-017 §2: owned ListenerSet status is required, like a Gateway's. kgateway
// writes some failures (rejectDeniedListenerSets) only on the ListenerSet, and
// writes no listener status for a rejected one.
func TestListenerSetStatusAtTheAPI(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	cond := testutil.Cond
	accepted := []metav1.Condition{cond("Accepted", metav1.ConditionTrue, "Accepted"), cond("Programmed", metav1.ConditionTrue, "Programmed")}
	denied := []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "NotAllowed"), cond("Programmed", metav1.ConditionFalse, "NotAllowed")}
	// A rejected ListenerSet has no listener status blocks.
	deniedLS := testutil.ListenerSet("infra", "ls1", gw, denied, testutil.Listener{Name: "extra"})
	badListener := testutil.ListenerSet("infra", "ls1", gw, []metav1.Condition{cond("Accepted", metav1.ConditionTrue, "ListenersNotValid"), cond("Programmed", metav1.ConditionTrue, "Programmed")},
		testutil.Listener{Name: "extra", Conds: []metav1.Condition{cond("Accepted", metav1.ConditionTrue, "Accepted"), cond("Programmed", metav1.ConditionFalse, "PortUnavailable")}})
	noListenerStatus := testutil.ListenerSet("infra", "ls1", gw, accepted, testutil.Listener{Name: "extra"})
	noStatus := func() *unstructured.Unstructured {
		u := testutil.ListenerSet("infra", "ls1", gw, nil)
		delete(u.Object, "status")
		return u
	}
	xDenied := testutil.ListenerSet("infra", "xls1", gw, denied, testutil.Listener{Name: "extra"})
	xDenied.SetAPIVersion("gateway.networking.x-k8s.io/v1alpha1")
	xDenied.SetKind("XListenerSet")
	ls1 := "infra/ls1"

	cases := []struct {
		name   string
		s      setup
		objs   []runtime.Object
		state  health.State
		want   []expect
		forbid []expect
	}{
		{"accepted ListenerSet stays green", setup{}, isolated(testutil.ListenerSet("infra", "ls1", gw, nil)), health.StateHealthy,
			nil, []expect{{health.CodeConditionFalse, "", ls1}, {health.CodeRouteUnattached, "", ls1}}},
		{"denied ListenerSet", setup{}, isolated(deniedLS), health.StateError,
			[]expect{{health.CodeConditionFalse, health.SeverityError, ls1}}, []expect{{health.CodeRouteUnattached, "", ls1}}},
		{"denied XListenerSet", setup{}, isolated(xDenied), health.StateError,
			[]expect{{health.CodeConditionFalse, health.SeverityError, "infra/xls1"}}, nil},
		{"listener not programmed", setup{}, isolated(badListener), health.StateError,
			[]expect{{health.CodeConditionFalse, health.SeverityError, ls1}}, nil},
		{"listener without a status block", setup{}, isolated(noListenerStatus), health.StateWarning,
			[]expect{{health.CodeRouteUnattached, health.SeverityWarning, ls1}}, []expect{{health.CodeConditionFalse, "", ls1}}},
		{"no status after grace", setup{}, isolated(noStatus()), health.StateWarning,
			[]expect{{health.CodeRouteUnattached, health.SeverityWarning, ls1}}, nil},
		{"no status inside grace", setup{late: []*unstructured.Unstructured{noStatus()}}, isolated(), health.StateUnknown,
			[]expect{{health.CodeRouteUnattached, health.SeverityInfo, ls1}}, []expect{{health.CodeRouteUnattached, health.SeverityWarning, ls1}}},
		{"stale denial is pending, not an error", setup{}, isolated(stale(testutil.ListenerSet("infra", "ls1", gw, denied, testutil.Listener{Name: "extra"}))), health.StateWarning,
			[]expect{{health.CodeRouteUnattached, health.SeverityWarning, ls1}}, []expect{{health.CodeConditionFalse, "", ls1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, tc.s, tc.objs...)
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
			if h.State != tc.state {
				t.Fatalf("state: want %s, got %s; findings: %+v", tc.state, h.State, h.Findings)
			}
			for _, w := range tc.want {
				if !hasSev(h, w.code, w.sev, w.res) {
					t.Fatalf("missing %s/%s on %s; findings: %+v", w.code, w.sev, w.res, h.Findings)
				}
			}
			for _, f := range tc.forbid {
				if hasSev(h, f.code, f.sev, f.res) {
					t.Fatalf("unexpected %s/%s on %s; findings: %+v", f.code, f.sev, f.res, h.Findings)
				}
			}
			// The ListenerSet is owned, so a green result is not vacuous.
			o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", 200)
			ls, _ := count(o, "ListenerSet")
			xls, _ := count(o, "XListenerSet")
			if ls+xls != 1 {
				t.Fatalf("want one owned ListenerSet, got counts %+v", o.Counts)
			}
		})
	}
}

// D-017 §3: during a missing proxy's grace period the required proxy check is
// pending, not OK, so the install is unknown; after grace it is KGWUI-006.
func TestMissingProxyInGraceIsPendingAtTheAPI(t *testing.T) {
	young := func() *unstructured.Unstructured { return testutil.Gateway("infra", "young", "kgateway", nil) }
	check := func(t *testing.T, h HealthResponse, name string) health.CheckResult {
		t.Helper()
		for _, c := range h.Checks {
			if c.Name == name {
				return c
			}
		}
		t.Fatalf("no %q check: %+v", name, h.Checks)
		return health.CheckResult{}
	}
	cases := []struct {
		name    string
		s       setup
		objs    []runtime.Object
		state   health.State
		pending bool
		want    []expect
		forbid  []expect
	}{
		{"managed, inside grace", setup{late: []*unstructured.Unstructured{young()}}, isolated(), health.StateUnknown, true,
			nil, []expect{{health.CodeProxyNotReady, "", ""}}},
		{"managed, after grace", setup{}, isolated(young()), health.StateWarning, false,
			[]expect{{health.CodeProxyNotReady, health.SeverityWarning, "infra/young"}}, nil},
		{"self-managed, inside grace", setup{late: []*unstructured.Unstructured{young()}}, proxyWorld(proxySetup{classParams: "sm"}, testutil.GatewayParameters("infra", "sm", true)),
			health.StateHealthy, false, []expect{{health.CodeSelfManagedProxy, health.SeverityInfo, "infra/young"}}, []expect{{health.CodeProxyNotReady, "", ""}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, tc.s, tc.objs...)
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
			if h.State != tc.state {
				t.Fatalf("state: want %s, got %s; findings: %+v; checks: %+v", tc.state, h.State, h.Findings, h.Checks)
			}
			if c := check(t, h, "gateway proxies"); c.Pending != tc.pending || c.OK == tc.pending || !c.Required {
				t.Fatalf("proxy check: want pending=%v, got %+v", tc.pending, c)
			}
			// The young Gateway's own status is current, so only the proxy check can hold the install back.
			if c := check(t, h, "resource status"); !c.OK {
				t.Fatalf("resource status must be OK: %+v", c)
			}
			for _, w := range tc.want {
				if !hasSev(h, w.code, w.sev, w.res) {
					t.Fatalf("missing %s/%s on %s; findings: %+v", w.code, w.sev, w.res, h.Findings)
				}
			}
			for _, f := range tc.forbid {
				if hasSev(h, f.code, f.sev, f.res) {
					t.Fatalf("unexpected %s/%s on %s; findings: %+v", f.code, f.sev, f.res, h.Findings)
				}
			}
		})
	}
}
