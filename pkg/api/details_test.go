package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// Detail paths.
const (
	gwDetail    = base + "/gateways/infra/edge"
	routeDetail = base + "/routes/gateway.networking.k8s.io/HTTPRoute/"
	lsDetail    = base + "/listenersets/gateway.networking.k8s.io/ListenerSet/"
	xlsDetail   = base + "/listenersets/gateway.networking.x-k8s.io/XListenerSet/"
)

var midRef = testutil.HTTPRouteRef("shop", "mid")

// matchPath is a PathPrefix match on p.
func matchPath(p string) map[string]any {
	return map[string]any{"path": map[string]any{"type": "PathPrefix", "value": p}}
}

// midRules are shop/mid's rules: delegation refs of every mode, then the
// values easy to get wrong (filter order, mirror percent and fraction,
// absent and explicit weights, absent port, filters of two types on one
// backendRef, a Backend), then explicit matches.
func midRules() []map[string]any {
	deleg := testutil.Rule(matchPath("/api"))
	deleg["name"] = "deleg"
	deleg["backendRefs"] = []any{testutil.DelegateTo("shop", "child"), testutil.DelegateTo("shop", "declared"), testutil.DelegateTo("shop", "ghost"),
		testutil.DelegateWildcard("shop"), testutil.DelegateLabel("shop", "team")}
	values := testutil.Rule()
	values["filters"] = []any{
		map[string]any{"type": "RequestHeaderModifier", "requestHeaderModifier": map[string]any{"add": []any{map[string]any{"name": "x-a", "value": "1"}}}},
		map[string]any{"type": "RequestMirror", "requestMirror": map[string]any{"backendRef": svcRef("mirror-svc"), "percent": int64(10)}},
		map[string]any{"type": "RequestMirror", "requestMirror": map[string]any{"backendRef": map[string]any{"name": "mirror-frac"}, "fraction": map[string]any{"numerator": int64(1)}}},
		map[string]any{"type": "ExtensionRef", "extensionRef": map[string]any{"group": "gateway.kgateway.dev", "kind": "TrafficPolicy", "name": "tp"}},
	}
	values["backendRefs"] = []any{
		svcRef("cart-svc"),
		map[string]any{"name": "w1", "port": int64(8080), "weight": int64(1), "filters": []any{
			map[string]any{"type": "ResponseHeaderModifier", "responseHeaderModifier": map[string]any{"remove": []any{"x-b"}}},
			mirrorFilter("mirror-svc"),
		}},
		map[string]any{"name": "w0", "weight": int64(0)},
		beRef("be1"),
	}
	exact := testutil.Rule(map[string]any{"headers": []any{map[string]any{"name": "x-env", "value": "prod"}},
		"queryParams": []any{map[string]any{"type": "RegularExpression", "name": "q", "value": ".*"}}, "method": "GET"})
	exact["backendRefs"] = []any{svcRef("cart-svc")}
	return []map[string]any{deleg, values, exact}
}

// delegationWorld is world plus shop/mid, a delegating route attached to
// edge and reported under shop/loop (a cycle) and shop/top1..top5 (more
// paths than the up-chain keeps); shop/child is its reported child,
// shop/declared declares it without status, and the Backend shop/be1 exists.
// The world agrees with views B and C: loop and top1..top5 are
// roots that delegate to mid under /api, so mid has no hostnames; loop is
// also mid's child, so mid → loop → mid is a cycle, and loop's entry under
// mid says RefNotPermitted. mid's ghost, label and declared refs select
// nothing usable, so its entries say ResolvedRefs=False (D-021 item 8).
func delegationWorld(extra ...runtime.Object) []runtime.Object {
	loopRef, midEntry := testutil.HTTPRouteRef("shop", "loop"), unresolvedBy
	parents := []objects.ParentRef{edgeRef, loopRef}
	status := []testutil.RouteParent{midEntry(edgeRef), midEntry(loopRef)}
	cycle := testutil.RouteParent{Ref: midRef, Controller: testutil.Controller, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "RefNotPermitted")}}
	objs := []runtime.Object{
		// The child's match fits mid's /api, so its entry agrees with view B.
		testutil.WithRules(testutil.HTTPRoute("shop", "child", []objects.ParentRef{midRef}, testutil.AcceptedBy(midRef, testutil.Controller)),
			testutil.Rule(matchPath("/api/child"))),
		testutil.HTTPRoute("shop", "declared", []objects.ParentRef{midRef}),
		testutil.WithRules(testutil.HTTPRoute("shop", "loop", []objects.ParentRef{midRef, edgeRef}, cycle, testutil.AcceptedBy(edgeRef, testutil.Controller)),
			delegRule([]string{"/api"}, testutil.DelegateTo("shop", "mid"))),
		testutil.Backend("shop", "be1", nil),
	}
	for _, n := range []string{"top1", "top2", "top3", "top4", "top5"} {
		top := testutil.HTTPRouteRef("shop", n)
		parents, status = append(parents, top), append(status, midEntry(top))
		r := testutil.WithRules(testutil.HTTPRoute("shop", n, []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
			delegRule([]string{"/api"}, testutil.DelegateTo("shop", "mid")))
		if n == "top5" {
			// A root keeps its hostnames; a delegated child may not set any.
			r = testutil.WithHostnames(r, "top.example.com")
		}
		objs = append(objs, r)
	}
	mid := testutil.WithRules(testutil.HTTPRoute("shop", "mid", parents, status...), midRules()...)
	return world(append(append(objs, mid), extra...)...)
}

// unresolvedBy is an accepted entry under p whose delegation refs select
// nothing usable: ResolvedRefs=False/BackendNotFound (D-021 item 8).
func unresolvedBy(p objects.ParentRef) testutil.RouteParent {
	return testutil.RouteParent{Ref: p, Controller: testutil.Controller, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound"),
		testutil.Cond("kgateway.dev/Programmed", metav1.ConditionTrue, "Programmed")}}
}

func refName(r LinkedRef) string { return r.Namespace + "/" + r.Name }

// stepNames renders a path root first; a restricted step is "?".
func stepNames(p AncestorPathDTO) string {
	var out []string
	for _, s := range p.Steps {
		if s.Restricted {
			out = append(out, "?")
			continue
		}
		out = append(out, refName(*s.Ref))
	}
	return strings.Join(out, " > ")
}

