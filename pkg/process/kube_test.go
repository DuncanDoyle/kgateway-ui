package process_test

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/internal/testutil/healthtest"
	"github.com/DuncanDoyle/snellius/internal/testutil/scale"
	"github.com/DuncanDoyle/snellius/pkg/api"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/process"
	"github.com/DuncanDoyle/snellius/pkg/scopeverify"
)

const proxyImage = "cr.kgateway.dev/kgateway-dev/envoy-wrapper:v2.4.7"

// proxyDeployment is kgateway v2.3/v2.4's proxy Deployment for ns/gw, named and
// labelled by the safe name; owned adds the Gateway owner.
func proxyDeployment(ns, gw string, replicas int32, owned bool) *appsv1.Deployment {
	safe := process.SafeGatewayLabelValue(gw)
	sel := map[string]string{"app.kubernetes.io/name": safe, "app.kubernetes.io/instance": safe, "gateway.networking.k8s.io/gateway-name": safe}
	lbls := maps.Clone(sel)
	maps.Copy(lbls, map[string]string{"kgateway": "kube-gateway", "app.kubernetes.io/component": "proxy", "app.kubernetes.io/managed-by": "kgateway"})
	pod := maps.Clone(sel)
	maps.Copy(pod, map[string]string{"kgateway": "kube-gateway", "app.kubernetes.io/component": "proxy"})
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: safe, Labels: lbls, UID: types.UID("proxy/" + ns + "/" + gw), Generation: 1},
		Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: sel},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: pod}, Spec: corev1.PodSpec{Containers: []corev1.Container{
				{Name: "kgateway-proxy", Image: proxyImage, ReadinessProbe: &corev1.Probe{}}}}}},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: replicas, UpdatedReplicas: replicas, ReadyReplicas: replicas, AvailableReplicas: replicas,
			Conditions: []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue, Reason: "NewReplicaSetAvailable",
				LastUpdateTime: testutil.Old}}},
	}
	if owned {
		d.OwnerReferences = []metav1.OwnerReference{{APIVersion: "gateway.networking.k8s.io/v1", Kind: "Gateway", Name: gw, UID: types.UID("Gateway/" + ns + "/" + gw)}}
	}
	return d
}

// podOf is Pod name of d, Ready, running since an hour ago.
func podOf(d *appsv1.Deployment, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: d.Namespace, Name: name, UID: types.UID("pod/" + d.Namespace + "/" + name), Labels: maps.Clone(d.Spec.Template.Labels)},
		Spec:       *d.Spec.Template.Spec.DeepCopy(),
		Status: corev1.PodStatus{Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: d.Spec.Template.Spec.Containers[0].Name, Ready: true, RestartCount: 1,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: testutil.Old}}}}},
	}
}

// acmeDetector is a second distribution with upstream's proxy labels.
type acmeDetector struct{}

func (acmeDetector) Name() string { return "acme" }
func (acmeDetector) ControllerSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"app": "acme-controller"})
}
func (acmeDetector) ControllerPodSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"app": "acme-controller"})
}
func (acmeDetector) ProxySelector() labels.Selector {
	return install.UpstreamDetector{}.ProxySelector()
}
func (acmeDetector) Detect(d *appsv1.Deployment) (install.Install, bool) {
	return install.Install{Namespace: d.Namespace, Name: d.Name, Edition: "acme", Container: "acme", ControllerNames: []string{"acme.example/gateway"},
		Version: "1.0.0"}, true
}

func acmeController(ns string) *appsv1.Deployment {
	one := int32(1)
	sel := map[string]string{"app": "acme-controller"}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "acme", Labels: sel, UID: types.UID("deploy/" + ns + "/acme")},
		Spec: appsv1.DeploymentSpec{Replicas: &one, Selector: &metav1.LabelSelector{MatchLabels: sel},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: sel}, Spec: corev1.PodSpec{Containers: []corev1.Container{
				{Name: "acme", Image: "registry.example/acme/controller:v1.0.0"}}}}},
		Status: appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
	}
}

type staticSnap struct{ s *cache.Snapshot }

func (s staticSnap) Snapshot() *cache.Snapshot { return s.s }

