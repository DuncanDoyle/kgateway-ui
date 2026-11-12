package api

import (
	"cmp"
	"context"
	"maps"
	"net/http"
	goruntime "runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"
	"weak"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/endpoints"
	"github.com/DuncanDoyle/snellius/pkg/kgateway"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
)

const backendsURL = listBase + "/backends"

var backRef = testutil.GwRef("infra", "back")

// backendsWorld is pathsWorld plus the Gateway infra/back with shop/to-back
// on it, sending to the ExternalName Service ext-svc, to named-svc (whose
// port web targets the name http-web; one of its two endpoints is ready) and
// to the Backend be-used. shop/unattached names a listener back lacks, so its
// attachment fails, and sends to lonely-svc. The Backend be-unused is in
// scope and unused. The GatewayExtensions auth and auth2 use Service authz
// for ext-auth.
func backendsWorld() []runtime.Object {
	back := testutil.GatewayWithListeners("infra", "back", "kgateway", []objects.Listener{lst("web", 8080, "")}, up("web", 1))
	toBack := testutil.WithRules(testutil.HTTPRoute("shop", "to-back", []objects.ParentRef{backRef}, testutil.AcceptedBy(backRef, testutil.Controller)),
		backendRule([]any{svcRef("ext-svc")}), backendRule([]any{svcRef("named-svc")}), backendRule([]any{beRef("be-used")}))
	nope := backRef
	nope.SectionName = "nope"
	unattached := testutil.WithRules(testutil.HTTPRoute("shop", "unattached", []objects.ParentRef{nope}, testutil.RouteParent{Ref: nope, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "NoMatchingParent"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}}),
		backendRule([]any{map[string]any{"name": "lonely-svc", "port": int64(8080)}}))
	ext := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "ext-svc", UID: "svc-ext-svc"},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: "example.com", Ports: []corev1.ServicePort{{Name: "http", Port: 80}}}}
	web, port, ready, notReady := "web", int32(8080), true, false
	named := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "named-svc", UID: "svc-named-svc"},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Ports: []corev1.ServicePort{{Name: web, Port: 80, TargetPort: intstr.FromString("http-web")}}}}
	namedES := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "named-svc-x", UID: "es-named-svc",
		Labels: map[string]string{discoveryv1.LabelServiceName: "named-svc"}}, AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{{Name: &web, Port: &port}},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.1.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}},
			{Addresses: []string{"10.0.1.2"}, Conditions: discoveryv1.EndpointConditions{Ready: &notReady}}}}
	auth := func(name string) *unstructured.Unstructured {
		u := testutil.GatewayExtension("shop", name)
		u.Object["spec"] = map[string]any{"type": "ExtAuth", "extAuth": map[string]any{"grpcService": map[string]any{"backendRef": map[string]any{"name": "authz", "port": int64(9001)}}}}
		return u
	}
	return append(pathsWorld(), back, toBack, unattached, ext, named, namedES, testutil.Backend("shop", "be-used", nil), testutil.Backend("shop", "be-unused", nil),
		auth("auth"), auth("auth2"))
}

// backendsSetup is the setup of the backends tests: endpoints on, the
// kgateway pack's consumers, and r as the scoped reader when set.
func backendsSetup(authz Authorizer, r endpoints.ScopedReader) setup {
	s := setup{endpoints: true, authz: authz, opts: []HandlerOption{WithBackendConsumers(kgateway.BackendConsumers)}}
	if r != nil {
		s.opts = append(s.opts, WithScopedReader(r))
	}
	return s
}

// countingReader serves fixed endpoints per Service name and counts its reads.
type countingReader struct {
	reads atomic.Int64
	items map[string][]endpoints.Endpoint
}

func (c *countingReader) Endpoints(_ context.Context, _ string, svc objects.Ref) (endpoints.Result, error) {
	c.reads.Add(1)
	return endpoints.Result{Complete: endpoints.Complete, Items: c.items[svc.Name], ReadAt: testutil.Now}, nil
}

