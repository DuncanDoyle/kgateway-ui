package install

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
)

func TestDiscoverUpstreamInstall(t *testing.T) {
	d := testutil.ControllerDeployment("kgateway-system", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 2, 2)
	p1, p2 := testutil.ControllerPod(d, "kgateway-a", true), testutil.ControllerPod(d, "kgateway-b", true)
	mirrored := testutil.ControllerDeployment("kgw-b", "kgw-b", "registry.local:5000/mirror/kgateway@sha256:abc", 1, 1)
	unknown := testutil.ControllerDeployment("x", "other", "example.com/another-controller:1.0", 1, 1)
	snap := &cache.Snapshot{Deployments: []*appsv1.Deployment{unknown, mirrored, d}, Pods: testutil.PodList(p1, p2)}

	got, _ := Discover(snap, []Detector{UpstreamDetector{}})
	if len(got) != 2 {
		t.Fatalf("want 2 installs (upstream image and its mirror), got %d", len(got))
	}
	in := got[0] // sorted by ID: kgateway-system/kgateway < kgw-b/kgw-b
	if in.ID() != "kgateway-system.kgateway" || in.Version != "2.4.7" || in.Edition != "kgateway" ||
		in.MetricsPort != 9092 || len(in.Pods) != 2 || in.ControllerNames[0] != "kgateway.dev/kgateway" {
		t.Fatalf("unexpected install %+v", in)
	}
}

func TestDetectorOrderLetsDownstreamWin(t *testing.T) {
	d := testutil.ControllerDeployment("ns", "c", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	got, _ := Discover(&cache.Snapshot{Deployments: []*appsv1.Deployment{d}}, []Detector{fixedDetector{}, UpstreamDetector{}})
	if len(got) != 1 || got[0].Edition != "fixed" {
		t.Fatalf("first matching detector must win, got %+v", got)
	}
}

// Discover fills the Deployment and ProxySelector a detector leaves unset.
func TestDiscoverBackfillsDeploymentAndProxySelector(t *testing.T) {
	d := testutil.ControllerDeployment("ns", "c", "example.com/other:1.0", 1, 1)
	got, _ := Discover(&cache.Snapshot{Deployments: []*appsv1.Deployment{d}}, []Detector{bareDetector{}})
	if len(got) != 1 || got[0].Deployment != d || got[0].ProxySelector == nil ||
		got[0].ProxySelector.String() != (UpstreamDetector{}).ProxySelector().String() {
		t.Fatalf("Deployment and ProxySelector must be filled from the detector: %+v", got)
	}
}

// bareDetector claims every upstream-labelled Deployment and sets only its identity.
type bareDetector struct{ UpstreamDetector }

func (bareDetector) Detect(d *appsv1.Deployment) (Install, bool) {
	return Install{Namespace: d.Namespace, Name: d.Name}, true
}

type fixedDetector struct{ UpstreamDetector }

func (fixedDetector) Detect(d *appsv1.Deployment) (Install, bool) {
	in, ok := UpstreamDetector{}.Detect(d)
	in.Edition = "fixed"
	return in, ok
}

// The Pod watch must match the upstream Pod labels of every supported chart
// (v2.3+ adds app.kubernetes.io/component=controller to Pods; older charts and
// custom templates do not), and Pods are attributed through the Deployment's selector.
func TestControllerPodSelectorMatchesUpstreamPods(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 2, 2)
	current := testutil.ControllerPod(d, "kgateway-a", true)
	older := testutil.ControllerPod(d, "kgateway-b", true)
	delete(older.Labels, "app.kubernetes.io/component")
	det := UpstreamDetector{}
	for _, p := range []*corev1.Pod{current, older} {
		if !det.ControllerPodSelector().Matches(labels.Set(p.Labels)) {
			t.Fatalf("pod %s with labels %v must be watched", p.Name, p.Labels)
		}
	}
	if det.ControllerSelector().Matches(labels.Set(older.Labels)) {
		t.Fatal("the Deployment selector requires the component label; it must not be used for Pods")
	}
	other := testutil.ControllerPod(testutil.ControllerDeployment("kgw", "other", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1), "other-a", true)
	got, _ := Discover(&cache.Snapshot{Deployments: []*appsv1.Deployment{d}, Pods: testutil.PodList(current, older, other)}, []Detector{det})
	if len(got) != 1 || len(got[0].Pods) != 2 {
		t.Fatalf("both pods of kgw/kgateway, and only those, must be attributed to it: %+v", got)
	}
}
