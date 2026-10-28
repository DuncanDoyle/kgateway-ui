package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

const (
	edgeParent  = "parent=gateway.networking.k8s.io/Gateway/infra/edge"
	underEdge   = listBase + "/routes?" + edgeParent
	unknownNote = "Controller version unknown; using the 2.4 rules"
)

// lst is a plain HTTP listener admitting routes from every namespace.
func lst(name string, port int32, hostname string) objects.Listener {
	return objects.Listener{Name: name, Port: port, Protocol: "HTTP", Hostname: hostname,
		AllowedRoutes: objects.AllowedRoutes{Namespaces: objects.RouteNamespaces{From: "All"}}}
}

// up is a current, accepted listener status entry reporting attached routes.
func up(name string, attached int32) testutil.Listener {
	return testutil.Listener{Name: name, Attached: attached, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}}
}

// edgeWith is infra/edge with the given spec listeners and status entries.
func edgeWith(spec []objects.Listener, status ...testutil.Listener) *unstructured.Unstructured {
	return testutil.GatewayWithListeners("infra", "edge", "kgateway", spec, status...)
}

// viewAWorld is world with gw as infra/edge, without shop/cart, plus extra.
func viewAWorld(gw *unstructured.Unstructured, extra ...runtime.Object) []runtime.Object {
	var out []runtime.Object
	for _, o := range world() {
		if u, ok := o.(*unstructured.Unstructured); ok && u.GetNamespace() != "blog" {
			switch {
			case u.GetKind() == "Gateway" && u.GetName() == "edge":
				o = gw
			case u.GetKind() == "HTTPRoute" && u.GetName() == "cart":
				continue
			}
		}
		out = append(out, o)
	}
	return append(out, extra...)
}

// sectionRef is a parentRef to infra/edge's listener name.
func sectionRef(name string) objects.ParentRef {
	r := edgeRef
	r.SectionName = name
	return r
}

// rejectedBy is a current Accepted=False status entry with reason.
func rejectedBy(p objects.ParentRef, reason string) testutil.RouteParent {
	return testutil.RouteParent{Ref: p, Controller: testutil.Controller, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionFalse, reason), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}}
}

// hostWorld: edge has http (no hostname) and https (*.example.org); shop/multi
// names edge, edge's https and the HTTPRoute shop/api, and attaches to both listeners.
func hostWorld() []runtime.Object {
	gw := edgeWith([]objects.Listener{lst("http", 8080, ""), lst("https", 443, "*.example.org")}, up("http", 1), up("https", 2))
	multi := testutil.WithHostnames(testutil.HTTPRoute("shop", "multi", []objects.ParentRef{edgeRef, sectionRef("https"), apiRef},
		testutil.AcceptedBy(edgeRef, testutil.Controller), testutil.AcceptedBy(sectionRef("https"), testutil.Controller)),
		"shop.example.org", "shop.example.com")
	return viewAWorld(gw, multi, testutil.HTTPRoute("shop", "api", nil))
}

// Design §7.5: a rejected route's row carries view A's outcome and explanation.
func TestRouteListCarriesAttachment(t *testing.T) {
	gw := edgeWith([]objects.Listener{lst("http", 8080, ""), lst("https", 443, "*.example.org")}, up("http", 0), up("https", 0))
	cart := testutil.WithHostnames(testutil.HTTPRoute("shop", "cart", []objects.ParentRef{sectionRef("https")},
		rejectedBy(sectionRef("https"), "NoMatchingListenerHostname")), "shop.example.com")
	// A later stage's reason may have overwritten the attachment reason: inconclusive (rules §8.1).
	other := testutil.WithHostnames(testutil.HTTPRoute("shop", "other", []objects.ParentRef{sectionRef("https")},
		rejectedBy(sectionRef("https"), "UnsupportedValue")), "shop.example.com")
	_, srv, _ := startWith(t, setup{}, viewAWorld(gw, cart, other)...)
	p := get[map[string]any](t, srv, underEdge+"&attachment=not-accepted&limit=2", 200)
	items := p["items"].([]any)
	if len(items) != 2 || p["total"] != 2.0 {
		t.Fatalf("items %v", items)
	}
	row := items[0].(map[string]any)
	got := row["parents"].([]any)[0].(map[string]any)["computed"]
	want := decode(t, `{"outcome": "NoMatchingListenerHostname", "listeners": [],
		"explanation": "listener https has *.example.org, route has shop.example.com",
		"computed": {"by": "ui", "rules": "kgateway-2.4", "rule": "hostname-intersection", "state": "checked", "coverage": ["status-compatible"]}}`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parents[0].computed:\n%v\nwant\n%v", got, want)
	}
	if !reflect.DeepEqual(row["flags"], []any{"not-accepted"}) {
		t.Errorf("flags %v", row["flags"])
	}
	inconclusive := items[1].(map[string]any)["parents"].([]any)[0].(map[string]any)["computed"].(map[string]any)["computed"]
	if want := decode(t, `{"by": "ui", "rules": "kgateway-2.4", "rule": "hostname-intersection", "state": "checked",
		"coverage": ["not-verified"], "note": "The controller's status cannot confirm this"}`); !reflect.DeepEqual(inconclusive, want) {
		t.Errorf("inconclusive computed:\n%v\nwant\n%v", inconclusive, want)
	}
}

func TestRouteDetailAttachments(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, hostWorld()...)
	d := get[RouteDetail](t, srv, routeDetail+"shop/multi", 200)
	if d.RuleSet != "kgateway-2.4" || len(d.Attachments) != 2 {
		t.Fatalf("ruleSet %q, attachments %+v", d.RuleSet, d.Attachments)
	}
	notVerified := []string{"count-agrees", "status-compatible", "not-verified"}
	for i, want := range []struct {
		ref       objects.ParentRef
		listeners []string
		hosts     [][]string
		coverage  []string
	}{
		{edgeRef, []string{"http", "https"}, [][]string{{"shop.example.org", "shop.example.com"}, {"shop.example.org"}}, notVerified},
		{sectionRef("https"), []string{"https"}, [][]string{{"shop.example.org"}}, []string{"count-agrees", "status-compatible"}},
	} {
		a := d.Attachments[i]
		if a.Index != i || a.ParentRef != want.ref || a.Target.Ref != edgeRef.Ref() || a.Target.Link != LinkDetail ||
			a.Result.Outcome != "accepted" || a.Result.Explanation != "" || len(a.Result.Listeners) != len(want.listeners) {
			t.Fatalf("attachment %d: %+v", i, a)
		}
		if c := a.Result.Computed; c.By != "ui" || c.Rules != "kgateway-2.4" || c.Rule != "hostname-intersection" || c.State != "checked" ||
			!slices.Equal(c.Coverage, []string{"status-compatible"}) || c.Reason != "" || c.Note != "" {
			t.Errorf("attachment %d computed %+v", i, c)
		}
		for j, l := range a.Result.Listeners {
			if l.Listener != want.listeners[j] || l.Resource.Ref != edgeRef.Ref() || !slices.Equal(l.EffectiveHostnames, want.hosts[j]) ||
				l.Computed.State != "checked" || !slices.Equal(l.Computed.Coverage, want.coverage) {
				t.Errorf("attachment %d listener %d: %+v", i, j, l)
			}
		}
	}
	// The list row carries the same result per status parent.
	row := routeByName(t, get[Page[RouteRow]](t, srv, underEdge, 200), "shop/multi")
	if len(row.Parents) != 2 || row.Parents[1].Computed == nil || !reflect.DeepEqual(*row.Parents[1].Computed, d.Attachments[1].Result) {
		t.Errorf("row parents %+v", row.Parents)
	}
}

// stateWorld has one listener per check state: ok agrees, bad disagrees,
// stale reports for an old generation; wide gets the 2.4 isolation note from narrow.
func stateWorld() []runtime.Object {
	stale := up("stale", 0)
	for i := range stale.Conds {
		stale.Conds[i].ObservedGeneration = 0
	}
	gw := edgeWith([]objects.Listener{lst("ok", 8080, ""), lst("bad", 8081, ""), lst("stale", 8082, ""), lst("wide", 8083, ""), lst("narrow", 8083, "x.example.com")},
		up("ok", 1), up("bad", 1), stale, up("wide", 0), up("narrow", 0))
	return viewAWorld(gw, testutil.HTTPRoute("shop", "cart", []objects.ParentRef{sectionRef("ok")}, testutil.AcceptedBy(sectionRef("ok"), testutil.Controller)),
		testutil.WithHostnames(testutil.HTTPRoute("shop", "waiting", []objects.ParentRef{sectionRef("stale")}, testutil.AcceptedBy(sectionRef("stale"), testutil.Controller)), "a.example.com"))
}

// unknownScope is objs with the controller's scope read from a ConfigMap, so it is unknown.
func unknownScope(objs []runtime.Object) []runtime.Object {
	env := corev1.EnvVar{Name: install.DiscoveryNamespaceSelectorsEnv, ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{Key: "k"}}}
	d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, 1, env)
	out := slices.Clone(objs)
	for i, o := range out {
		switch v := o.(type) {
		case *appsv1.Deployment:
			if v.Name == "kgateway" {
				out[i] = d
			}
		case *corev1.Pod:
			if v.Name == "kgateway-0" {
				out[i] = testutil.ControllerPod(d, "kgateway-0", true)
			}
		}
	}
	return out
}

