package scale

import (
	"context"
	"strconv"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
)

// LiveWorld is a settled cache over a tier's world on fake clients, its
// change signal drained.
type LiveWorld struct {
	Cache  *cache.Cache
	Signal *trigger.Signal
	// Routes are the seeded HTTPRoutes; RouteGVR is their resource.
	Routes   []*unstructured.Unstructured
	RouteGVR schema.GroupVersionResource
	// Update writes u to the fake dynamic client; GetRoute reads a copy of an HTTPRoute from it.
	Update   func(ctx context.Context, u *unstructured.Unstructured) error
	GetRoute func(ctx context.Context, namespace, name string) (*unstructured.Unstructured, error)
	// CRDs and Metrics are the tier's Gateway API CRD metadata and controller
	// metrics capture; LivePhase3 sets them.
	CRDs    []*metav1.PartialObjectMetadata
	Metrics []byte
	// Kube is the fake typed client; Stop stops the cache before tb ends.
	Kube *testutil.FakeKube
	Stop func()
	// Slices are the seeded EndpointSlices; LivePhase3 sets them.
	Slices []types.NamespacedName
}

// Live seeds fake clients with tier's world, one kgateway controller and a
// proxy per Gateway (as Snapshot), starts a cache stamped by now until tb
// ends and waits until it has settled.
func Live(tb testing.TB, tier Tier, now func() time.Time) *LiveWorld {
	tb.Helper()
	return live(tb, tier, now, false)
}

// LivePhase3 is Live with the tier's proxy Deployments, proxy Pods, Warning Events,
// Services and EndpointSlices seeded on the fake typed client, its CRDs on a fake
// metadata client, the routes' Service backendRefs, ListOnlyObjects and its Metrics
// set. The proxy Pod, Event, CRD, Service and EndpointSlice watches run.
func LivePhase3(tb testing.TB, tier Tier, now func() time.Time) *LiveWorld {
	tb.Helper()
	return live(tb, tier, now, true)
}

func live(tb testing.TB, tier Tier, now func() time.Time, phase3 bool) *LiveWorld {
	tb.Helper()
	served := testutil.ServedUpstream()
	w := &LiveWorld{Signal: trigger.New()}
	for _, s := range served {
		if s.Group == "gateway.networking.k8s.io" && s.Kind == "HTTPRoute" {
			w.RouteGVR = s.GVR()
		}
	}
	objs := []runtime.Object{testutil.Namespace("kgw", nil)}
	seeded := World(tier, 1)
	if phase3 {
		seeded = append(seeded, ListOnlyObjects(tier)...)
	}
	for i, o := range seeded {
		o.SetResourceVersion("1")
		o.SetUID(types.UID(strconv.Itoa(i)))
		if o.GetKind() == "HTTPRoute" {
			w.Routes = append(w.Routes, o)
		}
		objs = append(objs, o)
	}
	for _, ns := range testutil.ScaleNamespaces(tier.Namespaces) {
		objs = append(objs, ns)
	}
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	objs = append(objs, d, testutil.ControllerPod(d, "kgateway-0", true))
	if !phase3 {
		for i := range tier.Gateways {
			objs = append(objs, testutil.ProxyDeployment(testutil.ScaleNamespace(i%tier.Namespaces), gwName(i), 1, 1))
		}
	}
	if phase3 {
		for _, d := range testutil.ScaleProxyDeployments(tier.Shape()) {
			objs = append(objs, d)
		}
		for _, gw := range testutil.ScaleLongGatewayObjects(tier.Shape()) {
			gw.SetResourceVersion("1")
			objs = append(objs, gw)
		}
		for _, p := range testutil.ScaleProxyPods(tier.Shape(), 1) {
			objs = append(objs, p)
		}
		for _, e := range testutil.ScaleEvents(tier.Shape(), 1) {
			objs = append(objs, e)
		}
		referenceServices(tier, w.Routes)
		for _, s := range testutil.ScaleServices(tier.Shape(), 1) {
			objs = append(objs, s)
		}
		for _, es := range testutil.ScaleEndpointSlices(tier.Shape(), 1) {
			w.Slices = append(w.Slices, types.NamespacedName{Namespace: es.Namespace, Name: es.Name})
			objs = append(objs, es)
		}
		w.CRDs, w.Metrics = testutil.ScaleCRDs(tier.Shape()), testutil.ScaleMetrics(tier.Shape(), 1)
	}
	dyn, kube := testutil.ScaleClients(tb, objs...)
	w.Kube = kube
	w.Update = func(ctx context.Context, u *unstructured.Unstructured) error {
		_, err := dyn.Resource(w.RouteGVR).Namespace(u.GetNamespace()).Update(ctx, u, metav1.UpdateOptions{})
		// The fakes keep a copy of every action forever; long streams would measure that log, not the UI.
		dyn.ClearActions()
		kube.ClearActions()
		return err
	}
	w.GetRoute = func(ctx context.Context, namespace, name string) (*unstructured.Unstructured, error) {
		u, err := dyn.Resource(w.RouteGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		dyn.ClearActions()
		return u, err
	}
	det := install.UpstreamDetector{}
	cfg := cache.Config{Dynamic: dyn, Kube: kube, Served: served, Now: now, Changes: w.Signal,
		PodSelectors: []labels.Selector{det.ControllerPodSelector()}, DeploymentSelectors: []labels.Selector{det.ControllerSelector(), det.ProxySelector()}}
	if phase3 {
		cfg.ProxyPodSelectors, cfg.Events, cfg.CRDs, cfg.Endpoints = []labels.Selector{det.ProxyPodSelector()}, true, true, true
		cfg.Metadata = testutil.ScaleMetadata(tb, w.CRDs...)
	}
	w.Cache = cache.New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)
	w.Stop = cancel
	w.Cache.Start(ctx)
	for deadline := time.Now().Add(time.Minute); !w.Cache.Settled(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			tb.Fatal("cache did not settle")
		}
	}
	if phase3 {
		// The optional watches settle on their own; each must be running.
		for deadline := time.Now().Add(time.Minute); !optionalSynced(w.Cache.Snapshot()); time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				tb.Fatal("the proxy Pod, Event, CRD, Service and EndpointSlice watches did not settle")
			}
		}
	}
	// Settled leaves out the list-only kinds; each must be running too.
	for deadline := time.Now().Add(time.Minute); !listOnlySynced(w.Cache.Snapshot(), served); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			tb.Fatal("the list-only kinds did not settle")
		}
	}
	testutil.Drain(w.Signal.C()) // the settle notify precedes Settled
	return w
}