func TestRouteDetailDelegation(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, delegationWorld()...)
	d := get[RouteDetail](t, srv, routeDetail+"shop/mid", 200)
	if d.Install != "kgw.kgateway" || d.Ref.Ref != midRef.Ref() || d.Ref.Link != LinkDetail || d.Candidate != nil || d.Generation != 1 || d.RuleSet != "kgateway-2.4" {
		t.Fatalf("header %+v", d)
	}
	if d.Hostnames == nil || len(d.Hostnames) != 0 || len(d.ParentRefs) != 7 || d.ParentRefs[0].ParentRef != edgeRef || d.Annotations.InheritParentMatcher != nil {
		t.Fatalf("hostnames %v parentRefs %v annotations %+v", d.Hostnames, d.ParentRefs, d.Annotations)
	}
	if d.ChildCount != 2 || d.DeclaredCount != 1 {
		t.Errorf("childCount %d (child, loop), declaredCount %d", d.ChildCount, d.DeclaredCount)
	}
	refs := d.Rules[0].BackendRefs
	type want struct {
		mode, ns, name, label, state string
		link                         Link
	}
	for i, w := range []want{
		{"name", "shop", "child", "", "reported", LinkDetail},
		{"name", "shop", "declared", "", "not-reported", LinkDetail}, // a KGWUI-011 candidate
		{"name", "shop", "ghost", "", "not-found", LinkNone},
		{"wildcard", "shop", "", "", "", LinkNone},
		{"label", "shop", "", "team", "", LinkNone},
	} {
		dl := refs[i].Delegation
		if dl == nil || string(dl.Mode) != w.mode || dl.Namespace != w.ns || dl.Name != w.name || dl.Label != w.label || dl.State != w.state || refs[i].Ref.Link != w.link {
			t.Errorf("ref %d: %+v %+v, want %+v", i, refs[i].Ref, dl, w)
		}
		if refs[i].Backend != nil {
			t.Errorf("ref %d: a delegation ref has a backend state", i)
		}
	}
	if r := refs[3].Ref; r.Group != objects.GatewayAPIGroup || r.Kind != "HTTPRoute" || r.Name != "*" {
		t.Errorf("wildcard ref as written: %+v", r)
	}
	if r := refs[4].Ref; r.Group != "delegation.kgateway.dev" || r.Kind != "label" || r.Name != "team" {
		t.Errorf("label ref as written: %+v", r)
	}
	if m := d.Rules[0].Matches; d.Rules[0].Name != "deleg" || len(m) != 1 || *m[0].Path != (PathMatchDTO{"PathPrefix", "/api"}) {
		t.Errorf("delegating rule %+v", d.Rules[0])
	}
	// Up-chain: root first, depth-first in (namespace, name) order; 5 paths kept.
	var paths []string
	for _, p := range d.Ancestors {
		s := stepNames(p)
		if p.Cycle {
			s += " (cycle)"
		}
		paths = append(paths, s)
	}
	wantPaths := []string{"infra/edge > shop/mid", "infra/edge > shop/loop > shop/mid", "shop/mid > shop/loop > shop/mid (cycle)",
		"infra/edge > shop/top1 > shop/mid", "infra/edge > shop/top2 > shop/mid"}
	if !slices.Equal(paths, wantPaths) || !d.AncestorsTruncated {
		t.Errorf("ancestors %q truncated %v, want %q truncated", paths, d.AncestorsTruncated, wantPaths)
	}
	if root := d.Ancestors[0].Steps[0].Ref; root.Kind != "Gateway" || root.Link != LinkDetail {
		t.Errorf("root step %+v", root)
	}
	// A root keeps its hostnames (mid, a child, has none).
	if h := get[RouteDetail](t, srv, routeDetail+"shop/top5", 200).Hostnames; !slices.Equal(h, []string{"top.example.com"}) {
		t.Errorf("top5 hostnames %v", h)
	}
	// The child's page names its reported parent and has no children.
	c := get[RouteDetail](t, srv, routeDetail+"shop/child", 200)
	if c.ChildCount != 0 || c.DeclaredCount != 0 || len(c.Ancestors) == 0 || stepNames(c.Ancestors[0]) != "infra/edge > shop/mid > shop/child" {
		t.Errorf("child detail: counts %d %d, ancestors %+v", c.ChildCount, c.DeclaredCount, c.Ancestors)
	}
}

func TestRouteDetailFreshness(t *testing.T) {
	other := testutil.RouteParent{Ref: edgeRef, Controller: "example.com/other", Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners")}}
	// Generation 2 makes every entry, observed at generation 1, stale.
	r := stale(testutil.HTTPRoute("shop", "fresh", []objects.ParentRef{edgeRef}, other, testutil.AcceptedBy(edgeRef, testutil.Controller)))
	_, srv, _ := startWith(t, setup{}, world(r)...)
	d := get[RouteDetail](t, srv, routeDetail+"shop/fresh", 200)
	if d.Generation != 2 || len(d.Parents) != 2 {
		t.Fatalf("generation %d parents %+v", d.Generation, d.Parents)
	}
	ours, theirs := d.Parents[0], d.Parents[1]
	if !ours.Ours || ours.ControllerName != testutil.Controller || theirs.Ours || theirs.ControllerName != "example.com/other" {
		t.Errorf("this controller's entry first, the other's ours=false: %+v", d.Parents)
	}
	if ours.Target.Ref != edgeRef.Ref() || ours.Target.Link != LinkDetail || ours.ParentRef != edgeRef {
		t.Errorf("target %+v parentRef %+v", ours.Target, ours.ParentRef)
	}
	for _, p := range d.Parents {
		for _, c := range p.Conditions {
			if !c.Stale || c.ObservedGeneration != 1 || c.LastTransitionTime.Location().String() != "UTC" {
				t.Errorf("%s %s: %+v", p.ControllerName, c.Type, c)
			}
		}
	}
	// A current entry is not stale.
	cur := get[RouteDetail](t, srv, routeDetail+"shop/cart", 200)
	if c := cur.Parents[0].Conditions[0]; c.Stale || c.Type != "Accepted" || c.Status != "True" {
		t.Errorf("cart: %+v", c)
	}
}

// listenerEdge replaces edge's listeners with one per value under test.
func listenerEdge(objs []runtime.Object) []runtime.Object {
	for _, o := range objs {
		u, ok := o.(*unstructured.Unstructured)
		if !ok || u.GetKind() != "Gateway" || u.GetName() != "edge" {
			continue
		}
		u.Object["spec"].(map[string]any)["listeners"] = []any{
			map[string]any{"name": "http", "port": int64(8080), "protocol": "HTTP", "allowedRoutes": map[string]any{
				"namespaces": map[string]any{"from": "Selector", "selector": map[string]any{"matchLabels": map[string]any{"team": "a"}}},
				"kinds":      []any{map[string]any{"kind": "HTTPRoute"}}}},
			map[string]any{"name": "https", "port": int64(443), "protocol": "HTTPS", "hostname": "*.example.com",
				"tls": map[string]any{"certificateRefs": []any{map[string]any{"name": "edge-cert"}}}},
			map[string]any{"name": "new", "protocol": "HTTP"},
		}
		u.Object["status"].(map[string]any)["listeners"] = []any{
			map[string]any{"name": "http", "attachedRoutes": int64(3), "supportedKinds": []any{map[string]any{"group": objects.GatewayAPIGroup, "kind": "HTTPRoute"}},
				"conditions": []any{map[string]any{"type": "Accepted", "status": "True", "reason": "Accepted", "message": "", "observedGeneration": int64(1), "lastTransitionTime": testutil.Old.UTC().Format("2006-01-02T15:04:05Z")}}},
			map[string]any{"name": "https", "attachedRoutes": int64(0), "supportedKinds": []any{}, "conditions": []any{}},
		}
		u.Object["status"].(map[string]any)["attachedListenerSets"] = int64(1)
	}
	return settleEdge(objs)
}

// ListenerSets is the total the ListenerSets list returns for the Gateway,
// recounted per caller (rule 5), so the SPA's skeleton has the loaded height.
func TestGatewayDetailListenerSetCount(t *testing.T) {
	const lsList = base + "/listenersets?parent=gateway.networking.k8s.io/Gateway/infra/edge"
	_, all, _ := startWith(t, setup{}, visWorld()...)
	_, some, _ := startWith(t, setup{authz: embedsAllowAll{}}, visWorld()...)
	full := get[Page[ListenerSetRow]](t, all, lsList, 200).Total
	if n := get[GatewayDetail](t, all, gwDetail, 200).ListenerSets; n != full || full == 0 {
		t.Errorf("listenerSets %d, list total %d", n, full)
	}
	// hiddenSome hides infra/ls1, a child of edge.
	vis := get[Page[ListenerSetRow]](t, some, lsList, 200).Total
	if n := get[GatewayDetail](t, some, gwDetail, 200).ListenerSets; n != vis || n != full-1 {
		t.Errorf("restricted listenerSets %d, list total %d, unrestricted %d", n, vis, full)
	}
}

func TestGatewayDetailListeners(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, listenerEdge(explorerWorld())...)
	d := get[GatewayDetail](t, srv, gwDetail, 200)
	if d.Ref.Ref != edgeRef.Ref() || d.Ref.Link != LinkDetail || d.Class != "kgateway" || d.Candidate != nil || d.Generation != 1 ||
		!slices.Equal(d.Addresses, []string{"10.0.0.1"}) || d.AttachedListenerSets == nil || *d.AttachedListenerSets != 1 || len(d.Conditions) != 2 {
		t.Fatalf("header %+v", d)
	}
	if len(d.Listeners) != 3 {
		t.Fatalf("listeners %+v", d.Listeners)
	}
	h, s, n := d.Listeners[0], d.Listeners[1], d.Listeners[2]
	if h.Name != "http" || h.Port != 8080 || h.Hostname != "" || h.TLS != nil || h.AllowedRoutes.Namespaces.From != "Selector" ||
		h.AllowedRoutes.Namespaces.Selector.MatchLabels["team"] != "a" || !slices.Equal(h.AllowedRoutes.Kinds, []objects.GroupKind{{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute"}}) {
		t.Errorf("http %+v", h)
	}
	if h.Status == nil || h.Status.AttachedRoutes != 6 || len(h.Status.SupportedKinds) != 1 || len(h.Status.Conditions) != 1 || h.Status.Conditions[0].Stale {
		t.Errorf("http status %+v", h.Status)
	}
	if s.Hostname != "*.example.com" || s.TLS == nil || s.TLS.Mode != "Terminate" || len(s.TLS.CertificateRefs) != 1 ||
		s.TLS.CertificateRefs[0].Ref != (objects.Ref{Kind: "Secret", Namespace: "infra", Name: "edge-cert"}) || s.TLS.CertificateRefs[0].Link != LinkNone {
		t.Errorf("https %+v", s.TLS)
	}
	if s.AllowedRoutes.Namespaces.From != "Same" || s.AllowedRoutes.Kinds == nil || s.Status == nil || s.Status.SupportedKinds == nil {
		t.Errorf("https defaults %+v %+v", s.AllowedRoutes, s.Status)
	}
	if n.Port != 0 || n.Status != nil {
		t.Errorf("a listener without port or status: %+v", n)
	}
	// routeCounts agree with the routes list totals under the Gateway.
	for _, tc := range []struct {
		a string
		n int
	}{{"accepted", d.RouteCounts.Accepted}, {"not-accepted", d.RouteCounts.NotAccepted}, {"pending", d.RouteCounts.Pending}, {"unwatched", d.RouteCounts.Unwatched}} {
		p := get[Page[RouteRow]](t, srv, listBase+"/routes?parent=gateway.networking.k8s.io/Gateway/infra/edge&attachment="+tc.a, 200)
		if p.Total != tc.n || tc.n == 0 {
			t.Errorf("routeCounts %s %d, list total %d", tc.a, tc.n, p.Total)
		}
	}
	// ListenerSets are paged through the list, never embedded: the detail carries only their count.
	if b := wire(t, srv, gwDetail); bytes.Contains(b, []byte(`"listenerSets": [`)) || bytes.Contains(b, []byte(`"ls1"`)) {
		t.Errorf("the detail embeds ListenerSets:\n%s", b)
	}
}

// A hidden ListenerSet's routes leave the Gateway's routeCounts (rule 3).
func TestGatewayRouteCountsSkipHiddenListenerSets(t *testing.T) {
	hideLS := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Kind != "ListenerSet" })
	_, srv, _ := startWith(t, setup{authz: hideLS}, explorerWorld()...)
	_, all, _ := startWith(t, setup{}, explorerWorld()...)
	got, full := get[GatewayDetail](t, srv, gwDetail, 200).RouteCounts, get[GatewayDetail](t, all, gwDetail, 200).RouteCounts
	if got.Accepted != full.Accepted-1 || got.NotAccepted != full.NotAccepted || got.Pending != full.Pending {
		t.Errorf("hidden ListenerSet: %+v, all %+v", got, full)
	}
}

