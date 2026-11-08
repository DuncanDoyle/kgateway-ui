package setup_test

import (
	"context"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/computed/kgw23"
	"github.com/DuncanDoyle/snellius/pkg/extension"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/process"
	"github.com/DuncanDoyle/snellius/pkg/scopeverify"
	"github.com/DuncanDoyle/snellius/pkg/setup"
)

// renamed is the upstream detector under another name, as a downstream adds it.
type renamed struct{ install.UpstreamDetector }

func (renamed) Name() string { return "renamed" }

// An install a WithDetectors detector finds in New belongs to the kgateway
// pack: the model equals the base's (Phase 5 design §4.2).
func TestWithDetectorsInstallMatchesTheBase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base := newOneShotWith(t, ctx, world(), extension.New()).at(t0)
	down := newOneShotWith(t, ctx, world(), extension.New(extension.WithDetectors(renamed{}))).at(t0)
	same(t, "downstream detector", down, base)
	b, d := base.Installs[0], down.Installs[0]
	if d.Resolved.Impl != "kgateway" || d.Rules.Rules.Version() != b.Rules.Rules.Version() || d.Rules.Note != b.Rules.Note ||
		d.Resolved.Scope.Mode != b.Resolved.Scope.Mode || d.Resolved.Settings != b.Resolved.Settings {
		t.Fatalf("downstream install %+v rules %s, want the base's %+v rules %s", d.Resolved, d.Rules.Rules.Version(), b.Resolved, b.Rules.Rules.Version())
	}
}

// acmeDetector claims Deployments labelled app=acme.
type acmeDetector struct{}

func (acmeDetector) Name() string { return "acme" }

func (acmeDetector) ControllerSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"app": "acme"})
}

func (acmeDetector) ControllerPodSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"app": "acme"})
}

func (acmeDetector) ProxySelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"app": "acme-proxy"})
}

func (acmeDetector) Detect(d *appsv1.Deployment) (install.Install, bool) {
	if d.Labels["app"] != "acme" {
		return install.Install{}, false
	}
	return install.Install{Namespace: d.Namespace, Name: d.Name, Edition: "acme", Version: "9.9.0", Container: "acme",
		ControllerNames: []string{"acme.example.com/gateway"}}, true
}

// teamA is a scope provider that always watches team=a.
type teamA struct{}

func (teamA) Name() string { return "team-a" }

func (teamA) Scope(install.Install) install.Scope {
	return install.Scope{Mode: install.ScopeSelectors, Selectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"team": "a"}}}}
}

// Two packs each claim their own controller through setup.Build; each install
// resolves scope, settings, version and rule set with its own pack's hooks.
func TestPackHooksArePerInstall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	replicas := int32(1)
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "acme-system", Name: "acme", UID: "deploy/acme", Labels: map[string]string{"app": "acme"}},
		Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "acme"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "acme"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "acme", Image: "example.com/acme:9.9.0"}}}}},
		Status: appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1}}
	off := install.Setting{Value: "false", Known: true, Source: "acme"}
	acme := extension.Impl{Scope: teamA{},
		Settings: func(install.Install) install.Settings {
			return install.Settings{Experimental: off, WildcardNamespace: off, GlobalPolicyNamespace: off}
		},
		Version: func(i install.Install) string { return "acme-" + i.Version },
		RulesFor: func(v string) computed.Selection {
			return computed.Selection{Rules: kgw23.New(), Note: "rules for " + v}
		}}
	reg := extension.New(extension.WithPack(extension.Pack{Name: "acme", Detectors: []install.Detector{acmeDetector{}}, Impl: acme}))
	m := newOneShotWith(t, ctx, append(world(), dep), reg).at(t0)
	if len(m.Installs) != 2 {
		t.Fatalf("%d installs, want acme and kgateway", len(m.Installs))
	}
	a, k := m.Installs[0], m.Installs[1] // sorted by ID: acme-system.acme < kgw.kgateway
	if a.Resolved.Impl != "acme" || a.Resolved.Scope.Mode != install.ScopeSelectors || a.Resolved.Settings.Experimental != off ||
		a.Rules.Note != "rules for acme-9.9.0" || a.Rules.Rules.Version() != "kgateway-2.3" {
		t.Fatalf("acme install %+v, rules %s %q", a.Resolved, a.Rules.Rules.Version(), a.Rules.Note)
	}
	if k.Resolved.Impl != "kgateway" || k.Resolved.Scope.Mode != install.ScopeAll || k.Resolved.Scope.Source != "pod kgateway-0" ||
		k.Resolved.Settings.GlobalPolicyNamespace.Value != "global" || k.Rules.Note != "" || k.Rules.Rules.Version() != "kgateway-2.4" {
		t.Fatalf("kgateway install %+v, rules %s %q", k.Resolved, k.Rules.Rules.Version(), k.Rules.Note)
	}
}

// Class-only installs are off unless asked for.
func TestBuildClassOnlyInstalls(t *testing.T) {
	for _, on := range []bool{false, true} {
		dyn, kube := fakeClients(t, nil)
		cfg, err := setup.Build(setup.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery()},
			setup.Options{MetricsInterval: 10 * time.Second, ClassOnlyInstalls: on}, extension.New())
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Engine.ClassOnly != on || cfg.Engine.HooksFor == nil {
			t.Errorf("ClassOnlyInstalls=%v: engine ClassOnly=%v, hooks set %v", on, cfg.Engine.ClassOnly, cfg.Engine.HooksFor != nil)
		}
	}
}

// acmeDeployment is the controller acmeDetector claims.
func acmeDeployment() *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "acme-system", Name: "acme", UID: "deploy/acme", Labels: map[string]string{"app": "acme"}},
		Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "acme"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "acme"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "acme", Image: "example.com/acme:9.9.0"}}}}},
		Status: appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1}}
}