func namedReader() *countingReader {
	return &countingReader{items: map[string][]endpoints.Endpoint{
		"named-svc": {{Address: "10.0.1.1", Port: 8080, PortName: "web", Ready: true, Serving: true, Pod: "named-a"},
			{Address: "10.0.1.2", Port: 8080, PortName: "web", Pod: "named-b"}},
		"cart-svc": {{Address: "10.0.0.1", Port: 80, PortName: "http", Ready: true, Serving: true}},
	}}
}

func detailURL(group, kind, name string) string {
	return backendsURL + "/" + group + "/" + kind + "/shop/" + name
}

// backendsCases are the backends goldens: the list, a Service detail with a
// named targetPort, a Backend detail and a detail with an attributed ref.
func backendsCases(t *testing.T) []goldenCase {
	_, srv, _ := startWith(t, backendsSetup(nil, nil), backendsWorld()...)
	_, ex, _ := startWith(t, explainSetup(nil), explainWorld()...)
	return []goldenCase{
		{"backends", srv, backendsURL + "?limit=200"},
		{"backend-detail-service", srv, detailURL("-", "Service", "named-svc")},
		{"backend-detail-backend", srv, detailURL("gateway.kgateway.dev", "Backend", "be-used")},
		{"backend-detail-explained", ex, backendsURL + "/-/Service/store/cart"},
	}
}

// TestGoldenBackends pins the 6.3 backends bodies (design §8, §9).
func TestGoldenBackends(t *testing.T) { runGolden(t, backendsCases(t)) }

func rowsByName(resp BackendsResponse) map[string]BackendRowDTO {
	out := map[string]BackendRowDTO{}
	for _, r := range resp.Rows {
		out[r.Ref.Name] = r
	}
	return out
}

// §8.1: the list holds every in-scope Backend, used or not, every ref an
// owned route writes, attached or not, and every consumer's backend; a
// Service nothing references is not listed.
func TestBackendsListed(t *testing.T) {
	_, srv, _ := startWith(t, backendsSetup(nil, nil), append(backendsWorld(), testutil.Service("shop", "unreferenced", nil))...)
	all := get[BackendsResponse](t, srv, backendsURL+"?limit=200", 200)
	rows := rowsByName(all)
	for _, name := range []string{"cart-svc", "other-svc", "mirror-svc", "ext-svc", "named-svc", "lonely-svc", "authz", "be-used", "be-unused"} {
		if _, ok := rows[name]; !ok {
			t.Errorf("%s not listed: %v", name, slices.Sorted(maps.Keys(rows)))
		}
	}
	if _, ok := rows["unreferenced"]; ok {
		t.Error("an unreferenced Service is listed")
	}
	if all.Hidden {
		t.Error("hidden set without a restricting Authorizer")
	}
	unused := get[BackendsResponse](t, srv, backendsURL+"?used=no", 200)
	if len(unused.Rows) != 1 || unused.Rows[0].Ref.Name != "be-unused" || unused.Rows[0].Kind != "Backend · Static" || unused.Rows[0].Routes != 0 {
		t.Fatalf("used=no rows %+v", unused.Rows)
	}
	if r := rows["be-used"]; r.Kind != "Backend · Static" || len(r.Endpoints) != 1 || r.Endpoints[0].Complete != paths.EndpointsNotApplicable || r.Routes != 1 {
		t.Fatalf("be-used %+v", r)
	}
	// Problems first: no row is in a better state than one after it.
	rank := map[string]int{"error": 0, "warning": 1, "unknown": 2, "healthy": 3}
	for k := 1; k < len(all.Rows); k++ {
		if rank[string(all.Rows[k-1].Problem.State)] > rank[string(all.Rows[k].Problem.State)] {
			t.Fatalf("not problems first: %s before %s", all.Rows[k-1].Problem.State, all.Rows[k].Problem.State)
		}
	}
	// Paging with limit 2 lists every row once, in order.
	var paged []string
	for cur := ""; ; {
		p := get[BackendsResponse](t, srv, backendsURL+"?limit=2&cursor="+cur, 200)
		for _, r := range p.Rows {
			paged = append(paged, r.Ref.Kind+"/"+r.Ref.Name)
		}
		if cur = p.Next; cur == "" {
			break
		}
	}
	var want []string
	for _, r := range all.Rows {
		want = append(want, r.Ref.Kind+"/"+r.Ref.Name)
	}
	if !slices.Equal(paged, want) {
		t.Fatalf("paged %v, want %v", paged, want)
	}
	// Filters and sorts at the API.
	names := func(q string) []string {
		var out []string
		for _, r := range get[BackendsResponse](t, srv, backendsURL+"?limit=200&"+q, 200).Rows {
			out = append(out, r.Ref.Name)
		}
		return out
	}
	if got := names("gateway=infra/back&sort=name"); !slices.Equal(got, []string{"be-used", "ext-svc", "named-svc"}) {
		t.Errorf("gateway=infra/back: %v", got)
	}
	if got := names("kind=Backend&sort=name"); !slices.Equal(got, []string{"be-unused", "be-used", "be1"}) {
		t.Errorf("kind=Backend: %v", got)
	}
	if got := names("q=NAMED"); !slices.Equal(got, []string{"named-svc"}) {
		t.Errorf("q=NAMED: %v", got)
	}
	if got := names("state=error"); !slices.Equal(got, []string{"lonely-svc"}) {
		t.Errorf("state=error: %v", got)
	}
	if got := names("sort=used.desc"); len(got) == 0 || got[0] != "cart-svc" || got[len(got)-1] != "be-unused" {
		t.Errorf("sort=used.desc: %v", got)
	}
	for _, q := range []string{"bogus=1", "sort=size", "used=maybe", "state=bad", "limit=0", "gateway=back", "cursor=x", "kind=", "q=a&q=b"} {
		get[ErrorResponse](t, srv, backendsURL+"?"+q, http.StatusBadRequest)
	}
}