// world is a synced snapshot of kgw/kgateway, its class, objs, deps and Pods;
// events adds a running Events watch.
func world(t *testing.T, objs []*unstructured.Unstructured, deps []*appsv1.Deployment, ctrlPods, proxyPods []*corev1.Pod, events bool) *cache.Snapshot {
	t.Helper()
	ctrl := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	if ctrlPods == nil {
		ctrlPods = []*corev1.Pod{testutil.ControllerPod(ctrl, "kgateway-0", true)}
	}
	access := map[schema.GroupVersionResource]cache.Access{cache.NamespacesGVR: {OK: true}, cache.PodsGVR: {OK: true}, cache.DeploymentsGVR: {OK: true},
		cache.ProxyPodsGVR: {OK: true}}
	if events {
		access[cache.EventsGVR] = cache.Access{OK: true}
	}
	for _, k := range testutil.ServedUpstream() {
		access[k.GVR()] = cache.Access{OK: true}
	}
	all := append([]*unstructured.Unstructured{testutil.GatewayClass("kgateway", testutil.Controller, true)}, objs...)
	return &cache.Snapshot{Objects: testutil.ObjectsByGVR(t, all...), Access: access,
		Namespaces:  []*corev1.Namespace{testutil.Namespace("kgw", nil), testutil.Namespace("infra", nil), testutil.Namespace("acme-system", nil)},
		Deployments: append([]*appsv1.Deployment{ctrl}, deps...), Pods: ctrlPods, ProxyPods: proxyPods, Synced: true, TakenAt: testutil.Now}
}

func newEngine(snap *cache.Snapshot, checks []health.Check, detectors ...install.Detector) *engine.Engine {
	if detectors == nil {
		detectors = []install.Detector{install.UpstreamDetector{}}
	}
	if checks == nil {
		checks = health.Upstream()
	}
	return engine.New(engine.Config{Snapshots: staticSnap{snap}, Served: testutil.ServedUpstream(), Detectors: detectors,
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour),
		Checks: checks, Now: func() time.Time { return testutil.Now }})
}

func modelOf(t *testing.T, m *engine.Model, id string) engine.InstallModel {
	t.Helper()
	for _, im := range m.Installs {
		if im.Resolved.ID() == id {
			if im.Process == nil {
				t.Fatalf("install %s has no process data", id)
			}
			return im
		}
	}
	t.Fatalf("no install %s", id)
	return engine.InstallModel{}
}

func names(s process.Set) []string {
	var out []string
	for _, p := range s.Processes {
		out = append(out, p.Name)
	}
	slices.Sort(out)
	return out
}

// Review Focus 1: two installs' proxies in one namespace never cross.
func TestProxyPodsBelongToOneInstall(t *testing.T) {
	gwA, gwB := testutil.Gateway("infra", "a", "kgateway", nil), testutil.Gateway("infra", "b", "acme", nil)
	acmeClass := testutil.GatewayClass("acme", "acme.example/gateway", true)
	dA, dB := proxyDeployment("infra", "a", 2, true), proxyDeployment("infra", "b", 2, true)
	acme := acmeController("acme-system")
	acmePod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "acme-system", Name: "acme-0", UID: "pod/acme-0", Labels: map[string]string{"app": "acme-controller"}},
		Spec: acme.Spec.Template.Spec}
	snap := world(t, []*unstructured.Unstructured{gwA, gwB, acmeClass}, []*appsv1.Deployment{dA, dB, acme}, nil,
		[]*corev1.Pod{podOf(dA, "a-1"), podOf(dB, "b-1"), podOf(dA, "a-2"), podOf(dB, "b-2")}, false)
	snap.Pods = append(snap.Pods, acmePod)
	m := newEngine(snap, nil, acmeDetector{}, install.UpstreamDetector{}).Compute()

	up, other := modelOf(t, m, "kgw.kgateway"), modelOf(t, m, "acme-system.acme")
	for _, tc := range []struct {
		im    engine.InstallModel
		gw    string
		pods  []string
		ctrls []string
	}{{up, "a", []string{"a-1", "a-2"}, []string{"kgateway-0"}}, {other, "b", []string{"b-1", "b-2"}, []string{"acme-0"}}} {
		id := tc.im.Resolved.ID()
		if len(tc.im.Process.Proxies) != 1 {
			t.Fatalf("%s: want one Gateway's proxies, got %v", id, slices.Collect(maps.Keys(tc.im.Process.Proxies)))
		}
		s, ok := tc.im.Process.Proxies[gwRef(tc.gw)]
		if !ok || !s.Observed || !slices.Equal(names(s), tc.pods) {
			t.Fatalf("%s: proxies of %s: want observed %v, got %+v", id, tc.gw, tc.pods, s)
		}
		if got := names(tc.im.Process.Controller); !slices.Equal(got, tc.ctrls) {
			t.Fatalf("%s: controller processes: want %v, got %v", id, tc.ctrls, got)
		}
	}
}