func lsWith(name, kind string, parent objects.ParentRef, cs []metav1.Condition, ls ...testutil.Listener) *unstructured.Unstructured {
	u := testutil.ListenerSet("infra", name, parent, cs, ls...)
	if kind == "XListenerSet" {
		u.SetAPIVersion("gateway.networking.x-k8s.io/v1alpha1")
		u.SetKind("XListenerSet")
	}
	return u
}

func TestListenerSetDetail(t *testing.T) {
	cond := testutil.Cond
	notValid := lsWith("partial", "ListenerSet", edgeRef, []metav1.Condition{cond("Accepted", metav1.ConditionTrue, "ListenersNotValid"), cond("Programmed", metav1.ConditionTrue, "Programmed")},
		testutil.Listener{Name: "extra", Conds: []metav1.Condition{cond("Accepted", metav1.ConditionTrue, "Accepted"), cond("Programmed", metav1.ConditionFalse, "PortUnavailable")}})
	rejected := lsWith("rejected", "ListenerSet", edgeRef, []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "ListenersNotValid")}, testutil.Listener{Name: "extra"})
	xls := lsWith("xls", "XListenerSet", edgeRef, nil)
	delete(xls.Object, "status")
	orphanRef := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "orphan"}
	orphan := lsWith("orphan", "ListenerSet", testutil.GwRef("infra", "missing"), nil)
	// shop/both is owned through edge and names the orphan, so the orphan is a referenced candidate.
	both := testutil.HTTPRoute("shop", "both", []objects.ParentRef{edgeRef, orphanRef}, testutil.AcceptedBy(edgeRef, testutil.Controller))
	_, srv, _ := startWith(t, setup{}, explorerWorld(notValid, rejected, xls, orphan, both)...)

	p := get[ListenerSetDetail](t, srv, lsDetail+"infra/partial", 200)
	if p.Parent.Ref != edgeRef.Ref() || p.Parent.Link != LinkDetail || !p.ParentFound || p.Candidate != nil || p.ExperimentalHint != "" || p.ParentAllowedListeners != nil {
		t.Errorf("partial %+v", p)
	}
	if len(p.Listeners) != 1 || p.Listeners[0].Status == nil || len(p.Listeners[0].Status.Conditions) != 2 || p.Conditions[0].Reason != "ListenersNotValid" {
		t.Errorf("partial listeners %+v conditions %+v", p.Listeners, p.Conditions)
	}
	r := get[ListenerSetDetail](t, srv, lsDetail+"infra/rejected", 200)
	if len(r.Listeners) != 1 || r.Listeners[0].Status != nil || r.Conditions[0].Status != "False" || r.ParentAllowedListeners != nil {
		t.Errorf("rejected: listener status absent, no allowedListeners (not NotAllowed): %+v", r)
	}
	x := get[ListenerSetDetail](t, srv, xlsDetail+"infra/xls", 200)
	if !strings.Contains(x.ExperimentalHint, "XListenerSet v1alpha1") || !strings.Contains(x.ExperimentalHint, "experimental Gateway API features") || x.Ref.Kind != "XListenerSet" {
		t.Errorf("xls hint %q", x.ExperimentalHint)
	}
	o := get[ListenerSetDetail](t, srv, lsDetail+"infra/orphan", 200)
	if o.ParentFound || o.Parent.Link != LinkNone || o.Parent.Name != "missing" || o.Candidate == nil || o.Candidate.Reason != "referenced" ||
		o.Candidate.By == nil || refName(*o.Candidate.By) != "shop/both" || o.Candidate.By.Link != LinkDetail {
		t.Errorf("orphan %+v candidate %+v", o, o.Candidate)
	}
	if ls := get[ListenerSetDetail](t, srv, lsDetail+"infra/ls1", 200); ls.RouteCounts.Accepted != 1 || ls.Health != "healthy" {
		t.Errorf("ls1 routeCounts %+v", ls.RouteCounts)
	}
	// The kind in the path decides: ls1 is not an XListenerSet.
	getErr(t, srv, xlsDetail+"infra/ls1", 404)
}

