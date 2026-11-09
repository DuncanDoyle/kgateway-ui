package extension

import (
	"reflect"
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/api"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/computed/kgw23"
	"github.com/DuncanDoyle/snellius/pkg/computed/rulesets"
	"github.com/DuncanDoyle/snellius/pkg/diagnose"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kgateway"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
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

// A second pack's detectors are tried first. The old With… options and field
// assignments set the default (kgateway) pack's hooks whatever their order,
// and the second pack keeps its own (Phase 5 design §4.2, old callers).
func TestWithPackOnTopOfKgateway(t *testing.T) {
	acme := func(string) computed.Selection { return computed.Selection{Rules: kgw23.New(), Note: "acme"} }
	later := func(string) computed.Selection { return computed.Selection{Rules: kgw23.New(), Note: "later"} }
	r := New(WithPack(Pack{Name: "acme", Detectors: []install.Detector{acmeDetector{}}, Impl: Impl{RulesFor: acme}}),
		WithComputedRules(later), WithDetectors(otherDetector{}))
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if r.Detectors[0].Name() != "other" || r.Detectors[1].Name() != "acme" || r.Detectors[2].Name() != "kgateway" {
		t.Fatalf("detectors %v, want other, acme, kgateway", r.Detectors)
	}
	a := detect(t, r, testutil.ControllerDeployment("acme-system", "acme", "acme/controller:1", 1, 1))
	k := detect(t, r, kgwDeployment())
	if a.Impl != "acme" || k.Impl != "kgateway" {
		t.Fatalf("packs %q and %q; the WithDetectors detector must claim for kgateway", a.Impl, k.Impl)
	}
	if got := r.ImplFor(k).RulesFor("2.3.1"); got.Note != "later" {
		t.Fatalf("kgateway rules %+v, want the later option's", got)
	}
	if got := r.ImplFor(a).RulesFor("1.0"); got.Note != "acme" {
		t.Fatalf("acme rules %+v, want acme's own", got)
	}
	// A field assigned after New also belongs to the default pack.
	r.Scope = nil
	if r.ImplFor(k).Scope != nil {
		t.Fatal("an assigned Scope must reach the kgateway pack")
	}
	if r.ImplFor(a).Scope != nil || r.ImplFor(a).Settings != nil {
		t.Fatalf("acme must keep its own (nil) hooks, got %+v", r.ImplFor(a))
	}
	if im := r.ImplFor(install.Install{}); im.Scope != nil || im.RulesFor != nil {
		t.Fatalf("an install no pack claims gets the core hooks, got %+v", im)
	}
}

// The order of the old options does not matter: f lands on the default pack
// and the added pack is untouched.
func TestLegacyOptionsApplyToTheDefaultPack(t *testing.T) {
	f := func(string) computed.Selection { return computed.Selection{Rules: kgw23.New(), Note: "f"} }
	other := Pack{Name: "other", Detectors: []install.Detector{acmeDetector{}}, Impl: Impl{Scope: install.NamespaceSelectorScope{}}}
	for _, r := range []*Registry{New(WithPack(other), WithComputedRules(f)), New(WithComputedRules(f), WithPack(other))} {
		k := detect(t, r, kgwDeployment())
		o := detect(t, r, testutil.ControllerDeployment("acme-system", "acme", "acme/controller:1", 1, 1))
		if got := r.ImplFor(k).RulesFor("2.4.7"); got.Note != "f" {
			t.Fatalf("kgateway rules %+v, want f", got)
		}
		if im := r.ImplFor(o); o.Impl != "other" || im.RulesFor != nil || im.Scope == nil {
			t.Fatalf("other pack %q hooks %+v, want its own", o.Impl, im)
		}
	}
}

func TestWithPackRejectsBadNames(t *testing.T) {
	for _, name := range []string{"", "kgateway"} {
		if New(WithPack(Pack{Name: name})).Err() == nil {
			t.Errorf("pack name %q must be rejected", name)
		}
	}
}

// names are the checks' names, in order.
func names(cs []health.Check) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name())
	}
	return out
}