func TestGatewayDetailListenerStates(t *testing.T) {
	_, srv, _ := startWith(t, setup{disputes: true}, stateWorld()...)
	d := get[GatewayDetail](t, srv, gwDetail, 200)
	if d.RuleSet != "kgateway-2.4" || len(d.Listeners) != 5 {
		t.Fatalf("ruleSet %q, listeners %d", d.RuleSet, len(d.Listeners))
	}
	ls := map[string]ListenerDTO{}
	for _, l := range d.Listeners {
		ls[l.Name] = l
		if l.Computed == nil || l.Computed.By != "ui" || l.Computed.Rules != "kgateway-2.4" || l.Computed.Rule != "listener-count" {
			t.Errorf("%s: computed %+v", l.Name, l.Computed)
		}
	}
	for _, want := range []struct {
		name     string
		routes   int
		state    string
		coverage []string
		note     bool
	}{
		{"ok", 1, "checked", []string{"count-agrees"}, false},
		{"bad", 0, "disagrees", nil, false},
		{"stale", 1, "pending", nil, false},
		{"wide", 0, "checked", []string{"count-agrees"}, true},
		{"narrow", 0, "checked", []string{"count-agrees"}, false},
	} {
		l := ls[want.name]
		if l.ComputedRoutes == nil || *l.ComputedRoutes != want.routes || l.Computed.State != want.state ||
			!slices.Equal(l.Computed.Coverage, want.coverage) || l.IsolationNote != want.note || l.Computed.Reason != "" {
			t.Errorf("%s: routes %v, computed %+v, isolationNote %v", want.name, l.ComputedRoutes, l.Computed, l.IsolationNote)
		}
	}
	// A disagreement sends both numbers.
	if bad := ls["bad"]; bad.Status == nil || bad.Status.AttachedRoutes != 1 {
		t.Errorf("bad status %+v", bad.Status)
	}
	g := get[Page[GatewayListRow]](t, srv, listBase+"/gateways?q=edge", 200).Items[0]
	if g.Routes == nil || *g.Routes != 2 || g.Disagreements != 1 {
		t.Errorf("gateway row routes %v, disagreements %d", g.Routes, g.Disagreements)
	}
	// A pending membership sends no hostnames.
	w := routeByName(t, get[Page[RouteRow]](t, srv, underEdge, 200), "shop/waiting").Parents[0].Computed
	if w == nil || len(w.Listeners) != 1 || w.Listeners[0].Computed.State != "pending" || w.Listeners[0].EffectiveHostnames != nil {
		t.Errorf("pending membership %+v", w)
	}

	t.Run("unknown", func(t *testing.T) {
		_, srv, _ := startWith(t, setup{disputes: true}, unknownScope(stateWorld())...)
		for _, l := range get[GatewayDetail](t, srv, gwDetail, 200).Listeners {
			if c := l.Computed; c == nil || c.State != "unknown" || c.Reason != string(computed.UnknownScope) || l.ComputedRoutes != nil || l.IsolationNote || c.Coverage != nil {
				t.Errorf("%s: routes %v, computed %+v", l.Name, l.ComputedRoutes, c)
			}
		}
		row := routeByName(t, get[Page[RouteRow]](t, srv, underEdge, 200), "shop/cart")
		if a := row.Parents[0].Computed; a == nil || a.Outcome != "" || len(a.Listeners) != 0 || a.Computed.State != "unknown" || a.Computed.Reason != "scope-unknown" {
			t.Errorf("unknown ref %+v", a)
		}
		if g := get[Page[GatewayListRow]](t, srv, listBase+"/gateways?q=edge", 200).Items[0]; g.Routes != nil {
			t.Errorf("unknown counts give no routes: %v", *g.Routes)
		}
	})
}

func TestListenerParam(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, hostWorld()...)
	for _, q := range []string{
		"/routes?listener=http", "/routes?listener=http&parent=gateway.networking.k8s.io/HTTPRoute/shop/api",
		"/routes?listener=&" + edgeParent, "/routes?listener=http&listener=https&" + edgeParent, "/gateways?listener=http",
	} {
		getErr(t, srv, listBase+q, 400)
	}
	if p := get[Page[RouteRow]](t, srv, underEdge+"&listener=_unknown", 200); p.Total != 0 {
		t.Errorf("_unknown: %v", routeNames(p))
	}
	for _, l := range []string{"http", "https"} {
		if p := get[Page[RouteRow]](t, srv, underEdge+"&listener="+l, 200); p.Total != 1 || !slices.Equal(routeNames(p), []string{"shop/multi"}) {
			t.Errorf("listener=%s: %v", l, routeNames(p))
		}
	}
	if m := get[MetaResponse](t, srv, "/api/v1/meta", 200); !reflect.DeepEqual(m.Filters["routes"], []string{"listener"}) {
		t.Errorf("meta filters %v", m.Filters)
	}
}

func TestMemberCoverage(t *testing.T) {
	count := computed.CheckResult{Coverage: []computed.Coverage{computed.CoverageCount}}
	status := computed.CheckResult{Coverage: []computed.Coverage{computed.CoverageStatus}}
	for _, tc := range []struct {
		listener, ref computed.CheckResult
		matched       int
		want          []computed.Coverage
	}{
		{count, status, 1, []computed.Coverage{"count-agrees", "status-compatible"}},
		{count, status, 2, []computed.Coverage{"count-agrees", "status-compatible", "not-verified"}},
		{computed.CheckResult{}, status, 1, []computed.Coverage{"status-compatible"}},
		{computed.CheckResult{}, computed.CheckResult{}, 1, []computed.Coverage{"not-verified"}},
	} {
		if got := memberCoverage(tc.listener, tc.ref, tc.matched); !slices.Equal(got, tc.want) {
			t.Errorf("memberCoverage(%v, %v, %d) = %v, want %v", tc.listener.Coverage, tc.ref.Coverage, tc.matched, got, tc.want)
		}
	}
}

// selectorWorld: edge's only listener sel selects namespaces with zone=in.
// shop has it, so shop/cart is a member; infra does not, so infra/odd, which
// the controller accepted, has no listener and disagrees. sel reports 2 for 1.
func selectorWorld() []runtime.Object {
	sel := lst("sel", 8080, "")
	sel.AllowedRoutes.Namespaces = objects.RouteNamespaces{From: "Selector", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"zone": "in"}}}
	objs := viewAWorld(edgeWith([]objects.Listener{sel}, up("sel", 2)),
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		testutil.HTTPRoute("infra", "odd", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)))
	for i, o := range objs {
		if ns, ok := o.(*corev1.Namespace); ok && ns.Name == "shop" {
			objs[i] = testutil.Namespace("shop", map[string]string{"team": "a", "zone": "in"})
		}
	}
	return objs
}

// hideNamespaces denies every Namespace object and nothing else.
var hideNamespaces = AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Kind != "Namespace" })

// flagged reports whether row r carries flag f.
func flagged(r RouteRow, f string) bool { return slices.Contains(r.Flags, f) }

// Design §9.1 (2.3): a Selector over a hidden Namespace restricts membership;
// listener-unknown follows the caller (rule 6); health, row states and
// disagreements do not (rule 9). The count, its check and the routes column
// are the listener's, so every caller sees them (rule 5, user 2026-10-26).
func TestSelectorOverHiddenNamespaceIsRestricted(t *testing.T) {
	_, all, _ := startWith(t, setup{disputes: true}, selectorWorld()...)
	_, some, _ := startWith(t, setup{disputes: true, authz: hideNamespaces}, selectorWorld()...)
	full, hid := get[Page[RouteRow]](t, all, underEdge, 200), get[Page[RouteRow]](t, some, underEdge, 200)
	for _, name := range []string{"shop/cart", "infra/odd"} {
		a, h := routeByName(t, full, name), routeByName(t, hid, name)
		if c := a.Parents[0].Computed; c == nil || c.Computed.State == "restricted" {
			t.Errorf("%s unrestricted: %+v", name, c)
		}
		c := h.Parents[0].Computed
		if c == nil || c.Outcome != "" || len(c.Listeners) != 0 || c.Explanation != "" ||
			!reflect.DeepEqual(c.Computed, Computed{By: "ui", Rules: "kgateway-2.4", State: "restricted"}) {
			t.Errorf("%s restricted: %+v", name, c)
		}
		if a.Health != h.Health {
			t.Errorf("%s health %s, restricted %s", name, a.Health, h.Health)
		}
	}
	if !flagged(routeByName(t, full, "infra/odd"), "listener-unknown") || flagged(routeByName(t, hid, "infra/odd"), "listener-unknown") {
		t.Errorf("listener-unknown per caller: %v / %v", routeByName(t, full, "infra/odd").Flags, routeByName(t, hid, "infra/odd").Flags)
	}
	const unknownState = listBase + "/routes?state=listener-unknown"
	if a, h := get[Page[RouteRow]](t, all, unknownState, 200), get[Page[RouteRow]](t, some, unknownState, 200); a.Total != 1 || h.Total != 0 {
		t.Errorf("state=listener-unknown: %v / %v", routeNames(a), routeNames(h))
	}
	ad, hd := get[GatewayDetail](t, all, gwDetail, 200), get[GatewayDetail](t, some, gwDetail, 200)
	if l := ad.Listeners[0]; l.ComputedRoutes == nil || *l.ComputedRoutes != 1 || l.Computed.State != "disagrees" {
		t.Errorf("sel unrestricted: %v %+v", l.ComputedRoutes, l.Computed)
	}
	if !reflect.DeepEqual(ad.Listeners[0], hd.Listeners[0]) {
		t.Errorf("sel with a hidden Namespace: %+v, unrestricted %+v", hd.Listeners[0], ad.Listeners[0])
	}
	if ad.Health != hd.Health {
		t.Errorf("gateway health %s, restricted %s", ad.Health, hd.Health)
	}
	if a, h := get[HealthResponse](t, all, base+"/health", 200), get[HealthResponse](t, some, base+"/health", 200); a.State != h.State || !reflect.DeepEqual(a.Counts, h.Counts) {
		t.Errorf("install health %s %v, restricted %s %v", a.State, a.Counts, h.State, h.Counts)
	}
	// Disagreements count every check, as health does.
	ag, hg := get[Page[GatewayListRow]](t, all, listBase+"/gateways?q=edge", 200).Items[0], get[Page[GatewayListRow]](t, some, listBase+"/gateways?q=edge", 200).Items[0]
	if ag.Routes == nil || *ag.Routes != 1 || hg.Routes == nil || *hg.Routes != 1 || ag.Disagreements != 1 || hg.Disagreements != 1 || ag.Health != hg.Health {
		t.Errorf("gateway rows %+v / %+v", ag, hg)
	}
}

