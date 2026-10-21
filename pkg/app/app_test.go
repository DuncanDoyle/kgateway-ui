package app

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/extension"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxymode"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

// stubScraper serves an empty /metrics payload and counts its calls. The fake
// clientset's pods/proxy cannot serve a body, so tests never use the real scrapers.
type stubScraper struct{ calls atomic.Int32 }

func (s *stubScraper) Scrape(context.Context, *corev1.Pod, int32) (io.ReadCloser, error) {
	s.calls.Add(1)
	return io.NopCloser(strings.NewReader("")), nil
}

var ui = fstest.MapFS{"index.html": {Data: []byte("x")}}

// start builds and starts the app over fake clients and serves its handler.
func start(t *testing.T, objs []runtime.Object, extra []kinds.Descriptor, o Options, sc *stubScraper, opts ...extension.Option) (*App, *httptest.Server) {
	t.Helper()
	dyn, kube := testutil.ClientsWith(t, extra, objs...)
	a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: sc}, o, extension.New(opts...))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.Start(ctx)
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	return a, srv
}

// getJSON decodes GET path into v and returns the status code.
func getJSON(t *testing.T, srv *httptest.Server, path string, v any) int {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK && v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode
}

// eventually polls cond until it holds or 5 s pass.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

const acmeController = "acme.example.com/controller"

type acmeDetector struct{}

func (acmeDetector) Name() string { return "acme" }
func (acmeDetector) ControllerSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"app": "acme-controller"})
}
func (acmeDetector) ProxySelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"app": "acme-proxy"})
}

// ControllerPodSelector matches the Pods of the fixture Deployment, whose
// template comes from testutil.ControllerDeployment.
func (acmeDetector) ControllerPodSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"kgateway": "kgateway"})
}

// Detect leaves Deployment and ProxySelector unset: Discover fills them.
func (acmeDetector) Detect(d *appsv1.Deployment) (install.Install, bool) {
	return install.Install{Namespace: d.Namespace, Name: d.Name, Edition: "acme", Version: "9.9.9", Container: "kgateway",
		ControllerNames: []string{acmeController}, MetricsPort: 9092}, true
}

type acmeCheck struct{}

func (acmeCheck) Name() string { return "acme demo" }
func (acmeCheck) Run(in health.Input) ([]health.Finding, health.CheckResult) {
	return []health.Finding{{Code: "ACME-001", Slug: "demo", Severity: health.SeverityInfo, Install: in.Install.ID(), Message: "Downstream check ran"}},
		health.CheckResult{Name: "acme demo", OK: true, CollectedAt: in.Now}
}

var widgetKind = kinds.Descriptor{Group: "widgets.acme.example.com", Kind: "WidgetPolicy", Resource: "widgetpolicies", Versions: []string{"v1"},
	Namespaced: true, Category: kinds.CategoryPolicy, DisplayName: "Widget policies"}

func widgetPolicy(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "widgets.acme.example.com/v1", "kind": "WidgetPolicy",
		"metadata": map[string]any{"name": name, "namespace": "shop", "uid": "widget/" + name},
		"status": map[string]any{"ancestors": []any{map[string]any{
			"ancestorRef": map[string]any{"name": "edge", "namespace": "infra"}, "controllerName": acmeController, "conditions": []any{}}}},
	}}
}

