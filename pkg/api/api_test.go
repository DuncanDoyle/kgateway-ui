package api

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
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
	// zone, when set, is the zone the engine clock reports its times in.
	zone *time.Location
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
	// The fakes serve every upstream kind, at the version s.served picks for it.
	fakeServed := testutil.ServedUpstream()
	for i, f := range fakeServed {
		for _, k := range s.served {
			if k.Group == f.Group && k.Kind == f.Kind {
				fakeServed[i].Version = k.Version
			}
		}
	}
	dyn, kube := testutil.ClientsFor(t, fakeServed, objs...)
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
	clk := &clock{}
	clk.set(testutil.Now.Add(-health.Grace - time.Second))
	now := func() time.Time {
		if s.zone != nil {
			return clk.now().In(s.zone)
		}
		return clk.now()
	}
	// The cache stamps snapshots on the same clock, so snapshot times are pinned too.
	c := cache.New(cache.Config{Dynamic: dyn, Kube: kube, Served: s.served, PodSelectors: podSel, DeploymentSelectors: depSel, Now: now})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Start(ctx)
	e := engine.New(engine.Config{Snapshots: c, Served: s.served, Detectors: dets, Scope: install.NamespaceSelectorScope{},
		Verifier: scopeverify.NewManagedCountVerifier(persist), Metrics: s.metrics, Checks: health.Upstream(), Pinned: s.pinned,
		MetricsMaxAge: 30 * time.Second, Now: now, Platform: s.platform, Logger: s.logger})
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

