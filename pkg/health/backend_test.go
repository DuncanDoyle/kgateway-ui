package health

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// backendWorld routes shop/cart to the Backend so it is in use.
func backendWorld(b *unstructured.Unstructured) *world {
	w := healthyWorld()
	gw := testutil.GwRef("infra", "edge")
	rt := testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller))
	rt.Object["spec"].(map[string]any)["rules"] = []any{map[string]any{"backendRefs": []any{
		map[string]any{"group": "gateway.kgateway.dev", "kind": "Backend", "name": b.GetName()}}}}
	w.objs[2] = rt
	w.objs = append(w.objs, b)
	return w
}

func TestBackendAcceptedRules(t *testing.T) {
	acc := func(st metav1.ConditionStatus, og int64) []metav1.Condition {
		c := testutil.Cond("Accepted", st, "Accepted")
		c.ObservedGeneration = og
		return []metav1.Condition{c}
	}
	tests := []struct {
		name  string
		gen   int64
		conds []metav1.Condition
		sev   Severity // "" = no finding
		state State
	}{
		{"current false", 2, acc(metav1.ConditionFalse, 2), SeverityError, StateError},
		{"newer than generation, false", 2, acc(metav1.ConditionFalse, 3), SeverityError, StateError},
		{"current true", 2, acc(metav1.ConditionTrue, 2), "", StateHealthy},
		{"stale false", 3, acc(metav1.ConditionFalse, 2), "", StateUnknown},
		{"stale true", 3, acc(metav1.ConditionTrue, 2), "", StateUnknown},
		{"no evidence false", 2, acc(metav1.ConditionFalse, 0), SeverityWarning, StateUnknown},
		{"no evidence true", 2, acc(metav1.ConditionTrue, 0), "", StateUnknown},
		{"absent", 2, []metav1.Condition{testutil.Cond("Other", metav1.ConditionTrue, "X")}, "", StateUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := testutil.Backend("shop", "b", tc.conds)
			b.SetGeneration(tc.gen)
			st, cond := BackendState(b)
			if st != tc.state {
				t.Fatalf("BackendState = %s, want %s", st, tc.state)
			}
			if wantCond := tc.conds[0].Type == "Accepted"; (cond != nil) != wantCond {
				t.Fatalf("condition = %v, want present=%v", cond, wantCond)
			}
			r := backendWorld(b).report(t)
			var got []Finding
			for _, f := range r.Findings {
				if f.Resource != nil && f.Resource.Name == "b" {
					got = append(got, f)
				}
			}
			switch {
			case tc.sev == "" && len(got) != 0:
				t.Fatalf("unexpected findings %+v", got)
			case tc.sev != "" && (len(got) != 1 || got[0].Code != CodeConditionFalse || got[0].Severity != tc.sev):
				t.Fatalf("findings = %+v, want one KGWUI-002 %s", got, tc.sev)
			}
			for _, c := range r.Checks {
				if c.Required && !c.OK {
					t.Fatalf("required check %s failed: %s", c.Name, c.Error)
				}
			}
			if has(r, CodeRouteUnattached, "", "") {
				t.Fatalf("not-yet-processed finding: %+v", r.Findings)
			}
			if r.State == StateUnknown {
				t.Fatalf("install turned unknown")
			}
		})
	}
}
