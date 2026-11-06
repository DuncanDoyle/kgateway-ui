package api

import (
	"net/http"
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var listOnlyKinds = []string{"DirectResponse", "GatewayExtension", "ReferenceGrant"}

func kindNames(m MetaResponse) []string {
	var out []string
	for _, k := range m.Kinds {
		out = append(out, k.Kind)
	}
	return out
}

// A cluster serving the three kinds lists them after Backend, so v1's kind list only gains them at its end.
func TestMetaListsListOnlyKindsLast(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, world()...)
	got := kindNames(get[MetaResponse](t, srv, "/api/v1/meta", http.StatusOK))
	if tail := got[len(got)-4:]; !slices.Equal(tail, []string{"Backend", "DirectResponse", "GatewayExtension", "ReferenceGrant"}) {
		t.Fatalf("kinds %v", got)
	}
	if i := slices.Index(got, "Deployment"); i < 0 || i > len(got)-5 {
		t.Fatalf("Deployment must precede the added kinds: %v", got)
	}
}

// A kind the cluster does not serve is absent, without an error.
func TestMetaOmitsUnservedListOnlyKinds(t *testing.T) {
	var served []kinds.Served
	for _, s := range testutil.ServedUpstream() {
		if !slices.Contains(listOnlyKinds, s.Kind) {
			served = append(served, s)
		}
	}
	_, srv, _ := startWith(t, setup{served: served}, world()...)
	got := kindNames(get[MetaResponse](t, srv, "/api/v1/meta", http.StatusOK))
	for _, k := range listOnlyKinds {
		if slices.Contains(got, k) {
			t.Errorf("%s listed though not served", k)
		}
	}
	get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", http.StatusOK)
}

// ReferenceGrant is read at whichever of v1 and v1beta1 the cluster serves.
func TestReferenceGrantServedAtV1Beta1(t *testing.T) {
	var served []kinds.Served
	for _, s := range testutil.ServedUpstream() {
		if s.Kind == "ReferenceGrant" {
			s.Version = "v1beta1"
		}
		served = append(served, s)
	}
	_, srv, _ := startWith(t, setup{served: served}, world(testutil.ReferenceGrant("shop", "g", "blog"))...)
	m := get[MetaResponse](t, srv, "/api/v1/meta", http.StatusOK)
	i := slices.IndexFunc(m.Kinds, func(k kinds.Served) bool { return k.Kind == "ReferenceGrant" })
	if i < 0 || m.Kinds[i].Version != "v1beta1" || m.Kinds[i].Category != kinds.CategoryReferenceGrant {
		t.Fatalf("ReferenceGrant entry: %+v", m.Kinds)
	}
	if n, ok := count(get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", http.StatusOK), "ReferenceGrant"); !ok || n != 1 {
		t.Fatalf("ReferenceGrant tile %d, %v", n, ok)
	}
}

// None of the three has a status the controller must write: no kind gets a
// KGWUI-002 or KGWUI-003 for the missing status, and the install stays healthy.
func TestListOnlyKindsRaiseNoStatusFindings(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, isolated(testutil.DirectResponse("shop", "dr"), testutil.GatewayExtension("shop", "ext"),
		testutil.ReferenceGrant("shop", "grant", "blog"))...)
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", http.StatusOK)
	for _, f := range h.Findings {
		if f.Resource != nil && slices.Contains([]string{"dr", "ext", "grant"}, f.Resource.Name) {
			t.Errorf("finding %s on %s", f.Code, f.Resource)
		}
	}
	if h.State != health.StateHealthy {
		t.Fatalf("state %s: %+v", h.State, h.Findings)
	}
	o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", http.StatusOK)
	for kind, want := range map[string]int{"DirectResponse": 1, "ReferenceGrant": 1} {
		if n, ok := count(o, kind); !ok || n != want {
			t.Errorf("%s tile %d (listed %v), want %d", kind, n, ok, want)
		}
	}
	if _, ok := count(o, "GatewayExtension"); ok {
		t.Error("GatewayExtension has a tile, like GatewayParameters it has none")
	}
}

// ReferenceGrants are never evaluated: 013 and 014 are the same with grants present.
func TestReferenceGrantsDoNotChangeRefFindings(t *testing.T) {
	route := func() runtime.Object {
		return refsRoute("RefNotPermitted", map[string]any{"name": "cart", "namespace": "shop-b", "port": int64(80)})
	}
	body := func(extra ...*unstructured.Unstructured) HealthResponse {
		objs := world(route())
		for _, e := range extra {
			objs = append(objs, e)
		}
		_, srv, _ := startWith(t, setup{}, objs...)
		return get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", http.StatusOK)
	}
	without := body()
	with := body(testutil.ReferenceGrant("shop-b", "allow", "shop"), testutil.DirectResponse("shop", "dr"))
	if !hasFinding(without, health.CodeRefNotPermitted, "") && !hasFinding(without, health.CodeConditionFalse, "") {
		t.Fatalf("fixture raises neither 014 nor 002: %+v", without.Findings)
	}
	a, b := without.Findings, with.Findings
	if len(a) != len(b) {
		t.Fatalf("findings differ: %+v vs %+v", a, b)
	}
	for i := range a {
		if a[i].Code != b[i].Code || a[i].ID != b[i].ID || a[i].Message != b[i].Message || a[i].Detail != b[i].Detail {
			t.Fatalf("finding %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}
