package setup_test

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/computed/kgw23"
	"github.com/DuncanDoyle/snellius/pkg/extension"
	"github.com/DuncanDoyle/snellius/pkg/install"
)

// renamed is the upstream detector under another name, as a downstream adds it.
type renamed struct{ install.UpstreamDetector }

func (renamed) Name() string { return "renamed" }

// nobody is a detector that claims no Deployment.
type nobody struct{ install.UpstreamDetector }

func (nobody) Name() string { return "nobody" }

func (nobody) Detect(*appsv1.Deployment) (install.Install, bool) { return install.Install{}, false }

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

// A second pack's hooks apply to its installs only: the kgateway install keeps
// the kgateway pack's rule sets.
func TestPackHooksArePerInstall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	acme := func(string) computed.Selection { return computed.Selection{Rules: kgw23.New(), Note: "acme"} }
	reg := extension.New(extension.WithPack(extension.Pack{Name: "acme", Detectors: []install.Detector{nobody{}}, Impl: extension.Impl{RulesFor: acme}}))
	m := newOneShotWith(t, ctx, world(), reg).at(t0)
	if r := m.Installs[0].Rules; r.Note != "" || r.Rules.Version() != "kgateway-2.4" {
		t.Fatalf("kgateway install rules %s %q, want kgateway-2.4 from rulesets.Select", r.Rules.Version(), r.Note)
	}
}
