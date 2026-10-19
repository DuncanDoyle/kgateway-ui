package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	"github.com/DuncanDoyle/kgateway-ui/pkg/extension"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
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

const acmeController = "acme.example.com/edge"

type acmeDetector struct{}

func (acmeDetector) Name() string { return "acme" }
func (acmeDetector) ControllerSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"app": "acme-edge"})
}
func (acmeDetector) ProxySelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"app": "acme-proxy"})
}

// ControllerPodSelector matches the Pods of the fixture Deployment, whose
// template comes from testutil.ControllerDeployment.
func (acmeDetector) ControllerPodSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"kgateway": "kgateway"})
}
func (acmeDetector) Detect(d *appsv1.Deployment) (install.Install, bool) {
	return install.Install{Namespace: d.Namespace, Name: d.Name, Edition: "acme", Version: "9.9.9", Container: "kgateway",
		ControllerNames: []string{acmeController}, Deployment: d, MetricsPort: 9092}, true
}

type acmeCheck struct{}

func (acmeCheck) Name() string { return "acme demo" }
func (acmeCheck) Run(in health.Input) ([]health.Finding, health.CheckResult) {
	return []health.Finding{{Code: "ACME-001", Slug: "demo", Severity: health.SeverityInfo, Install: in.Install.ID(), Message: "Downstream check ran"}},
		health.CheckResult{Name: "acme demo", OK: true, CollectedAt: in.Now}
}

var wafKind = kinds.Descriptor{Group: "policy.acme.example.com", Kind: "WafPolicy", Resource: "wafpolicies", Versions: []string{"v1"},
	Namespaced: true, Category: kinds.CategoryPolicy, DisplayName: "WAF policies"}

func wafPolicy(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "policy.acme.example.com/v1", "kind": "WafPolicy",
		"metadata": map[string]any{"name": name, "namespace": "shop", "uid": "waf/" + name},
		"status": map[string]any{"ancestors": []any{map[string]any{
			"ancestorRef": map[string]any{"name": "edge", "namespace": "infra"}, "controllerName": acmeController, "conditions": []any{}}}},
	}}
}

// A downstream registers a detector, a kind, a check and an Authorizer
// through pkg/extension, and each shows up in the API output (design §9).
func TestDownstreamExtensionSeam(t *testing.T) {
	d := testutil.ControllerDeployment("acme-system", "edge", "example.com/acme/edge-controller:9.9.9", 1, 1)
	d.Labels = map[string]string{"app": "acme-edge"}
	gw := testutil.GwRef("infra", "edge")
	hidden := objects.Ref{Group: wafKind.Group, Kind: "WafPolicy", Namespace: "shop", Name: "waf-hidden"}
	deny := api.AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != hidden })
	_, srv := start(t, []runtime.Object{
		testutil.Namespace("infra", nil), testutil.Namespace("shop", nil), d, testutil.ControllerPod(d, "edge-0", true),
		testutil.GatewayClass("acme", acmeController, true), testutil.Gateway("infra", "edge", "acme", nil),
		testutil.HTTPRoute("shop", "r", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, acmeController)), wafPolicy("waf"), wafPolicy("waf-hidden"),
	}, []kinds.Descriptor{wafKind}, Options{PollInterval: 50 * time.Millisecond, MetricsInterval: time.Hour, UI: ui}, &stubScraper{},
		extension.WithDetectors(acmeDetector{}), extension.WithKinds(wafKind), extension.WithChecks(acmeCheck{}), extension.WithAuthorizer(deny))

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
		getJSON(t, srv, "/api/v1/installs/acme-system/edge/health", &h)
		for _, f := range h.Findings {
			if f.Code == "ACME-001" {
				return true
			}
		}
		return false
	})
	var o api.OverviewResponse
	getJSON(t, srv, "/api/v1/installs/acme-system/edge/overview", &o)
	found := false
	for _, c := range o.Counts {
		if c.Kind == "WafPolicy" && c.Count == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("downstream kind not counted, or the denied one not dropped: %+v", o.Counts)
	}
	base := "/api/v1/installs/acme-system/edge/resources/policy.acme.example.com/WafPolicy/shop/"
	if code := getJSON(t, srv, base+"waf", nil); code != http.StatusOK {
		t.Fatalf("an allowed downstream resource must be served, got %d", code)
	}
	if code := getJSON(t, srv, base+"waf-hidden", nil); code != http.StatusNotFound {
		t.Fatalf("the downstream Authorizer must hide the denied resource, got %d", code)
	}
}

// D-013 / Review Focus 9: a deny-some Authorizer registered through the
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
		return getJSON(t, srv, "/api/v1/installs/kgw/kgateway/health", &h) == http.StatusOK && len(h.Findings) > 0 && h.State == health.StateError
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
	if code := getJSON(t, srv, "/api/v1/installs/kgw/kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/broken", nil); code != http.StatusNotFound {
		t.Fatalf("a denied resource must 404, got %d", code)
	}
	if code := getJSON(t, srv, "/api/v1/installs/kgw/kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/cart", nil); code != http.StatusOK {
		t.Fatalf("an allowed resource must be served, got %d", code)
	}
	var o api.OverviewResponse
	getJSON(t, srv, "/api/v1/installs/kgw/kgateway/overview", &o)
	for _, c := range o.Counts {
		if c.Kind == "HTTPRoute" && c.Count != 1 {
			t.Fatalf("the denied route must leave the HTTPRoute count: %d", c.Count)
		}
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
	eventually(t, "no metrics result for the install", func() bool { _, ok := a.collector.Get("kgw/kgateway"); return ok })
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
