package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/clock"
	clocktesting "k8s.io/utils/clock/testing"
	"sigs.k8s.io/yaml"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/internal/testutil/healthtest"
	"github.com/DuncanDoyle/snellius/pkg/api"
	"github.com/DuncanDoyle/snellius/pkg/app"
	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/extension"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
)

// The README's extension example is this program, so the build compiles it.
func TestReadmeShowsThisExample(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(readme), "## Extending")
	if ok {
		_, block, ok = strings.Cut(block, "```go\n")
	}
	if ok {
		block, _, ok = strings.Cut(block, "```\n")
	}
	if !ok {
		t.Fatal("README has no Go block under ## Extending")
	}
	if block != string(src) {
		t.Fatal("README's extension example differs from examples/downstream/main.go; copy main.go into it")
	}
}

// The example's options and extensions build, and the app serves its UI.
func TestExampleBuildsAndServes(t *testing.T) {
	ui, err := fs.Sub(dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	dyn, kube := testutil.Clients(t)
	a, err := app.Build(app.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery()}, options(ui), extension.New(extensions(clock.RealClock{})...))
	if err != nil {
		t.Fatalf("the example must build: %v", err)
	}
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "example") {
		t.Fatalf("GET /: %d %q", rec.Code, rec.Body.String())
	}
}

// emptyScraper serves an empty /metrics body; the fake clientset cannot proxy to Pods.
type emptyScraper struct{}

func (emptyScraper) Scrape(context.Context, *corev1.Pod, int32) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

// tickerClock is a fake clock that reports each ticker it makes, so a step
// never runs before the collector waits on it.
type tickerClock struct {
	*clocktesting.FakeClock
	made chan struct{}
}

func (c tickerClock) NewTicker(d time.Duration) clock.Ticker {
	tk := c.FakeClock.NewTicker(d)
	c.made <- struct{}{}
	return tk
}

func acmePolicyObject(ns, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "acme.example.com/v1", "kind": "AcmePolicy",
		"metadata": map[string]any{"name": name, "namespace": ns, "uid": "acme/" + ns + "/" + name},
	}}
}

