package explore

import (
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

const img = "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7"

// watchScope watches namespaces labelled watch=yes; see watched.
const watchScope = `[{"matchLabels":{"watch":"yes"}}]`

var (
	gw    = testutil.GwRef("infra", "edge")
	gwObj = gw.Ref()
)

// world is a one-install cluster: the kgateway class, Gateway infra/edge
// with its proxy, and objs. Namespaces infra and shop are watched, out is not.
type world struct {
	objs      []*unstructured.Unstructured
	scope     string
	firstSeen func(*unstructured.Unstructured) time.Time
	// seed, when nonzero, shuffles the order of objects and namespaces.
	seed uint64
}

func newWorld(objs ...*unstructured.Unstructured) *world {
	base := []*unstructured.Unstructured{testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.Gateway("infra", "edge", "kgateway", nil)}
	return &world{objs: append(base, objs...), scope: watchScope}
}

func watched() []*corev1.Namespace {
	yes := map[string]string{"watch": "yes"}
	return []*corev1.Namespace{testutil.Namespace("infra", yes), testutil.Namespace("shop", yes), testutil.Namespace("out", nil)}
}

// input assigns and evaluates the world and returns Build's input.
func (w *world) input(t *testing.T) Input {
	t.Helper()
	d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, 1)
	deploys := []*appsv1.Deployment{d}
	for _, o := range w.objs {
		o.SetResourceVersion("1")
		if o.GetKind() == "Gateway" {
			deploys = append(deploys, testutil.ProxyDeployment(o.GetNamespace(), o.GetName(), 1, 1))
		}
	}
	nss := watched()
	snap := &cache.Snapshot{Objects: testutil.ObjectsByGVR(t, w.objs...), Access: map[schema.GroupVersionResource]cache.Access{},
		Namespaces: nss, Deployments: deploys, Pods: []*corev1.Pod{testutil.ControllerPod(d, "p", true)}, Synced: true, TakenAt: testutil.Now}
	if w.seed != 0 {
		rng := rand.New(rand.NewPCG(w.seed, w.seed))
		for _, l := range snap.Objects {
			rng.Shuffle(len(l), func(i, j int) { l[i], l[j] = l[j], l[i] })
		}
		rng.Shuffle(len(nss), func(i, j int) { nss[i], nss[j] = nss[j], nss[i] })
	}
	served := testutil.ServedUpstream()
	for _, k := range served {
		snap.Access[k.GVR()] = cache.Access{OK: true}
	}
	for _, gvr := range []schema.GroupVersionResource{cache.NamespacesGVR, cache.PodsGVR, cache.DeploymentsGVR} {
		snap.Access[gvr] = cache.Access{OK: true}
	}
	in, _ := install.UpstreamDetector{}.Detect(d)
	res := install.Resolved{Install: in, Scope: install.ParseSelectors(w.scope)}
	a := install.Assign(snap, served, []install.Resolved{res}, nil)
	owned := a.PerInstall[res.ID()]
	rep := health.Evaluate(health.Input{Install: res, Owned: owned, Assignment: a, Snapshot: snap, Served: served, Now: testutil.Now,
		Metrics:    ctrlmetrics.Result{CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{}, ManagedPresent: true}},
		HasMetrics: true, MetricsMaxAge: 30 * time.Second, Verify: scopeverify.Result{Available: true}, FirstSeen: w.firstSeen}, health.Upstream())
	return Input{Install: res, Owned: owned, Assignment: a, Snapshot: snap, Served: served, Health: rep}
}

func (w *world) build(t *testing.T) *Index {
	t.Helper()
	return Build(w.input(t))
}

func routeRef(ns, name string) objects.Ref {
	return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: ns, Name: name}
}

// row returns the row of ns/name; it fails the test when there is none.
func row(t *testing.T, ix *Index, ns, name string) (RowID, *RouteRow) {
	t.Helper()
	for i := range ix.Routes {
		if r := &ix.Routes[i]; r.Ref.Namespace == ns && r.Ref.Name == name {
			return RowID(i), r
		}
	}
	t.Fatalf("no row %s/%s", ns, name)
	return 0, nil
}

func ids(t *testing.T, ix *Index, names ...string) []RowID {
	t.Helper()
	out := []RowID{}
	for _, n := range names {
		id, _ := row(t, ix, "shop", n)
		out = append(out, id)
	}
	return out
}

func rowNames(ix *Index) []string {
	var out []string
	for _, r := range ix.Routes {
		out = append(out, r.Ref.String())
	}
	return out
}

func cond(typ string, st metav1.ConditionStatus, reason, msg string) metav1.Condition {
	c := testutil.Cond(typ, st, reason)
	c.Message = msg
	return c
}

// status is a status.parents entry from the install's controller.
func status(p objects.ParentRef, cs ...metav1.Condition) testutil.RouteParent {
	return testutil.RouteParent{Ref: p, Controller: testutil.Controller, Conds: cs}
}

func accepted(p objects.ParentRef) testutil.RouteParent {
	return testutil.AcceptedBy(p, testutil.Controller)
}

var resolved = cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs", "")

