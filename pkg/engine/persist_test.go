package engine

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Persistence of KGWUI-012 (computed-rules §9.3, §11.5 rows owned by 2.3).

const (
	image24 = "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7"
	image23 = "cr.kgateway.dev/kgateway-dev/kgateway:v2.3.11"
)

// pworld is a Gateway infra/edge whose listener http admits shop/a and
// shop/b, so the rules compute 2 routes against attached.
type pworld struct {
	image    string
	attached int32
	hostname string
	// selector makes http admit only namespaces labelled team=web.
	selector bool
	// nsDenied makes Namespaces unreadable.
	nsDenied bool
	// noGateway deletes the Gateway.
	noGateway bool
	// touch applies unrelated writes: annotations, a condition, a resourceVersion.
	touch bool
}

func defaultWorld() pworld { return pworld{image: image24, attached: 1} }

func (w pworld) objects() []*unstructured.Unstructured {
	ns := objects.RouteNamespaces{From: "All"}
	if w.selector {
		ns = objects.RouteNamespaces{From: "Selector", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "web"}}}
	}
	gw := testutil.GatewayWithListeners("infra", "edge", "kgateway",
		[]objects.Listener{{Name: "http", Port: 80, Protocol: "HTTP", Hostname: w.hostname, AllowedRoutes: objects.AllowedRoutes{Namespaces: ns}}},
		testutil.Listener{Name: "http", Attached: w.attached, Conds: []metav1.Condition{
			testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}})
	ref := testutil.GwRef("infra", "edge")
	a := testutil.HTTPRoute("shop", "a", []objects.ParentRef{ref}, testutil.AcceptedBy(ref, testutil.Controller))
	b := testutil.HTTPRoute("shop", "b", []objects.ParentRef{ref}, testutil.AcceptedBy(ref, testutil.Controller))
	if w.touch {
		gw.SetAnnotations(map[string]string{"note": "edited"})
		gw.SetResourceVersion("9")
		// A ResolvedRefs flip on a route is not evidence of any view A check.
		st := testutil.AcceptedBy(ref, testutil.Controller)
		st.Conds[1] = testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")
		b = testutil.HTTPRoute("shop", "b", []objects.ParentRef{ref}, st)
		a.SetResourceVersion("12")
	}
	out := []*unstructured.Unstructured{testutil.GatewayClass("kgateway", testutil.Controller, true), a, b}
	if !w.noGateway {
		out = append(out, gw)
	}
	return out
}

// snapshot is w's synced snapshot, with one controller running w.image.
func (w pworld) snapshot(t *testing.T) *cache.Snapshot {
	t.Helper()
	d := testutil.ControllerDeployment("kgw", "kgateway", w.image, 1, 1)
	delete(d.Labels, "app.kubernetes.io/version") // the version comes from the image tag
	access := map[schema.GroupVersionResource]cache.Access{cache.NamespacesGVR: {OK: !w.nsDenied}}
	for _, k := range testutil.ServedUpstream() {
		access[k.GVR()] = cache.Access{OK: true}
	}
	var nss []*corev1.Namespace
	if !w.nsDenied {
		nss = []*corev1.Namespace{testutil.Namespace("kgw", nil), testutil.Namespace("infra", nil), testutil.Namespace("shop", map[string]string{"team": "web"})}
	}
	return &cache.Snapshot{Objects: testutil.ObjectsByGVR(t, w.objects()...), Access: access, Namespaces: nss,
		Deployments: []*appsv1.Deployment{d, testutil.ProxyDeployment("infra", "edge", 1, 1)}, Pods: []*corev1.Pod{testutil.ControllerPod(d, "p", true)},
		Synced: true, TakenAt: testutil.Now}
}

// print is w's source fingerprint: it changes with Namespace access.
func (w pworld) print() cache.Fingerprint { return fprint(1, cache.Access{OK: !w.nsDenied}) }

// stepper computes directly at chosen instants after testutil.Now.
type stepper struct {
	t   *testing.T
	now time.Time
	src *fakeSource
	e   *Engine
	met ctrlmetrics.Result
}

