package health

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/proxystats"
)

// wantHold fails unless check name in r is ok and pending as given and holds count items until until.
func wantHold(t *testing.T, r Report, name string, ok, pending bool, until time.Time, count int, reason string) {
	t.Helper()
	c := checkNamed(r, name)
	if c == nil {
		t.Fatalf("no check %q", name)
	}
	if c.OK != ok || c.Pending != pending || c.Hold == nil || !c.Hold.Until.Equal(until) || c.Hold.Count != count || c.Hold.Reason != reason {
		t.Fatalf("check %q: ok %v pending %v hold %+v; want ok %v pending %v hold {%v %d %q}", name, c.OK, c.Pending, c.Hold, ok, pending, until, count, reason)
	}
}

// wantNoHold fails when check name in r has a hold.
func wantNoHold(t *testing.T, r Report, name string) {
	t.Helper()
	if c := checkNamed(r, name); c == nil || c.Hold != nil {
		t.Fatalf("check %q: %+v, want no hold", name, c)
	}
}

// 018: 3 of 20 objects stuck holds the check OK until the longest-stuck
// object's start + 5 min; then the finding replaces the hold.
func TestHoldStatusProgress(t *testing.T) {
	const reason = "Waiting to see if status catches up"
	rs := progressRuns(t, progressWorld(20, 3, 0), nil, 0, 2*time.Minute, 5*time.Minute+time.Second)
	for i, r := range rs[:2] {
		if fs := notProgressing(r); len(fs) != 0 {
			t.Fatalf("run %d: 018 during the hold: %+v", i, fs)
		}
		wantHold(t, r, "status progress", true, false, testutil.Now.Add(5*time.Minute), 3, reason)
	}
	if fs := notProgressing(rs[2]); len(fs) != 1 {
		t.Fatalf("after the hold: %+v", rs[2].Findings)
	}
	wantNoHold(t, rs[2], "status progress")
}

// 018: when the longest-stuck objects recover, until moves to the next-longest one's start + 5 min.
func TestHoldStatusProgressMovesWithChurn(t *testing.T) {
	const reason = "Waiting to see if status catches up"
	w := progressWorld(22, 0, 0)
	rt := Runtime{States: NewStates()}
	gen := func(from, to int, g int64) {
		for _, o := range w.objs[2+from : 2+to] {
			o.SetGeneration(g)
		}
	}
	run := func(off time.Duration) Report {
		in := w.input(t)
		in.Now = testutil.Now.Add(off)
		return Evaluate(in, Upstream(), rt)
	}
	gen(0, 3, 2)
	run(0)
	gen(3, 6, 2)
	wantHold(t, run(time.Minute), "status progress", true, false, testutil.Now.Add(5*time.Minute), 6, reason)
	gen(0, 3, 1)
	wantHold(t, run(2*time.Minute), "status progress", true, false, testutil.Now.Add(6*time.Minute), 3, reason)
}

// 023: one Service with two empty ports, first seen 20 s and 10 s ago, holds
// 2 until the earlier due time; then the first is raised beside the held second.
func TestHoldBackendEndpoints(t *testing.T) {
	const reason = "Waiting for backends to get ready endpoints"
	e, st := newEndpointsWorld(), NewStates()
	e.svcs[0].Ports = append(e.svcs[0].Ports, cache.ServicePort{Name: "grpc", Port: 9090, Target: 9090})
	e.slices[0].Ports = append(e.slices[0].Ports, cache.SlicePort{Name: "grpc", Port: 9090})
	e.slices[0].Ready = []int{0, 0}
	e.run(t, st, 0)
	withRefs(e.w.objs[2], backendRef("cart", 8080, nil), backendRef("cart", 9090, nil))
	e.run(t, st, 10*time.Second)
	r := e.run(t, st, 20*time.Second)
	if fs := endpointFindings(r); len(fs) != 0 {
		t.Fatalf("at 20 s: %+v", fs)
	}
	wantHold(t, r, endpointsCheckName, true, false, testutil.Now.Add(30*time.Second), 2, reason)
	r = e.run(t, st, 31*time.Second)
	if fs := endpointFindings(r); len(fs) != 1 || fs[0].Facts.(*EndpointsEvidence).Port != 8080 {
		t.Fatalf("at 31 s: %+v", fs)
	}
	wantHold(t, r, endpointsCheckName, true, false, testutil.Now.Add(40*time.Second), 1, reason)
	r = e.run(t, st, 41*time.Second)
	if fs := endpointFindings(r); len(fs) != 2 {
		t.Fatalf("at 41 s: %+v", fs)
	}
	wantNoHold(t, r, endpointsCheckName)
}

