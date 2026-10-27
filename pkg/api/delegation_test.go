package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/rulesets"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// delegRule is a rule matching each path as a PathPrefix, delegating to refs.
func delegRule(paths []string, refs ...map[string]any) map[string]any {
	ms := make([]map[string]any, 0, len(paths))
	for _, p := range paths {
		ms = append(ms, matchPath(p))
	}
	r := testutil.Rule(ms...)
	bs := make([]any, 0, len(refs))
	for _, b := range refs {
		bs = append(bs, b)
	}
	r["backendRefs"] = bs
	return r
}

// leaf is an HTTPRoute ns/name with one rule on path and the given parentRefs and status.
func leaf(ns, name string, parents []objects.ParentRef, path string, st ...testutil.RouteParent) *unstructured.Unstructured {
	return testutil.WithRules(testutil.HTTPRoute(ns, name, parents, st...), testutil.Rule(matchPath(path)))
}

// entry is a current status entry for p with Accepted status and reason.
func entry(p objects.ParentRef, status metav1.ConditionStatus, reason string) testutil.RouteParent {
	return testutil.RouteParent{Ref: p, Controller: testutil.Controller, Conds: []metav1.Condition{
		testutil.Cond("Accepted", status, reason), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}}
}

// delegURL is GET …/routes/…/ns/name/delegation with query q.
func delegURL(ns, name, q string) string { return routeDetail + ns + "/" + name + "/delegation" + q }

// rowKey names a row as "rule.ref:namespace/name"; a row with no child ends in ":".
func rowKey(r DelegationRow) string {
	k := fmt.Sprintf("%d.%d:", r.Rule, r.Ref)
	if r.Child != nil {
		k += r.Child.Namespace + "/" + r.Child.Name
	}
	return k
}

func rowsByKey(p Page[DelegationRow]) map[string]DelegationRow {
	m := map[string]DelegationRow{}
	for _, r := range p.Items {
		m[rowKey(r)] = r
	}
	return m
}

func rowKeys(p Page[DelegationRow]) []string {
	out := make([]string, 0, len(p.Items))
	for _, r := range p.Items {
		out = append(out, rowKey(r))
	}
	return out
}

func pathMatch(p string) MatchDTO { return MatchDTO{Path: &PathMatchDTO{Type: "PathPrefix", Value: p}} }

var (
	shopAPI  = testutil.HTTPRouteRef("shop", "api")
	kidsNS   = testutil.Namespace("kids", map[string]string{"team": "a"})
	edge2Ref = testutil.GwRef("infra", "edge2")
)

// outcomeWorld: the root shop/api attaches to edge and has one rule per
// outcome of view B (rules §5.5), each delegating through one name ref:
//
//	0 /api → kid attached       5 /api → loop attached; loop selects itself: cycle
//	1 /api → drop dropped       6 /p,/q → part partial
//	2 /api → forb forbidden     7 /zzz → kid dropped, unconfirmable
//	3 /api → ghost unresolved   8 /api → blog/b unresolved, not watched
//	4 /api → host rejected-hostnames
func outcomeWorld() []runtime.Object {
	api := testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		delegRule([]string{"/api"}, testutil.DelegateTo("shop", "kid")),
		delegRule([]string{"/api"}, testutil.DelegateTo("shop", "drop")),
		delegRule([]string{"/api"}, testutil.DelegateTo("shop", "forb")),
		delegRule([]string{"/api"}, testutil.DelegateTo("shop", "ghost")),
		delegRule([]string{"/api"}, testutil.DelegateTo("shop", "host")),
		delegRule([]string{"/api"}, testutil.DelegateTo("shop", "loop")),
		delegRule([]string{"/p", "/q"}, testutil.DelegateTo("shop", "part")),
		delegRule([]string{"/zzz"}, testutil.DelegateTo("shop", "kid")),
		delegRule([]string{"/api"}, testutil.DelegateTo("blog", "b")))
	loop := testutil.WithRules(testutil.HTTPRoute("shop", "loop", nil, testutil.AcceptedBy(shopAPI, testutil.Controller)),
		delegRule([]string{"/api/loop"}, testutil.DelegateTo("shop", "loop")))
	return isolated(api, loop,
		leaf("shop", "kid", nil, "/api/v1", testutil.AcceptedBy(shopAPI, testutil.Controller)),
		leaf("shop", "drop", nil, "/other"),
		leaf("shop", "forb", []objects.ParentRef{testutil.HTTPRouteRef("shop", "else")}, "/api/f"),
		testutil.WithHostnames(leaf("shop", "host", nil, "/api/h", entry(shopAPI, metav1.ConditionFalse, "UnsupportedValue")), "h.example.org"),
		leaf("shop", "part", nil, "/p/x", testutil.AcceptedBy(shopAPI, testutil.Controller)),
		leaf("blog", "b", nil, "/b"))
}

// Design §7.3: one row per outcome, with its reason, contexts, check and the
// controller's signal.
func TestDelegationEndpoint(t *testing.T) {
	_, srv, _ := startWith(t, setup{disputes: true}, outcomeWorld()...)
	p := get[Page[DelegationRow]](t, srv, delegURL("shop", "api", ""), 200)
	want := []string{"0.0:shop/kid", "1.0:shop/drop", "2.0:shop/forb", "3.0:", "4.0:shop/host", "5.0:shop/loop", "6.0:shop/part", "7.0:shop/kid", "8.0:"}
	if got := rowKeys(p); !slices.Equal(got, want) || p.Total != len(want) || p.Install != "kgw.kgateway" || p.Next != "" || p.Prev != "" {
		t.Fatalf("rows %v total %d", got, p.Total)
	}
	rows := rowsByKey(p)
	for key, w := range map[string]struct{ outcome, reason string }{
		"0.0:shop/kid":  {"attached", ""},
		"1.0:shop/drop": {"dropped", "the route's rule 1: path /other is outside the parent's /api"},
		"2.0:shop/forb": {"forbidden", reasonForbidden},
		"3.0:":          {"unresolved", reasonMissing},
		"4.0:shop/host": {"rejected-hostnames", reasonRejectedHostnames},
		"5.0:shop/loop": {"attached", ""},
		"6.0:shop/part": {"partial", reasonPartial + "; the route's rule 1: path /p/x is outside the parent's /q"},
		"7.0:shop/kid":  {"dropped", "the route's rule 1: path /api/v1 is outside the parent's /zzz"},
		"8.0:":          {"unresolved", reasonUnwatched},
	} {
		if r := rows[key]; r.Outcome != w.outcome || r.Reason != w.reason {
			t.Errorf("%s: outcome %q reason %q, want %q %q", key, r.Outcome, r.Reason, w.outcome, w.reason)
		}
	}

	kid := rows["0.0:shop/kid"]
	if kid.RefAsWritten != (DelegationRefDTO{Mode: objects.DelegationName, Namespace: "shop", Name: "kid"}) || kid.Allows != "any" ||
		kid.Child.Link != LinkDetail || kid.ContextsTruncated {
		t.Errorf("kid row %+v", kid)
	}
	wantCtx := []DelegationContext{{ParentMatch: ptr(pathMatch("/api")), Outcome: "attached",
		Path: []PathStepDTO{{Ref: LinkedRef{Ref: route("shop", "api"), Link: LinkDetail}, Match: pathMatch("/api")}}}}
	if !reflect.DeepEqual(kid.Contexts, wantCtx) {
		t.Errorf("kid contexts %+v", kid.Contexts)
	}
	if c := kid.Computed; c.By != ComputedBy || c.Rules != "kgateway-2.4" || c.Rule != ruleDelegation || c.State != "checked" ||
		!slices.Equal(c.Coverage, []string{"status-compatible"}) {
		t.Errorf("kid computed %+v", c)
	}
	// The controller's signal: kid's entry under the row's key and api's ResolvedRefs.
	if e := kid.Observed.Entry; e == nil || e.Target.Ref != route("shop", "api") || !e.Ours || e.ControllerName != testutil.Controller || len(e.Conditions) != 3 {
		t.Errorf("kid entry %+v", e)
	}
	if c := kid.Observed.ParentCondition; c == nil || c.Type != "ResolvedRefs" || c.Status != "True" {
		t.Errorf("api ResolvedRefs %+v", c)
	}
	if o := rows["1.0:shop/drop"].Observed; o.Entry != nil || o.ParentCondition == nil {
		t.Errorf("drop observed %+v", o)
	}
	if r := rows["2.0:shop/forb"]; r.Allows != "other" {
		t.Errorf("forb allows %q", r.Allows)
	}
	for _, key := range []string{"3.0:", "8.0:"} {
		if r := rows[key]; r.Child != nil || r.Allows != "" || r.Observed.Entry != nil {
			t.Errorf("%s: %+v", key, r)
		}
	}
	if r := rows["4.0:shop/host"]; r.Observed.Entry == nil || r.Observed.Entry.Conditions[0].Reason != "UnsupportedValue" {
		t.Errorf("host observed %+v", r.Observed)
	}
	part := rows["6.0:shop/part"]
	if len(part.Contexts) != 2 || part.Contexts[0].Outcome != "attached" || part.Contexts[1].Outcome != "dropped" ||
		part.Contexts[1].Reason != "the route's rule 1: path /p/x is outside the parent's /q" || !reflect.DeepEqual(part.Contexts[1].ParentMatch, ptr(pathMatch("/q"))) {
		t.Errorf("part contexts %+v", part.Contexts)
	}
	// An unconfirmable row says so in its check's note (design §5.2).
	if c := rows["7.0:shop/kid"].Computed; !strings.Contains(c.Note, noteUnconfirmable) {
		t.Errorf("unconfirmable row computed %+v", c)
	}

	loop := get[Page[DelegationRow]](t, srv, delegURL("shop", "loop", ""), 200)
	if len(loop.Items) != 1 || loop.Items[0].Outcome != "cycle" || loop.Items[0].Reason != reasonCycle || len(loop.Items[0].Contexts[0].Path) != 2 {
		t.Fatalf("loop rows %+v", loop.Items)
	}

	// A scope-unknown install makes every row unknown (D-024).
	_, unk, _ := startWith(t, setup{disputes: true}, unknownScope(outcomeWorld())...)
	for _, r := range get[Page[DelegationRow]](t, unk, delegURL("shop", "api", ""), 200).Items {
		if r.Outcome != "unknown" || r.Computed.State != "unknown" || r.Computed.Reason != string(computed.UnknownScope) {
			t.Errorf("scope unknown %s: %q %+v", rowKey(r), r.Outcome, r.Computed)
		}
	}
}

