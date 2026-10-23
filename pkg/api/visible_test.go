package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// countingAuthz counts the calls per ref. With all set it claims to admit
// every ref, as AllowAll does, so the API must never call it.
type countingAuthz struct {
	mu    sync.Mutex
	all   bool
	deny  func(objects.Ref) bool
	asked map[objects.Ref]int
}

func (a *countingAuthz) allowsAll() bool { return a.all }

func (a *countingAuthz) Authorize(_ *http.Request, ref objects.Ref) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.asked == nil {
		a.asked = map[objects.Ref]int{}
	}
	a.asked[ref]++
	return a.deny == nil || !a.deny(ref)
}

// take returns the calls since the last take and resets them.
func (a *countingAuthz) take() map[objects.Ref]int {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.asked
	a.asked = nil
	return out
}

// hiddenSome denies the routes shop/secret, shop/child and shop/declared,
// the ListenerSet infra/ls1 and the Backend shop/be-x.
func hiddenSome(ref objects.Ref) bool {
	route := ref.Kind == "HTTPRoute" && ref.Namespace == "shop" && slices.Contains([]string{"secret", "child", "declared"}, ref.Name)
	return route || ref.Kind == "ListenerSet" && ref.Name == "ls1" || ref.Kind == "Backend" && ref.Name == "be-x"
}

// visWorld is explorerWorld with the probe routes and the Backend shop/be-x.
func visWorld() []runtime.Object {
	return explorerWorld(append(probeRoutes(), testutil.Backend("shop", "be-x", nil))...)
}

// endpoints are one request of each kind over visWorld.
var endpoints = []string{
	"/api/v1/installs", base + "/overview", base + "/health",
	base + "/routes", base + "/routes?limit=200", base + "/routes?q=shop&sort=health.desc",
	base + "/routes?parent=gateway.networking.k8s.io/Gateway/infra/edge&attachment=accepted",
	base + "/routes?backend=gateway.kgateway.dev/Backend/shop/be-x",
	base + "/gateways", base + "/gateways?sort=routes.desc", base + "/listenersets",
	gwDetail, lsDetail + "infra/ls1", routeDetail + "shop/api", routeDetail + "shop/backends", routeDetail + "shop/secret",
	base + "/resources/gateway.networking.k8s.io/HTTPRoute/shop/cart",
}

// Rule 10: an Authorizer that admits everything is never called, and the
// answers are AllowAll's byte for byte.
func TestAllowAllMakesNoAuthorizerCalls(t *testing.T) {
	a := &countingAuthz{all: true}
	_, srv, _ := startWith(t, setup{authz: a}, visWorld()...)
	_, ref, _ := startWith(t, setup{}, visWorld()...)
	for _, p := range endpoints {
		if got, want := wire(t, srv, p), wire(t, ref, p); !bytes.Equal(got, want) {
			t.Errorf("GET %s differs from AllowAll:\n%s\n%s", p, got, want)
		}
		if n := len(a.take()); n != 0 {
			t.Errorf("GET %s asked the Authorizer about %d refs", p, n)
		}
	}
}

// One view per request: every ref is asked about at most once, whatever
// the number of rows, counts and links that name it.
func TestViewAsksEachRefOnce(t *testing.T) {
	a := &countingAuthz{deny: hiddenSome}
	_, srv, _ := startWith(t, setup{authz: a}, visWorld()...)
	for _, p := range endpoints {
		wire(t, srv, p)
		asked, calls := a.take(), 0
		for ref, n := range asked {
			calls += n
			if n > 1 {
				t.Errorf("GET %s asked about %s %s %d times", p, ref.Kind, ref, n)
			}
		}
		t.Logf("GET %s: %d Authorizer calls", p, calls)
	}
}

// Rule 2: deniedInInstall takes no filter.
func TestDeniedInInstallIgnoresFilters(t *testing.T) {
	_, srv, _ := startWith(t, setup{authz: AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return !hiddenSome(ref) })}, visWorld()...)
	for _, q := range []string{"", "q=secret", "namespace=shop", "namespace=blog", "state=declared", "parent=gateway.networking.k8s.io/Gateway/infra/edge",
		"parent=gateway.networking.k8s.io/HTTPRoute/shop/api", "backend=gateway.kgateway.dev/Backend/shop/be-x", "limit=1"} {
		if p := get[Page[RouteRow]](t, srv, listBase+"/routes?"+q, 200); p.DeniedInInstall != 3 {
			t.Errorf("routes?%s: deniedInInstall %d, want 3", q, p.DeniedInInstall)
		}
	}
	for _, q := range []string{"", "q=ls1", "namespace=infra", "namespace=nope", "parent=gateway.networking.k8s.io/Gateway/infra/edge"} {
		if p := get[Page[ListenerSetRow]](t, srv, listBase+"/listenersets?"+q, 200); p.DeniedInInstall != 1 || p.Total != 0 {
			t.Errorf("listenersets?%s: deniedInInstall %d, total %d", q, p.DeniedInInstall, p.Total)
		}
	}
	if p := get[Page[GatewayListRow]](t, srv, listBase+"/gateways?q=edge", 200); p.DeniedInInstall != 0 || p.Total != 1 {
		t.Errorf("gateways: deniedInInstall %d, total %d", p.DeniedInInstall, p.Total)
	}
}