// Design §9.1: listener= over a hidden Selector Namespace answers as when
// those facts are unknown; deniedInInstall takes no filter.
func TestListenerFilterCannotProbe(t *testing.T) {
	_, all, _ := startWith(t, setup{disputes: true}, selectorWorld()...)
	_, some, _ := startWith(t, setup{disputes: true, authz: hideNamespaces}, selectorWorld()...)
	_, unknown, _ := startWith(t, setup{disputes: true}, unknownScope(selectorWorld())...)
	const q = underEdge + "&listener=sel"
	if p := get[Page[RouteRow]](t, all, q, 200); !slices.Equal(routeNames(p), []string{"shop/cart"}) || p.Total != 1 {
		t.Fatalf("unrestricted: %v total %d", routeNames(p), p.Total)
	}
	h, u := get[Page[RouteRow]](t, some, q, 200), get[Page[RouteRow]](t, unknown, q, 200)
	if len(h.Items) != len(u.Items) || h.Total != u.Total || h.Total != 0 || h.Next != "" {
		t.Errorf("restricted %v total %d; unknown %v total %d", routeNames(h), h.Total, routeNames(u), u.Total)
	}
	for _, p := range []string{listBase + "/routes", underEdge, q} {
		if d := get[Page[RouteRow]](t, some, p, 200).DeniedInInstall; d != h.DeniedInInstall {
			t.Errorf("%s: deniedInInstall %d, with listener %d", p, d, h.DeniedInInstall)
		}
	}
}

// memberWorld: edge's http has shop/cart; ListenerSet ls1's extra has shop/via-ls.
func memberWorld() []runtime.Object {
	ls := admitAll(testutil.ListenerSet("infra", "ls1", edgeRef, nil, up("extra", 1)))
	return viewAWorld(edgeWith([]objects.Listener{lst("http", 8080, "")}, up("http", 1)), ls,
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		testutil.HTTPRoute("shop", "via-ls", []objects.ParentRef{ls1Ref}, testutil.AcceptedBy(ls1Ref, testutil.Controller)))
}

// Rule 7: KGWUI-012 on a visible object whose evidence names a hidden one
// is listed with its detail replaced. A count's finding names none (rule 5).
func TestKGWUI012EvidenceRestricted(t *testing.T) {
	kgwui012 := func(t *testing.T, srv string, h HealthResponse, on string) FindingDTO {
		t.Helper()
		for _, f := range findingsOn(h, on) {
			if f.Code == health.CodeComputedDisagrees {
				return f
			}
		}
		t.Fatalf("%s: no KGWUI-012 on %s", srv, on)
		return FindingDTO{}
	}
	for _, tc := range []struct {
		name  string
		objs  []runtime.Object
		authz Authorizer
		on    string
	}{
		{"hidden selector namespace", selectorWorld(), hideNamespaces, "infra/odd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, all, _ := startWith(t, setup{disputes: true}, tc.objs...)
			_, some, _ := startWith(t, setup{disputes: true, authz: tc.authz}, tc.objs...)
			if f := kgwui012(t, "all", get[HealthResponse](t, all, base+"/health", 200), tc.on); f.EvidenceRestricted || f.Detail == restrictedEvidence {
				t.Errorf("unrestricted: %+v", f)
			}
			f := kgwui012(t, "some", get[HealthResponse](t, some, base+"/health", 200), tc.on)
			if !f.EvidenceRestricted || f.Detail != restrictedEvidence || f.Restricted || f.Message == "" || f.Resource == nil || f.Resource.Link != LinkDetail {
				t.Errorf("restricted: %+v", f)
			}
		})
	}
}

// Every computed label carries the selection note of an unknown controller version.
func TestNoteOnUnknownVersion(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, withControllerVersion("unknown", hostWorld())...)
	var cs []Computed
	add := func(a *Attachment) {
		if a != nil {
			cs = append(cs, a.Computed)
			for _, l := range a.Listeners {
				cs = append(cs, l.Computed)
			}
		}
	}
	for _, r := range get[Page[RouteRow]](t, srv, underEdge, 200).Items {
		for _, p := range r.Parents {
			add(p.Computed)
		}
	}
	for _, a := range get[RouteDetail](t, srv, routeDetail+"shop/multi", 200).Attachments {
		add(&a.Result)
	}
	for _, l := range get[GatewayDetail](t, srv, gwDetail, 200).Listeners {
		if l.Computed != nil {
			cs = append(cs, *l.Computed)
		}
	}
	if len(cs) < 8 {
		t.Fatalf("only %d computed labels", len(cs))
	}
	for _, c := range cs {
		if c.Note != unknownNote || c.Rules != "kgateway-2.4" {
			t.Errorf("computed %+v", c)
		}
	}
	b, err := json.Marshal(cs[0])
	if err != nil || !bytes.Contains(b, []byte(`"note":"`+unknownNote+`"`)) {
		t.Errorf("wire %s", b)
	}
}

