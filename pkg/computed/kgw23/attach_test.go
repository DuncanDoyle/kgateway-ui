package kgw23

import (
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

func gw(ns, name string, ls ...objects.Listener) computed.Resource {
	return computed.Resource{Ref: objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: ns, Name: name}, Listeners: ls}
}

func lset(ns, name string, acc computed.Acceptance, ls ...objects.Listener) computed.Resource {
	r := gw(ns, name, ls...)
	r.Ref.Kind = "ListenerSet"
	r.Acceptance = acc
	return r
}

func route(kind, ns, name string, refs ...objects.ParentRef) computed.Route {
	return computed.Route{
		Ref:        objects.Ref{Group: objects.GatewayAPIGroup, Kind: kind, Namespace: ns, Name: name},
		ParentRefs: refs,
		Written:    make([]objects.WrittenRef, len(refs)),
	}
}

// pref is a parentRef to r with an optional sectionName.
func pref(r computed.Resource, section string) objects.ParentRef {
	return objects.ParentRef{Group: r.Ref.Group, Kind: r.Ref.Kind, Namespace: r.Ref.Namespace, Name: r.Ref.Name, SectionName: section}
}

func fact(l string, code computed.FactCode, want, got string) computed.Fact {
	return computed.Fact{Listener: l, Code: code, Want: want, Got: got}
}

func lis(name, proto string, port int32) objects.Listener {
	return objects.Listener{Name: name, Protocol: proto, Port: port}
}

func withFrom(l objects.Listener, from string, sel map[string]string) objects.Listener {
	l.AllowedRoutes.Namespaces.From = from
	if sel != nil {
		l.AllowedRoutes.Namespaces.Selector = &metav1.LabelSelector{MatchLabels: sel}
	}
	return l
}

func withHosts(r computed.Route, h ...string) computed.Route {
	r.Hostnames = h
	return r
}

func expSetting(v string, known bool) install.Setting {
	return install.Setting{Value: v, Known: known}
}

func run(in computed.AttachmentInput) computed.AttachmentResult {
	in.NamespacesOK = in.NamespacesOK || in.NamespaceLabels != nil
	return New().Attachment(in)
}

// members returns "resource-name/listener" of route's members, in order.
func members(res computed.AttachmentResult, rt computed.Route) []string {
	var out []string
	for _, m := range res.Members {
		if m.Route == rt.Ref {
			out = append(out, m.Resource.Name+"/"+m.Listener)
		}
	}
	return out
}

func outcome(t *testing.T, res computed.AttachmentResult, rt computed.Route, ref int) computed.RefOutcome {
	t.Helper()
	for _, o := range res.Outcomes {
		if o.Route == rt.Ref && o.ParentRef == ref {
			return o
		}
	}
	t.Fatalf("no outcome for %s ref %d", rt.Ref.Name, ref)
	return computed.RefOutcome{}
}

func count(t *testing.T, res computed.AttachmentResult, r computed.Resource, l string) computed.ListenerCount {
	t.Helper()
	for _, c := range res.Counts {
		if c.Resource == r.Ref && c.Listener == l {
			return c
		}
	}
	t.Fatalf("no count for %s/%s", r.Ref.Name, l)
	return computed.ListenerCount{}
}