// A downstream registers a detector, a kind, a check and an Authorizer
// through pkg/extension, and each shows up in the API output (design §9).
func TestDownstreamExtensionSeam(t *testing.T) {
	d := testutil.ControllerDeployment("acme-system", "controller", "example.com/acme/controller:9.9.9", 1, 1)
	d.Labels = map[string]string{"app": "acme-controller"}
	gw := testutil.GwRef("infra", "edge")
	hidden := objects.Ref{Group: widgetKind.Group, Kind: "WidgetPolicy", Namespace: "shop", Name: "widget-hidden"}
	deny := api.AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != hidden })
	_, srv := start(t, []runtime.Object{
		testutil.Namespace("infra", nil), testutil.Namespace("shop", nil), d, testutil.ControllerPod(d, "controller-0", true),
		testutil.GatewayClass("acme", acmeController, true), testutil.Gateway("infra", "edge", "acme", nil),
		testutil.HTTPRoute("shop", "r", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, acmeController)), widgetPolicy("widget"), widgetPolicy("widget-hidden"),
	}, []kinds.Descriptor{widgetKind}, Options{PollInterval: 50 * time.Millisecond, MetricsInterval: time.Hour, UI: ui}, &stubScraper{},
		extension.WithDetectors(acmeDetector{}), extension.WithKinds(widgetKind, widgetKind), extension.WithChecks(acmeCheck{}), extension.WithAuthorizer(deny))

	var insts []api.InstallSummary
	eventually(t, "install never appeared", func() bool {
		insts = nil
		return getJSON(t, srv, "/api/v1/installs", &insts) == http.StatusOK && len(insts) == 1
	})
	if insts[0].Edition != "acme" {
		t.Fatalf("downstream detector not used: %+v", insts[0])
	}
	var h api.HealthResponse
	eventually(t, "downstream check finding missing", func() bool {
		h = api.HealthResponse{}
		getJSON(t, srv, "/api/v1/installs/acme-system.controller/health", &h)
		for _, f := range h.Findings {
			if f.Code == "ACME-001" {
				return true
			}
		}
		return false
	})
	var o api.OverviewResponse
	getJSON(t, srv, "/api/v1/installs/acme-system.controller/overview", &o)
	// The kind is registered twice; it is watched and counted once.
	tiles := 0
	for _, c := range o.Counts {
		if c.Kind == "WidgetPolicy" {
			tiles++
			if c.Count != 1 {
				t.Fatalf("downstream kind not counted, or the denied one not dropped: %+v", o.Counts)
			}
		}
	}
	if tiles != 1 {
		t.Fatalf("want one WidgetPolicy tile, got %d: %+v", tiles, o.Counts)
	}
	base := "/api/v1/installs/acme-system.controller/resources/widgets.acme.example.com/WidgetPolicy/shop/"
	if code := getJSON(t, srv, base+"widget", nil); code != http.StatusOK {
		t.Fatalf("an allowed downstream resource must be served, got %d", code)
	}
	if code := getJSON(t, srv, base+"widget-hidden", nil); code != http.StatusNotFound {
		t.Fatalf("the downstream Authorizer must hide the denied resource, got %d", code)
	}
}

// D-013: a deny-some Authorizer registered through the
// extension hides the denied resource (404) and restricts its finding, while
// the roll-up still covers it.
func TestAuthorizerSeamRestrictsDeniedResources(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	gw := testutil.GwRef("infra", "edge")
	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	denied := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "broken"}
	deny := api.AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != denied })
	_, srv := start(t, []runtime.Object{
		testutil.Namespace("kgw", nil), testutil.Namespace("infra", nil), testutil.Namespace("shop", nil),
		d, testutil.ControllerPod(d, "kgateway-0", true), testutil.ProxyDeployment("infra", "edge", 1, 1),
		testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.Gateway("infra", "edge", "kgateway", nil),
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)), broken,
	}, nil, Options{PollInterval: 50 * time.Millisecond, MetricsInterval: time.Hour, UI: ui}, &stubScraper{}, extension.WithAuthorizer(deny))

	// Wait for the first model: the broken route's KGWUI-002 makes the state error.
	var h api.HealthResponse
	eventually(t, "health never settled", func() bool {
		h = api.HealthResponse{}
		return getJSON(t, srv, "/api/v1/installs/kgw.kgateway/health", &h) == http.StatusOK && len(h.Findings) > 0 && h.State == health.StateError
	})
	var restricted *health.Finding
	for i := range h.Findings {
		if f := h.Findings[i]; f.Resource != nil && *f.Resource == denied {
			restricted = &h.Findings[i]
		}
	}
	if restricted == nil || !restricted.Restricted || restricted.Code != health.CodeConditionFalse || restricted.Severity != health.SeverityError ||
		restricted.Message != "" || restricted.Detail != "" || restricted.Since != nil {
		t.Fatalf("the denied route's finding must stay listed, restricted, with code/severity/ref and no message or evidence: %+v", restricted)
	}
	if h.State != health.StateError || h.Counts[health.SeverityError] < 1 {
		t.Fatalf("the roll-up must still cover the restricted finding: state=%s counts=%v", h.State, h.Counts)
	}
	if code := getJSON(t, srv, "/api/v1/installs/kgw.kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/broken", nil); code != http.StatusNotFound {
		t.Fatalf("a denied resource must 404, got %d", code)
	}
	if code := getJSON(t, srv, "/api/v1/installs/kgw.kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/cart", nil); code != http.StatusOK {
		t.Fatalf("an allowed resource must be served, got %d", code)
	}
	var o api.OverviewResponse
	getJSON(t, srv, "/api/v1/installs/kgw.kgateway/overview", &o)
	tile := false
	for _, c := range o.Counts {
		if c.Kind == "HTTPRoute" {
			tile = true
			if c.Count != 1 {
				t.Fatalf("the denied route must leave the HTTPRoute count: %d", c.Count)
			}
		}
	}
	if !tile {
		t.Fatalf("HTTPRoute tile missing: %+v", o.Counts)
	}
}