func ptr[T any](v T) *T { return &v }

// Design §7.2: rule and ref are integers, outcome a row outcome; no sort;
// paging over the fixed order.
func TestDelegationParams(t *testing.T) {
	_, srv, _ := startWith(t, setup{disputes: true}, outcomeWorld()...)
	for _, q := range []string{"?rule=x", "?ref=1.5", "?rule=-1", "?ref=-2", "?outcome=gone", "?sort=name", "?rule=1&rule=2",
		"?limit=0", "?limit=201", "?cursor=abc", "?parent=gateway.networking.k8s.io/Gateway/infra/edge", "?outcome=attached&outcome=x"} {
		get[ErrorResponse](t, srv, delegURL("shop", "api", q), http.StatusBadRequest)
	}
	// An object-list cursor is not a delegation cursor.
	if next := get[Page[RouteRow]](t, srv, listBase+"/routes?limit=1", 200).Next; next != "" {
		get[ErrorResponse](t, srv, delegURL("shop", "api", "?cursor="+next), http.StatusBadRequest)
	}
	for _, path := range []string{delegURL("shop", "nope", ""), base + "/routes/gateway.networking.k8s.io/GRPCRoute/shop/api/delegation",
		"/api/v1/installs/nope/routes/gateway.networking.k8s.io/HTTPRoute/shop/api/delegation"} {
		get[ErrorResponse](t, srv, path, http.StatusNotFound)
	}
	if p := get[Page[DelegationRow]](t, srv, delegURL("shop", "api", "?rule=6&ref=0"), 200); !slices.Equal(rowKeys(p), []string{"6.0:shop/part"}) || p.Total != 1 {
		t.Errorf("rule=6&ref=0: %v", rowKeys(p))
	}
	if p := get[Page[DelegationRow]](t, srv, delegURL("shop", "api", "?outcome=unresolved&outcome=cycle"), 200); !slices.Equal(rowKeys(p), []string{"3.0:", "8.0:"}) {
		t.Errorf("outcome filter: %v", rowKeys(p))
	}
	// Forward over pages of 2, then back from the end: the same rows.
	var fwd []string
	cursor := ""
	for i := 0; ; i++ {
		p := get[Page[DelegationRow]](t, srv, delegURL("shop", "api", "?limit=2"+cursor), 200)
		fwd = append(fwd, rowKeys(p)...)
		if p.Total != 9 {
			t.Fatalf("total %d", p.Total)
		}
		if p.Next == "" {
			var back []string
			for prev := p.Prev; prev != ""; {
				q := get[Page[DelegationRow]](t, srv, delegURL("shop", "api", "?limit=2&cursor="+prev), 200)
				back = append(rowKeys(q), back...)
				prev = q.Prev
			}
			if want := fwd[:len(fwd)-len(p.Items)]; !slices.Equal(back, want) {
				t.Fatalf("backward %v, want %v", back, want)
			}
			break
		}
		cursor = "&cursor=" + p.Next
	}
	if len(fwd) != 9 {
		t.Fatalf("forward %v", fwd)
	}
}

// PF8: a client sends the delegation filters only when /meta lists them.
func TestMetaAdvertisesDelegation(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, isolated()...)
	m := get[MetaResponse](t, srv, "/api/v1/meta", 200)
	if !slices.Equal(m.Filters["delegation"], []string{"rule", "ref", "outcome"}) || !slices.Equal(m.Filters["routes"], []string{"listener"}) {
		t.Fatalf("filters %v", m.Filters)
	}
}

// boundsRules wraps the real rule sets with other bounds for views B and C.
type boundsRules struct {
	computed.Rules
	b computed.Bounds
}

func (r boundsRules) Delegation(in computed.DelegationInput) computed.DelegationResult {
	in.Bounds = r.b
	return r.Rules.Delegation(in)
}