func newStepper(t *testing.T, w pworld) *stepper {
	s := &stepper{t: t, now: testutil.Now}
	s.met = ctrlmetrics.Result{CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{}, ManagedPresent: true}}
	s.src = &fakeSource{base: w.snapshot(t), print: w.print(), now: func() time.Time { return s.now }}
	cfg := config(nil, metricsFunc(func(string) (ctrlmetrics.Result, bool) { return s.met, true }), func() time.Time { return s.now })
	cfg.Snapshots, cfg.MetricsMaxAge = s.src, 0
	s.e = New(cfg)
	return s
}

// at computes at testutil.Now + d over w.
func (s *stepper) at(d time.Duration, w pworld) *Model {
	s.t.Helper()
	s.now = testutil.Now.Add(d)
	s.src.setBase(w.snapshot(s.t), w.print())
	return s.e.Compute()
}

// disagreements returns m's KGWUI-012 findings.
func disagreements(m *Model) []health.Finding {
	var out []health.Finding
	for _, f := range m.Installs[0].Health.Findings {
		if f.Code == health.CodeComputedDisagrees {
			out = append(out, f)
		}
	}
	return out
}

// countCheck returns the listener-count check of infra/edge http.
func countCheck(t *testing.T, m *Model) computed.CheckResult {
	t.Helper()
	v := m.Installs[0].Computed
	if v == nil {
		t.Fatal("no computed views")
	}
	for _, c := range v.Checks {
		if c.ID.Kind == computed.KindListenerCount && c.Listener == "http" {
			return c
		}
	}
	t.Fatal("no listener-count check for http")
	return computed.CheckResult{}
}

// matured asserts exactly one KGWUI-012 on infra/edge with window start since.
func matured(t *testing.T, m *Model, since time.Time) health.Finding {
	t.Helper()
	fs := disagreements(m)
	if len(fs) != 1 || fs[0].Resource == nil || fs[0].Resource.String() != "infra/edge" || fs[0].Since == nil || !fs[0].Since.Equal(since) {
		t.Fatalf("want one KGWUI-012 on infra/edge since %v, got %+v", since, fs)
	}
	return fs[0]
}

func none(t *testing.T, m *Model, when string) {
	t.Helper()
	if fs := disagreements(m); len(fs) != 0 {
		t.Fatalf("%s: unexpected KGWUI-012 %+v", when, fs)
	}
}

// Row 1: a disagreement fixed within the window never raises KGWUI-012.
func TestDisagreementClearsBefore30s(t *testing.T) {
	w := defaultWorld()
	s := newStepper(t, w)
	m := s.at(0, w)
	if c := countCheck(t, m); c.State != computed.StatePending || c.Mature || !c.Since.Equal(testutil.Now) {
		t.Fatalf("off by one at T: %+v", c)
	}
	if m.Next != (Deadline{At: testutil.Now.Add(Window)}) {
		t.Fatalf("Next %+v, want the maturation at T+30s", m.Next)
	}
	none(t, s.at(10*time.Second, w), "T+10s")
	w.attached = 2
	m = s.at(29*time.Second, w)
	none(t, m, "T+29s")
	if c := countCheck(t, m); c.State != computed.StateChecked || !c.Since.IsZero() {
		t.Fatalf("agreeing at T+29s: %+v", c)
	}
	if len(s.e.persist.m) != 0 {
		t.Fatalf("entry kept after the disagreement cleared: %v", s.e.persist.m)
	}
	none(t, s.at(Window, w), "T+30s")
	none(t, s.at(2*Window, w), "T+60s")
}

// Row 2 (design §9.3): with no events after T, only the scheduled deadline
// can explain the run that raises KGWUI-012 at exactly T+30s.
func TestMaturesAtScheduledDeadline(t *testing.T) {
	w := defaultWorld()
	h := newHarness(t, nil)
	h.src.setBase(w.snapshot(t), w.print())
	h.start(10 * time.Minute)
	if p := h.e.Current(); len(disagreements(p.Model)) != 0 || p.Model.Next != (Deadline{At: testutil.Now.Add(Window)}) {
		t.Fatalf("at T: findings %v, Next %+v", disagreements(p.Model), p.Model.Next)
	}
	h.c.Step(Window - time.Nanosecond)
	h.await(EvConfirmed)
	h.await(EvArmed)
	if h.runs != 0 || len(disagreements(h.e.Current().Model)) != 0 {
		t.Fatalf("at T+30s-1ns: runs %d, findings %v", h.runs, disagreements(h.e.Current().Model))
	}
	h.c.Step(time.Nanosecond)
	e := h.await(EvPublished)
	if !e.At.Equal(testutil.Now.Add(Window)) {
		t.Fatalf("maturation run at %v, want T+30s", e.At)
	}
	f := matured(t, e.Published.Model, testutil.Now)
	if f.Severity != health.SeverityWarning || f.Message != "The UI's computed view disagrees with the controller" ||
		f.Detail != "Listener `http`: computed 2 routes, controller reports 1. Rule set kgateway-2.4." {
		t.Fatalf("finding %+v", f)
	}
	h.await(EvArmed)
	h.tickIdle()
	if h.runs != 1 {
		t.Fatalf("runs = %d, want exactly one publication", h.runs)
	}
}