func TestRowsCoverEveryRouteThatTargetsTheInstall(t *testing.T) {
	api := testutil.HTTPRouteRef("shop", "api")
	other := testutil.GwRef("infra", "theirs")
	ix := newWorld(
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, accepted(gw)),
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, accepted(gw)),
		testutil.HTTPRoute("shop", "c", []objects.ParentRef{api}),                                   // declared
		testutil.HTTPRoute("shop", "lost", []objects.ParentRef{testutil.GwRef("infra", "missing")}), // unattached
		testutil.HTTPRoute("out", "far", []objects.ParentRef{gw}),                                   // unwatched
		testutil.HTTPRoute("shop", "free", nil),                                                     // no parent
		testutil.GatewayClass("other", "example.com/other", true),
		testutil.Gateway("infra", "theirs", "other", nil),
		testutil.HTTPRoute("shop", "theirs", []objects.ParentRef{other}, testutil.AcceptedBy(other, "example.com/other")),
	).build(t)

	if want := []string{"out/far", "shop/api", "shop/c", "shop/cart", "shop/free", "shop/lost"}; !slices.Equal(rowNames(ix), want) {
		t.Fatalf("rows %q, want %q", rowNames(ix), want)
	}
	attachments := func(r *RouteRow) []Attachment {
		var out []Attachment
		for _, p := range r.Parents {
			out = append(out, p.Attachment)
		}
		return out
	}
	for _, tc := range []struct {
		ns, name string
		owned    bool
		att      []Attachment
		target   objects.Ref
		flags    Flag
	}{
		{"out", "far", false, []Attachment{AttachmentUnwatched}, gwObj, 0},
		{"shop", "api", true, []Attachment{AttachmentAccepted}, gwObj, FlagAccepted},
		{"shop", "c", false, []Attachment{AttachmentDeclared}, api.Ref(), FlagDeclared},
		{"shop", "cart", true, []Attachment{AttachmentAccepted}, gwObj, FlagAccepted},
		{"shop", "free", false, nil, objects.Ref{}, FlagNoParent},
		{"shop", "lost", false, []Attachment{AttachmentPending}, testutil.GwRef("infra", "missing").Ref(), FlagPending},
	} {
		_, r := row(t, ix, tc.ns, tc.name)
		if r.Owned != tc.owned || !slices.Equal(attachments(r), tc.att) || r.Flags != tc.flags {
			t.Errorf("%s/%s: owned %v attachments %v flags %v, want %v %v %v", tc.ns, tc.name, r.Owned, attachments(r), r.Flags.Names(), tc.owned, tc.att, tc.flags.Names())
		}
		if len(r.Parents) > 0 && r.Parents[0].Target != tc.target {
			t.Errorf("%s/%s: target %v, want %v", tc.ns, tc.name, r.Parents[0].Target, tc.target)
		}
		if r.View == nil || r.Kind.Kind != "HTTPRoute" || r.Obj == nil {
			t.Errorf("%s/%s: view %v kind %q", tc.ns, tc.name, r.View, r.Kind.Kind)
		}
	}
	far, _ := row(t, ix, "out", "far")
	if want := append([]RowID{far}, ids(t, ix, "api", "cart")...); !slices.Equal(ix.ByParent[gwObj], want) {
		t.Errorf("ByParent[gateway] %v, want %v", ix.ByParent[gwObj], want)
	}
	if want := ids(t, ix, "c"); !slices.Equal(ix.Declared[api.Ref()], want) {
		t.Errorf("Declared %v, want %v", ix.Declared[api.Ref()], want)
	}
	if want := ids(t, ix, "free"); !slices.Equal(ix.NoParent, want) {
		t.Errorf("NoParent %v, want %v", ix.NoParent, want)
	}
	if want := ids(t, ix, "api", "c", "cart", "free", "lost"); !slices.Equal(ix.ByNamespace["shop"], want) {
		t.Errorf("ByNamespace[shop] %v, want %v", ix.ByNamespace["shop"], want)
	}
	if _, r := row(t, ix, "shop", "cart"); r.search != "shop/cart" {
		t.Errorf("search %q", r.search)
	}
}

func TestSearchIsLowerCasedNameAndHostnames(t *testing.T) {
	ix := newWorld(testutil.WithHostnames(testutil.HTTPRoute("shop", "Cart", []objects.ParentRef{gw}, accepted(gw)), "Shop.Example.COM", "b.example.com")).build(t)
	_, r := row(t, ix, "shop", "Cart")
	if r.search != "shop/cart\nshop.example.com\nb.example.com" || !slices.Equal(r.Hostnames, []string{"Shop.Example.COM", "b.example.com"}) {
		t.Fatalf("search %q hostnames %q", r.search, r.Hostnames)
	}
}

func TestAttachmentFromCurrentStatusOnly(t *testing.T) {
	stale := func(name string, st metav1.ConditionStatus) *unstructured.Unstructured {
		u := testutil.HTTPRoute("shop", name, []objects.ParentRef{gw}, status(gw, cond("Accepted", st, "Accepted", ""), resolved))
		u.SetGeneration(2) // conditions describe generation 1
		return u
	}
	reject := func(name, reason string) *unstructured.Unstructured {
		return testutil.HTTPRoute("shop", name, []objects.ParentRef{gw}, status(gw, cond("Accepted", metav1.ConditionFalse, reason, "msg "+reason), resolved))
	}
	ls := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	ix := newWorld(
		stale("stale-true", metav1.ConditionTrue),
		stale("stale-false", metav1.ConditionFalse),
		reject("listeners", "NotAllowedByListeners"),
		reject("parent", "NoMatchingParent"),
		reject("hostname", "NoMatchingListenerHostname"),
		testutil.HTTPRoute("shop", "none", []objects.ParentRef{gw}),
		testutil.ListenerSet("infra", "ls", gw, nil),
		testutil.HTTPRoute("shop", "via", []objects.ParentRef{ls}, accepted(ls)),
	).build(t)

	for _, name := range []string{"stale-true", "stale-false"} {
		_, r := row(t, ix, "shop", name)
		p := r.Parents[0]
		if p.Attachment != AttachmentPending || p.Pending != "status describes an older generation" || p.Cond != nil || r.Flags != FlagPending {
			t.Errorf("%s: %+v flags %v", name, p, r.Flags.Names())
		}
	}
	for _, name := range []string{"listeners", "parent", "hostname"} {
		_, r := row(t, ix, "shop", name)
		p := r.Parents[0]
		if p.Attachment != AttachmentNotAccepted || p.Cond == nil || p.Cond.Type != "Accepted" || p.Cond.Message != "msg "+p.Cond.Reason {
			t.Errorf("%s: %+v", name, p)
		}
	}
	if _, r := row(t, ix, "shop", "listeners"); r.Parents[0].Cond.Reason != "NotAllowedByListeners" {
		t.Errorf("reason %q", r.Parents[0].Cond.Reason)
	}
	if _, r := row(t, ix, "shop", "none"); r.Parents[0].Attachment != AttachmentPending || r.Parents[0].Pending != "no status from the controller" {
		t.Errorf("no status: %+v", r.Parents[0])
	}
	id, r := row(t, ix, "shop", "via")
	if p := r.Parents[0]; p.Target != ls.Ref() || p.Via == nil || *p.Via != gwObj || p.Attachment != AttachmentAccepted {
		t.Fatalf("via: %+v", p)
	}
	if !slices.Contains(ix.ByParent[gwObj], id) || !slices.Equal(ix.ByParent[ls.Ref()], []RowID{id}) {
		t.Errorf("ByParent gateway %v, listenerset %v", ix.ByParent[gwObj], ix.ByParent[ls.Ref()])
	}
}

