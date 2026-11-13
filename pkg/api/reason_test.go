package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// reasonRow is what each list and overview row says about its state.
type reasonRow struct {
	name   string
	ref    objects.Ref
	state  health.State
	reason *health.RowReason
}

// rowReason is r without its backend.
func rowReason(r *RouteReason) *health.RowReason {
	if r == nil {
		return nil
	}
	return &r.RowReason
}

// reasonRows collects the rows of the three lists and the overview of srv.
func reasonRows(t *testing.T, srv *httptest.Server) []reasonRow {
	t.Helper()
	var out []reasonRow
	for _, r := range get[Page[RouteRow]](t, srv, listBase+"/routes?limit=200", 200).Items {
		out = append(out, reasonRow{"route " + r.Ref.Namespace + "/" + r.Ref.Name, r.Ref.Ref, r.Health, rowReason(r.Reason)})
	}
	for _, r := range get[Page[GatewayListRow]](t, srv, listBase+"/gateways", 200).Items {
		out = append(out, reasonRow{"gateway " + r.Ref.Name, r.Ref.Ref, r.Health, r.Reason})
	}
	for _, r := range get[Page[ListenerSetRow]](t, srv, listBase+"/listenersets", 200).Items {
		out = append(out, reasonRow{"listenerset " + r.Ref.Name, r.Ref.Ref, r.Health, r.Reason})
	}
	for _, r := range get[OverviewResponse](t, srv, listBase+"/overview", 200).Gateways {
		out = append(out, reasonRow{"overview " + r.Ref.Name, r.Ref, r.State, r.Reason})
	}
	return out
}

// checkReasons checks every row's reason against the install's findings and
// returns the reason kinds seen, "none" for a row without one.
func checkReasons(t *testing.T, srv *httptest.Server) map[string][]string {
	t.Helper()
	fs := get[HealthResponse](t, srv, listBase+"/health", 200).Findings
	seen := map[string][]string{}
	for _, r := range reasonRows(t, srv) {
		var own []FindingDTO
		for _, f := range fs {
			if f.Resource != nil && f.Resource.Ref == r.ref {
				own = append(own, f)
			}
		}
		got := r.reason
		switch {
		case r.state == health.StateHealthy:
			if got != nil {
				t.Errorf("%s: healthy row has reason %+v", r.name, got)
			}
			seen["none"] = append(seen["none"], r.name)
			continue
		case got == nil:
			t.Errorf("%s: %s row has no reason", r.name, r.state)
			continue
		}
		seen[got.Kind] = append(seen[got.Kind], r.name)
		switch got.Kind {
		case health.ReasonUnknown:
			if r.state != health.StateUnknown || got.Code != "" || got.Message != "" || got.More != 0 {
				t.Errorf("%s: %s row, reason %+v", r.name, r.state, got)
			}
		case health.ReasonGatewayRejecting:
			if r.state != health.StateWarning || got.Code != "" || got.Message != "" || slices.ContainsFunc(own, func(f FindingDTO) bool { return f.Severity == health.SeverityWarning }) {
				t.Errorf("%s: %s row, reason %+v, findings %d", r.name, r.state, got, len(own))
			}
		case health.ReasonBackendProblem:
			if r.state != health.StateWarning || got.Code == "" || got.More != 0 || slices.ContainsFunc(own, func(f FindingDTO) bool { return f.Severity == health.SeverityWarning }) {
				t.Errorf("%s: %s row, reason %+v, findings %d", r.name, r.state, got, len(own))
			}
		case health.ReasonFinding:
			// The deciding finding is one of the row's own at the row's severity; more counts the rest.
			i := slices.IndexFunc(own, func(f FindingDTO) bool {
				return string(f.Severity) == string(r.state) && f.Code == got.Code && f.Message == got.Message
			})
			if i < 0 || got.More != len(own)-1 || got.Message == "" {
				t.Errorf("%s: %s row, reason %+v, own findings %d", r.name, r.state, got, len(own))
			}
		default:
			t.Errorf("%s: unknown reason kind %q", r.name, got.Kind)
		}
	}
	return seen
}