// The first metrics collection follows the first discovered install, not the
// first collector tick: with a one-hour interval nothing else would scrape.
func TestFirstMetricsCollectionFollowsDiscovery(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	sc := &stubScraper{}
	a, _ := start(t, []runtime.Object{testutil.Namespace("kgw", nil), d, testutil.ControllerPod(d, "kgateway-0", true)}, nil,
		Options{PollInterval: 20 * time.Millisecond, MetricsInterval: time.Hour, UI: ui}, sc)
	eventually(t, "the controller was never scraped after discovery", func() bool { return sc.calls.Load() > 0 })
	eventually(t, "no metrics result for the install", func() bool { _, ok := a.collector.Get("kgw.kgateway"); return ok })
}

// The scope verifier must wait 2 × and metrics expire after 3 × the metrics
// interval (design §4.4); zero would disable either guard.
func TestBuildDerivesMetricsTimingsFromTheInterval(t *testing.T) {
	dyn, kube := testutil.Clients(t)
	c := Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: &stubScraper{}}
	a, err := Build(c, Options{PollInterval: time.Second, MetricsInterval: 7 * time.Second, UI: ui}, extension.New())
	if err != nil {
		t.Fatal(err)
	}
	if a.cfg.MetricsMaxAge != 21*time.Second {
		t.Fatalf("MetricsMaxAge must be 3 × the metrics interval, got %v", a.cfg.MetricsMaxAge)
	}
	v, ok := a.cfg.Verifier.(*scopeverify.ManagedCountVerifier)
	if !ok || v.Persist() != 14*time.Second {
		t.Fatalf("the upstream verifier must persist for 2 × the metrics interval, got %T", a.cfg.Verifier)
	}
	own := scopeverify.NewManagedCountVerifier(time.Minute)
	if a, err := Build(c, Options{PollInterval: time.Second, MetricsInterval: 7 * time.Second, UI: ui}, extension.New(extension.WithScopeVerifier(own))); err != nil || a.cfg.Verifier != own {
		t.Fatalf("a downstream verifier must be used unchanged: %v", err)
	}
	for _, o := range []Options{{PollInterval: time.Second}, {MetricsInterval: time.Second}} {
		if _, err := Build(c, o, extension.New()); err == nil {
			t.Fatalf("Build must reject a zero interval: %+v", o)
		}
	}
}

// build builds the app over fake clients without starting it.
func build(t *testing.T, objs ...runtime.Object) *App {
	t.Helper()
	dyn, kube := testutil.Clients(t, objs...)
	a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: &stubScraper{}},
		Options{PollInterval: 10 * time.Millisecond, MetricsInterval: time.Hour, UI: ui}, extension.New())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// A listen failure returns an error and starts nothing that outlives serve.
func TestServeListenFailureStartsNothing(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	a := build(t)
	if err := serve(context.Background(), a, ln.Addr().String(), nil); err == nil {
		t.Fatal("serve must fail when the address is in use")
	}
	// The compute loop runs every 10 ms; a leaked one would publish a model.
	time.Sleep(100 * time.Millisecond)
	if a.Engine.Current() != nil {
		t.Fatal("no component may run after serve returned an error")
	}
}