// resolved is an upstream install, as Discover and Resolve give it.
func resolved() install.Resolved {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	in, _ := install.UpstreamDetector{}.Detect(d)
	return install.Resolved{Install: in}
}

// Review Focus 3: long Gateway names sharing a truncated prefix never merge.
func TestLongGatewayNameProxyPods(t *testing.T) {
	t.Run("scale worst world", func(t *testing.T) {
		// Task 25's long-named Gateways and their proxy Pods, with label-only Deployments.
		shape := scale.Large.Worst().Shape()
		gws := testutil.ScaleLongGatewayObjects(shape)
		if len(gws) != 3 || gws[0].GetName()[:50] != gws[1].GetName()[:50] {
			t.Fatalf("scale worst world: want 3 long Gateways, two sharing 50 characters: %v", gws)
		}
		var deps []*appsv1.Deployment
		for _, gw := range gws {
			safe := process.SafeGatewayLabelValue(gw.GetName())
			if want := testutil.SafeGatewayLabelValue(gw.GetName()); safe != want {
				t.Fatalf("label value of %s: want %s, got %s", gw.GetName(), want, safe)
			}
			d := proxyDeployment(gw.GetNamespace(), gw.GetName(), int32(shape.ProxyPodsPerGateway), false)
			d.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"gateway.networking.k8s.io/gateway-name": safe}}
			deps = append(deps, d)
		}
		snap := &cache.Snapshot{Deployments: deps, ProxyPods: testutil.ScaleProxyPods(shape, 1), TakenAt: testutil.Now,
			Access: map[schema.GroupVersionResource]cache.Access{cache.ProxyPodsGVR: {OK: true}}}
		seen := map[string]string{}
		for _, gw := range gws {
			s := process.Kube{}.Proxies(resolved(), objects.RefOf(gw), snap)
			if !s.Observed || len(s.Processes) != shape.ProxyPodsPerGateway {
				t.Fatalf("%s: want %d observed processes, got %+v", gw.GetName(), shape.ProxyPodsPerGateway, s)
			}
			for _, p := range s.Processes {
				if prev, dup := seen[p.ID]; dup {
					t.Fatalf("Pod %s maps to %s and %s", p.Name, prev, gw.GetName())
				}
				seen[p.ID] = gw.GetName()
				if !strings.HasPrefix(p.Name, gw.GetName()) {
					t.Fatalf("%s: Pod %s of another Gateway", gw.GetName(), p.Name)
				}
			}
		}
	})
	t.Run("70 characters sharing 63", func(t *testing.T) {
		prefix := strings.Repeat("abcdefghi-", 7)[:63]
		a, b := prefix+"-alpha1", prefix+"-bravo2"
		if len(a) != 70 || len(b) != 70 || process.SafeGatewayLabelValue(a) == process.SafeGatewayLabelValue(b) {
			t.Fatalf("names: %d, %d characters; label values %s, %s", len(a), len(b), process.SafeGatewayLabelValue(a), process.SafeGatewayLabelValue(b))
		}
		for _, owned := range []bool{true, false} {
			dA, dB := proxyDeployment("infra", a, 1, owned), proxyDeployment("infra", b, 2, owned)
			snap := &cache.Snapshot{Deployments: []*appsv1.Deployment{dB, dA}, TakenAt: testutil.Now,
				ProxyPods: []*corev1.Pod{podOf(dA, "pa-1"), podOf(dB, "pb-1"), podOf(dB, "pb-2")},
				Access:    map[schema.GroupVersionResource]cache.Access{cache.ProxyPodsGVR: {OK: true}}}
			for gw, want := range map[string][]string{a: {"pa-1"}, b: {"pb-1", "pb-2"}} {
				s := process.Kube{}.Proxies(resolved(), gwRef(gw), snap)
				if got := names(s); !s.Observed || !slices.Equal(got, want) || s.Desired == nil || int(*s.Desired) != len(want) {
					t.Fatalf("owner reference %v, %s: want %v, got %v (%+v)", owned, gw, want, got, s)
				}
			}
		}
	})
}