// The resource endpoint reports the model's collection time in a Collected-At
// header (D-017 §6); the body stays the raw object.
func TestResourceEndpointReportsCollectedAt(t *testing.T) {
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw.kgateway"), world()...)
	resp, err := http.Get(srv.URL + "/api/v1/installs/kgw.kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/cart")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	raw := resp.Header.Get("Collected-At")
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatalf("Collected-At %q is not RFC 3339: %v", raw, err)
	}
	if !strings.HasSuffix(raw, "Z") || !at.Equal(testutil.Now) {
		t.Errorf("Collected-At = %q, want the model's computation time %s in UTC", raw, testutil.Now.UTC().Format(time.RFC3339Nano))
	}
	if o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", 200); !o.CollectedAt.Equal(at) {
		t.Errorf("Collected-At %s differs from the overview's collectedAt %s", at, o.CollectedAt)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	keys := slices.Sorted(maps.Keys(body))
	if want := []string{"apiVersion", "kind", "metadata", "spec", "status"}; !slices.Equal(keys, want) {
		t.Errorf("body keys = %v, want the raw object's %v", keys, want)
	}
	nf, err := http.Get(srv.URL + "/api/v1/installs/kgw.kgateway/resources/-/Secret/shop/x")
	if err != nil {
		t.Fatal(err)
	}
	_ = nf.Body.Close()
	if h := nf.Header.Get("Collected-At"); nf.StatusCode != http.StatusNotFound || h != "" {
		t.Errorf("404: status %d, Collected-At %q; want 404 without the header", nf.StatusCode, h)
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
		if f.Code != w.Code || f.Severity != w.Severity || (f.Resource == nil) != (w.Resource == nil) || (f.Resource != nil && f.Resource.Ref != w.Resource.Ref) {
			t.Fatalf("finding %d: order, code, severity or resource changed: %+v vs %+v", i, f, w)
		}
		if f.Resource != nil && denied(f.Resource.Ref) {
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

func (s staticModel) Current() *engine.Published {
	if s.m == nil {
		return nil
	}
	return &engine.Published{Model: s.m}
}

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
	withoutBlog := func(objs []runtime.Object) []runtime.Object {
		return slices.DeleteFunc(objs, func(o runtime.Object) bool {
			u, ok := o.(*unstructured.Unstructured)
			return ok && u.GetNamespace() == "blog" && u.GetName() == "posts"
		})
	}
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
			&expect{health.CodeProxyNotReady, health.SeverityWarning, "infra/edge"}, "Proxy has 0 replicas", overview.ReadinessReady},
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
			if tc.want != nil {
				i := slices.IndexFunc(h.Findings, func(f FindingDTO) bool {
					return f.Code == tc.want.code && f.Resource != nil && f.Resource.String() == tc.want.res
				})
				if i < 0 || h.Findings[i].Message != tc.msg {
					t.Errorf("message: want %q; findings: %+v", tc.msg, h.Findings)
				}
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
	notProgrammed := testutil.ListenerSet("infra", "ls1", gw, []metav1.Condition{cond("Accepted", metav1.ConditionTrue, "Accepted"), cond("Programmed", metav1.ConditionFalse, "Invalid")})
	// "No valid listeners": rejected, but kgateway still writes listener status.
	noValid := testutil.ListenerSet("infra", "ls1", gw, []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "ListenersNotValid"), cond("Programmed", metav1.ConditionFalse, "ListenersNotValid")},
		testutil.Listener{Name: "extra", Conds: []metav1.Condition{cond("Accepted", metav1.ConditionTrue, "Accepted"), cond("Programmed", metav1.ConditionFalse, "PortUnavailable")}})
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
		detail string // a finding on ls1 must have a detail containing this
	}{
		{"accepted ListenerSet stays green", setup{}, isolated(testutil.ListenerSet("infra", "ls1", gw, nil)), health.StateHealthy,
			nil, []expect{{health.CodeConditionFalse, "", ls1}, {health.CodeRouteUnattached, "", ls1}}, ""},
		{"denied ListenerSet", setup{}, isolated(deniedLS), health.StateError,
			[]expect{{health.CodeConditionFalse, health.SeverityError, ls1}}, []expect{{health.CodeRouteUnattached, "", ls1}}, ""},
		{"denied XListenerSet", setup{}, isolated(xDenied), health.StateError,
			[]expect{{health.CodeConditionFalse, health.SeverityError, "infra/xls1"}}, nil, ""},
		{"listener not programmed", setup{}, isolated(badListener), health.StateError,
			[]expect{{health.CodeConditionFalse, health.SeverityError, ls1}}, nil, ""},
		{"listener without a status block", setup{}, isolated(noListenerStatus), health.StateWarning,
			[]expect{{health.CodeRouteUnattached, health.SeverityWarning, ls1}}, []expect{{health.CodeConditionFalse, "", ls1}}, ""},
		{"no status after grace", setup{}, isolated(noStatus()), health.StateWarning,
			[]expect{{health.CodeRouteUnattached, health.SeverityWarning, ls1}}, nil, ""},
		{"no status inside grace", setup{late: []*unstructured.Unstructured{noStatus()}}, isolated(), health.StateUnknown,
			[]expect{{health.CodeRouteUnattached, health.SeverityInfo, ls1}}, []expect{{health.CodeRouteUnattached, health.SeverityWarning, ls1}}, ""},
		{"Programmed=False with Accepted=True", setup{}, isolated(notProgrammed), health.StateError,
			[]expect{{health.CodeConditionFalse, health.SeverityError, ls1}}, nil, "Programmed=False"},
		{"no valid listeners keeps listener detail", setup{}, isolated(noValid), health.StateError,
			[]expect{{health.CodeConditionFalse, health.SeverityError, ls1}}, []expect{{health.CodeRouteUnattached, "", ls1}}, "listener extra"},
		{"stale denial is pending, not an error", setup{}, isolated(stale(testutil.ListenerSet("infra", "ls1", gw, denied, testutil.Listener{Name: "extra"}))), health.StateWarning,
			[]expect{{health.CodeRouteUnattached, health.SeverityWarning, ls1}}, []expect{{health.CodeConditionFalse, "", ls1}}, ""},
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
			if tc.detail != "" && !slices.ContainsFunc(h.Findings, func(f FindingDTO) bool {
				return f.Resource != nil && f.Resource.String() == ls1 && strings.Contains(f.Detail, tc.detail)
			}) {
				t.Fatalf("no finding on %s with detail %q; findings: %+v", ls1, tc.detail, h.Findings)
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
	ctrlDown := slices.DeleteFunc(worldWith(0, 1), func(o runtime.Object) bool {
		u, ok := o.(*unstructured.Unstructured)
		return ok && u.GetNamespace() == "blog"
	})
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
		// Pending never masks red: the roll-up checks errors first.
		{"managed, inside grace, controller not ready", setup{late: []*unstructured.Unstructured{young()}}, ctrlDown, health.StateError, true,
			[]expect{{health.CodeControllerNotReady, health.SeverityError, "kgw/kgateway"}}, []expect{{health.CodeProxyNotReady, "", ""}}},
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
			} else if tc.pending && c.Error != "1 proxy not found yet" {
				t.Fatalf("proxy check error: want %q, got %q", "1 proxy not found yet", c.Error)
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

// tcpRoute is an HTTPRoute fixture as a TCPRoute of version.
func tcpRoute(version, ns, name string, parents []objects.ParentRef, status ...testutil.RouteParent) *unstructured.Unstructured {
	u := testutil.HTTPRoute(ns, name, parents, status...)
	u.SetAPIVersion("gateway.networking.k8s.io/" + version)
	u.SetKind("TCPRoute")
	return u
}

// D-018 §1–2: an experimental-only object (XListenerSet, pre-v1 TCPRoute and
// TLSRoute) without status follows the normal rule: pending within grace, a
// KGWUI-003 warning after it, with a hint in the detail that the controller's
// experimental features may be off. Status only from another controller is
// still not judged.
func TestExperimentalOnlyKindsAtTheAPI(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	cond := testutil.Cond
	denied := []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "NotAllowed"), cond("Programmed", metav1.ConditionFalse, "NotAllowed")}
	xls := func(cs []metav1.Condition, ls ...testutil.Listener) *unstructured.Unstructured {
		u := testutil.ListenerSet("infra", "xls1", gw, cs, ls...)
		u.SetAPIVersion("gateway.networking.x-k8s.io/v1alpha1")
		u.SetKind("XListenerSet")
		return u
	}
	bare := func() *unstructured.Unstructured {
		u := xls(nil)
		delete(u.Object, "status")
		return u
	}
	preV1 := slices.Clone(testutil.ServedUpstream())
	for i := range preV1 {
		switch preV1[i].Kind {
		case "TCPRoute":
			preV1[i].Version = "v1alpha2"
		case "TLSRoute":
			preV1[i].Version = "v1alpha3"
		}
	}
	rejected := testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners"), cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}}
	// Status written only by another controller does not show this one watches the kind.
	foreign := rejected
	foreign.Controller = "example.com/other-controller"
	tlsRoute := func(status ...testutil.RouteParent) *unstructured.Unstructured {
		u := tcpRoute("v1alpha3", "shop", "tls", []objects.ParentRef{gw}, status...)
		u.SetKind("TLSRoute")
		return u
	}
	anyFinding := func(res string) []expect {
		return []expect{{health.CodeRouteUnattached, "", res}, {health.CodeConditionFalse, "", res}}
	}
	hint := func(kindVersion string) string { return fmt.Sprintf(health.ExperimentalHint, kindVersion) }
	warn := func(res string) []expect { return []expect{{health.CodeRouteUnattached, health.SeverityWarning, res}} }
	info := func(res string) []expect { return []expect{{health.CodeRouteUnattached, health.SeverityInfo, res}} }

	cases := []struct {
		name   string
		s      setup
		objs   []runtime.Object
		state  health.State
		want   []expect
		forbid []expect
		hint   string // the hint the KGWUI-003 detail must end with; "" means no hint on any finding
		orphan bool   // a route whose parent does not exist is not counted
	}{
		{"XListenerSet without status", setup{}, isolated(bare()), health.StateWarning, warn("infra/xls1"), nil, hint("XListenerSet v1alpha1"), false},
		{"XListenerSet without status inside grace", setup{late: []*unstructured.Unstructured{bare()}}, isolated(), health.StateUnknown,
			info("infra/xls1"), warn("infra/xls1"), hint("XListenerSet v1alpha1"), false},
		{"XListenerSet rejected by the controller", setup{}, isolated(xls(denied, testutil.Listener{Name: "extra"})), health.StateError,
			[]expect{{health.CodeConditionFalse, health.SeverityError, "infra/xls1"}}, nil, "", false},
		{"pre-v1 TCPRoute without status", setup{served: preV1}, isolated(tcpRoute("v1alpha2", "shop", "tcp", []objects.ParentRef{gw})), health.StateWarning,
			warn("shop/tcp"), nil, hint("TCPRoute v1alpha2"), false},
		{"pre-v1 TCPRoute without status inside grace", setup{served: preV1, late: []*unstructured.Unstructured{tcpRoute("v1alpha2", "shop", "tcp", []objects.ParentRef{gw})}}, isolated(), health.StateUnknown,
			info("shop/tcp"), warn("shop/tcp"), hint("TCPRoute v1alpha2"), false},
		{"pre-v1 TLSRoute without status", setup{served: preV1}, isolated(tlsRoute()), health.StateWarning, warn("shop/tls"), nil, hint("TLSRoute v1alpha3"), false},
		// "Parent not found" holds whether or not the controller watches the kind, so it carries no hint.
		{"pre-v1 TCPRoute with a missing parent and no status", setup{served: preV1}, isolated(tcpRoute("v1alpha2", "shop", "tcp", []objects.ParentRef{testutil.GwRef("infra", "gone")})), health.StateWarning,
			warn("shop/tcp"), nil, "", true},
		{"pre-v1 TCPRoute rejected by the controller", setup{served: preV1}, isolated(tcpRoute("v1alpha2", "shop", "tcp", []objects.ParentRef{gw}, rejected)), health.StateError,
			[]expect{{health.CodeConditionFalse, health.SeverityError, "shop/tcp"}}, nil, "", false},
		{"pre-v1 TCPRoute with status only from another controller", setup{served: preV1}, isolated(tcpRoute("v1alpha2", "shop", "tcp", []objects.ParentRef{gw}, foreign)), health.StateHealthy,
			nil, anyFinding("shop/tcp"), "", false},
		{"pre-v1 TLSRoute with status only from another controller", setup{served: preV1}, isolated(tlsRoute(foreign)), health.StateHealthy,
			nil, anyFinding("shop/tls"), "", false},
		{"v1 TCPRoute without status is still required", setup{}, isolated(tcpRoute("v1", "shop", "tcp", []objects.ParentRef{gw})), health.StateWarning,
			warn("shop/tcp"), nil, "", false},
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
			// The hint goes in the detail, never the short message.
			for _, f := range h.Findings {
				if strings.Contains(f.Message, "experimental") {
					t.Fatalf("hint in the message: %+v", f)
				}
				if tc.hint == "" && strings.Contains(f.Detail, "experimental") {
					t.Fatalf("unexpected hint: %+v", f)
				}
				if tc.hint != "" && f.Code == health.CodeRouteUnattached && !strings.HasSuffix(f.Detail, tc.hint) {
					t.Fatalf("detail %q lacks the hint %q", f.Detail, tc.hint)
				}
			}
			if tc.orphan {
				return
			}
			// The object is counted, so the result is about it.
			o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", 200)
			xn, _ := count(o, "XListenerSet")
			tn, _ := count(o, "TCPRoute")
			sn, _ := count(o, "TLSRoute")
			if xn+tn+sn != 1 {
				t.Fatalf("want the object counted once, got %+v", o.Counts)
			}
		})
	}
}