func TestAttachMembersPerKind(t *testing.T) {
	tls := lis("tls", "TLS", 8443)
	tls.TLS = &objects.ListenerTLS{Mode: "Passthrough"}
	g := gw("infra", "edge", lis("http", "HTTP", 80), tls)
	hr := route("HTTPRoute", "infra", "hr", pref(g, ""))
	gr := route("GRPCRoute", "infra", "gr", pref(g, ""))
	tcp := route("TCPRoute", "infra", "tcp", pref(g, ""))
	tr := route("TLSRoute", "infra", "tr", pref(g, ""))
	res := run(computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{hr, gr, tcp, tr}, Experimental: expSetting("true", true)})
	for rt, want := range map[*computed.Route][]string{&hr: {"edge/http"}, &gr: {"edge/http"}, &tcp: nil, &tr: {"edge/tls"}} {
		if got := members(res, *rt); !slices.Equal(got, want) {
			t.Errorf("%s: members %v, want %v", rt.Ref.Name, got, want)
		}
	}
	if o := outcome(t, res, tcp, 0); o.Outcome != computed.OutcomeNotAllowed || o.Rule != "allowed-routes" {
		t.Errorf("tcp: %+v", o)
	}
	if c := count(t, res, g, "http"); c.Count != 2 {
		t.Errorf("http count %d", c.Count)
	}
	// allowedRoutes.kinds narrows the defaults.
	g.Listeners[0].AllowedRoutes.Kinds = []objects.GroupKind{{Group: objects.GatewayAPIGroup, Kind: "GRPCRoute"}}
	res = run(computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{hr, gr}})
	if got := members(res, hr); got != nil {
		t.Errorf("hr with kinds [GRPCRoute]: %v", got)
	}
	if got := members(res, gr); !slices.Equal(got, []string{"edge/http"}) {
		t.Errorf("gr with kinds [GRPCRoute]: %v", got)
	}
}

func TestAttachNamespaces(t *testing.T) {
	labels := map[string]map[string]string{"shop": {"team": "web"}, "other": {"team": "retail"}}
	g := gw("infra", "edge",
		lis("same", "HTTP", 80),
		withFrom(lis("all", "HTTP", 81), "All", nil),
		withFrom(lis("sel", "HTTP", 82), "Selector", map[string]string{"team": "web"}),
	)
	inInfra := route("HTTPRoute", "infra", "a", pref(g, ""))
	inShop := route("HTTPRoute", "shop", "b", pref(g, ""))
	inOther := route("HTTPRoute", "other", "c", pref(g, ""))
	inMissing := route("HTTPRoute", "gone", "d", pref(g, ""))
	res := run(computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{inInfra, inShop, inOther, inMissing}, NamespaceLabels: labels})
	for rt, want := range map[*computed.Route][]string{
		&inInfra: {"edge/same", "edge/all"}, &inShop: {"edge/all", "edge/sel"}, &inOther: {"edge/all"}, &inMissing: {"edge/all"},
	} {
		if got := members(res, *rt); !slices.Equal(got, want) {
			t.Errorf("%s: members %v, want %v", rt.Ref.Name, got, want)
		}
	}

	// Same on a ListenerSet listener means the ListenerSet's namespace (Task 38 review M5).
	parent := gw("infra", "edge")
	ls := lset("shop", "ls", computed.Accepted, lis("ls-same", "HTTP", 90))
	lsShop := route("HTTPRoute", "shop", "e", pref(ls, ""))
	lsInfra := route("HTTPRoute", "infra", "f", objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "shop", Name: "ls"})
	res = run(computed.AttachmentInput{Resources: []computed.Resource{parent, ls}, Routes: []computed.Route{lsShop, lsInfra}})
	if got := members(res, lsShop); !slices.Equal(got, []string{"ls/ls-same"}) {
		t.Errorf("ListenerSet Same, route in its namespace: %v", got)
	}
	if o := outcome(t, res, lsInfra, 0); o.Outcome != computed.OutcomeNotAllowed {
		t.Errorf("ListenerSet Same, route in the Gateway's namespace: %+v", o)
	}

	// A nil selector is a listener error: no members, and NotAllowedByListeners when no other listener allows.
	bad := gw("infra", "bad", withFrom(lis("nil", "HTTP", 80), "Selector", nil))
	rt := route("HTTPRoute", "shop", "g", pref(bad, ""))
	res = run(computed.AttachmentInput{Resources: []computed.Resource{bad}, Routes: []computed.Route{rt}, NamespaceLabels: labels})
	if c := count(t, res, bad, "nil"); c.Error == "" || c.Count != 0 || c.Unknown != "" {
		t.Errorf("nil selector count: %+v", c)
	}
	o := outcome(t, res, rt, 0)
	if o.Outcome != computed.OutcomeNotAllowed || len(o.Facts) != 1 || o.Facts[0].Code != computed.FactSelectorError {
		t.Errorf("nil selector outcome: %+v", o)
	}
	if got := members(res, rt); got != nil {
		t.Errorf("nil selector members: %v", got)
	}
}