// D-016 R3: a Gateway without a proxy Deployment (here self-managed) is not observed.
func TestNoDeploymentIsNotObserved(t *testing.T) {
	gw := testutil.Gateway("infra", "edge", "kgateway", nil)
	testutil.SetGatewayParameters(gw, kinds.KgatewayGroup, "GatewayParameters", "sm")
	snap := world(t, []*unstructured.Unstructured{gw, testutil.GatewayParameters("infra", "sm", true)}, nil, nil, nil, true)
	im := modelOf(t, newEngine(snap, nil).Compute(), "kgw.kgateway")
	s, ok := im.Process.Proxies[gwRef("edge")]
	if !ok {
		t.Fatalf("no proxy set for infra/edge: %+v", im.Process.Proxies)
	}
	if s.Observed || s.Desired != nil || s.Rollout != nil || s.Processes != nil {
		t.Fatalf("self-managed proxies: want not observed with nil facts, got %+v", s)
	}
	if !im.Process.Controller.Observed || len(im.Process.Controller.Processes) != 1 {
		t.Fatalf("controller: want one observed process, got %+v", im.Process.Controller)
	}
}

// §9.1: the termination message never reaches the model or an API body.
func TestTerminationMessageNeverRead(t *testing.T) {
	const secret = "secret-value"
	finished := metav1.NewTime(testutil.Now.Add(-5 * time.Minute))
	term := &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 2, FinishedAt: finished, Message: "panic: token=" + secret}
	gw := testutil.Gateway("infra", "edge", "kgateway", nil)
	d := proxyDeployment("infra", "edge", 1, true)
	proxy := podOf(d, "edge-1")
	proxy.Status.ContainerStatuses[0].LastTerminationState.Terminated = term
	proxy.Status.Message = secret
	ctrl := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	cp := testutil.ControllerPod(ctrl, "kgateway-0", true)
	cp.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "kgateway", RestartCount: 3,
		State: corev1.ContainerState{Terminated: term}, LastTerminationState: corev1.ContainerState{Terminated: term}}}
	snap := world(t, []*unstructured.Unstructured{gw}, []*appsv1.Deployment{d}, []*corev1.Pod{cp}, []*corev1.Pod{proxy}, true)
	e := newEngine(snap, nil)
	im := modelOf(t, e.Compute(), "kgw.kgateway")

	b, err := json.Marshal([]any{im.Process.Controller, slices.Collect(maps.Values(im.Process.Proxies))})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) {
		t.Fatalf("process model holds the termination message: %s", b)
	}
	want := process.Termination{Reason: "Error", ExitCode: 2, At: finished.Time}
	for what, s := range map[string]process.Set{"controller": im.Process.Controller, "proxy": im.Process.Proxies[gwRef("edge")]} {
		if len(s.Processes) != 1 || s.Processes[0].Termination == nil || *s.Processes[0].Termination != want {
			t.Fatalf("%s: want termination %+v, got %+v", what, want, s)
		}
	}

	srv := httptest.NewServer(api.NewHandler(e, nil, nil))
	defer srv.Close()
	base := "/api/v1/installs/kgw.kgateway"
	for _, path := range []string{"/api/v1/meta", "/api/v1/installs", base + "/overview", base + "/health", base + "/findings", base + "/gateways",
		base + "/gateways/infra/edge", base + "/routes", base + "/listenersets", base + "/resources/apps/Deployment/infra/edge",
		base + "/resources/apps/Deployment/kgw/kgateway", base + "/refs"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if strings.Contains(string(body), secret) {
			t.Fatalf("%s (%d) serves the termination message: %s", path, resp.StatusCode, body)
		}
	}
}