// Cancelling ctx drains an in-flight request before serve returns.
func TestServeDrainsInFlightRequests(t *testing.T) {
	a := build(t)
	entered, release := make(chan struct{}), make(chan struct{})
	a.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		select {
		case <-release:
			_, _ = io.WriteString(w, "done")
		case <-time.After(testutil.RecvTimeout):
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	addr := make(chan net.Addr, 1)
	served := make(chan error, 1)
	go func() { served <- serve(ctx, a, "127.0.0.1:0", func(a net.Addr) { addr <- a }) }()
	resp := make(chan string, 1)
	go func() {
		var ad net.Addr
		select {
		case ad = <-addr:
		case <-time.After(testutil.RecvTimeout):
			resp <- "serve never listened"
			return
		}
		r, err := http.Get("http://" + ad.String() + "/")
		if err != nil {
			resp <- err.Error()
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		resp <- string(b)
	}()
	select {
	case <-entered:
	case <-time.After(testutil.RecvTimeout):
		t.Fatal("the request never reached the handler")
	}
	cancel()
	select {
	case err := <-served:
		t.Fatalf("serve returned with a request in flight: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	if got := testutil.Recv(t, resp, "the in-flight response"); got != "done" {
		t.Fatalf("the in-flight request must complete, got %q", got)
	}
	if err := testutil.Recv(t, served, "serve to return"); err != nil {
		t.Fatalf("a cancelled serve must return nil, got %v", err)
	}
}

// selectorScope is a downstream ScopeProvider that builds its Scope by hand,
// as a downstream outside pkg/install must.
type selectorScope struct{}

func (selectorScope) Name() string { return "team-a" }
func (selectorScope) Scope(install.Install) install.Scope {
	return install.Scope{Mode: install.ScopeSelectors, Selectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"team": "a"}}}}
}

// A downstream ScopeProvider returning selectors decides namespace membership
// through the API: team=a namespaces are watched, others are not.
func TestDownstreamSelectorScopeSeam(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	gw := testutil.GwRef("infra", "edge")
	_, srv := start(t, []runtime.Object{
		testutil.Namespace("kgw", nil), testutil.Namespace("infra", map[string]string{"team": "a"}),
		testutil.Namespace("shop", map[string]string{"team": "a"}), testutil.Namespace("blog", nil),
		d, testutil.ControllerPod(d, "kgateway-0", true), testutil.ProxyDeployment("infra", "edge", 1, 1),
		testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.Gateway("infra", "edge", "kgateway", nil),
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.HTTPRoute("blog", "posts", []objects.ParentRef{gw}),
	}, nil, Options{PollInterval: 50 * time.Millisecond, MetricsInterval: time.Hour, UI: ui}, &stubScraper{}, extension.WithScopeProvider(selectorScope{}))

	var o api.OverviewResponse
	eventually(t, "overview never appeared", func() bool {
		o = api.OverviewResponse{}
		return getJSON(t, srv, "/api/v1/installs/kgw.kgateway/overview", &o) == http.StatusOK
	})
	if o.Header.Scope.Watched != 2 || o.Header.Scope.Total != 4 {
		t.Fatalf("a downstream selector scope must match team=a namespaces: %+v", o.Header.Scope)
	}
	for kind, want := range map[string]int{"Gateway": 1, "HTTPRoute": 1} {
		got := -1
		for _, c := range o.Counts {
			if c.Kind == kind {
				got = c.Count
			}
		}
		if got != want {
			t.Fatalf("%s count: want %d, got %d (%+v)", kind, want, got, o.Counts)
		}
	}
}

// Proxies are matched with the detector's ProxySelector plus the standard
// gateway-name label: a downstream proxy without the upstream component label
// is found, and an upstream-labelled Deployment for the same Gateway is not.
func TestDownstreamProxySelectorSeam(t *testing.T) {
	d := testutil.ControllerDeployment("acme-system", "controller", "example.com/acme/controller:9.9.9", 1, 1)
	d.Labels = map[string]string{"app": "acme-controller"}
	proxy := testutil.ProxyDeployment("infra", "edge-proxy", 2, 1)
	proxy.Labels = map[string]string{"app": "acme-proxy", "gateway.networking.k8s.io/gateway-name": "edge"}
	proxy.OwnerReferences = nil
	decoy := testutil.ProxyDeployment("infra", "edge", 1, 1) // upstream labels, ready
	_, srv := start(t, []runtime.Object{
		testutil.Namespace("acme-system", nil), testutil.Namespace("infra", nil), d, testutil.ControllerPod(d, "controller-0", true),
		proxy, decoy, testutil.GatewayClass("acme", acmeController, true), testutil.Gateway("infra", "edge", "acme", nil),
	}, nil, Options{PollInterval: 50 * time.Millisecond, MetricsInterval: time.Hour, UI: ui}, &stubScraper{}, extension.WithDetectors(acmeDetector{}))

	var h api.HealthResponse
	eventually(t, "the downstream proxy was never matched", func() bool {
		h = api.HealthResponse{}
		getJSON(t, srv, "/api/v1/installs/acme-system.controller/health", &h)
		for _, f := range h.Findings {
			if f.Code == health.CodeProxyNotReady && f.Resource != nil && f.Resource.String() == "infra/edge" && f.Message == "Proxy not ready (1 of 2 replicas)" {
				return true
			}
		}
		return false
	})
}