// edgeAllowing gives edge spec.allowedListeners when v is non-nil.
func edgeAllowing(v map[string]any, objs []runtime.Object) []runtime.Object {
	for _, o := range objs {
		if u, ok := o.(*unstructured.Unstructured); ok && u.GetKind() == "Gateway" && u.GetName() == "edge" && v != nil {
			u.Object["spec"].(map[string]any)["allowedListeners"] = v
		}
	}
	return objs
}

func TestParentAllowedListeners(t *testing.T) {
	cond := testutil.Cond
	notAllowed := []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "NotAllowed"), cond("Programmed", metav1.ConditionFalse, "NotAllowed")}
	allowed := map[string]any{"namespaces": map[string]any{"from": "Same"}}
	for _, tc := range []struct {
		name   string
		value  map[string]any
		ls     *unstructured.Unstructured
		want   *AllowedListenersDTO
		absent bool
	}{
		{"set", allowed, lsWith("na", "ListenerSet", edgeRef, notAllowed, testutil.Listener{Name: "extra"}), &AllowedListenersDTO{Set: true, Value: allowed}, false},
		{"absent field", nil, lsWith("na", "ListenerSet", edgeRef, notAllowed, testutil.Listener{Name: "extra"}), &AllowedListenersDTO{CRDHint: true}, false},
		{"accepted", nil, lsWith("na", "ListenerSet", edgeRef, nil), nil, true},
		{"stale NotAllowed", nil, stale(lsWith("na", "ListenerSet", edgeRef, notAllowed, testutil.Listener{Name: "extra"})), nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, setup{}, edgeAllowing(tc.value, world(tc.ls))...)
			d := get[ListenerSetDetail](t, srv, lsDetail+"infra/na", 200)
			got := d.ParentAllowedListeners
			if tc.absent != (got == nil) || got != nil && (got.Set != tc.want.Set || got.CRDHint != tc.want.CRDHint || (got.Value == nil) != (tc.want.Value == nil)) {
				t.Fatalf("parentAllowedListeners %+v, want %+v", got, tc.want)
			}
			if got != nil && got.Value != nil && got.Value["namespaces"].(map[string]any)["from"] != "Same" {
				t.Fatalf("value as written: %+v", got.Value)
			}
		})
	}
}

// A NotAllowed ListenerSet whose parent is hidden or unadmitted reads the
// parent as not found and sends nothing of the Gateway's spec (D-013).
func TestListenerSetParentHiddenOrUnadmitted(t *testing.T) {
	cond := testutil.Cond
	notAllowed := []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "NotAllowed"), cond("Programmed", metav1.ConditionFalse, "NotAllowed")}
	secret := map[string]any{"namespaces": map[string]any{"from": "Selector", "selector": map[string]any{"matchLabels": map[string]any{"zone": "secret-zone"}}}}
	foreign := testutil.GwRef("infra", "foreign")
	// infra/away names the foreign Gateway; shop/both makes it a referenced candidate.
	awayRef := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "away"}
	both := testutil.HTTPRoute("shop", "both", []objects.ParentRef{edgeRef, awayRef}, testutil.AcceptedBy(edgeRef, testutil.Controller))
	objs := world(lsWith("na", "ListenerSet", edgeRef, notAllowed, testutil.Listener{Name: "extra"}),
		lsWith("away", "ListenerSet", foreign, notAllowed, testutil.Listener{Name: "extra"}), both)
	for _, o := range objs {
		if u, ok := o.(*unstructured.Unstructured); ok && u.GetKind() == "Gateway" {
			u.Object["spec"].(map[string]any)["allowedListeners"] = secret
		}
	}
	hideEdge := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != edgeRef.Ref() })
	_, srv, _ := startWith(t, setup{authz: hideEdge}, objs...)
	for _, path := range []string{lsDetail + "infra/na", lsDetail + "infra/away"} {
		d := get[ListenerSetDetail](t, srv, path, 200)
		if d.ParentFound || d.Parent.Link != LinkNone || d.ParentAllowedListeners != nil {
			t.Errorf("%s: parentFound %v, parent %+v, allowedListeners %+v", path, d.ParentFound, d.Parent, d.ParentAllowedListeners)
		}
		if b := wire(t, srv, path); bytes.Contains(b, []byte("secret-zone")) || bytes.Contains(b, []byte("parentAllowedListeners")) {
			t.Errorf("%s: the body carries the parent's spec:\n%s", path, b)
		}
	}
	if c := get[ListenerSetDetail](t, srv, lsDetail+"infra/away", 200).Candidate; c == nil || c.Reason != "referenced" {
		t.Errorf("away is a referenced candidate: %+v", c)
	}
	// Under AllowAll the owned ListenerSet's parent opens; the candidate's still does not.
	_, all, _ := startWith(t, setup{}, objs...)
	if d := get[ListenerSetDetail](t, all, lsDetail+"infra/na", 200); !d.ParentFound || d.ParentAllowedListeners == nil || !d.ParentAllowedListeners.Set {
		t.Errorf("visible parent: %+v", d)
	}
	if d := get[ListenerSetDetail](t, all, lsDetail+"infra/away", 200); d.ParentFound || d.ParentAllowedListeners != nil {
		t.Errorf("unadmitted parent: %+v", d)
	}
}