func TestAttachNamespaceLabelsUnavailable(t *testing.T) {
	g := gw("shop", "edge",
		withFrom(lis("sel", "HTTP", 80), "Selector", map[string]string{"team": "web"}),
		lis("same", "HTTP", 81),
	)
	rt := route("HTTPRoute", "shop", "a", pref(g, ""))
	res := New().Attachment(computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{rt}, NamespacesOK: false})
	if c := count(t, res, g, "sel"); c.Unknown != computed.UnknownNamespaceLabels {
		t.Errorf("sel count: %+v", c)
	}
	if c := count(t, res, g, "same"); c.Unknown != "" || c.Count != 1 {
		t.Errorf("same count: %+v", c)
	}
	if o := outcome(t, res, rt, 0); o.Unknown != computed.UnknownNamespaceLabels || o.Outcome != "" {
		t.Errorf("outcome: %+v", o)
	}
	if got := members(res, rt); !slices.Equal(got, []string{"edge/same"}) {
		t.Errorf("members: %v", got)
	}
}

func TestAttachSectionAndPort(t *testing.T) {
	g := gw("infra", "edge", lis("a", "HTTP", 80), lis("b", "HTTP", 81))
	port := pref(g, "")
	port.Port = 8080
	rtPort := route("HTTPRoute", "infra", "port", port)
	rtSection := route("HTTPRoute", "infra", "section", pref(g, "c"))
	anyPort := route("HTTPRoute", "infra", "any", pref(g, "b"))
	exact := pref(g, "")
	exact.Port = 81
	rtExact := route("HTTPRoute", "infra", "exact", exact)
	res := run(computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{rtPort, rtSection, anyPort, rtExact}})
	for _, rt := range []computed.Route{rtPort, rtSection} {
		if o := outcome(t, res, rt, 0); o.Outcome != computed.OutcomeNoParent || o.Rule != "section-and-port" {
			t.Errorf("%s: %+v", rt.Ref.Name, o)
		}
	}
	if o := outcome(t, res, rtPort, 0); !reflect.DeepEqual(o.Facts, []computed.Fact{fact("a", computed.FactPort, "8080", "80"), fact("b", computed.FactPort, "8080", "81")}) {
		t.Errorf("port facts: %+v", o.Facts)
	}
	if got := members(res, anyPort); !slices.Equal(got, []string{"edge/b"}) {
		t.Errorf("port 0: %v", got)
	}
	if o := outcome(t, res, rtExact, 0); o.Outcome != computed.OutcomeAccepted || !slices.Equal(o.Listeners, []string{"b"}) {
		t.Errorf("port 81: %+v", o)
	}
}

func TestAttachAllowedQuirk(t *testing.T) {
	// listener a: Same (allows shop); listener b: Selector team=web; namespace shop has team=retail; the ref sets sectionName b
	g := gw("shop", "edge",
		lis("a", "HTTP", 80),
		withFrom(lis("b", "HTTP", 81), "Selector", map[string]string{"team": "web"}),
	)
	rt := route("HTTPRoute", "shop", "r", pref(g, "b"))
	res := run(computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{rt}, NamespaceLabels: map[string]map[string]string{"shop": {"team": "retail"}}})
	o := outcome(t, res, rt, 0)
	if o.Outcome != computed.OutcomeNoParent {
		t.Fatalf("outcome %s, want NoMatchingParent", o.Outcome)
	}
	want := []computed.Fact{fact("a", computed.FactSection, "b", "a"), fact("b", computed.FactNamespace, "team=web", "team=retail")}
	if !reflect.DeepEqual(o.Facts, want) {
		t.Errorf("facts %+v, want %+v", o.Facts, want)
	}
}

