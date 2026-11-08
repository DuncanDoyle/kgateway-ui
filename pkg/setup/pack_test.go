package setup_test

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/computed/kgw23"
	"github.com/DuncanDoyle/snellius/pkg/extension"
	"github.com/DuncanDoyle/snellius/pkg/install"
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