// §9.1: a nil field means "not observed", never a failure.
func TestNilFieldsMeanNotObserved(t *testing.T) {
	d := proxyDeployment("infra", "edge", 1, true)
	bare := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "infra", Name: "edge-1", UID: "pod/edge-1", Labels: maps.Clone(d.Spec.Template.Labels)},
		Spec: *d.Spec.Template.Spec.DeepCopy()}
	snap := &cache.Snapshot{Deployments: []*appsv1.Deployment{d}, ProxyPods: []*corev1.Pod{bare}, TakenAt: testutil.Now,
		Access: map[schema.GroupVersionResource]cache.Access{cache.ProxyPodsGVR: {OK: true}}}
	gw := gwRef("edge")

	s := process.Kube{}.Proxies(resolved(), gw, snap)
	if !s.Observed || len(s.Processes) != 1 {
		t.Fatalf("want one observed process, got %+v", s)
	}
	p := s.Processes[0]
	if p.ID != "pod/edge-1" || p.Name != "edge-1" || p.Version != "2.4.7" {
		t.Fatalf("identity and version from the spec: %+v", p)
	}
	if p.Ready != nil || p.Restarts != nil || p.LastStart != nil || p.Termination != nil || p.Waiting != "" || p.Probe != nil {
		t.Fatalf("a Pod without status, Events off: want every fact nil, got %+v", p)
	}

	t.Run("events watch running", func(t *testing.T) {
		withEvents := *snap
		withEvents.Access = map[schema.GroupVersionResource]cache.Access{cache.ProxyPodsGVR: {OK: true}, cache.EventsGVR: {OK: true}}
		at := testutil.Now.Add(-time.Minute)
		withEvents.EventDeltas = map[types.UID][]cache.Observation{"pod/edge-1": {
			{At: testutil.Now.Add(-16 * time.Minute), Reason: "Unhealthy", Delta: 5}, // outside the window
			{At: testutil.Now.Add(-10 * time.Minute), Reason: "Unhealthy", Delta: 1},
			{At: testutil.Now.Add(-5 * time.Minute), Reason: "BackOff", Delta: 4},
			{At: at, Reason: "Unhealthy", Delta: 2}}}
		withEvents.EventHistoryUnknown = map[types.UID]bool{"pod/edge-1": true}
		got := process.Kube{}.Proxies(resolved(), gw, &withEvents).Processes[0].Probe
		want := process.ProbeFailures{Type: "readiness", Count: 3, First: testutil.Now.Add(-10 * time.Minute), Last: at, HistoryUnknown: true}
		if got == nil || *got != want {
			t.Fatalf("probe failures: want %+v, got %+v", want, got)
		}
		// Two probe types: the Event cannot say which failed.
		two := bare.DeepCopy()
		two.Spec.Containers[0].LivenessProbe = &corev1.Probe{}
		withEvents.ProxyPods = []*corev1.Pod{two}
		withEvents.EventDeltas = map[types.UID][]cache.Observation{"pod/edge-1": {{At: at, Reason: "Unhealthy", Delta: 1}}}
		withEvents.EventHistoryUnknown = nil
		got = process.Kube{}.Proxies(resolved(), gw, &withEvents).Processes[0].Probe
		if got == nil || *got != (process.ProbeFailures{Count: 1, First: at, Last: at}) {
			t.Fatalf("two probe types, one failure: want no type, got %+v", got)
		}
		// No counted failure, even with history unknown: no probe facts, so nothing reads "failed 0 times".
		withEvents.EventDeltas, withEvents.EventHistoryUnknown = nil, map[types.UID]bool{"pod/edge-1": true}
		got = process.Kube{}.Proxies(resolved(), gw, &withEvents).Processes[0].Probe
		if got != nil {
			t.Fatalf("no failures: want nil, got %+v", got)
		}
	})

	t.Run("proxy Pods not watched", func(t *testing.T) {
		off := *snap
		off.Access = map[schema.GroupVersionResource]cache.Access{}
		s := process.Kube{}.Proxies(resolved(), gw, &off)
		if s.Observed || s.Processes != nil || s.Desired == nil || *s.Desired != 1 {
			t.Fatalf("no proxy Pod watch: want not observed, Deployment facts kept, got %+v", s)
		}
	})
}