// Explicit matches: [] stays empty on the wire; only omitted matches get the
// CRD default (computed-rules §3.2), so the SPA can tell them apart.
func TestExplicitEmptyMatchesStayEmpty(t *testing.T) {
	r := testutil.WithRules(testutil.HTTPRoute("shop", "empty", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		map[string]any{"matches": []any{}}, map[string]any{})
	_, srv, _ := startWith(t, setup{}, world(r)...)
	d := get[RouteDetail](t, srv, routeDetail+"shop/empty", 200)
	if len(d.Rules) != 2 || d.Rules[0].Matches == nil || len(d.Rules[0].Matches) != 0 || len(d.Rules[1].Matches) != 1 {
		t.Fatalf("explicit vs omitted matches: %+v", d.Rules)
	}
}

func TestRouteDetailValuesEasyToGetWrong(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, delegationWorld()...)
	d := get[RouteDetail](t, srv, routeDetail+"shop/mid", 200)
	r := d.Rules[1]
	// Omitted matches carry the CRD default; filters keep their written order.
	if len(r.Matches) != 1 || *r.Matches[0].Path != (PathMatchDTO{"PathPrefix", "/"}) || r.Name != "" || r.Index != 1 {
		t.Errorf("default match %+v", r.Matches)
	}
	var types []string
	for _, f := range r.Filters {
		types = append(types, f.Type)
	}
	if !slices.Equal(types, []string{"RequestHeaderModifier", "RequestMirror", "RequestMirror", "ExtensionRef"}) {
		t.Errorf("filter order %v", types)
	}
	if f := r.Filters[0]; f.Config["requestHeaderModifier"] == nil || f.Mirror != nil || f.ExtensionRef != nil {
		t.Errorf("config as written: %+v", f)
	}
	m1, m2 := r.Filters[1].Mirror, r.Filters[2].Mirror
	if m1 == nil || m1.BackendRef.Ref != (objects.Ref{Kind: "Service", Namespace: "shop", Name: "mirror-svc"}) || m1.BackendRef.Link != LinkBackend ||
		m1.Port != 80 || m1.Percent == nil || *m1.Percent != 10 || m1.Fraction != nil {
		t.Errorf("mirror percent %+v", m1)
	}
	if m2 == nil || m2.Port != 0 || m2.Percent != nil || m2.Fraction == nil || *m2.Fraction != (FractionDTO{1, 100}) {
		t.Errorf("mirror fraction %+v", m2)
	}
	if e := r.Filters[3].ExtensionRef; e == nil || e.Ref != (objects.Ref{Group: "gateway.kgateway.dev", Kind: "TrafficPolicy", Namespace: "shop", Name: "tp"}) || e.Link != LinkNone {
		t.Errorf("extensionRef %+v", e)
	}
	b := r.BackendRefs
	if b[0].Weight != 1 || !b[0].WeightDefaulted || b[0].Port != 80 || b[0].Backend != nil || b[0].Delegation != nil || b[0].Filters == nil {
		t.Errorf("absent weight %+v", b[0])
	}
	if b[1].Weight != 1 || b[1].WeightDefaulted || b[1].Port != 8080 {
		t.Errorf("explicit weight 1 %+v", b[1])
	}
	if f := b[1].Filters; len(f) != 2 || f[0].Type != "ResponseHeaderModifier" || f[1].Mirror == nil || f[1].Mirror.BackendRef.Name != "mirror-svc" {
		t.Errorf("backendRef filters %+v", f)
	}
	if b[2].Weight != 0 || b[2].WeightDefaulted || b[2].Port != 0 {
		t.Errorf("explicit weight 0, absent port %+v", b[2])
	}
	if be := b[3]; be.Ref.Ref != backendRef("shop", "be1") || be.Ref.Link != LinkBackend || be.Backend == nil || be.Backend.State != "healthy" ||
		be.Backend.Accepted == nil || be.Backend.Accepted.Status != "True" || be.Backend.Restricted {
		t.Errorf("Backend ref %+v %+v", be.Ref, be.Backend)
	}
	// An absent port is absent on the wire, and weightDefaulted only when set.
	raw := wire(t, srv, routeDetail+"shop/mid")
	if bytes.Contains(raw, []byte(`"port": 0`)) || bytes.Contains(raw, []byte(`"weightDefaulted": false`)) {
		t.Errorf("zero port or false weightDefaulted on the wire:\n%s", raw)
	}
	x := d.Rules[2].Matches
	if len(x) != 1 || x[0].Method != "GET" || len(x[0].Headers) != 1 || x[0].Headers[0] != (KeyMatchDTO{"Exact", "x-env", "prod"}) ||
		x[0].QueryParams[0] != (KeyMatchDTO{"RegularExpression", "q", ".*"}) || *x[0].Path != (PathMatchDTO{"PathPrefix", "/"}) {
		t.Errorf("explicit match %+v", x)
	}
}

// farWorld is blog/far, out of scope, with the other controller's entry
// written first; it sends to the Backend blog/be-far and delegates to
// blog/kid, which reports far as its parent.
func farWorld() []runtime.Object {
	foreign, farRef := testutil.GwRef("infra", "foreign"), testutil.HTTPRouteRef("blog", "far")
	far := testutil.WithRules(testutil.HTTPRoute("blog", "far", []objects.ParentRef{foreign}, testutil.AcceptedBy(foreign, "example.com/other"), testutil.AcceptedBy(foreign, testutil.Controller)),
		map[string]any{"backendRefs": []any{toBackend("blog", "be-far"), testutil.DelegateTo("blog", "kid")}})
	return []runtime.Object{far, testutil.Backend("blog", "be-far", nil), testutil.HTTPRoute("blog", "kid", []objects.ParentRef{farRef}, testutil.AcceptedBy(farRef, testutil.Controller))}
}

func TestCandidateDetail(t *testing.T) {
	foreign := testutil.GwRef("infra", "foreign")
	// blog/far is named by shop/mid's delegation ref; far is not watched, so the ref is unresolved.
	mid := testutil.WithRules(testutil.HTTPRoute("shop", "mid", []objects.ParentRef{edgeRef, foreign}, unresolvedBy(edgeRef)),
		map[string]any{"backendRefs": []any{testutil.DelegateTo("blog", "far")}})
	mid2 := testutil.WithRules(testutil.HTTPRoute("shop", "mid2", []objects.ParentRef{edgeRef}, unresolvedBy(edgeRef)),
		map[string]any{"backendRefs": []any{testutil.DelegateTo("blog", "far")}})
	objs := world(append(farWorld(), mid, mid2, testutil.HTTPRoute("shop", "declared", []objects.ParentRef{testutil.HTTPRouteRef("shop", "mid")}))...)
	_, srv, _ := startWith(t, setup{}, objs...)

	d := get[RouteDetail](t, srv, routeDetail+"blog/far", 200)
	if d.Candidate == nil || d.Candidate.Reason != "referenced" || d.Candidate.By == nil || refName(*d.Candidate.By) != "shop/mid" || d.Candidate.By.Link != LinkDetail {
		t.Fatalf("candidate %+v", d.Candidate)
	}
	if len(d.Parents) != 2 || !d.Parents[0].Ours || d.Parents[1].Ours || d.Parents[0].Target.Link != LinkDetail {
		t.Errorf("this controller's entries first: %+v", d.Parents)
	}
	if len(d.Ancestors) != 0 || d.ChildCount != 0 {
		t.Errorf("a candidate has no reported edges: %+v", d)
	}
	// Admission does not follow chains: far's Backend does not open, so it has
	// no state, and its child reads not-reported (edges are for owned parents).
	if refs := d.Rules[0].BackendRefs; len(refs) != 2 || refs[0].Ref.Ref != backendRef("blog", "be-far") || refs[0].Ref.Link != LinkNone || refs[0].Backend != nil ||
		refs[1].Delegation == nil || refs[1].Delegation.State != "not-reported" || refs[1].Ref.Link != LinkNone {
		t.Errorf("candidate rules %+v", d.Rules)
	}
	// A KGWUI-011 declared route is a finding candidate, without by.
	if c := get[RouteDetail](t, srv, routeDetail+"shop/declared", 200).Candidate; c == nil || c.Reason != "finding" || c.By != nil {
		t.Errorf("declared candidate %+v", c)
	}
	// A Gateway named by an owned route's parentRef.
	if g := get[GatewayDetail](t, srv, base+"/gateways/infra/foreign", 200); g.Candidate == nil || g.Candidate.Reason != "referenced" || refName(*g.Candidate.By) != "shop/mid" || g.Class != "other" {
		t.Errorf("foreign Gateway %+v", g)
	}
	// by is the first visible referrer; with none visible the candidate is a 404.
	hideMid := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != route("shop", "mid") })
	_, some, _ := startWith(t, setup{authz: hideMid}, objs...)
	if c := get[RouteDetail](t, some, routeDetail+"blog/far", 200).Candidate; c == nil || refName(*c.By) != "shop/mid2" {
		t.Errorf("by under a restricting Authorizer: %+v", c)
	}
	getErr(t, some, base+"/gateways/infra/foreign", 404)
}

