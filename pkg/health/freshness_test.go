package health

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// freshness003 returns the evidence of the one KGWUI-003 on res.
func freshness003(t *testing.T, r Report, res string) (Finding, *FreshnessEvidence) {
	t.Helper()
	var got []Finding
	for _, f := range r.Findings {
		if f.Code == CodeRouteUnattached && f.Resource != nil && f.Resource.String() == res {
			got = append(got, f)
		}
	}
	if len(got) != 1 {
		t.Fatalf("want one KGWUI-003 on %s, got %+v", res, r.Findings)
	}
	ev, _ := got[0].Facts.(*FreshnessEvidence)
	if ev == nil {
		t.Fatalf("no freshness evidence: %+v", got[0])
	}
	return got[0], ev
}

// Blocks of one object are judged in turn; a later block whose kind comes
// first in §10.1 order wins, with its detail.
func TestFreshnessFirstKindWinsAcrossBlocks(t *testing.T) {
	w := healthyWorld()
	gw := testutil.Gateway("infra", "edge", "kgateway", nil)
	gw.SetGeneration(2) // stale top-level conditions
	testutil.AddSpecListener(gw, "https")
	w.objs[1] = gw
	f, ev := freshness003(t, w.report(t), "infra/edge")
	if ev.Freshness != FreshnessNeverWritten || ev.Observed != nil || f.Detail != "No status for listener https" {
		t.Fatalf("want never-written for the listener, got %q %+v", f.Detail, ev)
	}
}

// Only a block that is all CRD default, or a Gateway with no conditions, is
// never written; a partly written block is judged by its own conditions.
func TestBlockFreshnessCRDDefault(t *testing.T) {
	dflt := metav1.Condition{Type: "Accepted", Status: metav1.ConditionUnknown, Reason: "Pending", Message: "Waiting for controller", LastTransitionTime: metav1.Unix(0, 0)}
	programmed := testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")
	notEpoch := dflt
	notEpoch.LastTransitionTime = testutil.Old
	u := testutil.Gateway("infra", "edge", "kgateway", nil)
	cases := []struct {
		name string
		cs   []metav1.Condition
		kind string
	}{
		{"all default", []metav1.Condition{dflt}, FreshnessNeverWritten},
		// The default's observedGeneration 0 is stale.
		{"default beside a written condition", []metav1.Condition{dflt, programmed}, FreshnessStale},
		{"Pending, not at the epoch", []metav1.Condition{func() metav1.Condition { c := notEpoch; c.ObservedGeneration = 1; return c }(), programmed}, FreshnessPending},
	}
	for _, tc := range cases {
		if _, kind, _ := blockFreshness(u, tc.cs, gatewayRequired, nil); kind != tc.kind {
			t.Errorf("%s: kind %q, want %q", tc.name, kind, tc.kind)
		}
	}
	// A Gateway with no top-level conditions is never written.
	w := healthyWorld()
	w.objs[1] = testutil.Gateway("infra", "edge", "kgateway", []metav1.Condition{})
	if f, ev := freshness003(t, w.report(t), "infra/edge"); ev.Freshness != FreshnessNeverWritten || f.Detail != noControllerStatus {
		t.Fatalf("want never-written, got %q %+v", f.Detail, ev)
	}
}

// progressWorld is the healthy world with total objects with status: the
// GatewayClass, the Gateway and routes, of which stale and never are routes
// with a KGWUI-003 warning (a stale status, and none from the controller).
func progressWorld(total, stale, never int) *world {
	w := healthyWorld()
	gw := testutil.GwRef("infra", "edge")
	w.objs = w.objs[:2]
	for i := range total - 2 {
		var r *unstructured.Unstructured
		if i < never {
			r = testutil.HTTPRoute("shop", fmt.Sprintf("r%03d", i), []objects.ParentRef{gw})
		} else {
			r = testutil.HTTPRoute("shop", fmt.Sprintf("r%03d", i), []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller))
			if i < never+stale {
				r.SetGeneration(2) // conditions observed generation 1
			}
		}
		w.objs = append(w.objs, r)
	}
	return w
}

// progressRuns evaluates w at testutil.Now plus each offset on one States; at
// sets the metrics collected at an offset, nil leaves them as they are.
func progressRuns(t *testing.T, w *world, at func(off time.Duration) *ctrlmetrics.Result, offsets ...time.Duration) []Report {
	t.Helper()
	rt := Runtime{States: NewStates()}
	var out []Report
	for _, off := range offsets {
		if at != nil {
			w.metrics = at(off)
		}
		in := w.input(t)
		in.Now = testutil.Now.Add(off)
		out = append(out, Evaluate(in, Upstream(), rt))
	}
	return out
}

// notProgressing returns the KGWUI-018 findings of r.
func notProgressing(r Report) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Code == CodeStatusNotProgressing {
			out = append(out, f)
		}
	}
	return out
}

