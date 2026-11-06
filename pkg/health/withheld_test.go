package health

import (
	"slices"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
)

// withheldWorld is healthyWorld with Gateway infra/nack and one controller Pod
// reporting capture file (2.5 synthesised fixtures carry nack's per-client series).
func withheldWorld(t *testing.T, file string) *world {
	t.Helper()
	w := healthyWorld()
	w.objs = append(w.objs, testutil.Gateway("infra", "nack", "kgateway", nil))
	w.deploys = append(w.deploys, testutil.ProxyDeployment("infra", "nack", 1, 1))
	w.metrics = resultAt(testutil.Now, capture(t, file, "pod/kgw/p", "p"))
	return w
}

// withheldAt evaluates w with st at now, its metrics collected at now.
func withheldAt(t *testing.T, w *world, st *States, now time.Time) Report {
	t.Helper()
	in := w.input(t)
	in.Now = now
	if w.metrics != nil {
		in.Metrics.CollectedAt = now
	}
	return Evaluate(in, Upstream(), Runtime{States: st})
}

// withheldOn is the 024 finding of severity sev on Gateway infra/gw, or nil.
func withheldOn(r Report, gw string, sev Severity) *Finding {
	for i, f := range r.Findings {
		if f.Code == CodeConfigWithheld && f.Severity == sev && f.Resource != nil && f.Resource.Name == gw {
			return &r.Findings[i]
		}
	}
	return nil
}

func withheldCodes(r Report) int {
	n := 0
	for _, f := range r.Findings {
		if f.Code == CodeConfigWithheld {
			n++
		}
	}
	return n
}

// warm_withheld > 0 is an error on that Gateway; within_budget on edge is not.
func TestWithheldClientsAreAnError(t *testing.T) {
	w := withheldWorld(t, "v2.5.0-withheld-synth")
	r := withheldAt(t, w, NewStates(), testutil.Now)
	f := withheldOn(r, "nack", SeverityError)
	if f == nil || withheldCodes(r) != 1 || r.State != StateError {
		t.Fatalf("findings %+v, state %s", r.Findings, r.State)
	}
	ev, ok := f.Facts.(*WithheldEvidence)
	if f.Slug != "xds-config-withheld" || f.Message != "Proxies get no config updates, because referenced clusters are missing" || !ok ||
		ev.Withheld != 2 || len(ev.Releases) != 0 || !slices.Equal(ev.ReportedBy, []string{"p"}) {
		t.Errorf("finding %+v, evidence %+v", f, f.Facts)
	}
	if c := checkNamed(r, "withheld config"); c == nil || !c.OK || c.Required || c.Unavailable != "" {
		t.Errorf("check %+v", c)
	}
}

// Withheld clients on two replicas are summed per Gateway, with both Pods named.
func TestWithheldSumsReplicas(t *testing.T) {
	w := withheldWorld(t, "v2.5.0-withheld-synth")
	w.metrics = resultAt(testutil.Now, capture(t, "v2.5.0-withheld-synth", "pod/kgw/a", "a"), capture(t, "v2.5.0-withheld-synth", "pod/kgw/b", "b"))
	f := withheldOn(withheldAt(t, w, NewStates(), testutil.Now), "nack", SeverityError)
	if ev, _ := f.Facts.(*WithheldEvidence); ev == nil || ev.Withheld != 4 || !slices.Equal(ev.ReportedBy, []string{"a", "b"}) {
		t.Fatalf("finding %+v", f)
	}
}

// An increase of an alerting mode is a warning for 15 min; first_publish on edge
// never warns; the first scrape of a Pod is a baseline.
func TestBoundedPublishIncreaseWarns(t *testing.T) {
	st := NewStates()
	w := withheldWorld(t, "v2.5.0-withheld-synth")
	if r := withheldAt(t, w, st, testutil.Now); withheldOn(r, "nack", SeverityWarning) != nil {
		t.Fatal("a Pod's first scrape is a baseline, not an increase")
	}
	later := testutil.Now.Add(time.Minute)
	w = withheldWorld(t, "v2.5.0-released-synth")
	r := withheldAt(t, w, st, later)
	f := withheldOn(r, "nack", SeverityWarning)
	if f == nil || withheldCodes(r) != 1 || withheldOn(r, "edge", SeverityWarning) != nil || r.State != StateWarning {
		t.Fatalf("findings %+v", r.Findings)
	}
	ev, _ := f.Facts.(*WithheldEvidence)
	want := []ReleaseModeCount{{Mode: "flip_release", Count: 1}, {Mode: "warm_release", Count: 2}}
	if f.Message != "Some routes fail: config was released before every cluster was ready" || ev == nil || ev.Withheld != 0 ||
		!slices.Equal(ev.Releases, want) || !slices.Equal(ev.ReportedBy, []string{"p"}) {
		t.Errorf("finding %+v, evidence %+v", f, f.Facts)
	}
	// Unchanged counters keep the warning inside the window and clear it after.
	if r := withheldAt(t, w, st, later.Add(14*time.Minute)); withheldOn(r, "nack", SeverityWarning) == nil {
		t.Error("the warning must last 15 min")
	}
	if r := withheldAt(t, w, st, later.Add(WithheldReleaseWindow)); withheldCodes(r) != 0 || r.State != StateHealthy {
		t.Errorf("after the window: %+v", r.Findings)
	}
}