// 025/026: a Gateway whose Ready proxy holds the condition is one held item
// for 60 s; edge-1 is not Ready, so it adds nothing.
func TestHoldProxyStats(t *testing.T) {
	cases := []struct {
		code, check, reason string
		apply               func(*proxystats.Pod)
	}{
		{CodeProxyDisconnected, "proxy connection", "Waiting for proxies to reconnect", disconnected},
		{CodeProxyConfigWarming, "proxy config warming", "Waiting for proxies to finish loading config", warming},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			st := NewStates()
			col := collected(testutil.Now, edgeStats(statsPod("edge-0", c.apply), statsPod("edge-1", c.apply)))
			run := func(at time.Time) Report {
				return Evaluate(statsInput(t, at), Upstream(), freshMetrics(Runtime{States: st, Collected: col}))
			}
			wantHold(t, run(testutil.Now), c.check, true, false, testutil.Now.Add(ProxyConditionAge), 1, c.reason)
			wantHold(t, run(testutil.Now.Add(30*time.Second)), c.check, true, false, testutil.Now.Add(ProxyConditionAge), 1, c.reason)
			r := run(testutil.Now.Add(61 * time.Second))
			if findingOn(r, c.code) == nil {
				t.Fatalf("at 61 s: %+v", r.Findings)
			}
			wantNoHold(t, r, c.check)
		})
	}
}

// A policy whose target is missing, grace over, inside TargetHold: resource status is OK with a hold.
func TestHoldPolicyTarget(t *testing.T) {
	tp := targetPolicy("infra", []map[string]any{tRef("Gateway", "nope", "")}, nil)
	r := targetsRun(t, rules24, []*unstructured.Unstructured{tp}, 5*time.Second)
	if fs := targetFindings(r, "infra/tp"); len(fs) != 0 {
		t.Fatalf("016 inside the hold: %+v", fs)
	}
	wantHold(t, r, "resource status", true, false, t0.Add(Grace+5*time.Second+TargetHold), 1, "Waiting for policy targets to appear")
	r = targetsRun(t, rules24, []*unstructured.Unstructured{tp}, 5*time.Second, 36*time.Second)
	if fs := targetFindings(r, "infra/tp"); len(fs) != 1 {
		t.Fatalf("after the hold: %+v", r.Findings)
	}
	wantNoHold(t, r, "resource status")
}

// Resource status with 3 resources in grace: pending as today, plus a hold
// until the earliest grace end; none once grace is over.
func TestHoldResourceGrace(t *testing.T) {
	w := healthyWorld()
	for i, age := range []time.Duration{10, 20, 30} {
		r := testutil.HTTPRoute("shop", "young"+string(rune('a'+i)), []objects.ParentRef{testutil.GwRef("infra", "edge")})
		r.SetCreationTimestamp(metav1.NewTime(testutil.Now.Add(-age * time.Second)))
		w.objs = append(w.objs, r)
	}
	r := w.report(t)
	if c := checkNamed(r, "resource status"); c == nil || c.Error != "3 resources are being processed by the controller" || r.State != StateUnknown {
		t.Fatalf("state %s check %+v", r.State, c)
	}
	wantHold(t, r, "resource status", false, true, testutil.Now.Add(30*time.Second), 3, "Waiting for the controller to process resources")
	in := w.input(t)
	in.Now = testutil.Now.Add(61 * time.Second)
	wantNoHold(t, Evaluate(in, Upstream(), freshMetrics(Runtime{})), "resource status")
}

