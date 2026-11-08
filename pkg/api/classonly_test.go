package api

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kgateway"
	"github.com/DuncanDoyle/snellius/pkg/overview"
)

// kgwDetector is the upstream detector stamping the kgateway pack, as extension.New wraps it.
type kgwDetector struct{ install.UpstreamDetector }

func (kgwDetector) Detect(d *appsv1.Deployment) (install.Install, bool) {
	in, ok := install.UpstreamDetector{}.Detect(d)
	in.Impl = kgateway.Name
	return in, ok
}

// packHooks gives kgateway installs the kgateway pack's hooks and the rest the core's.
func packHooks(i install.Install) engine.Hooks {
	if i.Impl != kgateway.Name {
		return engine.Hooks{}
	}
	return engine.Hooks{Scope: kgateway.Scope(), Settings: kgateway.Settings, RulesFor: kgateway.RulesFor, Version: kgateway.Version,
		ExperimentalOnly: kgateway.ExperimentalOnly}
}

// A class-only install has no edition, version or readiness, and its scope
// is all namespaces, assumed (Phase 5 design §5.2, §5.3).
func TestClassOnlyInstallSummary(t *testing.T) {
	// world has the class "other" of example.com/other and its Gateway infra/foreign.
	h := newHarness(t, setup{detectors: []install.Detector{kgwDetector{}}, hooks: packHooks, classOnly: true}, world()...)
	got := get[[]InstallSummary](t, h.srv, "/api/v1/installs", 200)
	if len(got) != 2 {
		t.Fatalf("installs %+v, want kgateway and one class-only", got)
	}
	co := got[0] // sorted by ID: "example.com~2Fother" < "kgw.kgateway"
	if co.ID != "example.com~2Fother" || co.DisplayName != "Gateway API controller example.com/other" || co.Edition != "" || co.Version != "" ||
		co.Namespace != "" || co.Name != "" || len(co.ControllerNames) != 1 || co.ControllerNames[0] != "example.com/other" {
		t.Fatalf("class-only summary %+v", co)
	}
	if co.Readiness.State != overview.ReadinessNotObserved || co.Readiness.Ready != nil || co.Readiness.Desired != nil {
		t.Fatalf("class-only readiness %+v, want not observed", co.Readiness)
	}
	if co.Scope == nil || co.Scope.Mode != "all" || co.Scope.Source != install.SourceAssumed {
		t.Fatalf("class-only scope %+v, want all, assumed", co.Scope)
	}
	if k := got[1]; k.ID != "kgw.kgateway" || k.Edition != "kgateway" || k.Scope == nil || k.Scope.Source == install.SourceAssumed {
		t.Fatalf("kgateway summary %+v", k)
	}
}

// A class-only install gets no experimental advice; a kgateway install in the
// same model keeps it (Phase 5 design §1, item 1).
func TestClassOnlyGetsNoExperimentalAdvice(t *testing.T) {
	foreign := testutil.GwRef("infra", "foreign")
	cxls, kxls := lsWith("cxls", "XListenerSet", foreign, nil), lsWith("kxls", "XListenerSet", edgeRef, nil)
	delete(cxls.Object, "status")
	delete(kxls.Object, "status")
	h := newHarness(t, setup{detectors: []install.Detector{kgwDetector{}}, hooks: packHooks, classOnly: true, disputes: true}, world(cxls, kxls)...)
	const co = "/api/v1/installs/example.com~2Fother"
	if x := get[ListenerSetDetail](t, h.srv, co+"/listenersets/gateway.networking.x-k8s.io/XListenerSet/infra/cxls", 200); x.ExperimentalHint != "" {
		t.Errorf("class-only ListenerSet page hint %q, want none", x.ExperimentalHint)
	}
	if x := get[ListenerSetDetail](t, h.srv, xlsDetail+"infra/kxls", 200); x.ExperimentalHint == "" {
		t.Error("the kgateway install must keep its ListenerSet page hint")
	}
	found := false
	for _, f := range get[HealthResponse](t, h.srv, co+"/health", 200).Findings {
		if f.Resource != nil && f.Resource.Name == "cxls" {
			found = true
			if strings.Contains(f.Detail, "experimental") {
				t.Errorf("class-only finding %s has experimental advice: %q", f.Code, f.Detail)
			}
		}
	}
	if !found {
		t.Fatal("fixture: the class-only XListenerSet without status must raise a finding")
	}
	kept := false
	for _, f := range get[HealthResponse](t, h.srv, base+"/health", 200).Findings {
		kept = kept || f.Resource != nil && f.Resource.Name == "kxls" && strings.Contains(f.Detail, "experimental")
	}
	if !kept {
		t.Error("the kgateway install must keep its experimental advice")
	}
}
