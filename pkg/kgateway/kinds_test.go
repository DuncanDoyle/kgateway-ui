package kgateway

import (
	"testing"

	"github.com/DuncanDoyle/snellius/pkg/kinds"
)

// Kinds are kgateway's own CRDs; with kinds.GatewayAPI they make up kinds.Upstream.
func TestKindsAreKgatewaysOwn(t *testing.T) {
	got := Kinds()
	if len(got) != 8 {
		t.Fatalf("%d kinds, want 8", len(got))
	}
	for _, d := range got {
		if d.Group != kinds.KgatewayGroup {
			t.Errorf("%s/%s is not a kgateway kind", d.Group, d.Kind)
		}
	}
	if len(got)+len(kinds.GatewayAPI()) != len(kinds.Upstream()) {
		t.Fatal("GatewayAPI and kgateway kinds must make up Upstream")
	}
}