// A Service ref whose attachment failed is listed with 0 confirmed exposures
// and the attachment's failure as its problem (design §8.1).
func TestBackendsAttachmentFailed(t *testing.T) {
	_, srv, _ := startWith(t, backendsSetup(nil, nil), backendsWorld()...)
	r := rowsByName(get[BackendsResponse](t, srv, backendsURL+"?limit=200", 200))["lonely-svc"]
	if r.Confirmed != 0 || r.Routes != 1 || r.Gateways != 0 || r.Problem.State != "error" {
		t.Fatalf("lonely-svc %+v", r)
	}
	if !slices.ContainsFunc(r.Problem.Signals, func(s SignalDTO) bool {
		return s.Kind == paths.SignalRouteStatus && s.Code == "Accepted" && s.Ref != nil && s.Ref.Name == "unattached"
	}) {
		t.Fatalf("no attachment signal: %+v", r.Problem.Signals)
	}
	if c := rowsByName(get[BackendsResponse](t, srv, backendsURL+"?limit=200", 200))["named-svc"]; c.Confirmed != 1 || c.Gateways != 1 {
		t.Fatalf("named-svc %+v", c)
	}
}

// A GatewayExtension's backend is listed with its consumers, and detail
// names each with its role, field and port (design §8.3).
func TestBackendsConsumers(t *testing.T) {
	_, srv, _ := startWith(t, backendsSetup(nil, nil), backendsWorld()...)
	if r := rowsByName(get[BackendsResponse](t, srv, backendsURL+"?limit=200", 200))["authz"]; r.Consumers != 2 || r.Routes != 0 {
		t.Fatalf("authz row %+v", r)
	}
	d := get[BackendDetail](t, srv, detailURL("-", "Service", "authz"), 200)
	if len(d.Consumers) != 2 {
		t.Fatalf("consumers %+v", d.Consumers)
	}
	c := d.Consumers[0]
	if c.Consumer.Kind != "GatewayExtension" || c.Consumer.Name != "auth" || c.Role != "ext-auth" || c.Field != "spec.extAuth.grpcService.backendRef" || c.Port != 9001 {
		t.Fatalf("consumer %+v", c)
	}
}

