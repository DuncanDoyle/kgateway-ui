package engine

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kgateway"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

const otherController = "example.com/gateway"

// kgwDetector is the upstream detector stamping the kgateway pack, as extension.New wraps it.
type kgwDetector struct{ install.UpstreamDetector }

func (kgwDetector) Detect(d *appsv1.Deployment) (install.Install, bool) {
	in, ok := install.UpstreamDetector{}.Detect(d)
	in.Impl = kgateway.Name
	return in, ok
}

// packHooks gives kgateway installs the kgateway pack's hooks and the rest the core's.
func packHooks(i install.Install) Hooks {
	if i.Impl != kgateway.Name {
		return Hooks{}
	}
	return Hooks{Scope: kgateway.Scope(), Settings: kgateway.Settings, RulesFor: kgateway.RulesFor, Version: kgateway.Version}
}

// twoControllers is snapshot plus the class "other" of example.com/gateway, a
// Gateway shop/other of that class and a route attached to it.
func twoControllers(t *testing.T) *cache.Snapshot {
	t.Helper()
	other := testutil.GwRef("shop", "other")
	return snapshot(t, testutil.GatewayClass("other", otherController, true),
		testutil.Gateway("shop", "other", "other", nil, testutil.Listener{Name: "http", Attached: 1, Conds: []metav1.Condition{
			testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}}),
		testutil.HTTPRoute("shop", "web", []objects.ParentRef{other}, testutil.AcceptedBy(other, otherController)))
}

func classOnlyConfig(snap *cache.Snapshot, on bool) Config {
	cfg := config(snap, metricsAt(testutil.Now), func() time.Time { return testutil.Now })
	cfg.Detectors, cfg.HooksFor, cfg.ClassOnly = []install.Detector{kgwDetector{}}, packHooks, on
	return cfg
}

// With class-only installs on, a controller no detector claims is an install
// of its own, known from its GatewayClass; kgateway's controller gets no twin.
func TestClassOnlyInstallsAtTheEngine(t *testing.T) {
	m := New(classOnlyConfig(twoControllers(t), true)).Compute()
	if len(m.Installs) != 2 {
		t.Fatalf("%d installs, want kgateway and one class-only", len(m.Installs))
	}
	var co *InstallModel
	for i := range m.Installs {
		if m.Installs[i].Resolved.Impl == "" {
			co = &m.Installs[i]
		}
	}
	if co == nil {
		t.Fatal("no class-only install")
	}
	r := co.Resolved
	if r.ID() != "example.com~2Fgateway" || r.Display() != "Gateway API controller example.com/gateway" || r.Deployment != nil ||
		r.Scope.Mode != install.ScopeAll || r.Scope.Source != install.SourceAssumed {
		t.Fatalf("class-only install %+v", r)
	}
	if co.Owned == nil || len(co.Owned.Gateways) != 1 || co.Owned.Gateways[0].GetName() != "other" || len(co.Owned.Routes) != 1 {
		t.Fatalf("class-only install owns %+v, want shop/other and its route", co.Owned)
	}
	for _, i := range m.Installs {
		if i.Resolved.Impl == kgateway.Name && (len(i.Owned.Gateways) != 1 || i.Owned.Gateways[0].GetName() != "edge") {
			t.Fatalf("kgateway install owns %+v, want infra/edge only", i.Owned.Gateways)
		}
	}
	if m := New(classOnlyConfig(twoControllers(t), false)).Compute(); len(m.Installs) != 1 || m.Installs[0].Resolved.Impl != kgateway.Name {
		t.Fatalf("class-only installs off: %d installs, want the kgateway one", len(m.Installs))
	}
}

// Every upstream check runs on a class-only install without a panic. Those
// that read the controller workload, proxies or processes are not available
// for it and raise nothing.
func TestChecksOnAClassOnlyInstall(t *testing.T) {
	cfg := classOnlyConfig(twoControllers(t), true)
	cfg.Metrics = ctrlmetrics.Static{}
	cfg.Pinned = "example.com~2Fgateway"
	m := New(cfg).Compute()
	if len(m.Installs) != 1 {
		t.Fatalf("pinned: %d installs", len(m.Installs))
	}
	h := m.Installs[0].Health
	for _, name := range []string{"controller pods", "gateway proxies"} {
		c, ok := checkResult(h, name)
		if !ok || c.OK || c.Required || c.Unavailable != health.AvailNotObserved || c.Error != health.NotForThisController {
			t.Errorf("%s: %+v, want not available for this controller", name, c)
		}
	}
	for _, f := range h.Findings {
		switch f.Code {
		case health.CodeControllerNotReady, health.CodeProxyNotReady, health.CodeSelfManagedProxy, health.CodeProcessRestarting, health.CodeProbeFailing, health.CodeRolloutStuck:
			t.Errorf("finding %s on a class-only install: %+v", f.Code, f)
		}
	}
}

func checkResult(h health.Report, name string) (health.CheckResult, bool) {
	for _, c := range h.Checks {
		if c.Name == name {
			return c, true
		}
	}
	return health.CheckResult{}, false
}