// unknownCountWorld: edge's TLS listener tls has the accepted TLSRoute
// shop/tls and the pre-v1 TCPRoute shop/tcp, which names edge through
// tcpRef, under an unknown experimental setting.
func unknownCountWorld(tcpRef objects.ParentRef) (setup, []runtime.Object) {
	served := slices.Clone(testutil.ServedUpstream())
	for i := range served {
		switch served[i].Kind {
		case "TCPRoute":
			served[i].Version = "v1alpha2"
		case "TLSRoute":
			served[i].Version = "v1"
		}
	}
	tls := lst("tls", 443, "")
	tls.Protocol, tls.TLS = "TLS", &objects.ListenerTLS{Mode: "Terminate"}
	tlsRoute := tcpRoute("v1", "shop", "tls", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller))
	tlsRoute.SetKind("TLSRoute")
	objs := viewAWorld(edgeWith([]objects.Listener{tls}, up("tls", 2)), tlsRoute,
		tcpRoute("v1alpha2", "shop", "tcp", []objects.ParentRef{tcpRef}, testutil.AcceptedBy(tcpRef, testutil.Controller)))
	exp := corev1.EnvVar{Name: install.ExperimentalEnv, ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{Key: "k"}}}
	d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"a"}}]`), exp)
	for i, o := range objs {
		switch v := o.(type) {
		case *appsv1.Deployment:
			if v.Name == "kgateway" {
				objs[i] = d
			}
		case *corev1.Pod:
			if v.Name == "kgateway-0" {
				objs[i] = testutil.ControllerPod(d, "kgateway-0", true)
			}
		}
	}
	return setup{served: served, disputes: true}, objs
}

// A listener whose count is unknown presents no members: on a TLS listener a
// pre-v1 TCPRoute under an unknown experimental setting makes the count
// setting-unknown, so the accepted TLSRoute's membership is unknown too.
func TestUnknownCountHasNoMembers(t *testing.T) {
	s, objs := unknownCountWorld(edgeRef)
	_, srv, _ := startWith(t, s, objs...)
	if l := get[GatewayDetail](t, srv, gwDetail, 200).Listeners[0]; l.ComputedRoutes != nil || l.Computed == nil || l.Computed.Reason != string(computed.UnknownSetting) {
		t.Fatalf("tls count %v %+v", l.ComputedRoutes, l.Computed)
	}
	a := routeByName(t, get[Page[RouteRow]](t, srv, underEdge, 200), "shop/tls").Parents[0].Computed
	if a == nil || a.Outcome != "accepted" || len(a.Listeners) != 1 {
		t.Fatalf("tls ref %+v", a)
	}
	if m := a.Listeners[0]; m.Listener != "tls" || m.EffectiveHostnames != nil || m.Computed.Coverage != nil ||
		m.Computed.State != "unknown" || m.Computed.Reason != string(computed.UnknownSetting) {
		t.Errorf("membership under an unknown count: %+v", m)
	}
	if p := get[Page[RouteRow]](t, srv, underEdge+"&listener=tls", 200); p.Total != 0 {
		t.Errorf("listener=tls lists %v", routeNames(p))
	}
}

// matrixListener is a listener count of a rules fixture; matrixRef a parentRef outcome.
type matrixListener struct {
	Count         int    `json:"count"`
	Unknown       string `json:"unknown"`
	Error         string `json:"error"`
	IsolationNote bool   `json:"isolationNote"`
}

type matrixRef struct {
	Outcome   string    `json:"outcome"`
	Unknown   string    `json:"unknown"`
	Listeners *[]string `json:"listeners"`
}

// matrixCheck is the Computed label the case expects on a listener or parentRef key.
type matrixCheck struct {
	State    string   `json:"state"`
	Coverage []string `json:"coverage"`
	Note     string   `json:"note"`
}

// matrixExpect is a rules fixture's expect block (pkg/computed's keys, plus
// checks, and kgwui012 and kgwui011, the objects each finding is on). Checks
// are keyed by listener, parentRef and, for views B and C, row.
type matrixExpect struct {
	Listeners  map[string]matrixListener `json:"listeners"`
	ParentRefs map[string]matrixRef      `json:"parentRefs"`
	Members    *map[string][]string      `json:"members"`
	Checks     map[string]matrixCheck    `json:"checks"`
	KGWUI012   *[]string                 `json:"kgwui012"`
	KGWUI011   *[]string                 `json:"kgwui011"`
	// Views B and C, as pkg/computed's golden writes them (see its delegationExpect).
	Rows               *map[string]matrixRow `json:"rows"`
	Entries            map[string]string     `json:"entries"`
	Signals            map[string][]string   `json:"signals"`
	Effective          map[string]matrixEdge `json:"effective"`
	EffectiveTruncated []string              `json:"effectiveTruncated"`
	// Incomplete, Uncertain and Cap are rule-level and never served.
	Incomplete json.RawMessage `json:"incomplete"`
	Uncertain  json.RawMessage `json:"uncertain"`
	Cap        json.RawMessage `json:"cap"`
}

// matrixRow is a view B row, keyed "<parent> <rule>.<ref> <child>" ("-" for none).
type matrixRow struct {
	Outcome           string    `json:"outcome"`
	Unknown           string    `json:"unknown"`
	Reason            string    `json:"reason"`
	SomeNotComputed   bool      `json:"someNotComputed"`
	ContextsTruncated bool      `json:"contextsTruncated"`
	Contexts          *int      `json:"contexts"`
	Paths             *[]string `json:"paths"`
	Deps              *[]string `json:"deps"`
}

// matrixEdge is a view C edge, keyed "<child> <- <parent>"; each context is
// "r<i> <match> | <match>" for a kept rule or "r<i> dropped <clause>", joined by "; ".
type matrixEdge struct {
	Contexts          []string `json:"contexts"`
	ContextsTruncated bool     `json:"contextsTruncated"`
}

// decodeMatrix decodes expect strictly; every listener, parentRef and row key needs a check.
func decodeMatrix(t *testing.T, m map[string]any) matrixExpect {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var e matrixExpect
	if err := dec.Decode(&e); err != nil {
		t.Fatalf("expect: %v", err)
	}
	if e.KGWUI012 == nil {
		t.Fatal("expect needs kgwui012, even when empty")
	}
	keys := slices.Concat(slices.Collect(maps.Keys(e.Listeners)), slices.Collect(maps.Keys(e.ParentRefs)))
	if e.Rows != nil {
		if e.KGWUI011 == nil {
			t.Fatal("a delegation case needs kgwui011, even when empty")
		}
		keys = append(keys, slices.Collect(maps.Keys(*e.Rows))...)
	}
	if len(keys) != len(e.Checks) {
		t.Fatalf("expect has %d checks for %d listeners, parentRefs and rows", len(e.Checks), len(keys))
	}
	for _, k := range keys {
		if _, ok := e.Checks[k]; !ok {
			t.Fatalf("expect has no check for %s", k)
		}
	}
	return e
}

// ruleCaseWorld seeds a fixture as a cluster: a controller at the case's
// version and settings, one GatewayClass per class named, the case's
// Namespaces (forbidden when the case has no Namespace access) and objects.
// Each kind is served at the version of its first object.
func ruleCaseWorld(t *testing.T, c testutil.RuleCase) (setup, []runtime.Object, []*unstructured.Unstructured) {
	t.Helper()
	var env []corev1.EnvVar
	for _, k := range slices.Sorted(maps.Keys(c.Settings)) {
		if v := c.Settings[k]; v == testutil.SettingValueFrom {
			env = append(env, corev1.EnvVar{Name: k, ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{Key: "k"}}})
		} else {
			env = append(env, testutil.Env(k, v))
		}
	}
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:"+c.ControllerVersion, 1, 1, env...)
	d.Labels["app.kubernetes.io/version"] = strings.TrimPrefix(c.ControllerVersion, "v")
	objs := []runtime.Object{d, testutil.ControllerPod(d, "kgateway-0", true)}
	s := setup{served: testutil.ServedUpstream()}
	if len(c.Bounds) > 0 {
		// The case's bounds run through the engine, as in production (rules §5.7).
		s.bounds = computed.DefaultBounds
		v := reflect.ValueOf(&s.bounds).Elem()
		for _, name := range slices.Sorted(maps.Keys(c.Bounds)) {
			f := v.FieldByName(name)
			if !f.IsValid() {
				t.Fatalf("bounds: no field %q", name)
			}
			f.SetInt(int64(c.Bounds[name]))
		}
	}
	if c.NamespacesOK {
		objs = append(objs, testutil.Namespace("kgw", nil))
		for _, ns := range slices.Sorted(maps.Keys(c.Namespaces)) {
			objs = append(objs, testutil.Namespace(ns, c.Namespaces[ns]))
		}
	} else {
		s.prepare = func(_ *dynamicfake.FakeDynamicClient, kube *kubefake.Clientset) {
			kube.PrependReactor("list", "namespaces", func(clienttesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "", nil)
			})
		}
	}
	pinned := map[objects.GroupKind]bool{}
	classes := map[string]bool{}
	var us []*unstructured.Unstructured
	for _, o := range c.Objects {
		u := o.DeepCopy()
		gv, _ := schema.ParseGroupVersion(u.GetAPIVersion())
		gk := objects.GroupKind{Group: gv.Group, Kind: u.GetKind()}
		i := slices.IndexFunc(s.served, func(k kinds.Served) bool { return k.Group == gk.Group && k.Kind == gk.Kind })
		switch {
		case i < 0:
			t.Fatalf("kind %v is not served", gk)
		case !pinned[gk]:
			if !slices.Contains(s.served[i].Versions, gv.Version) {
				t.Fatalf("%v has no version %s", gk, gv.Version)
			}
			s.served[i].Version, pinned[gk] = gv.Version, true
		}
		u.SetAPIVersion(schema.GroupVersion{Group: gv.Group, Version: s.served[i].Version}.String())
		u.SetUID(types.UID(u.GetKind() + "/" + u.GetNamespace() + "/" + u.GetName()))
		u.SetCreationTimestamp(testutil.Old)
		if u.GetKind() == "Gateway" {
			cls, _, _ := unstructured.NestedString(u.Object, "spec", "gatewayClassName")
			classes[cls] = true
		}
		us = append(us, u)
	}
	for _, cls := range slices.Sorted(maps.Keys(classes)) {
		objs = append(objs, testutil.GatewayClass(cls, testutil.Controller, true))
	}
	return s, objs, us
}

// ruleCaseFiles are the rules fixtures of every rule set.
func ruleCaseFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "computed", "testdata", "*", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no rules fixtures (%v)", err)
	}
	return files
}

// Design §9.1 "Computed views through the API": every rules fixture not
// marked ruleOnly is seeded as a cluster and read back through the handlers.
// Seeded objects are first seen 61 s before the responses (newHarness), past
// the 30 s window; a case with a KGWUI-012 also runs with its objects first
// seen at testutil.Now, stepping the engine clock to the window's end.
func TestViewAMatrixThroughAPI(t *testing.T) {
	ran := 0
	for _, f := range ruleCaseFiles(t) {
		c := testutil.LoadRuleCase(t, f)
		if c.RuleOnly {
			continue
		}
		ran++
		rules := filepath.Base(filepath.Dir(f))
		t.Run(rules+"/"+filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			want := decodeMatrix(t, c.Expect)
			s, objs, us := ruleCaseWorld(t, c)
			// A case expecting KGWUI-012 disagrees with the controller on purpose.
			s.disputes = len(*want.KGWUI012) > 0
			h := newHarness(t, s, append(objs, asObjects(us)...)...)
			checkMatrix(t, h.srv, rules, c, want, false)
			if !s.disputes {
				return
			}
			s.late = us
			h = newHarness(t, s, objs...)
			checkMatrix(t, h.srv, rules, c, want, true)
			h.clk.set(testutil.Now.Add(engine.Window - time.Nanosecond))
			h.e.Compute()
			checkMatrix(t, h.srv, rules, c, want, true)
			h.clk.set(testutil.Now.Add(engine.Window))
			h.e.Compute()
			checkMatrix(t, h.srv, rules, c, want, false)
		})
	}
	// Every fixture that is not rule-only: rules §11.1 and §11.4 (view A),
	// and three of §11.2 per rule set, whose view A is checked here.
	if want := 47; ran != want {
		t.Errorf("ran %d cases, want %d", ran, want)
	}
}

func asObjects(us []*unstructured.Unstructured) []runtime.Object {
	out := make([]runtime.Object, len(us))
	for i, u := range us {
		out[i] = u
	}
	return out
}

// checkMatrix asserts every listener, parentRef and KGWUI-012 of the case
// through the handlers. inWindow: the window is still open, so a check
// expected to disagree is pending and no KGWUI-012 is raised yet.
func checkMatrix(t *testing.T, srv *httptest.Server, rules string, c testutil.RuleCase, want matrixExpect, inWindow bool) {
	t.Helper()
	check := func(key string) matrixCheck {
		w := want.Checks[key]
		if inWindow && w.State == "disagrees" {
			w = matrixCheck{State: "pending"}
		}
		return w
	}
	// The list merges the checks of written refs that resolve alike (design §7.3).
	merged := func(keys []string) matrixCheck {
		w := check(keys[0])
		m := Computed{State: w.State, Coverage: w.Coverage, Note: w.Note}
		for _, k := range keys[1:] {
			w := check(k)
			m = mergeComputed(m, Computed{State: w.State, Coverage: w.Coverage, Note: w.Note})
		}
		return matrixCheck{State: m.State, Coverage: m.Coverage, Note: m.Note}
	}
	label := func(key string, got Computed, reason string, w matrixCheck) {
		t.Helper()
		if got.By != ComputedBy || got.Rules != rules || got.State != w.State || !slices.Equal(got.Coverage, w.Coverage) ||
			(w.Note != "" && got.Note != w.Note) || got.Reason != reason {
			t.Errorf("%s: computed %+v, want %+v reason %q", key, got, w, reason)
		}
	}
	// List and detail are asserted separately: each must serve every key it covers.
	seen, listSeen, detailSeen := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, u := range c.Objects {
		ref := objects.RefOf(u)
		var ls []ListenerDTO
		switch ref.Kind {
		case "Gateway":
			ls = get[GatewayDetail](t, srv, base+"/gateways/"+ref.Namespace+"/"+ref.Name, 200).Listeners
		case "ListenerSet":
			ls = get[ListenerSetDetail](t, srv, lsDetail+ref.Namespace+"/"+ref.Name, 200).Listeners
		}
		for _, l := range ls {
			key := ref.Kind + " " + ref.Namespace + "/" + ref.Name + " " + l.Name
			w, ok := want.Listeners[key]
			switch {
			case !ok && l.Computed != nil:
				t.Errorf("%s: computed %+v, want none", key, l.Computed)
			case !ok:
			case l.Computed == nil:
				t.Errorf("%s: no computed count", key)
			case w.Unknown != "":
				seen[key] = true
				label(key, *l.Computed, w.Unknown, check(key))
				if l.ComputedRoutes != nil || l.IsolationNote {
					t.Errorf("%s: unknown count sends routes %v, isolationNote %v", key, l.ComputedRoutes, l.IsolationNote)
				}
			default:
				seen[key] = true
				label(key, *l.Computed, "", check(key))
				if l.ComputedRoutes == nil || *l.ComputedRoutes != w.Count || l.IsolationNote != w.IsolationNote {
					t.Errorf("%s: routes %v, isolationNote %v, want %+v", key, l.ComputedRoutes, l.IsolationNote, w)
				}
			}
		}
	}

	// attachment asserts a for keys, the written refs it covers; the first names it.
	attachment := func(served map[string]bool, keys []string, a Attachment) {
		t.Helper()
		key := keys[0]
		w, ok := want.ParentRefs[key]
		if !ok {
			t.Errorf("%s: computed %+v, want none", key, a)
			return
		}
		for _, k := range keys {
			served[k] = true
		}
		if w.Unknown != "" {
			label(key, a.Computed, w.Unknown, merged(keys))
			if a.Outcome != "" || len(a.Listeners) != 0 {
				t.Errorf("%s: unknown sends outcome %q, listeners %+v", key, a.Outcome, a.Listeners)
			}
			return
		}
		label(key, a.Computed, "", merged(keys))
		names := []string{}
		for _, l := range a.Listeners {
			names = append(names, l.Listener)
			m := key + " -> " + l.Resource.Kind + " " + l.Resource.Namespace + "/" + l.Resource.Name + " " + l.Listener
			hosts, written := []string(nil), want.Members != nil
			if written {
				hosts = (*want.Members)[m]
			}
			if st := l.Computed.State; written && (st == "checked" || st == "disagrees") && !slices.Equal(l.EffectiveHostnames, hosts) &&
				(len(hosts) > 0 || len(l.EffectiveHostnames) > 0) {
				t.Errorf("%s: hostnames %v, want %v", m, l.EffectiveHostnames, hosts)
			}
		}
		if a.Outcome != w.Outcome || (w.Listeners != nil && !slices.Equal(names, *w.Listeners)) {
			t.Errorf("%s: outcome %q on %v, want %+v", key, a.Outcome, names, w)
		}
	}
	routes := get[Page[RouteRow]](t, srv, base+"/routes?limit=200", 200)
	for _, u := range c.Objects {
		ref := objects.RefOf(u)
		i := slices.IndexFunc(routes.Items, func(r RouteRow) bool { return r.Ref.Ref == ref })
		if i < 0 {
			continue
		}
		spec := objects.RouteParentRefs(u)
		refKey := func(j int) string { return fmt.Sprintf("%s %s/%s #%d", ref.Kind, ref.Namespace, ref.Name, j) }
		for _, p := range routes.Items[i].Parents {
			j := slices.IndexFunc(spec, func(s objects.ParentRef) bool {
				return s.Ref() == p.Ref.Ref && s.SectionName == p.SectionName && s.Port == p.Port
			})
			switch {
			case p.Computed != nil && j >= 0:
				keys := []string{refKey(j)}
				for k := j + 1; k < len(spec); k++ {
					if spec[k] == spec[j] {
						keys = append(keys, refKey(k))
					}
				}
				attachment(listSeen, keys, *p.Computed)
			case p.Computed != nil:
				t.Errorf("%v: computed on parent %+v, not a spec parentRef", ref, p)
			case j >= 0:
				if _, ok := want.ParentRefs[refKey(j)]; ok {
					t.Errorf("%s: no computed outcome", refKey(j))
				}
			}
		}
		if ref.Kind == "HTTPRoute" {
			for _, a := range get[RouteDetail](t, srv, routeDetail+ref.Namespace+"/"+ref.Name, 200).Attachments {
				attachment(detailSeen, []string{refKey(a.Index)}, a.Result)
			}
		}
	}
	for k := range want.Listeners {
		if !seen[k] {
			t.Errorf("%s: not served", k)
		}
	}
	for k := range want.ParentRefs {
		if !listSeen[k] {
			t.Errorf("%s: not served by the list", k)
		}
		if strings.HasPrefix(k, "HTTPRoute ") && !detailSeen[k] {
			t.Errorf("%s: not served by route detail", k)
		}
	}

	var on []string
	for _, f := range get[HealthResponse](t, srv, base+"/health", 200).Findings {
		if f.Code == health.CodeComputedDisagrees {
			on = append(on, f.Resource.Kind+" "+f.Resource.Namespace+"/"+f.Resource.Name)
		}
	}
	wantOn := slices.Clone(*want.KGWUI012)
	if inWindow {
		wantOn = nil
	}
	slices.Sort(on)
	slices.Sort(wantOn)
	if !slices.Equal(on, wantOn) {
		t.Errorf("KGWUI-012 on %v, want %v", on, wantOn)
	}
}

// Design §9.1 for views B and C: every delegation fixture not marked
// ruleOnly is seeded as a cluster, with its bounds through
// engine.Config.DelegationBounds, and read back through the delegation
// endpoint, route detail and health. A row with written deps also runs once
// per hidden non-route dependency, which restricts it (design §7.4 rule 4).
func TestDelegationMatrixThroughAPI(t *testing.T) {
	ran := 0
	for _, f := range ruleCaseFiles(t) {
		c := testutil.LoadRuleCase(t, f)
		if c.RuleOnly || c.Expect["rows"] == nil {
			continue
		}
		ran++
		rules := filepath.Base(filepath.Dir(f))
		t.Run(rules+"/"+filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			want := decodeMatrix(t, c.Expect)
			s, objs, us := ruleCaseWorld(t, c)
			s.disputes = len(*want.KGWUI012)+len(*want.KGWUI011) > 0
			h := newHarness(t, s, append(objs, asObjects(us)...)...)
			checkDelegationMatrix(t, h.srv, rules, c, want)
			for _, dep := range hiddenDeps(t, want) {
				s.authz = AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != dep })
				checkRestrictedRows(t, newHarness(t, s, append(objs, asObjects(us)...)...).srv, rules, want, dep)
			}
		})
	}
	// Three per rule set: a cut after a dropping context, a row with no
	// contexts kept at the install cap, and a root behind a Selector.
	if want := 6; ran != want {
		t.Errorf("ran %d cases, want %d", ran, want)
	}
}

// matrixRouteKey parses "<ns>/<name>" as an HTTPRoute.
func matrixRouteKey(k string) objects.Ref {
	ns, name, _ := strings.Cut(k, "/")
	return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: ns, Name: name}
}

// delegRowKey is r's key under parent, as the fixtures write it.
func delegRowKey(parent objects.Ref, r DelegationRow) string {
	child := "-"
	if r.Child != nil {
		child = r.Child.Namespace + "/" + r.Child.Name
	}
	return fmt.Sprintf("%s/%s %d.%d %s", parent.Namespace, parent.Name, r.Rule, r.Ref, child)
}

// dtoMatch writes m as pkg/computed's golden does: "<type> <path>", then
// " h:<name>=<value>", " q:<name>=<value>" (~ for a regex) and " m:<method>".
func dtoMatch(m MatchDTO) string {
	s := "PathPrefix /"
	if m.Path != nil {
		s = m.Path.Type + " " + m.Path.Value
	}
	kv := func(p string, ks []KeyMatchDTO) {
		for _, k := range ks {
			op := "="
			if k.Type == "RegularExpression" {
				op = "~"
			}
			s += " " + p + ":" + k.Name + op + k.Value
		}
	}
	kv("h", m.Headers)
	kv("q", m.QueryParams)
	if m.Method != "" {
		s += " m:" + m.Method
	}
	return s
}

// effectiveContext writes c as the fixtures do. A dropped rule's clause is
// served as text, so it is compared by presence (the goldens pin the text).
func effectiveContext(c EffectiveContext) (string, []string) {
	var out, dropped []string
	for _, r := range c.Rules {
		if !r.Kept {
			out = append(out, fmt.Sprintf("r%d dropped", r.Index))
			dropped = append(dropped, r.Reason)
			continue
		}
		ms := make([]string, 0, len(r.Matches))
		for _, m := range r.Matches {
			ms = append(ms, dtoMatch(m))
		}
		out = append(out, fmt.Sprintf("r%d %s", r.Index, strings.Join(ms, " | ")))
	}
	return strings.Join(out, "; "), dropped
}

// withoutClauses drops each dropped rule's clause from a fixture context.
func withoutClauses(c string) string {
	parts := strings.Split(c, "; ")
	for i, p := range parts {
		if f := strings.Fields(p); len(f) == 3 && f[1] == "dropped" {
			parts[i] = f[0] + " " + f[1]
		}
	}
	return strings.Join(parts, "; ")
}

// checkDelegationMatrix asserts the case's rows, entries, signals, effective
// edges, checks and findings through the handlers.
func checkDelegationMatrix(t *testing.T, srv *httptest.Server, rules string, c testutil.RuleCase, want matrixExpect) {
	t.Helper()
	var routes []objects.Ref
	for _, u := range c.Objects {
		if r := objects.RefOf(u); r.Kind == "HTTPRoute" {
			routes = append(routes, r)
		}
	}
	// Rows: every row of every route, by the fixtures' key.
	got := map[string]DelegationRow{}
	for _, p := range routes {
		page := get[Page[DelegationRow]](t, srv, delegURL(p.Namespace, p.Name, "?limit=200"), 200)
		if page.Total != len(page.Items) {
			t.Errorf("%s: total %d for %d rows", refName(LinkedRef{Ref: p}), page.Total, len(page.Items))
		}
		for _, r := range page.Items {
			got[delegRowKey(p, r)] = r
		}
	}
	if g, w := slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(*want.Rows)); !slices.Equal(g, w) {
		t.Fatalf("rows %v, want %v", g, w)
	}
	for k, w := range *want.Rows {
		r, chk := got[k], want.Checks[k]
		if r.Outcome != w.Outcome || r.ContextsTruncated != w.ContextsTruncated || (w.Contexts != nil && len(r.Contexts) != *w.Contexts) {
			t.Errorf("%s: outcome %q contextsTruncated %v contexts %d, want %+v", k, r.Outcome, r.ContextsTruncated, len(r.Contexts), w)
		}
		// The row's reason is text; a fit clause shows on the row or, for an unknown row, as "dropped on the paths computed".
		if (w.Reason != "") != (r.Reason != "") {
			t.Errorf("%s: reason %q, want clause %q", k, r.Reason, w.Reason)
		}
		cmp := r.Computed
		if cmp.By != ComputedBy || cmp.Rules != rules || cmp.Rule != ruleDelegation || cmp.State != chk.State || !slices.Equal(cmp.Coverage, chk.Coverage) ||
			cmp.Reason != w.Unknown || (chk.Note != "" && cmp.Note != chk.Note) || w.SomeNotComputed != strings.Contains(cmp.Note, noteSomeNotComputed) {
			t.Errorf("%s: computed %+v, want %+v unknown %q someNotComputed %v", k, cmp, chk, w.Unknown, w.SomeNotComputed)
		}
		if w.Paths != nil {
			ps := []string{}
			for _, ctx := range r.Contexts {
				var steps []string
				for _, st := range ctx.Path {
					steps = append(steps, st.Ref.Namespace+"/"+st.Ref.Name+" "+dtoMatch(st.Match))
				}
				ps = append(ps, strings.Join(steps, " > "))
			}
			if !slices.Equal(ps, *w.Paths) {
				t.Errorf("%s: paths %q, want %q", k, ps, *w.Paths)
			}
		}
	}
	// Entries: each expected entry is the row's observed entry under its report key.
	for k, w := range want.Entries {
		child, key, _ := strings.Cut(k, " ")
		var seen bool
		for rk, r := range got {
			if r.Child == nil || r.Child.Namespace+"/"+r.Child.Name != child || r.Observed.Entry == nil {
				continue
			}
			e := r.Observed.Entry
			if writtenKey(e.ParentRef) != key {
				t.Errorf("%s: entry under %s, want %s", rk, writtenKey(e.ParentRef), key)
			}
			acc := slices.IndexFunc(e.Conditions, func(c Condition) bool { return c.Type == "Accepted" })
			if acc < 0 || e.Conditions[acc].Reason != w {
				t.Errorf("%s: entry %+v, want Accepted %s", rk, e.Conditions, w)
			}
			seen = true
		}
		if !seen {
			t.Errorf("entry %s: no row observes it", k)
		}
	}
	// Signals have no field of their own: the parent's KGWUI-012 below says
	// whether status agrees. Each signalled parent has rows.
	for k := range want.Signals {
		p, _, _ := strings.Cut(k, " ")
		if !slices.ContainsFunc(slices.Collect(maps.Keys(got)), func(rk string) bool { return strings.HasPrefix(rk, p+" ") }) {
			t.Errorf("signal %s: the parent has no rows", k)
		}
	}
	// Effective edges: every edge of every route, and the truncated routes.
	edges := map[string]matrixEdge{}
	var truncated []string
	for _, r := range routes {
		path := routeDetail + r.Namespace + "/" + r.Name
		d := get[RouteDetail](t, srv, path, 200)
		if d.EffectiveTruncated {
			truncated = append(truncated, r.Namespace+"/"+r.Name)
		}
		raw := get[map[string]any](t, srv, path, 200)
		rawEdges, _ := raw["effective"].([]any)
		for i, e := range d.Effective {
			k := r.Namespace + "/" + r.Name + " <- " + e.Parent.Namespace + "/" + e.Parent.Name
			// An edge with no contexts kept is sent as [], never omitted (rules §5.7).
			if cs, ok := rawEdges[i].(map[string]any)["contexts"].([]any); !ok || cs == nil {
				t.Errorf("%s: contexts %v, want a list", k, rawEdges[i].(map[string]any)["contexts"])
			}
			if e.Computed.State != "checked" || e.Computed.Rule != ruleEffective || !slices.Equal(e.Computed.Coverage, []string{"not-verified"}) {
				t.Errorf("%s: computed %+v", k, e.Computed)
			}
			cs := []string{}
			for _, ctx := range e.Contexts {
				s, dropped := effectiveContext(ctx)
				for _, why := range dropped {
					if why == "" {
						t.Errorf("%s: a dropped rule without its clause", k)
					}
				}
				cs = append(cs, s)
			}
			edges[k] = matrixEdge{Contexts: cs, ContextsTruncated: e.ContextsTruncated}
		}
	}
	wantEdges := map[string]matrixEdge{}
	for k, e := range want.Effective {
		cs := []string{}
		for _, c := range e.Contexts {
			cs = append(cs, withoutClauses(c))
		}
		wantEdges[k] = matrixEdge{Contexts: cs, ContextsTruncated: e.ContextsTruncated}
	}
	if !reflect.DeepEqual(edges, wantEdges) {
		t.Errorf("effective %+v, want %+v", edges, wantEdges)
	}
	if !slices.Equal(truncated, want.EffectiveTruncated) && len(truncated)+len(want.EffectiveTruncated) > 0 {
		t.Errorf("effectiveTruncated %v, want %v", truncated, want.EffectiveTruncated)
	}
	// Findings: KGWUI-012 (child-entry, parent-resolvedrefs) and KGWUI-011.
	h := get[HealthResponse](t, srv, base+"/health", 200)
	for code, w := range map[string][]string{health.CodeComputedDisagrees: *want.KGWUI012, health.CodeDelegatedNotAttached: *want.KGWUI011} {
		var on []string
		for _, f := range h.Findings {
			if f.Code == code {
				on = append(on, f.Resource.Kind+" "+f.Resource.Namespace+"/"+f.Resource.Name)
			}
		}
		slices.Sort(on)
		w = slices.Sorted(slices.Values(w))
		if !slices.Equal(on, w) {
			t.Errorf("%s on %v, want %v", code, on, w)
		}
	}
}

// writtenKey writes a parentRef as the fixtures write a report key: the
// fields set, in kgateway's status order.
func writtenKey(p objects.ParentRef) string {
	var f []string
	for _, kv := range [][2]string{{"group", p.Group}, {"kind", p.Kind}, {"name", p.Name}, {"namespace", p.Namespace}, {"sectionName", p.SectionName}} {
		if kv[1] != "" {
			f = append(f, kv[0]+": "+kv[1])
		}
	}
	return "{" + strings.Join(f, ", ") + "}"
}

// hiddenDeps are the non-route dependencies written on the case's rows:
// hiding any of them restricts the row (routes on a path never do, rule 4b).
func hiddenDeps(t *testing.T, want matrixExpect) []objects.Ref {
	t.Helper()
	var out []objects.Ref
	for _, k := range slices.Sorted(maps.Keys(*want.Rows)) {
		w := (*want.Rows)[k]
		if w.Deps == nil {
			continue
		}
		for _, d := range *w.Deps {
			kind, nsName, _ := strings.Cut(d, " ")
			ns, name, _ := strings.Cut(nsName, "/")
			ref := objects.Ref{Kind: kind, Namespace: ns, Name: name}
			// Each dependency kind has its own group (pkg/kinds); routes never restrict (rule 4b).
			switch kind {
			case "HTTPRoute":
				continue
			case "Namespace":
			case "XListenerSet":
				ref.Group = "gateway.networking.x-k8s.io"
			case "Gateway", "ListenerSet":
				ref.Group = objects.GatewayAPIGroup
			default:
				t.Fatalf("dependency %s: no group known for kind %s", d, kind)
			}
			if !slices.Contains(out, ref) {
				out = append(out, ref)
			}
		}
	}
	return out
}

// checkRestrictedRows asserts that every row whose deps include dep, and
// its child's effective edge, is restricted for a caller who cannot view dep.
func checkRestrictedRows(t *testing.T, srv *httptest.Server, rules string, want matrixExpect, dep objects.Ref) {
	t.Helper()
	hidden := Computed{By: ComputedBy, Rules: rules, State: string(computed.StateRestricted)}
	for _, k := range slices.Sorted(maps.Keys(*want.Rows)) {
		w := (*want.Rows)[k]
		if w.Deps == nil || !slices.Contains(*w.Deps, dep.Kind+" "+dep.Namespace+"/"+dep.Name) {
			continue
		}
		f := strings.Fields(k)
		parent, child := matrixRouteKey(f[0]), matrixRouteKey(f[2])
		page := get[Page[DelegationRow]](t, srv, delegURL(parent.Namespace, parent.Name, "?limit=200"), 200)
		i := slices.IndexFunc(page.Items, func(r DelegationRow) bool { return delegRowKey(parent, r) == k })
		if i < 0 {
			t.Errorf("hidden %v: row %s not listed", dep, k)
			continue
		}
		r := page.Items[i]
		if !reflect.DeepEqual(r.Computed, hidden) || r.Outcome != "" || r.Reason != "" ||
			!reflect.DeepEqual(r.Contexts, []DelegationContext{{Restricted: true}}) || r.Observed.ParentCondition != nil {
			t.Errorf("hidden %v: row %s %+v, want restricted", dep, k, r)
		}
		if f[2] == "-" {
			continue
		}
		d := get[RouteDetail](t, srv, routeDetail+child.Namespace+"/"+child.Name, 200)
		j := slices.IndexFunc(d.Effective, func(e EffectiveDTO) bool { return e.Parent.Ref == parent })
		if j < 0 || !reflect.DeepEqual(d.Effective[j].Computed, hidden) ||
			!reflect.DeepEqual(d.Effective[j].Contexts, []EffectiveContext{{Restricted: true}}) {
			t.Errorf("hidden %v: edge %s <- %s %+v, want restricted", dep, f[2], f[0], d.Effective)
		}
	}
}

// TestExplainClauses pins each fact's clause: no inner ";", since explain
// joins clauses with "; ", and the kind clause names the route's kind first.
func TestExplainClauses(t *testing.T) {
	cases := []struct {
		f    computed.Fact
		want string
	}{
		{computed.Fact{Listener: "https", Code: computed.FactHostname, Want: "*.example.org", Got: "shop.example.com"},
			"listener https has *.example.org, route has shop.example.com"},
		{computed.Fact{Listener: "https", Code: computed.FactNamespace, Want: "infra", Got: "shop"},
			"listener https allows routes from namespace infra, route is in shop"},
		{computed.Fact{Listener: "https", Code: computed.FactNamespace, Want: "team=web", Got: "team=retail"},
			"listener https allows namespaces with team=web, route's namespace has team=retail"},
		{computed.Fact{Listener: "https", Code: computed.FactNamespace, Want: "team=web"},
			"listener https allows namespaces with team=web, route's namespace has no labels"},
		{computed.Fact{Listener: "https", Code: computed.FactKind, Want: "GRPCRoute, TCPRoute", Got: "HTTPRoute"},
			"listener https does not allow HTTPRoute (allows GRPCRoute, TCPRoute)"},
		{computed.Fact{Listener: "https", Code: computed.FactSelectorError, Got: "bad"},
			"listener https has an invalid namespace selector: bad"},
		{computed.Fact{Listener: "https", Code: computed.FactPort, Want: "8443", Got: "443"},
			"parentRef wants port 8443, listener https is on 443"},
		{computed.Fact{Listener: "https", Code: computed.FactSection, Want: "api", Got: "https"},
			"parentRef names section api, not listener https"},
		{computed.Fact{Listener: "https", Code: "other"}, "listener https: other"},
	}
	for _, c := range cases {
		if got := explainFact(c.f); got != c.want {
			t.Errorf("%s: got %q, want %q", c.f.Code, got, c.want)
		}
		if strings.Contains(c.want, ";") {
			t.Errorf("%s: clause holds a ';': %q", c.f.Code, c.want)
		}
	}
	// Two facts of one stage stay two clauses.
	two := []computed.Fact{
		{Listener: "a", Code: computed.FactSection, Want: "api", Got: "a"},
		{Listener: "b", Code: computed.FactSection, Want: "api", Got: "b"},
	}
	if got, want := explain(two), "parentRef names section api, not listener a; parentRef names section api, not listener b"; got != want {
		t.Errorf("explain: got %q, want %q", got, want)
	}
}

// isolationWorld: edge's wide (*.example.com) and ls1's extra (host) share
// port 8081, so wide's 2.4 isolation note follows ls1's hostname.
func isolationWorld(host string) []runtime.Object {
	ls := admitAll(testutil.ListenerSet("infra", "ls1", edgeRef, nil, up("extra", 0)))
	ls.Object["spec"].(map[string]any)["listeners"].([]any)[0].(map[string]any)["hostname"] = host
	return viewAWorld(edgeWith([]objects.Listener{lst("wide", 8081, "*.example.com")}, up("wide", 0)), ls)
}

// Rule 5 (user, 2026-10-26): the isolation note is wide's own fact, so a
// caller who cannot view ls1 sees it as an unrestricted one does, including
// how ls1's hostname moves it, as a count includes hidden members. Rule 10:
// AllowAll asks nothing.
func TestIsolationNoteIsTheListeners(t *testing.T) {
	note := func(t *testing.T, s setup, host string) bool {
		t.Helper()
		_, srv, _ := startWith(t, s, isolationWorld(host)...)
		l := get[GatewayDetail](t, srv, gwDetail, 200).Listeners[0]
		if l.Name != "wide" || l.ComputedRoutes == nil || l.Computed == nil || l.Computed.State != "checked" {
			t.Fatalf("wide %v %+v", l.ComputedRoutes, l.Computed)
		}
		return l.IsolationNote
	}
	hide := setup{authz: AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Name != "ls1" })}
	for _, s := range []setup{{}, hide} {
		if in, out := note(t, s, "a.example.com"), note(t, s, "a.example.org"); !in || out {
			t.Errorf("hidden %v: note %v inside, %v outside", s.authz != nil, in, out)
		}
	}
	a := &countingAuthz{all: true}
	_, srv, _ := startWith(t, setup{authz: a}, isolationWorld("a.example.com")...)
	_, ref, _ := startWith(t, setup{}, isolationWorld("a.example.com")...)
	if got, want := wire(t, srv, gwDetail), wire(t, ref, gwDetail); !bytes.Equal(got, want) {
		t.Errorf("AllowAll differs:\n%s\n%s", got, want)
	}
	if n := len(a.take()); n != 0 {
		t.Errorf("AllowAll asked about %d refs", n)
	}
}

// Rules §8.2: {name: edge} and {name: edge, namespace: infra} name one
// listener under two report keys, so two checks; the controller accepted the
// first and rejected the second. Route detail shows each; the list's one
// ParentSummary merges them, worst first (design §7.3).
func TestListMergesWrittenRefChecks(t *testing.T) {
	short := edgeRef
	short.Namespace = ""
	dup := testutil.HTTPRoute("infra", "dup", []objects.ParentRef{short, edgeRef},
		testutil.AcceptedBy(short, testutil.Controller), rejectedBy(edgeRef, "NotAllowedByListeners"))
	_, srv, _ := startWith(t, setup{disputes: true}, viewAWorld(edgeWith([]objects.Listener{lst("http", 8080, "")}, up("http", 2)), dup)...)
	var detail []string
	for _, a := range get[RouteDetail](t, srv, routeDetail+"infra/dup", 200).Attachments {
		detail = append(detail, a.Result.Computed.State)
	}
	if !slices.Equal(detail, []string{"checked", "disagrees"}) {
		t.Fatalf("detail states %v", detail)
	}
	ps := routeByName(t, get[Page[RouteRow]](t, srv, underEdge, 200), "infra/dup").Parents
	if len(ps) != 1 || ps[0].Computed == nil {
		t.Fatalf("parents %+v", ps)
	}
	a := ps[0].Computed
	if a.Outcome != "accepted" || a.Computed.State != "disagrees" || len(a.Listeners) != 1 || a.Listeners[0].Computed.State != "disagrees" {
		t.Errorf("merged %+v", a)
	}
}

// The merge order and the restricted rule (design §7.3).
func TestMergeComputed(t *testing.T) {
	c := func(st string, cov ...string) Computed { return Computed{State: st, Coverage: cov, Note: st} }
	for _, tc := range []struct {
		a, b Computed
		want Computed
	}{
		{c("checked", "status-compatible"), c("checked", "not-verified"), Computed{State: "checked", Coverage: []string{"status-compatible", "not-verified"}, Note: "checked"}},
		{c("checked", "count-agrees"), c("disagrees"), Computed{State: "disagrees", Coverage: []string{"count-agrees"}, Note: "disagrees"}},
		{c("pending"), c("disagrees"), Computed{State: "disagrees", Note: "disagrees"}},
		{c("unknown"), c("pending"), Computed{State: "pending", Note: "pending"}},
		{c("checked"), c("unknown"), Computed{State: "unknown", Note: "unknown"}},
		{c("disagrees"), Computed{State: "restricted"}, Computed{State: "restricted"}},
		{Computed{State: "restricted"}, c("checked", "status-compatible"), Computed{State: "restricted"}},
	} {
		if got := mergeComputed(tc.a, tc.b); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("merge(%s, %s) = %+v, want %+v", tc.a.State, tc.b.State, got, tc.want)
		}
	}
}

// otherGateway is the owned Gateway infra/other, so a hidden object can name it instead of edge.
var otherRef = testutil.GwRef("infra", "other")

func otherGateway() *unstructured.Unstructured {
	return testutil.GatewayWithListeners("infra", "other", "kgateway", []objects.Listener{lst("web", 8080, "")}, up("web", 0))
}

// Rule 5 (user, 2026-10-26), Codex R2: a count's facts belong to its visible
// listener. A visible TCPRoute makes tls setting-unknown; a hidden route h
// naming edge or another Gateway moves neither the count, the visible
// TLSRoute's membership nor listener=tls. Rule 10: AllowAll asks nothing.
func TestHiddenRouteCannotMoveListenerFacts(t *testing.T) {
	hide := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Name != "h" })
	look := func(t *testing.T, hRef objects.ParentRef, authz Authorizer) (count, member, filter []byte) {
		t.Helper()
		s, objs := unknownCountWorld(edgeRef)
		s.authz = authz
		objs = append(objs, otherGateway(), testutil.HTTPRoute("shop", "h", []objects.ParentRef{hRef}))
		_, srv, _ := startWith(t, s, objs...)
		l := get[GatewayDetail](t, srv, gwDetail, 200).Listeners[0]
		if l.Name != "tls" || l.ComputedRoutes != nil || l.Computed == nil || l.Computed.State != "unknown" || l.Computed.Reason != string(computed.UnknownSetting) {
			t.Errorf("h on %s: tls count %v %+v", hRef.Name, l.ComputedRoutes, l.Computed)
		}
		m := routeByName(t, get[Page[RouteRow]](t, srv, underEdge, 200), "shop/tls").Parents[0].Computed
		if m == nil || len(m.Listeners) != 1 || m.Listeners[0].Computed.State != "unknown" {
			t.Errorf("h on %s: tls membership %+v", hRef.Name, m)
		}
		if p := get[Page[RouteRow]](t, srv, underEdge+"&listener=tls", 200); p.Total != 0 {
			t.Errorf("h on %s: listener=tls lists %v", hRef.Name, routeNames(p))
		}
		count, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		if member, err = json.Marshal(m); err != nil {
			t.Fatal(err)
		}
		return count, member, wire(t, srv, underEdge+"&listener=tls")
	}
	for _, authz := range []Authorizer{nil, hide} {
		c1, m1, f1 := look(t, edgeRef, authz)
		c2, m2, f2 := look(t, otherRef, authz)
		if !bytes.Equal(c1, c2) || !bytes.Equal(m1, m2) || !bytes.Equal(f1, f2) {
			t.Errorf("h moves a visible fact (hidden %v):\ncount %s\n      %s\nmember %s\n       %s", authz != nil, c1, c2, m1, m2)
		}
	}
	a := &countingAuthz{all: true}
	s, objs := unknownCountWorld(edgeRef)
	objs = append(objs, otherGateway(), testutil.HTTPRoute("shop", "h", []objects.ParentRef{edgeRef}))
	_, ref, _ := startWith(t, s, objs...)
	s.authz = a
	_, srv, _ := startWith(t, s, objs...)
	for _, p := range []string{underEdge, underEdge + "&listener=tls", gwDetail} {
		if got, want := wire(t, srv, p), wire(t, ref, p); !bytes.Equal(got, want) {
			t.Errorf("GET %s differs from AllowAll", p)
		}
		if n := len(a.take()); n != 0 {
			t.Errorf("GET %s asked the Authorizer about %d refs", p, n)
		}
	}
}

// Rule 5 (user, 2026-10-26), Codex R1: the isolation note belongs to its
// visible listener. wide always has it from narrow on the same port; a hidden
// accepted ListenerSet ls1 naming edge or another Gateway does not move it.
func TestHiddenListenerSetCannotMoveIsolationNote(t *testing.T) {
	hide := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Name != "ls1" })
	look := func(t *testing.T, parent objects.ParentRef) []byte {
		t.Helper()
		ls := admitAll(testutil.ListenerSet("infra", "ls1", parent, nil, up("extra", 0)))
		gw := edgeWith([]objects.Listener{lst("wide", 8081, "*.example.com"), lst("narrow", 8081, "a.example.com")}, up("wide", 0), up("narrow", 0))
		_, srv, _ := startWith(t, setup{authz: hide}, viewAWorld(gw, otherGateway(), ls)...)
		l := get[GatewayDetail](t, srv, gwDetail, 200).Listeners[0]
		if l.Name != "wide" || !l.IsolationNote || l.ComputedRoutes == nil || l.Computed == nil || l.Computed.State != "checked" {
			t.Errorf("ls1 on %s: wide note %v, routes %v, %+v", parent.Name, l.IsolationNote, l.ComputedRoutes, l.Computed)
		}
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if on, off := look(t, edgeRef), look(t, otherRef); !bytes.Equal(on, off) {
		t.Errorf("hidden ls1 moves wide:\n%s\n%s", on, off)
	}
}

// Rule 5 (user, 2026-10-26): a count with a hidden member shows the full
// number, which equals attachedRoutes (rule 8), with its check; the hidden
// route's row stays absent (rule 1). The rows' routes column counts it too.
func TestHiddenMemberIsCounted(t *testing.T) {
	for _, hidden := range []string{"", "cart", "via-ls"} {
		t.Run("hidden "+hidden, func(t *testing.T) {
			authz := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Name != hidden })
			_, srv, _ := startWith(t, setup{authz: authz}, memberWorld()...)
			gw := get[GatewayDetail](t, srv, gwDetail, 200).Listeners[0]
			ls := get[ListenerSetDetail](t, srv, lsDetail+"infra/ls1", 200).Listeners[0]
			for _, l := range []ListenerDTO{gw, ls} {
				if l.ComputedRoutes == nil || *l.ComputedRoutes != 1 || l.Status == nil || int32(*l.ComputedRoutes) != l.Status.AttachedRoutes ||
					l.Computed == nil || l.Computed.State != "checked" || !slices.Equal(l.Computed.Coverage, []string{"count-agrees"}) {
					t.Errorf("%s: routes %v, status %+v, computed %+v", l.Name, l.ComputedRoutes, l.Status, l.Computed)
				}
			}
			g := get[Page[GatewayListRow]](t, srv, listBase+"/gateways?q=edge", 200).Items[0]
			s := get[Page[ListenerSetRow]](t, srv, listBase+"/listenersets", 200).Items[0]
			if g.Routes == nil || *g.Routes != 1 || s.Routes == nil || *s.Routes != 1 {
				t.Errorf("routes columns %v / %v", g.Routes, s.Routes)
			}
			want := []string{"shop/cart"}
			if hidden == "cart" {
				want = []string{}
			}
			if p := get[Page[RouteRow]](t, srv, underEdge+"&listener=http", 200); !slices.Equal(routeNames(p), want) || p.Total != len(want) {
				t.Errorf("listener=http: %v total %d", routeNames(p), p.Total)
			}
			if p := get[Page[RouteRow]](t, srv, underEdge, 200); hidden == "cart" && slices.Contains(routeNames(p), "shop/cart") {
				t.Errorf("hidden shop/cart listed: %v", routeNames(p))
			}
		})
	}
	t.Run("AllowAll", func(t *testing.T) {
		a := &countingAuthz{all: true}
		_, srv, _ := startWith(t, setup{authz: a}, memberWorld()...)
		_, ref, _ := startWith(t, setup{}, memberWorld()...)
		for _, p := range []string{underEdge, underEdge + "&listener=http", listBase + "/routes?state=listener-unknown", listBase + "/gateways",
			listBase + "/listenersets", gwDetail, lsDetail + "infra/ls1", routeDetail + "shop/cart", routeDetail + "shop/via-ls"} {
			if got, want := wire(t, srv, p), wire(t, ref, p); !bytes.Equal(got, want) {
				t.Errorf("GET %s differs from AllowAll", p)
			}
			if n := len(a.take()); n != 0 {
				t.Errorf("GET %s asked the Authorizer about %d refs", p, n)
			}
		}
	})
}

// D-024 and design §7.4 rule 7: an orphan's evidence names no route on its
// paths, so a hidden wildcard parent never restricts it.
func TestOrphanUnderHiddenParentNotRestricted(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	rule := testutil.Rule(matchPath("/api"))
	rule["backendRefs"] = []any{testutil.DelegateWildcard("shop")}
	// api keeps no child, so it reports ResolvedRefs=False/BackendNotFound, as kgateway does (D-021 item 9).
	st := testutil.AcceptedBy(gw, testutil.Controller)
	st.Conds[1] = testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")
	objs := isolated(
		testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, st), rule),
		testutil.WithRules(testutil.HTTPRoute("shop", "o", nil), testutil.Rule(matchPath("/other"))))
	hideAPI := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Kind != "HTTPRoute" || ref.Name != "api" })
	for _, authz := range []Authorizer{nil, hideAPI} {
		_, srv, _ := startWith(t, setup{authz: authz}, objs...)
		var got []FindingDTO
		for _, f := range findingsOn(get[HealthResponse](t, srv, base+"/health", 200), "shop/o") {
			if f.Code == health.CodeDelegatedNotAttached {
				got = append(got, f)
			}
		}
		if len(got) != 1 || got[0].EvidenceRestricted || got[0].Detail != health.OrphanDetail {
			t.Fatalf("hidden parent %v: %+v", authz != nil, got)
		}
	}
}

// Design §6.2 and rules §9.3: the orphan conclusion is install-local. An entry
// from a shared controller name that names another install's parent does not
// suppress this install's orphan KGWUI-011; one naming this install's parent does.
func TestOrphanIgnoresOtherInstallsEntry(t *testing.T) {
	gw, gwB := testutil.GwRef("infra", "edge"), testutil.GwRef("infra-b", "edge")
	api, pb := testutil.HTTPRouteRef("shop", "api"), testutil.HTTPRouteRef("infra-b", "pb")
	ok := func(p objects.ParentRef) testutil.RouteParent { return testutil.AcceptedBy(p, testutil.Controller) }
	// api keeps no child, so it reports ResolvedRefs=False/BackendNotFound (D-021 item 9).
	apiSt := ok(gw)
	apiSt.Conds[1] = testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")
	// kgw2 shares the controller name and watches team=b or env=b; shared is in both scopes.
	d2 := testutil.ControllerDeployment("kgw2", "kgateway2", img, 1, 1,
		testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"b"}},{"matchLabels":{"env":"b"}}]`))
	world := func(entry objects.ParentRef) []runtime.Object {
		return isolated(testutil.Namespace("kgw2", nil), testutil.Namespace("infra-b", map[string]string{"team": "b"}),
			testutil.Namespace("shared", map[string]string{"team": "a", "env": "b"}),
			d2, testutil.ControllerPod(d2, "kgateway2-0", true), edgeIn("infra-b", 1), testutil.ProxyDeployment("infra-b", "edge", 1, 1),
			// A's api selects shared by wildcard and drops c (no /api match); B's pb keeps it.
			testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, apiSt), delegRule([]string{"/api"}, testutil.DelegateWildcard("shared"))),
			testutil.WithRules(testutil.HTTPRoute("infra-b", "pb", []objects.ParentRef{gwB}, ok(gwB)), delegRule([]string{"/pb"}, testutil.DelegateTo("shared", "c"))),
			leaf("shared", "c", nil, "/pb/c", ok(entry)))
	}
	orphans := func(h HealthResponse) []FindingDTO {
		var out []FindingDTO
		for _, f := range findingsOn(h, "shared/c") {
			if f.Code == health.CodeDelegatedNotAttached {
				out = append(out, f)
			}
		}
		return out
	}
	m := okMetrics("kgw.kgateway", "kgw2.kgateway2")
	hideAPI := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != api.Ref() })
	for _, authz := range []Authorizer{nil, hideAPI} {
		_, srv, _ := startWith(t, setup{metrics: m, authz: authz}, world(pb)...)
		h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)
		got := orphans(h)
		if len(got) != 1 || got[0].Detail != health.OrphanDetail || got[0].Severity != health.SeverityWarning ||
			got[0].Since == nil || got[0].EvidenceRestricted {
			t.Fatalf("A, entry under B's parent, hidden api %v: want one orphan warning, got %+v", authz != nil, got)
		}
		for _, c := range h.Checks {
			if c.Pending {
				t.Errorf("check %s pending", c.Name)
			}
		}
		if got := orphans(get[HealthResponse](t, srv, "/api/v1/installs/kgw2.kgateway2/health", 200)); len(got) != 0 {
			t.Errorf("B: want no orphan, got %+v", got)
		}
	}
	// The entry names A's own parent: an eligible entry of A (§4.2), so no orphan.
	_, srv, _ := startWith(t, setup{metrics: m, disputes: true}, world(api)...)
	if got := orphans(get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", 200)); len(got) != 0 {
		t.Errorf("A, entry under its own parent: want no orphan, got %+v", got)
	}
}