// Assign records one target per unwatched route; the row is under every
// parent of the install it names: a Gateway and that Gateway's ListenerSet.
func TestUnwatchedRouteNamingGatewayAndListenerSet(t *testing.T) {
	ls := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	ix := newWorld(testutil.ListenerSet("infra", "ls", gw, nil), testutil.HTTPRoute("out", "far", []objects.ParentRef{gw, ls})).build(t)
	id, r := row(t, ix, "out", "far")
	if len(r.Parents) != 2 || r.Parents[0].Attachment != AttachmentUnwatched || r.Parents[1].Attachment != AttachmentUnwatched ||
		r.Parents[1].Target != ls.Ref() || r.Parents[1].Via == nil || *r.Parents[1].Via != gwObj {
		t.Fatalf("parents %+v", r.Parents)
	}
	if !slices.Equal(ix.ByParent[gwObj], []RowID{id}) || !slices.Equal(ix.ByParent[ls.Ref()], []RowID{id}) {
		t.Fatalf("ByParent gateway %v, listenerset %v", ix.ByParent[gwObj], ix.ByParent[ls.Ref()])
	}
}

func TestFlags(t *testing.T) {
	api := testutil.HTTPRouteRef("shop", "api")
	other := testutil.GwRef("infra", "other")
	with := func(name string, cs ...metav1.Condition) *unstructured.Unstructured {
		return testutil.HTTPRoute("shop", name, []objects.ParentRef{gw}, status(gw, cs...))
	}
	acc := cond("Accepted", metav1.ConditionTrue, "Accepted", "")
	ix := newWorld(
		testutil.Gateway("infra", "other", "kgateway", nil),
		with("accepted", acc, resolved),
		with("not-accepted", cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners", ""), resolved),
		with("refs-unresolved", acc, cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound", "")),
		with("programmed-false", acc, resolved, cond("kgateway.dev/Programmed", metav1.ConditionFalse, "RouteRuleDropped", "")),
		with("replaced", cond("Accepted", metav1.ConditionFalse, "RouteRuleReplaced", ""), resolved),
		with("dropped", cond("Accepted", metav1.ConditionFalse, "RouteRuleDropped", ""), resolved),
		testutil.HTTPRoute("shop", "pending", []objects.ParentRef{gw}),
		testutil.HTTPRoute("shop", "mixed", []objects.ParentRef{gw, other}, accepted(gw)),
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, accepted(gw)),
		testutil.HTTPRoute("shop", "declared", []objects.ParentRef{api}),
		testutil.HTTPRoute("shop", "no-parent", nil),
	).build(t)
	for name, want := range map[string]Flag{
		"accepted":         FlagAccepted,
		"not-accepted":     FlagNotAccepted,
		"refs-unresolved":  FlagAccepted | FlagRefsUnresolved,
		"programmed-false": FlagAccepted | FlagNotProgrammed,
		"replaced":         FlagNotAccepted | FlagNotProgrammed, // 2.3 shape
		"dropped":          FlagNotAccepted | FlagNotProgrammed,
		"pending":          FlagPending,
		"mixed":            FlagAccepted | FlagPending,
		"declared":         FlagDeclared,
		"no-parent":        FlagNoParent,
	} {
		if _, r := row(t, ix, "shop", name); r.Flags != want {
			t.Errorf("%s: flags %v, want %v", name, r.Flags.Names(), want.Names())
		}
	}
	if _, r := row(t, ix, "shop", "refs-unresolved"); r.Parents[0].Cond == nil || r.Parents[0].Cond.Type != "ResolvedRefs" {
		t.Errorf("deciding condition %+v", r.Parents[0].Cond)
	}
	if _, r := row(t, ix, "shop", "accepted"); r.Parents[0].Cond != nil {
		t.Errorf("accepted has deciding condition %+v", r.Parents[0].Cond)
	}
	all := Flag(1<<9 - 1)
	if want := []string{"accepted", "not-accepted", "refs-unresolved", "not-programmed", "pending", "declared", "no-parent", "disagrees", "listener-unknown"}; !slices.Equal(all.Names(), want) {
		t.Fatalf("names %q", all.Names())
	}
	for _, n := range all.Names() {
		if f, ok := ParseFlag(n); !ok || f.Names()[0] != n {
			t.Errorf("ParseFlag(%q) = %v, %v", n, f, ok)
		}
	}
	if _, ok := ParseFlag("dropped"); ok {
		t.Error("ParseFlag accepted a 2.4 flag")
	}
}

func TestRowStateRules(t *testing.T) {
	cart := routeRef("shop", "cart")
	f := func(sev health.Severity, res objects.Ref) health.Finding {
		return health.Finding{Code: "KGWUI-002", Severity: sev, Resource: &res}
	}
	okCheck := health.CheckResult{Name: "resource status", OK: true, Required: true}
	pending := health.CheckResult{Name: "resource status", Required: true, Pending: true}
	optional := health.CheckResult{Name: "backend status", Required: false}
	for _, tc := range []struct {
		name string
		fs   []health.Finding
		cs   []health.CheckResult
		want health.State
	}{
		{"warning and a pending required check", []health.Finding{f(health.SeverityWarning, cart)}, []health.CheckResult{pending}, health.StateUnknown},
		{"info only, required checks OK", []health.Finding{f(health.SeverityInfo, cart)}, []health.CheckResult{okCheck, optional}, health.StateHealthy},
		{"error beats unknown", []health.Finding{f(health.SeverityWarning, cart), f(health.SeverityError, cart)}, []health.CheckResult{pending}, health.StateError},
		{"warning", []health.Finding{f(health.SeverityWarning, cart)}, []health.CheckResult{okCheck}, health.StateWarning},
		{"another object's error", []health.Finding{f(health.SeverityError, gwObj)}, []health.CheckResult{okCheck}, health.StateHealthy},
	} {
		if got := RowState(cart, health.Report{Findings: tc.fs, Checks: tc.cs}); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}

	// A Gateway with a failing route keeps its own state.
	in := newWorld(
		testutil.HTTPRoute("shop", "bad", []objects.ParentRef{gw}, status(gw, cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners", ""), resolved)),
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, accepted(gw)),
	).input(t)
	ix := Build(in)
	if _, r := row(t, ix, "shop", "bad"); r.State != health.StateError {
		t.Errorf("failing route %s, want error", r.State)
	}
	if ix.Gateways[0].State != health.StateHealthy {
		t.Errorf("gateway %s, want healthy", ix.Gateways[0].State)
	}
	for _, r := range ix.Routes {
		if r.State != RowState(r.Ref, in.Health) {
			t.Errorf("%s: state %s, RowState %s", r.Ref, r.State, RowState(r.Ref, in.Health))
		}
	}
	if w := ix.Findings[routeRef("shop", "bad")]; w.Error == nil || w.Error.Code != health.CodeConditionFalse || w.Warning != nil {
		t.Errorf("findings %+v", w)
	}
}

func TestKGWUI011InGraceMakesEveryRowUnknown(t *testing.T) {
	api := testutil.HTTPRouteRef("shop", "api")
	w := newWorld(
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, accepted(gw)),
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, accepted(gw)),
		testutil.HTTPRoute("shop", "c", []objects.ParentRef{api}),
	)
	age := func(d time.Duration) func(*unstructured.Unstructured) time.Time {
		return func(u *unstructured.Unstructured) time.Time {
			if u.GetName() == "c" {
				return testutil.Now.Add(-d)
			}
			return testutil.Now.Add(-time.Hour)
		}
	}
	w.firstSeen = age(10 * time.Second)
	ix := w.build(t)
	if ix.Gateways[0].State != health.StateUnknown {
		t.Errorf("in grace: gateway %s, want unknown", ix.Gateways[0].State)
	}
	for _, r := range ix.Routes {
		if r.State != health.StateUnknown {
			t.Errorf("in grace: %s %s, want unknown", r.Ref, r.State)
		}
	}
	w.firstSeen = age(2 * health.Grace)
	ix = w.build(t)
	if ix.Gateways[0].State != health.StateHealthy {
		t.Errorf("after grace: gateway %s, want healthy", ix.Gateways[0].State)
	}
	if _, r := row(t, ix, "shop", "c"); r.State != health.StateWarning {
		t.Errorf("after grace: declared %s, want warning", r.State)
	}
}

