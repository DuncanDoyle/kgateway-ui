package engine

import (
	"bytes"
	"cmp"
	"log/slog"
	"reflect"
	"strings"
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
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
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
	return imageSnapshot(t, w.image, w.nsDenied, w.objects()...)
}

// imageSnapshot is a synced snapshot of objs, with one controller running
// image and Namespaces kgw, infra, shop (team=web) and apps, unless denied.
func imageSnapshot(t *testing.T, image string, nsDenied bool, objs ...*unstructured.Unstructured) *cache.Snapshot {
	t.Helper()
	d := testutil.ControllerDeployment("kgw", "kgateway", image, 1, 1)
	delete(d.Labels, "app.kubernetes.io/version") // the version comes from the image tag
	access := map[schema.GroupVersionResource]cache.Access{cache.NamespacesGVR: {OK: !nsDenied}}
	for _, k := range testutil.ServedUpstream() {
		access[k.GVR()] = cache.Access{OK: true}
	}
	var nss []*corev1.Namespace
	if !nsDenied {
		nss = []*corev1.Namespace{testutil.Namespace("kgw", nil), testutil.Namespace("infra", nil), testutil.Namespace("shop", map[string]string{"team": "web"}),
			testutil.Namespace("apps", nil)}
	}
	return &cache.Snapshot{Objects: testutil.ObjectsByGVR(t, objs...), Access: access, Namespaces: nss,
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

// Row 10 (Task 39 review): losing Namespace access records no Namespace dep.
// In production the cache notifies on the access change (pkg/cache onChange);
// the confirm tick is the fallback when that notification lags. Either way the
// run turns the check unknown and clears KGWUI-012 in the same recompute.
func TestTurnsUnknownClearsSameRun(t *testing.T) {
	for _, notify := range []bool{true, false} {
		name := map[bool]string{true: "notified", false: "fingerprint only"}[notify]
		t.Run(name, func(t *testing.T) {
			w := defaultWorld()
			w.selector = true
			h := newHarness(t, nil)
			h.src.setBase(w.snapshot(t), w.print())
			h.start(10 * time.Minute)
			h.c.Step(Window)
			matured(t, h.await(EvPublished).Published.Model, testutil.Now)
			h.await(EvArmed)
			w.nsDenied = true
			h.src.setBase(w.snapshot(t), w.print())
			if notify {
				h.notify()
			} else {
				h.c.Step(ConfirmEvery)
				h.await(EvConfirmRefused)
			}
			awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
			m := h.await(EvPublished).Published.Model
			none(t, m, "after Namespace access was lost")
			if c := countCheck(t, m); c.State != computed.StateUnknown || c.Mature {
				t.Fatalf("check after access loss: %+v", c)
			}
			if len(h.e.persist.m) != 0 {
				t.Fatalf("entries left: %v", h.e.persist.m)
			}
		})
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
		{ID: id("t"), Class: computed.ClassTrigger, Fingerprint: 5},
	}
	grace := func(c computed.CheckResult) time.Time { return t0.Add(45 * time.Second) }
	if next := p.step(t0, cs, grace); !next.Equal(t0.Add(Window)) {
		t.Fatalf("next %v", next)
	}
	want := []computed.State{computed.StatePending, computed.StatePending, computed.StatePending, computed.StateChecked,
		computed.StatePending, computed.StateChecked, computed.StateUnknown, computed.StateChecked, computed.StateChecked}
	for i, c := range cs {
		if c.State != want[i] || c.Mature {
			t.Errorf("%s: state %s mature %v, want %s", c.ID.Subject, c.State, c.Mature, want[i])
		}
	}
	if !cs[8].Since.Equal(t0) {
		t.Fatalf("trigger since %v", cs[8].Since)
	}
	// The missing entry matures at its later grace end; P not current keeps it
	// pending. A trigger matures at 30 s and its state stays checked (PF12).
	if next := p.step(t0.Add(Window), cs, grace); !next.Equal(t0.Add(45*time.Second)) || cs[0].State != computed.StateDisagrees || cs[1].Mature {
		t.Fatalf("at T+30s: next %v, %+v %+v", next, cs[0], cs[1])
	}
	if t8 := cs[8]; !t8.Mature || t8.State != computed.StateChecked || !t8.Since.Equal(t0) {
		t.Fatalf("trigger at T+30s: %+v", t8)
	}
	if next := p.step(t0.Add(45*time.Second), cs, grace); !next.IsZero() || !cs[1].Mature || cs[2].Mature || cs[2].State != computed.StatePending {
		t.Fatalf("at T+45s: next %v, %+v %+v", next, cs[1], cs[2])
	}
	if len(p.m) != 4 {
		t.Fatalf("entries %v, want the four candidates", p.m)
	}
}

// persist over several installs equals step over their concatenation: states,
// window starts, the map and the next maturation.
func TestPersistEqualsStep(t *testing.T) {
	id := func(inst, s string) computed.Identity {
		return computed.Identity{Install: inst, RuleSet: "r", Kind: computed.KindListenerCount, Subject: s}
	}
	a := []computed.CheckResult{{ID: id("a", "c"), Class: computed.ClassContradicting, Fingerprint: 1}, {ID: id("a", "k"), Class: computed.ClassCompatible}}
	b := []computed.CheckResult{{ID: id("b", "c"), Class: computed.ClassContradicting, Fingerprint: 2}, {ID: id("b", "s"), Class: computed.ClassStale},
		{ID: id("b", "u"), Class: computed.ClassUnknown}}
	t0 := testutil.Now
	ps, pp := newPersistence(), newPersistence()
	for _, d := range []time.Duration{0, 10 * time.Second, Window} {
		now := t0.Add(d)
		if d == 10*time.Second {
			b[0].Fingerprint = 3 // b's window restarts
		}
		all := append(append([]computed.CheckResult{}, a...), b...)
		views := []*computed.Views{{Checks: append([]computed.CheckResult{}, a...)}, {Checks: append([]computed.CheckResult{}, b...)}}
		ns, np := ps.step(now, all, nil), pp.persist(now, views, nil)
		got := append(append([]computed.CheckResult{}, views[0].Checks...), views[1].Checks...)
		if !ns.Equal(np) || !reflect.DeepEqual(all, got) || !reflect.DeepEqual(ps.m, pp.m) {
			t.Fatalf("at +%v: next %v vs %v\nstep    %+v\npersist %+v", d, ns, np, all, got)
		}
	}
}

// Persistence of views B and C and KGWUI-011's computed and orphan triggers
// (computed-rules §9.3, §11.4 and §11.5 rows owned by 2.4).

var edgeRef = testutil.GwRef("infra", "edge")

// dworld is a Gateway infra/edge whose listener http admits every
// namespace, and routes; the listener counts the routes with a Gateway parentRef.
type dworld struct {
	image  string
	routes []*unstructured.Unstructured
}

func (w dworld) snapshot(t *testing.T) *cache.Snapshot {
	t.Helper()
	var roots int32
	for _, u := range w.routes {
		if ps := objects.DecodeHTTPRoute(u).ParentRefs; len(ps) > 0 && ps[0].Kind == "Gateway" {
			roots++
		}
	}
	gw := testutil.GatewayWithListeners("infra", "edge", "kgateway",
		[]objects.Listener{{Name: "http", Port: 80, Protocol: "HTTP", AllowedRoutes: objects.AllowedRoutes{Namespaces: objects.RouteNamespaces{From: "All"}}}},
		testutil.Listener{Name: "http", Attached: roots, Conds: []metav1.Condition{
			testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}})
	objs := append([]*unstructured.Unstructured{testutil.GatewayClass("kgateway", testutil.Controller, true), gw}, w.routes...)
	return imageSnapshot(t, cmp.Or(w.image, image24), false, objs...)
}

// step computes w at testutil.Now + d.
func (w dworld) step(s *stepper, d time.Duration) *Model {
	s.t.Helper()
	s.now = testutil.Now.Add(d)
	s.src.setBase(w.snapshot(s.t), fprint(1, accessOK))
	return s.e.Compute()
}

// newDStepper is a stepper for dworlds; mut, if set, adjusts the config.
func newDStepper(t *testing.T, mut func(*Config)) *stepper {
	s := &stepper{t: t, now: testutil.Now}
	s.met = ctrlmetrics.Result{CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{}, ManagedPresent: true}}
	s.src = &fakeSource{base: dworld{}.snapshot(t), print: fprint(1, accessOK), now: func() time.Time { return s.now }}
	cfg := config(nil, metricsFunc(func(string) (ctrlmetrics.Result, bool) { return s.met, true }), func() time.Time { return s.now })
	cfg.Snapshots, cfg.MetricsMaxAge = s.src, 0
	if mut != nil {
		mut(&cfg)
	}
	s.e = New(cfg)
	return s
}

// drule is a rule matching the path prefixes, with backendRefs refs.
func drule(paths []string, refs ...map[string]any) map[string]any {
	ms := make([]map[string]any, len(paths))
	for i, p := range paths {
		ms[i] = map[string]any{"path": map[string]any{"type": "PathPrefix", "value": p}}
	}
	r := testutil.Rule(ms...)
	bs := make([]any, len(refs))
	for i, b := range refs {
		bs[i] = b
	}
	r["backendRefs"] = bs
	return r
}

// droot is root shop/name on edge, delegating under path to refs, with a current entry.
func droot(name, path string, refs ...map[string]any) *unstructured.Unstructured {
	u := testutil.HTTPRoute("shop", name, []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller))
	return testutil.WithRules(u, drule([]string{path}, refs...))
}