// Row 3: an annotation edit, an unrelated condition write, a
// resourceVersion-only update and a metrics arrival never reset the window.
func TestUnrelatedWritesDoNotReset(t *testing.T) {
	w := defaultWorld()
	s := newStepper(t, w)
	s.at(0, w)
	w.touch = true
	s.met.CollectedAt = testutil.Now.Add(10 * time.Second)
	m := s.at(10*time.Second, w)
	if c := countCheck(t, m); c.State != computed.StatePending || !c.Since.Equal(testutil.Now) || m.Next != (Deadline{At: testutil.Now.Add(Window)}) {
		t.Fatalf("after unrelated writes: %+v, Next %+v", c, m.Next)
	}
	matured(t, s.at(Window, w), testutil.Now)
}

// §11.5: the finding's since is the window start, also after a reset.
func TestFindingSinceIsWindowStart(t *testing.T) {
	w := defaultWorld()
	s := newStepper(t, w)
	s.at(0, w)
	matured(t, s.at(Window+5*time.Second, w), testutil.Now)
	w.attached = 3 // still off, with a new observed value
	reset := 40 * time.Second
	m := s.at(reset, w)
	none(t, m, "after the reset")
	if c := countCheck(t, m); !c.Since.Equal(testutil.Now.Add(reset)) {
		t.Fatalf("since after reset %v", c.Since)
	}
	matured(t, s.at(reset+Window, w), testutil.Now.Add(reset))
}

// Row 7: a listed input (the listener's hostname) or the observed value resets the window.
func TestListedChangeResets(t *testing.T) {
	w := defaultWorld()
	s := newStepper(t, w)
	s.at(0, w)
	// The generation stays, so only the hostname changes the fingerprint.
	w.hostname = "*.example.com"
	s.at(20*time.Second, w)
	none(t, s.at(Window, w), "T+30s after the hostname edit")
	matured(t, s.at(50*time.Second, w), testutil.Now.Add(20*time.Second))
	w.attached = 0
	none(t, s.at(60*time.Second, w), "after the attachedRoutes change")
	none(t, s.at(89*time.Second, w), "T+89s")
	matured(t, s.at(90*time.Second, w), testutil.Now.Add(60*time.Second))
}

// Row 8: a rule set switch is a new identity, so the window restarts.
func TestRuleSetSwitchResets(t *testing.T) {
	w := defaultWorld()
	s := newStepper(t, w)
	s.at(0, w)
	w.image = image23
	m := s.at(20*time.Second, w)
	if c := countCheck(t, m); c.ID.RuleSet != "kgateway-2.3" || !c.Since.Equal(testutil.Now.Add(20*time.Second)) {
		t.Fatalf("after the switch: %+v", c)
	}
	none(t, s.at(Window, w), "T+30s")
	f := matured(t, s.at(50*time.Second, w), testutil.Now.Add(20*time.Second))
	if f.Detail != "Listener `http`: computed 2 routes, controller reports 1. Rule set kgateway-2.3." {
		t.Fatalf("detail %q", f.Detail)
	}
	if len(s.e.persist.m) != 1 {
		t.Fatalf("the 2.4 entry must be pruned: %v", s.e.persist.m)
	}
}

// Row 9: a deleted subject prunes its entry and drops its finding the same run.
func TestSubjectDeletedPrunes(t *testing.T) {
	w := defaultWorld()
	s := newStepper(t, w)
	s.at(0, w)
	matured(t, s.at(Window, w), testutil.Now)
	w.noGateway = true
	none(t, s.at(Window+time.Second, w), "after deletion")
	if len(s.e.persist.m) != 0 {
		t.Fatalf("entries left: %v", s.e.persist.m)
	}
}