// envWorld is world with the controller env replaced by env. Without a Pod the
// scope comes from the Deployment template (D-015).
func envWorld(withPod bool, env ...corev1.EnvVar) []runtime.Object {
	d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, 1, env...)
	var out []runtime.Object
	for _, o := range world() {
		switch v := o.(type) {
		case *appsv1.Deployment:
			if v.Name == "kgateway" {
				o = d
			}
		case *corev1.Pod:
			if !withPod {
				continue
			}
			o = testutil.ControllerPod(d, "kgateway-0", true)
		}
		out = append(out, o)
	}
	return out
}

// Q12, D-009 §2: duplicate env entries resolve last-wins, as in Kubernetes;
// only a last entry using valueFrom makes the scope unknown.
func TestDuplicateScopeEnvIsLastWinsAtTheAPI(t *testing.T) {
	ref := corev1.EnvVar{Name: install.DiscoveryNamespaceSelectorsEnv, ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{Key: "k"}}}
	lit := testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"a"}}]`)
	cases := []struct {
		name    string
		withPod bool
		env     []corev1.EnvVar
		mode    install.ScopeMode
	}{
		{"pod: valueFrom then literal", true, []corev1.EnvVar{ref, lit}, install.ScopeSelectors},
		{"pod: literal then valueFrom", true, []corev1.EnvVar{lit, ref}, install.ScopeUnknown},
		{"template: valueFrom then literal", false, []corev1.EnvVar{ref, lit}, install.ScopeSelectors},
		{"template: literal then valueFrom", false, []corev1.EnvVar{lit, ref}, install.ScopeUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw.kgateway"), envWorld(tc.withPod, tc.env...)...)
			insts := get[[]InstallSummary](t, srv, "/api/v1/installs", 200)
			if len(insts) != 1 || insts[0].Scope == nil {
				t.Fatalf("installs: %+v", insts)
			}
			want := install.SourceDeploymentTemplate
			if tc.withPod {
				want = "pod kgateway-0"
			}
			if sc := insts[0].Scope; sc.Mode != string(tc.mode) || sc.Source != want {
				t.Fatalf("install scope: want %s from %q, got %+v", tc.mode, want, *sc)
			}
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
			if tc.mode == install.ScopeUnknown {
				if h.State != health.StateUnknown || !hasFinding(h, health.CodeSignalUnavailable, "") {
					t.Fatalf("unknown scope must force unknown health with KGWUI-008: %s %+v", h.State, h.Findings)
				}
				return
			}
			if insts[0].Scope.Watched != 2 {
				t.Errorf("watched: want 2 (infra, shop), got %+v", insts[0].Scope)
			}
			for _, c := range h.Checks {
				if c.Name == "controller scope" && !c.OK {
					t.Errorf("controller scope check must be OK: %+v", c)
				}
			}
		})
	}
}

// Q13, D-017 §4: KGWUI-007 compares stable kinds only. kgateway counts
// XListenerSet and pre-v1 TCPRoute/TLSRoute only with experimental features on.
func TestScopeVerifierSkipsExperimentalOnlyKindsAtTheAPI(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	xls := testutil.ListenerSet("infra", "xls1", gw, nil)
	xls.SetAPIVersion("gateway.networking.x-k8s.io/v1alpha1")
	xls.SetKind("XListenerSet")
	delete(xls.Object, "status")
	preV1 := slices.Clone(testutil.ServedUpstream())
	for i := range preV1 {
		if preV1[i].Kind == "TCPRoute" {
			preV1[i].Version = "v1alpha2"
		}
	}
	// managed is the gauge for isolated(): 2 Gateways in infra (any class), 1 HTTPRoute parentRef in shop.
	managed := func(extra map[ctrlmetrics.ManagedKey]float64) fakeMetrics {
		m := map[ctrlmetrics.ManagedKey]float64{{Namespace: "infra", Resource: "Gateway"}: 2, {Namespace: "shop", Resource: "HTTPRoute"}: 1}
		maps.Copy(m, extra)
		return fakeMetrics{"kgw.kgateway": {CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{
			Rejects: map[ctrlmetrics.GatewayKey]float64{}, Managed: m, ManagedPresent: true}}}
	}
	var noLS []kinds.Served
	for _, k := range testutil.ServedUpstream() {
		if k.Kind != "ListenerSet" {
			noLS = append(noLS, k)
		}
	}
	shopXLS := xls.DeepCopy()
	shopXLS.SetNamespace("shop")
	shopXLS.SetName("xls2")
	experimental := []runtime.Object{xls, tcpRoute("v1alpha2", "shop", "tcp", []objects.ParentRef{gw})}
	cases := []struct {
		name    string
		served  []kinds.Served
		metrics fakeMetrics
		objs    []runtime.Object
		want    string // the one mismatching "namespace, resource"; "" means no KGWUI-007
	}{
		{"matching stable counts", nil, managed(nil), isolated(), ""},
		{"experimental-only objects the gauge omits", preV1, managed(nil), isolated(experimental...), ""},
		{"experimental-only counts the UI does not match", preV1,
			managed(map[ctrlmetrics.ManagedKey]float64{{Namespace: "infra", Resource: "XListenerSet"}: 3, {Namespace: "shop", Resource: "TCPRoute"}: 4}), isolated(experimental...), ""},
		{"stable mismatch alongside experimental-only objects", preV1,
			managed(map[ctrlmetrics.ManagedKey]float64{{Namespace: "shop", Resource: "HTTPRoute"}: 5}), isolated(experimental...), "namespace shop, HTTPRoute:"},
		{"v1 TCPRoute the gauge omits", nil, managed(nil), isolated(tcpRoute("v1", "shop", "tcp", []objects.ParentRef{gw})), "namespace shop, TCPRoute:"},
		{"ListenerSet the gauge omits, no XListenerSet beside it", nil, managed(nil), isolated(testutil.ListenerSet("infra", "ls1", gw, nil)), "namespace infra, XListenerSet:"},
		// Design §4.4: ListenerSet shares the XListenerSet label, so an XListenerSet beside it skips the pair.
		{"ListenerSet beside an XListenerSet, gauge omits both", nil, managed(nil), isolated(testutil.ListenerSet("infra", "ls1", gw, nil), xls), ""},
		{"ListenerSet beside an XListenerSet, gauge counts both", nil,
			managed(map[ctrlmetrics.ManagedKey]float64{{Namespace: "infra", Resource: "XListenerSet"}: 2}), isolated(testutil.ListenerSet("infra", "ls1", gw, nil), xls), ""},
		{"ListenerSet beside an XListenerSet in another namespace", nil, managed(nil),
			isolated(testutil.ListenerSet("infra", "ls1", gw, nil), shopXLS), "namespace infra, XListenerSet:"},
		// D-018 §4: experimental-only series are left out on the metrics side too, in any namespace.
		{"pre-v1 TCPRoute counted outside the UI's scope", preV1,
			managed(map[ctrlmetrics.ManagedKey]float64{{Namespace: "blog", Resource: "TCPRoute"}: 1}), isolated(experimental...), ""},
		{"stable kind counted outside the UI's scope", preV1,
			managed(map[ctrlmetrics.ManagedKey]float64{{Namespace: "blog", Resource: "HTTPRoute"}: 1}), isolated(experimental...), "namespace blog, HTTPRoute:"},
		{"XListenerSet label counted outside the UI's scope, no ListenerSet served", noLS,
			managed(map[ctrlmetrics.ManagedKey]float64{{Namespace: "blog", Resource: "XListenerSet"}: 1}), isolated(xls), ""},
		// The label also counts stable ListenerSets, so only namespaces holding an XListenerSet are skipped (design §4.4).
		{"XListenerSet label counted outside the UI's scope, ListenerSet served", nil,
			managed(map[ctrlmetrics.ManagedKey]float64{{Namespace: "blog", Resource: "XListenerSet"}: 1}), isolated(xls), "namespace blog, XListenerSet:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, srv, _ := startWith(t, setup{served: tc.served, metrics: tc.metrics, immediate: true}, tc.objs...)
			e.Compute() // a mismatch must persist across two computations
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
			var got []string
			for _, f := range h.Findings {
				if f.Code == health.CodeScopeMismatch {
					got = append(got, f.Detail)
				}
			}
			if tc.want == "" && len(got) > 0 || tc.want != "" && (len(got) != 1 || !strings.HasPrefix(got[0], tc.want)) {
				t.Fatalf("KGWUI-007: want %q, got %q", tc.want, got)
			}
		})
	}
}

// withControllerVersion is objs with the controller Deployment's version label
// set to v; the upstream detector reads the install version from it.
func withControllerVersion(v string, objs []runtime.Object) []runtime.Object {
	out := slices.Clone(objs)
	for i, o := range out {
		if d, ok := o.(*appsv1.Deployment); ok && d.Name == "kgateway" {
			d = d.DeepCopy()
			d.Labels["app.kubernetes.io/version"] = v
			out[i] = d
		}
	}
	return out
}

// D-018 §3: which kinds are experimental-only depends on the controller
// version. kgateway v2.3 watches no TCPRoute without experimental features,
// v2.4 and later watch v1 TCPRoute always; an unknown version is conservative.
func TestExperimentalOnlyDependsOnControllerVersionAtTheAPI(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	// The gauge for isolated(): 2 Gateways in infra, 1 HTTPRoute parentRef in shop; no TCPRoute.
	gauge := fakeMetrics{"kgw.kgateway": {CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{},
		Managed: map[ctrlmetrics.ManagedKey]float64{{Namespace: "infra", Resource: "Gateway"}: 2, {Namespace: "shop", Resource: "HTTPRoute"}: 1}, ManagedPresent: true}}}
	hint := fmt.Sprintf(health.ExperimentalHint, "TCPRoute v1")
	cases := []struct {
		version      string
		experimental bool // hint on the warning; left out of KGWUI-007
	}{
		{"2.3.11", true},
		{"2.4.7", false},
		{"2.5.0", false},
		{"dev", true},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			objs := withControllerVersion(tc.version, isolated(tcpRoute("v1", "shop", "tcp", []objects.ParentRef{gw})))
			e, srv, _ := startWith(t, setup{metrics: gauge, immediate: true}, objs...)
			e.Compute() // a mismatch must persist across two computations
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
			var pending *FindingDTO
			mismatch := false
			for i, f := range h.Findings {
				if f.Code == health.CodeRouteUnattached && f.Resource != nil && f.Resource.String() == "shop/tcp" {
					pending = &h.Findings[i]
				}
				if f.Code == health.CodeScopeMismatch && strings.HasPrefix(f.Detail, "namespace shop, TCPRoute:") {
					mismatch = true
				}
			}
			if pending == nil || pending.Severity != health.SeverityWarning {
				t.Fatalf("want a KGWUI-003 warning on shop/tcp; findings: %+v", h.Findings)
			}
			if got := strings.HasSuffix(pending.Detail, hint); got != tc.experimental {
				t.Errorf("hint: want %v, got detail %q", tc.experimental, pending.Detail)
			}
			if mismatch == tc.experimental {
				t.Errorf("KGWUI-007 for TCPRoute: want %v; findings: %+v", !tc.experimental, h.Findings)
			}
		})
	}
}

// D-009 §2: the selector env may come from an envFrom ConfigMap/Secret the UI
// does not read, so envFrom without an explicit env entry makes the scope
// unknown; an explicit entry still wins because env is applied after envFrom.
func TestScopeEnvFromIsUnknownUnlessExplicitAtTheAPI(t *testing.T) {
	lit := testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"a"}}]`)
	from := []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "kgw-env"}}}}
	cases := []struct {
		name    string
		withPod bool
		env     []corev1.EnvVar
		mode    install.ScopeMode
	}{
		{"pod: envFrom only", true, nil, install.ScopeUnknown},
		{"pod: envFrom and explicit env", true, []corev1.EnvVar{lit}, install.ScopeSelectors},
		{"template: envFrom only", false, nil, install.ScopeUnknown},
		{"template: envFrom and explicit env", false, []corev1.EnvVar{lit}, install.ScopeSelectors},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			objs := envWorld(tc.withPod, tc.env...)
			for _, o := range objs {
				var spec *corev1.PodSpec
				switch v := o.(type) {
				case *appsv1.Deployment:
					spec = &v.Spec.Template.Spec
				case *corev1.Pod:
					spec = &v.Spec
				}
				if spec != nil {
					for i := range spec.Containers {
						spec.Containers[i].EnvFrom = from
					}
				}
			}
			_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw.kgateway"), objs...)
			insts := get[[]InstallSummary](t, srv, "/api/v1/installs", 200)
			if len(insts) != 1 || insts[0].Scope == nil {
				t.Fatalf("installs: %+v", insts)
			}
			if sc := insts[0].Scope; sc.Mode != string(tc.mode) {
				t.Fatalf("install scope: want %s, got %+v", tc.mode, *sc)
			}
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
			unknown := h.State == health.StateUnknown && hasFinding(h, health.CodeSignalUnavailable, "")
			if (tc.mode == install.ScopeUnknown) != unknown {
				t.Fatalf("unknown scope must force unknown health with KGWUI-008, and only then: %s %+v", h.State, h.Findings)
			}
		})
	}
}