// hidingDiscovery serves base without the resource named in hidden, so a test
// can install or remove a CRD after start.
type hidingDiscovery struct {
	base   kinds.DiscoveryClient
	hidden atomic.Pointer[string]
}

func (h *hidingDiscovery) hide(resource string) { h.hidden.Store(&resource) }

func (h *hidingDiscovery) ServerResourcesForGroupVersion(gv string) (*metav1.APIResourceList, error) {
	l, err := h.base.ServerResourcesForGroupVersion(gv)
	hidden := h.hidden.Load()
	if err != nil || hidden == nil {
		return l, err
	}
	out := &metav1.APIResourceList{GroupVersion: l.GroupVersion}
	for _, r := range l.APIResources {
		if r.Name != *hidden {
			out.APIResources = append(out.APIResources, r)
		}
	}
	return out, nil
}

// kindsFinding returns the served-kinds finding and check of h, if any.
func kindsFinding(h api.HealthResponse) (*health.Finding, *health.CheckResult) {
	var f *health.Finding
	var c *health.CheckResult
	for i := range h.Findings {
		if h.Findings[i].Code == health.CodeSignalUnavailable && h.Findings[i].Message == "Served kinds changed; restart the UI to watch them" {
			f = &h.Findings[i]
		}
	}
	for i := range h.Checks {
		if h.Checks[i].Name == "served kinds" {
			c = &h.Checks[i]
		}
	}
	return f, c
}

// Served kinds are re-resolved while the UI runs (design §10): a CRD removed
// or installed after start is a required KGWUI-008 per install until the
// served set matches the watched one again.
func TestServedKindChangesAfterStartAreReported(t *testing.T) {
	cases := []struct {
		name    string
		atStart string // resource hidden when the app starts
		later   string // resource hidden afterwards
		detail  string
	}{
		{"CRD removed", "", "tcproutes", "removed tcproutes.gateway.networking.k8s.io/v1"},
		{"CRD installed", "tcproutes", "", "added tcproutes.gateway.networking.k8s.io/v1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
			dyn, kube := testutil.Clients(t, testutil.Namespace("kgw", nil), d, testutil.ControllerPod(d, "kgateway-0", true))
			disc := &hidingDiscovery{base: kube.Discovery()}
			disc.hide(tc.atStart)
			a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: disc, Scraper: &stubScraper{}},
				Options{PollInterval: 20 * time.Millisecond, MetricsInterval: time.Hour, KindsInterval: 20 * time.Millisecond, UI: ui}, extension.New())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			a.Start(ctx)
			srv := httptest.NewServer(a.Handler)
			t.Cleanup(srv.Close)
			report := func() api.HealthResponse {
				var h api.HealthResponse
				getJSON(t, srv, "/api/v1/installs/kgw.kgateway/health", &h)
				return h
			}
			eventually(t, "the served-kinds check never ran", func() bool { _, c := kindsFinding(report()); return c != nil && c.OK })

			disc.hide(tc.later)
			var h api.HealthResponse
			eventually(t, "the served-kinds change was never reported", func() bool { h = report(); f, _ := kindsFinding(h); return f != nil })
			f, c := kindsFinding(h)
			if !strings.Contains(f.Detail, tc.detail) || c.OK || !c.Required || h.State != health.StateUnknown {
				t.Fatalf("want a required KGWUI-008 with %q and state unknown: state=%s finding=%+v check=%+v", tc.detail, h.State, f, c)
			}

			disc.hide(tc.atStart)
			eventually(t, "the finding must clear once the served set matches again", func() bool { f, c := kindsFinding(report()); return f == nil && c != nil && c.OK })
		})
	}
}

