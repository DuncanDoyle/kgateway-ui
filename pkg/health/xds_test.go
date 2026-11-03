package health

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

const (
	clusterType = "envoy.config.cluster.v3.Cluster"
	routeType   = "envoy.config.route.v3.RouteConfiguration"
)

// capture parses a Task 20 controller capture as the Pod uid/name.
func capture(t *testing.T, file, uid, name string) ctrlmetrics.Metrics {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "ctrlmetrics", "testdata", file+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	m, err := ctrlmetrics.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	m.PerPod[0].UID, m.PerPod[0].Name = uid, name
	return m
}

// cleared is m with every active reject at zero.
func cleared(m ctrlmetrics.Metrics) ctrlmetrics.Metrics {
	m.Rejects = map[ctrlmetrics.GatewayKey]float64{}
	pods := make([]ctrlmetrics.PodMetrics, len(m.PerPod))
	for i, p := range m.PerPod {
		p.Active = map[ctrlmetrics.TypeKey]float64{}
		pods[i] = p
	}
	m.PerPod = pods
	return m
}

func resultAt(at time.Time, ms ...ctrlmetrics.Metrics) *ctrlmetrics.Result {
	return &ctrlmetrics.Result{Metrics: ctrlmetrics.Aggregate(ms), CollectedAt: at}
}

// xdsWorld is healthyWorld with controller Pods ctrl-a and ctrl-b, Gateway
// infra/nack with a 2/2 proxy, and the v2.4.7 two-replica reject captures.
func xdsWorld(t *testing.T) *world {
	t.Helper()
	w := healthyWorld()
	w.pods = []*corev1.Pod{testutil.ControllerPod(w.ctrl, "ctrl-a", true), testutil.ControllerPod(w.ctrl, "ctrl-b", true)}
	w.objs = append(w.objs, testutil.Gateway("infra", "nack", "kgateway", nil))
	w.deploys = append(w.deploys, testutil.ProxyDeployment("infra", "nack", 2, 2))
	w.metrics = resultAt(testutil.Now, capture(t, "v2.4.7-rejects-a", "pod/kgw/ctrl-a", "ctrl-a"), capture(t, "v2.4.7-rejects-b", "pod/kgw/ctrl-b", "ctrl-b"))
	return w
}

// xdsAt evaluates w at now with its metrics collected at now.
func xdsAt(t *testing.T, w *world, st *States, now time.Time) Report {
	t.Helper()
	in := processInput(t, w)
	in.Now = now
	if w.metrics != nil {
		in.Metrics.CollectedAt = now
	}
	return Evaluate(in, Upstream(), Runtime{States: st})
}

func xdsOn(r Report, gw string) *Finding {
	for i := range r.Findings {
		if f := &r.Findings[i]; f.Code == CodeXDSRejected && f.Resource != nil && f.Resource.Namespace+"/"+f.Resource.Name == gw {
			return f
		}
	}
	return nil
}

func xdsFacts(t *testing.T, f *Finding) *XDSRejectsEvidence {
	t.Helper()
	if f == nil {
		t.Fatal("no 001")
	}
	ev, ok := f.Facts.(*XDSRejectsEvidence)
	if !ok {
		t.Fatalf("evidence %T, want xds-rejects", f.Facts)
	}
	return ev
}

// Two controller replicas each serve one nack proxy: one 001 per Gateway,
// streams summed per type, the reporting replicas named and as evidence.
func TestXDSRejectsPerTypeOverTwoReplicas(t *testing.T) {
	w := xdsWorld(t)
	r := xdsAt(t, w, NewStates(), testutil.Now)
	n := 0
	for _, f := range r.Findings {
		if f.Code == CodeXDSRejected {
			n++
		}
	}
	if n != 2 || r.State != StateError {
		t.Fatalf("want one 001 per Gateway, got %d (%s): %+v", n, r.State, r.Findings)
	}
	nack := xdsOn(r, "infra/nack")
	ev := xdsFacts(t, nack)
	want := []XDSTypeRejects{
		{TypeURL: clusterType, Streams: 2, ReportedBy: []string{"ctrl-a", "ctrl-b"}, FirstSeen: testutil.Now},
		{TypeURL: routeType, Streams: 2, ReportedBy: []string{"ctrl-a", "ctrl-b"}, FirstSeen: testutil.Now},
	}
	if !reflect.DeepEqual(ev.Types, want) || ev.Proxies == nil || *ev.Proxies != (ProxyCount{Ready: 2, Desired: 2}) {
		t.Fatalf("nack evidence %+v proxies %+v", ev.Types, ev.Proxies)
	}
	pods := []objects.Ref{{Kind: "Pod", Namespace: "kgw", Name: "ctrl-a"}, {Kind: "Pod", Namespace: "kgw", Name: "ctrl-b"}}
	if !reflect.DeepEqual(nack.Evidence, pods) || nack.Since == nil || !nack.Since.Equal(testutil.Now) {
		t.Fatalf("nack evidence refs %+v since %v", nack.Evidence, nack.Since)
	}
	edge := xdsFacts(t, xdsOn(r, "infra/edge"))
	if len(edge.Types) != 1 || edge.Types[0].TypeURL != clusterType || edge.Types[0].Streams != 1 || !reflect.DeepEqual(edge.Types[0].ReportedBy, []string{"ctrl-a"}) {
		t.Fatalf("edge evidence %+v", edge.Types)
	}
	if f := xdsOn(r, "infra/edge"); !reflect.DeepEqual(f.Evidence, pods[:1]) || !strings.Contains(f.Detail, "Cluster") {
		t.Fatalf("edge refs %+v detail %q", f.Evidence, f.Detail)
	}
}