// Every time on the wire is UTC, though internal clocks keep their zone and
// monotonic reading. The harness stamps snapshots in the engine's zone too.
func TestWireTimesAreUTCAtTheAPI(t *testing.T) {
	cest := time.FixedZone("CEST", 2*3600)
	metrics := okMetrics("kgw.kgateway")
	r := metrics["kgw.kgateway"]
	r.CollectedAt = r.CollectedAt.In(cest)
	metrics["kgw.kgateway"] = r
	gw := testutil.GwRef("infra", "edge")
	// A rejected route gives a finding with a since time.
	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	_, srv, _ := startWith(t, setup{zone: cest, metrics: metrics}, world(broken)...)
	var n int
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, c := range v {
				if s, ok := c.(string); ok && (k == "computedAt" || k == "confirmedAt" || k == "collectedAt" || k == "since") {
					n++
					if at, err := time.Parse(time.RFC3339Nano, s); err != nil || s != at.UTC().Format(time.RFC3339Nano) {
						t.Errorf("%s.%s: want a UTC RFC 3339 time, got %q", path, k, s)
					}
				}
				walk(path+"."+k, c)
			}
		case []any:
			for i, c := range v {
				walk(fmt.Sprintf("%s[%d]", path, i), c)
			}
		}
	}
	for _, p := range []string{"/api/v1/installs", "/api/v1/installs/kgw.kgateway/overview", "/api/v1/installs/kgw.kgateway/health"} {
		walk(p, get[any](t, srv, p, 200))
	}
	if n < 5 {
		t.Fatalf("want computedAt, collectedAt, checks and since times on the wire, saw %d", n)
	}
	resp, err := http.Get(srv.URL + "/api/v1/installs/kgw.kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/broken")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if h := resp.Header.Get("Collected-At"); !strings.HasSuffix(h, "Z") {
		t.Errorf("Collected-At: want UTC, got %q", h)
	}
}

