package api

import (
	"context"
	"net/http"
	"slices"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
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

// A denied ReferenceGrant list is optional input (D-034): no KGWUI-008, the
// install stays known, and the tile still shows the denied access.
func TestDeniedListOnlyKindRaisesNoSignalUnavailable(t *testing.T) {
	deny := func(dyn *dynamicfake.FakeDynamicClient, _ *kubefake.Clientset) {
		dyn.PrependReactor("list", "referencegrants", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: "referencegrants"}, "", nil)
		})
	}
	_, srv, _ := startWith(t, setup{prepare: deny}, isolated(testutil.ReferenceGrant("shop", "grant", "blog"))...)
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", http.StatusOK)
	if hasFinding(h, health.CodeSignalUnavailable, "") || h.State != health.StateHealthy {
		t.Fatalf("state %s: %+v", h.State, h.Findings)
	}
	o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw.kgateway/overview", http.StatusOK)
	for _, c := range o.Counts {
		if c.Kind == "ReferenceGrant" && (c.Access.OK || !c.Access.Denied) {
			t.Fatalf("tile access %+v, want denied", c.Access)
		}
	}
}

// stalledList is a dynamic client whose List of resource blocks until its
// context ends; the fake client cannot block one resource alone.
type stalledList struct {
	dynamic.Interface
	resource string
}

// IsWatchListSemanticsUnSupported keeps the fake's list-then-watch reflector path.
func (stalledList) IsWatchListSemanticsUnSupported() bool { return true }

func (s stalledList) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	ri := s.Interface.Resource(gvr)
	if gvr.Resource != s.resource {
		return ri
	}
	return stalledResource{ri}
}

type stalledResource struct {
	dynamic.NamespaceableResourceInterface
}

func (stalledResource) List(ctx context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// Codex 3.4 #3: a ReferenceGrant list that never completes does not hold back
// Synced or Settled: no KGWUI-008, the install stays known, the tile shows syncing.
func TestPendingListOnlyKindDoesNotGateReadiness(t *testing.T) {
	stall := func(d dynamic.Interface) dynamic.Interface { return stalledList{d, "referencegrants"} }
	h := newHarness(t, setup{wrapDynamic: stall}, isolated(testutil.ReferenceGrant("shop", "grant", "blog"))...)
	waitUntil(t, "settled", h.cache.Settled)
	s := h.cache.Snapshot()
	var gvr schema.GroupVersionResource
	for _, k := range testutil.ServedUpstream() {
		if k.Kind == "ReferenceGrant" {
			gvr = k.GVR()
		}
	}
	if a, known := s.Access[gvr]; !known || !s.Synced || a.OK || a.Err != "" {
		t.Fatalf("synced %v, ReferenceGrant access %+v; want synced with ReferenceGrants syncing", s.Synced, a)
	}
	hr := get[HealthResponse](t, h.srv, "/api/v1/installs/kgw.kgateway/health", http.StatusOK)
	if hasFinding(hr, health.CodeSignalUnavailable, "") || hr.State != health.StateHealthy {
		t.Fatalf("state %s: %+v", hr.State, hr.Findings)
	}
	o := get[OverviewResponse](t, h.srv, "/api/v1/installs/kgw.kgateway/overview", http.StatusOK)
	tile := false
	for _, c := range o.Counts {
		if c.Kind == "ReferenceGrant" {
			tile = !c.Access.OK && c.Access.Err == ""
		}
	}
	if !tile {
		t.Fatalf("ReferenceGrant tile %+v, want syncing", o.Counts)
	}
}