// A current Programmed=True status that contradicts the rejects gets the
// status-says-healthy message; a stale status does not.
func TestXDSStatusSaysHealthyMessage(t *testing.T) {
	w := xdsWorld(t)
	w.objs[3].SetGeneration(2) // nack's status describes generation 1
	r := xdsAt(t, w, NewStates(), testutil.Now)
	edge, nack := xdsOn(r, "infra/edge"), xdsOn(r, "infra/nack")
	if edge == nil || edge.Message != xdsStatusHealthyMessage || !xdsFacts(t, edge).StatusSaysHealthy {
		t.Fatalf("edge %+v", edge)
	}
	if nack == nil || nack.Message != xdsRejectMessage || xdsFacts(t, nack).StatusSaysHealthy {
		t.Fatalf("nack %+v", nack)
	}
}

// rejects_total growth since first seen is taken per Pod UID: a counter that
// went down and a new UID add nothing, and a scrape with Err adds nothing and
// keeps the stored totals.
func TestXDSTotalGrowthSinceFirstSeen(t *testing.T) {
	w := xdsWorld(t)
	st := NewStates()
	xdsAt(t, w, st, testutil.Now)
	a, b := capture(t, "v2.4.7-rejects-a", "pod/kgw/ctrl-a", "ctrl-a"), capture(t, "v2.4.7-rejects-b", "pod/kgw/ctrl-b", "ctrl-b")
	k := ctrlmetrics.TypeKey{Gateway: ctrlmetrics.GatewayKey{Namespace: "infra", Name: "nack"}, TypeURL: clusterType}
	a.PerPod[0].Total[k] += 5 // grows by 5
	b.PerPod[0].Total[k] = 1  // reset
	w.metrics = resultAt(testutil.Now, a, b)
	growth := func(r Report) int {
		for _, ty := range xdsFacts(t, xdsOn(r, "infra/nack")).Types {
			if ty.TypeURL == clusterType {
				return ty.TotalSinceFirstSeen
			}
		}
		t.Fatal("no Cluster type")
		return 0
	}
	if g := growth(xdsAt(t, w, st, testutil.Now.Add(10*time.Second))); g != 5 {
		t.Fatalf("growth %d, want 5", g)
	}
	w.metrics = &ctrlmetrics.Result{Err: "scrape failed"}
	if r := xdsAt(t, w, st, testutil.Now.Add(20*time.Second)); xdsOn(r, "infra/nack") != nil || r.State != StateUnknown {
		t.Fatalf("an Err result must be unavailable: %s %+v", r.State, r.Findings)
	}
	b.PerPod[0].Total[k] = 2 // grows by 1 from the reset
	w.metrics = resultAt(testutil.Now, a, b)
	r := xdsAt(t, w, st, testutil.Now.Add(30*time.Second))
	if g := growth(r); g != 6 {
		t.Fatalf("growth after an Err result %d, want 6", g)
	}
	// The failed scrape does not restart the reject's age.
	if f := xdsOn(r, "infra/nack"); f.Since == nil || !f.Since.Equal(testutil.Now) || !xdsFacts(t, f).Types[0].FirstSeen.Equal(*f.Since) {
		t.Fatalf("since %v, types %+v; want since %v", f.Since, xdsFacts(t, f).Types, testutil.Now)
	}
	c := capture(t, "v2.4.7-restart", "pod/kgw/ctrl-c", "ctrl-c") // a new UID sets a baseline
	w.metrics = resultAt(testutil.Now, a, b, c)
	if g := growth(xdsAt(t, w, st, testutil.Now.Add(40*time.Second))); g != 6 {
		t.Fatalf("growth with a new UID %d, want 6", g)
	}
}