// hookCalls records which pack's hook saw which install, as "pack hook install".
type hookCalls struct {
	mu  sync.Mutex
	got map[string]bool
}

func (c *hookCalls) add(pack, hook, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got[pack+" "+hook+" "+id] = true
}

type recVerifier struct {
	pack string
	c    *hookCalls
}

func (v recVerifier) Name() string { return v.pack }

func (v recVerifier) Verify(in scopeverify.Input) scopeverify.Result {
	v.c.add(v.pack, "verifier", in.Install.ID())
	return scopeverify.Result{Available: true}
}

// recProcess reports desired replicas so a model shows which source it read.
type recProcess struct {
	pack    string
	desired int32
	c       *hookCalls
}

func (p recProcess) Controller(in install.Resolved, _ *cache.Snapshot) process.Set {
	p.c.add(p.pack, "process", in.ID())
	return process.Set{Desired: &p.desired}
}

func (p recProcess) Proxies(in install.Resolved, _ objects.Ref, _ *cache.Snapshot) process.Set {
	p.c.add(p.pack, "process", in.ID())
	return process.Set{}
}

// Through setup.Build, each install reads its own pack's scope verifier and
// process source; a class-only install reads neither. Without WithScopeVerifier
// the default verifier still checks the kgateway install (Phase 5 design §4.2).
func TestPackRuntimeHooksArePerInstall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &hookCalls{got: map[string]bool{}}
	acme := extension.Pack{Name: "acme", Detectors: []install.Detector{acmeDetector{}},
		Impl: extension.Impl{Verifier: recVerifier{"acme", c}, Process: recProcess{"acme", 7, c}}}
	reg := extension.New(extension.WithPack(acme), extension.WithScopeVerifier(recVerifier{"kgateway", c}),
		extension.WithProcessSource(recProcess{"kgateway", 3, c}))
	m := oneShotSequence(t, ctx, append(otherWorld(), acmeDeployment()), reg, emptyScraper{}, true, true).at(t0)
	if len(m.Installs) != 3 {
		t.Fatalf("%d installs, want acme, kgateway and a class-only one", len(m.Installs))
	}
	want := []string{"acme process acme-system.acme", "acme verifier acme-system.acme", "kgateway process kgw.kgateway", "kgateway verifier kgw.kgateway"}
	if got := slices.Sorted(maps.Keys(c.got)); !slices.Equal(got, want) {
		t.Errorf("hook calls:\n%q\nwant:\n%q", got, want)
	}
	for id, desired := range map[string]int32{"acme-system.acme": 7, "kgw.kgateway": 3} {
		if p := installOf(t, m, id).Process; p == nil || p.Controller.Desired == nil || *p.Controller.Desired != desired {
			t.Errorf("%s: controller processes %+v, want its pack's source", id, p)
		}
	}
	if p := installOf(t, m, classOnlyID).Process; p == nil || p.Controller.Desired != nil {
		t.Errorf("class-only controller processes %+v, want none", p)
	}
	// The default verifier: it reads the kgateway install's metrics and says why it cannot verify.
	m = newOneShotWith(t, ctx, world(), extension.New()).at(t0)
	for _, r := range m.Installs[0].Health.Checks {
		if r.Name == "scope matches controller" && r.Error == "" {
			t.Errorf("the kgateway install was not verified: %+v", r)
		}
	}
}

// A second pack without a verifier or process source gets neither, though
// the default pack's are set: no calls for its install, empty process sets.
func TestSecondPackWithoutRuntimeHooks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &hookCalls{got: map[string]bool{}}
	acme := extension.Pack{Name: "acme", Detectors: []install.Detector{acmeDetector{}}}
	reg := extension.New(extension.WithPack(acme), extension.WithScopeVerifier(recVerifier{"kgateway", c}),
		extension.WithProcessSource(recProcess{"kgateway", 3, c}))
	m := oneShotSequence(t, ctx, append(world(), acmeDeployment()), reg, emptyScraper{}, true, false).at(t0)
	if got, want := slices.Sorted(maps.Keys(c.got)), []string{"kgateway process kgw.kgateway", "kgateway verifier kgw.kgateway"}; !slices.Equal(got, want) {
		t.Errorf("hook calls:\n%q\nwant:\n%q", got, want)
	}
	p := installOf(t, m, "acme-system.acme").Process
	if p == nil || p.Controller.Observed || p.Controller.Desired != nil || len(p.Controller.Processes) != 0 {
		t.Errorf("acme controller processes %+v, want none", p)
	}
}

// Only the kgateway pack gets the interval-sized default verifier; a pack
// without a verifier gets none, also as NewCore's first and default pack.
func TestDefaultVerifierIsKgatewaysOnly(t *testing.T) {
	acme := extension.Pack{Name: "acme", Detectors: []install.Detector{acmeDetector{}}}
	for _, tc := range []struct {
		name     string
		reg      *extension.Registry
		impl     string
		verifier bool
	}{{"New, kgateway", extension.New(extension.WithPack(acme)), "kgateway", true}, {"New, second pack", extension.New(extension.WithPack(acme)), "acme", false},
		{"NewCore, default pack", extension.NewCore(extension.WithPack(acme)), "acme", false}, {"NewCore, class-only", extension.NewCore(), "", false}} {
		dyn, kube := fakeClients(t, nil)
		cfg, err := setup.Build(setup.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery()}, setup.Options{MetricsInterval: 10 * time.Second}, tc.reg)
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.Engine.HooksFor(install.Install{Impl: tc.impl}).Verifier != nil; got != tc.verifier {
			t.Errorf("%s: verifier set %v, want %v", tc.name, got, tc.verifier)
		}
	}
}
