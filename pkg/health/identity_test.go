package health

import (
	"regexp"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

var idFormat = regexp.MustCompile(`^f1-[a-z2-7]{26}$`)

// sinceOf returns the since entry of id in check's Store.
func sinceOf(s *States, check, id string) (time.Time, bool) {
	return Load[time.Time](s.store(check), sincePrefix+id)
}

// busyWorld raises most codes at once: two 002 on one Gateway, 002 per route
// parent and per policy ancestor, 003 per parent and on self, 001, 006, 007 and 008.
func busyWorld(t *testing.T) Input {
	t.Helper()
	gw, gone := testutil.GwRef("infra", "edge"), testutil.GwRef("infra", "gone")
	w := healthyWorld()
	w.objs[1] = testutil.Gateway("infra", "edge", "kgateway", []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionFalse, "Invalid"), testutil.Cond("Programmed", metav1.ConditionFalse, "Invalid")})
	w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw, withSection("infra", "edge", "http"), gone})
	w.objs = append(w.objs,
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{
			testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}}),
		testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
			Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "Invalid")}}),
		testutil.TrafficPolicy("shop", "lonely"),
		testutil.TrafficPolicy("shop", "ports", portAncestor(gw, 80), portAncestor(gw, 443)))
	w.metrics.Metrics.Rejects[ctrlmetrics.GatewayKey{Namespace: "infra", Name: "edge"}] = 1
	w.deploys[1] = testutil.ProxyDeployment("infra", "edge", 2, 1)
	w.access = map[schema.GroupVersionResource]cache.Access{cache.PodsGVR: {Err: "denied"}, cache.DeploymentsGVR: {Err: "denied"}}
	w.computed = &computed.Views{RuleSet: "kgateway-2.4", Checks: []computed.CheckResult{
		disagreeing(computed.KindListenerCount, "edge http"), disagreeing(computed.KindRouteAccepted, "edge cart")}}
	in := w.input(t)
	in.Verify = scopeverify.Result{Available: true, Mismatches: []scopeverify.Mismatch{
		{Namespace: "shop", Resource: "HTTPRoute", UI: 2, Controller: 1}, {Namespace: "shop", Resource: "GRPCRoute", UI: 2, Controller: 1}}}
	return in
}

// portAncestor is a rejecting ancestor entry for gw at port.
func portAncestor(gw objects.ParentRef, port int32) testutil.PolicyAncestor {
	gw.Port = port
	return testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "Invalid")}}
}

// disagreeing is a mature KGWUI-012 cross-check on infra/edge.
func disagreeing(kind computed.CheckKind, subject string) computed.CheckResult {
	edge := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}
	return computed.CheckResult{ID: computed.Identity{Install: "kgw.kgateway", RuleSet: "kgateway-2.4", Kind: kind, Subject: subject},
		Class: computed.ClassContradicting, On: edge, State: computed.StateDisagrees, Mature: true, Since: testutil.Now}
}

func TestIDFormat(t *testing.T) {
	r := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}
	for _, id := range []string{
		FindingID("kgw.kgateway", CodeXDSRejected, &r, nil),
		FindingID("kgw.kgateway", CodeSignalUnavailable, nil, []string{"xDS rejects"}),
		FindingID("", "", nil, nil),
		GroupID("kgw.kgateway", "rejects", &r, nil),
	} {
		if !idFormat.MatchString(id) {
			t.Errorf("id %q does not match %s", id, idFormat)
		}
	}
	rep := Evaluate(busyWorld(t), Upstream(), Runtime{States: NewStates()})
	for _, f := range rep.Findings {
		if !idFormat.MatchString(f.ID) {
			t.Errorf("%s on %v: id %q", f.Code, f.Resource, f.ID)
		}
	}
	// A group id is a finding id with code G and the family first in the key.
	if GroupID("i", "rejects", &r, []string{"k"}) != FindingID("i", "G", &r, []string{"rejects", "k"}) {
		t.Error("GroupID must be FindingID with code G and the family prepended to the cause key")
	}
}