func TestByBackendIndex(t *testing.T) {
	mirror := func(name string) map[string]any {
		return map[string]any{"type": "RequestMirror", "requestMirror": map[string]any{"backendRef": map[string]any{"name": name, "port": int64(80)}}}
	}
	svc := func(name string) map[string]any { return map[string]any{"name": name, "port": int64(80)} }
	be := map[string]any{"group": "gateway.kgateway.dev", "kind": "Backend", "name": "be1"}
	rule := func(backends []any, filters ...any) map[string]any {
		r := testutil.Rule()
		r["backendRefs"] = backends
		if len(filters) > 0 {
			r["filters"] = filters
		}
		return r
	}
	grpc := testutil.WithRules(testutil.HTTPRoute("shop", "g1", []objects.ParentRef{gw}, accepted(gw)), rule([]any{svc("grpc-svc")}))
	grpc.SetAPIVersion("gateway.networking.k8s.io/v1")
	grpc.SetKind("GRPCRoute")
	tcp := testutil.WithRules(testutil.HTTPRoute("shop", "t1", []objects.ParentRef{gw}, accepted(gw)), rule([]any{svc("tcp-svc")}))
	tcp.SetKind("TCPRoute")
	tls := testutil.WithRules(testutil.HTTPRoute("shop", "s1", []objects.ParentRef{gw}, accepted(gw)), rule([]any{svc("tls-svc"), be}))
	tls.SetKind("TLSRoute")
	ix := newWorld(
		testutil.WithRules(testutil.HTTPRoute("shop", "r1", []objects.ParentRef{gw}, accepted(gw)), rule([]any{svc("cart"), be}, mirror("mirror-svc"))),
		testutil.WithRules(testutil.HTTPRoute("shop", "r2", []objects.ParentRef{gw}, accepted(gw)), rule([]any{svc("both")}, mirror("both"))),
		testutil.WithRules(testutil.HTTPRoute("shop", "r3", []objects.ParentRef{gw}, accepted(gw)), rule([]any{testutil.DelegateTo("shop", "child")})),
		grpc, tcp, tls,
	).build(t)
	service := func(name string) objects.Ref { return objects.Ref{Kind: "Service", Namespace: "shop", Name: name} }
	r1, r2 := ids(t, ix, "r1")[0], ids(t, ix, "r2")[0]
	g1, _ := row(t, ix, "shop", "g1")
	t1, _ := row(t, ix, "shop", "t1")
	s1, _ := row(t, ix, "shop", "s1")
	want := map[objects.Ref][]BackendUse{
		service("cart"): {{Row: r1}},
		{Group: "gateway.kgateway.dev", Kind: "Backend", Namespace: "shop", Name: "be1"}: {{Row: r1}, {Row: s1}},
		service("mirror-svc"): {{Row: r1, Mirror: true}},
		service("both"):       {{Row: r2}},
		service("grpc-svc"):   {{Row: g1}},
		service("tcp-svc"):    {{Row: t1}},
		service("tls-svc"):    {{Row: s1}},
	}
	if !reflect.DeepEqual(ix.ByBackend, want) {
		t.Fatalf("ByBackend\n got %v\nwant %v", ix.ByBackend, want)
	}
}

func TestReportedEdgeIndexes(t *testing.T) {
	api, api2 := testutil.HTTPRouteRef("shop", "api"), testutil.HTTPRouteRef("shop", "api2")
	ix := newWorld(
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, accepted(gw)),
		testutil.HTTPRoute("shop", "api2", []objects.ParentRef{gw}, accepted(gw)),
		testutil.HTTPRoute("shop", "kid", []objects.ParentRef{api2, api}, accepted(api2), accepted(api)),
	).build(t)
	kid, k := row(t, ix, "shop", "kid")
	if want := []objects.Ref{api.Ref(), api2.Ref()}; !slices.Equal(k.ParentRoutes, want) || !slices.Equal(ix.ParentRoutes[k.Ref], want) {
		t.Errorf("parent routes %v / %v, want %v", k.ParentRoutes, ix.ParentRoutes[k.Ref], want)
	}
	// Parents keep spec order.
	if len(k.Parents) != 2 || k.Parents[0].Target != api2.Ref() || k.Parents[1].Target != api.Ref() || k.Flags != FlagAccepted || !k.Owned {
		t.Errorf("kid parents %+v flags %v", k.Parents, k.Flags.Names())
	}
	if !slices.Equal(ix.ByParentRoute[api.Ref()], []RowID{kid}) || !slices.Equal(ix.ByParentRoute[api2.Ref()], []RowID{kid}) {
		t.Errorf("ByParentRoute %v", ix.ByParentRoute)
	}
	if _, a := row(t, ix, "shop", "api"); a.Children != 1 {
		t.Errorf("api children %d", a.Children)
	}
	if _, ok := ix.ByParent[api.Ref()]; ok {
		t.Error("a reported edge is in ByParent")
	}
}