// hideKinds hides refs of kind with the given names.
func hideKinds(kind string, names ...string) AuthorizerFunc {
	return func(_ *http.Request, ref objects.Ref) bool {
		return ref.Kind != kind || !slices.Contains(names, ref.Name)
	}
}

// §13: a hidden backend is a 404 and nothing is read for it, memo hits
// included; a visible one is read.
func TestBackendDetailHiddenReadsNothing(t *testing.T) {
	var hide atomic.Bool
	inner := namedReader()
	memo := endpoints.Bounded(inner, endpoints.DefaultBounds, testutil.FakeClock())
	outer := &countingReader{}
	reader := readerFunc(func(ctx context.Context, id string, svc objects.Ref) (endpoints.Result, error) {
		outer.reads.Add(1)
		return memo.Endpoints(ctx, id, svc)
	})
	authz := AuthorizerFunc(func(r *http.Request, ref objects.Ref) bool {
		return !hide.Load() || ref.Kind != "Service" || ref.Name != "cart-svc"
	})
	_, srv, _ := startWith(t, backendsSetup(authz, reader), backendsWorld()...)
	url := detailURL("-", "Service", "cart-svc")
	if d := get[BackendDetail](t, srv, url, 200); len(d.Sources) != 1 || d.Sources[0].Complete != paths.EndpointsComplete || len(d.Sources[0].Items) != 1 {
		t.Fatalf("visible cart-svc sources %+v", d.Sources)
	}
	if outer.reads.Load() != 1 {
		t.Fatalf("%d reads for one visible detail", outer.reads.Load())
	}
	hide.Store(true)
	get[ErrorResponse](t, srv, url, http.StatusNotFound)
	if outer.reads.Load() != 1 || inner.reads.Load() != 1 {
		t.Fatalf("a hidden backend was read: %d calls, %d provider reads", outer.reads.Load(), inner.reads.Load())
	}
	list := get[BackendsResponse](t, srv, backendsURL+"?limit=200", 200)
	if _, ok := rowsByName(list)["cart-svc"]; ok || !list.Hidden {
		t.Fatalf("hidden cart-svc listed, or hidden unset: %v", list.Hidden)
	}
	get[ErrorResponse](t, srv, detailURL("-", "Service", "unreferenced"), http.StatusNotFound)
}

type readerFunc func(context.Context, string, objects.Ref) (endpoints.Result, error)

func (f readerFunc) Endpoints(ctx context.Context, id string, svc objects.Ref) (endpoints.Result, error) {
	return f(ctx, id, svc)
}

// §13: exposure leaves out the paths through a hidden Gateway and says so.
func TestBackendDetailHiddenExposure(t *testing.T) {
	_, open, _ := startWith(t, backendsSetup(nil, nil), backendsWorld()...)
	_, srv, _ := startWith(t, backendsSetup(AuthorizerFunc(hideEdge), nil), backendsWorld()...)
	url := detailURL("-", "Service", "cart-svc")
	all := get[BackendDetail](t, open, url, 200)
	got := get[BackendDetail](t, srv, url, 200)
	if !slices.ContainsFunc(all.Exposure.Rows, func(r PathRowDTO) bool { return r.Gateway.Name == "edge" }) {
		t.Fatalf("cart-svc has no path through edge: %v", pathKeys(all.Exposure))
	}
	if len(got.Exposure.Rows) == 0 || slices.ContainsFunc(got.Exposure.Rows, func(r PathRowDTO) bool { return r.Gateway.Name == "edge" }) || !got.Exposure.Summary.Hidden {
		t.Fatalf("restricted exposure %v, hidden %v", pathKeys(got.Exposure), got.Exposure.Summary.Hidden)
	}
	// The row counts only the Gateways of visible paths.
	openRow := rowsByName(get[BackendsResponse](t, open, backendsURL+"?limit=200", 200))["cart-svc"]
	row := rowsByName(get[BackendsResponse](t, srv, backendsURL+"?limit=200", 200))["cart-svc"]
	if openRow.Gateways != 2 || row.Gateways != 1 {
		t.Fatalf("cart-svc Gateways: %d open, %d restricted", openRow.Gateways, row.Gateways)
	}
}