// A rollout under way, a settled one and a stuck one, from the Deployment.
func TestRolloutFromDeployment(t *testing.T) {
	gw := gwRef("edge")
	for _, tc := range []struct {
		name string
		mut  func(*appsv1.Deployment)
		want process.Rollout
	}{
		{"settled", func(*appsv1.Deployment) {}, process.Rollout{Reason: "NewReplicaSetAvailable", SettledAt: &testutil.Old.Time}},
		{"new generation", func(d *appsv1.Deployment) { d.Generation = 2 }, process.Rollout{Progressing: true, Reason: "NewReplicaSetAvailable"}},
		{"old replicas left", func(d *appsv1.Deployment) {
			d.Status.Replicas = 2
			d.Status.Conditions[0].Reason = "ReplicaSetUpdated"
		}, process.Rollout{Progressing: true, Reason: "ReplicaSetUpdated"}},
		{"deadline exceeded", func(d *appsv1.Deployment) {
			d.Status.UpdatedReplicas = 0
			d.Status.Conditions[0].Status, d.Status.Conditions[0].Reason = corev1.ConditionFalse, "ProgressDeadlineExceeded"
		}, process.Rollout{Reason: "ProgressDeadlineExceeded"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := proxyDeployment("infra", "edge", 1, true)
			tc.mut(d)
			s := process.Kube{}.Proxies(resolved(), gw, &cache.Snapshot{Deployments: []*appsv1.Deployment{d}, TakenAt: testutil.Now})
			if s.Rollout == nil || s.Rollout.Progressing != tc.want.Progressing || s.Rollout.Reason != tc.want.Reason ||
				(s.Rollout.SettledAt == nil) != (tc.want.SettledAt == nil) || (s.Rollout.SettledAt != nil && !s.Rollout.SettledAt.Equal(*tc.want.SettledAt)) {
				t.Fatalf("want %+v, got %+v", tc.want, s.Rollout)
			}
		})
	}
}

// processCheck reads Input.Process; declare picks whether it says so.
type processCheck struct {
	declare bool
	runs    *int
}

func (processCheck) Name() string { return "process probe" }
func (c processCheck) Inputs() health.InputSet {
	return health.InputSet{Process: c.declare}
}
func (c processCheck) Run(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
	if c.runs != nil {
		*c.runs++
	}
	var fs []health.Finding
	if p := ctx.Process; p != nil {
		for _, s := range p.Proxies {
			for _, pr := range s.Processes {
				if pr.Restarts != nil && *pr.Restarts > 1 {
					fs = append(fs, health.Finding{Code: "TEST-001", Slug: "restarts", Severity: health.SeverityWarning, Message: pr.Name})
				}
			}
		}
	}
	return fs, health.CheckResult{Name: "process probe", OK: true}
}

// Process data keys a declaring check's memo: a restart reruns it.
func TestProcessDataKeysTheMemo(t *testing.T) {
	gw := testutil.Gateway("infra", "edge", "kgateway", nil)
	d := proxyDeployment("infra", "edge", 1, true)
	pod := podOf(d, "edge-1")
	snap := world(t, []*unstructured.Unstructured{gw}, []*appsv1.Deployment{d}, nil, []*corev1.Pod{pod}, false)
	runs := 0
	src := &staticSnapPtr{s: snap}
	e := engine.New(engine.Config{Snapshots: src, Served: testutil.ServedUpstream(), Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour),
		Checks: []health.Check{processCheck{declare: true, runs: &runs}}, Now: func() time.Time { return testutil.Now }})
	first := e.Compute()
	e.Compute()
	if runs != 1 {
		t.Fatalf("unchanged process data: want 1 run, got %d", runs)
	}
	restarted := pod.DeepCopy()
	restarted.Status.ContainerStatuses[0].RestartCount = 2
	next := *snap
	next.ProxyPods = []*corev1.Pod{restarted}
	src.s = &next
	m := e.Compute()
	if runs != 2 || len(m.Installs[0].Health.Findings) != 1 {
		t.Fatalf("a restart: want a second run with one finding, got %d runs, %+v", runs, m.Installs[0].Health.Findings)
	}
	if a, b := modelOf(t, first, "kgw.kgateway").Process.Print, modelOf(t, m, "kgw.kgateway").Process.Print; a == b {
		t.Fatalf("print unchanged by a restart: %x", a)
	}
}

type staticSnapPtr struct{ s *cache.Snapshot }

func (s *staticSnapPtr) Snapshot() *cache.Snapshot { return s.s }

// recordTB records the guard's failures instead of failing the test.
type recordTB struct {
	testing.TB
	failed bool
}