// Rules §5.7, §6 through route detail: view C per parent edge.
func TestEffectiveOnRoutePage(t *testing.T) {
	inherit := leaf("shop", "inh", nil, "/v2", testutil.AcceptedBy(shopAPI, testutil.Controller))
	inherit.SetAnnotations(map[string]string{objects.InheritParentMatcherAnnotation: "true"})
	api := testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		delegRule([]string{"/api"}, testutil.DelegateTo("shop", "kid"), testutil.DelegateTo("shop", "inh")),
		delegRule([]string{"/zzz"}, testutil.DelegateTo("shop", "kid")))
	objs := isolated(api, inherit, leaf("shop", "kid", nil, "/api/v1", testutil.AcceptedBy(shopAPI, testutil.Controller)))
	e, srv, _ := startWith(t, setup{disputes: true}, objs...)

	d := get[RouteDetail](t, srv, routeDetail+"shop/kid", 200)
	notVerified := Computed{By: ComputedBy, Rules: "kgateway-2.4", Rule: ruleEffective, State: "checked", Coverage: []string{"not-verified"}}
	want := []EffectiveDTO{{Parent: LinkedRef{Ref: route("shop", "api"), Link: LinkDetail}, Mode: computed.ModeDefault, Computed: notVerified,
		Contexts: []EffectiveContext{
			{ParentMatch: ptr(pathMatch("/api")), Rules: []EffectiveRuleDTO{{Index: 0, Kept: true, Matches: []MatchDTO{pathMatch("/api/v1")}}}},
			{ParentMatch: ptr(pathMatch("/zzz")), Rules: []EffectiveRuleDTO{{Index: 0, Matches: []MatchDTO{}, Reason: "path /api/v1 is outside the parent's /zzz"}}},
		}}}
	if !reflect.DeepEqual(d.Effective, want) || d.EffectiveTruncated {
		t.Fatalf("kid effective %+v", d.Effective)
	}
	// It projects the traversal: one context per retained context, in order.
	ce := e.Current().Model.Installs[0].Computed.Delegation.Effective[route("shop", "kid")]
	if len(ce.Edges) != 1 || len(ce.Edges[0].Contexts) != len(d.Effective[0].Contexts) {
		t.Fatalf("traversal %+v", ce)
	}
	if in := get[RouteDetail](t, srv, routeDetail+"shop/inh", 200).Effective; len(in) != 1 || in[0].Mode != computed.ModeInherit ||
		!reflect.DeepEqual(in[0].Contexts[0].Rules, []EffectiveRuleDTO{{Index: 0, Kept: true, Matches: []MatchDTO{pathMatch("/api/v2")}}}) {
		t.Fatalf("inherit effective %+v", in)
	}
	// A route no parent selects has no effective block.
	if b := wire(t, srv, routeDetail+"shop/api"); bytes.Contains(b, []byte(`"effective"`)) {
		t.Errorf("api has an effective block")
	}
	// The ref's counts and its merged check.
	dl := get[RouteDetail](t, srv, routeDetail+"shop/api", 200).Rules[0].BackendRefs[0].Delegation
	if dl.Selected == nil || *dl.Selected != 1 || dl.Attached == nil || *dl.Attached != 1 || dl.Computed == nil || dl.Computed.Rule != ruleDelegation {
		t.Errorf("api ref 0 %+v", dl)
	}

	// At the install's retention cap an edge keeps no contexts: [] and truncated, never complete.
	b := computed.DefaultBounds
	b.ContextsPerInstall = 1
	_, capped, _ := startWith(t, setup{disputes: true, rulesFor: func(v string) computed.Selection {
		s := rulesets.Select(v)
		s.Rules = boundsRules{Rules: s.Rules, b: b}
		return s
	}}, objs...)
	var empty int
	var raw []byte
	for _, ref := range []string{"shop/kid", "shop/inh"} {
		for _, ed := range get[RouteDetail](t, capped, routeDetail+ref, 200).Effective {
			if len(ed.Contexts) == 0 {
				empty++
				if !ed.ContextsTruncated {
					t.Errorf("%s: an edge with no contexts is not truncated", ref)
				}
			}
		}
		raw = append(raw, wire(t, capped, routeDetail+ref)...)
	}
	if empty == 0 || !bytes.Contains(raw, []byte(`"contexts": []`)) {
		t.Errorf("no edge at the cap sends contexts: [] (%d empty)", empty)
	}

	// A route with 21 parent edges keeps 20 and says so.
	many := []runtime.Object{leaf("shop", "fan", nil, "/f/x")}
	for i := range 21 {
		many = append(many, testutil.WithRules(testutil.HTTPRoute("shop", fmt.Sprintf("p%02d", i), []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
			delegRule([]string{"/f"}, testutil.DelegateTo("shop", "fan"))))
	}
	_, fan, _ := startWith(t, setup{disputes: true}, isolated(many...)...)
	if fd := get[RouteDetail](t, fan, routeDetail+"shop/fan", 200); len(fd.Effective) != 20 || !fd.EffectiveTruncated || fd.Effective[19].Parent.Name != "p19" {
		t.Fatalf("fan: %d edges, truncated %v", len(fd.Effective), fd.EffectiveTruncated)
	}
}

// selectorRootWorld: shop/api is a root through edge2's listener sel, which
// selects namespaces with zone=in; it also attaches to edge, so it stays a
// root whatever shop's labels are. api delegates /api to shop/kid, which
// fits, and to shop/drop, which does not. shop's labels are zone.
func selectorRootWorld(zone string) []runtime.Object {
	sel := lst("sel", 8080, "")
	sel.AllowedRoutes.Namespaces = objects.RouteNamespaces{From: "Selector", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"zone": "in"}}}
	edge2 := testutil.GatewayWithListeners("infra", "edge2", "kgateway", []objects.Listener{sel}, up("sel", 1))
	api := testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{edgeRef, edge2Ref},
		testutil.AcceptedBy(edgeRef, testutil.Controller), testutil.AcceptedBy(edge2Ref, testutil.Controller)),
		delegRule([]string{"/api"}, testutil.DelegateTo("shop", "kid")),
		delegRule([]string{"/api"}, testutil.DelegateTo("shop", "drop")))
	objs := isolated(edge2, api, leaf("shop", "kid", nil, "/api/v1", testutil.AcceptedBy(shopAPI, testutil.Controller)), leaf("shop", "drop", nil, "/other"))
	for i, o := range objs {
		if ns, ok := o.(*corev1.Namespace); ok && ns.Name == "shop" {
			objs[i] = testutil.Namespace("shop", map[string]string{"team": "a", "zone": zone})
		}
	}
	return objs
}

// restrictedComputed is a fact whose dependency is hidden (design §7.4 rule
// 4): no rule, reason or note, since each could vary with hidden data.
func restrictedComputed(string) Computed {
	return Computed{By: ComputedBy, Rules: "kgateway-2.4", State: "restricted"}
}

// withoutHealth is GET path's canonical JSON without any "health" field or
// "disagrees" flag, which cover every finding (design §7.4 rule 9), and
// without deniedInInstall, the one hidden count a list sends (rule 2).
func withoutHealth(t *testing.T, srv *httptest.Server, path string) []byte {
	t.Helper()
	var strip func(v any)
	strip = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			delete(x, "health")
			delete(x, "deniedInInstall")
			if fs, ok := x["flags"].([]any); ok {
				x["flags"] = slices.DeleteFunc(fs, func(f any) bool { return f == "disagrees" })
			}
			for _, e := range x {
				strip(e)
			}
		case []any:
			for _, e := range x {
				strip(e)
			}
		}
	}
	var body any
	if err := json.Unmarshal(wire(t, srv, path), &body); err != nil {
		t.Fatal(err)
	}
	strip(body)
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Rules §11.2: a root behind a Selector listener whose Namespace the caller
// cannot view restricts every descendant context, row and effective edge.
// Flipping that Namespace's labels changes nothing the caller sees.
func TestHiddenRootNamespace(t *testing.T) {
	_, all, _ := startWith(t, setup{disputes: true}, selectorRootWorld("in")...)
	if r := get[Page[DelegationRow]](t, all, delegURL("shop", "api", ""), 200).Items; r[0].Outcome != "attached" || r[1].Outcome != "dropped" {
		t.Fatalf("unrestricted rows %+v", r)
	}
	var bodies [][]byte
	for _, zone := range []string{"in", "out"} {
		_, srv, _ := startWith(t, setup{disputes: true, authz: hideNamespaces}, selectorRootWorld(zone)...)
		p := get[Page[DelegationRow]](t, srv, delegURL("shop", "api", ""), 200)
		if p.Total != 2 {
			t.Fatalf("zone %s: total %d", zone, p.Total)
		}
		for _, r := range p.Items {
			if r.Outcome != "" || r.Reason != "" || !reflect.DeepEqual(r.Computed, restrictedComputed(ruleDelegation)) || r.Observed.ParentCondition != nil ||
				len(r.Contexts) != 1 || !reflect.DeepEqual(r.Contexts[0], DelegationContext{Restricted: true}) {
				t.Errorf("zone %s: row %s %+v", zone, rowKey(r), r)
			}
		}
		d := get[RouteDetail](t, srv, routeDetail+"shop/kid", 200)
		if len(d.Effective) != 1 || !reflect.DeepEqual(d.Effective[0].Computed, restrictedComputed(ruleEffective)) || len(d.Effective[0].Contexts) != 1 ||
			!d.Effective[0].Contexts[0].Restricted || d.Effective[0].Contexts[0].ParentMatch != nil {
			t.Errorf("zone %s: kid effective %+v", zone, d.Effective)
		}
		dl := get[RouteDetail](t, srv, routeDetail+"shop/api", 200).Rules[0].BackendRefs[0].Delegation
		if dl.Selected == nil || *dl.Selected != 1 || dl.Attached != nil || dl.Computed == nil || !reflect.DeepEqual(*dl.Computed, restrictedComputed(ruleDelegation)) {
			t.Errorf("zone %s: ref %+v", zone, dl)
		}
		if rows := get[Page[RouteRow]](t, srv, listBase+"/routes?state=dropped", 200); rows.Total != 0 {
			t.Errorf("zone %s: state=dropped lists %v", zone, routeNames(rows))
		}
		bodies = append(bodies, slices.Concat(wire(t, srv, delegURL("shop", "api", "")), wire(t, srv, routeDetail+"shop/kid"),
			wire(t, srv, delegURL("shop", "api", "?outcome=dropped")), wire(t, srv, listBase+"/routes?state=dropped")))
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Errorf("the hidden Namespace's labels change what the caller sees")
	}
	// The flip is real: zone=out drops api's edge2 key, which an unrestricted caller sees.
	_, out, _ := startWith(t, setup{disputes: true}, selectorRootWorld("out")...)
	if bytes.Equal(wire(t, all, delegURL("shop", "api", "")), wire(t, out, delegURL("shop", "api", ""))) {
		t.Errorf("zone in and out look the same unrestricted")
	}
}