// Rules 1 and 5: hidden objects leave rows, totals and every derived count.
func TestDeniedObjectsLeaveRowsAndCounts(t *testing.T) {
	_, srv, _ := startWith(t, setup{authz: AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return !hiddenSome(ref) })}, visWorld()...)
	_, all, _ := startWith(t, setup{}, visWorld()...)
	const rows = listBase + "/routes?limit=200"
	p, full := get[Page[RouteRow]](t, srv, rows, 200), get[Page[RouteRow]](t, all, rows, 200)
	for _, n := range []string{"shop/secret", "shop/child", "shop/declared"} {
		if slices.Contains(routeNames(p), n) || !slices.Contains(routeNames(full), n) {
			t.Errorf("row %s: restricted %v, all %v", n, routeNames(p), routeNames(full))
		}
	}
	if p.Total != full.Total-3 || p.Total != len(p.Items) {
		t.Errorf("total %d, all %d, rows %d", p.Total, full.Total, len(p.Items))
	}
	if a, fa := routeByName(t, p, "shop/api"), routeByName(t, full, "shop/api"); a.Children != 0 || fa.Children != 1 {
		t.Errorf("children %d, all %d", a.Children, fa.Children)
	}
	g, fg := get[Page[GatewayListRow]](t, srv, listBase+"/gateways", 200).Items[0], get[Page[GatewayListRow]](t, all, listBase+"/gateways", 200).Items[0]
	if g.ListenerSets != 0 || fg.ListenerSets != 1 || g.NotAccepted != fg.NotAccepted {
		t.Errorf("gateway row %+v, all %+v", g, fg)
	}
	d, fd := get[GatewayDetail](t, srv, gwDetail, 200).RouteCounts, get[GatewayDetail](t, all, gwDetail, 200).RouteCounts
	// shop/secret and shop/via-ls (through the hidden ls1) leave the accepted count.
	if d.Accepted != fd.Accepted-2 || d.NotAccepted != fd.NotAccepted || d.Pending != fd.Pending || d.Unwatched != fd.Unwatched {
		t.Errorf("routeCounts %+v, all %+v", d, fd)
	}
	r, fr := get[RouteDetail](t, srv, routeDetail+"shop/api", 200), get[RouteDetail](t, all, routeDetail+"shop/api", 200)
	if r.ChildCount != 0 || r.DeclaredCount != 0 || fr.ChildCount != 1 || fr.DeclaredCount != 1 {
		t.Errorf("childCount %d declaredCount %d, all %d %d", r.ChildCount, r.DeclaredCount, fr.ChildCount, fr.DeclaredCount)
	}
}

// Rule 2a: backend= reads visible routes' specs only; a denied and an
// absent backend give identical bodies with only visible routes in total.
func TestBackendFilterCannotProbe(t *testing.T) {
	authz := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return !hiddenSome(ref) })
	_, denied, _ := startWith(t, setup{authz: authz}, visWorld()...)
	_, absent, _ := startWith(t, setup{authz: authz}, explorerWorld(probeRoutes()...)...)
	const path = listBase + "/routes?backend=gateway.kgateway.dev/Backend/shop/be-x"
	a, b := wire(t, denied, path), wire(t, absent, path)
	if !bytes.Equal(a, b) {
		t.Fatalf("denied and absent backends differ:\n%s\n%s", a, b)
	}
	p := get[Page[RouteRow]](t, denied, path, 200)
	if !slices.Equal(routeNames(p), []string{"shop/uses-be"}) || p.Total != 1 {
		t.Errorf("rows %v total %d; the hidden shop/secret names be-x too", routeNames(p), p.Total)
	}
	if u := get[Page[RouteRow]](t, denied, listBase+"/routes", 200); p.DeniedInInstall != u.DeniedInInstall {
		t.Errorf("deniedInInstall %d, unfiltered %d", p.DeniedInInstall, u.DeniedInInstall)
	}
}

