package extension

import (
	"reflect"
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/computed/kgw23"
	"github.com/DuncanDoyle/snellius/pkg/computed/rulesets"
	"github.com/DuncanDoyle/snellius/pkg/diagnose"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/process"
	"github.com/DuncanDoyle/snellius/pkg/proxymode"
)

func checkNames(cs []health.Check) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name())
	}
	return out
}

// detect runs r's detectors over d, as install.Discover does.
func detect(t *testing.T, r *Registry, d *appsv1.Deployment) install.Install {
	t.Helper()
	for _, det := range r.Detectors {
		if in, ok := det.Detect(d); ok {
			return in
		}
	}
	t.Fatalf("no detector claims %s/%s", d.Namespace, d.Name)
	return install.Install{}
}

func kgwDeployment() *appsv1.Deployment {
	return testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1,
		testutil.Env(install.ExperimentalEnv, "false"))
}

// New is the core plus the kgateway pack, with the base's kinds, detectors,
// checks (in order) and hooks.
func TestNewIsTheBaseRegistry(t *testing.T) {
	r := New()
	if !reflect.DeepEqual(r.Kinds, kinds.Upstream()) {
		t.Fatalf("kinds differ from the base:\n%+v", r.Kinds)
	}
	if got, want := checkNames(r.Checks), checkNames(health.Upstream()); !slices.Equal(got, want) {
		t.Fatalf("checks %v, want the base order %v", got, want)
	}
	if len(r.Detectors) != 1 || r.Detectors[0].Name() != "kgateway" {
		t.Fatalf("detectors %v, want the kgateway detector only", r.Detectors)
	}
	if _, ok := r.Detectors[0].(install.ProxyPodDetector); !ok {
		t.Fatal("the kgateway detector must keep selecting proxy Pods")
	}
	if _, ok := r.Scope.(install.NamespaceSelectorScope); !ok {
		t.Fatalf("scope %T", r.Scope)
	}
	if _, ok := r.ProxyMode.(proxymode.GatewayParameters); !ok {
		t.Fatalf("proxy mode %T", r.ProxyMode)
	}
	if _, ok := r.Process.(process.Kube); !ok {
		t.Fatalf("process %T", r.Process)
	}
	if _, ok := r.Commands.(diagnose.Kubectl); !ok {
		t.Fatalf("commands %T", r.Commands)
	}
	inst := detect(t, r, kgwDeployment())
	if inst.Impl != "kgateway" {
		t.Fatalf("install pack %q, want kgateway", inst.Impl)
	}
	im := r.ImplFor(inst)
	if _, ok := im.Scope.(install.NamespaceSelectorScope); !ok {
		t.Fatalf("pack scope %T", im.Scope)
	}
	if im.Version == nil || im.Version(inst) != inst.KgatewayVersion() {
		t.Fatal("the kgateway pack's version must be KgatewayVersion")
	}
	if im.RulesFor == nil || im.RulesFor("2.3.1").Rules.Version() != rulesets.Select("2.3.1").Rules.Version() {
		t.Fatal("the kgateway pack's rule sets must be rulesets.Select")
	}
	if im.Settings == nil || !reflect.DeepEqual(im.Settings(inst), install.ReadSettings(inst)) {
		t.Fatal("the kgateway pack's settings must be ReadSettings")
	}
	tcp := kinds.Served{Descriptor: kinds.Descriptor{Group: "gateway.networking.k8s.io", Kind: "TCPRoute"}, Version: "v1alpha2"}
	if im.ExperimentalOnly == nil || !im.ExperimentalOnly(tcp, inst) {
		t.Fatal("the kgateway pack must classify a pre-v1 TCPRoute as experimental-only")
	}
}