// The id is a pure function of its inputs: equal across recomputes, after a
// restart (new States) and across builds (a pinned value).
func TestIDStableAcrossRecomputesAndRestart(t *testing.T) {
	r := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}
	if got, want := FindingID("kgw.kgateway", CodeXDSRejected, &r, nil), "f1-qslrmw72bg3lfdomn6i7errpxd"; got != want {
		t.Fatalf("FindingID = %s, want the pinned %s: the f1 scheme must not change", got, want)
	}
	ids := func(rep Report) []string {
		out := make([]string, len(rep.Findings))
		for i, f := range rep.Findings {
			out[i] = f.ID
		}
		return out
	}
	in := busyWorld(t)
	states := NewStates()
	first := ids(Evaluate(in, Upstream(), Runtime{States: states}))
	in.Now = in.Now.Add(time.Minute)
	in.Metrics.CollectedAt, in.Snapshot.TakenAt = in.Now, in.Now
	again := ids(Evaluate(in, Upstream(), Runtime{States: states}))
	restarted := ids(Evaluate(busyWorld(t), Upstream(), Runtime{States: NewStates()}))
	if len(first) == 0 {
		t.Fatal("busy world has no findings")
	}
	for i := range first {
		if first[i] != again[i] || first[i] != restarted[i] {
			t.Fatalf("finding %d: id %s, recompute %s, restart %s", i, first[i], again[i], restarted[i])
		}
	}
}

// Message, detail, severity and since are not part of the identity.
func TestRewordingKeepsID(t *testing.T) {
	r := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}
	early, late := testutil.Now.Add(-time.Hour), testutil.Now
	variants := []Finding{
		{Code: "ACME-001", Severity: SeverityInfo, Resource: &r, Message: "one", Detail: "a", Since: &early, Key: []string{"k"}},
		{Code: "ACME-001", Severity: SeverityError, Resource: &r, Message: "two", Detail: "b", Since: &late, Key: []string{"k"}},
		{Code: "ACME-001", Severity: SeverityWarning, Resource: &r, Message: "three", Key: []string{"k"}},
	}
	var ids []string
	for _, v := range variants {
		c := funcCheck{name: "probe", run: func(RunContext) ([]Finding, CheckResult) { return []Finding{v}, CheckResult{Name: "probe", OK: true} }}
		rep := Evaluate(Input{Now: testutil.Now}, []Check{c}, Runtime{States: NewStates()})
		ids = append(ids, rep.Findings[0].ID)
	}
	if ids[0] != ids[1] || ids[1] != ids[2] {
		t.Fatalf("rewording changed the id: %v", ids)
	}
}

// Discriminators separate findings of one code on one resource: two KGWUI-012
// checks and two conditions on a Gateway, two policy ancestors differing by port,
// and two KGWUI-011 parentRefs to one route differing by section.
func TestDiscriminatorSeparates(t *testing.T) {
	in := busyWorld(t)
	rep := Evaluate(in, Upstream(), Runtime{States: NewStates()})
	ids := map[string]map[string]bool{} // code and resource to ids
	for _, f := range rep.Findings {
		if f.Resource == nil {
			continue
		}
		k := f.Code + " " + f.Resource.Kind + " " + f.Resource.String()
		if ids[k] == nil {
			ids[k] = map[string]bool{}
		}
		ids[k][f.ID] = true
	}
	for _, k := range []string{CodeComputedDisagrees + " Gateway infra/edge", CodeConditionFalse + " Gateway infra/edge", CodeConditionFalse + " TrafficPolicy shop/ports"} {
		if len(ids[k]) != 2 {
			t.Errorf("%s: %d distinct ids, want 2", k, len(ids[k]))
		}
	}
	// Assign declares one candidate per parent route, so the section case is built directly.
	api := testutil.HTTPRouteRef("shop", "api")
	a, b := api, api
	a.SectionName, b.SectionName = "a", "b"
	child := testutil.HTTPRoute("shop", "c", []objects.ParentRef{a, b})
	declared := make(map[string]bool)
	for _, w := range objects.DecodeHTTPRoute(child).ParentRefs {
		f := declaredFinding(in, install.DeclaredCandidate{Route: install.OwnedObject{Obj: child}, Parent: w})
		declared[FindingID(in.Install.ID(), f.Code, f.Resource, f.Key)] = true
	}
	if len(declared) != 2 {
		t.Errorf("KGWUI-011 on two sections of one parent route: %d distinct ids, want 2", len(declared))
	}
	if FindingID("i", "c", nil, []string{"a"}) == FindingID("i", "c", nil, []string{"b"}) {
		t.Error("different keys must give different ids")
	}
}