// Spec parentRefs link through admission like status targets (design §7.1): a
// declared candidate without status reaches its visible owned parent, and a
// hidden parent links none.
func TestSpecParentRefsAreLinked(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, delegationWorld()...)
	d := get[RouteDetail](t, srv, routeDetail+"shop/declared", 200)
	if len(d.Parents) != 0 || len(d.ParentRefs) != 1 || d.ParentRefs[0].ParentRef != midRef || d.ParentRefs[0].Target != (LinkedRef{Ref: midRef.Ref(), Link: LinkDetail}) {
		t.Fatalf("declared candidate parentRefs %+v, parents %+v", d.ParentRefs, d.Parents)
	}

	hideMid := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != route("shop", "mid") })
	_, some, _ := startWith(t, setup{authz: hideMid}, delegationWorld()...)
	h := get[RouteDetail](t, some, routeDetail+"shop/declared", 200)
	if len(h.ParentRefs) != 1 || h.ParentRefs[0].Target != (LinkedRef{Ref: midRef.Ref(), Link: LinkNone}) {
		t.Errorf("hidden parent %+v", h.ParentRefs)
	}
	// Every spec parent of an owned route links: the edge Gateway, then routes.
	for _, p := range get[RouteDetail](t, srv, routeDetail+"shop/mid", 200).ParentRefs {
		if p.Target.Ref != p.Ref() || p.Target.Link != LinkDetail {
			t.Errorf("mid parentRef %+v", p)
		}
	}
}

func TestOtherKindDetailIs404(t *testing.T) {
	// edge's HTTP listener does not admit a TCPRoute; its detail still opens.
	st := testutil.AcceptedBy(edgeRef, testutil.Controller)
	st.Conds[0] = testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners")
	_, srv, _ := startWith(t, setup{}, world(tcpRoute("v1alpha2", "shop", "tcp", []objects.ParentRef{edgeRef}, st))...)
	for _, p := range []string{
		base + "/routes/gateway.networking.k8s.io/UDPRoute/shop/tcp",
		base + "/routes/gateway.kgateway.dev/TCPRoute/shop/tcp",
		base + "/routes/gateway.networking.k8s.io/Gateway/infra/edge",
		base + "/listenersets/gateway.networking.k8s.io/Gateway/infra/edge",
	} {
		if e := getErr(t, srv, p, 404); e.Error != "no detail view for this kind" {
			t.Errorf("%s: %q", p, e.Error)
		}
	}
	get[RouteDetail](t, srv, base+"/routes/gateway.networking.k8s.io/TCPRoute/shop/tcp", 200)
	if e := getErr(t, srv, base+"/routes/gateway.networking.k8s.io/GRPCRoute/shop/cart", 404); e.Error != notFoundInScope {
		t.Errorf("absent GRPCRoute: %q", e.Error)
	}
	get[map[string]any](t, srv, resourcePath(objects.Ref{Group: objects.GatewayAPIGroup, Kind: "TCPRoute", Namespace: "shop", Name: "tcp"}), 200)
	for _, l := range []string{"/gateways/infra/edge", "/routes/gateway.networking.k8s.io/HTTPRoute/shop/cart", "/listenersets/gateway.networking.k8s.io/ListenerSet/infra/x"} {
		getErr(t, srv, "/api/v1/installs/nope"+l, 404)
	}
}

func TestMetaViews(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, world()...)
	m := get[MetaResponse](t, srv, "/api/v1/meta", 200)
	want := []objects.GroupKind{{Group: objects.GatewayAPIGroup, Kind: "GRPCRoute"}, {Group: objects.GatewayAPIGroup, Kind: "Gateway"},
		{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute"}, {Group: objects.GatewayAPIGroup, Kind: "ListenerSet"},
		{Group: objects.GatewayAPIGroup, Kind: "TCPRoute"}, {Group: objects.GatewayAPIGroup, Kind: "TLSRoute"}, {Group: "gateway.networking.x-k8s.io", Kind: "XListenerSet"}}
	got := slices.Clone(m.Views)
	slices.SortFunc(got, func(a, b objects.GroupKind) int { return strings.Compare(a.Group+a.Kind, b.Group+b.Kind) })
	if !slices.Equal(got, want) {
		t.Fatalf("views %v, want %v", m.Views, want)
	}
}

// recordingAuthz denies deny and records every ref it is asked about.
type recordingAuthz struct {
	mu    sync.Mutex
	deny  func(objects.Ref) bool
	asked []objects.Ref
}

func (a *recordingAuthz) Authorize(_ *http.Request, ref objects.Ref) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, ref)
	return !a.deny(ref)
}

// A denied detail object and an absent one get identical answers, and the
// handler stops before reading anything about a denied route (D-013).
func TestDeniedDetailEqualsAbsent(t *testing.T) {
	hidden := func(ref objects.Ref) bool {
		return ref == midRef.Ref() || ref == edgeRef.Ref() || ref.Kind == "ListenerSet" && ref.Name == "ls1"
	}
	a := &recordingAuthz{deny: hidden}
	_, srv, _ := startWith(t, setup{authz: a}, delegationWorld(testutil.ListenerSet("infra", "ls1", edgeRef, nil))...)
	for _, pair := range [][2]string{
		{routeDetail + "shop/mid", routeDetail + "shop/absent"},
		{gwDetail, base + "/gateways/infra/absent"},
		{lsDetail + "infra/ls1", lsDetail + "infra/absent"},
		// Unadmitted and existing: the other class's Gateway.
		{gwDetail, base + "/gateways/infra/foreign"},
	} {
		denied, absent := wire(t, srv, pair[0]), wire(t, srv, pair[1])
		if !bytes.Equal(denied, absent) || !bytes.Contains(absent, []byte(`"status": 404`)) {
			t.Errorf("want identical 404s:\n%s\n%s", denied, absent)
		}
	}
	a.mu.Lock()
	a.asked = nil
	a.mu.Unlock()
	getErr(t, srv, routeDetail+"shop/mid", 404)
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.asked) != 1 || a.asked[0] != midRef.Ref() {
		t.Errorf("a denied route's detail asks about %v; want only the route", a.asked)
	}
}

// hideMidDeps denies shop/top1..top4, the Backend shop/be1 and shop/child.
func hideMidDeps() Authorizer {
	return AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool {
		top := ref.Kind == "HTTPRoute" && ref.Namespace == "shop" && slices.Contains([]string{"top1", "top2", "top3", "top4"}, ref.Name)
		return !top && ref != backendRef("shop", "be1") && ref != route("shop", "child")
	})
}

