package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/kgw23"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
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
		extension.WithDetectors(acmeDetector{}), extension.WithKinds(widgetKind, widgetKind), extension.WithChecks("ACME-", acmeCheck{}), extension.WithAuthorizer(deny))

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

// baseDetector is acmeDetector with a BaseVersion.
type baseDetector struct {
	acmeDetector
	base string
}

func (d baseDetector) Detect(dep *appsv1.Deployment) (install.Install, bool) {
	in, ok := d.acmeDetector.Detect(dep)
	in.BaseVersion = d.base
	return in, ok
}

// tcpRoute is a TCPRoute attached to the Gateway at p, accepted by controller.
func tcpRoute(ns, name string, p objects.ParentRef, controller string) *unstructured.Unstructured {
	u := testutil.HTTPRoute(ns, name, []objects.ParentRef{p}, testutil.AcceptedBy(p, controller))
	u.SetKind("TCPRoute")
	u.SetUID(types.UID("TCPRoute/" + ns + "/" + name))
	return u
}

// A downstream detector's BaseVersion selects the computed rule set and the
// TCPRoute experimental classification the routes list shows; without it the
// version is unknown. A second, upstream install keeps its own classification.
func TestRuleSetFromBaseVersion(t *testing.T) {
	for _, tc := range []struct {
		name, base, note, upstream string
		acmeExp, upstreamExp       bool
	}{
		{"base set", "v2.4.7", "", "v2.3.4", false, true},
		{"base unset", "", "Controller version unknown; using the 2.4 rules", "v2.4.7", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// An unreadable experimental setting makes only an experimental-only TCPRoute unknown.
			unknownExp := corev1.EnvVar{Name: install.ExperimentalEnv, Value: "maybe"}
			d := testutil.ControllerDeployment("acme-system", "controller", "example.com/acme/controller:9.9.9", 1, 1, unknownExp)
			d.Labels = map[string]string{"app": "acme-controller"}
			up := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:"+tc.upstream, 1, 1, unknownExp)
			up.Labels["app.kubernetes.io/version"] = strings.TrimPrefix(tc.upstream, "v")
			gw, acmeTCP, upTCP := testutil.GwRef("infra", "edge"), testutil.GwRef("infra", "tcp"), testutil.GwRef("kgw-infra", "tcp")
			tcpListener := []objects.Listener{{Name: "tcp", Port: 9000, Protocol: "TCP"}}
			_, srv := start(t, []runtime.Object{
				testutil.Namespace("infra", nil), testutil.Namespace("shop", nil), testutil.Namespace("kgw-infra", nil),
				d, testutil.ControllerPod(d, "controller-0", true), up, testutil.ControllerPod(up, "kgateway-0", true),
				testutil.GatewayClass("acme", acmeController, true), testutil.GatewayClass("kgateway", testutil.Controller, true),
				testutil.Gateway("infra", "edge", "acme", nil),
				testutil.HTTPRoute("shop", "r", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, acmeController)),
				testutil.GatewayWithListeners("infra", "tcp", "acme", tcpListener, testutil.Listener{Name: "tcp", Attached: 1}),
				tcpRoute("infra", "t", acmeTCP, acmeController),
				testutil.GatewayWithListeners("kgw-infra", "tcp", "kgateway", tcpListener, testutil.Listener{Name: "tcp", Attached: 1}),
				tcpRoute("kgw-infra", "t", upTCP, testutil.Controller),
			}, nil, Options{PollInterval: 50 * time.Millisecond, MetricsInterval: time.Hour, UI: ui}, &stubScraper{},
				extension.WithDetectors(baseDetector{base: tc.base}))
			// routes waits until every row of id's routes list has a computed outcome.
			routes := func(id string, want int) map[string]*api.Attachment {
				var rows api.Page[api.RouteRow]
				eventually(t, id+" routes never computed", func() bool {
					rows = api.Page[api.RouteRow]{}
					return getJSON(t, srv, "/api/v1/installs/"+id+"/routes", &rows) == http.StatusOK && len(rows.Items) == want &&
						!slices.ContainsFunc(rows.Items, func(r api.RouteRow) bool { return len(r.Parents) != 1 || r.Parents[0].Computed == nil })
				})
				byKind := map[string]*api.Attachment{}
				for _, r := range rows.Items {
					byKind[r.Ref.Kind] = r.Parents[0].Computed
				}
				return byKind
			}
			// experimentalOnly reads the classification from a TCPRoute's computed outcome.
			experimentalOnly := func(id string, a *api.Attachment) bool {
				t.Helper()
				switch {
				case a.Computed.Reason == string(computed.UnknownSetting) && a.Outcome == "":
					return true
				case a.Computed.Reason == "" && a.Outcome == "accepted":
					return false
				}
				t.Fatalf("%s TCPRoute: unexpected outcome %+v", id, a)
				return false
			}
			acme := routes("acme-system.controller", 2)
			if c := acme["HTTPRoute"].Computed; c.Rules != "kgateway-2.4" || c.Note != tc.note {
				t.Fatalf("rules %q note %q, want kgateway-2.4 %q", c.Rules, c.Note, tc.note)
			}
			if got := experimentalOnly("acme", acme["TCPRoute"]); got != tc.acmeExp {
				t.Fatalf("acme TCPRoute experimental-only = %v, want %v", got, tc.acmeExp)
			}
			if got := experimentalOnly("upstream", routes("kgw.kgateway", 1)["TCPRoute"]); got != tc.upstreamExp {
				t.Fatalf("upstream TCPRoute experimental-only = %v, want %v", got, tc.upstreamExp)
			}
		})
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
	var restricted *api.FindingDTO
	for i := range h.Findings {
		if f := h.Findings[i]; f.Resource != nil && f.Resource.Ref == denied {
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

// D-020: rules registered with extension.WithComputedRules reach the engine
// through Build: a 2.4 install computes with them, as the API shows.
func TestComputedRulesSeamReachesTheEngine(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	gw := testutil.GwRef("infra", "edge")
	var asked atomic.Value
	down := func(v string) computed.Selection {
		asked.Store(v)
		return computed.Selection{Rules: kgw23.New(), Note: "Downstream rules"}
	}
	_, srv := start(t, []runtime.Object{
		testutil.Namespace("kgw", nil), testutil.Namespace("infra", nil), testutil.Namespace("shop", nil),
		d, testutil.ControllerPod(d, "kgateway-0", true), testutil.ProxyDeployment("infra", "edge", 1, 1),
		testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.Gateway("infra", "edge", "kgateway", nil),
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
	}, nil, Options{PollInterval: 50 * time.Millisecond, MetricsInterval: time.Hour, UI: ui}, &stubScraper{}, extension.WithComputedRules(down))

	var g api.GatewayDetail
	eventually(t, "gateway detail never computed", func() bool {
		g = api.GatewayDetail{}
		return getJSON(t, srv, "/api/v1/installs/kgw.kgateway/gateways/infra/edge", &g) == http.StatusOK && g.RuleSet != ""
	})
	if g.RuleSet != "kgateway-2.3" || asked.Load() != "2.4.7" || len(g.Listeners) == 0 {
		t.Fatalf("ruleSet %q for version %v, %d listeners; want the downstream kgateway-2.3", g.RuleSet, asked.Load(), len(g.Listeners))
	}
	for _, l := range g.Listeners {
		if l.Computed == nil || l.Computed.Rules != "kgateway-2.3" || l.Computed.Note != "Downstream rules" {
			t.Errorf("%s: computed %+v", l.Name, l.Computed)
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
	eventually(t, "no metrics result for the install", func() bool { _, ok := a.collector.Get("kgw.kgateway"); return ok })
}

// slowPods holds the Pod list until release closes, so the Deployment is
// cached before its Pods, as an unsettled informer start can leave it.
type slowPods struct {
	*kubefake.Clientset
	release chan struct{}
}

func (k slowPods) CoreV1() typedcorev1.CoreV1Interface {
	return slowCoreV1{k.Clientset.CoreV1(), k.release}
}

type slowCoreV1 struct {
	typedcorev1.CoreV1Interface
	release chan struct{}
}

func (c slowCoreV1) Pods(ns string) typedcorev1.PodInterface {
	return slowPodList{c.CoreV1Interface.Pods(ns), c.release}
}

type slowPodList struct {
	typedcorev1.PodInterface
	release chan struct{}
}

func (p slowPodList) List(ctx context.Context, o metav1.ListOptions) (*corev1.PodList, error) {
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return p.PodInterface.List(ctx, o)
}

// A model from a cache that has not settled must not start the collector: its
// install may still lack its Pods, and the collector would then record "no
// ready controller pod" and wait a full interval (here an hour) to retry.
func TestMetricsCollectionWaitsForASettledDiscovery(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	dyn, kube := testutil.Clients(t, testutil.Namespace("kgw", nil), d, testutil.ControllerPod(d, "kgateway-0", true))
	release := make(chan struct{})
	early := make(chan bool, 1) // whether discovery was signalled on the unsettled model
	var a *App
	onLoop := func(ev engine.LoopEvent) {
		if ev.Kind != engine.EvPublished || len(ev.Published.Model.Installs) == 0 || ev.Published.Model.Snapshot.Synced {
			return
		}
		select {
		case <-release: // already released
			return
		default:
		}
		select {
		case <-a.discovered:
			early <- true
		default:
			early <- false
		}
		close(release)
	}
	sc := &stubScraper{}
	var err error
	a, err = Build(Clients{Dynamic: dyn, Kube: slowPods{kube, release}, Discovery: kube.Discovery(), Scraper: sc},
		Options{PollInterval: 20 * time.Millisecond, MetricsInterval: time.Hour, UI: ui, OnLoop: onLoop}, extension.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.Start(ctx)
	select {
	case e := <-early:
		if e {
			t.Fatal("discovery was signalled on a model whose cache had not settled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no model with the install was published before the Pods were listed")
	}
	var r ctrlmetrics.Result
	eventually(t, "no metrics result for the install", func() bool { var ok bool; r, ok = a.collector.Get("kgw.kgateway"); return ok })
	if r.Err != "" || sc.calls.Load() == 0 {
		t.Fatalf("the first collection must scrape the install's pod: err=%q calls=%d", r.Err, sc.calls.Load())
	}
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

// The delegation edge cap reaches the engine; zero keeps the default and a
// negative cap is a start-up error (D-025).
func TestBuildSetsTheDelegationEdgeCap(t *testing.T) {
	dyn, kube := testutil.Clients(t)
	c := Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: &stubScraper{}}
	o := Options{PollInterval: time.Second, MetricsInterval: time.Second, UI: ui}
	a, err := Build(c, o, extension.New())
	if err != nil || a.cfg.DelegationBounds != (computed.Bounds{}) {
		t.Fatalf("zero must keep the engine's default bounds: %+v, %v", a.cfg.DelegationBounds, err)
	}
	o.DelegationEdgeCap = 1234
	if a, err = Build(c, o, extension.New()); err != nil || a.cfg.DelegationBounds != (computed.Bounds{Edges: 1234}) {
		t.Fatalf("the cap must set Edges only: %+v, %v", a.cfg.DelegationBounds, err)
	}
	o.DelegationEdgeCap = -1
	if _, err := Build(c, o, extension.New()); err == nil || !strings.Contains(err.Error(), "delegation edge cap") {
		t.Fatalf("a negative cap must fail: %v", err)
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
func kindsFinding(h api.HealthResponse) (*api.FindingDTO, *health.CheckResult) {
	var f *api.FindingDTO
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
		return slices.ContainsFunc(h.Findings, func(f api.FindingDTO) bool { return f.Code == health.CodeSelfManagedProxy })
	})
}

// An object change recomputes through the cache's trigger: neither the safety
// timer nor a confirm tick can explain the run, both being an hour away.
func TestAppRecomputesOnObjectChange(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	dyn, kube := testutil.ClientsWith(t, nil, testutil.Namespace("kgw", nil), testutil.Namespace("shop", nil), d,
		testutil.ControllerPod(d, "kgateway-0", true), testutil.GatewayClass("kgateway", testutil.Controller, true))
	events := make(chan engine.LoopEvent, 1024)
	var overflow atomic.Bool
	sc := &stubScraper{}
	a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: sc},
		Options{PollInterval: time.Hour, MetricsInterval: time.Hour, ConfirmEvery: time.Hour, UI: ui, OnLoop: func(e engine.LoopEvent) {
			select {
			case events <- e:
			default:
				overflow.Store(true)
			}
		}}, extension.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.Start(ctx)

	// Start-up, the sync notify and the first metrics result each recompute;
	// wait for them, then for a quiet loop.
	eventually(t, "never synced and scraped", func() bool {
		p := a.Engine.Current()
		_, scraped := a.collector.Get("kgw.kgateway")
		return a.cache.Settled() && p != nil && p.Model.Snapshot.Synced && scraped
	})
	for quiet := false; !quiet; {
		select {
		case <-events:
		case <-time.After(time.Second): // longer than the 250 ms debounce plus a compute
			quiet = true
		}
	}

	gvr := testutil.ServedUpstream()[0].GVR()
	for _, s := range testutil.ServedUpstream() {
		if s.Kind == "HTTPRoute" {
			gvr = s.GVR()
		}
	}
	if _, err := dyn.Resource(gvr).Namespace("shop").Create(ctx, testutil.HTTPRoute("shop", "added", nil), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	var since []engine.LoopEventKind
	for {
		e := testutil.Recv(t, events, "a run whose snapshot holds the new route")
		if e.Kind == engine.EvConfirmed || e.Kind == engine.EvConfirmRefused {
			t.Fatalf("no confirm tick may run within the hour: %+v", e)
		}
		if e.Kind != engine.EvPublished {
			since = append(since, e.Kind)
			continue
		}
		if !holdsRoute(e.Published.Model.Snapshot, gvr, "shop", "added") {
			since = nil
			continue
		}
		// One trigger per event; a second trigger repeats the Triggered, Armed pair.
		want := []engine.LoopEventKind{engine.EvTriggered, engine.EvArmed}
		for len(since) > 3 && slices.Equal(since[:2], want) {
			since = since[2:]
		}
		if !slices.Equal(since, append(want, engine.EvRunStarted)) {
			t.Fatalf("events before the publication: %v, want Triggered, Armed, RunStarted", since)
		}
		break
	}
	if overflow.Load() {
		t.Fatal("loop event buffer overflowed")
	}
}

// holdsRoute reports whether snap holds ns/name under gvr.
func holdsRoute(snap *cache.Snapshot, gvr schema.GroupVersionResource, ns, name string) bool {
	return slices.ContainsFunc(snap.Objects[gvr], func(u *unstructured.Unstructured) bool { return u.GetNamespace() == ns && u.GetName() == name })
}

// A registration error stops Build before any component is constructed.
func TestBuildFailsOnRegistryError(t *testing.T) {
	dyn, kube := testutil.ClientsWith(t, nil)
	reg := extension.New(extension.WithChecks("KGWUI-", acmeCheck{}))
	a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery()}, Options{PollInterval: time.Second, MetricsInterval: time.Second, UI: ui}, reg)
	if err == nil || !strings.Contains(err.Error(), "KGWUI-") || a != nil {
		t.Fatalf("Build must return the registry error and no app, got %v, %v", a, err)
	}
}

// A bad extension pattern or name fails Build, so no App exists to start;
// a good one is served under /api/ext/{name}/ with Deps wired to the engine.
func TestBuildRejectsBadExtensionEndpoints(t *testing.T) {
	dyn, kube := testutil.Clients(t)
	c := Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: &stubScraper{}}
	o := Options{PollInterval: time.Second, MetricsInterval: 7 * time.Second, UI: ui}
	with := func(name, pattern string) extension.Option {
		return extension.WithHandlers(name, func(r extension.Router, _ extension.Deps) { r.Handle(pattern, http.NotFoundHandler()) })
	}
	for _, opt := range []extension.Option{with("acme", "GET /../v1/meta"), with("acme", "hello"), with("Acme", "GET /x")} {
		if a, err := Build(c, o, extension.New(opt)); err == nil || a != nil {
			t.Errorf("want a Build error and no App, got App %v, err %v", a != nil, err)
		}
	}

	var sawNil bool
	good := extension.WithHandlers("acme", func(r extension.Router, d extension.Deps) {
		r.Handle("GET /hi", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			sawNil = d.Current() == nil // not started: no publication yet
			w.WriteHeader(http.StatusNoContent)
		}))
	})
	a, err := Build(c, o, extension.New(good))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ext/acme/hi", nil))
	if rec.Code != http.StatusNoContent || !sawNil || a.Engine.Current() != nil {
		t.Fatalf("GET /api/ext/acme/hi: %d, Current nil %v", rec.Code, sawNil)
	}
}

// A capability that collides with a core one fails Build, and a valid one
// reaches both /api/v1/meta and the extension's Deps.
func TestBuildCapabilities(t *testing.T) {
	dyn, kube := testutil.Clients(t)
	c := Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: &stubScraper{}}
	o := Options{PollInterval: time.Second, MetricsInterval: 7 * time.Second, UI: ui}
	clash := func() []api.Capability { return []api.Capability{{Name: "scope", State: api.CapOn}} }
	if _, err := Build(c, o, extension.New(extension.WithCapabilities(clash))); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("Build with a platform name: %v", err)
	}
	good := func() []api.Capability { return []api.Capability{{Name: "acme-widgets", State: api.CapOn}} }
	var deps extension.Deps
	a, err := Build(c, o, extension.New(extension.WithCapabilities(good), extension.WithHandlers("acme", func(_ extension.Router, d extension.Deps) { deps = d })))
	if err != nil {
		t.Fatal(err)
	}
	if got := deps.Capabilities(); len(got) != 1 || got[0].Name != "acme-widgets" {
		t.Errorf("Deps.Capabilities = %+v", got)
	}
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/meta", nil))
	var meta api.MetaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil || !slices.Contains(meta.Capabilities, "acme-widgets") || len(meta.CapabilityStates) != 1 {
		t.Errorf("meta: %v %+v", err, meta)
	}
}