// The example drives every Go seam through the production path: its
// collector feeds its check, its endpoint applies the Authorizer, its
// capability reaches /api/v1/meta, and its prefix registers once.
func TestExampleUsesEverySeam(t *testing.T) {
	ui, err := fs.Sub(dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	dyn, kube := testutil.ClientsWith(t, []kinds.Descriptor{acmePolicy}, testutil.Namespace("kgw", nil), testutil.Namespace("shop", nil),
		testutil.Namespace("acme-system", nil), d, testutil.ControllerPod(d, "kgateway-0", true), testutil.GatewayClass("kgateway", testutil.Controller, true))
	clients := app.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: emptyScraper{}}

	// The check declares its collector key; its finding in /health below proves the read works.
	if got := (acmeWidgetCheck{}).Inputs().Collected; !slices.Equal(got, []string{"acme-widgets/widgets"}) {
		t.Fatalf("acme widgets declares collected keys %v", got)
	}

	// The prefix argument registers once: a second ACME- registration fails Build.
	twice := append(extensions(clock.RealClock{}), extension.WithChecks("ACME-", acmeWidgetCheck{}))
	if a, err := app.Build(clients, options(ui), extension.New(twice...)); err == nil || a != nil || !strings.Contains(err.Error(), "ACME-") {
		t.Fatalf("a second ACME- prefix must fail Build, got %v, %v", a != nil, err)
	}

	clk := tickerClock{FakeClock: testutil.FakeClock(), made: make(chan struct{}, 1)}
	events := make(chan engine.LoopEvent, 4096)
	o := options(ui)
	o.ConfirmEvery = time.Hour
	o.OnLoop = func(e engine.LoopEvent) {
		select {
		case events <- e:
		default:
		}
	}
	a, err := app.Build(clients, o, extension.New(extensions(clk)...))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.Start(ctx)
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	testutil.Recv(t, clk.made, "the widget collector's ticker")

	// published waits for a publication that satisfies ok.
	published := func(what string, ok func(*engine.Published) bool) {
		t.Helper()
		if p := a.Engine.Current(); p != nil && ok(p) {
			return
		}
		for {
			if e := testutil.Recv(t, events, what); e.Kind == engine.EvPublished && ok(e.Published) {
				return
			}
		}
	}
	hasInstall := func(p *engine.Published) bool { return p.Model.Snapshot.Synced && len(p.Model.Installs) == 1 }
	published("a model with the install", hasInstall)

	// No AcmePolicy: one tick publishes 0 widgets, and one recompute later /health holds ACME-001.
	// The core proxy stats collector also publishes, so runs are told apart by the acme check's own result.
	testutil.Drain(events)
	clk.Step(widgetsEvery)
	published("a run over 0 widgets", func(p *engine.Published) bool {
		return hasInstall(p) && acmeOK(p) && modelHas(p, "ACME-001")
	})
	if !healthHas(t, srv, "ACME-001") {
		t.Fatal("0 widgets must raise ACME-001")
	}

	gvr := acmePolicyGVR
	testutil.Drain(events)
	for _, u := range []*unstructured.Unstructured{acmePolicyObject("shop", "discount"), acmePolicyObject("acme-system", "internal")} {
		if _, err := dyn.Resource(gvr).Namespace(u.GetNamespace()).Create(ctx, u, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	published("a model with both AcmePolicies", func(p *engine.Published) bool { return len(p.Model.Snapshot.Objects[gvr]) == 2 })

	// The endpoint lists what the caller may see: acme-system objects are hidden.
	var list widgetList
	if code := getJSON(t, srv, "/api/ext/acme/widgets", &list); code != http.StatusOK {
		t.Fatalf("GET /api/ext/acme/widgets: %d", code)
	}
	if want := []widget{{Namespace: "shop", Name: "discount"}}; !slices.Equal(list.Widgets, want) {
		t.Fatalf("widgets = %+v, want %+v", list.Widgets, want)
	}

	// The next tick counts them, and the finding clears.
	testutil.Drain(events)
	clk.Step(widgetsEvery)
	published("a run over 2 widgets", func(p *engine.Published) bool {
		return hasInstall(p) && acmeOK(p) && !modelHas(p, "ACME-001")
	})
	if healthHas(t, srv, "ACME-001") {
		t.Fatal("2 widgets must clear ACME-001")
	}

	var meta api.MetaResponse
	if code := getJSON(t, srv, "/api/v1/meta", &meta); code != http.StatusOK || !slices.Contains(meta.Capabilities, "acme-widgets") {
		t.Fatalf("meta: %d %+v", code, meta)
	}
}

// modelHas reports whether p's first install has a finding with code.
func modelHas(p *engine.Published, code string) bool {
	return slices.ContainsFunc(p.Model.Installs[0].Health.Findings, func(f health.Finding) bool { return f.Code == code })
}

// acmeOK reports whether p's acme widgets check read a widget count.
func acmeOK(p *engine.Published) bool {
	return slices.ContainsFunc(p.Model.Installs[0].Health.Checks, func(c health.CheckResult) bool { return c.Name == "acme widgets" && c.OK })
}

// healthHas reports whether the install's /health lists a finding with code.
func healthHas(t *testing.T, srv *httptest.Server, code string) bool {
	t.Helper()
	var h api.HealthResponse
	if c := getJSON(t, srv, "/api/v1/installs/kgw.kgateway/health", &h); c != http.StatusOK {
		t.Fatalf("GET health: %d", c)
	}
	return slices.ContainsFunc(h.Findings, func(f api.FindingDTO) bool { return f.Code == code })
}

// getJSON decodes GET path into v and returns the status code.
func getJSON(t *testing.T, srv *httptest.Server, path string, v any) int {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode
}

// The chart overlay grants the UI read access to acme's policy kind.
func TestChartOverlayRendersExtraRule(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("REQUIRE_HELM") == "1" {
			t.Fatalf("helm not on PATH and REQUIRE_HELM=1: %v", err)
		}
		t.Skip("helm not on PATH")
	}
	out, err := exec.Command("helm", "template", "ui", "../../deploy/helm/snellius", "-n", "snellius", "-f", "chart/values-acme.yaml").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	want := rbacv1.PolicyRule{APIGroups: []string{"acme.example.com"}, Resources: []string{"acmepolicies"}, Verbs: []string{"get", "list", "watch"}}
	for _, doc := range strings.Split(string(out), "\n---") {
		var cr rbacv1.ClusterRole
		if err := yaml.Unmarshal([]byte(doc), &cr); err != nil || cr.Kind != "ClusterRole" {
			continue
		}
		if slices.ContainsFunc(cr.Rules, func(r rbacv1.PolicyRule) bool {
			return slices.Equal(r.APIGroups, want.APIGroups) && slices.Equal(r.Resources, want.Resources) && slices.Equal(r.Verbs, want.Verbs)
		}) {
			return
		}
		t.Fatalf("the ClusterRole lacks %+v: %+v", want, cr.Rules)
	}
	t.Fatal("no ClusterRole rendered")
}

// The example check reads only the collector key it declares.
func TestAcmeWidgetCheckReadsOnlyDeclaredInputs(t *testing.T) {
	in := health.Input{Now: testutil.Now}
	values := map[string]collect.Value{
		"acme-widgets/other": {State: collect.OK, Data: 7},
		"other-collector/x":  {State: collect.OK, Data: "x"},
	}
	cases := map[string]collect.Value{
		"no widgets":  {State: collect.OK, Data: 0},
		"two widgets": {State: collect.OK, Data: 2},
		"failing":     {State: collect.Failing, Reason: "list failed"},
		"absent":      {},
	}
	for name, v := range cases {
		for _, seed := range []int64{1, 2, 3, 7, 42} {
			t.Run(fmt.Sprintf("%s/seed%d", name, seed), func(t *testing.T) {
				vs := maps.Clone(values)
				if name != "absent" {
					vs["acme-widgets/widgets"] = v
				}
				rt := health.Runtime{Collected: collect.Snapshot{Values: vs}}
				healthtest.CheckDeclaredInputs(t, acmeWidgetCheck{}, in, rt, seed)
			})
		}
	}
}

// afterClock is a fake clock that reports each After, so a test steps the
// proxy stats collector's first-round retry only once it waits.
type afterClock struct {
	*clocktesting.FakeClock
	afters chan struct{}
}

func (c afterClock) After(d time.Duration) <-chan time.Time {
	ch := c.FakeClock.After(d)
	select {
	case c.afters <- struct{}{}:
	default:
	}
	return ch
}

// A downstream Source with its own targets and a JSON stats format feeds
// KGWUI-026 through /health; the core collector owns the interval and publishing.
func TestDownstreamProxyStatsSource(t *testing.T) {
	ui, err := fs.Sub(dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	gw := testutil.Gateway("shop", "edge", "kgateway", []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")})
	dyn, kube := testutil.ClientsWith(t, []kinds.Descriptor{acmePolicy}, testutil.Namespace("kgw", nil), testutil.Namespace("shop", nil),
		d, testutil.ControllerPod(d, "kgateway-0", true), testutil.GatewayClass("kgateway", testutil.Controller, true), gw)
	var connected atomic.Bool
	var fetched atomic.Value
	fetches := make(chan struct{}, 64)
	src := acmeProxyStats{fetch: func(_ context.Context, addr string) ([]byte, error) {
		fetched.Store(addr)
		select {
		case fetches <- struct{}{}:
		default:
		}
		return json.Marshal(acmeStats{Connected: connected.Load(), State: "live"})
	}}
	clk := afterClock{FakeClock: testutil.FakeClock(), afters: make(chan struct{}, 64)}
	events := make(chan engine.LoopEvent, 4096)
	o := options(ui)
	o.Clock, o.MetricsInterval = clk, time.Minute
	o.OnLoop = func(e engine.LoopEvent) {
		select {
		case events <- e:
		default:
		}
	}
	opts := append(extensions(clk), extension.WithProxyStatsSource(src))
	a, err := app.Build(app.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: emptyScraper{}}, o, extension.New(opts...))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.Start(ctx)
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	// The first rounds find no synced model and retry after a second.
	for scraped := false; !scraped; {
		select {
		case <-fetches:
			scraped = true
		case <-clk.afters:
			clk.Step(time.Second)
		case <-time.After(10 * time.Second):
			t.Fatal("the acme proxy was never scraped")
		}
	}
	// until steps the clock past the debounce each time the engine waits, until ok holds.
	until := func(what string, ok func(*engine.Published) bool) {
		t.Helper()
		for {
			if e := testutil.Recv(t, events, what); e.Kind != engine.EvArmed {
				continue
			}
			if p := a.Engine.Current(); p != nil && len(p.Model.Installs) == 1 && ok(p) {
				return
			}
			clk.Step(engine.MaxDelay)
		}
	}
	until("a run over the acme stats", func(p *engine.Published) bool {
		return slices.ContainsFunc(p.Model.Installs[0].Health.Checks, func(c health.CheckResult) bool { return c.Name == "proxy connection" && c.OK })
	})
	if healthHas(t, srv, health.CodeProxyDisconnected) {
		t.Fatal("a disconnect younger than 60 s raised 026")
	}
	clk.Step(61 * time.Second)
	until("the disconnected proxy's finding", func(p *engine.Published) bool { return modelHas(p, health.CodeProxyDisconnected) })
	var h api.HealthResponse
	getJSON(t, srv, "/api/v1/installs/kgw.kgateway/health", &h)
	i := slices.IndexFunc(h.Findings, func(f api.FindingDTO) bool { return f.Code == health.CodeProxyDisconnected })
	if i < 0 || !strings.Contains(h.Findings[i].Detail, "edge-node") || fetched.Load() != "edge.shop.acme.internal:7000" {
		t.Fatalf("health %+v, fetched %v", h.Findings, fetched.Load())
	}
	connected.Store(true)
	clk.Step(time.Minute)
	until("the reconnected proxy", func(p *engine.Published) bool { return !modelHas(p, health.CodeProxyDisconnected) })
	if healthHas(t, srv, health.CodeProxyDisconnected) {
		t.Fatal("a reconnected proxy clears the finding")
	}
}

// An acme rule set wrapping kgw24 through WithComputedRules, without
// PolicyTargeter, builds and serves its views; a policy whose only target is
// missing raises no computed KGWUI-016 under it. The same world on the shipped
// kgw24 raises one, so the absence is acme's doing (R91).
func TestDownstreamRuleSet(t *testing.T) {
	if pt := acmeRuleSet("2.4.7").PolicyTargeter(); pt != nil {
		t.Fatalf("acme's rule set must not offer PolicyTargeter, got %T", pt)
	}
	if set, has := missingTarget016(t, acmeRuleSet); set != "acme-2.4" || has {
		t.Fatalf("acme: ruleSet %q, KGWUI-016 %v; want acme-2.4 and none", set, has)
	}
	if set, has := missingTarget016(t, nil); set != "kgateway-2.4" || !has {
		t.Fatalf("shipped rules: ruleSet %q, KGWUI-016 %v; want kgateway-2.4 and one", set, has)
	}
}

// missingTarget016 builds the example, with rules when set, over a policy whose
// only target is missing, and runs it past the policy's grace and the 016 hold.
// It returns the Gateway's rule set and whether /health has a KGWUI-016.
func missingTarget016(t *testing.T, rules func(string) computed.Selection) (string, bool) {
	t.Helper()
	ui, err := fs.Sub(dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	tp := testutil.TrafficPolicy("infra", "tp-missing-target")
	if err := unstructured.SetNestedSlice(tp.Object, []any{map[string]any{
		"group": "gateway.networking.k8s.io", "kind": "Gateway", "name": "nope"}}, "spec", "targetRefs"); err != nil {
		t.Fatal(err)
	}
	dyn, kube := testutil.Clients(t, testutil.Namespace("kgw", nil), testutil.Namespace("infra", nil), d,
		testutil.ControllerPod(d, "kgateway-0", true), testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.Gateway("infra", "edge", "kgateway", nil), tp)
	clk := afterClock{FakeClock: testutil.FakeClock(), afters: make(chan struct{}, 64)}
	events := make(chan engine.LoopEvent, 4096)
	o := options(ui)
	o.Clock = clk
	o.OnLoop = func(e engine.LoopEvent) {
		select {
		case events <- e:
		default:
		}
	}
	opts := extensions(clk)
	if rules != nil {
		opts = append(opts, extension.WithComputedRules(rules))
	}
	a, err := app.Build(app.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: emptyScraper{}}, o, extension.New(opts...))
	if err != nil {
		t.Fatalf("the example must build: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.Start(ctx)
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	// until steps the clock past the debounce each time the engine waits, until ok holds.
	until := func(what string, ok func(*engine.Published) bool) {
		t.Helper()
		for {
			if e := testutil.Recv(t, events, what); e.Kind != engine.EvArmed {
				continue
			}
			if p := a.Engine.Current(); p != nil && len(p.Model.Installs) == 1 && ok(p) {
				return
			}
			clk.Step(engine.MaxDelay)
		}
	}
	var at time.Time
	// The hold counts from the first model over the synced cache, which holds the policy.
	until("the first synced model", func(p *engine.Published) bool { at = p.Model.ComputedAt; return p.Model.Snapshot.Synced })
	due := at.Add(health.Grace + health.TargetHold + time.Second)
	clk.Step(health.Grace + health.TargetHold + time.Second)
	until("a model past the hold", func(p *engine.Published) bool { return !p.Model.ComputedAt.Before(due) })
	var g api.GatewayDetail
	if c := getJSON(t, srv, "/api/v1/installs/kgw.kgateway/gateways/infra/edge", &g); c != http.StatusOK {
		t.Fatalf("GET gateway: %d", c)
	}
	return g.RuleSet, healthHas(t, srv, health.CodePolicyTargetNotFound)
}
