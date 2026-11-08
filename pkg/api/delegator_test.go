package api

import (
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/computed/rulesets"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// attachOnly is a rule set without computed.Delegator: view A alone.
type attachOnly struct{ r computed.Rules }

func (a attachOnly) Version() string { return a.r.Version() }

func (a attachOnly) Attachment(in computed.AttachmentInput) computed.AttachmentResult {
	return a.r.Attachment(in)
}

// attachOnlyRules selects the kgateway rule set and drops its delegation.
func attachOnlyRules(v string) computed.Selection {
	s := rulesets.Select(v)
	s.Rules = attachOnly{s.Rules}
	return s
}

// delegatorWorld is outcomeWorld with shop/decl declaring shop/api as its
// parent without status, and shop/p whose only backendRef delegates and is
// reported BackendNotFound.
func delegatorWorld() []runtime.Object {
	failing := testutil.RouteParent{Ref: edgeRef, Controller: testutil.Controller, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}}
	return append(outcomeWorld(),
		leaf("shop", "decl", []objects.ParentRef{shopAPI}, "/api/d"),
		testutil.WithRules(testutil.HTTPRoute("shop", "p", []objects.ParentRef{edgeRef}, failing),
			map[string]any{"backendRefs": []any{testutil.DelegateTo("shop", "c")}}),
		mirrored(), testutil.Backend("shop", "be-m", nil))
}

// mirrored is shop/m, whose one backendRef names shop/kid and mirrors to the Backend shop/be-m.
func mirrored() *unstructured.Unstructured {
	ref := testutil.DelegateTo("shop", "kid")
	ref["filters"] = []any{map[string]any{"type": "RequestMirror", "requestMirror": map[string]any{"backendRef": beRef("be-m")}}}
	return testutil.WithRules(testutil.HTTPRoute("shop", "m", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		map[string]any{"backendRefs": []any{ref}})
}

// Without a Delegator a backendRef to an HTTPRoute is an ordinary backend: the
// backend filter finds its route and its RequestMirror keeps its target.
func TestOrdinaryHTTPRouteBackend(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules func(string) computed.Selection
		kid   []string
		svc   []string // routes mirroring to shop/be-m
	}{
		{"kgateway", nil, []string{}, []string{}},
		{"attachment only", attachOnlyRules, []string{"shop/api", "shop/m"}, []string{"shop/m"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, setup{disputes: true, rulesFor: tc.rules}, delegatorWorld()...)
			for q, want := range map[string][]string{"gateway.networking.k8s.io/HTTPRoute/shop/kid": tc.kid, "gateway.kgateway.dev/Backend/shop/be-m": tc.svc} {
				if got := routeNames(get[Page[RouteRow]](t, srv, listBase+"/routes?backend="+q, 200)); !slices.Equal(got, want) {
					t.Errorf("backend %s: rows %v, want %v", q, got, want)
				}
			}
			m := get[RouteDetail](t, srv, routeDetail+"shop/m", 200)
			if l := m.Rules[0].BackendRefs[0].Filters[0].Mirror.BackendRef.Link; (l != LinkNone) != (tc.rules != nil) {
				t.Errorf("mirror target link %q", l)
			}
		})
	}
}

// Design §8.3: without a Delegator only delegation is off. The kgateway rule
// set on the same world is the contrast, so no assertion holds vacuously.
func TestRuleSetWithoutDelegator(t *testing.T) {
	for _, tc := range []struct {
		name      string
		rules     func(string) computed.Selection
		delegates bool
	}{
		{"kgateway", nil, true},
		{"attachment only", attachOnlyRules, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, setup{disputes: true, rulesFor: tc.rules}, delegatorWorld()...)
			status := 200
			if !tc.delegates {
				status = 404
			}
			get[Page[DelegationRow]](t, srv, delegURL("shop", "api", ""), status)

			api := get[RouteDetail](t, srv, routeDetail+"shop/api", 200)
			ref := api.Rules[0].BackendRefs[0]
			// Admission opens shop/kid either way: as a child, or as an ordinary ref's target.
			if (ref.Delegation != nil) != tc.delegates || ref.Ref != (LinkedRef{Ref: route("shop", "kid"), Link: LinkDetail}) {
				t.Errorf("backendRef to shop/kid: delegation %+v ref %+v", ref.Delegation, ref.Ref)
			}
			if (api.ChildCount > 0) != tc.delegates || (api.DeclaredCount > 0) != tc.delegates {
				t.Errorf("children %d declared %d", api.ChildCount, api.DeclaredCount)
			}
			// View A and its cross-check stay.
			if len(api.Attachments) == 0 || api.RuleSet != "kgateway-2.4" {
				t.Fatalf("view A: rule set %q attachments %+v", api.RuleSet, api.Attachments)
			}
			if c := api.Attachments[0].Result.Computed; c.Rules != "kgateway-2.4" || c.State != string(computed.StateChecked) {
				t.Errorf("view A computed %+v", c)
			}

			h := get[HealthResponse](t, srv, base+"/health", 200)
			if got := hasSev(h, health.CodeDelegatedNotAttached, "", ""); got != tc.delegates {
				t.Errorf("KGWUI-011 raised %v", got)
			}
			// The refs check reads the delegating ref as an ordinary one.
			p := findingsOn(h, "shop/p")
			worded := false
			for _, f := range p {
				worded = worded || f.Code == health.CodeRefUnresolved && f.Message == health.DelegationNoChildMessage
			}
			if worded != tc.delegates || len(p) == 0 {
				t.Errorf("shop/p findings %+v", p)
			}
		})
	}
}

// A KGWUI-012 from an attachment disagreement survives without a Delegator.
func TestAttachmentOnlyKeepsKGWUI012(t *testing.T) {
	_, srv, _ := startWith(t, setup{disputes: true, rulesFor: attachOnlyRules}, selectorWorld()...)
	if !hasSev(get[HealthResponse](t, srv, base+"/health", 200), health.CodeComputedDisagrees, "", "infra/odd") {
		t.Fatal("no KGWUI-012 on infra/odd")
	}
}