// fakeCollector publishes one value, then waits for ctx or returns fail.
type fakeCollector struct {
	name    string
	fail    error
	started chan extension.Deps
	done    chan struct{}
}

func (c *fakeCollector) Name() string { return c.name }
func (c *fakeCollector) Start(ctx context.Context, d extension.Deps) error {
	defer close(c.done)
	d.Publish("k", c.name, 1, 0)
	c.started <- d
	if c.fail != nil {
		return c.fail
	}
	<-ctx.Done()
	return nil
}

// Collectors start with the app and publish into the store the engine reads.
// One that returns an error has its values expired and is not restarted; the
// others stop with the app's context.
func TestCollectorStartsAndStopsWithApp(t *testing.T) {
	dyn, kube := testutil.Clients(t)
	events := make(chan engine.LoopEvent, 4096)
	ok := &fakeCollector{name: "acme", started: make(chan extension.Deps, 1), done: make(chan struct{})}
	bad := &fakeCollector{name: "broken", fail: errors.New("boom"), started: make(chan extension.Deps, 2), done: make(chan struct{})}
	var handlerDeps extension.Deps
	a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: &stubScraper{}},
		Options{PollInterval: time.Hour, MetricsInterval: time.Hour, ConfirmEvery: time.Hour, UI: ui, OnLoop: func(e engine.LoopEvent) {
			select {
			case events <- e:
			default:
			}
		}}, extension.New(extension.WithCollectors(ok, bad), extension.WithHandlers("acme", func(_ extension.Router, d extension.Deps) { handlerDeps = d })))
	if err != nil {
		t.Fatal(err)
	}
	if handlerDeps.Publish != nil || handlerDeps.Fail != nil {
		t.Fatal("handlers must not get Publish or Fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.Start(ctx)
	if d := testutil.Recv(t, ok.started, "acme to start"); d.Current == nil || d.Logger == nil || d.Fail == nil {
		t.Fatalf("collector Deps: %+v", d)
	}
	testutil.Recv(t, bad.started, "broken to start")
	testutil.Recv(t, bad.done, "broken to return")
	for {
		e := testutil.Recv(t, events, "a run over acme's value and broken's expiry")
		if e.Kind != engine.EvPublished {
			continue
		}
		snap := a.collected.Snapshot()
		good, _ := snap.Get("acme/k")
		gone, _ := snap.Get("broken/k")
		if good.State == collect.OK && good.Data == "acme" && gone.State == collect.Unavailable && e.Published.Source.Collected == snap.Print {
			break
		}
	}
	select {
	case <-bad.started:
		t.Fatal("a collector that returned an error must not restart")
	default:
	}
	cancel()
	testutil.Recv(t, ok.done, "acme to stop with the app")
}

// Without collectors the engine reads no store, so the source print stays zero.
func TestNoCollectorsLeavesCollectedUnset(t *testing.T) {
	a := build(t)
	if a.cfg.Collected != nil || a.collected != nil {
		t.Fatal("no collectors must leave the engine's Collected unset")
	}
}

// probeCollector publishes v1, hands its Deps to the test and runs until
// stop is closed or the app stops.
type probeCollector struct {
	deps chan extension.Deps
	stop chan struct{}
}

func (*probeCollector) Name() string { return "probe" }
func (c *probeCollector) Start(ctx context.Context, d extension.Deps) error {
	d.Publish("k", "v1", 1, 0)
	c.deps <- d
	select {
	case <-ctx.Done():
	case <-c.stop:
	}
	return nil
}

// probeCheck reports the probe's value as PROBE-001; a value that is not OK
// leaves the check failed with its state and reason.
type probeCheck struct{}

func (probeCheck) Name() string { return "probe" }
func (probeCheck) Run(in health.Input) ([]health.Finding, health.CheckResult) {
	v, _ := in.Collected.Get("probe/k")
	if v.State != collect.OK {
		return nil, health.CheckResult{Name: "probe", CollectedAt: in.Now, Error: fmt.Sprintf("%s: %s", v.State, v.Reason)}
	}
	return []health.Finding{{Code: "PROBE-001", Slug: "probe", Severity: health.SeverityInfo, Install: in.Install.ID(), Message: fmt.Sprint(v.Data)}},
		health.CheckResult{Name: "probe", OK: true, CollectedAt: v.At}
}

// probeOf is what /health says about the probe: its finding, or its check's error.
func probeOf(h api.HealthResponse) string {
	for _, f := range h.Findings {
		if f.Code == "PROBE-001" {
			return "finding " + f.Message
		}
	}
	for _, c := range h.Checks {
		if c.Name == "probe" {
			return "error " + c.Error
		}
	}
	return "absent"
}

// D-019 at the API: while a collector value is newer than the published
// model, a confirm tick leaves /health's collectedAt as it is and the old
// findings are served; the recompute then serves the new value's findings.
func TestCollectedFreshnessAtTheAPI(t *testing.T) {
	type fixture struct {
		clk    *clocktesting.FakeClock
		deps   extension.Deps
		col    *probeCollector
		health func() api.HealthResponse
		await  func(...engine.LoopEventKind) engine.LoopEvent
		hold   func() (release func())
	}
	setup := func(t *testing.T) *fixture {
		d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
		col := &probeCollector{deps: make(chan extension.Deps, 1), stop: make(chan struct{})}
		events := make(chan engine.LoopEvent, 4096)
		// gate, when set, blocks the loop on the next refused tick, so nothing publishes until it is released.
		var gate atomic.Pointer[chan struct{}]
		f := &fixture{clk: testutil.FakeClock(), col: col}
		a, srv := start(t, []runtime.Object{testutil.Namespace("kgw", nil), d, testutil.ControllerPod(d, "kgateway-0", true),
			testutil.GatewayClass("kgateway", testutil.Controller, true)}, nil,
			Options{PollInterval: time.Hour, MetricsInterval: time.Hour, UI: ui, Clock: f.clk, OnLoop: func(e engine.LoopEvent) {
				select {
				case events <- e:
				default:
				}
				if g := gate.Load(); e.Kind == engine.EvConfirmRefused && g != nil && gate.CompareAndSwap(g, nil) {
					<-*g
				}
			}}, &stubScraper{}, extension.WithCollectors(col), extension.WithChecks("PROBE-", probeCheck{}))
		f.deps = testutil.Recv(t, col.deps, "the probe collector to start")
		f.health = func() api.HealthResponse {
			t.Helper()
			var h api.HealthResponse
			if c := getJSON(t, srv, "/api/v1/installs/kgw.kgateway/health", &h); c != http.StatusOK {
				t.Fatalf("GET health: %d", c)
			}
			return h
		}
		f.await = func(kinds ...engine.LoopEventKind) engine.LoopEvent {
			t.Helper()
			for {
				if e := testutil.Recv(t, events, "loop event"); slices.Contains(kinds, e.Kind) {
					return e
				}
			}
		}
		f.hold = func() func() {
			g := make(chan struct{})
			gate.Store(&g)
			var once sync.Once
			release := func() { once.Do(func() { close(g) }) }
			t.Cleanup(release)
			return release
		}
		// Settled: two ticks in a row confirm a model with the install, its metrics and v1.
		ready := func(p *engine.Published) bool {
			reads := p.Model.MetricsRead
			return p.Model.Snapshot.Synced && len(p.Model.Installs) == 1 && len(reads) > 0 &&
				!slices.ContainsFunc(reads, func(r engine.MetricsRead) bool { return !r.Present }) && p.Source.Collected == a.collected.Snapshot().Print
		}
		for confirmed := 0; confirmed < 2; {
			f.await(engine.EvArmed)
			f.clk.Step(engine.ConfirmEvery)
			if e := f.await(engine.EvConfirmed, engine.EvConfirmRefused); e.Kind == engine.EvConfirmed && ready(e.Published) {
				confirmed++
			} else {
				confirmed = 0
			}
		}
		if got := probeOf(f.health()); got != "finding v1" {
			t.Fatalf("settled probe = %q, want finding v1", got)
		}
		return f
	}
	// changeThenRecompute applies change; the next tick is refused with the old
	// findings and collectedAt still served, and the recompute then serves want.
	changeThenRecompute := func(t *testing.T, f *fixture, change func(), want string) api.HealthResponse {
		t.Helper()
		before := f.health()
		change()
		f.await(engine.EvTriggered)
		f.await(engine.EvArmed)
		release := f.hold()
		f.clk.Step(engine.ConfirmEvery)
		if e := f.await(engine.EvConfirmRefused, engine.EvConfirmed); e.Kind == engine.EvConfirmed {
			t.Fatal("a tick over a changed collector value must not confirm")
		}
		held := f.health()
		if probeOf(held) != probeOf(before) || !held.CollectedAt.Equal(before.CollectedAt) {
			t.Fatalf("a refused tick must change nothing: probe %q at %v, want %q at %v", probeOf(held), held.CollectedAt, probeOf(before), before.CollectedAt)
		}
		release()
		f.await(engine.EvPublished)
		after := f.health()
		if probeOf(after) != want || !after.CollectedAt.After(before.CollectedAt) {
			t.Fatalf("after the recompute: probe %q at %v, want %q after %v", probeOf(after), after.CollectedAt, want, before.CollectedAt)
		}
		return after
	}

	t.Run("newer publication", func(t *testing.T) {
		f := setup(t)
		changeThenRecompute(t, f, func() { f.deps.Publish("k", "v2", 2, 0) }, "finding v2")
	})
	t.Run("failing value", func(t *testing.T) {
		f := setup(t)
		changeThenRecompute(t, f, func() { f.deps.Fail("k", collect.Failing, "probe refused") }, "error failing: probe refused")
	})
	t.Run("stopped collector", func(t *testing.T) {
		f := setup(t)
		changeThenRecompute(t, f, func() { close(f.col.stop) }, "error unavailable: collector stopped")
	})
	t.Run("expired value", func(t *testing.T) {
		f := setup(t)
		expires := f.clk.Now().Add(30 * time.Second)
		changeThenRecompute(t, f, func() { f.deps.Publish("k", "v2", 2, 30*time.Second) }, "finding v2")
		// Before the expiry a tick still renews collectedAt.
		f.await(engine.EvArmed)
		f.clk.Step(expires.Add(-time.Second).Sub(f.clk.Now()))
		f.await(engine.EvConfirmed)
		live := f.health()
		if probeOf(live) != "finding v2" || !live.CollectedAt.Equal(f.clk.Now()) {
			t.Fatalf("before the expiry: probe %q at %v, want finding v2 at %v", probeOf(live), live.CollectedAt, f.clk.Now())
		}
		// At the expiry the tick is refused; the run at the expiry serves the value unavailable.
		f.await(engine.EvArmed)
		release := f.hold()
		f.clk.Step(time.Second)
		if e := f.await(engine.EvConfirmRefused, engine.EvConfirmed); e.Kind == engine.EvConfirmed {
			t.Fatal("the tick at the expiry must not confirm")
		}
		if held := f.health(); probeOf(held) != "finding v2" || !held.CollectedAt.Equal(live.CollectedAt) {
			t.Fatalf("at the expiry: probe %q at %v, want finding v2 at %v", probeOf(held), held.CollectedAt, live.CollectedAt)
		}
		release()
		f.await(engine.EvPublished)
		if after := f.health(); probeOf(after) != "error unavailable: expired" || !after.CollectedAt.Equal(expires) {
			t.Fatalf("after the expiry run: probe %q at %v, want unavailable at %v", probeOf(after), after.CollectedAt, expires)
		}
	})
}