// A reject that disappears while a controller Pod has a new UID or a higher
// restart count is held for 60 s: no 001, xDS rejects pending, the install
// unknown at +59 s and healthy at +61 s. A reject back at +30 s keeps since.
func TestRestartHoldKeepsInstallUnknown(t *testing.T) {
	t0, t1 := testutil.Now, testutil.Now.Add(10*time.Second)
	replace := func(w *world) {
		w.pods[1] = testutil.ControllerPod(w.ctrl, "ctrl-c", true)
	}
	restart := func(w *world) {
		w.pods[1] = ctrlStatus(w.pods[1], corev1.ContainerStatus{RestartCount: 1, State: running(t1)})
	}
	for _, tc := range []struct {
		name   string
		change func(*world)
	}{{"new UID", replace}, {"higher restart count", restart}} {
		t.Run(tc.name, func(t *testing.T) {
			w := xdsWorld(t)
			w.pods[1] = ctrlStatus(w.pods[1], corev1.ContainerStatus{State: running(testutil.Old.Time)})
			rejecting := w.metrics
			st := NewStates()
			first := xdsOn(xdsAt(t, w, st, t0), "infra/nack")
			tc.change(w)
			w.metrics = resultAt(t1, cleared(rejecting.Metrics))
			for _, at := range []time.Duration{0, 59 * time.Second} {
				r := xdsAt(t, w, st, t1.Add(at))
				c := resultOf(r, "xDS rejects")
				if r.State != StateUnknown || !c.Pending || c.OK || !c.Required || c.Error != xdsHoldError || xdsOn(r, "infra/nack") != nil {
					t.Fatalf("at +%s: %s %+v %+v", at, r.State, c, r.Findings)
				}
				if r.Next.IsZero() || r.Next.After(t1.Add(60*time.Second)) {
					t.Fatalf("at +%s: next %v, want the hold end", at, r.Next)
				}
			}
			// A restart also raises 019 (warning), so +61 s reads warning there.
			if r := xdsAt(t, w, st, t1.Add(61*time.Second)); !resultOf(r, "xDS rejects").OK || r.State == StateUnknown || r.State == StateError {
				t.Fatalf("at +61 s: %s %+v %+v", r.State, resultOf(r, "xDS rejects"), r.Findings)
			}

			// The reject comes back inside a second hold: since and first seen continue.
			w2, st2 := xdsWorld(t), NewStates()
			w2.pods[1] = ctrlStatus(w2.pods[1], corev1.ContainerStatus{State: running(testutil.Old.Time)})
			xdsAt(t, w2, st2, t0)
			tc.change(w2)
			w2.metrics = resultAt(t1, cleared(rejecting.Metrics))
			xdsAt(t, w2, st2, t1)
			w2.metrics = rejecting
			back := xdsOn(xdsAt(t, w2, st2, t1.Add(30*time.Second)), "infra/nack")
			if back == nil || back.ID != first.ID || !back.Since.Equal(t0) || !xdsFacts(t, back).Types[0].FirstSeen.Equal(t0) {
				t.Fatalf("returned 001 %+v, want since %v", back, t0)
			}
		})
	}
}

// Without a controller Pod change a reject that disappears clears at once; a
// Pod change seen while the reject was still reported holds it on the next scrape.
func TestRestartHoldNeedsAPodChange(t *testing.T) {
	w := xdsWorld(t)
	rejecting := w.metrics
	st := NewStates()
	xdsAt(t, w, st, testutil.Now)
	w.metrics = resultAt(testutil.Now, cleared(rejecting.Metrics))
	if r := xdsAt(t, w, st, testutil.Now.Add(10*time.Second)); r.State != StateHealthy {
		t.Fatalf("no Pod change: %s %+v", r.State, resultOf(r, "xDS rejects"))
	}

	w, st = xdsWorld(t), NewStates()
	xdsAt(t, w, st, testutil.Now)
	w.pods[1] = testutil.ControllerPod(w.ctrl, "ctrl-c", true)
	if r := xdsAt(t, w, st, testutil.Now.Add(5*time.Second)); xdsOn(r, "infra/nack") == nil {
		t.Fatal("the reject is still reported")
	}
	w.metrics = resultAt(testutil.Now, cleared(rejecting.Metrics))
	if r := xdsAt(t, w, st, testutil.Now.Add(10*time.Second)); r.State != StateUnknown || !resultOf(r, "xDS rejects").Pending {
		t.Fatalf("a Pod change before the scrape must hold: %s %+v", r.State, resultOf(r, "xDS rejects"))
	}
}