// Gateway proxies with a proxy not found yet: pending as today, plus a hold until its grace end.
func TestHoldGatewayProxiesGrace(t *testing.T) {
	w := healthyWorld()
	w.deploys = w.deploys[:1]
	w.firstSeen = func(*unstructured.Unstructured) time.Time { return testutil.Now.Add(-10 * time.Second) }
	r := w.report(t)
	if c := checkNamed(r, "gateway proxies"); c == nil || c.Error != "1 proxy not found yet" {
		t.Fatalf("check %+v", c)
	}
	wantHold(t, r, "gateway proxies", false, true, testutil.Now.Add(50*time.Second), 1, "Waiting for proxy deployments to appear")
	in := w.input(t)
	in.Now = testutil.Now.Add(51 * time.Second)
	r = Evaluate(in, Upstream(), freshMetrics(Runtime{}))
	if !has(r, CodeProxyNotReady, "", "infra/edge") {
		t.Fatalf("after grace: %+v", r.Findings)
	}
	wantNoHold(t, r, "gateway proxies")
}

// xDS rejects during the restart hold: pending as today, plus a hold of the two
// Gateways the captures reject on, until the hold end; none after it.
func TestHoldXDSRestart(t *testing.T) {
	const reason = "Waiting to see if rejects return after a controller restart"
	t1 := testutil.Now.Add(10 * time.Second)
	w := xdsWorld(t)
	w.pods[1] = ctrlStatus(w.pods[1], corev1.ContainerStatus{State: running(testutil.Old.Time)})
	rejecting := w.metrics
	st := NewStates()
	xdsAt(t, w, st, testutil.Now)
	w.pods[1] = testutil.ControllerPod(w.ctrl, "ctrl-c", true)
	w.metrics = resultAt(t1, cleared(rejecting.Metrics))
	for _, at := range []time.Duration{0, 59 * time.Second} {
		wantHold(t, xdsAt(t, w, st, t1.Add(at)), "xDS rejects", false, true, t1.Add(RestartHold), 2, reason)
	}
	wantNoHold(t, xdsAt(t, w, st, t1.Add(61*time.Second)), "xDS rejects")
}

// Holds merge per check: the earliest until, the summed count, distinct
// reasons in order; a due time not after Now is ignored, and each until wakes the report.
func TestHoldAggregation(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	held := funcCheck{name: "held", run: func(ctx RunContext) ([]Finding, CheckResult) {
		ctx.Hold(now.Add(20*time.Second), 1, "a")
		ctx.Hold(now.Add(10*time.Second), 2, "b")
		ctx.Hold(now.Add(30*time.Second), 1, "a")
		ctx.Hold(now, 5, "due") // due already: ignored
		return nil, CheckResult{Name: "held", OK: true}
	}}
	plain := funcCheck{name: "plain"}
	r := Evaluate(Input{Now: now}, []Check{held, plain}, Runtime{})
	wantHold(t, r, "held", true, false, now.Add(10*time.Second), 4, "a; b")
	wantNoHold(t, r, "plain")
	if !r.Next.Equal(now.Add(10 * time.Second)) {
		t.Fatalf("Next %v, want the earliest until", r.Next)
	}
}

// A raised finding that clears later is no wait: a 019 inside its window carries no hold.
func TestNoHoldOnFindingDecay(t *testing.T) {
	w := healthyWorld()
	at := testutil.Now.Add(-time.Minute)
	w.pods[0] = ctrlStatus(w.pods[0], corev1.ContainerStatus{RestartCount: 1, State: running(at), LastTerminationState: exited("OOMKilled", 137, at)})
	r := Evaluate(processInput(t, w), Upstream(), freshMetrics(Runtime{States: NewStates()}))
	if find(r, CodeProcessRestarting) == nil || r.Next.IsZero() {
		t.Fatalf("want 019 with a wake: %+v", r.Findings)
	}
	for _, c := range r.Checks {
		if c.Hold != nil {
			t.Fatalf("check %q holds %+v", c.Name, c.Hold)
		}
	}
}