// Dereferenced data is restricted on a visible route's page (rule 3).
func TestRouteDetailRestricted(t *testing.T) {
	deny := hideMidDeps()
	_, srv, _ := startWith(t, setup{authz: deny}, delegationWorld()...)
	d := get[RouteDetail](t, srv, routeDetail+"shop/mid", 200)
	if dl := d.Rules[0].BackendRefs[0]; dl.Delegation.State != "restricted" || dl.Ref.Link != LinkNone || dl.Ref.Name != "child" {
		t.Errorf("hidden name ref: %+v %+v", dl.Ref, dl.Delegation)
	}
	if be := d.Rules[1].BackendRefs[3]; be.Backend == nil || !be.Backend.Restricted || be.Backend.State != "" || be.Backend.Accepted != nil || be.Ref.Link != LinkNone {
		t.Errorf("hidden Backend: %+v", be.Backend)
	}
	if d.ChildCount != 1 {
		t.Errorf("childCount %d counts the hidden child", d.ChildCount)
	}
	// The child-route list counts what childCount counts, hidden child excluded.
	if kids := get[Page[RouteRow]](t, srv, base+"/routes?parent=gateway.networking.k8s.io/HTTPRoute/shop/mid", 200); kids.Total != d.ChildCount || len(kids.Items) != d.ChildCount {
		t.Errorf("list under mid: total %d rows %d, childCount %d", kids.Total, len(kids.Items), d.ChildCount)
	}
	// Hidden parent routes come last, named with link none (D-024); the
	// walk stops at them, and the five-path bound truncates the rest.
	var got []string
	for _, p := range d.Ancestors {
		got = append(got, stepNames(p))
		for _, s := range p.Steps {
			hidden := strings.HasPrefix(s.Ref.Name, "top") && s.Ref.Name != "top5"
			if s.Restricted || (s.Ref.Link == LinkNone) != hidden {
				t.Errorf("step %+v", s)
			}
		}
	}
	want := []string{"infra/edge > shop/mid", "infra/edge > shop/loop > shop/mid", "shop/mid > shop/loop > shop/mid", "infra/edge > shop/top5 > shop/mid", "shop/top1 > shop/mid"}
	if !slices.Equal(got, want) || !d.AncestorsTruncated {
		t.Errorf("ancestors %q truncated %v", got, d.AncestorsTruncated)
	}
	// The hidden top1 shows only the match it delegated mid under (rule 3, D-024).
	if s := d.Ancestors[4].Steps[0]; len(s.Matches) != 1 || *s.Matches[0].Path != (PathMatchDTO{"PathPrefix", "/api"}) {
		t.Errorf("hidden ancestor step %+v", s)
	}
}

// Trace fixture routes (design §7).
var (
	grpcDetail = base + "/routes/gateway.networking.k8s.io/GRPCRoute/shop/grpc"
	tcpDetail  = base + "/routes/gateway.networking.k8s.io/TCPRoute/shop/tcp"
	tlsDetail  = base + "/routes/gateway.networking.k8s.io/TLSRoute/shop/tls"
	otherGwRef = testutil.GwRef("infra", "other")
)

// traceWorld is edge with an http, a tcp and a tls listener, and infra/other
// whose listener web is not programmed. shop/grpc attaches to edge's http and
// to other: a method and header match sending 1:3 to cart-svc and other-svc.
// shop/tcp sends 2:0 to the same Services; shop/tls sends to cart-svc.
// cart-svc has one ready endpoint on port 80; other-svc is not in the cache.
func traceWorld() []runtime.Object {
	tcp, tls := lst("tcp", 9000, ""), lst("tls", 443, "")
	tcp.Protocol, tls.Protocol = "TCP", "TLS"
	gw := edgeWith([]objects.Listener{lst("http", 80, ""), tcp, tls}, up("http", 1), up("tcp", 1), up("tls", 1))
	other := testutil.GatewayWithListeners("infra", "other", "kgateway", []objects.Listener{lst("web", 8080, "")}, testutil.Listener{Name: "web", Attached: 1,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionFalse, "Invalid")}})
	ref := func(name string, w int64) map[string]any {
		return map[string]any{"name": name, "port": int64(80), "weight": w}
	}
	kind := func(k, name string, parents []objects.ParentRef, rules ...map[string]any) *unstructured.Unstructured {
		st := make([]testutil.RouteParent, 0, len(parents))
		for _, p := range parents {
			st = append(st, testutil.AcceptedBy(p, testutil.Controller))
		}
		return testutil.WithRules(asKind(k, testutil.HTTPRoute("shop", name, parents, st...)), rules...)
	}
	grpcRule := testutil.Rule(map[string]any{"method": map[string]any{"service": "cart.Cart", "method": "Get"},
		"headers": []any{map[string]any{"name": "x-env", "value": "prod"}}})
	grpcRule["backendRefs"] = []any{ref("cart-svc", 1), ref("other-svc", 3)}
	grpc := kind("GRPCRoute", "grpc", []objects.ParentRef{sectionRef("http"), otherGwRef}, grpcRule)
	tcpRt := kind("TCPRoute", "tcp", []objects.ParentRef{sectionRef("tcp")}, map[string]any{"backendRefs": []any{ref("cart-svc", 2), ref("other-svc", 0)}})
	tlsRt := testutil.WithHostnames(kind("TLSRoute", "tls", []objects.ParentRef{sectionRef("tls")}, map[string]any{"backendRefs": []any{svcRef("cart-svc")}}), "tls.example.com")
	name, port, ready := "http", int32(80), true
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "cart-svc", UID: "svc-cart-svc"},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Ports: []corev1.ServicePort{{Name: name, Port: port}}}}
	es := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "cart-svc-x", UID: "es-cart-svc",
		Labels: map[string]string{discoveryv1.LabelServiceName: "cart-svc"}}, AddressType: discoveryv1.AddressTypeIPv4,
		Ports:     []discoveryv1.EndpointPort{{Name: &name, Port: &port}},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}}}}
	return viewAWorld(gw, other, grpc, tcpRt, tlsRt, svc, es)
}

// traceCases are the route detail goldens of the other route kinds (design §7.2).
func traceCases(t *testing.T) []goldenCase {
	_, srv, _ := startWith(t, setup{endpoints: true}, traceWorld()...)
	return []goldenCase{
		{"route-detail-grpc", srv, grpcDetail},
		{"route-detail-tcp", srv, tcpDetail},
		{"route-detail-tls", srv, tlsDetail},
	}
}

// TestGoldenRouteTrace pins the GRPCRoute, TCPRoute and TLSRoute detail bodies.
func TestGoldenRouteTrace(t *testing.T) { runGolden(t, traceCases(t)) }