func TestGatewayAndListenerSetRows(t *testing.T) {
	ls := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	edge := testutil.Gateway("infra", "edge", "kgateway", nil,
		testutil.Listener{Name: "http", Attached: 2, Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}},
		testutil.Listener{Name: "https", Attached: 1, Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}})
	w := newWorld(
		testutil.ListenerSet("infra", "ls", gw, nil),
		testutil.ListenerSet("infra", "ls2", gw, []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowed"), testutil.Cond("Programmed", metav1.ConditionFalse, "NotAllowed")}),
		testutil.HTTPRoute("shop", "direct", []objects.ParentRef{gw}, accepted(gw)),
		testutil.HTTPRoute("shop", "bad", []objects.ParentRef{gw}, status(gw, cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners", ""), resolved)),
		testutil.HTTPRoute("shop", "through", []objects.ParentRef{ls}, accepted(ls)),
	)
	w.objs[1] = edge
	ix := w.build(t)
	if len(ix.Gateways) != 1 || len(ix.ListenerSets) != 2 {
		t.Fatalf("gateways %d listenersets %d", len(ix.Gateways), len(ix.ListenerSets))
	}
	g := ix.Gateways[0]
	if g.Ref != gwObj || g.View == nil || g.AttachedRoutes != 3 || !slices.Equal(g.Routes, ids(t, ix, "bad", "direct", "through")) ||
		!slices.Equal(g.NotAccepted, ids(t, ix, "bad")) || !slices.Equal(g.ListenerSets, []RowID{0, 1}) {
		t.Errorf("gateway row %+v", g)
	}
	l := ix.ListenerSets[0]
	if l.Ref != ls.Ref() || l.View == nil || l.Parent != gwObj || !l.ParentFound || l.AcceptedReason != "" || !slices.Equal(l.Routes, ids(t, ix, "through")) {
		t.Errorf("listenerset row %+v", l)
	}
	if l2 := ix.ListenerSets[1]; l2.AcceptedReason != "NotAllowed" || l2.State != health.StateError || len(l2.Routes) != 0 {
		t.Errorf("rejected listenerset %+v", l2)
	}
}

func TestAdmissionTable(t *testing.T) {
	api := testutil.HTTPRouteRef("shop", "api")
	orphanLS := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "orphan"}
	backendRef := func(ns, name string) map[string]any {
		return map[string]any{"group": "gateway.kgateway.dev", "kind": "Backend", "namespace": ns, "name": name}
	}
	rule := func(refs ...any) map[string]any { r := testutil.Rule(); r["backendRefs"] = refs; return r }
	cart := testutil.WithRules(testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw, orphanLS}, accepted(gw)),
		rule(backendRef("out", "be"), map[string]any{"name": "svc"}))
	ix := newWorld(
		cart,
		testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, accepted(gw)), rule(testutil.DelegateTo("out", "child"), backendRef("out", "be"))),
		testutil.HTTPRoute("shop", "lost", []objects.ParentRef{testutil.GwRef("infra", "missing")}), // unattached: KGWUI-003
		testutil.HTTPRoute("out", "child", []objects.ParentRef{testutil.GwRef("infra", "elsewhere")}),
		testutil.WithRules(testutil.HTTPRoute("shop", "c", []objects.ParentRef{api}), rule(backendRef("out", "d"))), // candidate C
		testutil.HTTPRoute("out", "far", []objects.ParentRef{gw}),
		testutil.HTTPRoute("shop", "free", nil),
		testutil.ListenerSet("infra", "orphan", testutil.GwRef("infra", "gone"), nil),
		testutil.Backend("out", "be", nil),
		testutil.Backend("out", "d", nil),
	).build(t)
	cartRef, apiRef := routeRef("shop", "cart"), routeRef("shop", "api")
	be := objects.Ref{Group: "gateway.kgateway.dev", Kind: "Backend", Namespace: "out", Name: "be"}
	for _, tc := range []struct {
		name string
		ref  objects.Ref
		want Admission
		ok   bool
	}{
		{"owned route", cartRef, Admission{Owned: true}, true},
		{"owned gateway", gwObj, Admission{Owned: true}, true},
		{"owned class", objects.Ref{Group: objects.GatewayAPIGroup, Kind: "GatewayClass", Name: "kgateway"}, Admission{Owned: true}, true},
		{"declared candidate", routeRef("shop", "c"), Admission{Reason: ReasonFinding}, true},
		{"unwatched route", routeRef("out", "far"), Admission{Reason: ReasonFinding}, true},
		{"unattached route", routeRef("shop", "lost"), Admission{Reason: ReasonFinding}, true},
		{"backendRef target of two routes", be, Admission{Reason: ReasonReferenced, By: &apiRef, Referrers: []objects.Ref{apiRef, cartRef}}, true},
		{"parentRef target", orphanLS.Ref(), Admission{Reason: ReasonReferenced, By: &cartRef, Referrers: []objects.Ref{cartRef}}, true},
		{"delegation name ref target", routeRef("out", "child"), Admission{Reason: ReasonReferenced, By: &apiRef, Referrers: []objects.Ref{apiRef}}, true},
		{"no-parent row", routeRef("shop", "free"), Admission{NoParent: true}, true},
		{"Service is never served", objects.Ref{Kind: "Service", Namespace: "shop", Name: "svc"}, Admission{}, false},
		{"named only by candidate C", objects.Ref{Group: "gateway.kgateway.dev", Kind: "Backend", Namespace: "out", Name: "d"}, Admission{}, false},
		{"unrelated", routeRef("shop", "nothing"), Admission{}, false},
	} {
		got, ok := ix.Admission(tc.ref)
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %+v %v, want %+v %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

// deterministicWorld has every row type, a child with two reported parents
// and a ListenerSet.
func deterministicWorld() *world {
	api, api2 := testutil.HTTPRouteRef("shop", "api"), testutil.HTTPRouteRef("shop", "api2")
	ls := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	return newWorld(
		testutil.ListenerSet("infra", "ls", gw, nil),
		testutil.ListenerSet("infra", "ls2", gw, nil),
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, accepted(gw)),
		testutil.HTTPRoute("shop", "api2", []objects.ParentRef{ls}, accepted(ls)),
		testutil.HTTPRoute("shop", "kid", nil, accepted(api2), accepted(api)), // implicit child of both
		testutil.HTTPRoute("shop", "c", []objects.ParentRef{api}),
		testutil.HTTPRoute("shop", "lost", []objects.ParentRef{testutil.GwRef("infra", "missing")}),
		testutil.HTTPRoute("out", "far", []objects.ParentRef{gw}),
		testutil.HTTPRoute("shop", "free", nil),
		testutil.Backend("shop", "be", nil),
	)
}

