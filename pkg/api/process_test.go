package api

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

const healthPath = "/api/v1/installs/kgw.kgateway/health"

// procWorld is world() with ctrl applied to the controller Pod and, when set,
// proxy replacing the edge proxy Deployment, plus pods.
func procWorld(ctrl func(*corev1.Pod), proxy *appsv1.Deployment, pods ...*corev1.Pod) []runtime.Object {
	objs := world()
	for i, o := range objs {
		switch v := o.(type) {
		case *corev1.Pod:
			if ctrl != nil {
				ctrl(v)
			}
		case *appsv1.Deployment:
			if proxy != nil && v.Namespace == "infra" && v.Name == "edge" {
				objs[i] = proxy
			}
		}
	}
	for _, p := range pods {
		objs = append(objs, p)
	}
	return objs
}

// container sets p's controller container status.
func container(cs corev1.ContainerStatus) func(*corev1.Pod) {
	return func(p *corev1.Pod) {
		cs.Name = "kgateway"
		p.Status.ContainerStatuses = []corev1.ContainerStatus{cs}
	}
}

func runningSince(at time.Time) corev1.ContainerState {
	return corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(at)}}
}

func exitedAt(reason string, code int32, at time.Time) corev1.ContainerState {
	return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: reason, ExitCode: code, FinishedAt: metav1.NewTime(at),
		Message: "secret-value"}}
}

func waiting(reason string) corev1.ContainerState {
	return corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: "secret-value"}}
}

// edgeProxy is kgateway's proxy Deployment for infra/edge, with its selector
// and Pod template labels.
func edgeProxy(desired, ready int32) *appsv1.Deployment {
	d := testutil.ProxyDeployment("infra", "edge", desired, ready)
	sel := map[string]string{"app.kubernetes.io/name": "edge", "gateway.networking.k8s.io/gateway-name": "edge"}
	d.Spec.Selector = &metav1.LabelSelector{MatchLabels: sel}
	d.Spec.Template.Labels = maps.Clone(sel)
	maps.Copy(d.Spec.Template.Labels, map[string]string{"kgateway": "kube-gateway", "app.kubernetes.io/component": "proxy"})
	return d
}

// edgePod is a Pod of d with container status cs.
func edgePod(d *appsv1.Deployment, name string, ready bool, cs corev1.ContainerStatus) *corev1.Pod {
	st := corev1.ConditionFalse
	if ready {
		st = corev1.ConditionTrue
	}
	cs.Name = "envoy"
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: d.Namespace, Name: name, UID: types.UID("pod/infra/" + name), Labels: maps.Clone(d.Spec.Template.Labels)},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "envoy", Image: "cr.kgateway.dev/kgateway-dev/envoy-wrapper:v2.4.7"}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: st}}, ContainerStatuses: []corev1.ContainerStatus{cs}},
	}
}

// at computes h's model at t and returns its /health.
func (h *harness) at(t *testing.T, at time.Time) HealthResponse {
	t.Helper()
	h.clk.set(at)
	h.e.Compute()
	return get[HealthResponse](t, h.srv, healthPath, 200)
}

// awaitCache waits until the cache's snapshot satisfies cond.
func (h *harness) awaitCache(t *testing.T, what string, cond func(*cache.Snapshot) bool) {
	t.Helper()
	waitUntil(t, what, func() bool { return cond(h.cache.Snapshot()) })
}