// Design §10.2: 018 matures 5 min after the condition starts; until then the
// check wakes at the deadline. Its evidence counts objects per §10.1 kind.
func TestStatusNotProgressingMatures(t *testing.T) {
	w := progressWorld(20, 8, 2)
	rs := progressRuns(t, w, nil, 0, 4*time.Minute+59*time.Second, 5*time.Minute+time.Second)
	deadline := testutil.Now.Add(progressHold)
	for i, r := range rs[:2] {
		if fs := notProgressing(r); len(fs) != 0 {
			t.Fatalf("run %d: 018 before maturing: %+v", i, fs)
		}
	}
	if !rs[1].Next.Equal(deadline) {
		t.Errorf("at 4:59 next is %v, want %v", rs[1].Next, deadline)
	}
	fs := notProgressing(rs[2])
	if len(fs) != 1 {
		t.Fatalf("want one 018 at 5:01, got %+v", fs)
	}
	f := fs[0]
	if f.Severity != SeverityWarning || f.Resource == nil || *f.Resource != (objects.Ref{Group: "apps", Kind: "Deployment", Namespace: "kgw", Name: "kgateway"}) || f.Slug != "status-not-progressing" ||
		f.Since == nil || !f.Since.Equal(testutil.Now) {
		t.Fatalf("018 %+v", f)
	}
	ev, _ := f.Facts.(*StatusProgressEvidence)
	want := &StatusProgressEvidence{Kinds: []FreshnessCount{{FreshnessNeverWritten, 2}, {FreshnessStale, 8}}, Oldest: testutil.Now}
	if !reflect.DeepEqual(ev, want) {
		t.Fatalf("evidence %+v, want %+v", ev, want)
	}
	if f.Message != "Status is not progressing on 10 objects" || f.Detail != "2 never written, 8 stale" {
		t.Errorf("wording %q / %q", f.Message, f.Detail)
	}
}

// Design §10.2: 018 needs an object whose 003 warning lasted 5 min, not only
// a count over the threshold: objects that recover and are replaced restart it.
func TestStatusNotProgressingNeedsAPersistentObject(t *testing.T) {
	w := progressWorld(22, 10, 0)
	rt := Runtime{States: NewStates()}
	run := func(off time.Duration) Report {
		in := w.input(t)
		in.Now = testutil.Now.Add(off)
		return Evaluate(in, Upstream(), rt)
	}
	run(0)
	// At 4 min the first ten recover and the other ten go stale.
	for i, o := range w.objs[2:] {
		gen := int64(1)
		if i >= 10 {
			gen = 2
		}
		o.SetGeneration(gen)
	}
	run(4 * time.Minute)
	r := run(5*time.Minute + time.Second)
	if fs := notProgressing(r); len(fs) != 0 {
		t.Fatalf("018 with no object stuck for 5 min: %+v", fs)
	}
	if want := testutil.Now.Add(4*time.Minute + progressHold); !r.Next.Equal(want) {
		t.Errorf("next %v, want %v", r.Next, want)
	}
	if fs := notProgressing(run(9*time.Minute + time.Second)); len(fs) != 1 {
		t.Fatalf("want 018 once the second ten are stuck for 5 min, got %+v", fs)
	}
}

// While stuck objects keep rotating, 018 never matures, and the state keeps
// only the objects stuck now.
func TestStatusNotProgressingBoundsHistoryWhileObjectsRotate(t *testing.T) {
	w := progressWorld(22, 10, 0)
	rt := Runtime{States: NewStates()}
	const every = 30 * time.Second
	for off := time.Duration(0); off <= 30*time.Minute; off += every {
		// Every 4 min the stuck ten swap with the other ten.
		half := int(off/(4*time.Minute)) % 2
		for i, o := range w.objs[2:] {
			gen := int64(1)
			if i/10 == half {
				gen = 2
			}
			o.SetGeneration(gen)
		}
		w.metrics = syncsScrape(t, off, int(off/time.Second))
		in := w.input(t)
		in.Now = testutil.Now.Add(off)
		if fs := notProgressing(Evaluate(in, Upstream(), rt)); len(fs) != 0 {
			t.Fatalf("at %v: 018 while objects rotate: %+v", off, fs)
		}
	}
	st, _ := Load[*progressState](rt.States.store(progressCheck{}.Name()), progressKey)
	if st == nil {
		t.Fatal("no 018 state while the condition holds")
	}
	if len(st.stuck) != 10 {
		t.Fatalf("kept %d stuck objects after 30 min, want the 10 stuck now", len(st.stuck))
	}
}

// Design §10.2: at least 10 objects, or 10 % of objects with status with a
// minimum of 3.
func TestStatusNotProgressingThreshold(t *testing.T) {
	for _, tc := range []struct {
		stuck, total int
		want         bool
	}{{9, 200, false}, {10, 200, true}, {2, 10, false}, {3, 30, true}, {3, 31, false}} {
		rs := progressRuns(t, progressWorld(tc.total, tc.stuck, 0), nil, 0, 5*time.Minute+time.Second)
		if got := len(notProgressing(rs[1])) == 1; got != tc.want {
			t.Errorf("%d of %d: 018 %v, want %v", tc.stuck, tc.total, got, tc.want)
		}
	}
}