// §13: a hidden GatewayExtension is absent from consumers and counted nowhere.
func TestBackendsHiddenConsumer(t *testing.T) {
	_, srv, _ := startWith(t, backendsSetup(hideKinds("GatewayExtension", "auth"), nil), backendsWorld()...)
	if r := rowsByName(get[BackendsResponse](t, srv, backendsURL+"?limit=200", 200))["authz"]; r.Consumers != 1 {
		t.Fatalf("authz consumers %d, want 1", r.Consumers)
	}
	d := get[BackendDetail](t, srv, detailURL("-", "Service", "authz"), 200)
	if len(d.Consumers) != 1 || d.Consumers[0].Consumer.Name != "auth2" {
		t.Fatalf("consumers %+v", d.Consumers)
	}
}

// §9: without a scoped reader the source is unavailable and the cached
// counts show; an ExternalName Service is not applicable and is not read.
func TestBackendDetailSources(t *testing.T) {
	_, srv, _ := startWith(t, backendsSetup(nil, nil), backendsWorld()...)
	d := get[BackendDetail](t, srv, detailURL("-", "Service", "cart-svc"), 200)
	if len(d.Sources) != 1 {
		t.Fatalf("sources %+v", d.Sources)
	}
	s := d.Sources[0]
	if s.Source != EndpointsSourceKubernetes || s.Complete != endpoints.Unavailable || s.Ready == nil || *s.Ready != 1 || s.Total == nil || *s.Total != 1 || len(s.Items) != 0 {
		t.Fatalf("cart-svc source without a reader %+v", s)
	}
	reader := namedReader()
	_, srv2, _ := startWith(t, backendsSetup(nil, reader), backendsWorld()...)
	ext := get[BackendDetail](t, srv2, detailURL("-", "Service", "ext-svc"), 200)
	if len(ext.Sources) != 1 || ext.Sources[0].Complete != paths.EndpointsNotApplicable || ext.Sources[0].Ready != nil || reader.reads.Load() != 0 {
		t.Fatalf("ExternalName sources %+v, %d reads", ext.Sources, reader.reads.Load())
	}
	if ext.Service == nil || ext.Service.Type != "ExternalName" {
		t.Fatalf("ExternalName facts %+v", ext.Service)
	}
}

// §9.4: a named targetPort shows its name, unresolved, and the slice ports
// observed for the port's name; the complete read splits its endpoints.
func TestBackendDetailPortChain(t *testing.T) {
	_, srv, _ := startWith(t, backendsSetup(nil, namedReader()), backendsWorld()...)
	d := get[BackendDetail](t, srv, detailURL("-", "Service", "named-svc"), 200)
	if len(d.PortChain) != 1 {
		t.Fatalf("port chain %+v", d.PortChain)
	}
	c := d.PortChain[0]
	if c.RefPort != 80 || c.ServicePort == nil || *c.ServicePort != 80 || c.ServicePortName != "web" || c.TargetName != "http-web" || c.TargetPort != "" ||
		!slices.Equal(c.SlicePorts, []int32{8080}) || c.NoSuchPort || c.NoSlicePort {
		t.Fatalf("named-svc chain %+v", c)
	}
	s := d.Sources[0]
	if s.Complete != endpoints.Complete || len(s.Items) != 2 || *s.Ready != 1 || *s.Total != 2 || s.Port == nil || s.Port.Name != "web" {
		t.Fatalf("named-svc source %+v", s)
	}
	if d.Service == nil || len(d.Service.Ports) != 1 || d.Explanations == nil || len(d.Explanations) != 0 {
		t.Fatalf("facts %+v, explanations %+v", d.Service, d.Explanations)
	}
}