// confirmedAt serves the engine's publication as confirmed at a later time,
// as an idle confirm tick would.
type confirmedAt struct {
	*engine.Engine
	at time.Time
}

func (c confirmedAt) Current() *engine.Published {
	p := *c.Engine.Current()
	p.ConfirmedAt = c.at
	return &p
}

// Each timestamp keeps its own meaning (design §3.1): computedAt is the model's,
// confirmedAt, collectedAt and Collected-At are the confirmation time, and a
// check's collectedAt and a finding's since keep theirs. All are UTC on the wire.
func TestTimestampsPerMeaningAtTheAPI(t *testing.T) {
	ams, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}
	gw := testutil.GwRef("infra", "edge")
	denied := testutil.HTTPRoute("shop", "denied", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "RefNotPermitted")}})
	h := newHarness(t, setup{zone: ams}, world(denied)...)
	t0, confirmed := testutil.Now.UTC().Format(time.RFC3339Nano), testutil.Now.Add(5*time.Second)
	want := confirmed.UTC().Format(time.RFC3339Nano)
	srv := httptest.NewServer(NewHandler(confirmedAt{h.e, confirmed.In(ams)}, fstest.MapFS{}, nil))
	t.Cleanup(srv.Close)

	inst := get[[]map[string]any](t, srv, "/api/v1/installs", http.StatusOK)
	if len(inst) != 1 || inst[0]["computedAt"] != t0 || inst[0]["confirmedAt"] != want {
		t.Errorf("installs: computedAt %v (want %s), confirmedAt %v (want %s)", inst[0]["computedAt"], t0, inst[0]["confirmedAt"], want)
	}
	if ov := get[map[string]any](t, srv, "/api/v1/installs/kgw.kgateway/overview", http.StatusOK); ov["collectedAt"] != want {
		t.Errorf("overview collectedAt %v, want %s", ov["collectedAt"], want)
	}
	rep := get[map[string]any](t, srv, "/api/v1/installs/kgw.kgateway/health", http.StatusOK)
	if rep["collectedAt"] != want {
		t.Errorf("health collectedAt %v, want %s", rep["collectedAt"], want)
	}
	for _, c := range rep["checks"].([]any) {
		if c := c.(map[string]any); c["collectedAt"] != t0 {
			t.Errorf("check %v collectedAt %v, want its own %s", c["name"], c["collectedAt"], t0)
		}
	}
	var since []string
	for _, f := range rep["findings"].([]any) {
		if s, ok := f.(map[string]any)["since"]; ok {
			since = append(since, s.(string))
		}
	}
	if !slices.Contains(since, "2026-10-17T11:00:00Z") || slices.Contains(since, want) {
		t.Errorf("findings since %v: want their own times", since)
	}
	resp, err := http.Get(srv.URL + "/api/v1/installs/kgw.kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/denied")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := resp.Header.Get("Collected-At"); got != want {
		t.Errorf("Collected-At %q, want %s", got, want)
	}
}

// countingSource counts Current calls.
type countingSource struct {
	ModelSource
	n atomic.Int32
}

func (c *countingSource) Current() *engine.Published {
	c.n.Add(1)
	return c.ModelSource.Current()
}

// A request loads the publication once, so its model and confirmation time come from one value.
func TestRequestReadsOnePublished(t *testing.T) {
	e, _, _ := start(t, nil, "", nil, world()...)
	src := &countingSource{ModelSource: e}
	srv := httptest.NewServer(NewHandler(src, fstest.MapFS{}, nil))
	t.Cleanup(srv.Close)
	for _, p := range []string{"/readyz", "/api/v1/meta", "/api/v1/installs", "/api/v1/installs/kgw.kgateway/overview",
		"/api/v1/installs/kgw.kgateway/health", "/api/v1/installs/kgw.kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/cart",
		"/api/v1/installs/nope/health"} {
		src.n.Store(0)
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if n := src.n.Load(); n != 1 {
			t.Errorf("GET %s read Current %d times, want 1", p, n)
		}
	}
}