// updatePod writes p and waits until the cache has its first container's restart count.
func (h *harness) updatePod(t *testing.T, p *corev1.Pod) {
	t.Helper()
	if _, err := h.kube.CoreV1().Pods(p.Namespace).Update(h.ctx, p, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	want := p.Status.ContainerStatuses[0].RestartCount
	// Controller and proxy Pods sit in separate informers; a Pod the proxy one holds must show the update too.
	updated := func(pods []*corev1.Pod) (found, ok bool) {
		for _, q := range pods {
			if q.UID == p.UID {
				found = true
				ok = len(q.Status.ContainerStatuses) > 0 && q.Status.ContainerStatuses[0].RestartCount == want
			}
		}
		return found, ok
	}
	h.awaitCache(t, "pod "+p.Name, func(s *cache.Snapshot) bool {
		_, inPods := updated(s.Pods)
		found, inProxy := updated(s.ProxyPods)
		return inPods && (!found || inProxy)
	})
}

// podOf returns the cache's copy of the controller Pod kgateway-0.
func (h *harness) ctrlPod(t *testing.T) *corev1.Pod {
	t.Helper()
	p, err := h.kube.CoreV1().Pods("kgw").Get(h.ctx, "kgateway-0", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// putEvent creates or updates ev and waits until the cache's deltas for its
// Pod sum to observed (with unknown history when unknown).
func (h *harness) putEvent(t *testing.T, ev *corev1.Event, create bool, observed int32, unknown bool) {
	t.Helper()
	var err error
	if create {
		_, err = h.kube.CoreV1().Events(ev.Namespace).Create(h.ctx, ev, metav1.CreateOptions{})
	} else {
		_, err = h.kube.CoreV1().Events(ev.Namespace).Update(h.ctx, ev, metav1.UpdateOptions{})
	}
	if err != nil {
		t.Fatal(err)
	}
	h.awaitCache(t, "event "+ev.Name, func(s *cache.Snapshot) bool {
		var n int32
		for _, o := range s.EventDeltas[ev.InvolvedObject.UID] {
			n += o.Delta
		}
		return n == observed && s.EventHistoryUnknown[ev.InvolvedObject.UID] == unknown
	})
}

func findingOf(h HealthResponse, code string) *FindingDTO {
	for i := range h.Findings {
		if h.Findings[i].Code == code {
			return &h.Findings[i]
		}
	}
	return nil
}

func checkOf(h HealthResponse, name string) *health.CheckResult {
	for i := range h.Checks {
		if h.Checks[i].Name == name {
			return &h.Checks[i]
		}
	}
	return nil
}

func processFacts(t *testing.T, f *FindingDTO) *health.ProcessEvidence {
	t.Helper()
	ev, ok := f.Facts.(*health.ProcessEvidence)
	if !ok {
		t.Fatalf("%s: evidence %T %+v, want process evidence", f.Code, f.Facts, f.Facts)
	}
	return ev
}

// A restart while watched raises 019 on the controller Deployment, present at
// restart + 15 min − 1 s and gone at + 1 s. The evidence carries the restarts
// in the window and the last termination, never its message.
func TestRestartWindowAtTheAPI(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason string
		code   int32
		panic  bool
	}{{"oom killed", "OOMKilled", 137, false}, {"exit code 2", "Error", 2, true}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, setup{}, procWorld(container(corev1.ContainerStatus{State: runningSince(testutil.Old.Time)}), nil)...)
			if f := findingOf(h.at(t, testutil.Now), health.CodeProcessRestarting); f != nil {
				t.Fatalf("019 before a restart: %+v", f)
			}
			restart := testutil.Now.Add(-time.Minute)
			p := h.ctrlPod(t)
			container(corev1.ContainerStatus{RestartCount: 1, State: runningSince(restart), LastTerminationState: exitedAt(tc.reason, tc.code, restart)})(p)
			h.updatePod(t, p)
			r := h.at(t, testutil.Now)
			f := findingOf(r, health.CodeProcessRestarting)
			if f == nil || f.Severity != health.SeverityWarning || f.Resource == nil || f.Resource.Ref != (objects.Ref{Group: "apps", Kind: "Deployment", Namespace: "kgw", Name: "kgateway"}) {
				t.Fatalf("want 019 on the controller Deployment, got %+v", r.Findings)
			}
			ev := processFacts(t, f)
			if ev.Pod != "kgateway-0" || ev.Restarts == nil || *ev.Restarts != 1 || ev.Termination == nil || ev.Termination.Reason != tc.reason ||
				ev.Termination.ExitCode != tc.code || !ev.Termination.At.Equal(restart) {
				t.Fatalf("evidence %+v", ev)
			}
			if got := strings.Contains(f.Detail, "A Go panic exits with code 2.") && !strings.Contains(f.Detail, "kubectl"); got != tc.panic {
				t.Fatalf("panic note %v, want %v: %q", got, tc.panic, f.Detail)
			}
			body := string(wire(t, h.srv, healthPath))
			if strings.Contains(body, "secret-value") || !strings.Contains(body, `"kind": "process"`) {
				t.Fatalf("body %s", body)
			}
			if f := findingOf(h.at(t, restart.Add(15*time.Minute-time.Second)), health.CodeProcessRestarting); f == nil {
				t.Fatal("019 gone before restart + 15 min")
			}
			if f := findingOf(h.at(t, restart.Add(15*time.Minute+time.Second)), health.CodeProcessRestarting); f != nil {
				t.Fatalf("019 after restart + 15 min: %+v", f)
			}
		})
	}
}

