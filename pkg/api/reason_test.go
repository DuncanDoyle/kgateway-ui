package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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

// reasonRows collects the rows of the three lists and the overview of srv.
func reasonRows(t *testing.T, srv *httptest.Server) []reasonRow {
	t.Helper()
	var out []reasonRow
	for _, r := range get[Page[RouteRow]](t, srv, listBase+"/routes?limit=200", 200).Items {
		out = append(out, reasonRow{"route " + r.Ref.Namespace + "/" + r.Ref.Name, r.Ref.Ref, r.Health, r.Reason})
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

// reasonKeys fails when a row reason on the wire has a key other than the four
// allowed ones: never detail or evidence (design §7.4 rule 7).
func reasonKeys(t *testing.T, srv *httptest.Server) {
	t.Helper()
	allowed := []string{"kind", "code", "message", "more"}
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
	if !slices.Contains(seen[health.ReasonUnknown], "route shop/cart") || !slices.Contains(seen[health.ReasonUnknown], "gateway edge") ||
		!slices.Contains(seen[health.ReasonUnknown], "listenerset ls1") || !slices.Contains(seen[health.ReasonUnknown], "overview edge") {
		t.Errorf("unknown reasons on %v", seen[health.ReasonUnknown])
	}

	_, ok, _ := startWith(t, setup{}, explorerWorld()...)
	seen = checkReasons(t, ok)
	if !slices.Contains(seen["none"], "route shop/cart") || !slices.Contains(seen["none"], "gateway edge") ||
		!slices.Contains(seen["none"], "listenerset ls1") || !slices.Contains(seen["none"], "overview edge") {
		t.Errorf("rows without a reason %v", seen["none"])
	}
}

// A caller who cannot view every object gets the same code and message on the
// rows it can see, since row health is not filtered (rule 9).
func TestRowReasonForRestrictedCaller(t *testing.T) {
	gw := edgeRef
	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	hide := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Name != "child" })
	_, all, _ := startWith(t, setup{}, explorerWorld(broken)...)
	_, some, _ := startWith(t, setup{authz: hide}, explorerWorld(broken)...)
	a := routeByName(t, get[Page[RouteRow]](t, all, listBase+"/routes?limit=200", 200), "shop/broken").Reason
	s := routeByName(t, get[Page[RouteRow]](t, some, listBase+"/routes?limit=200", 200), "shop/broken").Reason
	if a == nil || s == nil || *a != *s {
		t.Errorf("reasons differ: all %+v, restricted %+v", a, s)
	}
	reasonKeys(t, some)
}