// onlySelectorWorld: shop/api attaches only through edge2's Selector listener
// (zone=in), so whether it is a root depends on shop's labels, zone. api
// delegates /api to shop/mid, which delegates /api/m to shop/kid.
func onlySelectorWorld(zone string) []runtime.Object {
	sel := lst("sel", 8080, "")
	sel.AllowedRoutes.Namespaces = objects.RouteNamespaces{From: "Selector", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"zone": "in"}}}
	edge2 := testutil.GatewayWithListeners("infra", "edge2", "kgateway", []objects.Listener{sel}, up("sel", 1))
	api := testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{edge2Ref}, testutil.AcceptedBy(edge2Ref, testutil.Controller)),
		delegRule([]string{"/api"}, testutil.DelegateTo("shop", "mid")))
	mid := testutil.WithRules(testutil.HTTPRoute("shop", "mid", nil, testutil.AcceptedBy(shopAPI, testutil.Controller)),
		delegRule([]string{"/api/m"}, testutil.DelegateTo("shop", "kid")))
	objs := isolated(edge2, api, mid, leaf("shop", "kid", nil, "/api/m/k", testutil.AcceptedBy(testutil.HTTPRouteRef("shop", "mid"), testutil.Controller)))
	for i, o := range objs {
		if ns, ok := o.(*corev1.Namespace); ok && ns.Name == "shop" {
			objs[i] = testutil.Namespace("shop", map[string]string{"team": "a", "zone": zone})
		}
	}
	return objs
}

// Fix round 1: whether a route is a root at all can depend on a hidden
// Namespace. Rows no context reached take the view A deps of every
// top-level route that reaches their parent, and an unreached edge is a
// restricted placeholder, so the caller sees the same either way.
func TestUnreachedRowsFollowRootEligibility(t *testing.T) {
	// api's own detail and list row too, without health and the disagrees
	// flag: they follow api's status (rule 9), which this world leaves accepted.
	paths := []string{delegURL("shop", "api", ""), delegURL("shop", "mid", ""), routeDetail + "shop/mid", routeDetail + "shop/kid",
		delegURL("shop", "mid", "?outcome=unknown"), routeDetail + "shop/api", listBase + "/routes?namespace=shop"}
	var bodies [][]byte
	for _, zone := range []string{"in", "out"} {
		_, srv, _ := startWith(t, setup{disputes: true, authz: hideNamespaces}, onlySelectorWorld(zone)...)
		for _, ref := range []string{"api", "mid"} {
			r := get[Page[DelegationRow]](t, srv, delegURL("shop", ref, ""), 200).Items
			if len(r) != 1 || r[0].Computed.State != "restricted" || r[0].Outcome != "" {
				t.Errorf("zone %s: %s rows %+v", zone, ref, r)
			}
		}
		for _, ref := range []string{"mid", "kid"} {
			e := get[RouteDetail](t, srv, routeDetail+"shop/"+ref, 200).Effective
			if len(e) != 1 || e[0].Computed.State != "restricted" || !reflect.DeepEqual(e[0].Contexts, []EffectiveContext{{Restricted: true}}) {
				t.Errorf("zone %s: %s effective %+v", zone, ref, e)
			}
		}
		var b []byte
		for _, p := range paths {
			b = append(b, withoutHealth(t, srv, p)...)
		}
		bodies = append(bodies, b)
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Errorf("the hidden Namespace's labels change what the caller sees")
	}
	// Unrestricted, zone=out is not reached and has no edge.
	_, out, _ := startWith(t, setup{disputes: true}, onlySelectorWorld("out")...)
	if r := get[Page[DelegationRow]](t, out, delegURL("shop", "mid", ""), 200).Items; len(r) != 1 || r[0].Outcome != "unknown" || r[0].Computed.Reason != string(computed.UnknownNotReached) {
		t.Errorf("unrestricted unreached row %+v", r)
	}
	if e := get[RouteDetail](t, out, routeDetail+"shop/kid", 200).Effective; e != nil {
		t.Errorf("unrestricted unreached edge %+v", e)
	}
}

// D-024, rule 7: a merged KGWUI-011 (status and computed trigger) names a
// parent that is on the child's delegation paths, so a hidden parent does
// not restrict its evidence.
func TestMergedTriggerUnderHiddenParent(t *testing.T) {
	api := testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		delegRule([]string{"/api"}, testutil.DelegateTo("shop", "c")))
	objs := isolated(api, leaf("shop", "c", []objects.ParentRef{shopAPI}, "/other"))
	hideAPI := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != route("shop", "api") })
	trigger := func(t *testing.T, authz Authorizer) FindingDTO {
		t.Helper()
		_, srv, _ := startWith(t, setup{disputes: true, authz: authz}, objs...)
		for _, f := range findingsOn(get[HealthResponse](t, srv, base+"/health", 200), "shop/c") {
			if f.Code == health.CodeDelegatedNotAttached {
				return f
			}
		}
		t.Fatal("no KGWUI-011 on shop/c")
		return FindingDTO{}
	}
	full, f := trigger(t, nil), trigger(t, hideAPI)
	if !strings.Contains(full.Detail, "UI computes") || f.EvidenceRestricted || f.Detail != full.Detail {
		t.Fatalf("hidden parent: %+v, want %+v", f, full)
	}
}

// statusRootWorld: shop/sr attaches to the ListenerSet ls1, which has no
// status, so view A is unknown and sr is a status root through its entry for
// ls1 (rules §5.1). sr delegates /s to shop/sk. ls1's listener is named ln.
func statusRootWorld(ln string) []runtime.Object {
	ls := testutil.ListenerSet("infra", "ls1", edgeRef, []metav1.Condition{}, testutil.Listener{Name: ln})
	sr := testutil.WithRules(testutil.HTTPRoute("shop", "sr", []objects.ParentRef{ls1Ref}, testutil.AcceptedBy(ls1Ref, testutil.Controller)),
		delegRule([]string{"/s"}, testutil.DelegateTo("shop", "sk")))
	return isolated(ls, sr, leaf("shop", "sk", nil, "/s/k", testutil.AcceptedBy(testutil.HTTPRouteRef("shop", "sr"), testutil.Controller)))
}

// Rules §11.2: a status root whose target the caller cannot view restricts
// its descendants; the target's spec changes nothing the caller sees. While
// the target has no status, nothing of its spec decides a view B or C fact,
// so this flip guards the projection only.
func TestStatusRootTargetDenied(t *testing.T) {
	hideLS := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Kind != "ListenerSet" })
	_, all, _ := startWith(t, setup{disputes: true}, statusRootWorld("extra")...)
	if r := get[Page[DelegationRow]](t, all, delegURL("shop", "sr", ""), 200).Items; len(r) != 1 || r[0].Outcome != "attached" {
		t.Fatalf("unrestricted rows %+v", r)
	}
	var bodies [][]byte
	for _, ln := range []string{"extra", "other"} {
		_, srv, _ := startWith(t, setup{disputes: true, authz: hideLS}, statusRootWorld(ln)...)
		r := get[Page[DelegationRow]](t, srv, delegURL("shop", "sr", ""), 200).Items
		if len(r) != 1 || r[0].Outcome != "" || r[0].Computed.State != "restricted" || !reflect.DeepEqual(r[0].Contexts[0], DelegationContext{Restricted: true}) {
			t.Fatalf("listener %s: rows %+v", ln, r)
		}
		if e := get[RouteDetail](t, srv, routeDetail+"shop/sk", 200).Effective; len(e) != 1 || e[0].Computed.State != "restricted" || !e[0].Contexts[0].Restricted {
			t.Errorf("listener %s: effective %+v", ln, e)
		}
		bodies = append(bodies, slices.Concat(wire(t, srv, delegURL("shop", "sr", "")), wire(t, srv, routeDetail+"shop/sk"), wire(t, srv, routeDetail+"shop/sr")))
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Errorf("the hidden ListenerSet's spec changes what the caller sees")
	}
}