// The handler's memos keep no old publication alive: once the engine
// publishes a new model, the old one is collected even if no request comes.
func TestMemosReleaseOldModel(t *testing.T) {
	h := newHarness(t, backendsSetup(nil, namedReader()), backendsWorld()...)
	get[PathsResponse](t, h.srv, pathsURL, 200)
	get[BackendsResponse](t, h.srv, backendsURL, 200)
	get[BackendDetail](t, h.srv, detailURL("-", "Service", "named-svc"), 200)
	old := weak.Make(h.e.Current().Model)
	h.clk.set(testutil.Now)
	if h.e.Compute() == old.Value() {
		t.Fatal("no new publication")
	}
	goruntime.GC()
	goruntime.GC()
	if old.Value() != nil {
		t.Fatal("the previous model is still reachable after a new publication")
	}
}

// §9: a read the Service's cached fact cannot split shows that port as
// unavailable, never as an empty complete read; a partial read shows its
// items with the cached counts at the cache's read time.
func TestBackendDetailUnsplitAndPartial(t *testing.T) {
	r := namedReader()
	r.items["other-svc"] = []endpoints.Endpoint{{Address: "10.0.2.1", Port: 80, PortName: "http", Ready: true}}
	_, srv, _ := startWith(t, backendsSetup(nil, r), backendsWorld()...)
	for _, s := range get[BackendDetail](t, srv, detailURL("-", "Service", "other-svc"), 200).Sources {
		if s.Complete != endpoints.Unavailable || len(s.Items) != 0 || s.Ready != nil {
			t.Fatalf("unsplit read %+v", s)
		}
	}
	partial := readerFunc(func(_ context.Context, _ string, svc objects.Ref) (endpoints.Result, error) {
		return endpoints.Result{Complete: endpoints.Partial, Items: r.items[svc.Name], ReadAt: testutil.Now.Add(time.Hour)}, nil
	})
	_, srv2, _ := startWith(t, backendsSetup(nil, partial), backendsWorld()...)
	s := get[BackendDetail](t, srv2, detailURL("-", "Service", "cart-svc"), 200).Sources[0]
	if s.Complete != endpoints.Partial || len(s.Items) != 1 || s.Ready == nil || *s.Ready != 1 || !s.ReadAt.Equal(testutil.Now) {
		t.Fatalf("partial read %+v (readAt %v)", s, s.ReadAt)
	}
}

// §13, D-077: a backend only hidden routes write is not listed, and an
// in-scope Backend only a hidden route uses has no visible consumers: it is
// listed under used=no and counted there by the facets, as an unused one is.
func TestBackendsHiddenRoutesOnly(t *testing.T) {
	_, srv, _ := startWith(t, backendsSetup(hideKinds("HTTPRoute", "unattached", "to-back"), nil), backendsWorld()...)
	all := get[BackendsResponse](t, srv, backendsURL+"?limit=200", 200)
	rows := rowsByName(all)
	if _, ok := rows["lonely-svc"]; ok || !all.Hidden {
		t.Fatalf("lonely-svc listed through a hidden route, or hidden unset: %v", all.Hidden)
	}
	if r, ok := rows["be-used"]; !ok || r.Routes != 0 || r.Consumers != 0 {
		t.Fatalf("the in-scope Backend be-used %+v, want listed with no visible use", r)
	}
	unused := get[BackendsResponse](t, srv, backendsURL+"?used=no&limit=200", 200)
	shop := 0
	for _, r := range unused.Rows {
		if r.Ref.Namespace == "shop" {
			shop++
		}
	}
	if _, ok := rowsByName(unused)["be-used"]; !ok {
		t.Fatal("be-used, with no visible consumer, is not listed under used=no")
	}
	if i := slices.IndexFunc(unused.Facets.Namespaces, func(n BackendNamespaceFacetDTO) bool { return n.Name == "shop" }); i < 0 || unused.Facets.Namespaces[i].Backends != shop {
		t.Fatalf("used=no namespace facets %+v, want shop = %d", unused.Facets.Namespaces, shop)
	}
}