// order is the snapshot-dependent order Build must not show: the owned
// routes and the implicit child's reported parents, as Assign produced them.
func order(in Input) []string {
	var out []string
	for _, r := range in.Owned.Routes {
		out = append(out, r.Obj.GetName())
		if r.Obj.GetName() == "kid" {
			for _, p := range r.RouteParents {
				out = append(out, "kid<-"+p.Name)
			}
		}
	}
	return out
}

func TestBuildIsDeterministic(t *testing.T) {
	w := deterministicWorld()
	base := w.input(t)
	a := Build(base)
	var in Input
	for w.seed = 1; w.seed < 100; w.seed++ {
		if in = w.input(t); !slices.Equal(order(in), order(base)) {
			break
		}
	}
	if slices.Equal(order(in), order(base)) {
		t.Fatal("no shuffle changed the assignment order")
	}
	b := Build(in)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("Builds over shuffled snapshots (%v, %v) differ:\n%v\n%v", order(base), order(in), rowNames(a), rowNames(b))
	}
	if !reflect.DeepEqual(Build(in), b) {
		t.Fatal("two Builds over one input differ")
	}
	if _, k := row(t, a, "shop", "kid"); len(k.Parents) != 2 || k.Parents[0].Target.Name != "api" {
		t.Fatalf("implicit child parents %+v", k.Parents)
	}
}

// stripObjects returns a copy of ix without object pointers.
func stripObjects(ix *Index) Index {
	c := *ix
	c.Routes = slices.Clone(ix.Routes)
	for i := range c.Routes {
		c.Routes[i].Obj = nil
	}
	c.Gateways = slices.Clone(ix.Gateways)
	for i := range c.Gateways {
		c.Gateways[i].Obj = nil
	}
	c.ListenerSets = slices.Clone(ix.ListenerSets)
	for i := range c.ListenerSets {
		c.ListenerSets[i].Obj = nil
	}
	return c
}

// Design §9.7: no Pods or Deployments and no uid or resourceVersion give the same rows.
func TestBuildWithoutPodsOrUIDs(t *testing.T) {
	full := deterministicWorld().input(t)
	bare := full
	snap := *full.Snapshot
	snap.Pods, snap.Deployments = nil, nil
	snap.Objects = map[schema.GroupVersionResource][]*unstructured.Unstructured{}
	for gvr, l := range full.Snapshot.Objects {
		for _, u := range l {
			c := u.DeepCopy()
			c.SetUID("")
			c.SetResourceVersion("")
			snap.Objects[gvr] = append(snap.Objects[gvr], c)
		}
	}
	bare.Snapshot = &snap
	bare.Assignment = install.Assign(&snap, full.Served, []install.Resolved{full.Install}, nil)
	bare.Owned = bare.Assignment.PerInstall[full.Install.ID()]
	a, b := Build(full), Build(bare)
	if len(a.Routes) == 0 || !reflect.DeepEqual(stripObjects(a), stripObjects(b)) {
		t.Fatalf("rows differ without Pods, Deployments or uids:\n%v\n%v", rowNames(a), rowNames(b))
	}
}

func TestDetailKinds(t *testing.T) {
	want := []objects.GroupKind{{Group: objects.GatewayAPIGroup, Kind: "Gateway"}, {Group: objects.GatewayAPIGroup, Kind: "ListenerSet"},
		{Group: "gateway.networking.x-k8s.io", Kind: "XListenerSet"}, {Group: objects.GatewayAPIGroup, Kind: "HTTPRoute"}}
	if got := DetailKinds(testutil.ServedUpstream()); !slices.Equal(got, want) {
		t.Fatalf("DetailKinds %v", got)
	}
}

// View A fixtures: Gateway infra/edge with listeners http and https, a
// ListenerSet infra/ls with listener extra, and routes in shop:
//   - a: one ref to edge, a member of http;
//   - b: two refs (https by section, and edge), both members of https;
//   - c: one ref to ls, a member of extra;
//   - u: one ref to edge, outcome unknown;
//   - r: one ref to edge, rejected by status and computed NotAllowedByListeners.
var (
	lsRef  = objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	lsObj  = lsRef.Ref()
	gwTLS  = objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge", SectionName: "https"}
	shopNS = objects.Ref{Kind: "Namespace", Name: "shop"}
)

