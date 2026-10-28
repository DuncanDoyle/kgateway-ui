package engine

import (
	"log/slog"
	"slices"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
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

// forgetViews makes e's next Compute recompute every view, as when their
// input changed; the memo's old inputs and results stay live until then.
func (e *Engine) forgetViews() {
	for _, m := range e.views {
		m.set = false
	}
}

// BenchmarkCompute measures one warm Compute per iteration: the engine has
// already seen the snapshot once, as after the first run in production. The
// views are recomputed each time; the hit sub-benchmark reuses them.
func BenchmarkCompute(b *testing.B) {
	for _, tier := range append(slices.Clone(scale.Tiers), scale.Large.Contradicting()) {
		b.Run(tier.Name, func(b *testing.B) {
			e := scaleEngine(tier)
			e.Compute()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				e.forgetViews()
				e.Compute()
			}
		})
	}
	b.Run("large-hit", func(b *testing.B) {
		e := scaleEngine(scale.Large)
		e.Compute()
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			e.Compute()
		}
	})
}

// denseSnapshot is a synced snapshot of scale.DenseSelection(parents, routes)
// with one kgateway v2.4.7 controller, as scale.Snapshot builds a tier's.
func denseSnapshot(parents, routes int) *cache.Snapshot {
	byKind := map[schema.GroupKind]schema.GroupVersionResource{}
	access := map[schema.GroupVersionResource]cache.Access{}
	for _, s := range testutil.ServedUpstream() {
		byKind[schema.GroupKind{Group: s.Group, Kind: s.Kind}] = s.GVR()
		access[s.GVR()] = cache.Access{OK: true}
	}
	objs := map[schema.GroupVersionResource][]*unstructured.Unstructured{}
	for _, o := range scale.DenseSelection(parents, routes) {
		gvr := byKind[o.GroupVersionKind().GroupKind()]
		objs[gvr] = append(objs[gvr], o)
	}
	for _, g := range []schema.GroupVersionResource{cache.NamespacesGVR, cache.PodsGVR, cache.DeploymentsGVR} {
		access[g] = cache.Access{OK: true}
	}
	s := &cache.Snapshot{Objects: objs, Access: access, Synced: true, TakenAt: testutil.Now}
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	s.Namespaces = append(testutil.ScaleNamespaces(2), testutil.Namespace("kgw", nil))
	s.Deployments = append(s.Deployments, d, testutil.ProxyDeployment(scale.DenseParents, "dense", 1, 1))
	s.Pods = append(s.Pods, testutil.ControllerPod(d, "kgateway-0", true))
	return s
}

// BenchmarkDenseSelection measures one warm Compute over the dense-selection
// shape of design §9.6 and reports its rows and whether a cap was hit. Two
// fresh engines must agree on the cap and the last row's key and outcome: the
// cut is deterministic.
func BenchmarkDenseSelection(b *testing.B) {
	for _, c := range []struct {
		name            string
		parents, routes int
	}{{"large", 20, 500}, {"extreme", 100, 1000}} {
		b.Run(c.name, func(b *testing.B) {
			snap := denseSnapshot(c.parents, c.routes)
			deleg := func(e *Engine) computed.DelegationResult {
				m := e.Compute()
				if len(m.Installs) != 1 || m.Installs[0].Computed == nil {
					b.Fatalf("installs %d", len(m.Installs))
				}
				return m.Installs[0].Computed.Delegation
			}
			newEngine := func() *Engine {
				cfg := config(snap, metricsAt(testutil.Now), func() time.Time { return testutil.Now })
				cfg.Logger = slog.New(slog.DiscardHandler) // the cap warning would split a result line
				return New(cfg)
			}
			e := newEngine()
			first, second := deleg(e), deleg(newEngine())
			if len(first.Rows) == 0 || first.Cap != second.Cap || len(first.Rows) != len(second.Rows) ||
				first.Rows[len(first.Rows)-1].Key != second.Rows[len(second.Rows)-1].Key ||
				first.Rows[len(first.Rows)-1].Outcome != second.Rows[len(second.Rows)-1].Outcome {
				b.Fatalf("two runs differ: cap %q/%q, %d/%d rows", first.Cap, second.Cap, len(first.Rows), len(second.Rows))
			}
			last := first.Rows[len(first.Rows)-1]
			b.Logf("%s: %d parents × %d routes: cap %q, %d rows, last row %s rule %d ref %d → %s (%s)",
				c.name, c.parents, c.routes, first.Cap, len(first.Rows), last.Key.Parent, last.Key.Rule, last.Key.Ref, last.Key.Child, last.Outcome)
			capped := 0.0
			if first.Cap != "" {
				capped = 1
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				e.forgetViews()
				e.Compute()
			}
			b.ReportMetric(float64(len(first.Rows)), "rows")
			b.ReportMetric(capped, "capped")
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