// A publication without a confirmation time omits confirmedAt, so clients fall back to computedAt.
func TestZeroConfirmedAtIsOmitted(t *testing.T) {
	e, _, _ := start(t, nil, "", nil, world()...)
	srv := httptest.NewServer(NewHandler(confirmedAt{e, time.Time{}}, fstest.MapFS{}, nil))
	t.Cleanup(srv.Close)
	inst := get[[]map[string]any](t, srv, "/api/v1/installs", http.StatusOK)
	if _, ok := inst[0]["confirmedAt"]; ok || inst[0]["computedAt"] == nil {
		t.Errorf("zero ConfirmedAt: confirmedAt %v, computedAt %v", inst[0]["confirmedAt"], inst[0]["computedAt"])
	}
}

// checkPending reports whether h's named check is pending.
func checkPending(h HealthResponse, name string) bool {
	for _, c := range h.Checks {
		if c.Name == name {
			return c.Pending
		}
	}
	return false
}

// A child naming an existing HTTPRoute is not "parent not found"; a child
// naming a missing one is.
func TestExistingHTTPRouteParentIsNotMissing(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	_, srv, _ := startWith(t, setup{}, isolated(
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.HTTPRoute("shop", "named", []objects.ParentRef{testutil.HTTPRouteRef("shop", "api")}),
		testutil.HTTPRoute("shop", "lost", []objects.ParentRef{testutil.HTTPRouteRef("shop", "gone")}),
	)...)
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
	if hasFinding(h, health.CodeRouteUnattached, "shop/named") {
		t.Errorf("existing HTTPRoute parent reported missing: %+v", h.Findings)
	}
	if !hasSev(h, health.CodeRouteUnattached, health.SeverityWarning, "shop/lost") {
		t.Errorf("missing HTTPRoute parent not reported: %+v", h.Findings)
	}
}

// A child edited so that its entry describes the previous generation keeps
// its edge: it stays owned and counted, and Phase 1 freshness judges it
// (design §4.2; pre-flight PF16, a known limitation after a child-edit drop).
func TestStaleEligibleEntryKeepsEdge(t *testing.T) {
	gw, api := testutil.GwRef("infra", "edge"), testutil.HTTPRouteRef("shop", "api")
	parent := func() *unstructured.Unstructured {
		return testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller))
	}
	child := func() *unstructured.Unstructured {
		return testutil.HTTPRoute("shop", "child", []objects.ParentRef{api}, testutil.AcceptedBy(api, testutil.Controller))
	}
	cases := []struct {
		name    string
		s       setup
		objs    []runtime.Object
		state   health.State
		sev     health.Severity // KGWUI-003 on the child; "" for none
		pending bool
	}{
		{"current entry", setup{}, isolated(parent(), child()), health.StateHealthy, "", false},
		{"edited, inside grace", setup{late: []*unstructured.Unstructured{stale(child())}}, isolated(parent()), health.StateUnknown, health.SeverityInfo, true},
		{"edited, after grace", setup{}, isolated(parent(), stale(child())), health.StateWarning, health.SeverityWarning, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, tc.s, tc.objs...)
			o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", 200)
			if n, _ := count(o, "HTTPRoute"); n != 3 {
				t.Fatalf("HTTPRoute count %d, want 3 (cart, api, child) before and after the edit", n)
			}
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
			if h.State != tc.state {
				t.Fatalf("state %s, want %s; findings %+v", h.State, tc.state, h.Findings)
			}
			if got := hasFinding(h, health.CodeRouteUnattached, "shop/child"); got != (tc.sev != "") || (got && !hasSev(h, health.CodeRouteUnattached, tc.sev, "shop/child")) {
				t.Fatalf("KGWUI-003 on the child: want %q; findings %+v", tc.sev, h.Findings)
			}
			if checkPending(h, "resource status") != tc.pending {
				t.Fatalf("resource status pending: want %v; checks %+v", tc.pending, h.Checks)
			}
			if hasFinding(h, "KGWUI-011", "") {
				t.Fatalf("a stale eligible entry is not KGWUI-011: %+v", h.Findings)
			}
		})
	}
}