// A counter that drops is a reset after a Pod restart, never an increase; a
// new Pod UID starts a baseline.
func TestBoundedPublishResetIsNotAnIncrease(t *testing.T) {
	st := NewStates()
	withheldAt(t, withheldWorld(t, "v2.5.0-released-synth"), st, testutil.Now)
	// The container restarts under the same UID: warm_release 3 -> 1, flip_release gone.
	if r := withheldAt(t, withheldWorld(t, "v2.5.0-withheld-synth"), st, testutil.Now.Add(time.Minute)); withheldOn(r, "nack", SeverityWarning) != nil {
		t.Errorf("a reset read as an increase: %+v", r.Findings)
	}
	w := withheldWorld(t, "v2.5.0-released-synth")
	w.metrics = resultAt(testutil.Now, capture(t, "v2.5.0-released-synth", "pod/kgw/q", "q"))
	if r := withheldAt(t, w, st, testutil.Now.Add(2*time.Minute)); withheldOn(r, "nack", SeverityWarning) != nil {
		t.Errorf("a new Pod's first scrape read as an increase: %+v", r.Findings)
	}
}

// Without the per-client families or build_info (2.3, 2.4) 024 is not
// available on this version: no finding, no KGWUI-008, the install healthy.
func TestWithheldNotOnVersion(t *testing.T) {
	for _, file := range []string{"v2.3.11", "v2.4.7"} {
		w := healthyWorld()
		w.metrics = resultAt(testutil.Now, capture(t, file, "pod/kgw/p", "p"))
		r := withheldAt(t, w, NewStates(), testutil.Now)
		c := checkNamed(r, "withheld config")
		if c == nil || c.OK || c.Required || c.Unavailable != AvailNotOnVersion || c.Error != "" {
			t.Errorf("%s: check %+v", file, c)
		}
		if withheldCodes(r) != 0 || r.State != StateHealthy || slices.ContainsFunc(r.Findings, func(f Finding) bool { return f.Code == CodeSignalUnavailable }) {
			t.Errorf("%s: state %s, findings %+v", file, r.State, r.Findings)
		}
	}
	// The real healthy 2.5.0 capture: available, nothing held, healthy.
	w := withheldWorld(t, "v2.5.0")
	r := withheldAt(t, w, NewStates(), testutil.Now)
	if c := checkNamed(r, "withheld config"); c == nil || !c.OK || withheldCodes(r) != 0 || r.State != StateHealthy {
		t.Errorf("v2.5.0: check %+v, findings %+v", c, r.Findings)
	}
}

// Missing, failed or stale metrics make 024 not available, never KGWUI-008 of
// its own; a failed scrape keeps the counters, so recovery adds no increase.
func TestWithheldMetricsFailing(t *testing.T) {
	st := NewStates()
	withheldAt(t, withheldWorld(t, "v2.5.0-withheld-synth"), st, testutil.Now)
	for name, mutate := range map[string]func(w *world){
		"missing": func(w *world) { w.metrics = nil },
		"failed":  func(w *world) { w.metrics.Err = "connection refused" },
	} {
		w := withheldWorld(t, "v2.5.0-withheld-synth")
		mutate(w)
		r := withheldAt(t, w, st, testutil.Now.Add(time.Minute))
		if c := checkNamed(r, "withheld config"); c == nil || c.Required || c.Unavailable != AvailFailing || withheldCodes(r) != 0 {
			t.Errorf("%s: check %+v, findings %+v", name, c, r.Findings)
		}
	}
	w := withheldWorld(t, "v2.5.0-withheld-synth")
	in := w.input(t)
	in.Now = testutil.Now.Add(time.Hour) // collected at testutil.Now, an hour old
	if c := checkNamed(Evaluate(in, Upstream(), Runtime{States: NewStates()}), "withheld config"); c == nil || c.Unavailable != AvailFailing {
		t.Errorf("stale: %+v", c)
	}
	if r := withheldAt(t, withheldWorld(t, "v2.5.0-withheld-synth"), st, testutil.Now.Add(2*time.Minute)); withheldOn(r, "nack", SeverityWarning) != nil {
		t.Errorf("recovery read as an increase: %+v", r.Findings)
	}
}

