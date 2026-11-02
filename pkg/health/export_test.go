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
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
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
		"xds rejected per type": func(w *world) {
			k := ctrlmetrics.TypeKey{Gateway: ctrlmetrics.GatewayKey{Namespace: "infra", Name: "edge"}, TypeURL: "envoy.config.cluster.v3.Cluster"}
			w.metrics.Metrics.Rejects[k.Gateway] = 2
			for _, p := range []string{"p", "q"} {
				w.metrics.Metrics.PerPod = append(w.metrics.Metrics.PerPod, ctrlmetrics.PodMetrics{UID: "pod/kgw/" + p, Name: p,
					Active: map[ctrlmetrics.TypeKey]float64{k: 1}, Total: map[ctrlmetrics.TypeKey]float64{k: 4}})
			}
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
		"inside grace":     func(w *world) { w.firstSeen = young },
		"computed present": func(w *world) { w.computed = &computed.Views{RuleSet: "kgateway-2.4"} },
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
	for name, in := range processWorlds(t) {
		out[name] = in
	}
	return out
}

// processWorlds have process data: a recent restart with exit code 2,
// probe failures with Events on, a stuck proxy rollout, and a not-ready proxy.
func processWorlds(t *testing.T) map[string]Input {
	t.Helper()
	at, no, yes := testutil.Now.Add(-time.Minute), false, true
	three := int32(3)
	stuck := func(in Input) Input {
		edge := objects.RefOf(in.Owned.Gateways[0])
		in.Process.Proxies[edge] = process.Set{Observed: true, Rollout: &process.Rollout{Reason: "ProgressDeadlineExceeded"},
			Processes: []process.Process{{ID: "e1", Name: "edge-1", Ready: &no, Waiting: "ImagePullBackOff"}, {ID: "e0", Name: "edge-0", Ready: &yes}}}
		return in
	}
	worlds := map[string]func(w *world) func(Input) Input{
		"process restarted": func(*world) func(Input) Input {
			return func(in Input) Input {
				in.Process.Controller.Processes = []process.Process{{ID: "p", Name: "p", Ready: &yes, Restarts: &three, LastStart: &at,
					Termination: &process.Termination{Reason: "Error", ExitCode: 2, At: at}}}
				return in
			}
		},
		"probe failing": func(w *world) func(Input) Input {
			w.access = map[schema.GroupVersionResource]cache.Access{cache.EventsGVR: {OK: true}}
			return func(in Input) Input {
				in.Process.Controller.Processes = []process.Process{{ID: "p", Name: "p", Ready: &yes,
					Probe: &process.ProbeFailures{Type: "readiness", Count: 4, First: at, Last: at, HistoryUnknown: true}}}
				return in
			}
		},
		"rollout stuck": func(*world) func(Input) Input { return stuck },
		"version skew": func(w *world) func(Input) Input {
			w.access = map[schema.GroupVersionResource]cache.Access{cache.CRDsGVR: {OK: true}}
			return func(in Input) Input {
				in.Snapshot.CRDs = []cache.CRDMeta{{Name: "gateways.gateway.networking.k8s.io", BundleVersion: "v1.5.1", Channel: "standard"},
					{Name: "httproutes.gateway.networking.k8s.io", BundleVersion: "v1.6.1", Channel: "experimental"}}
				in.GatewayAPI = func(string) (string, bool) { return "v1.6", true }
				in.Process.Controller.Processes = []process.Process{{ID: "p", Name: "p", Ready: &yes, Version: "2.4.7"}, {ID: "q", Name: "q", Ready: &yes, Version: "2.3.1"}}
				edge := objects.RefOf(in.Owned.Gateways[0])
				in.Process.Proxies[edge] = process.Set{Observed: true, Processes: []process.Process{{ID: "e0", Name: "edge-0", Ready: &yes, Version: "2.3.6"}}}
				return in
			}
		},
		"proxy not ready with facts": func(w *world) func(Input) Input {
			w.deploys[1] = testutil.ProxyDeployment("infra", "edge", 2, 0)
			return stuck
		},
	}
	out := make(map[string]Input, len(worlds))
	for name, mutate := range worlds {
		w := healthyWorld()
		add := mutate(w)
		in := w.input(t)
		in.Process = &ProcessData{Controller: process.Set{Observed: true}, Proxies: map[objects.Ref]process.Set{objects.RefOf(in.Owned.Gateways[0]): {}}, Print: 1}
		out[name] = add(in)
	}
	return out
}
