package install

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/internal/testutil"
)

func TestClassOnlyIDEscapesToAValidID(t *testing.T) {
	for name, want := range map[string]string{
		"example.com/gateway":     "example.com~2Fgateway",
		"example.com/a/b":         "example.com~2Fa~2Fb",
		"example.com/a_b":         "example.com~2Fa~5Fb",
		"example.com/a~b":         "example.com~2Fa~7Eb",
		"example.com:8443/x":      "example.com~3A8443~2Fx",
		"Example.com/Mixed-Case.": "Example.com~2FMixed-Case.",
	} {
		got := ClassOnlyID(name)
		if got != want {
			t.Errorf("ClassOnlyID(%q) = %q, want %q", name, got, want)
		}
		if err := ValidateID(got); err != nil {
			t.Errorf("ClassOnlyID(%q): %v", name, err)
		}
	}
	if ClassOnlyID("example.com/a/b") == ClassOnlyID("example.com/a_b") {
		t.Fatal("distinct controller names must get distinct IDs")
	}
}

func classes(cs ...[2]string) []*unstructured.Unstructured {
	out := make([]*unstructured.Unstructured, 0, len(cs))
	for _, c := range cs {
		out = append(out, testutil.GatewayClass(c[0], c[1], true))
	}
	return out
}

// One class-only install per controller name no detected install lists; a
// workload install's ID, which never holds "~", is never taken.
func TestClassOnlyInstalls(t *testing.T) {
	claimed := []Install{{Namespace: "kgw", Name: "kgateway", ControllerNames: []string{testutil.Controller}, Impl: "kgateway"}}
	got := ClassOnly(classes([2]string{"kgateway", testutil.Controller}, [2]string{"waypoint", testutil.Controller},
		[2]string{"other", "example.com/gateway"}, [2]string{"other-b", "example.com/gateway"}, [2]string{"empty", ""}), claimed)
	if len(got) != 1 {
		t.Fatalf("installs %+v, want one for example.com/gateway", got)
	}
	in := got[0]
	if in.ID() != "example.com~2Fgateway" || in.Display() != "Gateway API controller example.com/gateway" || in.Impl != "" ||
		in.Deployment != nil || in.Edition != "" || in.Version != "" || len(in.ControllerNames) != 1 || in.ControllerNames[0] != "example.com/gateway" {
		t.Fatalf("class-only install %+v", in)
	}
	if in.ID() == claimed[0].ID() || len(ClassOnly(classes([2]string{"x", testutil.Controller}), claimed)) != 0 {
		t.Fatal("a claimed controller name must get no class-only twin")
	}
	// Sorted by ID, so the install list is stable.
	two := ClassOnly(classes([2]string{"b", "example.com/b"}, [2]string{"a", "example.com/a"}), nil)
	if len(two) != 2 || two[0].ID() != "example.com~2Fa" || two[1].ID() != "example.com~2Fb" {
		t.Fatalf("installs %+v, want example.com/a then example.com/b", two)
	}
}