// dleaf is route ns/name matching path, to a Service, with parentRefs parents and status entries.
func dleaf(ns, name, path string, parents []objects.ParentRef, status ...testutil.RouteParent) *unstructured.Unstructured {
	return testutil.WithRules(testutil.HTTPRoute(ns, name, parents, status...), drule([]string{path}, map[string]any{"name": "svc", "port": int64(80)}))
}

// under is a current Accepted entry under parent HTTPRoute shop/p.
func under(p string) testutil.RouteParent {
	return testutil.AcceptedBy(testutil.HTTPRouteRef("shop", p), testutil.Controller)
}

// dbase is root shop/api under /api delegating to the attached child shop/c2
// and to refs, plus c2, so api always keeps a child and reports no failure.
func dbase(refs ...map[string]any) []*unstructured.Unstructured {
	return []*unstructured.Unstructured{droot("api", "/api", append([]map[string]any{testutil.DelegateTo("shop", "c2")}, refs...)...),
		dleaf("shop", "c2", "/api/c2", nil, under("api"))}
}

// findingsOn returns m's findings with code on res.
func findingsOn(m *Model, code, res string) []health.Finding {
	var out []health.Finding
	for _, f := range m.Installs[0].Health.Findings {
		if f.Code == code && f.Resource != nil && f.Resource.String() == res {
			out = append(out, f)
		}
	}
	return out
}