// reasonKeys fails when a row reason on the wire has a key other than the
// allowed ones: never detail or evidence (design §7.4 rule 7). Only route rows
// add backend (D-082).
func reasonKeys(t *testing.T, srv *httptest.Server) {
	t.Helper()
	allowed := []string{"kind", "code", "message", "more", "backend"}
	check := func(path string, rows []map[string]json.RawMessage) {
		for _, row := range rows {
			raw, ok := row["reason"]
			if !ok {
				continue
			}
			var keys map[string]any
			if err := json.Unmarshal(raw, &keys); err != nil {
				t.Fatal(err)
			}
			for k := range keys {
				if !slices.Contains(allowed, k) {
					t.Errorf("%s: reason key %q in %s", path, k, raw)
				}
			}
		}
	}
	for _, l := range []string{"routes?limit=200", "gateways", "listenersets"} {
		check(l, get[struct {
			Items []map[string]json.RawMessage `json:"items"`
		}](t, srv, listBase+"/"+l, 200).Items)
	}
	check("overview", get[struct {
		Gateways []map[string]json.RawMessage `json:"gateways"`
	}](t, srv, listBase+"/overview", 200).Gateways)
}

// detailReasons checks that each Gateway, ListenerSet and route detail
// carries its list row's reason, and that a finding reason names one of the
// object's own findings by id.
func detailReasons(t *testing.T, srv *httptest.Server) {
	t.Helper()
	fs := get[HealthResponse](t, srv, listBase+"/health", 200).Findings
	check := func(name string, ref objects.Ref, row *health.RowReason, got *DetailReason) {
		switch {
		case row == nil && got == nil:
			return
		case row == nil || got == nil || got.RowReason != *row:
			t.Errorf("%s: detail reason %+v, row reason %+v", name, got, row)
			return
		}
		ok := slices.ContainsFunc(fs, func(f FindingDTO) bool {
			return f.ID == got.Finding && f.Code == got.Code && f.Resource != nil && f.Resource.Ref == ref
		})
		if (got.Kind == health.ReasonFinding) != ok || (got.Kind != health.ReasonFinding && got.Finding != "") {
			t.Errorf("%s: reason %+v names finding %q", name, got, got.Finding)
		}
	}
	for _, r := range get[Page[RouteRow]](t, srv, listBase+"/routes?limit=200", 200).Items {
		p := fmt.Sprintf("%s/routes/%s/%s/%s/%s", listBase, r.Ref.Group, r.Ref.Kind, r.Ref.Namespace, r.Ref.Name)
		got := get[RouteDetail](t, srv, p, 200).Reason
		check("route "+r.Ref.Name, r.Ref.Ref, rowReason(r.Reason), got)
		if r.Reason != nil && got != nil && !reflect.DeepEqual(r.Reason.Backend, got.Backend) {
			t.Errorf("route %s: detail backend %+v, row backend %+v", r.Ref.Name, got.Backend, r.Reason.Backend)
		}
	}
	for _, r := range get[Page[GatewayListRow]](t, srv, listBase+"/gateways", 200).Items {
		p := fmt.Sprintf("%s/gateways/%s/%s", listBase, r.Ref.Namespace, r.Ref.Name)
		check("gateway "+r.Ref.Name, r.Ref.Ref, r.Reason, get[GatewayDetail](t, srv, p, 200).Reason)
	}
	for _, r := range get[Page[ListenerSetRow]](t, srv, listBase+"/listenersets", 200).Items {
		p := fmt.Sprintf("%s/listenersets/%s/%s/%s/%s", listBase, r.Ref.Group, r.Ref.Kind, r.Ref.Namespace, r.Ref.Name)
		check("listenerset "+r.Ref.Name, r.Ref.Ref, r.Reason, get[ListenerSetDetail](t, srv, p, 200).Reason)
	}
}

