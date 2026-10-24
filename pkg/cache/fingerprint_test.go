// The scale world imports pkg/cache, so its benchmark lives in an external test package.
package cache_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
)

// scaleCache is a running cache over fake clients seeded with tier's world,
// Backends included, returned once synced. It builds the fakes as
// testutil.ClientsWith does, which takes a *testing.T.
func scaleCache(b *testing.B, tier scale.Tier) *cache.Cache {
	b.Helper()
	served := testutil.ServedUpstream()
	listKinds := map[schema.GroupVersionResource]string{}
	byKind := map[schema.GroupKind]schema.GroupVersionResource{}
	for _, s := range served {
		listKinds[s.GVR()] = s.Kind + "List"
		byKind[schema.GroupKind{Group: s.Group, Kind: s.Kind}] = s.GVR()
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds)
	for i, o := range scale.World(tier, 1) {
		gvr, ok := byKind[o.GroupVersionKind().GroupKind()]
		if !ok {
			b.Fatalf("unserved kind %s", o.GetKind())
		}
		o.SetResourceVersion("1")
		o.SetUID(types.UID(strconv.Itoa(i)))
		if err := dyn.Tracker().Create(gvr, o, o.GetNamespace()); err != nil {
			b.Fatal(err)
		}
	}
	var typed []runtime.Object
	for _, ns := range testutil.ScaleNamespaces(tier.Namespaces) {
		typed = append(typed, ns)
	}
	sig := trigger.New()
	c := cache.New(cache.Config{Dynamic: dyn, Kube: kubefake.NewSimpleClientset(typed...), Served: served, Changes: sig})
	ctx, cancel := context.WithCancel(context.Background())
	b.Cleanup(cancel)
	c.Start(ctx)
	// Initial-list adds notify too; Start's last notify follows the sync.
	for !c.Snapshot().Synced {
		select {
		case <-sig.C():
		case <-time.After(time.Minute):
			b.Fatal("cache did not sync")
		}
	}
	return c
}

// BenchmarkFingerprint measures one confirm-tick walk of the stores (D-019).
func BenchmarkFingerprint(b *testing.B) {
	for _, tier := range []scale.Tier{scale.Large, scale.Extreme} {
		b.Run(tier.Name, func(b *testing.B) {
			c := scaleCache(b, tier)
			if n := c.Fingerprint().Kinds[schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}.String()].Count; n < tier.HTTPRoutes {
				b.Fatalf("seeded %d HTTPRoutes, want at least %d", n, tier.HTTPRoutes)
			}
			b.ReportAllocs()
			for b.Loop() {
				c.Fingerprint()
			}
		})
	}
}
