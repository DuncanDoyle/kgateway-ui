package health_test

import (
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

var edge = objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}

// countCheck is a listener-count check on infra/edge http in state.
func countCheck(state computed.State, class computed.Class) computed.CheckResult {
	return computed.CheckResult{
		ID:    computed.Identity{Install: "kgw.kgateway", RuleSet: "kgateway-2.4", Kind: computed.KindListenerCount, Subject: "edge http"},
		Class: class, On: edge, Listener: "http", Expected: "2", Observed: "1",
		Deps:  []objects.Ref{edge, {Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "a"}},
		State: state, Mature: state == computed.StateDisagrees, Since: testutil.Now,
	}
}

func runComputed(t *testing.T, cs ...computed.CheckResult) health.Report {
	t.Helper()
	in := health.Input{Install: install.Resolved{Install: install.Install{Namespace: "kgw", Name: "kgateway"}},
		Snapshot: &cache.Snapshot{Synced: true}, Now: testutil.Now.Add(time.Minute),
		Computed: &computed.Views{RuleSet: "kgateway-2.4", Checks: cs}}
	var check health.Check
	for _, c := range health.Upstream() {
		if c.Name() == "computed views" {
			check = c
		}
	}
	if check == nil {
		t.Fatal("no computed views check registered")
	}
	return health.Evaluate(in, []health.Check{check})
}

// KGWUI-012 is a warning on the disagreeing object, never an error.
func TestKGWUI012IsWarning(t *testing.T) {
	rep := runComputed(t, countCheck(computed.StateDisagrees, computed.ClassContradicting))
	if len(rep.Findings) != 1 {
		t.Fatalf("findings %+v", rep.Findings)
	}
	f := rep.Findings[0]
	if f.Code != health.CodeComputedDisagrees || f.Slug != "computed-view-disagrees" || f.Severity != health.SeverityWarning ||
		f.Resource == nil || *f.Resource != edge || f.Since == nil || !f.Since.Equal(testutil.Now) || len(f.Evidence) != 2 {
		t.Fatalf("finding %+v", f)
	}
	if f.Message != "The UI's computed view disagrees with the controller" ||
		f.Detail != "Listener `http`: computed 2 routes, controller reports 1. Rule set kgateway-2.4." {
		t.Fatalf("wording %q / %q", f.Message, f.Detail)
	}
	if rep.State != health.StateWarning || rep.Counts[health.SeverityError] != 0 {
		t.Fatalf("install state %s counts %v", rep.State, rep.Counts)
	}
	if got := explore.RowState(edge, rep); got != health.StateWarning {
		t.Fatalf("Gateway row %s, want warning", got)
	}
	c := rep.Checks[0]
	if c.Name != "computed views" || !c.OK || c.Required {
		t.Fatalf("check %+v", c)
	}
}

// Unknown, stale (pending), compatible and inconclusive checks raise nothing;
// nor does a contradiction still inside its window.
func TestNoFindingForUnknownStaleCompatible(t *testing.T) {
	rep := runComputed(t,
		countCheck(computed.StateUnknown, computed.ClassUnknown),
		countCheck(computed.StatePending, computed.ClassStale),
		countCheck(computed.StateChecked, computed.ClassCompatible),
		countCheck(computed.StateChecked, computed.ClassInconclusive),
		countCheck(computed.StatePending, computed.ClassContradicting))
	if len(rep.Findings) != 0 || rep.State != health.StateHealthy {
		t.Fatalf("findings %+v state %s", rep.Findings, rep.State)
	}
	if rep := runComputed(t); len(rep.Findings) != 0 || !rep.Checks[0].OK {
		t.Fatalf("no views: %+v", rep)
	}
}

// The route-accepted detail names the parentRef by its report key.
func TestDisagreeDetailRouteAccepted(t *testing.T) {
	c := countCheck(computed.StateDisagrees, computed.ClassContradicting)
	c.ID.Kind, c.Listener = computed.KindRouteAccepted, ""
	c.On = objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "a"}
	c.Key = computed.ReportKey{Name: "edge", SectionName: "http"}
	c.Expected, c.Observed, c.Note = "accepted", "False/NoMatchingParent", "Rules not yet verified for 2.5"
	rep := runComputed(t, c)
	want := "Parent `edge` (section `http`): computed accepted, controller reports False/NoMatchingParent. Rule set kgateway-2.4. Rules not yet verified for 2.5."
	if len(rep.Findings) != 1 || rep.Findings[0].Detail != want {
		t.Fatalf("got %+v\nwant detail %q", rep.Findings, want)
	}
}