// oneFinding fails unless m has exactly one finding with code on res, since since.
func oneFinding(t *testing.T, m *Model, code, res string, since time.Time) health.Finding {
	t.Helper()
	fs := findingsOn(m, code, res)
	if len(fs) != 1 || fs[0].Since == nil || !fs[0].Since.Equal(since) {
		t.Fatalf("want one %s on %s since %v, got %+v", code, res, since, fs)
	}
	return fs[0]
}

func noFinding(t *testing.T, m *Model, code, res, when string) {
	t.Helper()
	if fs := findingsOn(m, code, res); len(fs) != 0 {
		t.Fatalf("%s: unexpected %s on %s: %+v", when, code, res, fs)
	}
}

// delegCheck returns the one check of kind on route res.
func delegCheck(t *testing.T, m *Model, kind computed.CheckKind, res string) computed.CheckResult {
	t.Helper()
	var out []computed.CheckResult
	for _, c := range m.Installs[0].Computed.Checks {
		if c.ID.Kind == kind && c.On.String() == res {
			out = append(out, c)
		}
	}
	if len(out) != 1 {
		t.Fatalf("want one %s check on %s, got %+v", kind, res, out)
	}
	return out[0]
}

// §11.4 "computed-attached child, no entry, P current" and §11.5 row 11 in
// both orders: KGWUI-012 on the child at max(since + 30 s, graceEnd).
func TestChildEntryMaturesAtLaterOfGraceAndWindow(t *testing.T) {
	// The grace end is later: everything first seen at T, no entry ever.
	w := dworld{routes: append(dbase(testutil.DelegateTo("shop", "c")), dleaf("shop", "c", "/api/c", nil))}
	s := newDStepper(t, nil)
	m := w.step(s, 0)
	if ce := delegCheck(t, m, computed.KindChildEntry, "shop/c"); ce.Class != computed.ClassMissing || ce.State != computed.StatePending || !ce.ParentCurrent {
		t.Fatalf("at T: %+v", ce)
	}
	if m.Next != (Deadline{At: testutil.Now.Add(health.Grace)}) {
		t.Fatalf("Next %+v, want the grace end T+60s", m.Next)
	}
	noFinding(t, w.step(s, Window), health.CodeComputedDisagrees, "shop/c", "T+30s")
	noFinding(t, w.step(s, health.Grace-time.Nanosecond), health.CodeComputedDisagrees, "shop/c", "T+60s-1ns")
	oneFinding(t, w.step(s, health.Grace), health.CodeComputedDisagrees, "shop/c", testutil.Now)

	// The window ends later: the entry goes at T+45s, no generation changes.
	with := dworld{routes: append(dbase(testutil.DelegateTo("shop", "c")), dleaf("shop", "c", "/api/c", nil, under("api")))}
	s = newDStepper(t, nil)
	with.step(s, 0)
	gone := 45 * time.Second
	if m := w.step(s, gone); m.Next != (Deadline{At: testutil.Now.Add(gone + Window)}) {
		t.Fatalf("Next %+v, want T+75s", m.Next)
	}
	noFinding(t, w.step(s, health.Grace), health.CodeComputedDisagrees, "shop/c", "T+60s")
	noFinding(t, w.step(s, gone+Window-time.Nanosecond), health.CodeComputedDisagrees, "shop/c", "T+75s-1ns")
	oneFinding(t, w.step(s, gone+Window), health.CodeComputedDisagrees, "shop/c", testutil.Now.Add(gone))
}

