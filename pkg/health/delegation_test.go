package health

import (
	"reflect"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// delegationWorld is healthyWorld plus parent shop/api attached to the Gateway.
func delegationWorld(extra ...*unstructured.Unstructured) *world {
	w := healthyWorld()
	gw := testutil.GwRef("infra", "edge")
	w.objs = append(w.objs, testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)))
	w.objs = append(w.objs, extra...)
	return w
}

// seenAt makes every object first seen at testutil.Now minus its age; others are old.
func seenAt(ages map[string]time.Duration) func(*unstructured.Unstructured) time.Time {
	return func(u *unstructured.Unstructured) time.Time {
		if d, ok := ages[u.GetNamespace()+"/"+u.GetName()]; ok {
			return testutil.Now.Add(-d)
		}
		return testutil.Now.Add(-time.Hour)
	}
}

func findingsFor(r Report, code, res string) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Code == code && f.Resource != nil && f.Resource.String() == res {
			out = append(out, f)
		}
	}
	return out
}

func statusCheck(r Report) CheckResult {
	for _, c := range r.Checks {
		if c.Name == "resource status" {
			return c
		}
	}
	return CheckResult{}
}

func TestKGWUI011InGraceIsInfoAndPending(t *testing.T) {
	w := delegationWorld(testutil.HTTPRoute("shop", "c", []objects.ParentRef{testutil.HTTPRouteRef("shop", "api")}))
	w.firstSeen = seenAt(map[string]time.Duration{"shop/c": 10 * time.Second, "shop/api": 10 * time.Second})
	r := w.report(t)
	fs := findingsFor(r, CodeDelegatedNotAttached, "shop/c")
	if len(fs) != 1 || fs[0].Severity != SeverityInfo || fs[0].Slug != "delegated-route-not-attached" {
		t.Fatalf("want one KGWUI-011 info on shop/c: %+v", r.Findings)
	}
	if want := []objects.Ref{{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "api"}}; !reflect.DeepEqual(fs[0].Evidence, want) {
		t.Fatalf("evidence %+v, want %+v", fs[0].Evidence, want)
	}
	if c := statusCheck(r); c.OK || !c.Pending || c.Error != "1 resource is being processed by the controller" {
		t.Fatalf("resource status: %+v", c)
	}
	if r.State != StateUnknown {
		t.Fatalf("state %s, want unknown", r.State)
	}
}

// Grace runs from the later of the route's and the parent's first-seen time.
func TestKGWUI011AfterGraceIsWarningAndCheckOK(t *testing.T) {
	c := testutil.HTTPRoute("shop", "c", []objects.ParentRef{testutil.HTTPRouteRef("shop", "api")})
	cases := []struct {
		name    string
		ages    map[string]time.Duration
		mature  bool
		pending bool
	}{
		{"both old", map[string]time.Duration{"shop/c": 2 * Grace, "shop/api": 2 * Grace}, true, false},
		{"parent newer", map[string]time.Duration{"shop/c": 2 * Grace, "shop/api": Grace - time.Second}, false, true},
		{"route newer", map[string]time.Duration{"shop/c": Grace - time.Second, "shop/api": 2 * Grace}, false, true},
		{"exactly at grace end", map[string]time.Duration{"shop/c": Grace, "shop/api": 2 * Grace}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := delegationWorld(c.DeepCopy())
			w.firstSeen = seenAt(tc.ages)
			r := w.report(t)
			fs := findingsFor(r, CodeDelegatedNotAttached, "shop/c")
			want := SeverityInfo
			if tc.mature {
				want = SeverityWarning
			}
			if len(fs) != 1 || fs[0].Severity != want {
				t.Fatalf("want one KGWUI-011 %s: %+v", want, r.Findings)
			}
			if fs[0].Message != "Parent route does not use this route" || fs[0].Detail != "No status from HTTPRoute shop/api" {
				t.Fatalf("wording: %q / %q", fs[0].Message, fs[0].Detail)
			}
			if ck := statusCheck(r); ck.Pending != tc.pending || ck.OK == tc.pending {
				t.Fatalf("resource status: %+v", ck)
			}
			if tc.mature && r.State != StateWarning {
				t.Fatalf("state %s, want warning", r.State)
			}
		})
	}
}

