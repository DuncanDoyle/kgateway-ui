package kgw25

import (
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
)

// 2.5 reads KGWUI-016 from status, so the set has no PolicyTargeter (D-020 rule 5).
func TestRuleSetReadsTargetsFromStatus(t *testing.T) {
	r := New()
	if r.Version() != "kgateway-2.5" {
		t.Fatalf("version %q", r.Version())
	}
	if (computed.Selection{Rules: r}).PolicyTargeter() != nil {
		t.Fatal("kgw25 has a PolicyTargeter")
	}
	if s, ok := r.(interface{ ReportsTargetNotFound() bool }); !ok || !s.ReportsTargetNotFound() {
		t.Fatal("kgw25 does not report TargetNotFound from status")
	}
}