// GRPCRoute detail: its method and header match, shares of its 1:3 rule, and
// each ref's problem the worst across its two attachments, naming the one it
// came from (design §4.4, §4.5, §7).
func TestGRPCRouteDetail(t *testing.T) {
	_, srv, _ := startWith(t, setup{endpoints: true}, traceWorld()...)
	d := get[RouteDetail](t, srv, grpcDetail, 200)
	if d.Ref.Kind != "GRPCRoute" || len(d.Rules) != 1 || len(d.ParentRefs) != 2 || len(d.Parents) != 2 {
		t.Fatalf("detail %+v", d)
	}
	m := d.Rules[0].Matches
	if len(m) != 1 || m[0].GRPCMethod == nil || *m[0].GRPCMethod != (GRPCMethodDTO{"Exact", "cart.Cart", "Get"}) || m[0].Method != "" || m[0].Path != nil ||
		len(m[0].Headers) != 1 || m[0].Headers[0] != (KeyMatchDTO{"Exact", "x-env", "prod"}) {
		t.Errorf("gRPC match %+v", m)
	}
	refs := d.Rules[0].BackendRefs
	if len(refs) != 2 || refs[0].Share == nil || *refs[0].Share != 25 || refs[1].Share == nil || *refs[1].Share != 75 ||
		refs[0].ShareState != "shown" || refs[1].ShareState != "shown" || refs[0].Weight != 1 || refs[1].Weight != 3 {
		t.Fatalf("shares %+v", refs)
	}
	// edge serves healthy; other's listener is not programmed.
	if len(d.Serves) != 2 || d.Serves[0].Gateway.Name != "edge" || d.Serves[0].Listener != (PathListenerDTO{"http", "HTTP", 80}) ||
		d.Serves[0].State != health.StateHealthy || d.Serves[1].Gateway.Name != "other" || d.Serves[1].State != health.StateError {
		t.Fatalf("serves %+v", d.Serves)
	}
	p := refs[0].Problem
	if p == nil || p.State != health.StateError {
		t.Fatalf("cart-svc problem %+v", p)
	}
	var named bool
	for _, s := range p.Signals {
		if s.Kind == "listener" && s.Code == "Programmed" {
			named = s.Attachment != nil && *s.Attachment == 1
		}
		if s.Attachment != nil && *s.Attachment == 0 {
			t.Errorf("healthy edge attachment named by %+v", s)
		}
	}
	if !named {
		t.Errorf("listener signal does not name other's attachment: %+v", p.Signals)
	}
	if e := refs[0].Endpoints; e == nil || e.Complete != "complete" || e.Ready == nil || *e.Ready != 1 {
		t.Errorf("cart-svc endpoints %+v", e)
	}
	// Without other, only edge serves and cart-svc is healthy.
	hideOther := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != otherGwRef.Ref() })
	_, some, _ := startWith(t, setup{endpoints: true, authz: hideOther}, traceWorld()...)
	h := get[RouteDetail](t, some, grpcDetail, 200)
	if len(h.Serves) != 1 || h.Serves[0].Gateway.Name != "edge" || h.Rules[0].BackendRefs[0].Problem == nil || h.Rules[0].BackendRefs[0].Problem.State != health.StateHealthy {
		t.Errorf("hidden other: serves %+v, cart-svc %+v", h.Serves, h.Rules[0].BackendRefs[0].Problem)
	}
}

// TCPRoute and TLSRoute detail: no matches ([] on the wire), ports, weights and shares.
func TestTCPAndTLSRouteDetail(t *testing.T) {
	_, srv, _ := startWith(t, setup{endpoints: true}, traceWorld()...)
	tcp := get[RouteDetail](t, srv, tcpDetail, 200)
	r := tcp.Rules[0]
	if r.Matches == nil || len(r.Matches) != 0 || len(r.BackendRefs) != 2 || r.BackendRefs[0].Port != 80 || r.BackendRefs[0].Weight != 2 ||
		r.BackendRefs[1].Weight != 0 || *r.BackendRefs[0].Share != 100 || r.BackendRefs[1].ShareState != "no-traffic" || *r.BackendRefs[1].Share != 0 {
		t.Errorf("tcp rule %+v", r)
	}
	if len(tcp.Serves) != 1 || tcp.Serves[0].Listener != (PathListenerDTO{"tcp", "TCP", 9000}) || len(tcp.Serves[0].Hostnames) != 0 || tcp.Serves[0].Hostnames == nil {
		t.Errorf("tcp serves %+v", tcp.Serves)
	}
	tls := get[RouteDetail](t, srv, tlsDetail, 200)
	if r := tls.Rules[0]; r.Matches == nil || len(r.Matches) != 0 || len(r.BackendRefs) != 1 || *r.BackendRefs[0].Share != 100 {
		t.Errorf("tls rule %+v", r)
	}
	if !slices.Equal(tls.Hostnames, []string{"tls.example.com"}) || len(tls.Serves) != 1 || tls.Serves[0].Listener.Name != "tls" {
		t.Errorf("tls hostnames %v, serves %+v", tls.Hostnames, tls.Serves)
	}
	for _, p := range []string{tcpDetail, tlsDetail} {
		if raw := wire(t, srv, p); !bytes.Contains(raw, []byte(`"matches": []`)) {
			t.Errorf("%s: matches not [] on the wire:\n%s", p, raw)
		}
	}
}

// Every route kind with a detail view links to it from the routes list.
func TestRouteKindsLinkToDetail(t *testing.T) {
	_, srv, _ := startWith(t, setup{endpoints: true}, traceWorld()...)
	pg := get[Page[RouteRow]](t, srv, listBase+"/routes", 200)
	for _, n := range []string{"shop/grpc", "shop/tcp", "shop/tls"} {
		if r := routeByName(t, pg, n); r.Ref.Link != LinkDetail {
			t.Errorf("%s links %q", n, r.Ref.Link)
		}
	}
}

// HTTPRoute detail keeps every field as it was: rules and backendRefs gain
// only the trace keys, and no body sets effectiveFilters or grpcMethod.
func TestRouteDetailTraceKeysOnly(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, delegationWorld()...)
	var body map[string]any
	if err := json.Unmarshal(wire(t, srv, routeDetail+"shop/mid"), &body); err != nil {
		t.Fatal(err)
	}
	top := []string{"install", "ref", "generation", "health", "reason", "ruleSet", "ruleSetLabel", "hostnames", "annotations", "parentRefs", "parents",
		"attachments", "serves", "rules", "effective", "effectiveTruncated", "ancestors", "ancestorsTruncated", "childCount", "declaredCount", "collectedAt"}
	ref := []string{"ref", "port", "weight", "weightDefaulted", "filters", "delegation", "backend", "share", "shareState", "problem", "endpoints"}
	only := func(what string, m map[string]any, keys []string) {
		for k := range m {
			if !slices.Contains(keys, k) {
				t.Errorf("%s gained %q", what, k)
			}
		}
	}
	b := body["body"].(map[string]any)
	only("route", b, top)
	for _, r := range b["rules"].([]any) {
		only("rule", r.(map[string]any), []string{"index", "name", "matches", "filters", "backendRefs"})
		for _, br := range r.(map[string]any)["backendRefs"].([]any) {
			only("backendRef", br.(map[string]any), ref)
		}
	}
	_, trace, _ := startWith(t, setup{endpoints: true}, traceWorld()...)
	for _, p := range []string{routeDetail + "shop/mid", grpcDetail, tcpDetail, tlsDetail} {
		s := srv
		if p != routeDetail+"shop/mid" {
			s = trace
		}
		raw := wire(t, s, p)
		if bytes.Contains(raw, []byte(`"effectiveFilters"`)) || p != grpcDetail && bytes.Contains(raw, []byte(`"grpcMethod"`)) {
			t.Errorf("%s: reserved or gRPC key:\n%s", p, raw)
		}
	}
}

// serves keeps the path model's attachment order, which signal attachment
// indexes point into; shop/mid's seven root attachments on edge/http merge
// into one entry per hostname list, the worst state winning.
func TestServesOrderAndMerge(t *testing.T) {
	_, srv, _ := startWith(t, setup{endpoints: true}, traceWorld()...)
	g := get[RouteDetail](t, srv, grpcDetail, 200)
	for _, b := range g.Rules[0].BackendRefs {
		for _, s := range b.Problem.Signals {
			if s.Attachment != nil && s.Ref != nil && s.Ref.Kind == "Gateway" && g.Serves[*s.Attachment].Gateway.Ref != s.Ref.Ref {
				t.Errorf("signal %+v names serves %d, %+v", s, *s.Attachment, g.Serves[*s.Attachment])
			}
		}
	}
	_, deleg, _ := startWith(t, setup{}, delegationWorld()...)
	m := get[RouteDetail](t, deleg, routeDetail+"shop/mid", 200)
	if len(m.Serves) != 2 || len(m.Serves[0].Hostnames) != 0 || m.Serves[0].State != health.StateError ||
		!slices.Equal(m.Serves[1].Hostnames, []string{"top.example.com"}) || m.Serves[1].State != health.StateHealthy {
		t.Errorf("merged serves %+v", m.Serves)
	}
}