// A row's reason names its deciding finding, the install-wide unknown or the
// rejecting Gateway; healthy rows carry none (D-063).
func TestRowReasons(t *testing.T) {
	gw := edgeRef
	rejects := fakeMetrics{"kgw.kgateway": {CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{
		Rejects: map[ctrlmetrics.GatewayKey]float64{{Namespace: "infra", Name: "edge"}: 1}, Managed: map[ctrlmetrics.ManagedKey]float64{}, ManagedPresent: true}}}
	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	down := fakeMetrics{"kgw.kgateway": {CollectedAt: testutil.Now, Err: "connection refused"}}

	_, rej, _ := startWith(t, setup{metrics: rejects}, explorerWorld(broken)...)
	seen := checkReasons(t, rej)
	reasonKeys(t, rej)
	detailReasons(t, rej)
	if !slices.Contains(seen[health.ReasonFinding], "gateway edge") || !slices.Contains(seen[health.ReasonFinding], "overview edge") ||
		!slices.Contains(seen[health.ReasonFinding], "route shop/broken") || !slices.Contains(seen[health.ReasonFinding], "route blog/posts") {
		t.Errorf("finding reasons on %v", seen[health.ReasonFinding])
	}
	if !slices.Contains(seen[health.ReasonGatewayRejecting], "route shop/cart") {
		t.Errorf("gateway-rejecting reasons on %v", seen[health.ReasonGatewayRejecting])
	}
	// The error row and the warning row, exactly.
	rows := get[Page[RouteRow]](t, rej, listBase+"/routes?limit=200", 200)
	if r := routeByName(t, rows, "shop/broken").Reason; r == nil || r.Code != health.CodeRefUnresolved {
		t.Errorf("shop/broken reason %+v", r)
	}
	if r := routeByName(t, rows, "blog/posts").Reason; r == nil || r.Code != health.CodeNamespaceNotWatched {
		t.Errorf("blog/posts reason %+v", r)
	}

	_, unk, _ := startWith(t, setup{metrics: down}, explorerWorld()...)
	seen = checkReasons(t, unk)
	reasonKeys(t, unk)
	detailReasons(t, unk)
	if !slices.Contains(seen[health.ReasonUnknown], "route shop/cart") || !slices.Contains(seen[health.ReasonUnknown], "gateway edge") ||
		!slices.Contains(seen[health.ReasonUnknown], "listenerset ls1") || !slices.Contains(seen[health.ReasonUnknown], "overview edge") {
		t.Errorf("unknown reasons on %v", seen[health.ReasonUnknown])
	}

	_, ok, _ := startWith(t, setup{}, explorerWorld()...)
	seen = checkReasons(t, ok)
	detailReasons(t, ok)
	if !slices.Contains(seen["none"], "route shop/cart") || !slices.Contains(seen["none"], "gateway edge") ||
		!slices.Contains(seen["none"], "listenerset ls1") || !slices.Contains(seen["none"], "overview edge") {
		t.Errorf("rows without a reason %v", seen["none"])
	}
}

// A caller who cannot view a finding's evidence gets the same code, message and
// count on the row it can see, and no list or overview response quotes the
// evidence's Detail (rules 7 and 9).
func TestRowReasonForRestrictedCaller(t *testing.T) {
	const secret = "vault-9f2c"
	parent := testutil.HTTPRouteRef("shop", secret)
	// shop/claims names the hidden parent, which does not select it: KGWUI-011 with the parent as evidence.
	objs := explorerWorld(
		testutil.HTTPRoute("shop", secret, []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		testutil.HTTPRoute("shop", "claims", []objects.ParentRef{parent}),
	)
	hide := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Name != secret })
	_, all, _ := startWith(t, setup{}, objs...)
	_, some, _ := startWith(t, setup{authz: hide}, objs...)

	detail := fmt.Sprintf(health.DelegatedNotAttachedDetail, "shop/"+secret)
	fs := findingsOn(get[HealthResponse](t, all, listBase+"/health", 200), "shop/claims")
	if len(fs) != 1 || fs[0].Code != health.CodeDelegatedNotAttached || fs[0].Detail != detail {
		t.Fatalf("want one KGWUI-011 quoting the parent on shop/claims: %+v", fs)
	}
	a := routeByName(t, get[Page[RouteRow]](t, all, listBase+"/routes?limit=200", 200), "shop/claims").Reason
	s := routeByName(t, get[Page[RouteRow]](t, some, listBase+"/routes?limit=200", 200), "shop/claims").Reason
	if a == nil || s == nil || a.Code != health.CodeDelegatedNotAttached || *a != *s {
		t.Errorf("reasons differ: all %+v, restricted %+v", a, s)
	}
	for _, path := range []string{"/routes?limit=200", "/gateways", "/listenersets", "/overview"} {
		if body := rawGet(t, some, listBase+path); strings.Contains(body, detail) {
			t.Errorf("%s quotes the hidden evidence's detail", path)
		}
	}
	reasonKeys(t, some)
}

// rawGet returns the body of a 200 GET of path on srv.
func rawGet(t *testing.T, srv *httptest.Server, path string) string {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d %v", path, resp.StatusCode, err)
	}
	return string(b)
}