func TestAttachHostnamesPerListener(t *testing.T) {
	a := lis("a", "HTTP", 80)
	a.Hostname = "*.example.com"
	b := lis("b", "HTTP", 81)
	b.Hostname = "api.example.com"
	g := gw("infra", "edge", a, b)
	rt := withHosts(route("HTTPRoute", "infra", "r", pref(g, "")), "api.example.com", "x.example.com")
	res := run(computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{rt}})
	if len(res.Members) != 2 {
		t.Fatalf("members %+v", res.Members)
	}
	if got := res.Members[0]; got.Listener != "a" || !slices.Equal(got.Hostnames, []string{"api.example.com", "x.example.com"}) {
		t.Errorf("a: %+v", got)
	}
	if got := res.Members[1]; got.Listener != "b" || !slices.Equal(got.Hostnames, []string{"api.example.com"}) {
		t.Errorf("b: %+v", got)
	}
	if o := outcome(t, res, rt, 0); o.Outcome != computed.OutcomeAccepted || !slices.Equal(o.Listeners, []string{"a", "b"}) || o.Rule != "hostname-intersection" {
		t.Errorf("outcome %+v", o)
	}
}

func TestAttachTwoRefsOneListener(t *testing.T) {
	g := gw("infra", "edge", lis("http", "HTTP", 80))
	withPort := pref(g, "")
	withPort.Port = 80
	rt := route("HTTPRoute", "infra", "r", pref(g, ""), withPort)
	res := run(computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{rt}})
	if c := count(t, res, g, "http"); c.Count != 2 {
		t.Errorf("count %d", c.Count)
	}
	if len(res.Members) != 2 || res.Members[0].ParentRef != 0 || res.Members[1].ParentRef != 1 {
		t.Errorf("members %+v", res.Members)
	}
}

func TestAttachGatewayRefNeverReachesListenerSet(t *testing.T) {
	g := gw("infra", "edge", lis("gw", "HTTP", 80))
	ls := lset("infra", "edge", computed.Accepted, lis("ls", "HTTP", 81))
	denied := lset("infra", "denied", computed.Denied, lis("d", "HTTP", 82))
	toGW := route("HTTPRoute", "infra", "to-gw", pref(g, ""))
	toLS := route("HTTPRoute", "infra", "to-ls", pref(ls, ""))
	toDenied := route("HTTPRoute", "infra", "to-denied", pref(denied, ""))
	res := run(computed.AttachmentInput{Resources: []computed.Resource{g, ls, denied}, Routes: []computed.Route{toGW, toLS, toDenied}})
	if got := members(res, toGW); !slices.Equal(got, []string{"edge/gw"}) || res.Members[0].Resource != g.Ref {
		t.Errorf("Gateway ref: %v", got)
	}
	if got := members(res, toLS); !slices.Equal(got, []string{"edge/ls"}) {
		t.Errorf("ListenerSet ref: %v", got)
	}
	for _, m := range res.Members {
		if m.Route == toLS.Ref && m.Resource != ls.Ref {
			t.Errorf("ListenerSet ref reached %v", m.Resource)
		}
	}
	if got := members(res, toDenied); got != nil {
		t.Errorf("denied members: %v", got)
	}
	for _, o := range res.Outcomes {
		if o.Resource == denied.Ref {
			t.Errorf("denied outcome %+v", o)
		}
	}
	for _, c := range res.Counts {
		if c.Resource == denied.Ref {
			t.Errorf("denied count %+v", c)
		}
	}
	if len(res.Counts) != 2 {
		t.Errorf("counts %+v", res.Counts)
	}
}

func TestAttachListenerSetNoStatus(t *testing.T) {
	ls := lset("infra", "ls", computed.NoStatus, lis("a", "HTTP", 80), lis("b", "HTTP", 81))
	rt := route("HTTPRoute", "infra", "r", pref(ls, ""))
	res := run(computed.AttachmentInput{Resources: []computed.Resource{ls}, Routes: []computed.Route{rt}})
	if len(res.Members) != 0 {
		t.Errorf("members %+v", res.Members)
	}
	if o := outcome(t, res, rt, 0); o.Unknown != computed.UnknownListenerSetStatus || o.Outcome != "" {
		t.Errorf("outcome %+v", o)
	}
	for _, l := range []string{"a", "b"} {
		if c := count(t, res, ls, l); c.Unknown != computed.UnknownListenerSetStatus {
			t.Errorf("%s: %+v", l, c)
		}
	}
}

