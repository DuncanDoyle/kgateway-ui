package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

const base = "/api/v1/installs/kgw.kgateway"

// linker computes links as the handler of a running engine does.
type linker struct {
	e     *engine.Engine
	authz Authorizer
}

func (l linker) link(t *testing.T, ref objects.Ref) Link {
	t.Helper()
	m := l.e.Current().Model
	for k := range m.Installs {
		if i := &m.Installs[k]; i.Resolved.ID() == "kgw.kgateway" {
			h := &handler{src: l.e, authz: l.authz}
			return h.link(httptest.NewRequest(http.MethodGet, "/", nil), m, i, ref)
		}
	}
	t.Fatal("install kgw.kgateway not found")
	return ""
}

// resourcePath is the resource endpoint path of ref.
func resourcePath(ref objects.Ref) string {
	dash := func(s string) string {
		if s == "" {
			return "-"
		}
		return s
	}
	return base + "/resources/" + dash(ref.Group) + "/" + ref.Kind + "/" + dash(ref.Namespace) + "/" + ref.Name
}

// raw is the status and body of GET path.
func raw(t *testing.T, srv *httptest.Server, path string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, b
}

func route(ns, name string) objects.Ref {
	return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: ns, Name: name}
}

func backendRef(ns, name string) objects.Ref {
	return objects.Ref{Group: "gateway.kgateway.dev", Kind: "Backend", Namespace: ns, Name: name}
}

// withBackendRefs gives u one rule sending to refs.
func withBackendRefs(u *unstructured.Unstructured, refs ...map[string]any) *unstructured.Unstructured {
	r := testutil.Rule()
	brs := make([]any, 0, len(refs))
	for _, b := range refs {
		brs = append(brs, b)
	}
	r["backendRefs"] = brs
	return testutil.WithRules(u, r)
}

func toBackend(ns, name string) map[string]any {
	return map[string]any{"group": "gateway.kgateway.dev", "kind": "Backend", "namespace": ns, "name": name}
}

// Every finding of the golden world links to a view that opens (design §7.1).
func TestEveryFindingResourceOpensItsView(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	_, srv, _ := startWith(t, setup{late: []*unstructured.Unstructured{testutil.HTTPRoute("shop", "young", []objects.ParentRef{gw})}}, world(broken)...)
	h := get[HealthResponse](t, srv, base+"/health", 200)
	n := 0
	for _, f := range h.Findings {
		if f.Resource == nil {
			continue
		}
		n++
		if f.Resource.Link == LinkNone {
			t.Errorf("%s on %s %s: link none", f.Code, f.Resource.Kind, f.Resource)
			continue
		}
		get[map[string]any](t, srv, resourcePath(f.Resource.Ref), 200)
	}
	if n < 3 {
		t.Fatalf("want findings on resources, got %+v", h.Findings)
	}
	o := get[OverviewResponse](t, srv, base+"/overview", 200)
	if len(o.Gateways) != 1 || o.Gateways[0].Link != LinkDetail {
		t.Fatalf("overview Gateway row link: %+v", o.Gateways)
	}
}

// Objects named by an owned route's backendRef or parentRef are candidates:
// a Backend out of scope opens as YAML. A no-parent row opens as YAML.
func TestSpecNamedObjectsAreCandidates(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	e, srv, _ := startWith(t, setup{}, isolated(
		withBackendRefs(testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)), toBackend("blog", "far")),
		testutil.Backend("blog", "far", nil),
		testutil.HTTPRoute("shop", "free", nil),
	)...)
	l := linker{e: e, authz: AllowAll{}}
	for _, tc := range []struct {
		ref  objects.Ref
		want Link
	}{
		{backendRef("blog", "far"), LinkYAML},
		{route("shop", "free"), LinkYAML},
		{route("shop", "api"), LinkDetail},
		{objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}, LinkDetail},
	} {
		if got := l.link(t, tc.ref); got != tc.want {
			t.Errorf("%s %s: link %s, want %s", tc.ref.Kind, tc.ref, got, tc.want)
		}
		get[map[string]any](t, srv, resourcePath(tc.ref), 200)
	}
}

// An object the install neither owns, nor names, nor reports on is none and
// 404, as is a kind that is never served.
func TestUnrelatedObjectIs404OnBothEndpoints(t *testing.T) {
	foreign := testutil.GwRef("infra", "foreign")
	objs := isolated(testutil.HTTPRoute("shop", "other", []objects.ParentRef{foreign}, testutil.AcceptedBy(foreign, "example.com/other")))
	e, srv, _ := startWith(t, setup{}, objs...)
	l := linker{e: e, authz: AllowAll{}}
	for _, ref := range []objects.Ref{
		route("shop", "other"),
		{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "foreign"},
		{Kind: "Service", Namespace: "shop", Name: "svc"},
		{Kind: "Secret", Namespace: "shop", Name: "x"},
	} {
		if got := l.link(t, ref); got != LinkNone {
			t.Errorf("%s %s: link %s, want none", ref.Kind, ref, got)
		}
		get[map[string]any](t, srv, resourcePath(ref), 404)
	}
}

