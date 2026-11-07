package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// crdDefault is the condition the Gateway API CRDs default a status to.
func crdDefault(typ string) metav1.Condition {
	return metav1.Condition{Type: typ, Status: metav1.ConditionUnknown, Reason: "Pending", Message: "Waiting for controller", LastTransitionTime: metav1.Unix(0, 0)}
}

// routeWith is shop/name on infra/edge with one status entry of this controller.
func routeWith(name string, cs ...metav1.Condition) *unstructured.Unstructured {
	gw := testutil.GwRef("infra", "edge")
	return testutil.HTTPRoute("shop", name, []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: cs})
}

// freshnessOf returns the evidence of the one KGWUI-003 on res.
func freshnessOf(t *testing.T, h HealthResponse, res string) (FindingDTO, *health.FreshnessEvidence) {
	t.Helper()
	var got []FindingDTO
	for _, f := range findingsOn(h, res) {
		if f.Code == health.CodeRouteUnattached {
			got = append(got, f)
		}
	}
	if len(got) != 1 {
		t.Fatalf("want one KGWUI-003 on %s, got %+v", res, findingsOn(h, res))
	}
	ev, ok := got[0].Facts.(*health.FreshnessEvidence)
	if !ok {
		t.Fatalf("KGWUI-003 on %s: evidence %#v, want freshness", res, got[0].Facts)
	}
	return got[0], ev
}

// Design §10.1: each KGWUI-003 carries its freshness kind; an object that
// matches two kinds takes the first in the table order.
func TestFreshnessKindsAtTheAPI(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	accepted, resolved := testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")
	pendingAccepted := testutil.Cond("Accepted", metav1.ConditionTrue, "Pending")
	unknownRefs := testutil.Cond("ResolvedRefs", metav1.ConditionUnknown, "Unresolved")
	policy := testutil.TrafficPolicy("shop", "tp")
	policy.Object["spec"] = map[string]any{"targetRefs": []any{map[string]any{"group": objects.GatewayAPIGroup, "kind": "Gateway", "name": "edge"}}}
	defaultGw := testutil.Gateway("infra", "fresh", "kgateway", []metav1.Condition{crdDefault("Accepted"), crdDefault("Programmed")})
	seen := testutil.Now.Add(-health.Grace - time.Second)
	cases := []struct {
		name, res, kind string
		obj             *unstructured.Unstructured
		observed        int64 // 0: no generations in the evidence
	}{
		{"no status from this controller", "shop/none", health.FreshnessNeverWritten, testutil.HTTPRoute("shop", "none", []objects.ParentRef{gw}), 0},
		{"route with only the CRD default", "shop/dflt", health.FreshnessNeverWritten, routeWith("dflt", crdDefault("Accepted"), crdDefault("ResolvedRefs")), 0},
		{"Gateway with only the CRD default", "infra/fresh", health.FreshnessNeverWritten, defaultGw, 0},
		{"older generation", "shop/old", health.FreshnessStale, stale(routeWith("old", accepted, resolved)), 1},
		{"reason Pending", "shop/pend", health.FreshnessPending, routeWith("pend", pendingAccepted, resolved), 0},
		{"status Unknown", "shop/unk", health.FreshnessPending, routeWith("unk", accepted, unknownRefs), 0},
		{"parent not found", "shop/lost", health.FreshnessNeverWritten, testutil.HTTPRoute("shop", "lost", []objects.ParentRef{testutil.GwRef("infra", "gone")}), 0},
		{"policy with no ancestor", "shop/tp", health.FreshnessNoAncestor, policy, 0},
		{"stale and Pending", "shop/both", health.FreshnessStale, stale(routeWith("both", pendingAccepted, resolved)), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, setup{}, world(tc.obj)...)
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
			f, ev := freshnessOf(t, h, tc.res)
			// Severity and grace are unchanged: past the grace, a warning.
			if f.Severity != health.SeverityWarning || ev.Freshness != tc.kind {
				t.Fatalf("want a %s warning, got %s %+v", tc.kind, f.Severity, ev)
			}
			if tc.observed == 0 {
				if ev.Observed != nil || ev.Current != nil || ev.Since != nil {
					t.Fatalf("only stale carries generations: %+v", ev)
				}
				return
			}
			if ev.Observed == nil || *ev.Observed != tc.observed || ev.Current == nil || *ev.Current != 2 || ev.Since == nil || !ev.Since.Equal(seen) {
				t.Fatalf("want observed %d, current 2, since %s; got %+v", tc.observed, seen, ev)
			}
		})
	}
}

// Inside the grace a never-written route is info and still names its kind.
func TestFreshnessKindInsideGrace(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	_, srv, _ := startWith(t, setup{late: []*unstructured.Unstructured{testutil.HTTPRoute("shop", "young", []objects.ParentRef{gw})}}, world()...)
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
	if f, ev := freshnessOf(t, h, "shop/young"); f.Severity != health.SeverityInfo || ev.Freshness != health.FreshnessNeverWritten {
		t.Fatalf("want a never-written info, got %s %+v", f.Severity, ev)
	}
}

// Design §18.2: a 64 KiB status message is quoted cut at 1 KiB with the
// marker, and its tail reaches no API body. The raw object view is the object.
func TestEvidenceClipsLongStatus(t *testing.T) {
	const key = "AKIAFAKEKEY123456"
	bad := testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")
	// The leading byte puts a two-byte rune across the 1 KiB limit.
	bad.Message = "a" + strings.Repeat("é", 32*1024-len(key)) + key
	big := routeWith("big", testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), bad)
	h := newHarness(t, setup{}, world(big)...)
	hr := get[HealthResponse](t, h.srv, "/api/v1/installs/kgw.kgateway/health", 200)
	var detail, id string
	for _, f := range findingsOn(hr, "shop/big") {
		if f.Code == health.CodeRefUnresolved {
			detail, id = f.Detail, f.ID
		}
	}
	if !strings.Contains(detail, "…[truncated]") || len(detail) > 1024+200 || !utf8.ValidString(detail) || strings.Contains(detail, "�") {
		t.Fatalf("KGWUI-013 detail not cut at 1 KiB at a rune boundary: %d bytes", len(detail))
	}
	route := "/api/v1/installs/kgw.kgateway/routes/" + objects.GatewayAPIGroup + "/HTTPRoute/shop/big"
	for _, path := range []string{
		"/api/v1/installs/kgw.kgateway/health", "/api/v1/installs/kgw.kgateway/overview", "/api/v1/installs/kgw.kgateway/findings",
		"/api/v1/installs/kgw.kgateway/findings/" + id, "/api/v1/installs/kgw.kgateway/diagnostics", "/api/v1/installs/kgw.kgateway/routes",
		route, route + "/delegation", "/api/v1/installs/kgw.kgateway/gateways", "/api/v1/installs/kgw.kgateway/gateways/infra/edge",
	} {
		resp, err := http.Get(h.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d %v", path, resp.StatusCode, err)
		}
		if strings.Contains(string(b), key) {
			t.Errorf("GET %s quotes the key", path)
		}
	}
}