func (r *recordTB) Errorf(string, ...any) { r.failed = true }
func (r *recordTB) Helper()               {}

// The declared-input guard perturbs process data a check does not declare.
func TestUndeclaredProcessIsCaught(t *testing.T) {
	gw := testutil.Gateway("infra", "edge", "kgateway", nil)
	d := proxyDeployment("infra", "edge", 1, true)
	pod := podOf(d, "edge-1")
	pod.Status.ContainerStatuses[0].RestartCount = 4
	snap := world(t, []*unstructured.Unstructured{gw}, []*appsv1.Deployment{d}, nil, []*corev1.Pod{pod}, false)
	im := modelOf(t, newEngine(snap, nil).Compute(), "kgw.kgateway")
	in := health.Input{Install: im.Resolved, Owned: im.Owned, Snapshot: snap, Served: testutil.ServedUpstream(), Now: testutil.Now, Process: im.Process}
	for _, declare := range []bool{true, false} {
		rec := &recordTB{TB: t}
		healthtest.CheckDeclaredInputs(rec, processCheck{declare: declare}, in, health.Runtime{}, 7)
		if rec.failed == declare {
			t.Fatalf("declared %v: guard failed %v", declare, rec.failed)
		}
	}
}

// gwRef is the ref of Gateway infra/name.
func gwRef(name string) objects.Ref { return testutil.GwRef("infra", name).Ref() }

// Pod order in the snapshot never changes the process data or its print.
func TestProcessOrderIsDeterministic(t *testing.T) {
	gw := testutil.Gateway("infra", "edge", "kgateway", nil)
	d := proxyDeployment("infra", "edge", 2, true)
	ctrl := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 2, 2)
	c1, c2 := testutil.ControllerPod(ctrl, "kgateway-a", true), testutil.ControllerPod(ctrl, "kgateway-b", true)
	p1, p2 := podOf(d, "edge-a"), podOf(d, "edge-b")
	fwd := world(t, []*unstructured.Unstructured{gw}, []*appsv1.Deployment{d}, []*corev1.Pod{c1, c2}, []*corev1.Pod{p1, p2}, false)
	rev := *fwd
	rev.Pods, rev.ProxyPods = []*corev1.Pod{c2, c1}, []*corev1.Pod{p2, p1}
	runs := 0
	src := &staticSnapPtr{s: fwd}
	e := engine.New(engine.Config{Snapshots: src, Served: testutil.ServedUpstream(), Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour),
		Checks: []health.Check{processCheck{declare: true, runs: &runs}}, Now: func() time.Time { return testutil.Now }})
	a := modelOf(t, e.Compute(), "kgw.kgateway").Process
	src.s = &rev
	b := modelOf(t, e.Compute(), "kgw.kgateway").Process
	if a.Print != b.Print || runs != 1 {
		t.Fatalf("reordered Pods: prints %x and %x, %d runs", a.Print, b.Print, runs)
	}
	if got := names(b.Controller); !slices.Equal(got, []string{"kgateway-a", "kgateway-b"}) || b.Controller.Processes[0].Name != "kgateway-a" {
		t.Fatalf("controller processes out of order: %v", b.Controller.Processes)
	}
}

// Owner references need the Gateway API group; several label matches give none.
func TestProxyDeploymentMatching(t *testing.T) {
	sel := install.UpstreamDetector{}.ProxySelector()
	gw := gwRef("edge")
	other := proxyDeployment("infra", "edge", 1, true)
	other.OwnerReferences[0].APIVersion = "example.com/v1"
	other.Labels["gateway.networking.k8s.io/gateway-name"] = "unrelated"
	if got := process.ProxyDeployment(gw, []*appsv1.Deployment{other}, sel); got != nil {
		t.Fatalf("an owner of another group matched: %s", got.Name)
	}
	a, b := proxyDeployment("infra", "edge", 1, false), proxyDeployment("infra", "edge", 1, false)
	b.Name = "edge-copy"
	if got := process.ProxyDeployment(gw, []*appsv1.Deployment{a}, sel); got != a {
		t.Fatalf("one label match: want it, got %v", got)
	}
	if got := process.ProxyDeployment(gw, []*appsv1.Deployment{a, b}, sel); got != nil {
		t.Fatalf("two label matches: want none, got %s", got.Name)
	}
}
