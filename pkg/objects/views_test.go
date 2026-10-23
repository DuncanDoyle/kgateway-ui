package objects_test

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/json"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	. "github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// obj builds an object in namespace shop from JSON spec and status; the
// apimachinery decoder keeps integers as int64, as the API server does.
func obj(kind, spec, status string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "gateway.networking.k8s.io/v1", "kind": kind,
		"metadata": map[string]any{"name": "r", "namespace": "shop"}}}
	for k, s := range map[string]string{"spec": spec, "status": status} {
		if s == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			panic(err)
		}
		u.Object[k] = m
	}
	return u
}

func route(spec string) *unstructured.Unstructured { return obj("HTTPRoute", spec, "") }

func i32(v int32) *int32 { return &v }

func TestDecodeHTTPRouteDefaults(t *testing.T) {
	v := DecodeHTTPRoute(route(`{"rules":[{"backendRefs":[{"name":"svc","port":80}]},{"matches":[]}]}`))
	if len(v.Rules) != 2 {
		t.Fatalf("rules: %+v", v.Rules)
	}
	if v.Rules[0].Matches != nil || v.Rules[1].Matches == nil || len(v.Rules[1].Matches) != 0 {
		t.Fatal("omitted vs empty matches")
	}
	if v.Rules[0].Index != 0 || v.Rules[1].Index != 1 {
		t.Fatal("rule index")
	}
	b := v.Rules[0].BackendRefs[0]
	want := Ref{Group: "", Kind: "Service", Namespace: "shop", Name: "svc"}
	if b.Ref != want || b.Port != 80 || b.Weight != 1 || !b.WeightDefaulted || b.Delegation != nil {
		t.Fatalf("%+v", b)
	}
}