// R37: a replaced controller Pod (new UID, 0 restarts, no termination) is the
// restart hold's case (Task 40), not 019.
func TestReplacedControllerPodAtTheAPI(t *testing.T) {
	h := newHarness(t, setup{}, procWorld(container(corev1.ContainerStatus{RestartCount: 3, State: runningSince(testutil.Old.Time)}), nil)...)
	h.at(t, testutil.Now)
	old := h.ctrlPod(t)
	if err := h.kube.CoreV1().Pods("kgw").Delete(h.ctx, old.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	fresh := old.DeepCopy()
	fresh.Name, fresh.UID, fresh.ResourceVersion = "kgateway-1", "pod/kgw/kgateway-1", ""
	container(corev1.ContainerStatus{State: runningSince(testutil.Now.Add(-5 * time.Second))})(fresh)
	if _, err := h.kube.CoreV1().Pods("kgw").Create(h.ctx, fresh, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	h.awaitCache(t, "replacement Pod", func(s *cache.Snapshot) bool { return len(s.Pods) == 1 && s.Pods[0].UID == fresh.UID })
	if f := findingOf(h.at(t, testutil.Now.Add(time.Second)), health.CodeProcessRestarting); f != nil {
		t.Fatalf("a replaced Pod raised 019: %+v", f)
	}
}

// No ready replica: 005 and 006 carry "0/2 ready: CrashLoopBackOff, last exit
// OOMKilled" and no 021, even past the progress deadline.
func TestNotReadyDetailAtTheAPI(t *testing.T) {
	crash := corev1.ContainerStatus{RestartCount: 4, State: waiting("CrashLoopBackOff"), LastTerminationState: exitedAt("OOMKilled", 137, testutil.Old.Time)}
	d := edgeProxy(2, 0)
	d.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded"}}
	objs := replicaWorld(2, 0, 2, 0)
	for i, o := range objs {
		switch v := o.(type) {
		case *corev1.Pod:
			container(crash)(v)
		case *appsv1.Deployment:
			if v.Name == "edge" {
				objs[i] = d
			}
		}
	}
	objs = append(objs, edgePod(d, "edge-0", false, crash), edgePod(d, "edge-1", false, crash))
	r := get[HealthResponse](t, newHarness(t, setup{proxyPods: true}, objs...).srv, healthPath, 200)
	for _, code := range []string{health.CodeControllerNotReady, health.CodeProxyNotReady} {
		f := findingOf(r, code)
		if f == nil || f.Detail != "0/2 ready: CrashLoopBackOff, last exit OOMKilled" || processFacts(t, f).Waiting != "CrashLoopBackOff" {
			t.Fatalf("%s: %+v", code, f)
		}
	}
	if f := findingOf(r, health.CodeRolloutStuck); f != nil {
		t.Fatalf("021 without a ready replica: %+v", f)
	}
}

// 021 needs ProgressDeadlineExceeded. The waiting reasons of the Deployment's
// Pods are evidence; an image pull failure beside a ready old Pod without the
// deadline raises nothing.
func TestRolloutStuckAtTheAPI(t *testing.T) {
	for _, tc := range []struct {
		name string
		cond appsv1.DeploymentCondition
		want bool
	}{
		{"deadline exceeded", appsv1.DeploymentCondition{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded"}, true},
		{"still progressing", appsv1.DeploymentCondition{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue, Reason: "ReplicaSetUpdated"}, false},
		{"settled", appsv1.DeploymentCondition{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue, Reason: "NewReplicaSetAvailable"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := edgeProxy(1, 1)
			d.Status.Conditions = []appsv1.DeploymentCondition{tc.cond}
			old := edgePod(d, "edge-old", true, corev1.ContainerStatus{State: runningSince(testutil.Old.Time)})
			pulling := edgePod(d, "edge-new", false, corev1.ContainerStatus{State: waiting("ImagePullBackOff")})
			srv := newHarness(t, setup{proxyPods: true}, procWorld(nil, d, old, pulling)...).srv
			r := get[HealthResponse](t, srv, healthPath, 200)
			// The processes list names a reason for a stuck rollout only, never for a settled one.
			p := get[ProcessesResponse](t, srv, procsPath, 200)
			for _, g := range p.Proxies.Items {
				if g.Gateway.Ref == edgeRef.Ref() && (g.Set.Rollout == nil || (g.Set.Rollout.Reason != "") != tc.want) {
					t.Fatalf("processes: edge rollout %+v, want a reason %v", g.Set.Rollout, tc.want)
				}
			}
			if findingOf(r, health.CodeProxyNotReady) != nil {
				t.Fatalf("006 beside a ready replica: %+v", r.Findings)
			}
			f := findingOf(r, health.CodeRolloutStuck)
			if (f != nil) != tc.want {
				t.Fatalf("021 %v, want %v: %+v", f != nil, tc.want, r.Findings)
			}
			if f == nil {
				return
			}
			if ev := processFacts(t, f); f.Severity != health.SeverityWarning || f.Resource.Ref != edgeRef.Ref() || ev.Pod != "edge-new" || ev.Waiting != "ImagePullBackOff" {
				t.Fatalf("021 %+v, evidence %+v", f, ev)
			}
		})
	}
}

// probeWorld is world() with Events on and controller Pod kgateway-0
// defining the readiness probe, plus liveness when two.
func probeWorld(t *testing.T, two bool) (*harness, *corev1.Pod) {
	t.Helper()
	h := newHarness(t, setup{optional: true}, procWorld(func(p *corev1.Pod) {
		p.Spec.Containers[0].ReadinessProbe = &corev1.Probe{}
		if two {
			p.Spec.Containers[0].LivenessProbe = &corev1.Probe{}
		}
	}, nil)...)
	return h, h.ctrlPod(t)
}

// 2 observed probe failures raise nothing, 3 raise 020 with the probe type,
// count and time. One defined probe type is named; two read "a probe".
func TestProbeFailuresAtTheAPI(t *testing.T) {
	for _, two := range []bool{false, true} {
		h, pod := probeWorld(t, two)
		ev := testutil.WarningEvent(pod, "probe", "Unhealthy", testutil.Now)
		h.putEvent(t, ev, true, 1, false)
		for n := int32(2); n <= 3; n++ {
			last := testutil.Now.Add(time.Duration(n) * time.Second)
			h.clk.set(last)
			ev.Count, ev.LastTimestamp = n, metav1.NewTime(last)
			h.putEvent(t, ev, false, n, false)
			r := h.at(t, last)
			f := findingOf(r, health.CodeProbeFailing)
			if (f != nil) != (n == 3) {
				t.Fatalf("%d failures: 020 %+v", n, f)
			}
			if f == nil {
				continue
			}
			probe, wantType, wantDetail := processFacts(t, f).Probe, "readiness", "Events show Pod kgateway-0 failed its readiness probe 3 times in the last 15 minutes."
			if two {
				wantType, wantDetail = "", "Events show Pod kgateway-0 failed a probe 3 times in the last 15 minutes."
			}
			if probe == nil || probe.Type != wantType || probe.Failures != 3 || !probe.Last.Equal(last) || f.Detail != wantDetail ||
				f.Resource.Ref != (objects.Ref{Group: "apps", Kind: "Deployment", Namespace: "kgw", Name: "kgateway"}) {
				t.Fatalf("two probes %v: 020 %+v, probe %+v", two, f, probe)
			}
		}
	}
}

// An Event first seen with count 50 contributes nothing; later increments
// count, with "earlier failures not counted".
func TestFirstSeenCountIsNotCountedAtTheAPI(t *testing.T) {
	h, pod := probeWorld(t, false)
	ev := testutil.WarningEvent(pod, "probe", "Unhealthy", testutil.Now)
	ev.Count = 50
	h.putEvent(t, ev, true, 0, true)
	if f := findingOf(h.at(t, testutil.Now), health.CodeProbeFailing); f != nil {
		t.Fatalf("a first-seen count raised 020: %+v", f)
	}
	ev.Count, ev.LastTimestamp = 53, metav1.NewTime(testutil.Now.Add(time.Second))
	h.clk.set(testutil.Now.Add(time.Second))
	h.putEvent(t, ev, false, 3, true)
	f := findingOf(h.at(t, testutil.Now.Add(time.Second)), health.CodeProbeFailing)
	if f == nil || processFacts(t, f).Probe.Failures != 3 || !strings.HasSuffix(f.Detail, " Earlier failures were not counted.") {
		t.Fatalf("020 %+v", f)
	}
}

// A Pod with readable Events but no counted probe failure has no probe in /processes,
// so no row reads "failed 0 times"; a counted failure shows it.
func TestProcessesProbeOnlyWithFailures(t *testing.T) {
	h, pod := probeWorld(t, false)
	ctrlProbe := func() *health.ProbeFacts {
		t.Helper()
		p := get[ProcessesResponse](t, h.srv, procsPath, 200)
		if len(p.Controller.Processes) != 1 {
			t.Fatalf("controller processes %+v", p.Controller.Processes)
		}
		for _, g := range p.Proxies.Items {
			for _, x := range g.Set.Processes {
				if x.Probe != nil {
					t.Fatalf("proxy %s probe %+v without failures", x.Name, x.Probe)
				}
			}
		}
		return p.Controller.Processes[0].Probe
	}
	h.at(t, testutil.Now)
	if pr := ctrlProbe(); pr != nil {
		t.Fatalf("no Events: probe %+v, want none", pr)
	}
	// First seen with count 50: earlier failures are unknown and none is counted.
	ev := testutil.WarningEvent(pod, "probe", "Unhealthy", testutil.Now)
	ev.Count = 50
	h.putEvent(t, ev, true, 0, true)
	h.at(t, testutil.Now)
	if pr := ctrlProbe(); pr != nil {
		t.Fatalf("no counted failure: probe %+v, want none", pr)
	}
	last := testutil.Now.Add(time.Second)
	ev.Count, ev.LastTimestamp = 51, metav1.NewTime(last)
	h.clk.set(last)
	h.putEvent(t, ev, false, 1, true)
	h.at(t, last)
	if pr := ctrlProbe(); pr == nil || pr.Failures != 1 || !pr.Last.Equal(last) {
		t.Fatalf("one counted failure: probe %+v", pr)
	}
}

// Review Focus 5: an aggregated Event's count jump is observed failures in a
// 15-minute window, never a total. Once its observations leave the window
// 020 clears, although the Event still reads count 10.
func TestAggregatedEventCountIsAWindowNotATotal(t *testing.T) {
	h, pod := probeWorld(t, false)
	t0 := testutil.Now
	ev := testutil.WarningEvent(pod, "probe", "Unhealthy", t0)
	h.putEvent(t, ev, true, 1, false)
	t1 := t0.Add(time.Minute)
	h.clk.set(t1)
	ev.Count, ev.LastTimestamp = 10, metav1.NewTime(t1)
	h.putEvent(t, ev, false, 10, false)
	for _, tc := range []struct {
		at   time.Time
		want int
	}{{t1, 10}, {t0.Add(cache.EventWindow + time.Second), 9}, {t1.Add(cache.EventWindow + time.Second), 0}} {
		f := findingOf(h.at(t, tc.at), health.CodeProbeFailing)
		got := 0
		if f != nil {
			got = processFacts(t, f).Probe.Failures
			if !strings.Contains(f.Detail, "in the last 15 minutes") {
				t.Fatalf("detail %q claims more than the window", f.Detail)
			}
		}
		if got != tc.want {
			t.Fatalf("at %s: %d failures, want %d", tc.at.Sub(t0), got, tc.want)
		}
	}
}

// Events off: probe failures are not available (off) and the install state is
// what it is without the check.
func TestEventsOffProbeNotAvailableAtTheAPI(t *testing.T) {
	r := get[HealthResponse](t, startHarnessSrv(t), healthPath, 200)
	c := checkOf(r, "probe failures")
	if c == nil || c.OK || c.Required || c.Unavailable != health.AvailOff || r.State == health.StateUnknown || hasFinding(r, health.CodeSignalUnavailable, "") {
		t.Fatalf("state %s, check %+v", r.State, c)
	}
}

// §14 rules 12 and 16: a caller who cannot view the Pod gets 019 with the
// restricted detail and no evidence; one who cannot view the Deployment gets
// the restricted row. The id is the same for every caller (D-040).
func TestProcessEvidenceRestrictedAtTheAPI(t *testing.T) {
	restart := testutil.Now.Add(-time.Minute)
	h := newHarness(t, setup{}, procWorld(container(corev1.ContainerStatus{RestartCount: 1, State: runningSince(restart),
		LastTerminationState: exitedAt("OOMKilled", 137, restart)}), nil)...)
	full := findingOf(h.at(t, testutil.Now), health.CodeProcessRestarting)
	if full == nil || full.Facts == nil {
		t.Fatalf("019 %+v", full)
	}
	for _, tc := range []struct {
		name string
		deny func(objects.Ref) bool
	}{{"pod hidden", func(r objects.Ref) bool { return r.Kind == "Pod" }}, {"deployment hidden", func(r objects.Ref) bool { return r.Kind == "Deployment" }}} {
		srv := httptest.NewServer(NewHandler(h.e, fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}},
			AuthorizerFunc(func(_ *http.Request, r objects.Ref) bool { return !tc.deny(r) })))
		t.Cleanup(srv.Close)
		f := findingOf(get[HealthResponse](t, srv, healthPath, 200), health.CodeProcessRestarting)
		if f == nil || f.ID != full.ID || f.Facts != nil || strings.Contains(string(wire(t, srv, healthPath)), `"evidence"`) {
			t.Fatalf("%s: %+v", tc.name, f)
		}
		if tc.name == "pod hidden" && (!f.EvidenceRestricted || f.Detail != restrictedEvidence || f.Message != full.Message) {
			t.Fatalf("pod hidden: %+v", f)
		}
		if tc.name == "deployment hidden" && (!f.Restricted || f.Message != "" || f.Detail != "") {
			t.Fatalf("deployment hidden: %+v", f)
		}
	}
}

// R42: with the proxy Pod watch off or denied, the restart and probe results
// name the gap and its reason; the controller's 019 still shows.
func TestProxyPodsUnwatchedGapAtTheAPI(t *testing.T) {
	restart := testutil.Now.Add(-time.Minute)
	objs := func() []runtime.Object {
		return procWorld(container(corev1.ContainerStatus{RestartCount: 1, State: runningSince(restart), LastTerminationState: exitedAt("Error", 1, restart)}), edgeProxy(1, 1))
	}
	proxySel := install.UpstreamDetector{}.ProxyPodSelector().String()
	denyProxyPods := func(_ *dynamicfake.FakeDynamicClient, kube *kubefake.Clientset) {
		kube.PrependReactor("list", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
			if l, ok := a.(clienttesting.ListAction); ok && l.GetListRestrictions().Labels.String() == proxySel {
				return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", nil)
			}
			return false, nil, nil
		})
	}
	for _, tc := range []struct {
		name string
		s    setup
		why  health.Availability
	}{{"off", setup{optional: true}, health.AvailOff}, {"denied", setup{optional: true, proxyPods: true, prepare: denyProxyPods}, health.AvailDenied}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.s, objs()...)
			h.awaitCache(t, "Events synced", func(s *cache.Snapshot) bool { return s.Access[cache.EventsGVR].OK })
			if tc.why == health.AvailDenied {
				h.awaitCache(t, "denied proxy Pods", func(s *cache.Snapshot) bool { return s.Access[cache.ProxyPodsGVR].Err != "" })
			}
			r := h.at(t, testutil.Now)
			for _, name := range []string{"process restarts", "probe failures"} {
				c := checkOf(r, name)
				if c == nil || !c.OK || c.Detail != "Proxy Pods are not watched, so proxies were not checked" || c.Unavailable != tc.why {
					t.Fatalf("%s: %+v", name, c)
				}
			}
			if f := findingOf(r, health.CodeProcessRestarting); f == nil || f.Resource.Kind != "Deployment" || r.State == health.StateUnknown {
				t.Fatalf("controller 019 %+v, state %s", f, r.State)
			}
		})
	}
}
