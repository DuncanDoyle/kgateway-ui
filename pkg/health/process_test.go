package health

import (
	"maps"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
)

// processInput is w's Input, with proxyPods watched, and the process data
// process.Kube reads from its snapshot. Metrics never go stale, so Next is the checks' own.
func processInput(t *testing.T, w *world, proxyPods ...*corev1.Pod) Input {
	t.Helper()
	in := w.input(t)
	in.MetricsMaxAge = 0
	in.Snapshot.ProxyPods, in.Snapshot.Access[cache.ProxyPodsGVR] = proxyPods, cache.Access{OK: true}
	d := &ProcessData{Controller: process.Kube{}.Controller(in.Install, in.Snapshot), Proxies: map[objects.Ref]process.Set{}}
	for _, gw := range in.Owned.Gateways {
		r := objects.RefOf(gw)
		d.Proxies[r] = process.Kube{}.Proxies(in.Install, r, in.Snapshot)
	}
	in.Process = d
	return in
}

// withProxy gives w an edge proxy Deployment with a selector, as kgateway renders it.
func withProxy(w *world, desired, ready int32) *appsv1.Deployment {
	d := testutil.ProxyDeployment("infra", "edge", desired, ready)
	sel := map[string]string{"gateway.networking.k8s.io/gateway-name": "edge"}
	d.Spec.Selector = &metav1.LabelSelector{MatchLabels: sel}
	d.Spec.Template.Labels = maps.Clone(sel)
	w.deploys[1] = d
	return d
}

// proxyPod is a Pod of the edge proxy, ready or not, with container status cs.
func proxyPod(name string, ready bool, cs corev1.ContainerStatus) *corev1.Pod {
	st := corev1.ConditionTrue
	if !ready {
		st = corev1.ConditionFalse
	}
	cs.Name = "envoy"
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "infra", Name: name, UID: types.UID("pod/infra/" + name),
			Labels: map[string]string{"gateway.networking.k8s.io/gateway-name": "edge"}},
		Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "envoy", Image: "cr.kgateway.dev/kgateway-dev/envoy-wrapper:v2.4.7"}}},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: st}}, ContainerStatuses: []corev1.ContainerStatus{cs}},
	}
}

// ctrlStatus sets the controller container status of w's Pod p.
func ctrlStatus(p *corev1.Pod, cs corev1.ContainerStatus) *corev1.Pod {
	cs.Name = "kgateway"
	p.Status.ContainerStatuses = []corev1.ContainerStatus{cs}
	return p
}

func running(at time.Time) corev1.ContainerState {
	return corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(at)}}
}

func exited(reason string, code int32, at time.Time) corev1.ContainerState {
	return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: reason, ExitCode: code, FinishedAt: metav1.NewTime(at)}}
}

func find(r Report, code string) *Finding {
	for i := range r.Findings {
		if r.Findings[i].Code == code {
			return &r.Findings[i]
		}
	}
	return nil
}