// Admission does not follow chains: D named only by candidate C is none and
// 404; once the install owns D it is detail.
func TestSecondStepIsNone(t *testing.T) {
	gw, api := testutil.GwRef("infra", "edge"), testutil.HTTPRouteRef("shop", "api")
	foreign := testutil.GwRef("infra", "foreign")
	owner := testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller))
	for _, tc := range []struct {
		name string
		d    *unstructured.Unstructured
		want Link
	}{
		{"named only by C", testutil.HTTPRoute("blog", "d", []objects.ParentRef{foreign}), LinkNone},
		{"owned", testutil.HTTPRoute("blog", "d", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)), LinkDetail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testutil.WithRules(testutil.HTTPRoute("shop", "c", []objects.ParentRef{api}), map[string]any{"backendRefs": []any{testutil.DelegateTo("blog", "d")}})
			objs := isolated(owner.DeepCopy(), c, tc.d)
			if tc.want == LinkDetail {
				// Owning D needs blog in scope.
				for k, o := range objs {
					if n, ok := o.(*corev1.Namespace); ok && n.Name == "blog" {
						objs[k] = testutil.Namespace("blog", map[string]string{"team": "a"})
					}
				}
			}
			e, srv, _ := startWith(t, setup{}, objs...)
			l := linker{e: e, authz: AllowAll{}}
			if got := l.link(t, route("shop", "c")); got != LinkDetail {
				t.Fatalf("candidate C: link %s, want detail", got)
			}
			if got := l.link(t, route("blog", "d")); got != tc.want {
				t.Fatalf("D: link %s, want %s", got, tc.want)
			}
			status := 200
			if tc.want == LinkNone {
				status = 404
			}
			get[map[string]any](t, srv, resourcePath(route("blog", "d")), status)
		})
	}
}

// A referenced candidate needs a referrer the caller may view; denied and
// absent objects get identical answers (D-013).
func TestHiddenReferrerIsNotAdmitted(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	objs := isolated(
		withBackendRefs(testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)), toBackend("blog", "far"), toBackend("blog", "both")),
		withBackendRefs(testutil.HTTPRoute("shop", "cart2", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)), toBackend("blog", "both")),
		testutil.Backend("blog", "far", nil), testutil.Backend("blog", "both", nil), testutil.Backend("blog", "denied", nil),
		withBackendRefs(testutil.HTTPRoute("shop", "api3", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)), toBackend("blog", "denied"), toBackend("blog", "absent")),
	)
	authz := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool {
		return ref != route("shop", "api") && ref != backendRef("blog", "denied")
	})
	e, srv, _ := startWith(t, setup{authz: authz}, objs...)
	l := linker{e: e, authz: authz}
	for _, tc := range []struct {
		ref  objects.Ref
		want Link
	}{
		{backendRef("blog", "far"), LinkNone},  // only referrer hidden
		{backendRef("blog", "both"), LinkYAML}, // shop/cart2 is visible
		{backendRef("blog", "denied"), LinkNone},
		{backendRef("blog", "absent"), LinkNone},
	} {
		if got := l.link(t, tc.ref); got != tc.want {
			t.Errorf("%s: link %s, want %s", tc.ref, got, tc.want)
		}
	}
	get[map[string]any](t, srv, resourcePath(backendRef("blog", "both")), 200)
	ds, db := raw(t, srv, resourcePath(backendRef("blog", "denied")))
	as, ab := raw(t, srv, resourcePath(backendRef("blog", "absent")))
	fs, fb := raw(t, srv, resourcePath(backendRef("blog", "far")))
	if ds != 404 || as != ds || fs != ds || !bytes.Equal(db, ab) || !bytes.Equal(fb, ab) {
		t.Fatalf("denied %d %s, absent %d %s, hidden referrer %d %s: want identical 404s", ds, db, as, ab, fs, fb)
	}
}

// A KGWUI-011 detail names the parent route; a caller who may view the
// route but not the parent gets the finding without that name (D-013,
// design §7.4 rule 7).
func TestFindingEvidenceOnHiddenParentIsRestricted(t *testing.T) {
	gw, api := testutil.GwRef("infra", "edge"), testutil.HTTPRouteRef("shop", "api")
	objs := isolated(testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.HTTPRoute("shop", "c", []objects.ParentRef{api}))
	finding := func(h HealthResponse) FindingDTO {
		t.Helper()
		fs := findingsOn(h, "shop/c")
		if len(fs) != 1 || fs[0].Code != health.CodeDelegatedNotAttached {
			t.Fatalf("want one KGWUI-011 on shop/c: %+v", fs)
		}
		return fs[0]
	}
	_, all, _ := startWith(t, setup{}, objs...)
	if f := finding(get[HealthResponse](t, all, base+"/health", 200)); f.EvidenceRestricted || !strings.Contains(f.Detail, "shop/api") {
		t.Fatalf("allow-all keeps the detail: %+v", f)
	}
	authz := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != route("shop", "api") })
	_, some, _ := startWith(t, setup{authz: authz}, objs...)
	h := get[HealthResponse](t, some, base+"/health", 200)
	f := finding(h)
	if !f.EvidenceRestricted || f.Restricted || f.Detail != restrictedEvidence || f.Message == "" || f.Resource.Link != LinkDetail {
		t.Fatalf("want the finding with its detail replaced: %+v", f)
	}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "api") {
		t.Fatalf("the finding names the hidden parent: %s", b)
	}
}