// ancestorWorld: edge → shop/api → shop/mid → shop/leaf, each delegating one
// level down under /api, /api/m and /api/m/l; leaf delegates to every route
// in kids (k1 fits; k2's hostnames are k2Host). api also delegates to
// shop/sib on sibPath. mid, sib and k2 are the hidden routes of hideAncestor.
func ancestorWorld(sibPath, k2Host string) []runtime.Object {
	midRef, leafRef := testutil.HTTPRouteRef("shop", "mid"), testutil.HTTPRouteRef("shop", "leaf")
	api := testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		delegRule([]string{"/api"}, testutil.DelegateTo("shop", "mid")), delegRule([]string{"/api"}, testutil.DelegateTo("shop", "sib")))
	mid := testutil.WithRules(testutil.HTTPRoute("shop", "mid", nil, testutil.AcceptedBy(shopAPI, testutil.Controller)),
		delegRule([]string{"/api/m"}, testutil.DelegateTo("shop", "leaf")))
	lf := testutil.WithRules(testutil.HTTPRoute("shop", "leaf", nil, testutil.AcceptedBy(midRef, testutil.Controller)),
		delegRule([]string{"/api/m/l"}, testutil.DelegateWildcard("kids")))
	k2 := leaf("kids", "k2", nil, "/api/m/l/2", testutil.AcceptedBy(leafRef, testutil.Controller))
	if k2Host != "" {
		testutil.WithHostnames(k2, k2Host)
	}
	return isolated(kidsNS, api, mid, lf, k2, leaf("shop", "sib", nil, sibPath, testutil.AcceptedBy(shopAPI, testutil.Controller)),
		leaf("kids", "k1", nil, "/api/m/l/1", testutil.AcceptedBy(leafRef, testutil.Controller)))
}

var hideAncestor = AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool {
	return ref.Kind != "HTTPRoute" || !slices.Contains([]string{"shop/mid", "shop/sib", "kids/k2"}, ref.Namespace+"/"+ref.Name)
})

// D-024: under a hidden ancestor route, a visible child's rows, contexts and
// effective edges show the ancestor's name (link none) and the match it
// delegated under, and nothing else; total counts visible children and the
// ref's one restricted row, since leaf's wildcard reads the hidden k2. Hidden
// routes that are not ancestors change nothing the caller sees.
func TestHiddenAncestorShowsInheritedMatch(t *testing.T) {
	_, srv, _ := startWith(t, setup{disputes: true, authz: hideAncestor}, ancestorWorld("/api/s", "")...)
	// The hidden k2 makes the wildcard selection-restricted: one restricted row first.
	p := get[Page[DelegationRow]](t, srv, delegURL("shop", "leaf", ""), 200)
	if !slices.Equal(rowKeys(p), []string{"0.0:", "0.0:kids/k1"}) || p.Total != 2 || p.Items[0].Computed.State != "restricted" {
		t.Fatalf("leaf rows %v total %d", rowKeys(p), p.Total)
	}
	r := p.Items[1]
	wantPath := []PathStepDTO{
		{Ref: LinkedRef{Ref: route("shop", "api"), Link: LinkDetail}, Match: pathMatch("/api")},
		{Ref: LinkedRef{Ref: route("shop", "mid"), Link: LinkNone}, Match: pathMatch("/api/m")},
		{Ref: LinkedRef{Ref: route("shop", "leaf"), Link: LinkDetail}, Match: pathMatch("/api/m/l")},
	}
	if r.Outcome != "attached" || r.Computed.State == "restricted" || len(r.Contexts) != 1 || r.Contexts[0].Restricted ||
		!reflect.DeepEqual(r.Contexts[0].Path, wantPath) || !reflect.DeepEqual(r.Contexts[0].ParentMatch, ptr(pathMatch("/api/m/l"))) {
		t.Fatalf("k1 row %+v", r)
	}
	d := get[RouteDetail](t, srv, routeDetail+"shop/leaf", 200)
	if len(d.Effective) != 1 || d.Effective[0].Parent != (LinkedRef{Ref: route("shop", "mid"), Link: LinkNone}) || d.Effective[0].Computed.State != "checked" ||
		!reflect.DeepEqual(d.Effective[0].Contexts[0].ParentMatch, ptr(pathMatch("/api/m"))) {
		t.Fatalf("leaf effective %+v", d.Effective)
	}
	if dl := d.Rules[0].BackendRefs[0].Delegation; dl.Selected == nil || *dl.Selected != 1 || dl.Attached == nil || *dl.Attached != 1 || dl.Computed.State != "restricted" {
		t.Errorf("leaf ref %+v", dl)
	}
	// Nothing else of mid: no link, no rules, no status.
	for _, path := range []string{delegURL("shop", "mid", ""), routeDetail + "shop/mid"} {
		get[ErrorResponse](t, srv, path, http.StatusNotFound)
	}

	var bodies [][]byte
	for _, w := range []struct{ sib, host string }{{"/api/s", ""}, {"/nope", "k2.example.org"}} {
		_, s, _ := startWith(t, setup{disputes: true, authz: hideAncestor}, ancestorWorld(w.sib, w.host)...)
		bodies = append(bodies, slices.Concat(wire(t, s, delegURL("shop", "leaf", "")), wire(t, s, routeDetail+"shop/leaf"),
			wire(t, s, delegURL("shop", "api", "")), wire(t, s, delegURL("shop", "leaf", "?outcome=attached"))))
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Errorf("a hidden route that is not an ancestor changes what the caller sees")
	}
	// The flip is real: k2's hostnames reject it, and sib drops.
	_, a, _ := startWith(t, setup{disputes: true}, ancestorWorld("/api/s", "")...)
	_, b, _ := startWith(t, setup{disputes: true}, ancestorWorld("/nope", "k2.example.org")...)
	for _, path := range []string{delegURL("shop", "leaf", ""), delegURL("shop", "api", "")} {
		if bytes.Equal(wire(t, a, path), wire(t, b, path)) {
			t.Errorf("%s: the flip changes nothing unrestricted", path)
		}
	}
}

// Design §7.4 rule 3 (our call, extends D-024): a hidden ancestor route on
// the up-chain is its name, link none, and the matches it delegated under;
// a hidden Gateway stays a step with no identity.
func TestUpChainHiddenAncestorRoute(t *testing.T) {
	_, srv, _ := startWith(t, setup{disputes: true, authz: hideAncestor}, ancestorWorld("/api/s", "")...)
	d := get[RouteDetail](t, srv, routeDetail+"shop/leaf", 200)
	want := []AncestorPathDTO{{Steps: []AncestorStepDTO{
		{Ref: &LinkedRef{Ref: route("shop", "mid"), Link: LinkNone}, Matches: []MatchDTO{pathMatch("/api/m")}},
		{Ref: &LinkedRef{Ref: route("shop", "leaf"), Link: LinkDetail}},
	}}}
	if !reflect.DeepEqual(d.Ancestors, want) || d.AncestorsTruncated {
		t.Fatalf("leaf up-chain %+v", d.Ancestors)
	}

	hideEdge := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Kind != "Gateway" })
	_, gw, _ := startWith(t, setup{disputes: true, authz: hideEdge}, ancestorWorld("/api/s", "")...)
	var got struct {
		Ancestors []struct {
			Steps []map[string]any `json:"steps"`
		} `json:"ancestors"`
	}
	b, err := json.Marshal(get[map[string]any](t, gw, routeDetail+"shop/leaf", 200))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Ancestors) != 1 || len(got.Ancestors[0].Steps) != 4 || !reflect.DeepEqual(got.Ancestors[0].Steps[0], map[string]any{"restricted": true}) {
		t.Fatalf("hidden Gateway step %+v", got.Ancestors)
	}
	for _, s := range got.Ancestors[0].Steps[1:] {
		if _, ok := s["matches"]; ok || s["ref"].(map[string]any)["link"] != "detail" {
			t.Errorf("visible step %v", s)
		}
	}
}

