package health_test

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

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

// ResourceSince is the disagreeing object's own first-seen time, which a
// caller who cannot view every dep sees instead of the window start (design §7.4 rule 7).
func TestKGWUI012ResourceSince(t *testing.T) {
	gw := testutil.Gateway("infra", "edge", "kgateway", nil)
	seen := testutil.Now.Add(-5 * time.Minute)
	in := health.Input{Install: install.Resolved{Install: install.Install{Namespace: "kgw", Name: "kgateway"}},
		Owned: &install.Owned{Gateways: []*unstructured.Unstructured{gw}}, Snapshot: &cache.Snapshot{Synced: true}, Now: testutil.Now,
		FirstSeen: func(*unstructured.Unstructured) time.Time { return seen },
		Computed:  &computed.Views{RuleSet: "kgateway-2.4", Checks: []computed.CheckResult{countCheck(computed.StateDisagrees, computed.ClassContradicting)}}}
	rep := health.Evaluate(in, health.Upstream()[len(health.Upstream())-1:])
	if len(rep.Findings) != 1 || rep.Findings[0].ResourceSince == nil || !rep.Findings[0].ResourceSince.Equal(seen) || !rep.Findings[0].Since.Equal(testutil.Now) {
		t.Fatalf("findings %+v", rep.Findings)
	}
}

// Each KGWUI-012 keeps its own resource, and ResourceSince is that object's
// first-seen for an owned Gateway, ListenerSet or route; nil when not owned.
// Two of each resource kind make a lookup of the wrong object fail.
func TestKGWUI012SubjectsResourceSince(t *testing.T) {
	objs := []*unstructured.Unstructured{
		testutil.Gateway("infra", "edge", "kgateway", nil),
		testutil.Gateway("infra", "edge2", "kgateway", nil),
		testutil.ListenerSet("infra", "extra", testutil.GwRef("infra", "edge"), nil),
		testutil.ListenerSet("infra", "extra2", testutil.GwRef("infra", "edge2"), nil),
		testutil.HTTPRoute("shop", "a", []objects.ParentRef{testutil.GwRef("infra", "edge")}),
	}
	seen := map[objects.Ref]time.Time{}
	var checks []computed.CheckResult
	for i, u := range objs {
		on := objects.RefOf(u)
		seen[on] = testutil.Now.Add(-time.Duration(i+1) * time.Minute)
		c := countCheck(computed.StateDisagrees, computed.ClassContradicting)
		c.On, c.ID.Subject = on, on.String()
		checks = append(checks, c)
	}
	gone := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "gone"}
	c := countCheck(computed.StateDisagrees, computed.ClassContradicting)
	c.On, c.ID.Subject = gone, gone.String()
	checks = append(checks, c)
	in := health.Input{Install: install.Resolved{Install: install.Install{Namespace: "kgw", Name: "kgateway"}},
		Owned:    &install.Owned{Gateways: objs[:2], ListenerSets: objs[2:4], Routes: []install.OwnedObject{{Obj: objs[4]}}},
		Snapshot: &cache.Snapshot{Synced: true}, Now: testutil.Now,
		FirstSeen: func(u *unstructured.Unstructured) time.Time { return seen[objects.RefOf(u)] },
		Computed:  &computed.Views{RuleSet: "kgateway-2.4", Checks: checks}}
	rep := health.Evaluate(in, health.Upstream()[len(health.Upstream())-1:])
	if len(rep.Findings) != len(checks) {
		t.Fatalf("findings %+v", rep.Findings)
	}
	for _, f := range rep.Findings {
		want, owned := seen[*f.Resource]
		switch {
		case !owned && f.ResourceSince != nil:
			t.Errorf("%s: ResourceSince %v, want none", f.Resource, f.ResourceSince)
		case owned && (f.ResourceSince == nil || !f.ResourceSince.Equal(want)):
			t.Errorf("%s: ResourceSince %v, want %v", f.Resource, f.ResourceSince, want)
		}
	}
}

// The child-entry detail names the parent route; the parent-resolvedrefs
// detail names ResolvedRefs and the parent's own report key (PF7).
func TestDisagreeDetailDelegation(t *testing.T) {
	c := countCheck(computed.StateDisagrees, computed.ClassMissing)
	c.ID.Kind, c.Listener = computed.KindChildEntry, ""
	c.On = objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "c"}
	c.ID.Subject = computed.Subject(objects.GatewayAPIGroup, "HTTPRoute", "shop", "c", objects.GatewayAPIGroup, "HTTPRoute", "shop", "api",
		objects.GatewayAPIGroup, "HTTPRoute", "shop", "api", "")
	c.Key = computed.ReportKey{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "api"}
	for _, tc := range []struct{ expected, observed, want string }{
		{"entry", "none", "Status entry for parent HTTPRoute `shop/api`: computed an entry, controller reports no entry. Rule set kgateway-2.4."},
		{"none", "True", "Status entry for parent HTTPRoute `shop/api`: computed no entry, controller reports Accepted=True. Rule set kgateway-2.4."},
		{"False/UnsupportedValue", "True", "Status entry for parent HTTPRoute `shop/api`: computed Accepted=False/UnsupportedValue, controller reports Accepted=True. Rule set kgateway-2.4."},
	} {
		c.Expected, c.Observed = tc.expected, tc.observed
		if rep := runComputed(t, c); len(rep.Findings) != 1 || rep.Findings[0].Detail != tc.want {
			t.Errorf("got %+v\nwant detail %q", rep.Findings, tc.want)
		}
	}

	p := countCheck(computed.StateDisagrees, computed.ClassContradicting)
	p.ID.Kind, p.Listener = computed.KindParentResolvedRefs, ""
	p.On = objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "api"}
	p.Key = computed.ReportKey{Name: "edge", SectionName: "http"}
	p.Expected, p.Observed = "False/BackendNotFound", "True"
	want := "ResolvedRefs under parent `edge` (section `http`): computed False/BackendNotFound, controller reports True. Rule set kgateway-2.4."
	if rep := runComputed(t, p); len(rep.Findings) != 1 || rep.Findings[0].Detail != want {
		t.Fatalf("got %+v\nwant detail %q", rep.Findings, want)
	}
}
