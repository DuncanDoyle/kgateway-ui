package kgwbackend

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/computed"
)

// Judge reads only the Accepted condition's evidence (Phase 3 design §6.2).
func TestJudge(t *testing.T) {
	acc := func(st metav1.ConditionStatus, og int64) []metav1.Condition {
		c := testutil.Cond("Accepted", st, "Accepted")
		c.ObservedGeneration = og
		return []metav1.Condition{c}
	}
	for _, tc := range []struct {
		name  string
		gen   int64
		conds []metav1.Condition
		want  computed.BackendVerdict
	}{
		{"current false", 2, acc(metav1.ConditionFalse, 2), computed.BackendRejected},
		{"newer than generation, false", 2, acc(metav1.ConditionFalse, 3), computed.BackendRejected},
		{"current true", 2, acc(metav1.ConditionTrue, 2), computed.BackendAccepted},
		{"current unknown", 2, acc(metav1.ConditionUnknown, 2), computed.BackendUnknown},
		{"stale false", 3, acc(metav1.ConditionFalse, 2), computed.BackendUnknown},
		{"no evidence false", 2, acc(metav1.ConditionFalse, 0), computed.BackendUnknown},
		{"absent", 2, []metav1.Condition{testutil.Cond("Other", metav1.ConditionTrue, "X")}, computed.BackendUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := testutil.Backend("shop", "b", tc.conds)
			b.SetGeneration(tc.gen)
			v, cond := Judge(b)
			if v != tc.want {
				t.Fatalf("verdict %q, want %q", v, tc.want)
			}
			if (cond != nil) != (tc.conds[0].Type == "Accepted") {
				t.Fatalf("condition %v", cond)
			}
		})
	}
}