// A restart count that grows while watched raises 019 on the controller
// Deployment until 15 min after the restart, which wakes the report.
func TestRestartWhileWatched(t *testing.T) {
	w := healthyWorld()
	st := NewStates()
	w.pods[0] = ctrlStatus(w.pods[0], corev1.ContainerStatus{RestartCount: 0, State: running(testutil.Old.Time)})
	if r := Evaluate(processInput(t, w), Upstream(), Runtime{States: st}); find(r, CodeProcessRestarting) != nil {
		t.Fatalf("019 before any restart: %+v", r.Findings)
	}
	at := testutil.Now.Add(-time.Minute)
	w.pods[0] = ctrlStatus(w.pods[0], corev1.ContainerStatus{RestartCount: 1, State: running(at), LastTerminationState: exited("OOMKilled", 137, at)})
	in := processInput(t, w)
	r := Evaluate(in, Upstream(), Runtime{States: st})
	f := find(r, CodeProcessRestarting)
	if f == nil || f.Severity != SeverityWarning || f.Resource.String() != "kgw/kgateway" || f.Resource.Kind != "Deployment" {
		t.Fatalf("want 019 on the controller Deployment, got %+v", r.Findings)
	}
	ev, ok := f.Facts.(*ProcessEvidence)
	if !ok || ev.Pod != "p" || ev.Restarts == nil || *ev.Restarts != 1 || ev.Termination == nil || ev.Termination.Reason != "OOMKilled" || ev.Termination.ExitCode != 137 {
		t.Fatalf("evidence %+v", f.Facts)
	}
	if len(f.Evidence) != 1 || f.Evidence[0] != (objects.Ref{Kind: "Pod", Namespace: "kgw", Name: "p"}) {
		t.Fatalf("the evidence must depend on the Pod it names: %+v", f.Evidence)
	}
	if !r.Next.Equal(at.Add(15 * time.Minute)) {
		t.Fatalf("Next %v, want the restart + 15 min", r.Next)
	}
	for _, tc := range []struct {
		d    time.Duration
		want bool
	}{{15*time.Minute - time.Second, true}, {15*time.Minute + time.Second, false}} {
		in.Now = at.Add(tc.d)
		if got := find(Evaluate(in, Upstream(), Runtime{States: st}), CodeProcessRestarting) != nil; got != tc.want {
			t.Fatalf("at restart + %s: 019 %v, want %v", tc.d, got, tc.want)
		}
	}
}

// A last termination within 15 min raises 019 without an observed growth.
// The id hashes only the code and resource, never the Pod or reason (D-040).
func TestRecentTerminationIsARestart(t *testing.T) {
	ids := map[string]bool{}
	for _, name := range []string{"p", "q"} {
		w := healthyWorld()
		w.pods[0] = testutil.ControllerPod(w.ctrl, name, true)
		w.pods[0] = ctrlStatus(w.pods[0], corev1.ContainerStatus{RestartCount: 3, State: running(testutil.Now.Add(-2 * time.Minute)),
			LastTerminationState: exited(name+"-reason", 1, testutil.Now.Add(-2*time.Minute))})
		f := find(Evaluate(processInput(t, w), Upstream(), Runtime{}), CodeProcessRestarting)
		if f == nil || f.Facts.(*ProcessEvidence).Restarts != nil {
			t.Fatalf("want 019 with no observed restart count, got %+v", f)
		}
		ids[f.ID] = true
	}
	if len(ids) != 1 {
		t.Fatalf("the id depends on the Pod or reason: %v", ids)
	}
}

// R37: a deleted controller Pod is replaced by one with 0 restarts and no
// termination. That is the restart hold's case, not 019, and the old Pod's
// count leaves no state behind (Review Focus 2).
func TestReplacedControllerPodIsNotARestart(t *testing.T) {
	w := healthyWorld()
	st := NewStates()
	w.pods[0] = ctrlStatus(w.pods[0], corev1.ContainerStatus{RestartCount: 2, State: running(testutil.Old.Time),
		LastTerminationState: exited("Error", 1, testutil.Old.Time)})
	Evaluate(processInput(t, w), Upstream(), Runtime{States: st})
	// The old Pod is terminating; its replacement starts fresh.
	old := ctrlStatus(w.pods[0], corev1.ContainerStatus{RestartCount: 0, State: exited("Completed", 0, testutil.Now.Add(-10*time.Second))})
	fresh := ctrlStatus(testutil.ControllerPod(w.ctrl, "p2", true), corev1.ContainerStatus{State: running(testutil.Now.Add(-5 * time.Second))})
	w.pods = []*corev1.Pod{old, fresh}
	if r := Evaluate(processInput(t, w), Upstream(), Runtime{States: st}); find(r, CodeProcessRestarting) != nil {
		t.Fatalf("a replaced Pod raised 019: %+v", r.Findings)
	}
	w.pods = []*corev1.Pod{fresh}
	Evaluate(processInput(t, w), Upstream(), Runtime{States: st})
	if m, _ := Load[*restartState](st.store(restartsCheckName), restartsKey); m == nil || len(m.counts) != 1 || len(m.seen) != 0 {
		t.Fatalf("store keeps %+v, want only the live Pod", m)
	} else if _, ok := m.counts[string(fresh.UID)]; !ok {
		t.Fatalf("store keeps %+v, want the live Pod", m)
	}
}

