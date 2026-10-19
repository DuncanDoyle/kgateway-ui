package extension

import (
	"net/http"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

type otherDetector struct{ install.UpstreamDetector }

func (otherDetector) Name() string { return "other" }

func TestNewAppliesOptionsOnTopOfUpstream(t *testing.T) {
	r := New(WithKinds(kinds.Descriptor{Kind: "Extra"}), WithDetectors(otherDetector{}))
	if r.Kinds[len(r.Kinds)-1].Kind != "Extra" || len(r.Kinds) != len(kinds.Upstream())+1 {
		t.Fatal("extra kind must be appended to the upstream kinds")
	}
	if r.Detectors[0].Name() != "other" || r.Detectors[1].Name() != "kgateway" {
		t.Fatal("downstream detectors must be tried before the upstream detector")
	}
	if r.Scope == nil || len(r.Checks) == 0 {
		t.Fatal("upstream defaults missing")
	}
	// The upstream verifier depends on the metrics interval, so the app builds it.
	if r.Verifier != nil {
		t.Fatalf("the default verifier must be left to the app, got %T", r.Verifier)
	}
	if _, ok := r.Authorizer.(api.AllowAll); !ok {
		t.Fatalf("the default authorizer must be AllowAll (D-013), got %T", r.Authorizer)
	}
	deny := api.AuthorizerFunc(func(*http.Request, objects.Ref) bool { return false })
	if _, ok := New(WithAuthorizer(deny)).Authorizer.(api.AuthorizerFunc); !ok {
		t.Fatal("WithAuthorizer must replace the default")
	}
	v := scopeverify.NewManagedCountVerifier(1)
	if New(WithScopeVerifier(v)).Verifier != v {
		t.Fatal("WithScopeVerifier must set the verifier")
	}
}

// Kinds are unique by group and kind; a later registration replaces the
// earlier one in its position.
func TestWithKindsDedupesByGroupKind(t *testing.T) {
	up := kinds.Upstream()
	hr := up[4]
	hr.Versions = []string{"v2", "v1"}
	extra := kinds.Descriptor{Group: "acme.example.com", Kind: "Extra"}
	r := New(WithKinds(extra, hr, extra))
	if len(r.Kinds) != len(up)+1 || r.Kinds[4].Kind != "HTTPRoute" || r.Kinds[4].Versions[0] != "v2" || r.Kinds[len(r.Kinds)-1].Kind != "Extra" {
		t.Fatalf("want upstream kinds with HTTPRoute replaced in place, plus one Extra: %+v", r.Kinds)
	}
}