// §11.4 "the same with P stale": the check waits on P's status, not on time.
func TestChildEntryWithStaleParentStaysPending(t *testing.T) {
	routes := append(dbase(testutil.DelegateTo("shop", "c")), dleaf("shop", "c", "/api/c", nil))
	routes[0].SetGeneration(2) // api's entry observed generation 1
	w := dworld{routes: routes}
	s := newDStepper(t, nil)
	w.step(s, 0)
	m := w.step(s, 5*time.Minute)
	if ce := delegCheck(t, m, computed.KindChildEntry, "shop/c"); ce.Class != computed.ClassMissing || ce.ParentCurrent || ce.State != computed.StatePending || ce.Mature {
		t.Fatalf("at T+5m: %+v", ce)
	}
	noFinding(t, m, health.CodeComputedDisagrees, "shop/c", "T+5m")
}

// §11.4 "matcher-dropped child (parent edit or child edit)", both sets: a
// current entry left is KGWUI-012 on the child after 30 s.
func TestMatcherDroppedEntryLeftDisagrees(t *testing.T) {
	for _, image := range []string{image24, image23} {
		t.Run(image, func(t *testing.T) {
			w := dworld{image: image, routes: append(dbase(testutil.DelegateTo("shop", "c")), dleaf("shop", "c", "/other", nil, under("api")))}
			s := newDStepper(t, nil)
			if ce := delegCheck(t, w.step(s, 0), computed.KindChildEntry, "shop/c"); ce.Class != computed.ClassUnexpected || !ce.Verified {
				t.Fatalf("at T: %+v", ce)
			}
			noFinding(t, w.step(s, Window-time.Nanosecond), health.CodeComputedDisagrees, "shop/c", "T+30s-1ns")
			oneFinding(t, w.step(s, Window), health.CodeComputedDisagrees, "shop/c", testutil.Now)
		})
	}
}

// §11.5 row 4: the child's ResolvedRefs toggling never restarts its child-entry window.
func TestChildResolvedRefsToggleNoReset(t *testing.T) {
	w := dworld{routes: append(dbase(testutil.DelegateTo("shop", "c")), dleaf("shop", "c", "/other", nil, under("api")))}
	s := newDStepper(t, nil)
	w.step(s, 0)
	toggled := under("api")
	toggled.Conds[1] = testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")
	w.routes[len(w.routes)-1] = dleaf("shop", "c", "/other", nil, toggled)
	if ce := delegCheck(t, w.step(s, 10*time.Second), computed.KindChildEntry, "shop/c"); !ce.Since.Equal(testutil.Now) {
		t.Fatalf("since %v after the toggle", ce.Since)
	}
	oneFinding(t, w.step(s, Window), health.CodeComputedDisagrees, "shop/c", testutil.Now)
}

// failing is root shop/p under /api delegating only to shop/c, reporting
// ResolvedRefs as rr and Accepted as acc.
func failing(acc, rr metav1.Condition) *unstructured.Unstructured {
	st := testutil.RouteParent{Ref: edgeRef, Controller: testutil.Controller, Conds: []metav1.Condition{acc, rr}}
	return testutil.WithRules(testutil.HTTPRoute("shop", "p", []objects.ParentRef{edgeRef}, st), drule([]string{"/api"}, testutil.DelegateTo("shop", "c")))
}

var accepted = testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")