// optionalSynced reports whether the proxy Pod, Event, CRD, Service and EndpointSlice watches all run.
func optionalSynced(s *cache.Snapshot) bool {
	for _, g := range []schema.GroupVersionResource{cache.ProxyPodsGVR, cache.EventsGVR, cache.CRDsGVR, cache.ServicesGVR, cache.EndpointSlicesGVR} {
		if !s.Access[g].OK {
			return false
		}
	}
	return true
}

// listOnlySynced reports whether every list-only kind of served is OK.
func listOnlySynced(s *cache.Snapshot, served []kinds.Served) bool {
	for _, k := range served {
		if k.ListOnly && !s.Access[k.GVR()].OK {
			return false
		}
	}
	return true
}

// referenceServices adds Service backendRefs, in each route's own namespace,
// until every referenced Service is named by a leaf HTTPRoute. A route naming
// MissingBackend gets none, so its KGWUI-013 keeps one candidate and its group.
func referenceServices(tier Tier, routes []*unstructured.Unstructured) {
	var leaves = map[string][]*unstructured.Unstructured{}
	for _, r := range routes {
		if rules, _, _ := unstructured.NestedSlice(r.Object, "spec", "rules"); len(rules) == 1 {
			if refs, _ := rules[0].(map[string]any)["backendRefs"].([]any); len(refs) == 1 && refs[0].(map[string]any)["kind"] == "Backend" &&
				refs[0].(map[string]any)["name"] != MissingBackend {
				leaves[r.GetNamespace()] = append(leaves[r.GetNamespace()], r)
			}
		}
	}
	next := map[string]int{}
	for i := range tier.ReferencedServices {
		ns := testutil.ScaleNamespace(i % tier.Namespaces)
		if len(leaves[ns]) == 0 {
			panic("scale: no leaf HTTPRoute in " + ns)
		}
		r := leaves[ns][next[ns]%len(leaves[ns])]
		next[ns]++
		rules, _, _ := unstructured.NestedSlice(r.Object, "spec", "rules")
		rule := rules[0].(map[string]any)
		rule["backendRefs"] = append(rule["backendRefs"].([]any), map[string]any{"group": "", "kind": "Service", "name": testutil.ScaleServiceName(tier.Shape(), i), "namespace": ns, "port": int64(80)})
		_ = unstructured.SetNestedSlice(r.Object, rules, "spec", "rules")
	}
}

// ExtensionEvery puts a GatewayExtension in every tenth namespace.
const ExtensionEvery = 10

// ListOnlyObjects are tier's list-only kinds (#17): a ReferenceGrant and a
// DirectResponse per namespace and a GatewayExtension every ExtensionEvery. No check reads them.
func ListOnlyObjects(tier Tier) []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for i := range tier.Namespaces {
		ns := testutil.ScaleNamespace(i)
		from := testutil.ScaleNamespace((i + tier.Namespaces - 1) % tier.Namespaces)
		out = append(out, testutil.ReferenceGrant(ns, "rg-"+from, from), testutil.DirectResponse(ns, "dr-503"))
		if i%ExtensionEvery == 0 {
			out = append(out, testutil.GatewayExtension(ns, "ext-auth"))
		}
	}
	return out
}
