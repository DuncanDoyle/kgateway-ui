package extension

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/kgw23"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/rulesets"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
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

// A downstream rule selection replaces rulesets.Select, which is the default (D-020).
func TestWithComputedRulesReachesTheEngine(t *testing.T) {
	if got := New().RulesFor("2.3.1"); got.Rules.Version() != rulesets.Select("2.3.1").Rules.Version() {
		t.Fatalf("default must be rulesets.Select, got %s", got.Rules.Version())
	}
	down := func(string) computed.Selection { return computed.Selection{Rules: kgw23.New(), Note: "downstream"} }
	if got := New(WithComputedRules(down)).RulesFor("acme-1.0"); got.Note != "downstream" || got.Rules.Version() != "kgateway-2.3" {
		t.Fatalf("WithComputedRules must set Registry.RulesFor, got %+v", got)
	}
}

type stubCheck struct{}

func (stubCheck) Name() string { return "stub" }
func (stubCheck) Run(health.Input) ([]health.Finding, health.CheckResult) {
	return nil, health.CheckResult{Name: "stub", OK: true}
}

func TestWithChecksRejectsReservedPrefix(t *testing.T) {
	err := New(WithChecks("KGWUI-", stubCheck{})).Err()
	if err == nil || !strings.Contains(err.Error(), "KGWUI-") {
		t.Fatalf("the upstream prefix must be rejected by name, got %v", err)
	}
}

func TestWithChecksRejectsDuplicatePrefix(t *testing.T) {
	if err := New(WithChecks("ACME-", stubCheck{})).Err(); err != nil {
		t.Fatalf("a first registration must succeed, got %v", err)
	}
	r := New(WithChecks("ACME-", stubCheck{}), WithChecks("ACME-", stubCheck{}))
	if err := r.Err(); err == nil || !strings.Contains(err.Error(), "ACME-") {
		t.Fatalf("a repeated prefix must be rejected, got %v", err)
	}
}

func TestWithChecksRejectsMalformedPrefix(t *testing.T) {
	for _, p := range []string{"acme", "ACME", "A-", "acme-", "ACME_", "ABCDEFGHIJKLMNOPQ-", ""} {
		r := New(WithChecks(p, stubCheck{}))
		if r.Err() == nil {
			t.Errorf("prefix %q must be rejected", p)
		}
		if len(r.Checks) != len(health.Upstream()) {
			t.Errorf("prefix %q: a rejected registration must add no checks", p)
		}
	}
	for _, p := range []string{"AB-", "ACME2-", "ABCDEFGHIJKLMNO-"} {
		if err := New(WithChecks(p, stubCheck{})).Err(); err != nil {
			t.Errorf("prefix %q must be accepted, got %v", p, err)
		}
	}
}

func TestErrJoinsEveryRegistrationError(t *testing.T) {
	err := New(WithChecks("acme", stubCheck{}), WithChecks("KGWUI-", stubCheck{})).Err()
	if err == nil || !strings.Contains(err.Error(), "acme") || !strings.Contains(err.Error(), "KGWUI-") {
		t.Fatalf("every error must be reported, got %v", err)
	}
}
