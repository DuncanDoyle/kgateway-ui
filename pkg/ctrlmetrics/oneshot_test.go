package ctrlmetrics_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/extension"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/setup"
)

// publishedEngine is a one-shot engine over one ready controller, with r published.
func publishedEngine(t *testing.T, r ctrlmetrics.Results) (*engine.Engine, *clocktesting.FakeClock) {
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
	ctrlmetrics.Publish(cfg.Collected, r)
	cfg.Engine.Snapshots = ca
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

// D-053, E8: a published result is read as the Collector's would be; its
// MaxAge (3 × the interval) decides staleness per entry, not the store.
func TestPublishedStalenessIsTheEntrys(t *testing.T) {
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
			e, clk := publishedEngine(t, ctrlmetrics.Results{ByInstall: map[string]ctrlmetrics.Result{"kgw.kgateway": {CollectedAt: collected}}, MaxAge: maxAge})
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

// An install the published value holds no result for reads as not collected.
func TestPublishedWithoutAResult(t *testing.T) {
	e, clk := publishedEngine(t, ctrlmetrics.Results{ByInstall: map[string]ctrlmetrics.Result{"kgw.other": {CollectedAt: testutil.Now}}})
	if c := xdsAt(t, e, clk, testutil.Now); c.OK || !strings.Contains(c.Error, "not collected") {
		t.Fatalf("xDS rejects %+v; want not collected", c)
	}
}

// CollectOnce scrapes each install once; Publish stores the value with no
// expiry under Key, and Lookup finds each entry with the max age.
func TestCollectOnceAndPublish(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "kgateway:v2.4.7", 1, 1)
	insts := []install.Install{
		{Namespace: "kgw", Name: "a", Pods: testutil.PodList(testutil.ControllerPod(d, "a", true)), MetricsPort: 9092},
		{Namespace: "kgw", Name: "b", Pods: testutil.PodList(testutil.ControllerPod(d, "b", false))},
	}
	r := ctrlmetrics.CollectOnce(context.Background(), bodies{"a": ""}, insts, time.Minute)
	if len(r.ByInstall) != 2 || r.ByInstall["kgw.a"].Err != "" || r.ByInstall["kgw.b"].Err == "" || r.MaxAge != time.Minute {
		t.Fatalf("results %+v", r)
	}
	store := collect.New(nil, func() time.Time { return testutil.Now })
	ctrlmetrics.Publish(store, r)
	v, ok := store.Snapshot().Get(ctrlmetrics.Key)
	if !ok || v.State != collect.OK || !v.Expires.IsZero() || v.Fingerprint != r.Fingerprint() {
		t.Fatalf("published %+v", v)
	}
	if got, maxAge, ok := ctrlmetrics.Lookup(v, "kgw.a"); !ok || maxAge != time.Minute || got.Err != "" {
		t.Fatalf("lookup kgw.a: %+v %v %v", got, maxAge, ok)
	}
	if _, _, ok := ctrlmetrics.Lookup(v, "kgw.c"); ok {
		t.Fatal("lookup found an install it does not hold")
	}
}

// bodies serves a body per Pod name.
type bodies map[string]string

func (b bodies) Scrape(_ context.Context, p *corev1.Pod, _ int32) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(b[p.Name])), nil
}