func TestAttachExperimental(t *testing.T) {
	g := gw("infra", "edge", lis("tcp", "TCP", 9000), lis("http", "HTTP", 80))
	tcp := route("TCPRoute", "infra", "t", pref(g, ""))
	tcp.ExperimentalOnly = true
	in := computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{tcp}}

	in.Experimental = expSetting("true", true)
	res := run(in)
	if got := members(res, tcp); !slices.Equal(got, []string{"edge/tcp"}) {
		t.Errorf("true: %v", got)
	}
	if o := outcome(t, res, tcp, 0); o.Outcome != computed.OutcomeAccepted || o.Rule != "section-and-port" {
		t.Errorf("true: %+v", o)
	}

	in.Experimental = expSetting("false", true)
	res = run(in)
	if len(res.Members) != 0 || len(res.Outcomes) != 0 {
		t.Errorf("false: %+v", res)
	}
	if c := count(t, res, g, "tcp"); c.Count != 0 || c.Unknown != "" || slices.Contains(c.Deps, tcp.Ref) {
		t.Errorf("false count: %+v", c)
	}

	in.Experimental = expSetting("", false)
	res = run(in)
	if len(res.Members) != 0 {
		t.Errorf("unknown members %+v", res.Members)
	}
	if o := outcome(t, res, tcp, 0); o.Unknown != computed.UnknownSetting || o.Outcome != "" {
		t.Errorf("unknown outcome %+v", o)
	}
	if c := count(t, res, g, "tcp"); c.Unknown != computed.UnknownSetting {
		t.Errorf("unknown tcp count %+v", c)
	}
	if c := count(t, res, g, "http"); c.Unknown != "" || c.Count != 0 {
		t.Errorf("unknown http count %+v", c)
	}
}

func TestAttachDeps(t *testing.T) {
	g := gw("infra", "edge",
		withFrom(lis("all", "HTTP", 80), "All", nil),
		withFrom(lis("sel", "HTTP", 81), "Selector", map[string]string{"team": "web"}),
	)
	a := route("HTTPRoute", "shop", "a", pref(g, ""))
	b := route("HTTPRoute", "web", "b", pref(g, ""))
	res := run(computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{a, b},
		NamespaceLabels: map[string]map[string]string{"shop": {"team": "web"}, "web": {}}})
	for _, m := range res.Members {
		want := []objects.Ref{g.Ref, m.Route}
		if m.Listener == "sel" {
			want = append(want, nsRef(m.Route.Namespace))
		}
		if !sameRefs(m.Deps, want) {
			t.Errorf("member %s/%s deps %v, want %v", m.Route.Name, m.Listener, m.Deps, want)
		}
	}
	if c := count(t, res, g, "all"); !sameRefs(c.Deps, []objects.Ref{g.Ref, a.Ref, b.Ref}) {
		t.Errorf("all count deps %v", c.Deps)
	}
	if c := count(t, res, g, "sel"); !sameRefs(c.Deps, []objects.Ref{g.Ref, a.Ref, b.Ref, nsRef("shop"), nsRef("web")}) {
		t.Errorf("sel count deps %v", c.Deps)
	}
	if o := outcome(t, res, a, 0); !sameRefs(o.Deps, []objects.Ref{g.Ref, a.Ref, nsRef("shop")}) {
		t.Errorf("outcome deps %v", o.Deps)
	}
}

func sameRefs(got, want []objects.Ref) bool {
	g, w := slices.Clone(got), slices.Clone(want)
	slices.SortFunc(g, compareRef)
	slices.SortFunc(w, compareRef)
	return slices.Equal(g, w)
}

