package api

import (
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/computed/kgw25"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// routeBackend is the Backend state of the first backendRef of route ns/name's first rule.
func routeBackend(t *testing.T, h *harness, install, ns, name string) *BackendStateDTO {
	t.Helper()
	rt := get[RouteDetail](t, h.srv, "/api/v1/installs/"+install+"/routes/gateway.networking.k8s.io/HTTPRoute/"+ns+"/"+name, 200)
	if len(rt.Rules) == 0 || len(rt.Rules[0].BackendRefs) == 0 {
		t.Fatalf("route %s/%s has no backendRef: %+v", ns, name, rt.Rules)
	}
	return rt.Rules[0].BackendRefs[0].Backend
}

// has002 reports whether install's health has a KGWUI-002 on Backend shop/name.
func has002(t *testing.T, h *harness, install, name string) bool {
	t.Helper()
	return slices.ContainsFunc(get[HealthResponse](t, h.srv, "/api/v1/installs/"+install+"/health", 200).Findings, func(f FindingDTO) bool {
		return f.Code == health.CodeConditionFalse && f.Resource != nil && f.Resource.Kind == "Backend" && f.Resource.Name == name
	})
}

// A class-only install runs the core checks and has no rule set, so a
// Backend-shaped object it routes to raises no KGWUI-002 and its route detail
// carries the Accepted condition with no state. The kgateway install routing to
// the same Backend keeps both (Phase 6 design §11).
func TestClassOnlyBackendShowsConditionsOnly(t *testing.T) {
	foreign := testutil.GwRef("infra", "foreign")
	be := testutil.Backend("shop", "be-bad", []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "Invalid")})
	co := testutil.WithRules(testutil.HTTPRoute("shop", "co", []objects.ParentRef{foreign}, testutil.AcceptedBy(foreign, "example.com/other")),
		backendRule([]any{beRef("be-bad")}))
	kr := testutil.WithRules(testutil.HTTPRoute("shop", "kr", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		backendRule([]any{beRef("be-bad")}))
	// Production checks: the core's for a class-only install, core and pack for kgateway.
	hooks := func(i install.Install) engine.Hooks {
		h := packHooks(i)
		if i.Impl == "" {
			h.Checks = health.Core()
		}
		return h
	}
	h := newHarness(t, setup{detectors: []install.Detector{kgwDetector{}}, hooks: hooks, classOnly: true}, world(co, kr, be)...)
	const classOnly = "example.com~2Fother"
	if has002(t, h, classOnly, "be-bad") {
		t.Error("class-only install raised KGWUI-002 on a Backend")
	}
	if b := routeBackend(t, h, classOnly, "shop", "co"); b == nil || b.State != "" || b.Accepted == nil || b.Accepted.Status != "False" {
		t.Errorf("class-only Backend %+v, want its Accepted condition and no state", b)
	}
	if !has002(t, h, "kgw.kgateway", "be-bad") {
		t.Error("the kgateway install must keep its KGWUI-002")
	}
	if b := routeBackend(t, h, "kgw.kgateway", "shop", "kr"); b == nil || b.State != health.StateError || b.Accepted == nil {
		t.Errorf("kgateway Backend %+v, want error with its condition", b)
	}
}

// forwardingRules wraps kgw25 as a downstream rule set does and forwards computed.BackendJudge.
type forwardingRules struct{ computed.Rules }

func (w forwardingRules) JudgeBackend(b *unstructured.Unstructured) (computed.BackendVerdict, *metav1.Condition) {
	return computed.Selection{Rules: w.Rules}.BackendJudge().JudgeBackend(b)
}

// A wrapping rule set that forwards computed.BackendJudge keeps the Backend
// state on route details and the waiting signal on path rows; one that only
// embeds computed.Rules shows the condition alone (D-058 item 9). KGWUI-002 is
// the kgateway pack's check, so both keep it.
func TestBackendJudgeThroughWrapper(t *testing.T) {
	stale := testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")
	waiting := testutil.Backend("shop", "be-wait", []metav1.Condition{stale})
	waiting.SetGeneration(2)
	bad := testutil.Backend("shop", "be-bad", []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "Invalid")})
	rt := func(name, be string) *unstructured.Unstructured {
		return testutil.WithRules(testutil.HTTPRoute("shop", name, []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
			backendRule([]any{beRef(be)}))
	}
	if (computed.Selection{Rules: struct{ computed.Rules }{kgw25.New()}}).BackendJudge() != nil {
		t.Fatal("a bare embedding offers the hook")
	}
	for _, tc := range []struct {
		name    string
		rules   computed.Rules
		judged  bool
		badWant health.State
	}{{"forwarding", forwardingRules{kgw25.New()}, true, health.StateError}, {"bare", struct{ computed.Rules }{kgw25.New()}, false, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			sel := computed.Selection{Rules: tc.rules}
			h := newHarness(t, setup{rulesFor: func(string) computed.Selection { return sel }}, world(rt("r-wait", "be-wait"), rt("r-bad", "be-bad"), waiting, bad)...)
			if b := routeBackend(t, h, "kgw.kgateway", "shop", "r-bad"); b == nil || b.State != tc.badWant || b.Accepted == nil {
				t.Errorf("be-bad %+v, want state %q with its condition", b, tc.badWant)
			}
			if !has002(t, h, "kgw.kgateway", "be-bad") {
				t.Error("the pack check must raise KGWUI-002 under any rule set")
			}
			wait := false
			for _, r := range get[PathsResponse](t, h.srv, pathsURL+"?limit=200", 200).Rows {
				wait = wait || slices.ContainsFunc(r.Problem.Signals, func(s SignalDTO) bool {
					return s.Kind == "waiting" && s.Code == "Accepted" && s.Ref != nil && s.Ref.Name == "be-wait"
				})
			}
			if wait != tc.judged {
				t.Errorf("waiting signal on be-wait %v, want %v", wait, tc.judged)
			}
		})
	}
}