// A reported child with hostnames that kgateway rejects is owned, and its
// entry is judged: KGWUI-002, not KGWUI-011.
func TestReportedEdgeEntryFalseIsKGWUI002(t *testing.T) {
	api := testutil.HTTPRouteRef("shop", "api")
	child := testutil.WithHostnames(testutil.HTTPRoute("shop", "c", []objects.ParentRef{api}, testutil.RouteParent{Ref: api, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "UnsupportedValue"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}}), "shop.example.com")
	r := delegationWorld(child).report(t)
	if !has(r, CodeConditionFalse, SeverityError, "shop/c") || has(r, CodeDelegatedNotAttached, "", "shop/c") {
		t.Fatalf("want KGWUI-002 error only: %+v", r.Findings)
	}
}

// An eligible but stale entry follows Phase 1's freshness rules (KGWUI-003).
func TestStaleMatchingEntryIsKGWUI003NotKGWUI011(t *testing.T) {
	api := testutil.HTTPRouteRef("shop", "api")
	child := testutil.HTTPRoute("shop", "c", []objects.ParentRef{api}, testutil.AcceptedBy(api, testutil.Controller))
	child.SetGeneration(2)
	r := delegationWorld(child).report(t)
	if !has(r, CodeRouteUnattached, SeverityWarning, "shop/c") || has(r, CodeDelegatedNotAttached, "", "shop/c") {
		t.Fatalf("want KGWUI-003 only: %+v", r.Findings)
	}
}

// An explicit declared parent always gets the diagnostic, even when only a
// wildcard ref of that parent selects the route (design §15 Q15).
func TestWildcardOnlyParentStillGetsDiagnostic(t *testing.T) {
	w := delegationWorld(testutil.HTTPRoute("shop", "c", []objects.ParentRef{testutil.HTTPRouteRef("shop", "api")}))
	for _, u := range w.objs {
		if u.GetName() == "api" {
			testutil.WithRules(u, map[string]any{"backendRefs": []any{testutil.DelegateWildcard("shop")}})
		}
	}
	r := w.report(t)
	if !has(r, CodeDelegatedNotAttached, SeverityWarning, "shop/c") {
		t.Fatalf("want KGWUI-011 warning: %+v", r.Findings)
	}
}

// One finding per (route, parent HTTPRoute), ordered by parent.
func TestKGWUI011IsPerParent(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	w := delegationWorld(
		testutil.HTTPRoute("shop", "web", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.HTTPRoute("shop", "c", []objects.ParentRef{testutil.HTTPRouteRef("shop", "web"), testutil.HTTPRouteRef("shop", "api")}))
	fs := findingsFor(w.report(t), CodeDelegatedNotAttached, "shop/c")
	if len(fs) != 2 || fs[0].Detail != "No status from HTTPRoute shop/api" || fs[1].Detail != "No status from HTTPRoute shop/web" {
		t.Fatalf("want two findings ordered by parent: %+v", fs)
	}
}

// A parent whose delegating rules keep no child reports
// ResolvedRefs=False/BackendNotFound (D-021 item 9). Only when every
// backendRef delegates does the finding say so; code and severity stay.
func TestDelegationBackendNotFoundHasDelegationDetail(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	failing := testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}}
	cases := []struct {
		name       string
		backends   []any
		delegation bool
	}{
		{"delegation only", []any{testutil.DelegateTo("shop", "c")}, true},
		{"label only", []any{testutil.DelegateLabel("shop", "team")}, true},
		{"mixed with a Service", []any{testutil.DelegateTo("shop", "c"), map[string]any{"name": "svc", "port": int64(80)}}, false},
		{"Service only", []any{map[string]any{"name": "svc", "port": int64(80)}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := testutil.WithRules(testutil.HTTPRoute("shop", "p", []objects.ParentRef{gw}, failing), map[string]any{"backendRefs": tc.backends})
			r := delegationWorld(p).report(t)
			fs := findingsFor(r, CodeConditionFalse, "shop/p")
			if len(fs) != 1 || fs[0].Severity != SeverityError {
				t.Fatalf("want one KGWUI-002 error: %+v", r.Findings)
			}
			got := fs[0].Message == DelegationNoChildMessage && strings.HasPrefix(fs[0].Detail, DelegationNoChildDetail)
			if got != tc.delegation || (!tc.delegation && fs[0].Message != "Backend not found") {
				t.Fatalf("delegation wording %v, want %v: %q / %q", got, tc.delegation, fs[0].Message, fs[0].Detail)
			}
		})
	}
}
