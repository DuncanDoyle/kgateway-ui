package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// liveMetrics serves per-install metrics collected at the engine clock, or an
// Err; none until xdsHarness has started.
type liveMetrics struct {
	mu   sync.Mutex
	m    map[string]ctrlmetrics.Metrics
	errs map[string]string
	now  func() time.Time
}

func newLiveMetrics() *liveMetrics {
	return &liveMetrics{m: map[string]ctrlmetrics.Metrics{}, errs: map[string]string{}}
}

func (l *liveMetrics) set(id string, ms ...ctrlmetrics.Metrics) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m[id], l.errs[id] = ctrlmetrics.Aggregate(ms), ""
}

func (l *liveMetrics) fail(id, err string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errs[id] = err
}

// Get implements ctrlmetrics.Source.
func (l *liveMetrics) Get(id string) (ctrlmetrics.Result, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	m, ok := l.m[id]
	if l.now == nil {
		return ctrlmetrics.Result{}, false // not collected while the harness starts
	}
	return ctrlmetrics.Result{Metrics: m, CollectedAt: l.now(), Err: l.errs[id]}, ok
}

// ctrlCapture parses a Task 20 controller capture as controller Pod name in ns.
func ctrlCapture(t *testing.T, file, ns, name string) ctrlmetrics.Metrics {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "ctrlmetrics", "testdata", file+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	m, err := ctrlmetrics.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	m.PerPod[0].UID, m.PerPod[0].Name = "pod/"+ns+"/"+name, name
	return m
}

// withoutRejects is m with every active reject at zero.
func withoutRejects(m ctrlmetrics.Metrics) ctrlmetrics.Metrics {
	m.Rejects = map[ctrlmetrics.GatewayKey]float64{}
	pods := make([]ctrlmetrics.PodMetrics, len(m.PerPod))
	for i, p := range m.PerPod {
		p.Active = map[ctrlmetrics.TypeKey]float64{}
		pods[i] = p
	}
	m.PerPod = pods
	return m
}