// Row 10 (Task 39 review): losing Namespace access records no Namespace dep,
// yet the source fingerprint changes, so the confirm tick starts a run that
// turns the check unknown and clears KGWUI-012, with no event delivered.
func TestTurnsUnknownClearsSameRun(t *testing.T) {
	w := defaultWorld()
	w.selector = true
	h := newHarness(t, nil)
	h.src.setBase(w.snapshot(t), w.print())
	h.start(10 * time.Minute)
	h.c.Step(Window)
	matured(t, h.await(EvPublished).Published.Model, testutil.Now)
	h.await(EvArmed)
	w.nsDenied = true
	h.src.setBase(w.snapshot(t), w.print()) // no notify: only the fingerprint moved
	h.c.Step(ConfirmEvery)
	h.await(EvConfirmRefused)
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	m := h.await(EvPublished).Published.Model
	none(t, m, "after Namespace access was lost")
	if c := countCheck(t, m); c.State != computed.StateUnknown || c.Mature {
		t.Fatalf("check after access loss: %+v", c)
	}
	if len(h.e.persist.m) != 0 {
		t.Fatalf("entries left: %v", h.e.persist.m)
	}
}

// KGWUI-012 is a warning: the install and the Gateway row turn warning, never error.
func TestKGWUI012IsWarning(t *testing.T) {
	w := defaultWorld()
	s := newStepper(t, w)
	s.at(0, w)
	m := s.at(Window, w)
	matured(t, m, testutil.Now)
	rep := m.Installs[0].Health
	if rep.Counts[health.SeverityError] != 0 || rep.State == health.StateError {
		t.Fatalf("report %+v", rep)
	}
	if got := explore.RowState(objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}, rep); got != health.StateWarning {
		t.Fatalf("Gateway row state %s, want warning", got)
	}
}

// step: the window rules over hand-made checks, including a missing entry's grace.
func TestPersistenceStep(t *testing.T) {
	id := func(s string) computed.Identity {
		return computed.Identity{Install: "i", RuleSet: "r", Kind: computed.KindListenerCount, Subject: s}
	}
	t0 := testutil.Now
	p := newPersistence()
	cs := []computed.CheckResult{
		{ID: id("c"), Class: computed.ClassContradicting, Fingerprint: 1},
		{ID: id("m"), Class: computed.ClassMissing, Fingerprint: 2, ParentCurrent: true},
		{ID: id("p"), Class: computed.ClassMissing, Fingerprint: 3},
		{ID: id("u"), Class: computed.ClassUnexpected, Fingerprint: 4},
		{ID: id("s"), Class: computed.ClassStale},
		{ID: id("n"), Class: computed.ClassNotObservable},
		{ID: id("k"), Class: computed.ClassUnknown},
		{ID: id("x"), Class: computed.ClassInconclusive},
	}
	grace := func(c computed.CheckResult) time.Time { return t0.Add(45 * time.Second) }
	if next := p.step(t0, cs, grace); !next.Equal(t0.Add(Window)) {
		t.Fatalf("next %v", next)
	}
	want := []computed.State{computed.StatePending, computed.StatePending, computed.StatePending, computed.StateChecked,
		computed.StatePending, computed.StateChecked, computed.StateUnknown, computed.StateChecked}
	for i, c := range cs {
		if c.State != want[i] || c.Mature {
			t.Errorf("%s: state %s mature %v, want %s", c.ID.Subject, c.State, c.Mature, want[i])
		}
	}
	// The missing entry matures at its later grace end; P not current keeps it pending.
	if next := p.step(t0.Add(Window), cs, grace); !next.Equal(t0.Add(45*time.Second)) || cs[0].State != computed.StateDisagrees || cs[1].Mature {
		t.Fatalf("at T+30s: next %v, %+v %+v", next, cs[0], cs[1])
	}
	if next := p.step(t0.Add(45*time.Second), cs, grace); !next.IsZero() || !cs[1].Mature || cs[2].Mature || cs[2].State != computed.StatePending {
		t.Fatalf("at T+45s: next %v, %+v %+v", next, cs[1], cs[2])
	}
	if len(p.m) != 3 {
		t.Fatalf("entries %v, want the three candidates", p.m)
	}
}
