package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kgateway"
	"github.com/DuncanDoyle/snellius/pkg/objects"
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
		ExperimentalOnly: kgateway.ExperimentalOnly, Conditions: kgateway.Conditions()}
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

// A class-only install has no rule set (Phase 5 design §8.2, user 2026-11-08):
// its pages carry status attachment only, and no computed fact anywhere. The
// kgateway install in the same model keeps its computed facts.
func TestClassOnlyShowsStatusAttachmentOnly(t *testing.T) {
	foreign := testutil.GwRef("infra", "foreign")
	r := testutil.HTTPRoute("shop", "co", []objects.ParentRef{foreign}, testutil.AcceptedBy(foreign, "example.com/other"))
	h := newHarness(t, setup{detectors: []install.Detector{kgwDetector{}}, hooks: packHooks, classOnly: true}, world(r)...)
	const co = "/api/v1/installs/example.com~2Fother"
	gw := get[GatewayDetail](t, h.srv, co+"/gateways/infra/foreign", 200)
	if gw.RuleSet != "" || len(gw.Listeners) != 1 || gw.Listeners[0].Status == nil {
		t.Fatalf("class-only Gateway: rule set %q listeners %+v", gw.RuleSet, gw.Listeners)
	}
	rt := get[RouteDetail](t, h.srv, co+"/routes/gateway.networking.k8s.io/HTTPRoute/shop/co", 200)
	if rt.RuleSet != "" || len(rt.Attachments) != 0 || len(rt.Parents) != 1 || len(rt.Parents[0].Conditions) == 0 {
		t.Fatalf("class-only route: rule set %q attachments %+v parents %+v", rt.RuleSet, rt.Attachments, rt.Parents)
	}
	for _, p := range []string{"/gateways/infra/foreign", "/routes/gateway.networking.k8s.io/HTTPRoute/shop/co", "/gateways", "/routes"} {
		if body := get[json.RawMessage](t, h.srv, co+p, 200); bytes.Contains(body, []byte(`"computed`)) || bytes.Contains(body, []byte(`"ruleSet"`)) {
			t.Errorf("%s carries a computed fact: %s", p, body)
		}
	}
	if k := get[GatewayDetail](t, h.srv, base+"/gateways/infra/edge", 200); k.RuleSet == "" || k.Listeners[0].Computed == nil {
		t.Errorf("the kgateway install must keep its computed facts: %+v", k)
	}
}

// With the controller contradicting attachment and a route declaring an
// HTTPRoute parent, the class-only install raises no KGWUI-011 or 012; the
// kgateway install, given the same shapes, raises both (design §8.2).
func TestClassOnlyRaisesNoComputedFindings(t *testing.T) {
	foreign := testutil.GwRef("infra", "foreign")
	// infra/foreign reports no attached route, so a computed view would disagree.
	objs := withEdge(world(
		testutil.HTTPRoute("shop", "co", []objects.ParentRef{foreign}, testutil.AcceptedBy(foreign, "example.com/other")),
		testutil.HTTPRoute("shop", "co-child", []objects.ParentRef{testutil.HTTPRouteRef("shop", "co")}),
		testutil.HTTPRoute("shop", "k-child", []objects.ParentRef{testutil.HTTPRouteRef("shop", "cart")}),
	), 7)
	h := newHarness(t, setup{detectors: []install.Detector{kgwDetector{}}, hooks: packHooks, classOnly: true, disputes: true}, objs...)
	for _, tc := range []struct {
		install string
		want    bool
	}{{"/api/v1/installs/example.com~2Fother", false}, {base, true}} {
		hr := get[HealthResponse](t, h.srv, tc.install+"/health", 200)
		for _, code := range []string{health.CodeDelegatedNotAttached, health.CodeComputedDisagrees} {
			if got := hasSev(hr, code, "", ""); got != tc.want {
				t.Errorf("%s: %s raised %v, want %v", tc.install, code, got, tc.want)
			}
		}
	}
}