// changedWorld is xdsWorld whose objects' current generations were first seen
// at seen[name]; any other object counts as seen long before.
func changedWorld(t *testing.T, seen map[string]time.Duration) *world {
	t.Helper()
	w := xdsWorld(t)
	w.firstSeen = func(u *unstructured.Unstructured) time.Time {
		if d, ok := seen[u.GetName()]; ok {
			return testutil.Now.Add(-d)
		}
		return testutil.Old.Time
	}
	return w
}

// recentAt runs w without rejects at Now − 30 min, then with them at Now, and returns nack's recently changed.
func recentAt(t *testing.T, w *world) []string {
	t.Helper()
	st := NewStates()
	rejects := w.metrics
	w.metrics = resultAt(testutil.Now, cleared(rejects.Metrics))
	xdsAt(t, w, st, testutil.Now.Add(-30*time.Minute))
	w.metrics = rejects
	var names []string
	for _, r := range xdsFacts(t, xdsOn(xdsAt(t, w, st, testutil.Now), "infra/nack")).RecentlyChanged {
		names = append(names, r.Kind+"/"+r.Name)
	}
	return names
}

// Objects on the Gateway changed in the 10 min before the first reject, newest
// first: 9:59 before is in, 10:01 out, and 12 changes keep the 10 newest.
func TestRecentlyChangedWindowAndCap(t *testing.T) {
	nack := testutil.GwRef("infra", "nack")
	w := changedWorld(t, map[string]time.Duration{"in": 9*time.Minute + 59*time.Second, "out": 10*time.Minute + time.Second,
		"ls": 2 * time.Minute, "tp": time.Minute, "elsewhere": time.Minute, "via-ls": 3 * time.Minute})
	lsRef := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	w.objs = append(w.objs,
		testutil.HTTPRoute("shop", "in", []objects.ParentRef{nack}), testutil.HTTPRoute("shop", "out", []objects.ParentRef{nack}),
		testutil.ListenerSet("infra", "ls", nack, nil), testutil.HTTPRoute("shop", "via-ls", []objects.ParentRef{lsRef}),
		testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: nack, Controller: testutil.Controller}),
		testutil.HTTPRoute("shop", "elsewhere", []objects.ParentRef{testutil.GwRef("infra", "edge")}))
	if got, want := recentAt(t, w), []string{"TrafficPolicy/tp", "ListenerSet/ls", "HTTPRoute/via-ls", "HTTPRoute/in"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recently changed %v, want %v", got, want)
	}

	seen := map[string]time.Duration{}
	w = changedWorld(t, seen)
	for i := range 12 {
		name := fmt.Sprintf("r%02d", i)
		seen[name] = time.Duration(i+1) * 30 * time.Second
		w.objs = append(w.objs, testutil.HTTPRoute("shop", name, []objects.ParentRef{nack}))
	}
	got := recentAt(t, w)
	if len(got) != 10 || got[0] != "HTTPRoute/r00" || got[9] != "HTTPRoute/r09" {
		t.Fatalf("want the 10 newest, got %v", got)
	}
	// The list is evidence only: the finding id and its Pod dependencies stay as they are.
	r := xdsAt(t, changedWorld(t, nil), NewStates(), testutil.Now)
	if f := xdsOn(r, "infra/nack"); f.ID != FindingID(r.Install, CodeXDSRejected, f.Resource, nil) || len(f.Evidence) != 2 {
		t.Fatalf("id %s evidence %+v", f.ID, f.Evidence)
	}
}

// Objects the check first saw at or before its first run have no known change
// time, so a UI restart lists nothing as recently changed.
func TestRecentlyChangedSkipsObjectsSeenAtStart(t *testing.T) {
	w := changedWorld(t, map[string]time.Duration{"at-start": 5 * time.Minute, "after": 4 * time.Minute})
	nack := testutil.GwRef("infra", "nack")
	w.objs = append(w.objs, testutil.HTTPRoute("shop", "at-start", []objects.ParentRef{nack}), testutil.HTTPRoute("shop", "after", []objects.ParentRef{nack}))
	st := NewStates()
	rejects := w.metrics
	w.metrics = resultAt(testutil.Now, cleared(rejects.Metrics))
	xdsAt(t, w, st, testutil.Now.Add(-5*time.Minute))
	w.metrics = rejects
	ev := xdsFacts(t, xdsOn(xdsAt(t, w, st, testutil.Now), "infra/nack"))
	if len(ev.RecentlyChanged) != 1 || ev.RecentlyChanged[0].Name != "after" {
		t.Fatalf("recently changed %+v, want only after", ev.RecentlyChanged)
	}
}
