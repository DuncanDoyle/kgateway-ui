package health

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
)

// freshness003 returns the evidence of the one KGWUI-003 on res.
func freshness003(t *testing.T, r Report, res string) (Finding, *FreshnessEvidence) {
	t.Helper()
	var got []Finding
	for _, f := range r.Findings {
		if f.Code == CodeRouteUnattached && f.Resource != nil && f.Resource.String() == res {
			got = append(got, f)
		}
	}
	if len(got) != 1 {
		t.Fatalf("want one KGWUI-003 on %s, got %+v", res, r.Findings)
	}
	ev, _ := got[0].Facts.(*FreshnessEvidence)
	if ev == nil {
		t.Fatalf("no freshness evidence: %+v", got[0])
	}
	return got[0], ev
}

// Blocks of one object are judged in turn; a later block whose kind comes
// first in §10.1 order wins, with its detail.
func TestFreshnessFirstKindWinsAcrossBlocks(t *testing.T) {
	w := healthyWorld()
	gw := testutil.Gateway("infra", "edge", "kgateway", nil)
	gw.SetGeneration(2) // stale top-level conditions
	testutil.AddSpecListener(gw, "https")
	w.objs[1] = gw
	f, ev := freshness003(t, w.report(t), "infra/edge")
	if ev.Freshness != FreshnessNeverWritten || ev.Observed != nil || f.Detail != "no status for listener https" {
		t.Fatalf("want never-written for the listener, got %q %+v", f.Detail, ev)
	}
}

// Only a block that is all CRD default, or a Gateway with no conditions, is
// never written; a partly written block is judged by its own conditions.
func TestBlockFreshnessCRDDefault(t *testing.T) {
	dflt := metav1.Condition{Type: "Accepted", Status: metav1.ConditionUnknown, Reason: "Pending", Message: "Waiting for controller", LastTransitionTime: metav1.Unix(0, 0)}
	programmed := testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")
	notEpoch := dflt
	notEpoch.LastTransitionTime = testutil.Old
	u := testutil.Gateway("infra", "edge", "kgateway", nil)
	cases := []struct {
		name string
		cs   []metav1.Condition
		kind string
	}{
		{"all default", []metav1.Condition{dflt}, FreshnessNeverWritten},
		// The default's observedGeneration 0 is stale.
		{"default beside a written condition", []metav1.Condition{dflt, programmed}, FreshnessStale},
		{"Pending, not at the epoch", []metav1.Condition{func() metav1.Condition { c := notEpoch; c.ObservedGeneration = 1; return c }(), programmed}, FreshnessPending},
	}
	for _, tc := range cases {
		if _, kind, _ := blockFreshness(u, tc.cs, gatewayRequired, nil); kind != tc.kind {
			t.Errorf("%s: kind %q, want %q", tc.name, kind, tc.kind)
		}
	}
	// A Gateway with no top-level conditions is never written.
	w := healthyWorld()
	w.objs[1] = testutil.Gateway("infra", "edge", "kgateway", []metav1.Condition{})
	if f, ev := freshness003(t, w.report(t), "infra/edge"); ev.Freshness != FreshnessNeverWritten || f.Detail != noControllerStatus {
		t.Fatalf("want never-written, got %q %+v", f.Detail, ev)
	}
}