// Q12 changes Phase 1's numbers on clusters with delegation. Each object
// notes its Phase 1 outcome.
func TestQ12CountsBeforeAndAfter(t *testing.T) {
	cond := testutil.Cond
	gw, gwB := testutil.GwRef("infra", "edge"), testutil.GwRef("infra-b", "edge")
	api, api2, pb := testutil.HTTPRouteRef("shop", "api"), testutil.HTTPRouteRef("shop", "api2"), testutil.HTTPRouteRef("infra-b", "pb")
	ok := func(p objects.ParentRef) testutil.RouteParent { return testutil.AcceptedBy(p, testutil.Controller) }
	rejected := func(p objects.ParentRef) testutil.RouteParent {
		return testutil.RouteParent{Ref: p, Controller: testutil.Controller, Conds: []metav1.Condition{
			cond("Accepted", metav1.ConditionFalse, "UnsupportedValue"), cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}}
	}
	// kgw2 shares the controller name and watches team=b or env=b.
	d2 := testutil.ControllerDeployment("kgw2", "kgateway2", img, 1, 1,
		testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"b"}},{"matchLabels":{"env":"b"}}]`))
	objs := isolated(testutil.Namespace("kgw2", nil), testutil.Namespace("infra-b", map[string]string{"team": "b"}),
		testutil.Namespace("shared", map[string]string{"team": "a", "env": "b"}), testutil.Namespace("leaving", map[string]string{"team": "a"}),
		d2, testutil.ControllerPod(d2, "kgateway2-0", true), testutil.Gateway("infra-b", "edge", "kgateway", nil), testutil.ProxyDeployment("infra-b", "edge", 1, 1),
		// Phase 1: owned (kgw), unchanged.
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, ok(gw)),
		testutil.HTTPRoute("shop", "api2", []objects.ParentRef{gw}, ok(gw)),
		testutil.HTTPRoute("infra-b", "pb", []objects.ParentRef{gwB}, ok(gwB)),
		// Phase 1: owned through the Gateway; its HTTPRoute entry was ignored. Now judged on both.
		testutil.HTTPRoute("shop", "mixed", []objects.ParentRef{gw, api}, ok(gw), rejected(api)),
		// Phase 1: not owned. Now owned once with two reported parents.
		testutil.HTTPRoute("shop", "two", nil, ok(api), ok(api2)),
		// Phase 1: not owned. Now owned through api2 only; the entry for the removed api ref is ignored.
		testutil.HTTPRoute("shop", "moved", []objects.ParentRef{api2}, rejected(api), ok(api2)),
		// Phase 1: owned by neither. Now owned by both installs, each through its own parent.
		testutil.HTTPRoute("shared", "both", nil, ok(api), ok(pb)),
		// Phase 1: not owned. Now owned while its namespace is in scope.
		testutil.HTTPRoute("leaving", "child", nil, ok(api)),
		// Phase 1: not owned. Now owned with a stale implicit edge: KGWUI-003 warning.
		stale(testutil.HTTPRoute("shop", "stale", nil, ok(api))),
		// Phase 1 and now: a policy with an HTTPRoute ancestor stays owned (PF8).
		testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: api, Controller: testutil.Controller,
			Conds: []metav1.Condition{cond("Accepted", metav1.ConditionTrue, "Accepted")}}),
	)
	e, srv, update := startWith(t, setup{metrics: okMetrics("kgw.kgateway", "kgw2.kgateway2")}, objs...)
	routes := func(id string) int {
		n, _ := count(get[OverviewResponse](t, srv, "/api/v1/installs/"+id+"/overview", 200), "HTTPRoute")
		return n
	}
	// Phase 1: kgw 4 (cart, api, api2, mixed), kgw2 1 (pb).
	if n := routes("kgw.kgateway"); n != 9 {
		t.Errorf("kgw HTTPRoutes %d, want 9 (cart api api2 mixed two moved both child stale)", n)
	}
	if n := routes("kgw2.kgateway2"); n != 2 {
		t.Errorf("kgw2 HTTPRoutes %d, want 2 (pb both)", n)
	}
	if n, _ := count(get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", 200), "TrafficPolicy"); n != 1 {
		t.Errorf("TrafficPolicy %d, want 1 as in Phase 1", n)
	}
	// Phase 1: healthy, no findings on these routes.
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
	if h.State != health.StateError {
		t.Errorf("roll-up %s, want error (mixed is rejected by its HTTPRoute parent)", h.State)
	}
	if !hasSev(h, health.CodeConditionFalse, health.SeverityError, "shop/mixed") {
		t.Errorf("mixed not judged on its HTTPRoute parent: %+v", h.Findings)
	}
	if !hasSev(h, health.CodeRouteUnattached, health.SeverityWarning, "shop/stale") {
		t.Errorf("stale implicit edge: want KGWUI-003 warning: %+v", h.Findings)
	}
	for _, r := range []string{"shop/two", "shop/moved", "shared/both", "leaving/child"} {
		if hasFinding(h, health.CodeConditionFalse, r) || hasFinding(h, health.CodeRouteUnattached, r) {
			t.Errorf("unexpected finding on %s: %+v", r, h.Findings)
		}
	}
	if h2 := get[HealthResponse](t, srv, "/api/v1/installs/kgw2.kgateway2/health", 200); hasFinding(h2, health.CodeConditionFalse, "shared/both") {
		t.Errorf("kgw2 judges only its own parent's entry: %+v", h2.Findings)
	}
	// The namespace leaves the scope: the child is no longer owned.
	update(testutil.Namespace("leaving", nil))
	deadline := time.Now().Add(5 * time.Second)
	for e.Compute(); routes("kgw.kgateway") != 8; e.Compute() {
		if time.Now().After(deadline) {
			t.Fatal("leaving/child still owned after its namespace left the scope")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A child naming one parent twice (by section and by port) has each entry
// judged: a stale one is pending while the current one is clean.
func TestEveryEligibleEntryIsJudged(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	s1, s2 := testutil.HTTPRouteRef("shop", "api"), testutil.HTTPRouteRef("shop", "api")
	s1.SectionName, s2.Port = "a", 8080
	fresh := testutil.AcceptedBy(s1, testutil.Controller)
	for i := range fresh.Conds {
		fresh.Conds[i].ObservedGeneration = 2
	}
	child := stale(testutil.HTTPRoute("shop", "child", []objects.ParentRef{s1, s2}, fresh, testutil.AcceptedBy(s2, testutil.Controller)))
	_, srv, _ := startWith(t, setup{}, isolated(testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)), child)...)
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
	var details []string
	for _, f := range h.Findings {
		if f.Code == health.CodeRouteUnattached && f.Resource != nil && f.Resource.String() == "shop/child" {
			details = append(details, f.Detail)
		}
	}
	if len(details) != 1 || !strings.Contains(details[0], "port 8080") {
		t.Fatalf("want one KGWUI-003 for the stale port-8080 entry, got %q; findings %+v", details, h.Findings)
	}
}

// findingsOn returns h's findings on resource res.
func findingsOn(h HealthResponse, res string) []FindingDTO {
	var out []FindingDTO
	for _, f := range h.Findings {
		if f.Resource != nil && f.Resource.String() == res {
			out = append(out, f)
		}
	}
	return out
}

// Candidates are never counted: a declared route and a route with no parent
// leave the HTTPRoute tile as it was. The declared one's only finding is the
// KGWUI-011 warning, so the install is yellow, not pending.
func TestCandidatesAreNotCounted(t *testing.T) {
	gw, api := testutil.GwRef("infra", "edge"), testutil.HTTPRouteRef("shop", "api")
	_, srv, _ := startWith(t, setup{}, isolated(
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.HTTPRoute("shop", "declared", []objects.ParentRef{api}),
		testutil.HTTPRoute("shop", "lonely", nil),
	)...)
	o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", 200)
	if n, _ := count(o, "HTTPRoute"); n != 2 {
		t.Fatalf("HTTPRoute count %d, want 2 (cart, api)", n)
	}
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
	if h.State != health.StateWarning || checkPending(h, "resource status") {
		t.Fatalf("state %s; findings %+v", h.State, h.Findings)
	}
	if fs := findingsOn(h, "shop/declared"); len(fs) != 1 || fs[0].Code != health.CodeDelegatedNotAttached || fs[0].Severity != health.SeverityWarning {
		t.Fatalf("a declared route gets only the KGWUI-011 warning: %+v", fs)
	}
	if fs := findingsOn(h, "shop/lonely"); len(fs) != 0 {
		t.Fatalf("a no-parent route has no status finding: %+v", fs)
	}
}

// An out-of-scope route naming an owned HTTPRoute is never a candidate: it
// gets KGWUI-004 only, and the resource-status check stays OK (design §4.2).
func TestUnwatchedDeclaredRouteGetsOnlyKGWUI004(t *testing.T) {
	gw, api := testutil.GwRef("infra", "edge"), testutil.HTTPRouteRef("shop", "api")
	parent := testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller))
	// Baseline: blog/posts names the Gateway (Phase 1).
	_, base, _ := startWith(t, setup{}, world(parent.DeepCopy())...)
	want := get[HealthResponse](t, base, "/api/v1/installs/kgw.kgateway/health", 200).State
	// The out-of-scope route is created late, inside every grace period.
	_, srv, _ := startWith(t, setup{late: []*unstructured.Unstructured{testutil.HTTPRoute("blog", "child", []objects.ParentRef{api})}}, isolated(parent)...)
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
	fs := findingsOn(h, "blog/child")
	if len(fs) != 1 || fs[0].Code != health.CodeNamespaceNotWatched {
		t.Fatalf("want exactly KGWUI-004 on blog/child, got %+v", fs)
	}
	if checkPending(h, "resource status") {
		t.Fatalf("an out-of-scope route never makes resource status pending: %+v", h.Checks)
	}
	if h.State != want {
		t.Fatalf("state %s, want %s as for a route naming the Gateway", h.State, want)
	}
	if n, _ := count(get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", 200), "HTTPRoute"); n != 2 {
		t.Fatalf("HTTPRoute count %d, want 2", n)
	}
}

// KGWUI-004 names the parent's kind; the Gateway text is Phase 1's. A route
// naming a grandchild is found too (PF10).
func TestKGWUI004DetailNamesParentKind(t *testing.T) {
	gw, ok := testutil.GwRef("infra", "edge"), func(p objects.ParentRef) testutil.RouteParent { return testutil.AcceptedBy(p, testutil.Controller) }
	ls := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	api, child := testutil.HTTPRouteRef("shop", "api"), testutil.HTTPRouteRef("shop", "child")
	_, srv, _ := startWith(t, setup{}, isolated(
		testutil.ListenerSet("infra", "ls", gw, nil),
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, ok(gw)),
		testutil.HTTPRoute("shop", "child", nil, ok(api)),
		testutil.HTTPRoute("blog", "to-gw", []objects.ParentRef{gw}),
		testutil.HTTPRoute("blog", "to-ls", []objects.ParentRef{ls}),
		testutil.HTTPRoute("blog", "to-route", []objects.ParentRef{api}),
		testutil.HTTPRoute("blog", "to-child", []objects.ParentRef{child}),
		// Names a Gateway and a route of the same install: one finding, as in Phase 1.
		testutil.HTTPRoute("blog", "both", []objects.ParentRef{gw, api}),
	)...)
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
	for res, want := range map[string]string{"blog/to-gw": "targets Gateway infra/edge", "blog/to-ls": "targets ListenerSet infra/ls",
		"blog/to-route": "targets HTTPRoute shop/api", "blog/to-child": "targets HTTPRoute shop/child", "blog/both": "targets Gateway infra/edge"} {
		fs := findingsOn(h, res)
		if len(fs) != 1 || fs[0].Code != health.CodeNamespaceNotWatched || fs[0].Detail != want {
			t.Errorf("%s: want one KGWUI-004 %q, got %+v", res, want, fs)
		}
	}
}

// The Backend tile counts in-use Backends only, and a denied one leaves it.
func TestBackendTileCountsInUseBackends(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	r := testutil.Rule()
	r["backendRefs"] = []any{map[string]any{"group": "gateway.kgateway.dev", "kind": "Backend", "name": "used"},
		map[string]any{"group": "gateway.kgateway.dev", "kind": "Backend", "name": "hidden"}}
	objs := isolated(testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)), r),
		testutil.Backend("shop", "used", nil), testutil.Backend("shop", "hidden", nil), testutil.Backend("shop", "idle", nil),
		// Used only by a GRPCRoute and a TCPRoute.
		backendOnly("GRPCRoute", "v1", "grpc", "by-grpc"), backendOnly("TCPRoute", "v1", "tcp", "by-tcp"),
		testutil.Backend("shop", "by-grpc", nil), testutil.Backend("shop", "by-tcp", nil))
	_, all, _ := startWith(t, setup{}, objs...)
	if n, _ := count(get[OverviewResponse](t, all, "/api/v1/installs/kgw.kgateway/overview", 200), "Backend"); n != 4 {
		t.Fatalf("Backend tile %d, want 4 (used, hidden, by-grpc, by-tcp; idle is not in use)", n)
	}
	authz := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Name != "hidden" })
	_, some, _ := startWith(t, setup{authz: authz}, objs...)
	if n, _ := count(get[OverviewResponse](t, some, "/api/v1/installs/kgw.kgateway/overview", 200), "Backend"); n != 3 {
		t.Fatalf("Backend tile %d for a caller denied shop/hidden, want 1", n)
	}
}

// backendOnly is an accepted route of kind in shop whose one rule sends to Backend shop/backend.
func backendOnly(kind, version, name, backend string) *unstructured.Unstructured {
	gw := testutil.GwRef("infra", "edge")
	r := testutil.Rule()
	r["backendRefs"] = []any{map[string]any{"group": "gateway.kgateway.dev", "kind": "Backend", "name": backend}}
	u := testutil.WithRules(testutil.HTTPRoute("shop", name, []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)), r)
	u.SetAPIVersion("gateway.networking.k8s.io/" + version)
	u.SetKind(kind)
	return u
}

// A declared route created inside its grace period gets KGWUI-011 info, and
// the resource-status check is pending, so the install is unknown (design §6.2).
func TestDeclaredRouteInGraceIsPendingAtTheAPI(t *testing.T) {
	gw, api := testutil.GwRef("infra", "edge"), testutil.HTTPRouteRef("shop", "api")
	_, srv, _ := startWith(t, setup{late: []*unstructured.Unstructured{testutil.HTTPRoute("shop", "c", []objects.ParentRef{api})}},
		isolated(testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)))...)
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
	fs := findingsOn(h, "shop/c")
	if len(fs) != 1 || fs[0].Code != health.CodeDelegatedNotAttached || fs[0].Severity != health.SeverityInfo {
		t.Fatalf("want KGWUI-011 info on shop/c: %+v", fs)
	}
	if !checkPending(h, "resource status") || h.State != health.StateUnknown {
		t.Fatalf("state %s, checks %+v; want unknown and pending", h.State, h.Checks)
	}
	resp, err := http.Get(srv.URL + "/api/v1/installs/kgw.kgateway/health")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if b, _ := io.ReadAll(resp.Body); bytes.Contains(bytes.ToLower(b), []byte("evidence")) {
		t.Fatalf("Evidence is never serialised: %s", b)
	}
}

// PF15: on kgateway 2.3 a child whose parentRef omits the namespace or sets
// sectionName gets no entry (D-021 items 2 and 3), so it is a declared
// candidate and gets the KGWUI-011 warning after grace, as on every version.
func TestKGWUI011OnKgateway23ForUnreportedRefForms(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	noNS := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Name: "api"}
	section := testutil.HTTPRouteRef("shop", "api")
	section.SectionName = "http"
	objs := withControllerVersion("2.3.11", isolated(
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.HTTPRoute("shop", "no-ns", []objects.ParentRef{noNS}),
		testutil.HTTPRoute("shop", "section", []objects.ParentRef{section}),
	))
	_, srv, _ := startWith(t, setup{}, objs...)
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
	for _, res := range []string{"shop/no-ns", "shop/section"} {
		fs := findingsOn(h, res)
		if len(fs) != 1 || fs[0].Code != health.CodeDelegatedNotAttached || fs[0].Severity != health.SeverityWarning || fs[0].Detail != "No status entry for parent HTTPRoute shop/api" {
			t.Fatalf("%s: want the KGWUI-011 warning: %+v", res, fs)
		}
	}
	if h.State != health.StateWarning || checkPending(h, "resource status") {
		t.Fatalf("state %s; findings %+v", h.State, h.Findings)
	}
}
