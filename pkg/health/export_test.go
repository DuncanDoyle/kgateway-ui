package health

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// GuardWorlds returns the fixture worlds for the declared-input guard, which
// lives in package health_test so it can import internal/testutil/healthtest.
func GuardWorlds(t *testing.T) map[string]Input {
	t.Helper()
	gw := testutil.GwRef("infra", "edge")
	young := func(*unstructured.Unstructured) time.Time { return testutil.Now.Add(-10 * time.Second) }
	mutations := map[string]func(w *world){
		"healthy": func(*world) {},
		"xds rejected": func(w *world) {
			w.metrics.Metrics.Rejects[ctrlmetrics.GatewayKey{Namespace: "infra", Name: "edge"}] = 1
		},
		"route not processed":   func(w *world) { w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}) },
		"namespace not watched": func(w *world) { w.scope = `[{"matchLabels":{"team":"a"}}]` },
		"scope unknown":         func(w *world) { w.scope = "[{" },
		"controller not ready":  func(w *world) { w.ctrl.Status.ReadyReplicas = 0 },
		"proxy not ready":       func(w *world) { w.deploys[1] = testutil.ProxyDeployment("infra", "edge", 2, 1) },
		"metrics stale":         func(w *world) { w.metrics.CollectedAt = testutil.Now.Add(-5 * time.Minute) },
		"metrics missing":       func(w *world) { w.metrics = nil },
		"namespaces unreadable": func(w *world) {
			w.access = map[schema.GroupVersionResource]cache.Access{cache.NamespacesGVR: {Err: "connection refused"}}
		},
		"inside grace":      func(w *world) { w.firstSeen = young },
		"computed findings": func(w *world) { w.computed = &computed.Views{RuleSet: "kgateway-2.4"} },
	}
	out := make(map[string]Input, len(mutations)+1)
	for name, mutate := range mutations {
		w := healthyWorld()
		mutate(w)
		out[name] = w.input(t)
	}
	in := healthyWorld().input(t)
	in.KindsChange = kinds.Change{Removed: []string{"TCPRoute"}}
	out["kinds changed"] = in
	return out
}