// Design §7.4 rule 5: selected and attached count visible children only,
// per request.
func TestDelegationCountsRecounted(t *testing.T) {
	w := testutil.WithRules(testutil.HTTPRoute("shop", "w", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		delegRule([]string{"/w"}, testutil.DelegateWildcard("kids")))
	wRef := testutil.HTTPRouteRef("shop", "w")
	objs := isolated(kidsNS, w, leaf("kids", "a", nil, "/w/a", testutil.AcceptedBy(wRef, testutil.Controller)),
		leaf("kids", "b", nil, "/w/b", testutil.AcceptedBy(wRef, testutil.Controller)), leaf("kids", "c", nil, "/other"))
	for _, tc := range []struct {
		hide               string
		selected, attached int
	}{{"", 3, 2}, {"b", 2, 1}, {"c", 2, 2}} {
		authz := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Namespace != "kids" || ref.Name != tc.hide })
		_, srv, _ := startWith(t, setup{disputes: true, authz: authz}, objs...)
		dl := get[RouteDetail](t, srv, routeDetail+"shop/w", 200).Rules[0].BackendRefs[0].Delegation
		if dl.Selected == nil || *dl.Selected != tc.selected || dl.Attached == nil || *dl.Attached != tc.attached {
			t.Errorf("hide %q: selected %v attached %v", tc.hide, dl.Selected, dl.Attached)
		}
		// A hidden child adds the ref's one restricted row (our call, 2.4).
		total := tc.selected
		if tc.hide != "" {
			total++
		}
		if p := get[Page[DelegationRow]](t, srv, delegURL("shop", "w", ""), 200); p.Total != total {
			t.Errorf("hide %q: total %d", tc.hide, p.Total)
		}
	}
}

// D-013: a name ref to a hidden route is selection-restricted: a restricted
// check and visible counts only, so a present target and an absent one look
// the same.
func TestHiddenNameRefHasNoCounts(t *testing.T) {
	w := testutil.WithRules(testutil.HTTPRoute("shop", "w", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		delegRule([]string{"/w"}, testutil.DelegateTo("kids", "x")), delegRule([]string{"/w"}, testutil.DelegateTo("kids", "y")))
	hide := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Namespace != "kids" || ref.Kind != "HTTPRoute" })
	_, srv, _ := startWith(t, setup{disputes: true, authz: hide}, isolated(kidsNS, w, leaf("kids", "x", nil, "/w/x"))...)
	rules := get[RouteDetail](t, srv, routeDetail+"shop/w", 200).Rules
	for i, name := range []string{"x", "y"} {
		zero, rc := 0, restrictedComputed("")
		want := &DelegationRefDTO{Mode: objects.DelegationName, Namespace: "kids", Name: name, State: DelegationRestricted, Selected: &zero, Attached: &zero, Computed: &rc}
		if got := rules[i].BackendRefs[0].Delegation; !reflect.DeepEqual(got, want) {
			t.Errorf("kids/%s: %+v", name, got)
		}
	}
}

// flipBodies is the caller's view of parent shop/w in each world: its full
// delegation page, two outcome filters, its detail without health, and the
// state=dropped list, concatenated per world.
func flipBodies(t *testing.T, authz Authorizer, worlds ...[]runtime.Object) [][]byte {
	t.Helper()
	var out [][]byte
	for _, w := range worlds {
		_, srv, _ := startWith(t, setup{disputes: true, authz: authz}, w...)
		out = append(out, slices.Concat(wire(t, srv, delegURL("shop", "w", "")), wire(t, srv, delegURL("shop", "w", "?outcome=unresolved")),
			wire(t, srv, delegURL("shop", "w", "?outcome=attached")), withoutHealth(t, srv, routeDetail+"shop/w"),
			withoutHealth(t, srv, listBase+"/routes?state=dropped")))
	}
	return out
}

// wWorld is root shop/w on edge delegating /w through ref, plus extra.
func wWorld(ref map[string]any, extra ...runtime.Object) []runtime.Object {
	w := testutil.WithRules(testutil.HTTPRoute("shop", "w", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		delegRule([]string{"/w"}, ref))
	return isolated(append([]runtime.Object{kidsNS, w}, extra...)...)
}

var hideKids = AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool {
	return ref.Kind != "HTTPRoute" || ref.Namespace != "kids" || strings.HasPrefix(ref.Name, "k")
})

// Fix round 3, P1a (our call, 2.4): a name ref whose target the caller cannot
// view is selection-restricted: one restricted zero-child row, whether the
// target exists or not.
func TestHiddenNameTargetPresentOrAbsent(t *testing.T) {
	ref := testutil.DelegateTo("kids", "x")
	b := flipBodies(t, hideKids, wWorld(ref, leaf("kids", "x", nil, "/w/x")), wWorld(ref))
	if !bytes.Equal(b[0], b[1]) {
		t.Fatalf("a hidden name target's existence shows")
	}
	_, srv, _ := startWith(t, setup{disputes: true, authz: hideKids}, wWorld(ref)...)
	p := get[Page[DelegationRow]](t, srv, delegURL("shop", "w", ""), 200)
	want := DelegationRow{RefAsWritten: DelegationRefDTO{Mode: objects.DelegationName, Namespace: "kids", Name: "x"}, Contexts: []DelegationContext{},
		Computed: restrictedComputed("")}
	if len(p.Items) != 1 || p.Total != 1 || !reflect.DeepEqual(p.Items[0], want) {
		t.Fatalf("rows %+v total %d", p.Items, p.Total)
	}
}

// Fix round 3, P1b: a label ref over a namespace with a hidden route is
// selection-restricted, whether that route carries the label or not.
func TestHiddenRouteLabelledOrNot(t *testing.T) {
	ref := testutil.DelegateLabel("kids", "team")
	labelled := leaf("kids", "h", nil, "/w/h")
	labelled.SetLabels(map[string]string{objects.DelegationLabelKey: "team"})
	b := flipBodies(t, hideKids, wWorld(ref, labelled), wWorld(ref, leaf("kids", "h", nil, "/w/h")))
	if !bytes.Equal(b[0], b[1]) {
		t.Fatalf("a hidden route's label shows")
	}
}

// Fix round 3, P1b: under a wildcard ref, a hidden route created or deleted
// changes nothing while another hidden route stays in the namespace. With no
// hidden route left, the ref is no longer restricted: that reveals no more
// than deniedInInstall (design §7.4 rule 2, our call, 2.4).
func TestHiddenRouteUnderWildcard(t *testing.T) {
	ref := testutil.DelegateWildcard("kids")
	keep := []runtime.Object{leaf("kids", "k1", nil, "/w/k1"), leaf("kids", "h1", nil, "/w/h1")}
	b := flipBodies(t, hideKids, wWorld(ref, append(slices.Clone(keep), leaf("kids", "h2", nil, "/other"))...), wWorld(ref, keep...))
	if !bytes.Equal(b[0], b[1]) {
		t.Fatalf("a hidden route under a wildcard shows")
	}
	_, srv, _ := startWith(t, setup{disputes: true, authz: hideKids}, wWorld(ref, keep...)...)
	if p := get[Page[DelegationRow]](t, srv, delegURL("shop", "w", ""), 200); !slices.Equal(rowKeys(p), []string{"0.0:", "0.0:kids/k1"}) ||
		p.Items[0].Computed.State != "restricted" || p.Items[1].Outcome != "attached" {
		t.Fatalf("rows %v", rowKeys(p))
	}
	if p := get[Page[DelegationRow]](t, srv, delegURL("shop", "w", "?outcome=unknown&outcome=unresolved"), 200); p.Total != 0 {
		t.Fatalf("the restricted row matched an outcome filter: %v", rowKeys(p))
	}
}

