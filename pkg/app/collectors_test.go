package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/api"
	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/ctrllogs"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/extension"
	"github.com/DuncanDoyle/snellius/pkg/proxystats"
)

// recordingScraper serves an empty payload and records each scraped Pod.
type recordingScraper struct {
	mu   sync.Mutex
	pods []string
}

func (s *recordingScraper) Scrape(_ context.Context, p *corev1.Pod, _ int32) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pods = append(s.pods, p.Namespace+"/"+p.Name)
	return io.NopCloser(strings.NewReader("")), nil
}

func (s *recordingScraper) scraped() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Compact(slices.Sorted(slices.Values(s.pods)))
}

// names are the collectors' names, in order.
func names(cs []extension.Collector) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name())
	}
	return out
}

// installsOf is the install IDs of a collected value, keyed by install ID.
func installsOf(v collect.Value) []string {
	var ids []string
	switch d := v.Data.(type) {
	case ctrlmetrics.Results:
		for id := range d.ByInstall {
			ids = append(ids, id)
		}
	case proxystats.Stats:
		for id := range d {
			ids = append(ids, id)
		}
	case ctrllogs.Logs:
		for id := range d {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// The kgateway pack's collectors run in the default binary and cover only the
// installs the pack claimed: a class-only install is never scraped for
// metrics, proxy stats or logs (Phase 5 design §4.3, §7.1).
func TestPackCollectorsCoverOnlyTheirInstalls(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	dyn, kube := testutil.ClientsWith(t, nil, testutil.Namespace("kgw", nil), testutil.Namespace("infra", nil), d, testutil.ControllerPod(d, "kgateway-0", true),
		testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.Gateway("infra", "edge", "kgateway", nil),
		testutil.GatewayClass("other", "example.com/gateway", true), testutil.Gateway("infra", "side", "other", nil))
	sc := &recordingScraper{}
	a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: sc},
		Options{PollInterval: 20 * time.Millisecond, MetricsInterval: time.Hour, UI: ui, ClassOnlyInstalls: true, Diagnostics: Diagnostics{ControllerLogs: true}},
		extension.New())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(a.collectors), []string{ctrlmetrics.CollectorName, proxystats.CollectorName, ctrllogs.CollectorName}; !slices.Equal(got, want) {
		t.Fatalf("pack collectors %v, want %v", got, want)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.Start(ctx)
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	var insts []api.InstallSummary
	eventually(t, "both installs listed", func() bool {
		insts = nil
		return getJSON(t, srv, "/api/v1/installs", &insts) == http.StatusOK && len(insts) == 2
	})
	want := []string{"kgw.kgateway"}
	for _, key := range []string{ctrlmetrics.Key, proxystats.Key, ctrllogs.Key} {
		eventually(t, key+" covers the kgateway install", func() bool {
			v, ok := a.collected.Snapshot().Get(key)
			return ok && v.State == collect.OK && len(installsOf(v)) > 0
		})
		v, _ := a.collected.Snapshot().Get(key)
		if got := installsOf(v); !slices.Equal(got, want) {
			t.Errorf("%s covers %v, want %v", key, got, want)
		}
	}
	if got := sc.scraped(); !slices.Equal(got, []string{"kgw/kgateway-0"}) {
		t.Errorf("scraped %v, want the kgateway controller only", got)
	}
}

// The core alone registers no pack collector: no metrics, proxy stats or
// controller-logs collector starts, and nothing is scraped (design §7.4).
func TestCoreRegistryStartsNoPackCollector(t *testing.T) {
	dyn, kube := testutil.ClientsWith(t, nil, testutil.Namespace("infra", nil),
		testutil.GatewayClass("other", "example.com/gateway", true), testutil.Gateway("infra", "side", "other", nil))
	sc := &recordingScraper{}
	a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: sc},
		Options{PollInterval: 20 * time.Millisecond, MetricsInterval: time.Hour, UI: ui, ClassOnlyInstalls: true, Diagnostics: Diagnostics{ControllerLogs: true}},
		extension.NewCore())
	if err != nil {
		t.Fatal(err)
	}
	if len(a.collectors) != 0 {
		t.Fatalf("pack collectors %v, want none", names(a.collectors))
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.Start(ctx)
	eventually(t, "the class-only install over a synced cache", func() bool {
		p := a.Engine.Current()
		return p != nil && p.Model.Snapshot.Synced && len(p.Model.Installs) == 1
	})
	if vs := a.collected.Snapshot().Values; len(vs) != 0 || len(sc.scraped()) != 0 {
		t.Fatalf("collected %v, scraped %v; want nothing", vs, sc.scraped())
	}
}

// Review 5.2 #3: the kgateway pack's metrics collector runs on the app's
// clock: its results carry that clock's time and its ticker follows it.
func TestMetricsCollectorRunsOnTheAppClock(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	dyn, kube := testutil.Clients(t, testutil.Namespace("kgw", nil), d, testutil.ControllerPod(d, "kgateway-0", true))
	clk := clocktesting.NewFakeClock(testutil.Now)
	sc := &stubScraper{}
	a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: sc},
		Options{PollInterval: time.Hour, MetricsInterval: time.Minute, UI: ui, Clock: clk}, extension.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.Start(ctx)
	var r ctrlmetrics.Result
	// The loop debounces on the fake clock, so each poll moves it a second.
	eventually(t, "no metrics result", func() bool {
		clk.Step(time.Second)
		var ok bool
		r, ok = a.metrics("kgw.kgateway")
		return ok
	})
	if r.CollectedAt.Before(testutil.Now) || r.CollectedAt.After(clk.Now()) {
		t.Fatalf("collected at %v, want a time of the app's clock in [%v, %v]", r.CollectedAt, testutil.Now, clk.Now())
	}
	first, calls := r.CollectedAt, sc.calls.Load()
	clk.Step(time.Minute)
	eventually(t, "no scrape on the app clock's next tick", func() bool {
		r, _ = a.metrics("kgw.kgateway")
		return sc.calls.Load() > calls && r.CollectedAt.After(first)
	})
}