// D-077: a rejected Backend only a hidden route uses looks exactly like an
// unused rejected one, in the list (state, signals, state filter) and on its
// detail, with current evidence and with observedGeneration 0.
func TestBackendsHiddenUseLooksUnused(t *testing.T) {
	for _, gen := range []int64{1, 0} {
		cond := testutil.Cond("Accepted", metav1.ConditionFalse, "Invalid")
		cond.ObservedGeneration = gen
		world := slices.DeleteFunc(backendsWorld(), func(o runtime.Object) bool {
			u, ok := o.(*unstructured.Unstructured)
			return ok && u.GetKind() == "Backend" && u.GetName() == "be-used"
		})
		world = append(world, testutil.Backend("shop", "be-used", []metav1.Condition{cond}), testutil.Backend("shop", "be-bad", []metav1.Condition{cond}))
		_, srv, _ := startWith(t, backendsSetup(hideKinds("HTTPRoute", "unattached", "to-back"), nil), world...)
		// signals is a problem's signals without their object.
		signals := func(p ProblemDTO) []string {
			var out []string
			for _, s := range p.Signals {
				out = append(out, s.Kind+"/"+s.Code+"/"+string(s.State)+"/"+s.Title)
			}
			return out
		}
		rows := rowsByName(get[BackendsResponse](t, srv, backendsURL+"?limit=200", 200))
		used, bad := rows["be-used"], rows["be-bad"]
		if used.Problem.State != bad.Problem.State || !slices.Equal(signals(used.Problem), signals(bad.Problem)) {
			t.Errorf("gen %d: be-used %+v differs from be-bad %+v", gen, used.Problem, bad.Problem)
		}
		filtered := rowsByName(get[BackendsResponse](t, srv, backendsURL+"?limit=200&state="+string(bad.Problem.State), 200))
		if _, ok := filtered["be-used"]; !ok {
			t.Errorf("gen %d: state=%s lists be-bad but not be-used", gen, bad.Problem.State)
		}
		du := get[BackendDetail](t, srv, detailURL("gateway.kgateway.dev", "Backend", "be-used"), 200)
		db := get[BackendDetail](t, srv, detailURL("gateway.kgateway.dev", "Backend", "be-bad"), 200)
		if du.Problem.State != db.Problem.State || !slices.Equal(signals(du.Problem), signals(db.Problem)) {
			t.Errorf("gen %d: detail be-used %+v differs from be-bad %+v", gen, du.Problem, db.Problem)
		}
	}
}

// §4.5, D-006: an in-scope Backend nothing uses, with a current
// Accepted=False, is an error in the list and on its detail.
func TestBackendsUnusedRejected(t *testing.T) {
	bad := testutil.Backend("shop", "be-bad", []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "Invalid")})
	_, srv, _ := startWith(t, backendsSetup(nil, nil), append(backendsWorld(), bad)...)
	if r, ok := rowsByName(get[BackendsResponse](t, srv, backendsURL+"?limit=200", 200))["be-bad"]; !ok || r.Routes != 0 || r.Problem.State != "error" {
		t.Fatalf("be-bad row %+v", r)
	}
	d := get[BackendDetail](t, srv, detailURL("gateway.kgateway.dev", "Backend", "be-bad"), 200)
	if d.Problem.State != "error" || !slices.ContainsFunc(d.Status, func(s SignalDTO) bool { return s.Code == "Accepted" && s.State == "error" }) {
		t.Fatalf("be-bad detail problem %+v status %+v", d.Problem, d.Status)
	}
}