func TestAttachFacts(t *testing.T) {
	https := lis("https", "HTTPS", 443)
	https.Hostname = "*.example.org"
	g := gw("infra", "edge", withFrom(https, "Selector", map[string]string{"team": "web"}))
	tcp := route("TCPRoute", "shop", "t", pref(g, ""))
	ns := route("HTTPRoute", "shop", "n", pref(g, ""))
	host := withHosts(route("HTTPRoute", "web", "h", pref(g, "")), "shop.example.com")
	res := run(computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{tcp, ns, host}, Experimental: expSetting("true", true),
		NamespaceLabels: map[string]map[string]string{"shop": {"team": "retail"}, "web": {"team": "web"}}})
	for _, c := range []struct {
		rt      computed.Route
		outcome computed.Outcome
		fact    computed.Fact
	}{
		{tcp, computed.OutcomeNotAllowed, computed.Fact{Listener: "https", Code: computed.FactKind, Want: "HTTPRoute, GRPCRoute", Got: "TCPRoute"}},
		{ns, computed.OutcomeNotAllowed, computed.Fact{Listener: "https", Code: computed.FactNamespace, Want: "team=web", Got: "team=retail"}},
		{host, computed.OutcomeNoHostname, computed.Fact{Listener: "https", Code: computed.FactHostname, Want: "*.example.org", Got: "shop.example.com"}},
	} {
		o := outcome(t, res, c.rt, 0)
		if o.Outcome != c.outcome || !reflect.DeepEqual(o.Facts, []computed.Fact{c.fact}) {
			t.Errorf("%s: %s %+v, want %s %+v", c.rt.Ref.Name, o.Outcome, o.Facts, c.outcome, c.fact)
		}
	}
}

func TestAttachDeterministic(t *testing.T) {
	g1 := gw("infra", "edge", lis("a", "HTTP", 80), withFrom(lis("b", "HTTP", 81), "All", nil))
	g2 := gw("shop", "edge", lis("a", "HTTP", 80))
	ls := lset("infra", "ls", computed.Accepted, withFrom(lis("c", "HTTP", 82), "Selector", map[string]string{"team": "web"}))
	var routes []computed.Route
	for i, ns := range []string{"infra", "shop", "web", "infra", "shop"} {
		name := string(rune('a' + i))
		routes = append(routes, withHosts(route("HTTPRoute", ns, name, pref(g1, ""), pref(g2, ""), pref(ls, ""), pref(g1, "b")), "x.example.com"))
	}
	in := computed.AttachmentInput{Resources: []computed.Resource{g1, g2, ls}, Routes: routes,
		NamespaceLabels: map[string]map[string]string{"web": {"team": "web"}, "shop": {"team": "web"}}}
	want := run(in)
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // test shuffle
	for range 20 {
		in.Resources = slices.Clone(in.Resources)
		in.Routes = slices.Clone(in.Routes)
		rng.Shuffle(len(in.Resources), func(i, j int) { in.Resources[i], in.Resources[j] = in.Resources[j], in.Resources[i] })
		rng.Shuffle(len(in.Routes), func(i, j int) { in.Routes[i], in.Routes[j] = in.Routes[j], in.Routes[i] })
		if got := run(in); !reflect.DeepEqual(got, want) {
			t.Fatalf("shuffled input changed the result")
		}
	}
}

// largeInput builds the Large tier's view A input from the scale world.
func largeInput(b *testing.B) computed.AttachmentInput {
	in := computed.AttachmentInput{NamespaceLabels: map[string]map[string]string{}, NamespacesOK: true, Experimental: expSetting("true", true)}
	for _, ns := range testutil.ScaleNamespaces(scale.Large.Namespaces) {
		in.NamespaceLabels[ns.Name] = ns.Labels
	}
	for _, u := range scale.World(scale.Large, 1) {
		ref := objects.RefOf(u)
		switch u.GetKind() {
		case "Gateway":
			in.Resources = append(in.Resources, computed.Resource{Ref: ref, Listeners: objects.DecodeGateway(u).Listeners})
		case "ListenerSet":
			in.Resources = append(in.Resources, computed.Resource{Ref: ref, Listeners: objects.DecodeListenerSet(u).Listeners})
		case "HTTPRoute", "GRPCRoute", "TCPRoute", "TLSRoute":
			in.Routes = append(in.Routes, computed.Route{Ref: ref, Hostnames: objects.DecodeRouteHostnames(u).Hostnames,
				ParentRefs: objects.RouteParentRefs(u), Written: objects.WrittenParentRefs(u), ExperimentalOnly: u.GetKind() != "HTTPRoute" && u.GetKind() != "GRPCRoute"})
		}
	}
	if len(in.Resources) == 0 || len(in.Routes) == 0 {
		b.Fatal("empty scale world")
	}
	return in
}

