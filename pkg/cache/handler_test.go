// The engine imports pkg/cache, so this test lives in an external test package.
package cache_test

import (
	"context"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
)

// noMetrics has no result for any install.
type noMetrics struct{}

func (noMetrics) Get(string) (ctrlmetrics.Result, bool) { return ctrlmetrics.Result{}, false }

// Codex 3: with the real informer handler blocked after the store took an
// update, the confirm tick refuses on the store fingerprint, and the
// recompute it schedules publishes the change; no handler has notified.
func TestBlockedHandlerStillRefusesAndPublishes(t *testing.T) {
	route := testutil.HTTPRoute("shop", "cart", nil)
	route.SetResourceVersion("1")
	dyn, kube := testutil.Clients(t, testutil.Namespace("shop", nil), route)
	served := testutil.ServedUpstream()
	gvr := served[0].GVR()
	for _, s := range served {
		if s.Kind == "HTTPRoute" {
			gvr = s.GVR()
		}
	}
	clk := testutil.FakeClock()
	sig := trigger.New()
	c := cache.New(cache.Config{Dynamic: dyn, Kube: kube, Served: served, Changes: sig, Now: clk.Now})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	for deadline := time.Now().Add(testutil.RecvTimeout); !c.Settled(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) { // start-up only; the assertions below use acknowledgements
			t.Fatal("cache did not settle")
		}
	}
	testutil.Drain(sig.C())

	ev := make(chan engine.LoopEvent, 256)
	e := engine.New(engine.Config{Snapshots: c, Served: served, Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour), Metrics: noMetrics{},
		Checks: health.Upstream(), Triggers: sig, Clock: clk, OnLoop: func(l engine.LoopEvent) { ev <- l }})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		e.Run(ctx, time.Hour)
	}()
	defer func() {
		cancel()
		testutil.Recv(t, stopped, "Run to return")
	}()
	await := func(k engine.LoopEventKind) engine.LoopEvent {
		t.Helper()
		for {
			l := testutil.Recv(t, ev, "loop event")
			if l.Kind == engine.EvTriggered {
				t.Fatalf("no handler may notify while blocked: %+v", l)
			}
			if l.Kind == k {
				return l
			}
		}
	}
	await(engine.EvPublished)
	await(engine.EvArmed)

	// The handler acknowledges entry, which client-go reaches after the store update.
	entered, release := make(chan struct{}, 1), make(chan struct{})
	defer close(release) // never leaves the handler blocked
	var once sync.Once
	cache.SetHandlerHook(c, func() {
		once.Do(func() {
			entered <- struct{}{}
			<-release
		})
	})
	updated := route.DeepCopy()
	updated.SetResourceVersion("2")
	if _, err := dyn.Resource(gvr).Namespace("shop").Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	testutil.Recv(t, entered, "the handler, after the store update")

	clk.Step(engine.ConfirmEvery)
	await(engine.EvConfirmRefused)
	await(engine.EvArmed)
	clk.Step(engine.Quiet)
	p := await(engine.EvPublished).Published
	var rv string
	for _, u := range p.Model.Snapshot.Objects[gvr] {
		if u.GetNamespace() == "shop" && u.GetName() == "cart" {
			rv = u.GetResourceVersion()
		}
	}
	if rv != "2" {
		t.Fatalf("the publication holds resourceVersion %q, want 2", rv)
	}
}