// Rule 3: data from another object is sent only when that object is visible.
func TestDereferencedDataIsRestricted(t *testing.T) {
	_, srv, _ := startWith(t, setup{authz: hideMidDeps()}, delegationWorld()...)
	d := get[RouteDetail](t, srv, routeDetail+"shop/mid", 200)
	if be := d.Rules[1].BackendRefs[3]; be.Backend == nil || *be.Backend != (BackendStateDTO{Restricted: true}) {
		t.Errorf("hidden Backend: %+v", be.Backend)
	}
	if dl := d.Rules[0].BackendRefs[0].Delegation; dl.State != DelegationRestricted {
		t.Errorf("hidden name ref: %+v", dl)
	}
	for _, p := range d.Ancestors {
		for _, s := range p.Steps {
			if s.Restricted && s.Ref != nil {
				t.Errorf("a restricted step has an identity: %+v", s)
			}
		}
	}
	hideGW := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Kind != "Gateway" })
	_, gw, _ := startWith(t, setup{authz: hideGW}, explorerWorld()...)
	if p := routeByName(t, get[Page[RouteRow]](t, gw, listBase+"/routes?limit=200", 200), "shop/via-ls").Parents[0]; p.Via != nil || !p.ViaRestricted {
		t.Errorf("via with a hidden Gateway: %+v", p)
	}
}

// Rule 8: what a visible object's own spec and status say is sent as written.
func TestSpecRefsStay(t *testing.T) {
	hideGW := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Kind != "Gateway" && !hiddenSome(ref) })
	_, srv, _ := startWith(t, setup{authz: hideGW}, visWorld()...)
	d := get[RouteDetail](t, srv, routeDetail+"shop/uses-be", 200)
	if len(d.ParentRefs) != 1 || d.ParentRefs[0] != edgeRef {
		t.Errorf("parentRefs %+v", d.ParentRefs)
	}
	if len(d.Parents) != 1 || d.Parents[0].Target.Link != LinkNone || len(d.Parents[0].Conditions) == 0 {
		t.Errorf("status parents %+v", d.Parents)
	}
	if be := d.Rules[0].BackendRefs[0]; be.Ref.Name != "be-x" || be.Ref.Link != LinkNone {
		t.Errorf("backendRef %+v", be.Ref)
	}
	// Status-written numbers of a visible Gateway ignore hidden routes.
	_, some, _ := startWith(t, setup{authz: AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return !hiddenSome(ref) })}, visWorld()...)
	_, all, _ := startWith(t, setup{}, visWorld()...)
	g, fg := get[GatewayDetail](t, some, gwDetail, 200), get[GatewayDetail](t, all, gwDetail, 200)
	if !slices.EqualFunc(g.Listeners, fg.Listeners, func(x, y ListenerDTO) bool {
		return (x.Status == nil) == (y.Status == nil) && (x.Status == nil || x.Status.AttachedRoutes == y.Status.AttachedRoutes)
	}) {
		t.Errorf("listener attachedRoutes changed: %+v vs %+v", g.Listeners, fg.Listeners)
	}
	if r, fr := get[Page[GatewayListRow]](t, some, listBase+"/gateways", 200).Items[0], get[Page[GatewayListRow]](t, all, listBase+"/gateways", 200).Items[0]; r.AttachedRoutes != fr.AttachedRoutes {
		t.Errorf("attachedRoutes %d, all %d", r.AttachedRoutes, fr.AttachedRoutes)
	}
}

// Rule 1: a denied detail object is answered as an absent one, on every
// detail endpoint and the resource endpoint, after one Authorizer call.
func TestDeniedDetailIs404(t *testing.T) {
	a := &countingAuthz{deny: hiddenSome}
	_, srv, _ := startWith(t, setup{authz: a}, visWorld()...)
	for _, pair := range [][2]string{
		{routeDetail + "shop/secret", routeDetail + "shop/absent"},
		{lsDetail + "infra/ls1", lsDetail + "infra/absent"},
		{base + "/resources/gateway.networking.k8s.io/HTTPRoute/shop/secret", base + "/resources/gateway.networking.k8s.io/HTTPRoute/shop/absent"},
		{base + "/resources/gateway.kgateway.dev/Backend/shop/be-x", base + "/resources/gateway.kgateway.dev/Backend/shop/absent"},
	} {
		a.take()
		denied := wire(t, srv, pair[0])
		if asked := a.take(); len(asked) != 1 {
			t.Errorf("GET %s asked about %v; want only the object", pair[0], asked)
		}
		if absent := wire(t, srv, pair[1]); !bytes.Equal(denied, absent) || !bytes.Contains(absent, []byte(`"status": 404`)) {
			t.Errorf("want identical 404s:\n%s\n%s", denied, absent)
		}
	}
}

// newServer serves src through the handler with authz.
func newServer(t *testing.T, src ModelSource, authz Authorizer) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewHandler(src, fstest.MapFS{}, authz))
	t.Cleanup(srv.Close)
	return srv
}

// sinceSource serves the engine's publication with every KGWUI-011's since
// set to at, as a since derived from the parent would be.
type sinceSource struct {
	*engine.Engine
	at time.Time
}