// §11.5 row 5: a parent's Accepted changing never restarts its parent-resolvedrefs window.
func TestParentAcceptedChangeNoReset(t *testing.T) {
	// c is dropped, so p expects False/BackendNotFound; it reports True.
	resolved := testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")
	w := dworld{routes: []*unstructured.Unstructured{failing(accepted, resolved), dleaf("shop", "c", "/other", nil)}}
	s := newDStepper(t, nil)
	if pc := delegCheck(t, w.step(s, 0), computed.KindParentResolvedRefs, "shop/p"); pc.Class != computed.ClassContradicting {
		t.Fatalf("at T: %+v", pc)
	}
	w.routes[0] = failing(testutil.Cond("Accepted", metav1.ConditionFalse, "UnsupportedValue"), resolved)
	if pc := delegCheck(t, w.step(s, 10*time.Second), computed.KindParentResolvedRefs, "shop/p"); !pc.Since.Equal(testutil.Now) {
		t.Fatalf("since %v after the Accepted change", pc.Since)
	}
	oneFinding(t, w.step(s, Window), health.CodeComputedDisagrees, "shop/p", testutil.Now)
}

// §11.5 row 6: a selected child's inherit annotation changing while the
// parent still contradicts resets the window: False/BackendNotFound is
// expected, then no failure, while False/RefNotPermitted is observed.
func TestInheritAnnotationResets(t *testing.T) {
	notPermitted := testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "RefNotPermitted")
	w := dworld{routes: []*unstructured.Unstructured{failing(accepted, notPermitted), dleaf("shop", "c", "/other", nil)}}
	s := newDStepper(t, nil)
	if pc := delegCheck(t, w.step(s, 0), computed.KindParentResolvedRefs, "shop/p"); pc.Class != computed.ClassContradicting || pc.Expected != "False/BackendNotFound" {
		t.Fatalf("at T: %+v", pc)
	}
	w.routes[1].SetAnnotations(map[string]string{objects.InheritParentMatcherAnnotation: "true"})
	reset := 10 * time.Second
	pc := delegCheck(t, w.step(s, reset), computed.KindParentResolvedRefs, "shop/p")
	if pc.Class != computed.ClassContradicting || pc.Expected != "True" || !pc.Since.Equal(testutil.Now.Add(reset)) {
		t.Fatalf("after the annotation: %+v", pc)
	}
	noFinding(t, w.step(s, Window), health.CodeComputedDisagrees, "shop/p", "T+30s")
	oneFinding(t, w.step(s, reset+Window), health.CodeComputedDisagrees, "shop/p", testutil.Now.Add(reset))
}

// requiredChecks are m's required health checks, by name.
func requiredChecks(m *Model) map[string]health.CheckResult {
	out := map[string]health.CheckResult{}
	for _, c := range m.Installs[0].Health.Checks {
		if c.Required {
			c.CollectedAt = time.Time{}
			out[c.Name] = c
		}
	}
	return out
}

// §11.5 row 12, safety interval 10 min: the computed KGWUI-011 is a warning
// at exactly 30 s, raised by the deadline. Before it there is no finding,
// and it never makes a check pending.
func TestComputedKGWUI011(t *testing.T) {
	w := dworld{routes: append(dbase(testutil.DelegateTo("shop", "c")), dleaf("shop", "c", "/other", nil))}
	h := newHarness(t, nil)
	h.src.setBase(w.snapshot(t), fprint(1, accessOK))
	h.start(10 * time.Minute)
	m := h.e.Current().Model
	if fs := findingsOn(m, health.CodeDelegatedNotAttached, "shop/c"); len(fs) != 0 || m.Next != (Deadline{At: testutil.Now.Add(Window)}) {
		t.Fatalf("at T: findings %+v, Next %+v", fs, m.Next)
	}
	if tr := delegCheck(t, m, computed.KindDelegatedNotAttached, "shop/c"); tr.Class != computed.ClassTrigger || tr.State != computed.StateChecked || tr.Mature {
		t.Fatalf("trigger at T: %+v", tr)
	}
	before := requiredChecks(m)
	h.c.Step(Window - time.Nanosecond)
	h.await(EvConfirmed)
	h.await(EvArmed)
	if h.runs != 0 {
		t.Fatalf("at T+30s-1ns: %d runs", h.runs)
	}
	h.c.Step(time.Nanosecond)
	e := h.await(EvPublished)
	if !e.At.Equal(testutil.Now.Add(Window)) {
		t.Fatalf("maturation run at %v, want T+30s", e.At)
	}
	m = e.Published.Model
	f := oneFinding(t, m, health.CodeDelegatedNotAttached, "shop/c", testutil.Now)
	if f.Severity != health.SeverityWarning || f.Message != health.ComputedNotAttachedMessage ||
		f.Detail != "The UI computes that HTTPRoute shop/api drops this route: no rule fits the parent's matches." {
		t.Fatalf("finding %+v", f)
	}
	if tr := delegCheck(t, m, computed.KindDelegatedNotAttached, "shop/c"); tr.State != computed.StateChecked || !tr.Mature {
		t.Fatalf("trigger at T+30s: %+v", tr)
	}
	if after := requiredChecks(m); !reflect.DeepEqual(before, after) {
		t.Fatalf("required checks changed:\nbefore %+v\nafter  %+v", before, after)
	}
}

