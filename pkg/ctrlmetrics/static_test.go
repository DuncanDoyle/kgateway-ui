package ctrlmetrics_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/extension"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/setup"
)

// staticEngine is a one-shot engine over one ready controller, reading src.
func staticEngine(t *testing.T, src ctrlmetrics.Static) (*engine.Engine, *clocktesting.FakeClock) {
	t.Helper()
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	dyn, kube := testutil.Clients(t, testutil.Namespace("kgw", nil), d, testutil.ControllerPod(d, "kgateway-0", true))
	clk := clocktesting.NewFakeClock(testutil.Now)
	cfg, err := setup.Build(setup.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery()}, setup.Options{MetricsInterval: 10 * time.Second, Clock: clk}, extension.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ca := cache.New(cfg.Cache)
	ca.Start(ctx)
	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 10*time.Second, true, func(context.Context) (bool, error) { return ca.Settled(), nil }); err != nil {
		t.Fatal("cache did not settle")
	}
	cfg.Engine.Snapshots, cfg.Engine.Metrics = ca, src
	return engine.New(cfg.Engine), clk
}

// xdsAt computes at now and returns the xDS rejects check result.
func xdsAt(t *testing.T, e *engine.Engine, clk *clocktesting.FakeClock, now time.Time) health.CheckResult {
	t.Helper()
	clk.SetTime(now)
	m := e.Compute()
	if len(m.Installs) != 1 {
		t.Fatalf("installs %d", len(m.Installs))
	}
	for _, c := range m.Installs[0].Health.Checks {
		if c.Name == "xDS rejects" {
			return c
		}
	}
	t.Fatal("no xDS rejects check")
	return health.CheckResult{}
}

// D-053: a Static result is read as the Collector's would be; the engine's
// MetricsMaxAge (3 × the interval) decides staleness, not the source.
func TestStaticStalenessIsTheEngines(t *testing.T) {
	collected := testutil.Now.Add(-time.Minute)
	maxAge := setup.MetricsMaxAge(10 * time.Second)
	for _, tc := range []struct {
		name  string
		at    time.Time
		fresh bool
	}{
		{"max age - 1 s", collected.Add(maxAge - time.Second), true},
		{"max age + 1 s", collected.Add(maxAge + time.Second), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, clk := staticEngine(t, ctrlmetrics.Static{"kgw.kgateway": {CollectedAt: collected}})
			c := xdsAt(t, e, clk, tc.at)
			if c.OK != tc.fresh {
				t.Fatalf("xDS rejects ok %v (%q); want %v", c.OK, c.Error, tc.fresh)
			}
			if !tc.fresh && !strings.Contains(c.Error, "stale") {
				t.Fatalf("error %q; want stale", c.Error)
			}
		})
	}
}

// An install the Static source holds no result for reads as not collected.
func TestStaticWithoutAResult(t *testing.T) {
	src := ctrlmetrics.Static{"kgw.other": {CollectedAt: testutil.Now}}
	if _, ok := src.Get("kgw.kgateway"); ok {
		t.Fatal("Get found a result for an install it does not hold")
	}
	e, clk := staticEngine(t, src)
	if c := xdsAt(t, e, clk, testutil.Now); c.OK || !strings.Contains(c.Error, "not collected") {
		t.Fatalf("xDS rejects %+v; want not collected", c)
	}
}