func BenchmarkAttachLarge(b *testing.B) {
	in := largeInput(b)
	b.ReportAllocs()
	for b.Loop() {
		New().Attachment(in)
	}
}

func TestAttachUnknownCountsNarrowed(t *testing.T) {
	// Labels unavailable: a ref naming another section cannot count on the Selector listener.
	g := gw("shop", "edge",
		withFrom(lis("sel", "HTTP", 80), "Selector", map[string]string{"team": "web"}),
		lis("same", "HTTP", 81),
	)
	rt := route("HTTPRoute", "shop", "a", pref(g, "same"))
	res := New().Attachment(computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{rt}})
	if c := count(t, res, g, "sel"); c.Unknown != "" || c.Count != 0 {
		t.Errorf("sel count: %+v", c)
	}
	if c := count(t, res, g, "same"); c.Unknown != "" || c.Count != 1 {
		t.Errorf("same count: %+v", c)
	}

	// Setting unknown: a TCPRoute whose port misses the TCP listener leaves its count known.
	tg := gw("infra", "tcp", lis("tcp", "TCP", 9000))
	ref := pref(tg, "")
	ref.Port = 9001
	tcp := route("TCPRoute", "infra", "t", ref)
	tcp.ExperimentalOnly = true
	res = run(computed.AttachmentInput{Resources: []computed.Resource{tg}, Routes: []computed.Route{tcp}, Experimental: expSetting("", false)})
	if c := count(t, res, tg, "tcp"); c.Unknown != "" || c.Count != 0 {
		t.Errorf("tcp count: %+v", c)
	}
	if o := outcome(t, res, tcp, 0); o.Unknown != computed.UnknownSetting {
		t.Errorf("tcp outcome: %+v", o)
	}
}

func TestAttachInvalidSelector(t *testing.T) {
	l := lis("bad", "HTTP", 80)
	l.AllowedRoutes.Namespaces.From = "Selector"
	l.AllowedRoutes.Namespaces.Selector = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "team", Operator: "Bogus"}}}
	g := gw("infra", "edge", l)
	rt := route("HTTPRoute", "infra", "r", pref(g, ""))
	res := run(computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{rt}, NamespaceLabels: map[string]map[string]string{}})
	c := count(t, res, g, "bad")
	if c.Error == "" || c.Error == "selector must be set" || c.Count != 0 || c.Unknown != "" {
		t.Errorf("count %+v", c)
	}
	o := outcome(t, res, rt, 0)
	if o.Outcome != computed.OutcomeNotAllowed || !reflect.DeepEqual(o.Facts, []computed.Fact{fact("bad", computed.FactSelectorError, "", c.Error)}) {
		t.Errorf("outcome %+v", o)
	}
}

func TestAttachSettingUnknownBeforeLabels(t *testing.T) {
	g := gw("infra", "edge", withFrom(lis("tcp", "TCP", 9000), "Selector", map[string]string{"team": "web"}))
	tcp := route("TCPRoute", "infra", "t", pref(g, ""))
	tcp.ExperimentalOnly = true
	res := New().Attachment(computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{tcp}, Experimental: expSetting("", false)})
	if o := outcome(t, res, tcp, 0); o.Unknown != computed.UnknownSetting {
		t.Errorf("outcome %+v", o)
	}
}