// orphanDetail is the orphan trigger's detail.
const orphanDetail = "No parent route in this install keeps this route, and it has no parentRefs of its own."

// §11.5 row 18: an orphan dropped by every selecting wildcard ref is one
// KGWUI-011 warning after 30 s, however many parents select it.
func TestOrphanWarningAfter30s(t *testing.T) {
	w := dworld{routes: append(dbase(testutil.DelegateWildcard("apps")), droot("web", "/web", testutil.DelegateWildcard("apps")), dleaf("apps", "o", "/other", nil))}
	s := newDStepper(t, nil)
	w.step(s, 0)
	noFinding(t, w.step(s, Window-time.Nanosecond), health.CodeDelegatedNotAttached, "apps/o", "T+30s-1ns")
	m := w.step(s, Window)
	f := oneFinding(t, m, health.CodeDelegatedNotAttached, "apps/o", testutil.Now)
	tr := delegCheck(t, m, computed.KindOrphan, "apps/o")
	if f.Severity != health.SeverityWarning || f.Detail != orphanDetail || f.Message != health.ComputedNotAttachedMessage || !reflect.DeepEqual(f.Evidence, []objects.Ref{edgeRef.Ref()}) {
		t.Fatalf("finding %+v, deps %v", f, tr.Deps)
	}
}

// §11.5 row 19: a selecting parent cut by a compute bound gives no finding.
// Config.DelegationBounds sets the bounds; the cap is logged once.
func TestOrphanIncompleteNoFinding(t *testing.T) {
	var log bytes.Buffer
	w := dworld{routes: append(dbase(testutil.DelegateWildcard("apps")), droot("web", "/web", testutil.DelegateWildcard("apps")), dleaf("apps", "o", "/other", nil))}
	s := newDStepper(t, func(c *Config) {
		c.DelegationBounds = computed.DefaultBounds
		c.DelegationBounds.Contexts = 1
		c.Logger = slog.New(slog.NewTextHandler(&log, nil))
	})
	w.step(s, 0)
	w.step(s, time.Second)
	m := w.step(s, 2*Window)
	if c := m.Installs[0].Computed.Delegation.Cap; c != computed.CapContexts {
		t.Fatalf("cap %q", c)
	}
	noFinding(t, m, health.CodeDelegatedNotAttached, "apps/o", "with a cut parent")
	if n := strings.Count(log.String(), "cap=contexts"); n != 1 {
		t.Fatalf("cap logged %d times:\n%s", n, log.String())
	}
}