// Exit code 2 on the controller adds the Go panic note; the command is a next step.
func TestControllerPanicNote(t *testing.T) {
	w := healthyWorld()
	at := testutil.Now.Add(-30 * time.Second)
	w.pods[0] = ctrlStatus(w.pods[0], corev1.ContainerStatus{RestartCount: 1, State: running(at), LastTerminationState: exited("Error", 2, at)})
	f := find(Evaluate(processInput(t, w), Upstream(), Runtime{}), CodeProcessRestarting)
	if f == nil || !strings.Contains(f.Detail, "A Go panic exits with code 2.") || strings.Contains(f.Detail, "kubectl") {
		t.Fatalf("want the panic note, got %+v", f)
	}
}

// 005 and 006 name the waiting reason and last exit of a not-ready process,
// and depend on that Pod (§14 rule 16).
func TestNotReadyDetail(t *testing.T) {
	w := healthyWorld()
	w.ctrl.Spec.Replicas = new(int32(2))
	w.ctrl.Status.ReadyReplicas = 0
	w.pods[0] = testutil.ControllerPod(w.ctrl, "p", false)
	w.pods[0] = ctrlStatus(w.pods[0], corev1.ContainerStatus{RestartCount: 4,
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}, LastTerminationState: exited("OOMKilled", 137, testutil.Old.Time)})
	withProxy(w, 2, 0)
	r := Evaluate(processInput(t, w, proxyPod("edge-0", false, corev1.ContainerStatus{
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}})), Upstream(), Runtime{})
	c, p := find(r, CodeControllerNotReady), find(r, CodeProxyNotReady)
	if c == nil || c.Detail != "0/2 ready: CrashLoopBackOff, last exit OOMKilled" || len(c.Evidence) != 1 || c.Evidence[0].Name != "p" {
		t.Fatalf("005 %+v", c)
	}
	if p == nil || p.Detail != "0/2 ready: ImagePullBackOff" || len(p.Evidence) != 1 || p.Evidence[0] != (objects.Ref{Kind: "Pod", Namespace: "infra", Name: "edge-0"}) {
		t.Fatalf("006 %+v", p)
	}
	if ev, ok := p.Facts.(*ProcessEvidence); !ok || ev.Waiting != "ImagePullBackOff" {
		t.Fatalf("006 evidence %+v", p.Facts)
	}
}