func TestAttachNoSelectorNoNamespaceDep(t *testing.T) {
	g := gw("infra", "edge", withFrom(lis("all", "HTTP", 80), "All", nil))
	rt := route("HTTPRoute", "shop", "r", pref(g, ""))
	res := run(computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{rt}, NamespaceLabels: map[string]map[string]string{"shop": {}}})
	want := []objects.Ref{g.Ref, rt.Ref}
	if o := outcome(t, res, rt, 0); !sameRefs(o.Deps, want) {
		t.Errorf("outcome deps %v", o.Deps)
	}
	if c := count(t, res, g, "all"); !sameRefs(c.Deps, want) {
		t.Errorf("count deps %v", c.Deps)
	}
}

// Results share Deps slices internally, but never across calls or with the input.
func TestAttachResultsIndependent(t *testing.T) {
	g := gw("infra", "edge", lis("a", "HTTP", 80), withFrom(lis("b", "HTTP", 81), "Selector", map[string]string{"team": "web"}))
	rt := withHosts(route("HTTPRoute", "infra", "r", pref(g, ""), pref(g, "c")), "x.example.com")
	in := computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{rt},
		NamespaceLabels: map[string]map[string]string{"infra": {"team": "web"}}}
	want := run(in)
	got := run(in)
	junk := objects.Ref{Name: "junk"}
	for i := range got.Members {
		for j := range got.Members[i].Deps {
			got.Members[i].Deps[j] = junk
		}
		for j := range got.Members[i].Hostnames {
			got.Members[i].Hostnames[j] = "junk"
		}
	}
	for i := range got.Outcomes {
		for j := range got.Outcomes[i].Deps {
			got.Outcomes[i].Deps[j] = junk
		}
		for j := range got.Outcomes[i].Facts {
			got.Outcomes[i].Facts[j].Want = "junk"
		}
		for j := range got.Outcomes[i].Listeners {
			got.Outcomes[i].Listeners[j] = "junk"
		}
	}
	for i := range got.Counts {
		for j := range got.Counts[i].Deps {
			got.Counts[i].Deps[j] = junk
		}
	}
	if again := run(in); !reflect.DeepEqual(again, want) {
		t.Errorf("mutating one result changed the next")
	}
}

// Unknown scope (user, 2026-10-24) makes every count and outcome unknown,
// ahead of a statusless ListenerSet, unavailable labels and an unknown setting.
func TestAttachScopeUnknown(t *testing.T) {
	g := gw("infra", "edge", lis("http", "HTTP", 80), withFrom(lis("sel", "HTTP", 81), "Selector", map[string]string{"team": "web"}), lis("tcp", "TCP", 9000))
	ls := lset("infra", "ls", computed.NoStatus, lis("a", "HTTP", 82))
	web := route("HTTPRoute", "infra", "web", pref(g, ""), pref(ls, ""))
	tcp := route("TCPRoute", "infra", "t", pref(g, "tcp"))
	tcp.ExperimentalOnly = true
	in := computed.AttachmentInput{Resources: []computed.Resource{g, ls}, Routes: []computed.Route{web, tcp}, Experimental: expSetting("", false), ScopeUnknown: true}
	res := New().Attachment(in)
	if len(res.Members) != 0 || len(res.Outcomes) != 3 || len(res.Counts) != 4 {
		t.Fatalf("members %d outcomes %d counts %d", len(res.Members), len(res.Outcomes), len(res.Counts))
	}
	for _, o := range res.Outcomes {
		if o.Unknown != computed.UnknownScope || o.Outcome != "" || o.Listeners != nil || o.Facts != nil {
			t.Errorf("outcome %+v", o)
		}
	}
	for _, c := range res.Counts {
		if c.Unknown != computed.UnknownScope || c.Count != 0 {
			t.Errorf("count %+v", c)
		}
	}
	for _, c := range computed.CheckAttachment("i", computed.Selection{Rules: New()}, in, res) {
		if c.Class != computed.ClassUnknown || c.Candidate() {
			t.Errorf("check %+v", c)
		}
	}
}