// --install takes the install ID or the namespace/name form and pins the same
// install either way (D-016); anything else fails at startup.
func TestPinnedFlagAcceptsIDOrNamespaceName(t *testing.T) {
	const image = "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7"
	d1, d2 := testutil.ControllerDeployment("kgw", "kgateway", image, 1, 1), testutil.ControllerDeployment("kgw2", "kgateway2", image, 1, 1)
	objs := []runtime.Object{testutil.Namespace("kgw", nil), testutil.Namespace("kgw2", nil), d1, d2,
		testutil.ControllerPod(d1, "kgateway-0", true), testutil.ControllerPod(d2, "kgateway2-0", true)}
	for _, pin := range []string{"kgw/kgateway", "kgw.kgateway"} {
		t.Run(pin, func(t *testing.T) {
			_, srv := start(t, objs, nil, Options{PollInterval: 20 * time.Millisecond, MetricsInterval: time.Hour, UI: ui, Pinned: pin}, &stubScraper{})
			var insts []api.InstallSummary
			eventually(t, "the pinned install was never listed alone", func() bool {
				return getJSON(t, srv, "/api/v1/installs", &insts) == http.StatusOK && len(insts) == 1 && insts[0].ID == "kgw.kgateway"
			})
			var meta api.MetaResponse
			if getJSON(t, srv, "/api/v1/meta", &meta); meta.Pinned != "kgw.kgateway" {
				t.Fatalf("meta must report the normalised pin, got %q", meta.Pinned)
			}
		})
	}
	dyn, kube := testutil.Clients(t)
	c := Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: &stubScraper{}}
	for _, bad := range []string{"a/b/c", "kgw/", "/kgateway", "..", "a%b"} {
		if _, err := Build(c, Options{PollInterval: time.Second, MetricsInterval: time.Second, UI: ui, Pinned: bad}, extension.New()); err == nil {
			t.Errorf("--install %q must fail at startup", bad)
		}
	}
}

type alwaysSelfManaged struct{}

func (alwaysSelfManaged) Resolve(*unstructured.Unstructured, *cache.Snapshot, []kinds.Served) proxymode.Mode {
	return proxymode.SelfManaged
}

// A downstream replaces the self-managed resolution through pkg/extension,
// because it mirrors controller logic (D-016 decision 7).
func TestDownstreamProxyModeResolverSeam(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	_, srv := start(t, []runtime.Object{testutil.Namespace("kgw", nil), testutil.Namespace("infra", nil), d, testutil.ControllerPod(d, "kgateway-0", true),
		testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.Gateway("infra", "edge", "kgateway", nil)},
		nil, Options{PollInterval: 20 * time.Millisecond, MetricsInterval: time.Hour, UI: ui}, &stubScraper{}, extension.WithProxyModeResolver(alwaysSelfManaged{}))
	eventually(t, "the downstream resolver was not used", func() bool {
		var h api.HealthResponse
		if getJSON(t, srv, "/api/v1/installs/kgw.kgateway/health", &h) != http.StatusOK {
			return false
		}
		return slices.ContainsFunc(h.Findings, func(f health.Finding) bool { return f.Code == health.CodeSelfManagedProxy })
	})
}

// readmeOptions is the app.Options literal in the README's extension example.
// Keep it identical to the README; the test below fails if they drift.
const readmeOptions = "app.Options{UI: ui, PollInterval: 2 * time.Second, MetricsInterval: 10 * time.Second}"

// TestReadmeExtensionOptionsBuild guards the README example: its Options must
// start (the previous zero-interval example was rejected by Build).
func TestReadmeExtensionOptionsBuild(t *testing.T) {
	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), readmeOptions) {
		t.Fatalf("README extension example no longer uses %s; update it and readmeOptions together", readmeOptions)
	}
	dyn, kube := testutil.Clients(t)
	c := Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: &stubScraper{}}
	if _, err := Build(c, Options{UI: ui, PollInterval: 2 * time.Second, MetricsInterval: 10 * time.Second}, extension.New()); err != nil {
		t.Fatalf("README Options must build: %v", err)
	}
}
