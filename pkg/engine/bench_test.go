package engine

import (
	"slices"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
)

// scaleEngine is an upstream engine over tier's scale snapshot with fresh
// metrics. Under Contradict it has computed once and its clock is past the
// window and the grace end, so every check that can mature does.
func scaleEngine(tier scale.Tier) *Engine {
	now := testutil.Now
	cfg := config(scale.Snapshot(tier, 1), metricsAt(testutil.Now), func() time.Time { return now })
	cfg.MetricsMaxAge = 0
	e := New(cfg)
	if tier.Contradict {
		e.Compute()
		now = now.Add(max(Window, health.Grace) + time.Second)
	}
	return e
}

// BenchmarkCompute measures one warm Compute per iteration: the engine has
// already seen the snapshot once, as after the first run in production.
func BenchmarkCompute(b *testing.B) {
	for _, tier := range append(slices.Clone(scale.Tiers), scale.Large.Contradicting()) {
		b.Run(tier.Name, func(b *testing.B) {
			e := scaleEngine(tier)
			e.Compute()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				e.Compute()
			}
		})
	}
}

// The Large world yields one install that owns every HTTPRoute: delegated
// children through their reported edges (Q12), each with a reported parent.
func TestScaleComputeSanity(t *testing.T) {
	snap := scale.Snapshot(scale.Large, 1)
	children := map[string]bool{}
	for _, r := range snap.Objects[gvr(t, "HTTPRoute")] {
		for _, p := range r.Object["status"].(map[string]any)["parents"].([]any) {
			if p.(map[string]any)["parentRef"].(map[string]any)["kind"] == "HTTPRoute" {
				children[r.GetNamespace()+"/"+r.GetName()] = true
			}
		}
	}
	m := New(config(snap, metricsAt(testutil.Now), func() time.Time { return testutil.Now })).Compute()
	if len(m.Installs) != 1 {
		t.Fatalf("installs %d", len(m.Installs))
	}
	owned, reported := 0, 0
	for _, r := range m.Installs[0].Owned.Routes {
		if r.Kind.Kind != "HTTPRoute" {
			continue
		}
		owned++
		if children[r.Obj.GetNamespace()+"/"+r.Obj.GetName()] && len(r.RouteParents) > 0 {
			reported++
		}
	}
	if len(children) == 0 || owned != scale.Large.HTTPRoutes || reported != len(children) {
		t.Fatalf("owned HTTPRoutes %d, want %d; children with a reported parent %d, want %d", owned, scale.Large.HTTPRoutes, reported, len(children))
	}
	// Views B and C agree with the consistent world: no delegation check is a candidate (PF2).
	v := m.Installs[0].Computed
	n := 0
	for _, c := range v.Checks {
		if c.ID.Kind == computed.KindListenerCount || c.ID.Kind == computed.KindRouteAccepted {
			continue
		}
		if n++; c.Candidate() {
			t.Fatalf("%s %s: %s, expected %s observed %s", c.ID.Kind, c.On, c.Class, c.Expected, c.Observed)
		}
	}
	if len(v.Delegation.Rows) == 0 || n == 0 {
		t.Fatalf("%d delegation rows, %d delegation checks", len(v.Delegation.Rows), n)
	}
}

// Every tier's statuses agree with the computed views, so the budgets
// measure realistic data: every check is compatible, except that a view B
// check may be unknown when a compute cap was hit (Extreme). Under
// Contradicting every view A and parent check contradicts and every child
// entry is missing; a parent whose own entry was dropped is stale.
func TestScaleViewsConsistent(t *testing.T) {
	tiers := append(slices.Clone(scale.Tiers), scale.Large.Contradicting())
	for _, tier := range tiers {
		t.Run(tier.Name, func(t *testing.T) {
			m := New(config(scale.Snapshot(tier, 1), metricsAt(testutil.Now), func() time.Time { return testutil.Now })).Compute()
			capped := m.Installs[0].Computed.Delegation.Cap != ""
			want := func(c computed.CheckResult) bool {
				viewA := c.ID.Kind == computed.KindListenerCount || c.ID.Kind == computed.KindRouteAccepted
				switch {
				case !tier.Contradict && !viewA && capped && c.Class == computed.ClassUnknown:
					return true
				case !tier.Contradict:
					return c.Class == computed.ClassCompatible
				case c.ID.Kind == computed.KindChildEntry:
					return c.Class == computed.ClassMissing
				case c.ID.Kind == computed.KindParentResolvedRefs && !c.ParentCurrent:
					return c.Class == computed.ClassStale
				}
				return c.Class == computed.ClassContradicting
			}
			checks := m.Installs[0].Computed.Checks
			bad := 0
			for _, c := range checks {
				if !want(c) {
					if bad++; bad <= 5 {
						t.Errorf("%s %s: %s, expected %s observed %s", c.ID.Kind, c.On, c.Class, c.Expected, c.Observed)
					}
				}
			}
			if bad > 0 || len(checks) == 0 {
				t.Fatalf("%d of %d checks not as expected", bad, len(checks))
			}
		})
	}
}