// §11.5 row 20: a route with its own parentRefs, wildcard-selected and
// forbidden, gets no finding; its row keeps the reason.
func TestRouteWithParentRefsNoOrphan(t *testing.T) {
	g := testutil.WithRules(testutil.HTTPRoute("apps", "g", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		drule([]string{"/other"}, map[string]any{"name": "svc", "port": int64(80)}))
	w := dworld{routes: append(dbase(testutil.DelegateWildcard("apps")), g)}
	s := newDStepper(t, nil)
	w.step(s, 0)
	m := w.step(s, 2*Window)
	noFinding(t, m, health.CodeDelegatedNotAttached, "apps/g", "T+60s")
	forbidden := false
	for _, r := range m.Installs[0].Computed.Delegation.Rows {
		forbidden = forbidden || r.Key.Child.String() == "apps/g" && r.Outcome == computed.OutcomeForbidden
	}
	if !forbidden {
		t.Fatal("no forbidden row for apps/g")
	}
}

// §11.5 row 21: dropped by one parent's wildcard, attached by another's: no KGWUI-011.
func TestOrphanAttachedByAnotherParent(t *testing.T) {
	w := dworld{routes: append(dbase(testutil.DelegateWildcard("apps")), droot("oth", "/other", testutil.DelegateWildcard("apps")), dleaf("apps", "o", "/other/o", nil))}
	s := newDStepper(t, nil)
	w.step(s, 0)
	noFinding(t, w.step(s, 2*Window), health.CodeDelegatedNotAttached, "apps/o", "T+60s")
}

// §11.5 row 22: a new selecting parent restarts the orphan window.
func TestNewSelectingParentResetsOrphan(t *testing.T) {
	w := dworld{routes: append(dbase(testutil.DelegateWildcard("apps")), dleaf("apps", "o", "/other", nil))}
	s := newDStepper(t, nil)
	w.step(s, 0)
	reset := 20 * time.Second
	w.routes = append(w.routes, droot("web", "/web", testutil.DelegateWildcard("apps")))
	w.step(s, reset)
	noFinding(t, w.step(s, Window), health.CodeDelegatedNotAttached, "apps/o", "T+30s")
	oneFinding(t, w.step(s, reset+Window), health.CodeDelegatedNotAttached, "apps/o", testutil.Now.Add(reset))
}

// §11.5 row 23: an orphan whose only selecting wildcard ref fails with a cycle: no finding.
func TestOrphanCycleNoFinding(t *testing.T) {
	p := testutil.WithRules(testutil.HTTPRoute("apps", "p", nil), drule([]string{"/api/p"}, testutil.DelegateWildcard("apps")))
	w := dworld{routes: append(dbase(testutil.DelegateTo("apps", "p")), p, dleaf("apps", "o", "/other", nil))}
	s := newDStepper(t, nil)
	w.step(s, 0)
	noFinding(t, w.step(s, 2*Window), health.CodeDelegatedNotAttached, "apps/o", "T+60s")
}

// §11.5 row 24: selected by a wildcard ref and a label ref, dropped by
// both: only the label parent's per-parent KGWUI-011.
func TestOrphanAlsoLabelSelected(t *testing.T) {
	o := dleaf("apps", "o", "/other", nil)
	o.SetLabels(map[string]string{objects.DelegationLabelKey: "team"})
	w := dworld{routes: append(dbase(testutil.DelegateWildcard("apps")), droot("lab", "/lab", testutil.DelegateLabel("apps", "team")), o)}
	s := newDStepper(t, nil)
	w.step(s, 0)
	f := oneFinding(t, w.step(s, Window), health.CodeDelegatedNotAttached, "apps/o", testutil.Now)
	if f.Detail != "The UI computes that HTTPRoute shop/lab drops this route: no rule fits the parent's matches." {
		t.Fatalf("detail %q", f.Detail)
	}
}

// §11.5: a KGWUI-012 and both KGWUI-011 kinds raised after their windows
// carry their own window starts.
func TestKGWUI011SinceIsWindowStart(t *testing.T) {
	d := dleaf("shop", "d", "/other", nil)
	w := dworld{routes: append(dbase(testutil.DelegateTo("shop", "c"), testutil.DelegateTo("shop", "d"), testutil.DelegateWildcard("apps")),
		dleaf("shop", "c", "/other", nil), d, dleaf("apps", "o", "/other", nil))}
	s := newDStepper(t, nil)
	w.step(s, 0)
	// d gains an entry it should not have at T+10s: a KGWUI-012 window, and no trigger.
	left := 10 * time.Second
	w.routes[3] = dleaf("shop", "d", "/other", nil, under("api"))
	w.step(s, left)
	m := w.step(s, left+Window)
	oneFinding(t, m, health.CodeDelegatedNotAttached, "shop/c", testutil.Now)
	oneFinding(t, m, health.CodeDelegatedNotAttached, "apps/o", testutil.Now)
	oneFinding(t, m, health.CodeComputedDisagrees, "shop/d", testutil.Now.Add(left))
	noFinding(t, m, health.CodeDelegatedNotAttached, "shop/d", "with an entry")
}

// The status and computed triggers on one (route, parent) are one finding,
// with the computed reason appended.
func TestKGWUI011OnePerRouteParent(t *testing.T) {
	c := dleaf("shop", "c", "/other", []objects.ParentRef{testutil.HTTPRouteRef("shop", "api")})
	w := dworld{routes: append(dbase(testutil.DelegateTo("shop", "c")), c)}
	s := newDStepper(t, nil)
	w.step(s, 0)
	m := w.step(s, health.Grace+time.Second)
	f := oneFinding(t, m, health.CodeDelegatedNotAttached, "shop/c", testutil.Now)
	want := "No status entry for parent HTTPRoute shop/api. The UI computes that HTTPRoute shop/api drops this route: no rule fits the parent's matches."
	if f.Severity != health.SeverityWarning || f.Detail != want || f.Message != health.DelegatedNotAttachedMessage {
		t.Fatalf("finding %+v", f)
	}
}

// unknownScope resolves every install's scope as unknown.
type unknownScope struct{}

func (unknownScope) Name() string { return "unknown" }
func (unknownScope) Scope(install.Install) install.Scope {
	return install.Scope{Mode: install.ScopeUnknown, Err: "test"}
}

// D-024: with an unknown scope, views B and C raise no computed or orphan
// KGWUI-011 and no KGWUI-012, and keep no windows.
func TestScopeUnknownNoDelegationFindings(t *testing.T) {
	w := dworld{routes: append(dbase(testutil.DelegateTo("shop", "c"), testutil.DelegateTo("shop", "d"), testutil.DelegateTo("shop", "m"), testutil.DelegateWildcard("apps")),
		dleaf("shop", "c", "/other", nil), dleaf("shop", "d", "/other", nil, under("api")), dleaf("shop", "m", "/api/m", nil), dleaf("apps", "o", "/other", nil))}
	s := newDStepper(t, func(c *Config) { c.Scope = unknownScope{} })
	w.step(s, 0)
	m := w.step(s, 2*time.Minute)
	for _, res := range []string{"shop/api", "shop/c", "shop/d", "shop/m", "apps/o"} {
		noFinding(t, m, health.CodeDelegatedNotAttached, res, "scope unknown")
		noFinding(t, m, health.CodeComputedDisagrees, res, "scope unknown")
	}
	n := 0
	for _, c := range m.Installs[0].Computed.Checks {
		switch c.ID.Kind {
		case computed.KindChildEntry, computed.KindParentResolvedRefs:
			n++
			if c.State != computed.StateUnknown || c.Unknown != computed.UnknownScope {
				t.Fatalf("%s on %s: %s %s", c.ID.Kind, c.On, c.State, c.Unknown)
			}
		case computed.KindDelegatedNotAttached, computed.KindOrphan:
			t.Fatalf("trigger %s on %s", c.ID.Kind, c.On)
		}
	}
	if n == 0 || len(s.e.persist.m) != 0 {
		t.Fatalf("%d delegation checks, %d windows", n, len(s.e.persist.m))
	}
}

// The cap is logged once per distinct value, as install rejections are:
// clearing and hitting it again logs nothing new.
func TestDelegationCapLoggedOncePerValue(t *testing.T) {
	var log bytes.Buffer
	capped := dworld{routes: append(dbase(testutil.DelegateWildcard("apps")), dleaf("apps", "o", "/other", nil))}
	clear := dworld{routes: []*unstructured.Unstructured{droot("api", "/api", map[string]any{"name": "svc", "port": int64(80)})}}
	s := newDStepper(t, func(c *Config) {
		c.DelegationBounds = computed.DefaultBounds
		c.DelegationBounds.Contexts = 1
		c.Logger = slog.New(slog.NewTextHandler(&log, nil))
	})
	for i, w := range []dworld{capped, clear, capped, clear, capped} {
		m := w.step(s, time.Duration(i)*time.Second)
		if got, want := m.Installs[0].Computed.Delegation.Cap, map[bool]string{true: computed.CapContexts, false: ""}[i%2 == 0]; got != want {
			t.Fatalf("step %d: cap %q, want %q", i, got, want)
		}
	}
	if n := strings.Count(log.String(), "cap=contexts"); n != 1 {
		t.Fatalf("cap logged %d times:\n%s", n, log.String())
	}
}

// D-024 silences only the computed and orphan triggers: under an unknown
// scope the 2.2 status trigger still fires for a declared candidate.
func TestScopeUnknownKeepsStatusTrigger(t *testing.T) {
	c := dleaf("shop", "c", "/other", []objects.ParentRef{testutil.HTTPRouteRef("shop", "api")})
	w := dworld{routes: append(dbase(testutil.DelegateTo("shop", "c")), c)}
	s := newDStepper(t, func(c *Config) { c.Scope = unknownScope{} })
	w.step(s, 0)
	fs := findingsOn(w.step(s, health.Grace+time.Second), health.CodeDelegatedNotAttached, "shop/c")
	if len(fs) != 1 || fs[0].Severity != health.SeverityWarning || fs[0].Detail != "No status entry for parent HTTPRoute shop/api" || fs[0].Since != nil {
		t.Fatalf("want the status trigger alone: %+v", fs)
	}
}
