package install

import (
	"strings"
	"testing"
)

func TestValidateID(t *testing.T) {
	for _, id := range []string{"kgw.kgateway", "acme~edge_1", "A-Z.0-9"} {
		if err := ValidateID(id); err != nil {
			t.Errorf("%q: %v", id, err)
		}
	}
	for _, id := range []string{"", ".", "..", "kgw/kgateway", "50%", "a b", "é"} {
		err := ValidateID(id)
		if err == nil {
			t.Errorf("%q must be rejected", id)
			continue
		}
		if id != "" && !strings.Contains(err.Error(), id) {
			t.Errorf("the error must quote the ID: %v", err)
		}
	}
}

func TestParsePinned(t *testing.T) {
	for in, want := range map[string]string{"": "", "kgw/kgateway": "kgw.kgateway", "kgw.kgateway": "kgw.kgateway", "acme~edge_1": "acme~edge_1"} {
		if got, err := ParsePinned(in); err != nil || got != want {
			t.Errorf("ParsePinned(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"kgw/", "/kgateway", "a/b/c", "..", "a%b"} {
		if _, err := ParsePinned(bad); err == nil {
			t.Errorf("ParsePinned(%q) must fail", bad)
		}
	}
}

func TestInstallIDAndDisplay(t *testing.T) {
	in := Install{Namespace: "kgw", Name: "kgateway"}
	if in.ID() != "kgw.kgateway" || in.Display() != "kgw/kgateway" {
		t.Fatalf("Kubernetes install: ID %q, display %q", in.ID(), in.Display())
	}
	in.CustomID, in.DisplayName = "x~1", "Edge"
	if in.ID() != "x~1" || in.Display() != "Edge" {
		t.Fatalf("custom install: ID %q, display %q", in.ID(), in.Display())
	}
}