// NewCore is Gateway API alone: no kgateway kind, no detector, the core checks,
// and no pack hooks for any install.
func TestNewCoreIsGatewayAPIOnly(t *testing.T) {
	r := NewCore()
	if len(r.Kinds) == 0 || slices.ContainsFunc(r.Kinds, func(d kinds.Descriptor) bool { return d.Group == kinds.KgatewayGroup }) {
		t.Fatalf("kinds %+v, want Gateway API kinds only", r.Kinds)
	}
	if !reflect.DeepEqual(r.Kinds, kinds.GatewayAPI()) {
		t.Fatalf("kinds differ from kinds.GatewayAPI: %+v", r.Kinds)
	}
	if len(r.Detectors) != 0 {
		t.Fatalf("detectors %v, want none", r.Detectors)
	}
	if got, want := checkNames(r.Checks), checkNames(health.Core()); !slices.Equal(got, want) {
		t.Fatalf("checks %v, want %v", got, want)
	}
	im := r.ImplFor(install.Install{})
	if im.Scope != nil || im.Settings != nil || im.RulesFor != nil || im.Version != nil || im.ExperimentalOnly != nil || im.Verifier != nil {
		t.Fatalf("the core has no pack hooks, got %+v", im)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
}

// WithComputedRules on New sets the rules of the kgateway pack, the pack New builds.
func TestWithComputedRulesSetsTheKgatewayPack(t *testing.T) {
	down := func(string) computed.Selection { return computed.Selection{Rules: kgw23.New(), Note: "downstream"} }
	r := New(WithComputedRules(down))
	if got := r.ImplFor(detect(t, r, kgwDeployment())).RulesFor("acme-1.0"); got.Note != "downstream" {
		t.Fatalf("kgateway pack rules %+v, want the downstream selection", got)
	}
	// A hook assigned on the Registry after New belongs to the same pack.
	r = New()
	r.RulesFor = down
	if got := r.ImplFor(detect(t, r, kgwDeployment())).RulesFor("2.4.7"); got.Note != "downstream" {
		t.Fatalf("an assigned RulesFor must reach the kgateway pack, got %+v", got)
	}
}

// An install found by a WithDetectors detector belongs to the kgateway pack in New.
func TestWithDetectorsClaimsForTheKgatewayPack(t *testing.T) {
	r := New(WithDetectors(otherDetector{}))
	inst := detect(t, r, kgwDeployment())
	if inst.Impl != "kgateway" {
		t.Fatalf("install pack %q, want kgateway", inst.Impl)
	}
	if _, ok := r.ImplFor(inst).Scope.(install.NamespaceSelectorScope); !ok {
		t.Fatal("the downstream install must get the kgateway pack's scope")
	}
	// In NewCore the detector claims for the core.
	r = NewCore(WithDetectors(otherDetector{}))
	if inst := detect(t, r, kgwDeployment()); inst.Impl != "" || r.ImplFor(inst).Scope != nil {
		t.Fatalf("NewCore: install pack %q, want the core", inst.Impl)
	}
}

// acmeDetector claims Deployments named acme.
type acmeDetector struct{ install.UpstreamDetector }

func (acmeDetector) Name() string { return "acme" }

func (acmeDetector) Detect(d *appsv1.Deployment) (install.Install, bool) {
	if d.Name != "acme" {
		return install.Install{}, false
	}
	return install.Install{Namespace: d.Namespace, Name: d.Name, Edition: "acme"}, true
}

// A second pack's detectors are tried first; the old With… options then set
// that pack's hooks, and the kgateway pack keeps its own.
func TestWithPackOnTopOfKgateway(t *testing.T) {
	acme := func(string) computed.Selection { return computed.Selection{Rules: kgw23.New(), Note: "acme"} }
	later := func(string) computed.Selection { return computed.Selection{Rules: kgw23.New(), Note: "later"} }
	r := New(WithPack(Pack{Name: "acme", Detectors: []install.Detector{acmeDetector{}}, Impl: Impl{RulesFor: acme}}), WithComputedRules(later))
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if r.Detectors[0].Name() != "acme" || r.Detectors[1].Name() != "kgateway" {
		t.Fatalf("detectors %v, want acme before kgateway", r.Detectors)
	}
	a := detect(t, r, testutil.ControllerDeployment("acme-system", "acme", "acme/controller:1", 1, 1))
	k := detect(t, r, kgwDeployment())
	if a.Impl != "acme" || k.Impl != "kgateway" {
		t.Fatalf("packs %q and %q", a.Impl, k.Impl)
	}
	if got := r.ImplFor(a).RulesFor("1.0"); got.Note != "later" {
		t.Fatalf("acme rules %+v, want the later option's", got)
	}
	if got := r.ImplFor(k).RulesFor("2.3.1"); got.Rules.Version() != rulesets.Select("2.3.1").Rules.Version() || got.Note != "" {
		t.Fatalf("kgateway rules %+v, want rulesets.Select", got)
	}
	if im := r.ImplFor(install.Install{}); im.Scope != nil || im.RulesFor != nil {
		t.Fatalf("an install no pack claims gets the core hooks, got %+v", im)
	}
}

func TestWithPackRejectsBadNames(t *testing.T) {
	for _, name := range []string{"", "kgateway"} {
		if New(WithPack(Pack{Name: name})).Err() == nil {
			t.Errorf("pack name %q must be rejected", name)
		}
	}
}