// Design §10.2: a controller that is not ready raises 005 instead, and the
// condition starts again once it is ready.
func TestStatusNotProgressingNeedsAReadyController(t *testing.T) {
	w := progressWorld(20, 10, 0)
	rt := Runtime{States: NewStates()}
	run := func(off time.Duration, ready int32) Report {
		w.ctrl.Status.ReadyReplicas = ready
		in := w.input(t)
		in.Now = testutil.Now.Add(off)
		return Evaluate(in, Upstream(), rt)
	}
	run(0, 1)
	if r := run(5*time.Minute+time.Second, 0); len(notProgressing(r)) != 0 || !has(r, CodeControllerNotReady, SeverityError, "") {
		t.Fatalf("not ready: want 005 and no 018: %+v", r.Findings)
	}
	if r := run(6*time.Minute, 1); len(notProgressing(r)) != 0 {
		t.Fatalf("ready again: the condition restarts: %+v", notProgressing(r))
	}
	if r := run(11*time.Minute+time.Second, 1); len(notProgressing(r)) != 1 {
		t.Fatalf("5 min after ready again: want 018: %+v", r.Findings)
	}
}

// syncsScrape is a parsed 2.4 scrape at off whose status_syncs_total is n.
func syncsScrape(t *testing.T, off time.Duration, n int) *ctrlmetrics.Result {
	t.Helper()
	body := fmt.Sprintf("# TYPE kgateway_status_syncer_status_syncs_total counter\n"+
		"kgateway_status_syncer_status_syncs_total{name=\"r\",namespace=\"shop\",result=\"success\",syncer=\"HTTPRouteStatusSyncer\"} %d\n", n)
	m, err := ctrlmetrics.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	m.Rejects, m.Managed, m.ManagedPresent = map[ctrlmetrics.GatewayKey]float64{}, map[ctrlmetrics.ManagedKey]float64{}, true
	return &ctrlmetrics.Result{CollectedAt: testutil.Now.Add(off), Metrics: m}
}

// 018 quotes no status-sync count, even from a controller that exports
// status_syncs_total and syncs throughout (D-058 item 10).
func TestStatusNotProgressingNoSyncCount(t *testing.T) {
	at := func(off time.Duration) *ctrlmetrics.Result { return syncsScrape(t, off, int(off/time.Second)) }
	rs := progressRuns(t, progressWorld(20, 10, 0), at, 0, 150*time.Second, 5*time.Minute+time.Second)
	fs := notProgressing(rs[2])
	if len(fs) != 1 || fs[0].Detail != "10 stale" {
		t.Fatalf("want 018 with detail %q, got %+v", "10 stale", fs)
	}
	b, err := json.Marshal(fs[0].Facts)
	if err != nil || bytes.Contains(b, []byte("syncs")) {
		t.Fatalf("evidence %s (%v): want no syncs", b, err)
	}
}

// Info 003s (inside the grace period) are not stuck, so they never start 018.
func TestStatusNotProgressingIgnoresGrace(t *testing.T) {
	w := progressWorld(20, 10, 0)
	w.firstSeen = func(*unstructured.Unstructured) time.Time { return testutil.Now.Add(10 * time.Minute) }
	for _, r := range progressRuns(t, w, nil, 0, 5*time.Minute+time.Second) {
		if fs := notProgressing(r); len(fs) != 0 {
			t.Fatalf("018 from 003s in grace: %+v", fs)
		}
	}
}

// The objects with status are exactly those the resource status check judges:
// an experimental-only route whose status only another controller wrote is out
// (D-018 §1), and an unattached route counts once.
func TestStatusNotProgressingDenominator(t *testing.T) {
	w := healthyWorld()
	gw := testutil.GwRef("infra", "edge")
	foreign := testutil.HTTPRoute("shop", "tcp", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: "example.com/other",
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")}})
	foreign.SetAPIVersion("gateway.networking.k8s.io/v1alpha2")
	foreign.SetKind("TCPRoute")
	in := w.input(t)
	in.Served = slices.Clone(in.Served)
	for i := range in.Served {
		if in.Served[i].Kind == "TCPRoute" {
			in.Served[i].Version = "v1alpha2"
		}
	}
	if !in.ignored(foreign) {
		t.Fatal("fixture: the TCPRoute must be experimental-only with foreign status")
	}
	cart := in.Owned.Routes[0]
	in.Owned = &install.Owned{GatewayClasses: in.Owned.GatewayClasses, Gateways: in.Owned.Gateways,
		Routes:     []install.OwnedObject{cart, {Obj: foreign}},
		Unattached: []install.UnattachedRoute{{Route: cart, Parent: testutil.GwRef("infra", "gone")}}}
	if got := in.withStatus(); got != 3 {
		t.Fatalf("objects with status %d, want 3: the class, the Gateway and cart", got)
	}
}