// The route list judges kgateway.dev/Programmed only for an install whose pack
// declares it: a class-only route with it False is not "not programmed"; a
// kgateway route is, as before (Phase 5 design §9).
func TestClassOnlyRouteIgnoresKgatewayConditions(t *testing.T) {
	replaced := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs"),
		testutil.Cond("kgateway.dev/Programmed", metav1.ConditionFalse, "RouteRuleReplaced")}
	foreign := testutil.GwRef("infra", "foreign")
	objs := world(testutil.HTTPRoute("shop", "co", []objects.ParentRef{foreign}, testutil.RouteParent{Ref: foreign, Controller: "example.com/other", Conds: replaced}),
		testutil.HTTPRoute("shop", "kr", []objects.ParentRef{edgeRef}, testutil.RouteParent{Ref: edgeRef, Controller: testutil.Controller, Conds: replaced}))
	h := newHarness(t, setup{detectors: []install.Detector{kgwDetector{}}, hooks: packHooks, classOnly: true}, objs...)
	for _, tc := range []struct {
		install, route string
		judged         bool
	}{{"/api/v1/installs/example.com~2Fother", "co", false}, {base, "kr", true}} {
		var row *RouteRow
		for _, r := range get[Page[RouteRow]](t, h.srv, tc.install+"/routes", 200).Items {
			if r.Ref.Name == tc.route {
				row = &r
			}
		}
		if row == nil || len(row.Parents) != 1 {
			t.Fatalf("%s: row %+v", tc.route, row)
		}
		flagged, cond := slices.Contains(row.Flags, "not-programmed"), row.Parents[0].ConditionType == "kgateway.dev/Programmed"
		if flagged != tc.judged || cond != tc.judged {
			t.Errorf("%s: not-programmed %v, deciding condition %q; want judged by kgateway.dev/Programmed %v", tc.route, flagged, row.Parents[0].ConditionType, tc.judged)
		}
	}
}

// Route freshness reads the install's own condition types (Phase 5 design §9):
// a stale kgateway condition does not hold back a class-only route, a pack's own
// stale optional condition does, and a kgateway route keeps today's freshness.
func TestRouteFreshnessUsesInstallConditions(t *testing.T) {
	foreign := testutil.GwRef("infra", "foreign")
	// stale is a generation-2 route whose Accepted and ResolvedRefs are current and whose cond is not.
	stale := func(name string, p objects.ParentRef, controller, cond string) *unstructured.Unstructured {
		cs := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"),
			testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs"), testutil.Cond(cond, metav1.ConditionTrue, "Programmed")}
		cs[0].ObservedGeneration, cs[1].ObservedGeneration = 2, 2
		u := testutil.HTTPRoute("shop", name, []objects.ParentRef{p}, testutil.RouteParent{Ref: p, Controller: controller, Conds: cs})
		u.SetGeneration(2)
		return u
	}
	attachment := func(srv *httptest.Server, id, name string) explore.Attachment {
		for _, r := range get[Page[RouteRow]](t, srv, "/api/v1/installs/"+id+"/routes?limit=200", 200).Items {
			if r.Ref.Name == name && len(r.Parents) == 1 {
				return r.Parents[0].Attachment
			}
		}
		t.Fatalf("install %s: no route shop/%s with one parent", id, name)
		return ""
	}
	objs := world(stale("co", foreign, "example.com/other", "kgateway.dev/Programmed"),
		stale("kg", edgeRef, testutil.Controller, "kgateway.dev/Programmed"))
	h := newHarness(t, setup{detectors: []install.Detector{kgwDetector{}}, hooks: packHooks, classOnly: true}, objs...)
	if got := attachment(h.srv, "example.com~2Fother", "co"); got != explore.AttachmentAccepted {
		t.Errorf("class-only route with a stale kgateway condition: %q, want accepted", got)
	}
	if got := attachment(h.srv, "kgw.kgateway", "kg"); got != explore.AttachmentPending {
		t.Errorf("kgateway route with a stale kgateway.dev/Programmed: %q, want pending", got)
	}

	// A pack whose optional condition is example.com/Ready: its stale entry holds the route back.
	own := func(i install.Install) engine.Hooks {
		if i.Impl == "" {
			return engine.Hooks{Conditions: health.ConditionTypes{RouteOptional: []string{"example.com/Ready"}}}
		}
		return packHooks(i)
	}
	h = newHarness(t, setup{detectors: []install.Detector{kgwDetector{}}, hooks: own, classOnly: true},
		world(stale("co", foreign, "example.com/other", "example.com/Ready"))...)
	if got := attachment(h.srv, "example.com~2Fother", "co"); got != explore.AttachmentPending {
		t.Errorf("route with the install's own stale optional condition: %q, want pending", got)
	}
}