// A Gateway the install does not own raises nothing.
func TestWithheldOnlyOwnedGateways(t *testing.T) {
	w := healthyWorld()
	w.metrics = resultAt(testutil.Now, capture(t, "v2.5.0-withheld-synth", "pod/kgw/p", "p"))
	if r := withheldAt(t, w, NewStates(), testutil.Now); withheldCodes(r) != 0 {
		t.Errorf("findings %+v", r.Findings)
	}
}

// When build_info disagrees with the Deployment's version, "What was checked" shows both.
func TestReportedVersionDisagreement(t *testing.T) {
	w := withheldWorld(t, "v2.5.0")
	in := w.input(t)
	in.Install.ReportedVersion = "2.5.0" // the Deployment says 2.4.7
	c := checkNamed(Evaluate(in, Upstream(), Runtime{States: NewStates()}), "withheld config")
	if c == nil || c.Detail != "The controller reports version 2.5.0, but its Deployment says 2.4.7. The UI uses 2.5.0." {
		t.Errorf("check %+v", c)
	}
	in.Install.ReportedVersion = "2.4.7"
	if c := checkNamed(Evaluate(in, Upstream(), Runtime{States: NewStates()}), "withheld config"); c == nil || c.Detail != "" {
		t.Errorf("agreeing versions: %+v", c)
	}
	in.Install.Version, in.Install.ReportedVersion = "", "2.5.0"
	if c := checkNamed(Evaluate(in, Upstream(), Runtime{States: NewStates()}), "withheld config"); c == nil ||
		c.Detail != "The controller reports version 2.5.0; its Deployment has no version. The UI uses 2.5.0." {
		t.Errorf("no Deployment version: %+v", c)
	}
}

// The warning wakes the engine at the window's end, measured from the first release.
func TestBoundedPublishWakesAtWindowEnd(t *testing.T) {
	st := NewStore()
	run := func(file string, now time.Time) (int, time.Time) {
		in := withheldWorld(t, file).input(t)
		in.Now, in.MetricsMaxAge = now, 0
		in.Metrics.CollectedAt = now
		sink := &runSink{}
		fs, _ := withheldCheck{}.Run(RunContext{Input: in, State: st, sink: sink})
		return len(fs), sink.wake
	}
	run("v2.5.0-withheld-synth", testutil.Now)
	at := testutil.Now.Add(time.Minute)
	if n, wake := run("v2.5.0-released-synth", at); n != 1 || !wake.Equal(at.Add(WithheldReleaseWindow)) {
		t.Fatalf("%d findings, wake %v", n, wake)
	}
	if n, wake := run("v2.5.0-released-synth", at.Add(10*time.Minute)); n != 1 || !wake.Equal(at.Add(WithheldReleaseWindow)) {
		t.Errorf("unchanged counters: %d findings, wake %v", n, wake)
	}
}

// Withheld proxies and releases on one Gateway each carry only their own facts and Pods.
func TestWithheldEvidencePerSignal(t *testing.T) {
	st := NewStates()
	w := withheldWorld(t, "v2.5.0-withheld-synth")
	w.metrics = resultAt(testutil.Now, capture(t, "v2.5.0-withheld-synth", "pod/kgw/a", "a"), capture(t, "v2.5.0-released-synth", "pod/kgw/b", "b"))
	withheldAt(t, w, st, testutil.Now)
	// Replica b releases once more; replica a still withholds.
	b := capture(t, "v2.5.0-released-synth", "pod/kgw/b", "b")
	b.PerPod[0].Bounded[ctrlmetrics.ModeKey{Gateway: ctrlmetrics.GatewayKey{Namespace: "infra", Name: "nack"}, Mode: "warm_release"}]++
	w.metrics = resultAt(testutil.Now, capture(t, "v2.5.0-withheld-synth", "pod/kgw/a", "a"), b)
	r := withheldAt(t, w, st, testutil.Now.Add(time.Minute))
	e, wn := withheldOn(r, "nack", SeverityError), withheldOn(r, "nack", SeverityWarning)
	if e == nil || wn == nil {
		t.Fatalf("findings %+v", r.Findings)
	}
	if ev := e.Facts.(*WithheldEvidence); ev.Withheld != 2 || ev.Releases != nil || !slices.Equal(ev.ReportedBy, []string{"a"}) || len(e.Evidence) != 1 {
		t.Errorf("error evidence %+v", ev)
	}
	if ev := wn.Facts.(*WithheldEvidence); ev.Withheld != 0 || !slices.Equal(ev.Releases, []ReleaseModeCount{{Mode: "warm_release", Count: 1}}) ||
		!slices.Equal(ev.ReportedBy, []string{"b"}) || len(wn.Evidence) != 1 {
		t.Errorf("warning evidence %+v", ev)
	}
}