// A pack's checks run only on its installs: the kgateway pack's installs get
// health.Upstream in its order, any other install the core checks. WithChecks
// adds to the default pack: kgateway in New, the core in NewCore.
func TestChecksForPerPack(t *testing.T) {
	r := New(WithChecks("ACME-", stubCheck{}))
	if got, want := names(r.ChecksFor("kgateway")), append(names(health.Upstream()), "stub"); !slices.Equal(got, want) {
		t.Errorf("kgateway checks %v, want %v", got, want)
	}
	for _, impl := range []string{"", "unknown"} {
		if got, want := names(r.ChecksFor(impl)), names(health.Core()); !slices.Equal(got, want) {
			t.Errorf("checks for %q: %v, want the core's %v", impl, got, want)
		}
	}
	core := NewCore(WithChecks("ACME-", stubCheck{}))
	if got, want := names(core.ChecksFor("")), append(names(health.Core()), "stub"); !slices.Equal(got, want) {
		t.Errorf("NewCore checks %v, want %v", got, want)
	}
}

// Each install gets its claiming pack's UI features: kgateway's in New, a
// second pack's own, and none for the core or in NewCore (Phase 5 design §10).
func TestFeaturesFollowThePack(t *testing.T) {
	want := []string{api.FeatureDelegation, api.FeatureComputed, api.FeatureDataPlane, api.FeatureProcesses}
	r := New(WithPack(Pack{Name: "acme", Detectors: []install.Detector{acmeDetector{}}, Features: []string{api.FeatureProcesses}}))
	for _, tc := range []struct {
		r    *Registry
		impl string
		want []string
	}{
		{r, kgateway.Name, want},
		{r, "acme", []string{api.FeatureProcesses}},
		{r, "", nil},
		{NewCore(), kgateway.Name, nil},
		{NewCore(), "", nil},
	} {
		if got := tc.r.FeaturesFor(install.Install{Impl: tc.impl}); !slices.Equal(got, tc.want) {
			t.Errorf("%q: %v, want %v", tc.impl, got, tc.want)
		}
	}
}

// Packs names the registered packs; a pack's kinds apply only to its own
// installs, Gateway API kinds to every install (Phase 5 design §10).
func TestPacksAndKindApplicability(t *testing.T) {
	r := New(WithPack(Pack{Name: "acme", Detectors: []install.Detector{acmeDetector{}},
		Kinds: []kinds.Descriptor{{Group: "acme.example.com", Kind: "Pool", Resource: "pools", Versions: []string{"v1"}, Namespaced: true}}}))
	if got := r.Packs(); !slices.Equal(got, []string{kgateway.Name, "acme"}) {
		t.Errorf("packs %v", got)
	}
	if got := NewCore().Packs(); got == nil || len(got) != 0 {
		t.Errorf("core packs %#v, want an empty list", got)
	}
	tp := objects.GroupKind{Group: kinds.KgatewayGroup, Kind: "TrafficPolicy"}
	gw := objects.GroupKind{Group: "gateway.networking.k8s.io", Kind: "Gateway"}
	pool := objects.GroupKind{Group: "acme.example.com", Kind: "Pool"}
	kgw, acme, co := install.Install{Impl: kgateway.Name}, install.Install{Impl: "acme"}, install.Install{}
	for _, tc := range []struct {
		i    install.Install
		gk   objects.GroupKind
		want bool
	}{{kgw, tp, true}, {co, tp, false}, {acme, tp, false}, {co, gw, true}, {acme, pool, true}, {kgw, pool, false}, {co, pool, false}} {
		if got := r.KindAppliesTo(tc.i, tc.gk); got != tc.want {
			t.Errorf("%q %v: %v, want %v", tc.i.Impl, tc.gk, got, tc.want)
		}
	}
}
