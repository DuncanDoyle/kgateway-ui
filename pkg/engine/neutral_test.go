package engine

import (
	"slices"
	"sort"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// neutralFindings returns "code resource" for each finding of rep whose code
// is not one the cluster-only inputs raise.
func neutralFindings(rep health.Report, skip ...string) []string {
	var out []string
	for _, f := range rep.Findings {
		if slices.Contains(skip, f.Code) {
			continue
		}
		res := ""
		if f.Resource != nil {
			res = f.Resource.String()
		}
		out = append(out, f.Code+" "+res)
	}
	sort.Strings(out)
	return out
}

// §19: with no Pods, Events, Services, EndpointSlices or CRD metadata the
// process findings are "not observed", 023 is "not available", and every
// other code equals the run on the full snapshot.
func TestAnalyzerWithoutPodsEventsServicesOrCRDs(t *testing.T) {
	// One route with no status (003), one to a Service with no ready endpoint (023), one with an unresolved ref (013).
	unattached := testutil.HTTPRoute("shop", "plain", []objects.ParentRef{testutil.GwRef("infra", "edge")})
	gw := testutil.GwRef("infra", "edge")
	resolved := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}
	cart := testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: resolved})
	cart.Object["spec"].(map[string]any)["rules"] = []any{map[string]any{"backendRefs": []any{map[string]any{"name": "cart", "port": int64(8080)}}}}

	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	broken.Object["spec"].(map[string]any)["rules"] = []any{map[string]any{"backendRefs": []any{map[string]any{"name": "gone", "port": int64(80)}}}}

	full := snapshot(t, unattached, cart, broken)
	d := full.Deployments[0]
	pod := testutil.ControllerPod(d, "p", true)
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "kgateway", RestartCount: 3,
		State:                corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(testutil.Now.Add(-2 * time.Minute))}},
		LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137, FinishedAt: metav1.NewTime(testutil.Now.Add(-2 * time.Minute))}}}}
	full.Pods = []*corev1.Pod{pod}
	full.Access[cache.PodsGVR], full.Access[cache.ProxyPodsGVR] = cache.Access{OK: true}, cache.Access{OK: true}
	full.Access[cache.EventsGVR], full.Access[cache.CRDsGVR] = cache.Access{OK: true}, cache.Access{OK: true}
	full.Access[cache.ServicesGVR], full.Access[cache.EndpointSlicesGVR] = cache.Access{OK: true}, cache.Access{OK: true}
	full.Services = []cache.ServiceFact{{Namespace: "shop", Name: "cart", Type: "ClusterIP", Ports: []cache.ServicePort{{Name: "http", Port: 8080, Target: 8080}}}}
	full.EndpointSlices = []cache.SliceFact{{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{{Name: "http", Port: 8080}}, Ready: map[string]int{"http": 0}, Total: 1}}
	full.CRDs = []cache.CRDMeta{{Name: "gateways.gateway.networking.k8s.io", Group: "gateway.networking.k8s.io", BundleVersion: "v1.4.1", Channel: "standard"}}

	bare := snapshot(t, unattached, cart, broken)
	bare.Pods = nil
	delete(bare.Access, cache.PodsGVR)

	now := testutil.Now
	run := func(s *cache.Snapshot) *InstallModel {
		e := New(config(s, metricsAt(testutil.Now), func() time.Time { return now }))
		e.Compute()
		now = now.Add(health.EndpointsHold + health.Grace + time.Second) // past every hold and grace
		return &e.Compute().Installs[0]
	}
	fullM := run(full)
	now = testutil.Now
	bareM := run(bare)

	has := func(rep health.Report, code string) bool {
		for _, f := range rep.Findings {
			if f.Code == code {
				return true
			}
		}
		return false
	}
	if !has(fullM.Health, health.CodeProcessRestarting) || !has(fullM.Health, health.CodeBackendNoReadyEndpoints) {
		t.Fatalf("the full snapshot must raise 019 and 023: %v", neutralFindings(fullM.Health))
	}
	if has(bareM.Health, health.CodeProcessRestarting) || has(bareM.Health, health.CodeBackendNoReadyEndpoints) {
		t.Fatalf("the bare snapshot raised a cluster-only finding: %v", neutralFindings(bareM.Health))
	}
	got, want := neutralFindings(bareM.Health), neutralFindings(fullM.Health, health.CodeProcessRestarting, health.CodeBackendNoReadyEndpoints, health.CodeVersionSkew)
	if len(got) != len(want) {
		t.Fatalf("findings differ:\n bare %v\n full %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("findings differ:\n bare %v\n full %v", got, want)
		}
	}
	// The checks that read the missing inputs report "not available"; none errors out.
	for _, name := range []string{"process restarts", "probe failures", "version skew", "backend endpoints"} {
		if c := checkNamed(bareM.Health, name); c == nil || c.Unavailable != health.AvailOff {
			t.Fatalf("check %q on the bare snapshot: %+v", name, c)
		}
		if c := checkNamed(fullM.Health, name); c == nil || c.Unavailable != "" {
			t.Fatalf("check %q on the full snapshot: %+v", name, c)
		}
	}
}

func checkNamed(rep health.Report, name string) *health.CheckResult {
	for i := range rep.Checks {
		if rep.Checks[i].Name == name {
			return &rep.Checks[i]
		}
	}
	return nil
}
