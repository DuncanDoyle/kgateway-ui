package kgw25

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// 2.5 reads a missing targetRef's KGWUI-016 from status (D-020 rule 5); its
// PolicyTargeter judges selectors only (D-058 item 7).
func TestRuleSetReadsTargetsFromStatus(t *testing.T) {
	r := New()
	if r.Version() != "kgateway-2.5" {
		t.Fatalf("version %q", r.Version())
	}
	pt := (computed.Selection{Rules: r}).PolicyTargeter()
	if pt == nil {
		t.Fatal("kgw25 has no PolicyTargeter")
	}
	rs := pt.PolicyTargets(computed.TargetInput{TargetRefs: []computed.TargetRef{{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: "nope"}},
		Selectors:  []computed.TargetSelector{{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute"}},
		ScopeKnown: true, Served: func(objects.GroupKind) bool { return true }, Lookup: func(objects.GroupKind, string, string) (*unstructured.Unstructured, bool) { return nil, false },
		Labelled: func(objects.GroupKind, string, map[string]string) bool { return false }})
	if len(rs) != 2 || rs[0].Outcome != computed.TargetUnknown || rs[1].Index != 1 || rs[1].Outcome != computed.TargetMatchesNothing {
		t.Fatalf("results %+v: want the ref unknown and the empty selector matching nothing", rs)
	}
	if s := (computed.Selection{Rules: r}).TargetStatusReporter(); s == nil || !s.ReportsTargetNotFound() {
		t.Fatal("kgw25 does not report TargetNotFound from status")
	}
}
