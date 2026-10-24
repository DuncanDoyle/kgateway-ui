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
	// Update writes u to the fake dynamic client.
	Update func(ctx context.Context, u *unstructured.Unstructured) error
}

// Live seeds fake clients with tier's world, one kgateway controller and a
// proxy per Gateway (as Snapshot), starts a cache stamped by now until tb
// ends and waits until it has settled.
func Live(tb testing.TB, tier Tier, now func() time.Time) *LiveWorld {
	tb.Helper()
	served := testutil.ServedUpstream()
	w := &LiveWorld{Signal: trigger.New()}
	for _, s := range served {
		if s.Group == "gateway.networking.k8s.io" && s.Kind == "HTTPRoute" {
			w.RouteGVR = s.GVR()
		}
	}
	objs := []runtime.Object{testutil.Namespace("kgw", nil)}
	for i, o := range World(tier, 1) {
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
	for i := range tier.Gateways {
		objs = append(objs, testutil.ProxyDeployment(testutil.ScaleNamespace(i%tier.Namespaces), gwName(i), 1, 1))
	}
	dyn, kube := testutil.ScaleClients(tb, objs...)
	w.Update = func(ctx context.Context, u *unstructured.Unstructured) error {
		_, err := dyn.Resource(w.RouteGVR).Namespace(u.GetNamespace()).Update(ctx, u, metav1.UpdateOptions{})
		return err
	}
	det := install.UpstreamDetector{}
	w.Cache = cache.New(cache.Config{Dynamic: dyn, Kube: kube, Served: served, Now: now, Changes: w.Signal,
		PodSelectors: []labels.Selector{det.ControllerPodSelector()}, DeploymentSelectors: []labels.Selector{det.ControllerSelector(), det.ProxySelector()}})
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)
	w.Cache.Start(ctx)
	for deadline := time.Now().Add(time.Minute); !w.Cache.Settled(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			tb.Fatal("cache did not settle")
		}
	}
	testutil.Drain(w.Signal.C()) // the settle notify precedes Settled
	return w
}