// twoRootWorld: shop/r1 (edge, visible) and shop/r2 (edge2's Selector
// listener over shop's zone label) both delegate /x to shop/mid, which
// delegates /x/m to shop/kid.
func twoRootWorld(zone string) []runtime.Object {
	sel := lst("sel", 8080, "")
	sel.AllowedRoutes.Namespaces = objects.RouteNamespaces{From: "Selector", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"zone": "in"}}}
	edge2 := testutil.GatewayWithListeners("infra", "edge2", "kgateway", []objects.Listener{sel}, up("sel", 1))
	r1Ref, r2Ref := testutil.HTTPRouteRef("shop", "r1"), testutil.HTTPRouteRef("shop", "r2")
	r1 := testutil.WithRules(testutil.HTTPRoute("shop", "r1", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		delegRule([]string{"/x"}, testutil.DelegateTo("shop", "mid")))
	r2 := testutil.WithRules(testutil.HTTPRoute("shop", "r2", []objects.ParentRef{edge2Ref}, testutil.AcceptedBy(edge2Ref, testutil.Controller)),
		delegRule([]string{"/x"}, testutil.DelegateTo("shop", "mid")))
	mid := testutil.WithRules(testutil.HTTPRoute("shop", "mid", nil, testutil.AcceptedBy(r1Ref, testutil.Controller), testutil.AcceptedBy(r2Ref, testutil.Controller)),
		delegRule([]string{"/x/m"}, testutil.DelegateTo("shop", "kid")))
	objs := isolated(edge2, r1, r2, mid, leaf("shop", "kid", nil, "/x/m/k", testutil.AcceptedBy(testutil.HTTPRouteRef("shop", "mid"), testutil.Controller)))
	for i, o := range objs {
		if ns, ok := o.(*corev1.Namespace); ok && ns.Name == "shop" {
			objs[i] = testutil.Namespace("shop", map[string]string{"team": "a", "zone": zone})
		}
	}
	return objs
}

// Fix round 3, P1c: a row reached by a visible root and by a root behind a
// hidden Namespace restricts whatever that Namespace's labels are.
func TestRootEligibilityOnEveryRow(t *testing.T) {
	var bodies [][]byte
	for _, zone := range []string{"in", "out"} {
		_, srv, _ := startWith(t, setup{disputes: true, authz: hideNamespaces}, twoRootWorld(zone)...)
		var b []byte
		for _, p := range []string{delegURL("shop", "mid", ""), delegURL("shop", "mid", "?outcome=attached"), delegURL("shop", "r1", ""), delegURL("shop", "r2", "")} {
			b = append(b, wire(t, srv, p)...)
		}
		for _, p := range []string{routeDetail + "shop/mid", routeDetail + "shop/kid", routeDetail + "shop/r1", routeDetail + "shop/r2", listBase + "/routes?state=dropped"} {
			b = append(b, withoutHealth(t, srv, p)...)
		}
		bodies = append(bodies, b)
		if r := get[Page[DelegationRow]](t, srv, delegURL("shop", "mid", ""), 200).Items; len(r) != 1 || r[0].Computed.State != "restricted" {
			t.Errorf("zone %s: mid rows %+v", zone, r)
		}
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Fatalf("the hidden Namespace's labels change what the caller sees")
	}
}

// Fix round 3, P2: children decide parent-resolvedrefs, so a hidden child
// restricts its KGWUI-012 evidence; only ancestors never do (rule 7).
func TestParentResolvedRefsEvidenceHiddenChild(t *testing.T) {
	hideC := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != route("shop", "c") })
	_, srv, _ := startWith(t, setup{disputes: true, authz: hideC}, childDropWorld(false)...)
	var got []FindingDTO
	for _, f := range findingsOn(get[HealthResponse](t, srv, base+"/health", 200), "shop/api") {
		if f.Code == health.CodeComputedDisagrees {
			got = append(got, f)
		}
	}
	if len(got) != 1 || !got[0].EvidenceRestricted || got[0].Detail != restrictedEvidence {
		t.Fatalf("parent-resolvedrefs under a hidden child: %+v", got)
	}
}

// Rules §11.5: a route with a Gateway parentRef that a wildcard selects gets
// no KGWUI-011, and its Delegation row is shown with its reason.
func TestDroppedRouteWithGatewayParentShown(t *testing.T) {
	w := testutil.WithRules(testutil.HTTPRoute("shop", "w", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		delegRule([]string{"/w"}, testutil.DelegateWildcard("kids")))
	_, srv, _ := startWith(t, setup{disputes: true}, isolated(kidsNS, w, leaf("kids", "g", []objects.ParentRef{edgeRef}, "/g", testutil.AcceptedBy(edgeRef, testutil.Controller)))...)
	p := get[Page[DelegationRow]](t, srv, delegURL("shop", "w", ""), 200)
	if len(p.Items) != 1 || rowKey(p.Items[0]) != "0.0:kids/g" || p.Items[0].Outcome != "forbidden" || p.Items[0].Reason != reasonForbidden {
		t.Fatalf("rows %+v", p.Items)
	}
	if hasFinding(get[HealthResponse](t, srv, base+"/health", 200), health.CodeDelegatedNotAttached, "kids/g") {
		t.Fatal("KGWUI-011 on a route with a Gateway parent")
	}
}

// Design §7.4 rule 9: restricting delegation facts never changes health.
func TestHealthUnchangedUnderDelegationRestriction(t *testing.T) {
	type sig struct {
		code, res string
		sev       health.Severity
	}
	summary := func(h HealthResponse) (health.State, []sig) {
		var out []sig
		for _, f := range h.Findings {
			s := sig{code: f.Code, sev: f.Severity}
			if f.Resource != nil {
				s.res = f.Resource.String()
			}
			out = append(out, s)
		}
		return h.State, out
	}
	for _, tc := range []struct {
		name  string
		objs  []runtime.Object
		authz Authorizer
	}{
		{"hidden ancestor", ancestorWorld("/nope", "k2.example.org"), hideAncestor},
		{"hidden namespace", selectorRootWorld("in"), hideNamespaces},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, all, _ := startWith(t, setup{disputes: true}, tc.objs...)
			_, some, _ := startWith(t, setup{disputes: true, authz: tc.authz}, tc.objs...)
			sa, fa := summary(get[HealthResponse](t, all, base+"/health", 200))
			ss, fs := summary(get[HealthResponse](t, some, base+"/health", 200))
			if sa != ss || !slices.Equal(fa, fs) || get[RouteDetail](t, all, routeDetail+"shop/api", 200).Health != get[RouteDetail](t, some, routeDetail+"shop/api", 200).Health {
				t.Fatalf("state %s vs %s; findings %v vs %v", sa, ss, fa, fs)
			}
		})
	}
}

// Design §9.1: outcome=dropped under a root behind a hidden Selector
// Namespace, and state=dropped, give the rows and total of the unknown case.
func TestOutcomeFilterCannotProbe(t *testing.T) {
	_, all, _ := startWith(t, setup{disputes: true}, selectorRootWorld("in")...)
	if p := get[Page[DelegationRow]](t, all, delegURL("shop", "api", "?outcome=dropped"), 200); p.Total != 1 {
		t.Fatalf("unrestricted outcome=dropped: total %d", p.Total)
	}
	if p := get[Page[RouteRow]](t, all, listBase+"/routes?state=dropped", 200); !slices.Equal(routeNames(p), []string{"shop/drop"}) {
		t.Fatalf("unrestricted state=dropped: %v", routeNames(p))
	}
	_, hidden, _ := startWith(t, setup{disputes: true, authz: hideNamespaces}, selectorRootWorld("in")...)
	_, unknown, _ := startWith(t, setup{disputes: true}, unknownScope(selectorRootWorld("in"))...)
	for _, path := range []string{delegURL("shop", "api", "?outcome=dropped"), delegURL("shop", "api", "?outcome=dropped&outcome=attached")} {
		h, u := get[Page[DelegationRow]](t, hidden, path, 200), get[Page[DelegationRow]](t, unknown, path, 200)
		if !slices.Equal(rowKeys(h), rowKeys(u)) || h.Total != u.Total || h.Total != 0 {
			t.Errorf("%s: hidden %v (%d), unknown %v (%d)", path, rowKeys(h), h.Total, rowKeys(u), u.Total)
		}
	}
	h, u := get[Page[RouteRow]](t, hidden, listBase+"/routes?state=dropped", 200), get[Page[RouteRow]](t, unknown, listBase+"/routes?state=dropped", 200)
	if !slices.Equal(routeNames(h), routeNames(u)) || h.Total != u.Total {
		t.Errorf("state=dropped: hidden %v, unknown %v", routeNames(h), routeNames(u))
	}
}

