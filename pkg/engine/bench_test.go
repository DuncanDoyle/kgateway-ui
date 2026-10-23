package engine

import (
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
)

// scaleEngine is an upstream engine over tier's scale snapshot with fresh metrics.
func scaleEngine(tier scale.Tier) *Engine {
	return New(config(scale.Snapshot(tier, 1), metricsAt(testutil.Now), func() time.Time { return testutil.Now }))
}

// BenchmarkCompute measures one warm Compute per iteration: the engine has
// already seen the snapshot once, as after the first run in production.
func BenchmarkCompute(b *testing.B) {
	for _, tier := range scale.Tiers {
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

// The Large world yields one install that owns every HTTPRoute except the
// delegated children, which Phase 1 does not own.
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
	owned := 0
	for _, r := range m.Installs[0].Owned.Routes {
		if r.Kind.Kind == "HTTPRoute" {
			owned++
		}
	}
	if want := scale.Large.HTTPRoutes - len(children); len(children) == 0 || owned != want {
		t.Fatalf("owned HTTPRoutes %d, want %d (%d children)", owned, want, len(children))
	}
}