func TestDecodeDelegationModes(t *testing.T) {
	const gw = `"group":"gateway.networking.k8s.io"`
	cases := []struct {
		name, ref string
		want      *DelegationRef
	}{
		{"name", `{` + gw + `,"kind":"HTTPRoute","name":"child","namespace":"team"}`, &DelegationRef{Mode: DelegationName, Namespace: "team", Name: "child"}},
		{"star is wildcard", `{` + gw + `,"kind":"HTTPRoute","name":"*","namespace":"team"}`, &DelegationRef{Mode: DelegationWildcard, Namespace: "team"}},
		{"empty name is wildcard", `{` + gw + `,"kind":"HTTPRoute","name":""}`, &DelegationRef{Mode: DelegationWildcard, Namespace: "shop"}},
		{"label", `{"group":"delegation.kgateway.dev","kind":"label","name":"a-team","namespace":"team"}`, &DelegationRef{Mode: DelegationLabel, Namespace: "team", Label: "a-team"}},
		{"namespace defaults to the parent's", `{` + gw + `,"kind":"HTTPRoute","name":"child"}`, &DelegationRef{Mode: DelegationName, Namespace: "shop", Name: "child"}},
		{"omitted group is a backend", `{"kind":"HTTPRoute","name":"child"}`, nil},
		{"omitted kind is a Service", `{` + gw + `,"name":"child"}`, nil},
		{"label without group", `{"kind":"label","name":"a-team"}`, nil},
		{"label without kind", `{"group":"delegation.kgateway.dev","name":"a-team"}`, nil},
		{"other kind in the group", `{` + gw + `,"kind":"GRPCRoute","name":"child"}`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := DecodeHTTPRoute(route(`{"rules":[{"backendRefs":[` + c.ref + `]}]}`))
			got := v.Rules[0].BackendRefs[0].Delegation
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestDecodeDelegationBuilders(t *testing.T) {
	u := testutil.WithRules(testutil.HTTPRoute("shop", "p", nil), map[string]any{"backendRefs": []any{
		testutil.DelegateTo("team", "c"), testutil.DelegateWildcard("team"), testutil.DelegateLabel("team", "x")}})
	got := []DelegationMode{}
	for _, b := range DecodeHTTPRoute(u).Rules[0].BackendRefs {
		got = append(got, b.Delegation.Mode)
	}
	if !reflect.DeepEqual(got, []DelegationMode{DelegationName, DelegationWildcard, DelegationLabel}) {
		t.Fatalf("got %v", got)
	}
}

func TestDecodeMirrorWeightPortOrder(t *testing.T) {
	v := DecodeHTTPRoute(route(`{"rules":[{
		"filters":[
			{"type":"RequestHeaderModifier","requestHeaderModifier":{"set":[{"name":"x","value":"1"}]}},
			{"type":"RequestMirror","requestMirror":{"backendRef":{"name":"shadow","port":8080},"percent":25}},
			{"type":"ExtensionRef","extensionRef":{"group":"gateway.kgateway.dev","kind":"TrafficPolicy","name":"tp"}}
		],
		"backendRefs":[
			{"name":"a"},
			{"name":"b","weight":1,"port":9090},
			{"name":"c","weight":0,"filters":[
				{"type":"ExtensionRef","extensionRef":{"group":"gateway.kgateway.dev","kind":"TrafficPolicy","name":"tp"}},
				{"type":"RequestMirror","requestMirror":{"backendRef":{"group":"gateway.kgateway.dev","kind":"Backend","name":"m","namespace":"other"},"fraction":{"numerator":1}}},
				{"type":"ResponseHeaderModifier","responseHeaderModifier":{"remove":["y"]}}
			]}
		]}]}`))
	r := v.Rules[0]
	types := func(fs []Filter) []string {
		var out []string
		for _, f := range fs {
			out = append(out, f.Type)
		}
		return out
	}
	if got := types(r.Filters); !reflect.DeepEqual(got, []string{"RequestHeaderModifier", "RequestMirror", "ExtensionRef"}) {
		t.Fatalf("rule filter order %v", got)
	}
	if got := types(r.BackendRefs[2].Filters); !reflect.DeepEqual(got, []string{"ExtensionRef", "RequestMirror", "ResponseHeaderModifier"}) {
		t.Fatalf("backendRef filter order %v", got)
	}
	wantMirror := &MirrorFilter{Backend: Ref{Kind: "Service", Namespace: "shop", Name: "shadow"}, Port: 8080, Percent: i32(25)}
	if !reflect.DeepEqual(r.Filters[1].Mirror, wantMirror) {
		t.Fatalf("percent mirror %+v", r.Filters[1].Mirror)
	}
	wantFrac := &MirrorFilter{Backend: Ref{Group: "gateway.kgateway.dev", Kind: "Backend", Namespace: "other", Name: "m"}, Fraction: &Fraction{Numerator: 1, Denominator: 100}}
	if !reflect.DeepEqual(r.BackendRefs[2].Filters[1].Mirror, wantFrac) {
		t.Fatalf("fraction mirror %+v", r.BackendRefs[2].Filters[1].Mirror)
	}
	if e := r.Filters[2].ExtensionRef; e == nil || *e != (Ref{Group: "gateway.kgateway.dev", Kind: "TrafficPolicy", Namespace: "shop", Name: "tp"}) {
		t.Fatalf("extensionRef %+v", e)
	}
	if r.Filters[0].Config["requestHeaderModifier"] == nil || r.Filters[0].Mirror != nil {
		t.Fatalf("config as written %+v", r.Filters[0])
	}
	weights := []struct {
		w         int32
		defaulted bool
		port      int32
	}{{1, true, 0}, {1, false, 9090}, {0, false, 0}}
	for i, w := range weights {
		b := r.BackendRefs[i]
		if b.Weight != w.w || b.WeightDefaulted != w.defaulted || b.Port != w.port {
			t.Fatalf("backendRef %d: %+v", i, b)
		}
	}
}

func TestBackendTargets(t *testing.T) {
	v := DecodeHTTPRoute(route(`{"rules":[
		{"filters":[{"type":"RequestMirror","requestMirror":{"backendRef":{"name":"shadow"}}}],
		 "backendRefs":[{"name":"svc"},{"group":"gateway.kgateway.dev","kind":"Backend","name":"be"}]},
		{"backendRefs":[{"group":"gateway.networking.k8s.io","kind":"HTTPRoute","name":"child"},
		                {"group":"delegation.kgateway.dev","kind":"label","name":"x"}]}]}`))
	want := []BackendTarget{
		{Ref: Ref{Kind: "Service", Namespace: "shop", Name: "shadow"}, Mirror: true},
		{Ref: Ref{Kind: "Service", Namespace: "shop", Name: "svc"}},
		{Ref: Ref{Group: "gateway.kgateway.dev", Kind: "Backend", Namespace: "shop", Name: "be"}},
	}
	if got := BackendTargets(v); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestAllowedListenersAsWritten(t *testing.T) {
	if v := DecodeGateway(obj("Gateway", `{"gatewayClassName":"k"}`, "")); v.AllowedListeners != nil || v.Class != "k" {
		t.Fatalf("absent: %+v", v)
	}
	v := DecodeGateway(obj("Gateway", `{"allowedListeners":{"namespaces":{"from":"Selector","selector":{"matchLabels":{"a":"b"}}}}}`, ""))
	want := map[string]any{"namespaces": map[string]any{"from": "Selector", "selector": map[string]any{"matchLabels": map[string]any{"a": "b"}}}}
	if !reflect.DeepEqual(v.AllowedListeners, want) {
		t.Fatalf("got %+v", v.AllowedListeners)
	}
}

func TestDecodeHeaderTypeDefaultsExact(t *testing.T) {
	v := DecodeHTTPRoute(route(`{"rules":[{"matches":[{
		"path":{"value":"/api"},
		"headers":[{"name":"h","value":"1"},{"type":"RegularExpression","name":"r","value":".*"}],
		"queryParams":[{"name":"q","value":"2"}],"method":"GET"}]}]}`))
	m := v.Rules[0].Matches[0]
	want := Match{Path: &PathMatch{Type: "PathPrefix", Value: "/api"},
		Headers:     []KeyMatch{{Type: "Exact", Name: "h", Value: "1"}, {Type: "RegularExpression", Name: "r", Value: ".*"}},
		QueryParams: []KeyMatch{{Type: "Exact", Name: "q", Value: "2"}}, Method: "GET"}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got %+v", m)
	}
}

func TestWrittenAndResolvedRefs(t *testing.T) {
	u := obj("HTTPRoute", `{"parentRefs":[{"name":"edge"},{"name":"edge","namespace":"shop"},{"name":"edge","sectionName":"http","port":8080}]}`,
		`{"parents":[{"parentRef":{"name":"edge"},"controllerName":"c"}]}`)
	written := []WrittenRef{
		{Name: "edge"},
		{Name: "edge", Namespace: "shop"},
		{Name: "edge", SectionName: "http", Port: 8080, PortSet: true},
	}
	if got := WrittenParentRefs(u); !reflect.DeepEqual(got, written) {
		t.Fatalf("written %+v", got)
	}
	if written[0] == written[1] {
		t.Fatal("an explicit namespace equal to the default must stay a distinct key")
	}
	resolved := RouteParentRefs(u)
	if len(resolved) != 3 || resolved[0] != resolved[1] || resolved[0].Kind != "Gateway" || resolved[0].Namespace != "shop" {
		t.Fatalf("resolved %+v", resolved)
	}
	v := DecodeHTTPRoute(u)
	if !reflect.DeepEqual(v.WrittenRefs, written) || !reflect.DeepEqual(v.ParentRefs, resolved) {
		t.Fatalf("view refs %+v %+v", v.WrittenRefs, v.ParentRefs)
	}
	if len(v.Parents) != 1 || v.Parents[0].Written != (WrittenRef{Name: "edge"}) || v.Parents[0].ParentRef.Group != GatewayAPIGroup {
		t.Fatalf("status %+v", v.Parents)
	}
	// A key map works on the written form.
	keys := map[WrittenRef]int{}
	for _, w := range v.WrittenRefs {
		keys[w]++
	}
	if len(keys) != 3 {
		t.Fatalf("keys %v", keys)
	}
}

func TestInheritParentMatcherValues(t *testing.T) {
	for val, want := range map[string]bool{"true": true, "YES": true, "Enabled": true, "on": false, "": false, "false": false} {
		u := route(`{}`)
		u.SetAnnotations(map[string]string{"delegation.kgateway.dev/inherit-parent-matcher": val})
		if got := DecodeHTTPRoute(u).InheritParentMatcher; got != want {
			t.Errorf("%q: got %v", val, got)
		}
	}
	if DecodeHTTPRoute(route(`{}`)).InheritParentMatcher {
		t.Error("absent annotation")
	}
}

func TestDecodeHTTPRouteMetadata(t *testing.T) {
	u := testutil.WithHostnames(route(`{}`), "a.example.com", "b.example.com")
	u.SetLabels(map[string]string{"team": "web"})
	v := DecodeHTTPRoute(u)
	if !reflect.DeepEqual(v.Hostnames, []string{"a.example.com", "b.example.com"}) || v.Labels["team"] != "web" {
		t.Fatalf("%+v", v)
	}
	if h := DecodeRouteHostnames(u); !reflect.DeepEqual(h.Hostnames, v.Hostnames) {
		t.Fatalf("route hostnames %+v", h)
	}
}

func TestDecodeListenerDefaults(t *testing.T) {
	v := DecodeGateway(obj("Gateway", `{"gatewayClassName":"k","listeners":[
		{"name":"http","protocol":"HTTP"},
		{"name":"https","port":443,"protocol":"HTTPS","hostname":"*.example.com",
		 "tls":{"certificateRefs":[{"name":"cert"},{"kind":"Secret","name":"c2","namespace":"certs"}]},
		 "allowedRoutes":{"namespaces":{"from":"Selector","selector":{"matchLabels":{"team":"web"}}},"kinds":[{"kind":"HTTPRoute"}]}}],
		"addresses":[{"value":"1.2.3.4"}]}`,
		`{"addresses":[{"value":"10.0.0.1"}],"attachedListenerSets":2,
		  "listeners":[{"name":"http","attachedRoutes":3,"supportedKinds":[{"kind":"HTTPRoute"},{"group":"g","kind":"K"}],"conditions":[]}]}`))
	want0 := Listener{Name: "http", Protocol: "HTTP", AllowedRoutes: AllowedRoutes{Namespaces: RouteNamespaces{From: "Same"}}}
	if !reflect.DeepEqual(v.Listeners[0], want0) {
		t.Fatalf("listener 0 %+v", v.Listeners[0])
	}
	want1 := Listener{Name: "https", Port: 443, Protocol: "HTTPS", Hostname: "*.example.com",
		TLS: &ListenerTLS{Mode: "Terminate", CertificateRefs: []Ref{{Kind: "Secret", Namespace: "shop", Name: "cert"}, {Kind: "Secret", Namespace: "certs", Name: "c2"}}},
		AllowedRoutes: AllowedRoutes{Namespaces: RouteNamespaces{From: "Selector", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "web"}}},
			Kinds: []GroupKind{{Group: GatewayAPIGroup, Kind: "HTTPRoute"}}}}
	if !reflect.DeepEqual(v.Listeners[1], want1) {
		t.Fatalf("listener 1 %+v", v.Listeners[1])
	}
	if !reflect.DeepEqual(v.SpecAddresses, []string{"1.2.3.4"}) || !reflect.DeepEqual(v.StatusAddresses, []string{"10.0.0.1"}) ||
		v.AttachedListenerSets == nil || *v.AttachedListenerSets != 2 {
		t.Fatalf("addresses %+v", v)
	}
	ls := v.ListenerStatus
	if len(ls) != 1 || ls[0].AttachedRoutes != 3 || !reflect.DeepEqual(ls[0].SupportedKinds, []GroupKind{{Group: GatewayAPIGroup, Kind: "HTTPRoute"}, {Group: "g", Kind: "K"}}) {
		t.Fatalf("listener status %+v", ls)
	}
}

func TestDecodeListenerSet(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	u := testutil.ListenerSet("shop", "ls", gw, nil)
	v := DecodeListenerSet(u)
	if v.Parent != gw || len(v.Listeners) != 1 || v.Listeners[0].Port != 8081 || len(v.ListenerStatus) != 1 || len(v.Conditions) != 2 {
		t.Fatalf("%+v", v)
	}
}

func TestDecodeBackend(t *testing.T) {
	v := DecodeBackend(testutil.Backend("shop", "be", nil))
	if v.Type != "Static" || FindCondition(v.Conditions, "Accepted") == nil {
		t.Fatalf("%+v", v)
	}
}

func TestMalformedFieldsGiveZeroValues(t *testing.T) {
	g := DecodeGateway(obj("Gateway", `{"gatewayClassName":"k","listeners":"x"}`, `{"conditions":[{"type":"Accepted","status":"True"}]}`))
	if len(g.Listeners) != 0 || g.Class != "k" || len(g.Conditions) != 1 {
		t.Fatalf("gateway %+v", g)
	}
	g = DecodeGateway(obj("Gateway", `{"listeners":[{"name":"ok","port":80},{"name":"bad","port":"x"}]}`, ""))
	if len(g.Listeners) != 2 || g.Listeners[0].Port != 80 || !reflect.DeepEqual(g.Listeners[1], Listener{}) {
		t.Fatalf("one bad listener %+v", g.Listeners)
	}
	r := DecodeHTTPRoute(route(`{"hostnames":"x","rules":[{"backendRefs":"x","matches":"x"},{"backendRefs":[{"name":"a","weight":"x"}]}]}`))
	if len(r.Hostnames) != 0 || len(r.Rules) != 2 || r.Rules[0].Matches != nil || len(r.Rules[0].BackendRefs) != 0 {
		t.Fatalf("route %+v", r)
	}
	if b := r.Rules[1].BackendRefs[0]; b.Ref.Name != "" || b.Weight != 0 {
		t.Fatalf("bad backendRef %+v", b)
	}
	for _, u := range []*unstructured.Unstructured{obj("Backend", `{"type":3}`, ""), {Object: map[string]any{"spec": "x", "status": 1}}} {
		_ = DecodeBackend(u)
		_ = DecodeListenerSet(u)
		_ = DecodeRouteHostnames(u)
		_ = DecodeHTTPRoute(u)
		_ = DecodeGateway(u)
	}
}

func TestDirectDecodesEveryCall(t *testing.T) {
	var d Decoder = Direct{}
	u := route(`{}`)
	a, b := d.HTTPRoute(u), d.HTTPRoute(u)
	if a == nil || a == b {
		t.Fatal("HTTPRoute: two calls must give two distinct views")
	}
	if g1, g2 := d.Gateway(u), d.Gateway(u); g1 == nil || g1 == g2 {
		t.Fatal("Gateway")
	}
	if l1, l2 := d.ListenerSet(u), d.ListenerSet(u); l1 == nil || l1 == l2 {
		t.Fatal("ListenerSet")
	}
	if b1, b2 := d.Backend(u), d.Backend(u); b1 == nil || b1 == b2 {
		t.Fatal("Backend")
	}
	if h1, h2 := d.RouteHostnames(u), d.RouteHostnames(u); h1 == nil || h1 == h2 {
		t.Fatal("RouteHostnames")
	}
}

func TestHTTPRouteRef(t *testing.T) {
	if p := testutil.HTTPRouteRef("shop", "api"); p != (ParentRef{Group: GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "api"}) {
		t.Fatalf("%+v", p)
	}
	r := testutil.Rule(map[string]any{"path": map[string]any{"value": "/x"}})
	if v := DecodeHTTPRoute(testutil.WithRules(route(`{}`), r, testutil.Rule())); len(v.Rules[0].Matches) != 1 || v.Rules[1].Matches != nil {
		t.Fatalf("%+v", v.Rules)
	}
}