// No two findings of one install share an id, in any fixture world.
func TestIDUniqueWithinInstall(t *testing.T) {
	worlds := GuardWorlds(t)
	worlds["busy"] = busyWorld(t)
	for name, in := range worlds {
		rep := Evaluate(in, Upstream(), Runtime{States: NewStates()})
		seen := map[string]Finding{}
		for _, f := range rep.Findings {
			if prev, dup := seen[f.ID]; dup {
				t.Errorf("%s: %s %v and %s %v share id %s", name, prev.Code, prev.Resource, f.Code, f.Resource, f.ID)
			}
			seen[f.ID] = f
		}
	}
	if n := len(Evaluate(worlds["busy"], Upstream(), Runtime{}).Findings); n < 15 {
		t.Fatalf("busy world has %d findings; it must raise most codes", n)
	}
}

// A UI-derived since survives recomputes, is pruned when neither returned nor
// retained, and resets on restart.
func TestUIDerivedSinceSurvivesRecomputeAndResetsOnRestart(t *testing.T) {
	w := healthyWorld()
	w.metrics.Metrics.Rejects[ctrlmetrics.GatewayKey{Namespace: "infra", Name: "edge"}] = 1
	in := w.input(t)
	xds := func(rep Report) *Finding {
		for i := range rep.Findings {
			if rep.Findings[i].Code == CodeXDSRejected {
				return &rep.Findings[i]
			}
		}
		return nil
	}
	states := NewStates()
	t0 := in.Now
	first := xds(Evaluate(in, Upstream(), Runtime{States: states}))
	if first == nil || first.Since == nil || !first.Since.Equal(t0) {
		t.Fatalf("first KGWUI-001 %+v, want since %v", first, t0)
	}
	in.Now, in.Metrics.CollectedAt = t0.Add(time.Minute), t0.Add(time.Minute)
	if f := xds(Evaluate(in, Upstream(), Runtime{States: states})); f == nil || !f.Since.Equal(t0) || f.ID != first.ID {
		t.Fatalf("recomputed KGWUI-001 %+v, want since %v", f, t0)
	}
	// Cleared: the entry is pruned, so a later reject starts again.
	in.Metrics.Metrics.Rejects = map[ctrlmetrics.GatewayKey]float64{}
	in.Now, in.Metrics.CollectedAt = t0.Add(2*time.Minute), t0.Add(2*time.Minute)
	Evaluate(in, Upstream(), Runtime{States: states})
	if _, kept := sinceOf(states, "xDS rejects", first.ID); kept {
		t.Fatal("since entry of a finding no longer returned must be pruned")
	}
	in.Metrics.Metrics.Rejects = map[ctrlmetrics.GatewayKey]float64{{Namespace: "infra", Name: "edge"}: 1}
	t3 := t0.Add(3 * time.Minute)
	in.Now, in.Metrics.CollectedAt = t3, t3
	if f := xds(Evaluate(in, Upstream(), Runtime{States: states})); f == nil || !f.Since.Equal(t3) {
		t.Fatalf("returned KGWUI-001 %+v, want since %v", f, t3)
	}
	// A restart is a new States.
	t4 := t0.Add(4 * time.Minute)
	in.Now, in.Metrics.CollectedAt = t4, t4
	if f := xds(Evaluate(in, Upstream(), Runtime{States: NewStates()})); f == nil || !f.Since.Equal(t4) {
		t.Fatalf("KGWUI-001 after a restart %+v, want since %v", f, t4)
	}

	// Retain keeps an entry the run does not return.
	r := objects.Ref{Kind: "Namespace", Name: "shop"}
	id := FindingID(Input{}.Install.ID(), "ACME-001", &r, nil)
	returns := true
	c := funcCheck{name: "probe", inputs: InputSet{Clock: true}, run: func(ctx RunContext) ([]Finding, CheckResult) {
		if !returns {
			ctx.Retain(id)
			return nil, CheckResult{Name: "probe", OK: true}
		}
		return []Finding{{Code: "ACME-001", Severity: SeverityInfo, Resource: &r, Message: "m"}}, CheckResult{Name: "probe", OK: true}
	}}
	states = NewStates()
	Evaluate(Input{Now: t0}, []Check{c}, Runtime{States: states})
	returns = false
	Evaluate(Input{Now: t0.Add(time.Minute)}, []Check{c}, Runtime{States: states})
	returns = true
	rep := Evaluate(Input{Now: t0.Add(2 * time.Minute)}, []Check{c}, Runtime{States: states})
	if f := rep.Findings[0]; f.ID != id || !f.Since.Equal(t0) {
		t.Fatalf("retained finding %+v, want id %s since %v", f, id, t0)
	}
}