// §9.3, D-006: a complete scoped read with no ready endpoint, while the cache
// counts one ready, makes the detail unknown at best and keeps the cached
// count beside the read's; no finding is added.
func TestBackendDetailReadContradictsCache(t *testing.T) {
	r := &countingReader{items: map[string][]endpoints.Endpoint{
		"named-svc": {{Address: "10.0.1.1", Port: 8080, PortName: "web", Pod: "named-a"}, {Address: "10.0.1.2", Port: 8080, PortName: "web", Pod: "named-b"}},
		"cart-svc":  namedReader().items["cart-svc"]}}
	_, srv, _ := startWith(t, backendsSetup(nil, r), backendsWorld()...)
	d := get[BackendDetail](t, srv, detailURL("-", "Service", "named-svc"), 200)
	s := d.Sources[0]
	if s.Complete != endpoints.Complete || s.Ready == nil || *s.Ready != 0 || s.CachedReady == nil || *s.CachedReady != 1 {
		t.Fatalf("source %+v (cachedReady %v)", s, s.CachedReady)
	}
	waiting := func(sg SignalDTO) bool { return sg.Kind == paths.SignalWaiting && sg.Code == "KGWUI-023" && sg.State == "unknown" }
	if d.Problem.State == "healthy" || !slices.ContainsFunc(d.Problem.Signals, waiting) || !slices.ContainsFunc(d.Status, waiting) {
		t.Fatalf("problem %+v status %+v", d.Problem, d.Status)
	}
	// The cache and a read that agree add nothing.
	agree := get[BackendDetail](t, srv, detailURL("-", "Service", "cart-svc"), 200)
	if agree.Sources[0].CachedReady != nil || slices.ContainsFunc(agree.Status, waiting) {
		t.Fatalf("agreeing read %+v status %+v", agree.Sources[0], agree.Status)
	}
}

// §8.1, §13: the list's facets are the Gateways and namespaces of the visible
// backends; each applies every filter but its own, so a selected value keeps
// the others selectable.
func TestBackendsFacets(t *testing.T) {
	_, open, _ := startWith(t, backendsSetup(nil, nil), backendsWorld()...)
	_, srv, _ := startWith(t, backendsSetup(AuthorizerFunc(hideEdge), nil), backendsWorld()...)
	gateways := func(r BackendsResponse) map[string]int {
		out := map[string]int{}
		for _, g := range r.Facets.Gateways {
			out[g.Ref.Namespace+"/"+g.Ref.Name] = g.Backends
		}
		return out
	}
	namespaces := func(r BackendsResponse) map[string]int {
		out := map[string]int{}
		for _, n := range r.Facets.Namespaces {
			out[n.Name] = n.Backends
		}
		return out
	}
	all := get[BackendsResponse](t, open, backendsURL, 200)
	if g := gateways(all); g["infra/back"] != 3 || g["infra/edge"] == 0 || g["infra/other"] == 0 {
		t.Fatalf("gateway facets %v", g)
	}
	// A selected Gateway narrows the rows and the namespace counts, not its own facet.
	back := get[BackendsResponse](t, open, backendsURL+"?gateway=infra/back", 200)
	if len(back.Rows) != 3 || !maps.Equal(gateways(back), gateways(all)) || namespaces(back)["shop"] != 3 {
		t.Fatalf("gateway=infra/back: %d rows, gateways %v, namespaces %v", len(back.Rows), gateways(back), namespaces(back))
	}
	// A namespace with no backend empties the rows and keeps every namespace selectable.
	none := get[BackendsResponse](t, open, backendsURL+"?namespace=nope", 200)
	if len(none.Rows) != 0 || !maps.Equal(namespaces(none), namespaces(all)) || len(none.Facets.Gateways) != 0 {
		t.Fatalf("namespace=nope: %d rows, namespaces %v, gateways %v", len(none.Rows), namespaces(none), gateways(none))
	}
	// A Gateway the viewer may not see is not offered.
	if g := gateways(get[BackendsResponse](t, srv, backendsURL, 200)); g["infra/edge"] != 0 || g["infra/back"] != 3 {
		t.Fatalf("restricted gateway facets %v", g)
	}
	if !slices.IsSortedFunc(all.Facets.Gateways, func(a, b BackendGatewayFacetDTO) int {
		return cmp.Or(cmp.Compare(a.Ref.Namespace, b.Ref.Namespace), cmp.Compare(a.Ref.Name, b.Ref.Name))
	}) {
		t.Fatalf("gateway facets not sorted: %v", gateways(all))
	}
}