// childDropWorld: shop/api delegates /api to shop/c, whose only rule is
// /other, yet c keeps a current entry for api: a child-entry KGWUI-012 on c
// whose detail names api. With sel, api's only Gateway listener selects
// namespaces with zone=in, and shop has it.
func childDropWorld(sel bool) []runtime.Object {
	api := testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		delegRule([]string{"/api"}, testutil.DelegateTo("shop", "c")))
	c := leaf("shop", "c", nil, "/other", testutil.AcceptedBy(shopAPI, testutil.Controller))
	if !sel {
		return isolated(api, c)
	}
	l := lst("sel", 8080, "")
	l.AllowedRoutes.Namespaces = objects.RouteNamespaces{From: "Selector", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"zone": "in"}}}
	objs := viewAWorld(edgeWith([]objects.Listener{l}, up("sel", 1)), api, c)
	for i, o := range objs {
		if ns, ok := o.(*corev1.Namespace); ok && ns.Name == "shop" {
			objs[i] = testutil.Namespace("shop", map[string]string{"team": "a", "zone": "in"})
		}
	}
	return objs
}

// Design §9.1 (2.4), rule 7 (our call, extends D-024): a KGWUI-012 on a
// visible child whose only hidden evidence is its parent route keeps its
// detail; one whose root is behind a hidden Selector Namespace does not.
func TestHiddenAncestorEvidence(t *testing.T) {
	childEntry := func(t *testing.T, h HealthResponse) FindingDTO {
		t.Helper()
		for _, f := range findingsOn(h, "shop/c") {
			if f.Code == health.CodeComputedDisagrees {
				return f
			}
		}
		t.Fatalf("no KGWUI-012 on shop/c: %+v", h.Findings)
		return FindingDTO{}
	}
	hideAPI := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != route("shop", "api") })
	_, all, _ := startWith(t, setup{disputes: true}, childDropWorld(false)...)
	full := childEntry(t, get[HealthResponse](t, all, base+"/health", 200))
	_, some, _ := startWith(t, setup{disputes: true, authz: hideAPI}, childDropWorld(false)...)
	f := childEntry(t, get[HealthResponse](t, some, base+"/health", 200))
	if f.EvidenceRestricted || f.Detail != full.Detail || !strings.Contains(f.Detail, "shop/api") || f.Since == nil || !f.Since.Equal(*full.Since) {
		t.Fatalf("hidden parent route: %+v, want %+v", f, full)
	}

	_, ns, _ := startWith(t, setup{disputes: true, authz: hideNamespaces}, childDropWorld(true)...)
	f = childEntry(t, get[HealthResponse](t, ns, base+"/health", 200))
	if !f.EvidenceRestricted || f.Detail != restrictedEvidence || f.Restricted || f.Message == "" {
		t.Fatalf("hidden Selector Namespace: %+v", f)
	}
}

// Design §7.4 rule 10: under AllowAll the new endpoint and fields ask the
// Authorizer nothing, and a counting AllowAll gives the same bytes.
func TestDelegationAllowAllNoCalls(t *testing.T) {
	a := &countingAuthz{all: true}
	_, srv, _ := startWith(t, setup{disputes: true, authz: a}, ancestorWorld("/api/s", "")...)
	_, ref, _ := startWith(t, setup{disputes: true}, ancestorWorld("/api/s", "")...)
	for _, p := range []string{delegURL("shop", "api", ""), delegURL("shop", "leaf", "?outcome=attached"), routeDetail + "shop/leaf",
		routeDetail + "kids/k1", routeDetail + "shop/api"} {
		if got, want := wire(t, srv, p), wire(t, ref, p); !bytes.Equal(got, want) {
			t.Errorf("GET %s differs from AllowAll", p)
		}
		if n := len(a.take()); n != 0 {
			t.Errorf("GET %s asked the Authorizer about %d refs", p, n)
		}
	}
}

// A selected route is a candidate with reason selected, by its first
// visible selecting parent; it opens when that parent is visible.
func TestSelectedCandidate(t *testing.T) {
	w := testutil.WithRules(testutil.HTTPRoute("shop", "w", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		delegRule([]string{"/w"}, testutil.DelegateWildcard("kids")))
	// Created late, so its orphan trigger is not mature: no finding admits it.
	late := []*unstructured.Unstructured{leaf("kids", "s", nil, "/other")}
	objs := isolated(kidsNS, w)
	_, srv, _ := startWith(t, setup{disputes: true, late: late}, objs...)
	d := get[RouteDetail](t, srv, routeDetail+"kids/s", 200)
	if d.Candidate == nil || d.Candidate.Reason != "selected" || d.Candidate.By == nil || d.Candidate.By.Ref != route("shop", "w") || d.Candidate.By.Link != LinkDetail {
		t.Fatalf("candidate %+v", d.Candidate)
	}
	if r := get[Page[DelegationRow]](t, srv, delegURL("shop", "w", ""), 200).Items; len(r) != 1 || r[0].Child.Link != LinkDetail {
		t.Fatalf("row child %+v", r)
	}
	hideW := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != route("shop", "w") })
	_, some, _ := startWith(t, setup{disputes: true, authz: hideW, late: late}, objs...)
	get[ErrorResponse](t, some, routeDetail+"kids/s", http.StatusNotFound)
}

// Fit clauses and outcomes as text (PF9: rule sets return codes).
func TestExplainDelegation(t *testing.T) {
	for _, tc := range []struct {
		f    computed.FitFailure
		want string
	}{
		{computed.FitFailure{Clause: computed.ClausePathType, Parent: "Exact", Child: "/a"}, "the parent's path match is Exact, not PathPrefix"},
		{computed.FitFailure{Clause: computed.ClausePathPrefix, Parent: "/api", Child: "/x"}, "path /x is outside the parent's /api"},
		{computed.FitFailure{Clause: computed.ClauseDefaultSlash, Parent: "/api", Child: "/"}, "no matches, so it defaults to /, which is outside the parent's /api"},
		{computed.FitFailure{Clause: computed.ClauseHeader, Parent: "x=1", Child: ""}, "the parent needs header x=1, the rule has none"},
		{computed.FitFailure{Clause: computed.ClauseQuery, Parent: "q=1", Child: "q=2"}, "the parent needs query parameter q=1, the rule has q=2"},
		{computed.FitFailure{Clause: computed.ClauseMethod, Parent: "GET", Child: "POST"}, "the parent needs method GET, the rule has POST"},
		{computed.FitFailure{Clause: computed.ClauseNoMatches, Parent: "/api"}, "an empty match list fits no parent match"},
	} {
		if got := explainFit(&tc.f); got != tc.want || strings.Contains(got, ";") {
			t.Errorf("%s: %q, want %q", tc.f.Clause, got, tc.want)
		}
	}
	if got := explainFit(nil); got != "" {
		t.Errorf("nil fit %q", got)
	}
	if got := explainRow(computed.OutcomeUnknown, &computed.FitFailure{Clause: computed.ClausePathPrefix}, ""); got != reasonDroppedSome {
		t.Errorf("unknown with a drop: %q", got)
	}
	if got := explainRow(computed.OutcomeAttached, nil, ""); got != "" {
		t.Errorf("attached: %q", got)
	}
}

// Rules §5.7: a route with more parent edges than EffectiveEdges keeps the
// first ones, by parent, and the detail says so with effectiveTruncated.
func TestEffectiveTruncatedServed(t *testing.T) {
	b := computed.DefaultBounds
	b.EffectiveEdges = 1
	_, srv, _ := startWith(t, setup{bounds: b}, inheritWorld()...)
	d := get[RouteDetail](t, srv, routeDetail+"shop/x", 200)
	if !d.EffectiveTruncated || len(d.Effective) != 1 || d.Effective[0].Parent.Name != "a" {
		t.Fatalf("effective %+v truncated %v, want a's edge only, truncated", d.Effective, d.EffectiveTruncated)
	}
	raw := get[map[string]any](t, srv, routeDetail+"shop/x", 200)
	if raw["effectiveTruncated"] != true {
		t.Fatalf("effectiveTruncated on the wire: %v", raw["effectiveTruncated"])
	}
	// Under the default bound both edges are kept and the flag is absent.
	_, all, _ := startWith(t, setup{}, inheritWorld()...)
	if d := get[RouteDetail](t, all, routeDetail+"shop/x", 200); d.EffectiveTruncated || len(d.Effective) != 2 {
		t.Fatalf("default bound: effective %d truncated %v", len(d.Effective), d.EffectiveTruncated)
	}
}