func (s sinceSource) Current() *engine.Published {
	p := *s.Engine.Current()
	m := *p.Model
	m.Installs = slices.Clone(m.Installs)
	for k := range m.Installs {
		h := &m.Installs[k].Health
		h.Findings = slices.Clone(h.Findings)
		for j := range h.Findings {
			if h.Findings[j].Code == health.CodeDelegatedNotAttached {
				h.Findings[j].Since = &s.at
			}
		}
	}
	p.Model = &m
	return &p
}

// Rule 7: a visible finding whose evidence is hidden keeps code, severity,
// message and resource; its detail is replaced, and a since is replaced by
// the resource's own first-seen time, so neither says anything of the parent.
func TestEvidenceRestrictedFinding(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	objs := isolated(testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.HTTPRoute("shop", "c", []objects.ParentRef{apiRef}))
	finding := func(h HealthResponse) FindingDTO {
		t.Helper()
		fs := findingsOn(h, "shop/c")
		if len(fs) != 1 || fs[0].Code != health.CodeDelegatedNotAttached {
			t.Fatalf("want one KGWUI-011 on shop/c: %+v", fs)
		}
		return fs[0]
	}
	hideAPI := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != apiRef.Ref() })
	e, some, _ := startWith(t, setup{authz: hideAPI}, objs...)
	_, all, _ := startWith(t, setup{}, objs...)
	f, full := finding(get[HealthResponse](t, some, base+"/health", 200)), finding(get[HealthResponse](t, all, base+"/health", 200))
	if !f.EvidenceRestricted || f.Restricted || f.Detail != restrictedEvidence || f.Message != full.Message || f.Severity != full.Severity || f.Since != nil || full.Since != nil {
		t.Fatalf("restricted %+v, all %+v", f, full)
	}

	// A since from the parent is replaced by the child's own clock.
	parentSeen := testutil.Now.Add(-time.Minute / 2)
	srv := newServer(t, sinceSource{e, parentSeen}, hideAPI)
	open := newServer(t, sinceSource{e, parentSeen}, nil)
	if f := finding(get[HealthResponse](t, open, base+"/health", 200)); f.Since == nil || !f.Since.Equal(parentSeen) {
		t.Fatalf("allow-all since %v, want %v", f.Since, parentSeen)
	}
	childSeen := testutil.Now.Add(-health.Grace - time.Second).UTC()
	if f := finding(get[HealthResponse](t, srv, base+"/health", 200)); f.Since == nil || !f.Since.Equal(childSeen) || f.Since.Location() != time.UTC {
		t.Fatalf("restricted since %v, want the child's first-seen %v", f.Since, childSeen)
	}
}

// Rule 9: health, row states and counts cover every finding under any Authorizer.
func TestHealthUnchangedUnderRestriction(t *testing.T) {
	_, some, _ := startWith(t, setup{authz: AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return !hiddenSome(ref) })}, visWorld()...)
	_, all, _ := startWith(t, setup{}, visWorld()...)
	h, fh := get[HealthResponse](t, some, base+"/health", 200), get[HealthResponse](t, all, base+"/health", 200)
	if h.State != fh.State || len(h.Findings) != len(fh.Findings) {
		t.Fatalf("state %s/%d, all %s/%d", h.State, len(h.Findings), fh.State, len(fh.Findings))
	}
	for sev, n := range fh.Counts {
		if h.Counts[sev] != n {
			t.Errorf("%s count %d, all %d", sev, h.Counts[sev], n)
		}
	}
	for i, f := range h.Findings {
		if f.Code != fh.Findings[i].Code || f.Severity != fh.Findings[i].Severity {
			t.Errorf("finding %d: %+v vs %+v", i, f, fh.Findings[i])
		}
	}
	if s, fs := get[[]InstallSummary](t, some, "/api/v1/installs", 200), get[[]InstallSummary](t, all, "/api/v1/installs", 200); s[0].Health.State != fs[0].Health.State {
		t.Errorf("install state %s, all %s", s[0].Health.State, fs[0].Health.State)
	}
	rows, full := get[Page[RouteRow]](t, some, listBase+"/routes?limit=200", 200), get[Page[RouteRow]](t, all, listBase+"/routes?limit=200", 200)
	for _, r := range rows.Items {
		if fr := routeByName(t, full, refName(r.Ref)); r.Health != fr.Health || !slices.Equal(r.Flags, fr.Flags) {
			t.Errorf("row %s: %s %v, all %s %v", refName(r.Ref), r.Health, r.Flags, fr.Health, fr.Flags)
		}
	}
	if g, fg := get[GatewayDetail](t, some, gwDetail, 200), get[GatewayDetail](t, all, gwDetail, 200); g.Health != fg.Health {
		t.Errorf("gateway health %s, all %s", g.Health, fg.Health)
	}
}