// On a memo skip Run does not execute, so the memo carries the ids it
// returned and retained; their since entries survive the skip.
func TestMemoSkipKeepsRetainedSince(t *testing.T) {
	r := objects.Ref{Kind: "Namespace", Name: "shop"}
	id := FindingID(Input{}.Install.ID(), "ACME-001", &r, nil)
	print, runs := uint64(1), 0
	returns := true
	c := funcCheck{name: "probe", run: func(ctx RunContext) ([]Finding, CheckResult) {
		runs++
		if !returns {
			ctx.Retain(id)
			return nil, CheckResult{Name: "probe", OK: true}
		}
		return []Finding{{Code: "ACME-001", Severity: SeverityInfo, Resource: &r, Message: "m"}}, CheckResult{Name: "probe", OK: true}
	}}
	states := NewStates()
	rt := Runtime{States: states, Print: func(InputSet) (uint64, bool) { return print, true }}
	t0 := testutil.Now
	Evaluate(Input{Now: t0}, []Check{c}, rt)
	print, returns = 2, false
	Evaluate(Input{Now: t0.Add(time.Minute)}, []Check{c}, rt)
	Evaluate(Input{Now: t0.Add(2 * time.Minute)}, []Check{c}, rt) // memo skip
	if runs != 2 {
		t.Fatalf("probe ran %d times, want 2 (the third compute reuses the memo)", runs)
	}
	if at, ok := sinceOf(states, "probe", id); !ok || !at.Equal(t0) {
		t.Fatalf("since entry after a memo skip: %v %v, want %v", at, ok, t0)
	}
	print, returns = 3, true
	rep := Evaluate(Input{Now: t0.Add(3 * time.Minute)}, []Check{c}, rt)
	if f := rep.Findings[0]; !f.Since.Equal(t0) {
		t.Fatalf("finding returned after the skip has since %v, want %v", f.Since, t0)
	}
	// A reused finding carries the same since as the run that made it.
	again := Evaluate(Input{Now: t0.Add(4 * time.Minute)}, []Check{c}, rt)
	if runs != 3 || !again.Findings[0].Since.Equal(t0) || again.Findings[0].ID != id {
		t.Fatalf("memoized finding %+v after %d runs", again.Findings[0], runs)
	}
}

// A status-derived finding keeps the condition's lastTransitionTime.
func TestStatusFindingKeepsLastTransitionTime(t *testing.T) {
	w := healthyWorld()
	w.objs[1] = testutil.Gateway("infra", "edge", "kgateway", []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionFalse, "AddressNotAssigned")})
	in := w.input(t)
	in.Now = in.Now.Add(time.Hour)
	rep := Evaluate(in, Upstream(), Runtime{States: NewStates()})
	for _, f := range rep.Findings {
		if f.Code != CodeConditionFalse {
			continue
		}
		cond := objects.FindCondition(objects.Conditions(w.objs[1]), "Programmed")
		if f.Since == nil || !f.Since.Equal(cond.LastTransitionTime.Time) || f.Since.Equal(in.Now) {
			t.Fatalf("KGWUI-002 since %v, want lastTransitionTime %v", f.Since, cond.LastTransitionTime)
		}
		return
	}
	t.Fatalf("no KGWUI-002: %+v", rep.Findings)
}