// viewWorld is the world of the view A fixtures.
func viewWorld() *world {
	ok := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}
	w := newWorld(
		testutil.ListenerSet("infra", "ls", gw, nil),
		testutil.HTTPRoute("shop", "a", []objects.ParentRef{gw}, accepted(gw)),
		testutil.HTTPRoute("shop", "b", []objects.ParentRef{gwTLS, gw}, accepted(gwTLS), accepted(gw)),
		testutil.HTTPRoute("shop", "c", []objects.ParentRef{lsRef}, accepted(lsRef)),
		testutil.HTTPRoute("shop", "u", []objects.ParentRef{gw}, accepted(gw)),
		testutil.HTTPRoute("shop", "r", []objects.ParentRef{gw}, status(gw, cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners", ""), resolved)),
	)
	w.objs[1] = testutil.Gateway("infra", "edge", "kgateway", nil, testutil.Listener{Name: "http", Conds: ok}, testutil.Listener{Name: "https", Conds: ok})
	return w
}

// views are view A over viewWorld. Every slice is fresh, so a test may change one.
func views() *computed.Views {
	a, b, c, u, r := routeRef("shop", "a"), routeRef("shop", "b"), routeRef("shop", "c"), routeRef("shop", "u"), routeRef("shop", "r")
	bDeps := []objects.Ref{b, gwObj, shopNS} // https uses Selector
	return &computed.Views{RuleSet: "kgateway-2.4", Note: "a note", Attachment: computed.AttachmentResult{
		Members: []computed.Member{
			{Resource: gwObj, Listener: "http", Route: a, ParentRef: 0, Hostnames: []string{"a.example.com"}, Deps: []objects.Ref{a, gwObj}},
			{Resource: gwObj, Listener: "https", Route: b, ParentRef: 0, Hostnames: []string{"b.example.com"}, Deps: bDeps},
			{Resource: gwObj, Listener: "https", Route: b, ParentRef: 1, Hostnames: []string{"b.example.com"}, Deps: bDeps},
			{Resource: lsObj, Listener: "extra", Route: c, ParentRef: 0, Hostnames: []string{"c.example.com"}, Deps: []objects.Ref{c, lsObj}},
		},
		Outcomes: []computed.RefOutcome{
			{Route: a, ParentRef: 0, Resource: gwObj, Outcome: computed.OutcomeAccepted, Listeners: []string{"http"}, Rule: "hostname-intersection", Deps: []objects.Ref{a, gwObj}},
			{Route: b, ParentRef: 0, Resource: gwObj, Outcome: computed.OutcomeAccepted, Listeners: []string{"https"}, Rule: "hostname-intersection", Deps: bDeps},
			{Route: b, ParentRef: 1, Resource: gwObj, Outcome: computed.OutcomeAccepted, Listeners: []string{"https"}, Rule: "hostname-intersection", Deps: bDeps},
			{Route: c, ParentRef: 0, Resource: lsObj, Outcome: computed.OutcomeAccepted, Listeners: []string{"extra"}, Rule: "hostname-intersection", Deps: []objects.Ref{c, lsObj}},
			{Route: r, ParentRef: 0, Resource: gwObj, Outcome: computed.OutcomeNotAllowed, Rule: "allowed-routes",
				Facts: []computed.Fact{{Listener: "https", Code: computed.FactNamespace, Want: "team=web", Got: "team=retail"}}, Deps: []objects.Ref{r, gwObj, shopNS}},
			{Route: u, ParentRef: 0, Resource: gwObj, Unknown: computed.UnknownNamespaceLabels, Deps: []objects.Ref{u, gwObj, shopNS}},
		},
		Counts: []computed.ListenerCount{
			{Resource: gwObj, Listener: "http", Count: 1, Deps: []objects.Ref{gwObj, a, r, u}},
			{Resource: gwObj, Listener: "https", Count: 2, Deps: []objects.Ref{gwObj, a, b, r, u, shopNS}},
			{Resource: lsObj, Listener: "extra", Count: 1, Deps: []objects.Ref{lsObj, c}},
		},
	}}
}

// buildViews builds viewWorld with v.
func buildViews(t *testing.T, v *computed.Views) *Index {
	t.Helper()
	in := viewWorld().input(t)
	in.Computed = v
	return Build(in)
}

// refsOf maps d back to refs.
func refsOf(ix *Index, d DepSet) []objects.Ref {
	out := []objects.Ref{}
	for _, i := range d {
		out = append(out, ix.Ref(i))
	}
	return out
}

func TestByListenerMembers(t *testing.T) {
	ix := buildViews(t, views())
	a, _ := row(t, ix, "shop", "a")
	b, br := row(t, ix, "shop", "b")
	https := ix.ByListener[ListenerKey{Resource: gwObj, Listener: "https"}]
	if len(https) != 2 || https[0].Row != b || https[0].ParentRef != 0 || https[1].Row != b || https[1].ParentRef != 1 ||
		!slices.Equal(https[0].Hostnames, []string{"b.example.com"}) {
		t.Fatalf("https members %+v", https)
	}
	if !slices.Equal(refsOf(ix, https[0].Deps), []objects.Ref{routeRef("shop", "b"), gwObj, shopNS}) {
		t.Errorf("member deps %v", refsOf(ix, https[0].Deps))
	}
	if http := ix.ByListener[ListenerKey{Resource: gwObj, Listener: "http"}]; len(http) != 1 || http[0].Row != a || http[0].Hostnames[0] != "a.example.com" {
		t.Errorf("http members %+v", http)
	}
	if extra := ix.ByListener[ListenerKey{Resource: lsObj, Listener: "extra"}]; len(extra) != 1 {
		t.Errorf("extra members %+v", extra)
	}
	// One row per route on the page, however many members.
	p := mustRoutes(t, ix, Query{Parent: &gwObj, Listener: "https"}, nil)
	if !slices.Equal(p.IDs, []RowID{b}) || p.Total != 1 {
		t.Errorf("listener=https: %v total %d", p.IDs, p.Total)
	}
	// Per-ref facts carry each listener's hostnames.
	if len(br.Computed) != 2 {
		t.Fatalf("b facts %+v", br.Computed)
	}
	for i, f := range br.Computed {
		if f.ParentRef != i || f.Target != gwObj || f.Outcome != computed.OutcomeAccepted || len(f.Listeners) != 1 ||
			f.Listeners[0].Listener != "https" || f.Listeners[0].Hostnames[0] != "b.example.com" || len(f.Listeners[0].Deps) != 3 {
			t.Errorf("b ref %d: %+v", i, f)
		}
	}
	if ix.RuleSet != "kgateway-2.4" || ix.RuleNote != "a note" {
		t.Errorf("rule set %q note %q", ix.RuleSet, ix.RuleNote)
	}
	lf := ix.Listeners[ListenerKey{Resource: gwObj, Listener: "https"}]
	if !lf.Known || lf.Count != 2 || len(lf.Deps) != 6 {
		t.Errorf("https facts %+v", lf)
	}
	_, rr := row(t, ix, "shop", "r")
	if f := rr.Computed; len(f) != 1 || f[0].Outcome != computed.OutcomeNotAllowed || f[0].Rule != "allowed-routes" || len(f[0].Facts) != 1 || len(f[0].Listeners) != 0 {
		t.Errorf("r facts %+v", f)
	}
}

// A listener whose count is unknown presents no members, even when the rules
// listed some (setting-unknown); the ref keeps its listener.
func TestByListenerHidesMembersUnderUnknownCount(t *testing.T) {
	v := views()
	v.Attachment.Counts[0].Unknown = computed.UnknownSetting
	ix := buildViews(t, v)
	key := ListenerKey{Resource: gwObj, Listener: "http"}
	if m := ix.ByListener[key]; len(m) != 0 {
		t.Errorf("members under an unknown count: %+v", m)
	}
	if f := ix.Listeners[key]; f.Known || f.Unknown != computed.UnknownSetting {
		t.Errorf("facts %+v", f)
	}
	if _, r := row(t, ix, "shop", "a"); len(r.Computed) != 1 || r.Computed[0].Listeners[0].Hostnames[0] != "a.example.com" {
		t.Errorf("a facts %+v", r.Computed)
	}
	if g := ix.Gateways[0]; slices.Contains(g.Members, ids(t, ix, "a")[0]) {
		t.Errorf("members %v", g.Members)
	}
}

func TestComputedFlags(t *testing.T) {
	in := viewWorld().input(t)
	in.Computed = views()
	a := routeRef("shop", "a")
	in.Health.Findings = append(in.Health.Findings, health.Finding{Code: health.CodeComputedDisagrees, Severity: health.SeverityWarning, Resource: &a})
	ix := Build(in)
	for name, want := range map[string]Flag{"a": FlagAccepted | FlagDisagrees, "b": FlagAccepted, "c": FlagAccepted, "u": FlagAccepted | FlagListenerUnknown, "r": FlagNotAccepted} {
		if _, r := row(t, ix, "shop", name); r.Flags != want {
			t.Errorf("%s: flags %v, want %v", name, r.Flags.Names(), want.Names())
		}
	}
	if _, r := row(t, ix, "shop", "u"); !slices.Equal(refsOf(ix, r.UnknownDeps), []objects.Ref{routeRef("shop", "u"), gwObj, shopNS}) {
		t.Errorf("unknown deps %v", refsOf(ix, r.UnknownDeps))
	}
	for _, n := range []string{"disagrees", "listener-unknown"} {
		if f, ok := ParseFlag(n); !ok || f.Names()[0] != n {
			t.Errorf("ParseFlag(%q) = %v %v", n, f, ok)
		}
	}
	p := mustRoutes(t, ix, Query{States: FlagListenerUnknown}, nil)
	if !slices.Equal(p.IDs, ids(t, ix, "u")) {
		t.Errorf("state=listener-unknown: %v", names(ix, p))
	}

	// Without views no computed flag is set.
	in.Computed = nil
	if _, r := row(t, Build(in), "shop", "u"); r.Flags&FlagListenerUnknown != 0 || r.Computed != nil {
		t.Errorf("no views: flags %v facts %+v", r.Flags.Names(), r.Computed)
	}

	// scope-unknown: every status-accepted ref is listener-unknown; a rejected one is not.
	sv := &computed.Views{RuleSet: "kgateway-2.4"}
	for _, o := range views().Attachment.Outcomes {
		sv.Attachment.Outcomes = append(sv.Attachment.Outcomes, computed.RefOutcome{Route: o.Route, ParentRef: o.ParentRef, Resource: o.Resource,
			Unknown: computed.UnknownScope, Deps: o.Deps})
	}
	in.Computed = sv
	ix = Build(in)
	for _, name := range []string{"a", "b", "c", "u"} {
		if _, r := row(t, ix, "shop", name); r.Flags&FlagListenerUnknown == 0 {
			t.Errorf("scope-unknown: %s flags %v", name, r.Flags.Names())
		}
	}
	if _, r := row(t, ix, "shop", "r"); r.Flags&FlagListenerUnknown != 0 {
		t.Errorf("scope-unknown: rejected r flagged")
	}
}

func TestDepSetsInterned(t *testing.T) {
	v := views()
	before := slices.Clone(v.Attachment.Members[1].Deps)
	ix := buildViews(t, v)
	seen := map[objects.Ref]int32{}
	for i, r := range ix.Refs {
		if j, dup := seen[r]; dup {
			t.Errorf("ref %v interned at %d and %d", r, j, i)
		}
		seen[r] = int32(i)
		if ix.Ref(int32(i)) != r {
			t.Errorf("Ref(%d) = %v, want %v", i, ix.Ref(int32(i)), r)
		}
	}
	b := ix.ByListener[ListenerKey{Resource: gwObj, Listener: "https"}]
	if !slices.Equal(b[0].Deps, b[1].Deps) || seen[gwObj] != b[0].Deps[1] {
		t.Errorf("equal refs differ: %v %v", b[0].Deps, b[1].Deps)
	}
	if !slices.Equal(v.Attachment.Members[1].Deps, before) {
		t.Errorf("source deps changed: %v", v.Attachment.Members[1].Deps)
	}
	if !ix.Visible(b[0].Deps, nil) || ix.Visible(b[0].Deps, func(r objects.Ref) bool { return r.Kind != "Namespace" }) {
		t.Error("Visible")
	}
}

func TestGatewayRowComputedCounts(t *testing.T) {
	v := views()
	disagree := func(kind computed.CheckKind, on objects.Ref, listener string, st computed.State) computed.CheckResult {
		return computed.CheckResult{ID: computed.Identity{Kind: kind}, On: on, Listener: listener, State: st}
	}
	v.Checks = []computed.CheckResult{
		disagree(computed.KindListenerCount, gwObj, "http", computed.StateDisagrees),
		disagree(computed.KindListenerCount, gwObj, "https", computed.StateChecked),
		disagree(computed.KindListenerCount, lsObj, "extra", computed.StateDisagrees),
		disagree(computed.KindRouteAccepted, routeRef("shop", "a"), "", computed.StateDisagrees),
	}
	v.Checks[3].Key = computed.ReportKey{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}
	ix := buildViews(t, v)
	g := ix.Gateways[0]
	if !slices.Equal(g.Members, ids(t, ix, "a", "b")) || g.Disagreements != 1 {
		t.Errorf("gateway members %v disagreements %d", g.Members, g.Disagreements)
	}
	if l := ix.ListenerSets[0]; !slices.Equal(l.Members, ids(t, ix, "c")) || l.Disagreements != 1 {
		t.Errorf("listenerset members %v disagreements %d", l.Members, l.Disagreements)
	}
	if n, ok := ix.GatewayMemberCount(0, nil); n != 2 || !ok {
		t.Errorf("member count %d %v", n, ok)
	}
	if _, ok := ix.GatewayMemberCount(0, func(r objects.Ref) bool { return r.Kind != "Namespace" }); ok {
		t.Error("a hidden dependency leaves a count")
	}
	if f := ix.Listeners[ListenerKey{Resource: gwObj, Listener: "http"}]; f.Check == nil || f.Check.State != computed.StateDisagrees {
		t.Errorf("listener check %+v", f.Check)
	}
	if _, r := row(t, ix, "shop", "a"); r.Computed[0].Check == nil || r.Computed[0].Check.State != computed.StateDisagrees {
		t.Errorf("ref check %+v", r.Computed[0].Check)
	}
}

func TestBuildDeterministicWithViews(t *testing.T) {
	w := viewWorld()
	in := w.input(t)
	in.Computed = views()
	a := Build(in)
	for w.seed = 1; w.seed < 5; w.seed++ {
		in := w.input(t)
		in.Computed = views()
		if b := Build(in); !reflect.DeepEqual(a, b) {
			t.Fatalf("seed %d: Builds with views differ", w.seed)
		}
	}
	if len(a.Refs) == 0 || len(a.ByListener) != 3 {
		t.Fatalf("refs %d listeners %d", len(a.Refs), len(a.ByListener))
	}
}