// nackWorld is isolated() with a second controller replica kgateway-1 and the
// Gateway infra/nack (status of an older generation) with a 2/2 proxy.
func nackWorld(extra ...runtime.Object) []runtime.Object {
	d := testutil.ControllerDeployment("kgw", "kgateway", img, 2, 2, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"a"}}]`))
	return isolated(append([]runtime.Object{testutil.ControllerPod(d, "kgateway-1", true), stale(testutil.Gateway("infra", "nack", "kgateway", nil)),
		testutil.ProxyDeployment("infra", "nack", 2, 2)}, extra...)...)
}

// xdsHarness starts nackWorld with lm; the metrics are collected at the engine clock.
func xdsHarness(t *testing.T, s setup, lm *liveMetrics, objs ...runtime.Object) *harness {
	t.Helper()
	s.metrics = lm
	h := newHarness(t, s, objs...)
	lm.mu.Lock()
	lm.now = h.clk.now
	lm.mu.Unlock()
	return h
}

func xdsFinding(h HealthResponse, ns, name string) *FindingDTO {
	for i := range h.Findings {
		if f := &h.Findings[i]; f.Code == health.CodeXDSRejected && f.Resource != nil && f.Resource.Ref == (objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: ns, Name: name}) {
			return f
		}
	}
	return nil
}

// Two controller replicas each serve one nack proxy (Task 20 captures): one
// 001 per Gateway with per-type streams summed and the reporting replicas;
// edge's current Programmed status gets the status-says-healthy message.
// A caller who may not view the controller Pods gets no evidence.
func TestXDSRejectsAtTheAPI(t *testing.T) {
	for _, v := range []string{"v2.3.11", "v2.4.7"} {
		t.Run(v, func(t *testing.T) {
			lm := newLiveMetrics()
			lm.set("kgw.kgateway", ctrlCapture(t, v+"-rejects-a", "kgw", "kgateway-0"), ctrlCapture(t, v+"-rejects-b", "kgw", "kgateway-1"))
			h := xdsHarness(t, setup{}, lm, nackWorld()...)
			r := h.at(t, testutil.Now)
			nack, edge := xdsFinding(r, "infra", "nack"), xdsFinding(r, "infra", "edge")
			if r.State != health.StateError || nack == nil || edge == nil {
				t.Fatalf("want 001 on nack and edge: %s %+v", r.State, r.Findings)
			}
			if nack.Message != "Proxies reject this Gateway's config" || edge.Message != "Status says Programmed, but proxies reject its config" {
				t.Fatalf("messages %q / %q", nack.Message, edge.Message)
			}
			ev, ok := nack.Facts.(*health.XDSRejectsEvidence)
			if !ok || len(ev.Types) != 2 || ev.StatusSaysHealthy || ev.Proxies == nil || *ev.Proxies != (health.ProxyCount{Ready: 2, Desired: 2}) {
				t.Fatalf("nack evidence %+v", nack.Facts)
			}
			for _, ty := range ev.Types {
				if ty.Streams != 2 || strings.Join(ty.ReportedBy, ",") != "kgateway-0,kgateway-1" || !ty.FirstSeen.Equal(testutil.Now) {
					t.Fatalf("nack type %+v", ty)
				}
			}
			if ev, ok := edge.Facts.(*health.XDSRejectsEvidence); !ok || !ev.StatusSaysHealthy || len(ev.Types) != 1 || ev.Types[0].Streams != 1 || strings.Join(ev.Types[0].ReportedBy, ",") != "kgateway-0" {
				t.Fatalf("edge evidence %+v", edge.Facts)
			}
			if body := string(wire(t, h.srv, healthPath)); !strings.Contains(body, `"kind": "xds-rejects"`) || !strings.Contains(body, `"typeUrl": "envoy.config.route.v3.RouteConfiguration"`) {
				t.Fatalf("body %s", body)
			}

			// The survivor of a replaced replica reports one more reject per nack type.
			lm.set("kgw.kgateway", ctrlCapture(t, v+"-restart", "kgw", "kgateway-1"))
			r = h.at(t, testutil.Now.Add(10*time.Second))
			for _, ty := range xdsFinding(r, "infra", "nack").Facts.(*health.XDSRejectsEvidence).Types {
				if ty.TotalSinceFirstSeen != 1 || ty.Streams != 2 || strings.Join(ty.ReportedBy, ",") != "kgateway-1" {
					t.Fatalf("nack type after the restart capture %+v", ty)
				}
			}
		})
	}
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", ctrlCapture(t, "v2.4.7-rejects-a", "kgw", "kgateway-0"), ctrlCapture(t, "v2.4.7-rejects-b", "kgw", "kgateway-1"))
	noPods := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Kind != "Pod" })
	h := xdsHarness(t, setup{authz: noPods}, lm, nackWorld()...)
	f := xdsFinding(h.at(t, testutil.Now), "infra", "nack")
	if f == nil || !f.EvidenceRestricted || f.Facts != nil || f.Detail != restrictedEvidence {
		t.Fatalf("hidden controller Pods: %+v", f)
	}
}

// Metrics with an Err are KGWUI-008 and no 001, whatever they held.
func TestXDSRejectsUnavailableOnErrAtTheAPI(t *testing.T) {
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", ctrlCapture(t, "v2.4.7-rejects-a", "kgw", "kgateway-0"))
	lm.fail("kgw.kgateway", "scrape kgateway-1: connection refused")
	h := xdsHarness(t, setup{}, lm, nackWorld()...)
	r := h.at(t, testutil.Now)
	if c := checkOf(r, "xDS rejects"); r.State != health.StateUnknown || c == nil || c.OK || !c.Required || xdsFinding(r, "infra", "nack") != nil ||
		findingOf(r, health.CodeSignalUnavailable) == nil {
		t.Fatalf("%s %+v %+v", r.State, c, r.Findings)
	}
}

// A replaced controller Pod with the rejects gone holds xDS rejects pending:
// the install is unknown at +59 s and not at +61 s. A reject back at +30 s
// keeps its since.
func TestRestartHoldAtTheAPI(t *testing.T) {
	a, b := ctrlCapture(t, "v2.4.7-rejects-a", "kgw", "kgateway-0"), ctrlCapture(t, "v2.4.7-rejects-b", "kgw", "kgateway-1")
	for _, back := range []bool{false, true} {
		lm := newLiveMetrics()
		lm.set("kgw.kgateway", a, b)
		h := xdsHarness(t, setup{}, lm, nackWorld()...)
		first := xdsFinding(h.at(t, testutil.Now), "infra", "nack")
		h.replaceCtrlPod(t, "kgateway-1", "kgateway-2")
		lm.set("kgw.kgateway", withoutRejects(a), withoutRejects(ctrlCapture(t, "v2.4.7-rejects-b", "kgw", "kgateway-2")))
		t1 := testutil.Now.Add(10 * time.Second)
		if r := h.at(t, t1); r.State != health.StateUnknown || !checkOf(r, "xDS rejects").Pending || xdsFinding(r, "infra", "nack") != nil {
			t.Fatalf("hold: %s %+v %+v", r.State, checkOf(r, "xDS rejects"), r.Findings)
		}
		if back {
			lm.set("kgw.kgateway", a, ctrlCapture(t, "v2.4.7-rejects-b", "kgw", "kgateway-2"))
			f := xdsFinding(h.at(t, t1.Add(30*time.Second)), "infra", "nack")
			if f == nil || f.ID != first.ID || f.Since == nil || !f.Since.Equal(testutil.Now) {
				t.Fatalf("returned 001 %+v, want since %v", f, testutil.Now)
			}
			continue
		}
		if r := h.at(t, t1.Add(59*time.Second)); r.State != health.StateUnknown || !checkOf(r, "xDS rejects").Pending {
			t.Fatalf("at +59 s: %s %+v", r.State, checkOf(r, "xDS rejects"))
		}
		// nack's stale status keeps a 003 warning.
		if r := h.at(t, t1.Add(61*time.Second)); r.State != health.StateWarning || !checkOf(r, "xDS rejects").OK {
			t.Fatalf("at +61 s: %s %+v %+v", r.State, checkOf(r, "xDS rejects"), r.Findings)
		}
	}
}

// replaceCtrlPod deletes controller Pod old and creates fresh in its place.
func (h *harness) replaceCtrlPod(t *testing.T, old, fresh string) {
	t.Helper()
	p, err := h.kube.CoreV1().Pods("kgw").Get(h.ctx, old, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.kube.CoreV1().Pods("kgw").Delete(h.ctx, old, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	n := p.DeepCopy()
	n.Name, n.UID, n.ResourceVersion = fresh, types.UID("pod/kgw/"+fresh), ""
	if _, err := h.kube.CoreV1().Pods("kgw").Create(h.ctx, n, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	h.awaitCache(t, "pod "+fresh, func(s *cache.Snapshot) bool {
		seen := false
		for _, q := range s.Pods {
			if q.Name == old {
				return false
			}
			seen = seen || q.UID == n.UID
		}
		return seen
	})
}

// A second install's Gateway that stops rejecting while its controller Pod is
// replaced holds only that install; the first stays healthy.
func TestRestartHoldPerInstall(t *testing.T) {
	d2 := testutil.ControllerDeployment("kgw2", "kgateway2", img, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"b"}}]`))
	objs := isolated(testutil.Namespace("kgw2", nil), testutil.Namespace("infra-b", map[string]string{"team": "b"}), d2,
		testutil.ControllerPod(d2, "kgateway2-0", true), testutil.Gateway("infra-b", "edge", "kgateway", nil), testutil.ProxyDeployment("infra-b", "edge", 1, 1))
	rejectB := func(pod string, n float64) ctrlmetrics.Metrics {
		k := ctrlmetrics.GatewayKey{Namespace: "infra-b", Name: "edge"}
		tk := ctrlmetrics.TypeKey{Gateway: k, TypeURL: "envoy.config.cluster.v3.Cluster"}
		return ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{k: n}, Managed: map[ctrlmetrics.ManagedKey]float64{}, ManagedPresent: true,
			PerPod: []ctrlmetrics.PodMetrics{{UID: "pod/kgw2/" + pod, Name: pod, Active: map[ctrlmetrics.TypeKey]float64{tk: n}, Total: map[ctrlmetrics.TypeKey]float64{tk: 1}}}}
	}
	clean := ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{}, Managed: map[ctrlmetrics.ManagedKey]float64{}, ManagedPresent: true,
		PerPod: []ctrlmetrics.PodMetrics{{UID: "pod/kgw/kgateway-0", Name: "kgateway-0", Active: map[ctrlmetrics.TypeKey]float64{}, Total: map[ctrlmetrics.TypeKey]float64{}}}}
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", clean)
	lm.set("kgw2.kgateway2", rejectB("kgateway2-0", 1))
	h := xdsHarness(t, setup{}, lm, objs...)
	path2 := "/api/v1/installs/kgw2.kgateway2/health"
	if r := h.at(t, testutil.Now); r.State != health.StateHealthy {
		t.Fatalf("first install: %s %+v", r.State, r.Findings)
	}
	if r := get[HealthResponse](t, h.srv, path2, 200); xdsFinding(r, "infra-b", "edge") == nil {
		t.Fatalf("second install: %+v", r.Findings)
	}
	p, err := h.kube.CoreV1().Pods("kgw2").Get(h.ctx, "kgateway2-0", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.kube.CoreV1().Pods("kgw2").Delete(h.ctx, p.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	n := p.DeepCopy()
	n.Name, n.UID, n.ResourceVersion = "kgateway2-1", "pod/kgw2/kgateway2-1", ""
	if _, err := h.kube.CoreV1().Pods("kgw2").Create(h.ctx, n, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	h.awaitCache(t, "pod kgateway2-1", func(s *cache.Snapshot) bool {
		return containsPod(s.Pods, n.UID) && !containsPodNamed(s.Pods, "kgateway2-0")
	})
	lm.set("kgw2.kgateway2", rejectB("kgateway2-1", 0))
	t1 := testutil.Now.Add(10 * time.Second)
	if r := h.at(t, t1); r.State != health.StateHealthy || !checkOf(r, "xDS rejects").OK {
		t.Fatalf("first install held by the second: %s %+v", r.State, checkOf(r, "xDS rejects"))
	}
	if r := get[HealthResponse](t, h.srv, path2, 200); r.State != health.StateUnknown || !checkOf(r, "xDS rejects").Pending {
		t.Fatalf("second install: %s %+v", r.State, checkOf(r, "xDS rejects"))
	}
}

func containsPod(ps []*corev1.Pod, uid types.UID) bool {
	for _, p := range ps {
		if p.UID == uid {
			return true
		}
	}
	return false
}

func containsPodNamed(ps []*corev1.Pod, name string) bool {
	for _, p := range ps {
		if p.Name == name {
			return true
		}
	}
	return false
}