// R38: an unpullable image on a new Gateway has no ready replica and no
// progress deadline yet, so it is 006, not 021.
func TestUnpullableImageOnNewGatewayIs006(t *testing.T) {
	w := healthyWorld()
	d := withProxy(w, 1, 0)
	d.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue, Reason: "ReplicaSetUpdated"}}
	r := Evaluate(processInput(t, w, proxyPod("edge-0", false, corev1.ContainerStatus{
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull"}}})), Upstream(), Runtime{})
	if f := find(r, CodeProxyNotReady); f == nil || f.Severity != SeverityError || f.Detail != "0/1 ready: ErrImagePull" {
		t.Fatalf("want 006 with the waiting reason, got %+v", r.Findings)
	}
	if find(r, CodeRolloutStuck) != nil {
		t.Fatalf("021 without ProgressDeadlineExceeded: %+v", r.Findings)
	}
}

// 021 needs ProgressDeadlineExceeded beside a ready replica; the waiting
// reason is evidence. Without a ready replica 006 covers it.
func TestRolloutStuck(t *testing.T) {
	pde := []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded"}}
	pulling := corev1.ContainerStatus{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}
	w := healthyWorld()
	d := withProxy(w, 1, 1)
	d.Status.Conditions = pde
	newPod, oldPod := proxyPod("edge-new", false, pulling), proxyPod("edge-old", true, corev1.ContainerStatus{State: running(testutil.Old.Time)})
	r := Evaluate(processInput(t, w, newPod, oldPod), Upstream(), Runtime{})
	f := find(r, CodeRolloutStuck)
	if f == nil || f.Severity != SeverityWarning || f.Resource.String() != "infra/edge" || f.Resource.Kind != "Gateway" || find(r, CodeProxyNotReady) != nil {
		t.Fatalf("want 021 on the Gateway and no 006, got %+v", r.Findings)
	}
	if ev, ok := f.Facts.(*ProcessEvidence); !ok || ev.Pod != "edge-new" || ev.Waiting != "ImagePullBackOff" || len(f.Evidence) != 1 {
		t.Fatalf("evidence %+v, deps %+v", f.Facts, f.Evidence)
	}
	d.Status.ReadyReplicas = 0
	r = Evaluate(processInput(t, w, newPod), Upstream(), Runtime{})
	if find(r, CodeRolloutStuck) != nil || find(r, CodeProxyNotReady) == nil {
		t.Fatalf("no ready replica: want 006 and no 021, got %+v", r.Findings)
	}
}

// 020 needs 3 observed probe failures on one Pod; events off is not available.
func TestProbeFailures(t *testing.T) {
	w := healthyWorld()
	in := processInput(t, w)
	if r := Evaluate(in, Upstream(), Runtime{}); resultOf(r, probesCheckName).Unavailable != AvailOff || r.State == StateUnknown {
		t.Fatalf("events off: %+v, state %s", resultOf(r, probesCheckName), r.State)
	}
	first := testutil.Now.Add(-5 * time.Minute)
	for _, tc := range []struct {
		count int
		want  bool
	}{{2, false}, {3, true}} {
		in := processInput(t, w)
		in.Snapshot.Access[cache.EventsGVR] = cache.Access{OK: true}
		ready, last := false, testutil.Now.Add(-time.Minute)
		in.Process.Controller.Processes = []process.Process{{ID: "u", Name: "p", Ready: &ready,
			Probe: &process.ProbeFailures{Type: "readiness", Count: tc.count, First: first, Last: last}}}
		r := Evaluate(in, Upstream(), Runtime{})
		f := find(r, CodeProbeFailing)
		if (f != nil) != tc.want {
			t.Fatalf("%d failures: 020 %v, want %v", tc.count, f != nil, tc.want)
		}
		if f != nil {
			ev := f.Facts.(*ProcessEvidence)
			if ev.Probe == nil || ev.Probe.Type != "readiness" || ev.Probe.Failures != 3 || !ev.Probe.Last.Equal(last) || f.Detail != "Pod p failed its readiness probe 3 times in the last 15 minutes, from Events." {
				t.Fatalf("020 %+v %+v", f, ev.Probe)
			}
			if want := first.Add(cache.EventWindow + time.Nanosecond); !r.Next.Equal(want) {
				t.Fatalf("Next %v, want %v", r.Next, want)
			}
		}
	}
}

// A Gateway named past 63 characters, whose proxy Deployment carries only the
// truncated gateway-name label, still has its proxy: no false 006.
func TestLongGatewayNameWithoutOwnerHasProxy(t *testing.T) {
	for _, name := range testutil.ScaleLongGateways() {
		w := healthyWorld()
		w.objs[1] = testutil.Gateway("infra", name, "kgateway", nil)
		d := testutil.ProxyDeployment("infra", name, 1, 1)
		d.Name, d.OwnerReferences = process.SafeGatewayLabelValue(name), nil
		d.Labels["gateway.networking.k8s.io/gateway-name"] = process.SafeGatewayLabelValue(name)
		w.deploys[1] = d
		if r := Evaluate(processInput(t, w), Upstream(), Runtime{}); has(r, CodeProxyNotReady, "", "") {
			t.Fatalf("%s: false 006: %+v", name, r.Findings)
		}
	}
}

func resultOf(r Report, name string) CheckResult {
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	return CheckResult{}
}

// R43: with 1 of 2 replicas ready, 006 and 021 both show; 021 needs 0 ready to yield.
func TestRolloutStuckBesideOneReady(t *testing.T) {
	w := healthyWorld()
	d := withProxy(w, 2, 1)
	d.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded"}}
	r := Evaluate(processInput(t, w, proxyPod("edge-new", false, corev1.ContainerStatus{State: corev1.ContainerState{
		Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}), proxyPod("edge-old", true, corev1.ContainerStatus{State: running(testutil.Old.Time)})), Upstream(), Runtime{})
	if find(r, CodeRolloutStuck) == nil || find(r, CodeProxyNotReady) == nil {
		t.Fatalf("want 006 and 021, got %+v", r.Findings)
	}
}

// R44: a terminating Pod with restarts and a fresh termination is no process, so no 019.
func TestTerminatingPodIsNotARestart(t *testing.T) {
	w := healthyWorld()
	now := metav1.NewTime(testutil.Now)
	w.pods[0] = ctrlStatus(w.pods[0], corev1.ContainerStatus{RestartCount: 2, State: exited("Error", 143, testutil.Now.Add(-5*time.Second))})
	w.pods[0].DeletionTimestamp = &now
	if r := Evaluate(processInput(t, w), Upstream(), Runtime{}); find(r, CodeProcessRestarting) != nil {
		t.Fatalf("a terminating Pod raised 019: %+v", r.Findings)
	}
}

// M4: several label-only Deployments for one Gateway get their own 006 message.
func TestAmbiguousProxyDeployment(t *testing.T) {
	w := healthyWorld()
	a, b := testutil.ProxyDeployment("infra", "edge", 1, 1), testutil.ProxyDeployment("infra", "edge", 1, 1)
	a.OwnerReferences, b.OwnerReferences, b.Name = nil, nil, "edge-copy"
	w.deploys = []*appsv1.Deployment{w.ctrl, a, b}
	f := find(Evaluate(processInput(t, w), Upstream(), Runtime{}), CodeProxyNotReady)
	if f == nil || f.Message != "More than one proxy deployment matches this Gateway" || f.Severity != SeverityWarning {
		t.Fatalf("006 %+v", f)
	}
}

// R42: without the proxy Pod watch the restart and probe results name the gap
// and its reason, and the controller findings still show.
func TestProxyGapInResult(t *testing.T) {
	w := healthyWorld()
	at := testutil.Now.Add(-time.Minute)
	w.pods[0] = ctrlStatus(w.pods[0], corev1.ContainerStatus{RestartCount: 1, State: running(at), LastTerminationState: exited("Error", 1, at)})
	withProxy(w, 1, 1)
	for _, tc := range []struct {
		name string
		acc  *cache.Access
		why  Availability
	}{{"off", nil, AvailOff}, {"denied", &cache.Access{Err: "forbidden: no", Denied: true}, AvailDenied}, {"watched", &cache.Access{OK: true}, ""}} {
		in := w.input(t)
		in.Snapshot.Access[cache.EventsGVR] = cache.Access{OK: true}
		if tc.acc != nil {
			in.Snapshot.Access[cache.ProxyPodsGVR] = *tc.acc
		}
		in.Process = &ProcessData{Controller: process.Kube{}.Controller(in.Install, in.Snapshot), Proxies: map[objects.Ref]process.Set{}}
		edge := objects.RefOf(in.Owned.Gateways[0])
		in.Process.Proxies[edge] = process.Kube{}.Proxies(in.Install, edge, in.Snapshot)
		r := Evaluate(in, Upstream(), Runtime{})
		for _, name := range []string{restartsCheckName, probesCheckName} {
			c, wantDetail := resultOf(r, name), ""
			if tc.why != "" {
				wantDetail = "Proxy Pods are not watched; proxies were not checked"
			}
			if !c.OK || c.Detail != wantDetail || c.Unavailable != tc.why || r.State == StateUnknown {
				t.Fatalf("%s %s: %+v", tc.name, name, c)
			}
		}
		if find(r, CodeProcessRestarting) == nil {
			t.Fatalf("%s: controller 019 missing", tc.name)
		}
	}
}
