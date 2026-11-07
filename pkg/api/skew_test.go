package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	metadatafake "k8s.io/client-go/metadata/fake"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// skewWorld is world with the edge proxy Deployment, one proxy Pod on proxyTag,
// and the controller rollout settled at settled.
func skewWorld(proxyTag string, settled time.Time) []runtime.Object {
	d := edgeProxy(1, 1)
	p := edgePod(d, "edge-0", true, corev1.ContainerStatus{State: runningSince(testutil.Old.Time)})
	p.Spec.Containers[0].Image = "cr.kgateway.dev/kgateway-dev/envoy-wrapper:" + proxyTag
	objs := procWorld(nil, d, p)
	for _, o := range objs {
		if c, ok := o.(*appsv1.Deployment); ok && c.Name == "kgateway" {
			c.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue,
				Reason: "NewReplicaSetAvailable", LastUpdateTime: metav1.NewTime(settled)}}
		}
	}
	return objs
}

// withCRDs creates the CRD metadata before the cache starts.
func withCRDs(t *testing.T, crds ...*metav1.PartialObjectMetadata) func(*metadatafake.FakeMetadataClient) {
	return func(m *metadatafake.FakeMetadataClient) {
		for _, c := range crds {
			if _, err := m.Resource(cache.CRDsGVR).(metadatafake.MetadataClient).CreateFake(c, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func skewFacts(t *testing.T, f *FindingDTO) *health.VersionSkewEvidence {
	t.Helper()
	ev, ok := f.Facts.(*health.VersionSkewEvidence)
	if !ok {
		t.Fatalf("evidence %T %+v, want version-skew evidence", f.Facts, f.Facts)
	}
	return ev
}

// Pods in, /health out: a proxy on 2.3 beside a 2.4.7 controller raises 022 on
// its Gateway with version-skew evidence; a patch difference raises nothing.
func TestVersionSkewAtTheAPI(t *testing.T) {
	h := newHarness(t, setup{proxyPods: true}, skewWorld("v2.3.6", testutil.Old.Time)...)
	r := h.at(t, testutil.Now)
	f := findingOf(r, health.CodeVersionSkew)
	if f == nil || f.Severity != health.SeverityWarning || f.Resource == nil || f.Resource.Ref != (objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}) {
		t.Fatalf("want 022 on infra/edge, got %+v", r.Findings)
	}
	if ev := skewFacts(t, f); ev.Controller != "2.4.7" || len(ev.Proxies) != 1 || ev.Proxies[0].Version != "2.3.6" || ev.Proxies[0].Pods[0] != "edge-0" {
		t.Fatalf("evidence %+v", ev)
	}
	if body := string(wire(t, h.srv, healthPath)); !strings.Contains(body, `"kind": "version-skew"`) {
		t.Fatalf("body %s", body)
	}
	patch := newHarness(t, setup{proxyPods: true}, skewWorld("v2.4.5", testutil.Old.Time)...)
	if f := findingOf(patch.at(t, testutil.Now), health.CodeVersionSkew); f != nil {
		t.Fatalf("patch skew raised %+v", f)
	}
}

// A controller rollout that settled at Now suppresses 022 until 10 min later.
func TestVersionSkewRolloutAtTheAPI(t *testing.T) {
	h := newHarness(t, setup{proxyPods: true}, skewWorld("v2.3.6", testutil.Now)...)
	if f := findingOf(h.at(t, testutil.Now.Add(9*time.Minute+59*time.Second)), health.CodeVersionSkew); f != nil {
		t.Fatalf("022 at 9:59: %+v", f)
	}
	if f := findingOf(h.at(t, testutil.Now.Add(10*time.Minute+time.Second)), health.CodeVersionSkew); f == nil {
		t.Fatal("no 022 at 10:01")
	}
}

// Gateway API CRDs on another bundle than the 2.4 controller expects raise 022
// on the Gateway CRD; with the CRD watch off, the check says not available.
func TestCRDSkewAtTheAPI(t *testing.T) {
	crds := []*metav1.PartialObjectMetadata{testutil.CRDMetadata("gateways.gateway.networking.k8s.io", "v1.5.1", "standard"),
		testutil.CRDMetadata("httproutes.gateway.networking.k8s.io", "v1.5.1", "standard")}
	h := newHarness(t, setup{optional: true, prepareMeta: withCRDs(t, crds...)}, world()...)
	h.awaitCache(t, "CRDs", func(s *cache.Snapshot) bool { return len(s.CRDs) == 2 })
	r := h.at(t, testutil.Now)
	f := findingOf(r, health.CodeVersionSkew)
	if f == nil || f.Resource == nil || f.Resource.Kind != "CustomResourceDefinition" || f.Resource.Name != "gateways.gateway.networking.k8s.io" {
		t.Fatalf("want 022 on the Gateway CRD, got %+v", r.Findings)
	}
	if ev := skewFacts(t, f); ev.Expected != "v1.6" || len(ev.CRDs) != 1 || ev.CRDs[0].BundleVersion != "v1.5.1" {
		t.Fatalf("evidence %+v", ev)
	}
	off := newHarness(t, setup{}, world()...).at(t, testutil.Now)
	c := checkOf(off, "version skew")
	if c == nil || c.Unavailable != health.AvailOff || !strings.Contains(c.Detail, "Gateway API CRDs are not watched") || hasFinding(off, health.CodeSignalUnavailable, "") {
		t.Fatalf("CRDs off: %+v", c)
	}
}

// §9.4: the Gateway CRD's channel and bundle say whether an absent
// allowedListeners is missing from the CRD or unset.
func TestListenerSetHintNamesTheChannel(t *testing.T) {
	notAllowed := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowed")}
	for _, tc := range []struct {
		name, bundle, channel string
		field                 string
	}{{"old standard", "v1.4.1", "standard", "missing"}, {"standard with the field", "v1.5.1", "standard", "defined"}, {"experimental", "v1.4.1", "experimental", "defined"}} {
		t.Run(tc.name, func(t *testing.T) {
			ls := lsWith("na", "ListenerSet", edgeRef, notAllowed, testutil.Listener{Name: "extra"})
			h := newHarness(t, setup{optional: true, prepareMeta: withCRDs(t, testutil.CRDMetadata("gateways.gateway.networking.k8s.io", tc.bundle, tc.channel),
				testutil.CRDMetadata("listenersets.gateway.networking.k8s.io", tc.bundle, tc.channel))}, world(ls)...)
			h.awaitCache(t, "CRDs", func(s *cache.Snapshot) bool { return len(s.CRDs) == 2 })
			h.e.Compute()
			got := get[ListenerSetDetail](t, h.srv, lsDetail+"infra/na", 200).ParentAllowedListeners
			if got == nil || !got.CRDHint || got.CRDField != tc.field || got.CRDChannel != tc.channel || got.CRDBundleVersion != tc.bundle {
				t.Fatalf("parentAllowedListeners %+v", got)
			}
		})
	}
	ls := lsWith("na", "ListenerSet", edgeRef, notAllowed, testutil.Listener{Name: "extra"})
	_, srv, _ := startWith(t, setup{}, world(ls)...)
	if got := get[ListenerSetDetail](t, srv, lsDetail+"infra/na", 200).ParentAllowedListeners; got == nil || got.CRDField != "" || !got.CRDHint {
		t.Fatalf("CRDs off: %+v", got)
	}
}

// proxyAt is kgateway's proxy Deployment for ns/edge with one Pod on tag.
func proxyAt(ns, tag string) (*appsv1.Deployment, *corev1.Pod) {
	d := testutil.ProxyDeployment(ns, "edge", 1, 1)
	sel := map[string]string{"app.kubernetes.io/name": "edge", "gateway.networking.k8s.io/gateway-name": "edge"}
	d.Spec.Selector = &metav1.LabelSelector{MatchLabels: sel}
	d.Spec.Template.Labels = map[string]string{"app.kubernetes.io/name": "edge", "gateway.networking.k8s.io/gateway-name": "edge",
		"kgateway": "kube-gateway", "app.kubernetes.io/component": "proxy"}
	p := edgePod(d, "edge-0", true, corev1.ContainerStatus{State: runningSince(testutil.Old.Time)})
	p.Spec.Containers[0].Image = "cr.kgateway.dev/kgateway-dev/envoy-wrapper:" + tag
	p.UID = types.UID("pod/" + ns + "/edge-0")
	return d, p
}

// I2: on shared v1.5.1 CRDs, only the 2.4 install gets the CRD 022; install
// B's proxy skew stays on B.
func TestVersionSkewPerInstall(t *testing.T) {
	const imgB = "cr.kgateway.dev/kgateway-dev/kgateway:v2.3.11"
	dB := testutil.ControllerDeployment("kgw2", "kgateway2", imgB, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"b"}}]`))
	dB.Labels["app.kubernetes.io/version"] = "2.3.11"
	proxyA, podA := proxyAt("infra", "v2.4.7")
	proxyB, podB := proxyAt("infra-b", "v2.4.7")
	objs := procWorld(nil, proxyA, podA)
	objs = append(objs, testutil.Namespace("kgw2", nil), testutil.Namespace("infra-b", map[string]string{"team": "b"}), dB,
		testutil.ControllerPod(dB, "kgateway2-0", true), edgeIn("infra-b", 0), proxyB, podB)
	crds := []*metav1.PartialObjectMetadata{testutil.CRDMetadata("gateways.gateway.networking.k8s.io", "v1.5.1", "standard"),
		testutil.CRDMetadata("httproutes.gateway.networking.k8s.io", "v1.5.1", "standard")}
	h := newHarness(t, setup{optional: true, proxyPods: true, prepareMeta: withCRDs(t, crds...), metrics: okMetrics("kgw.kgateway", "kgw2.kgateway2")}, objs...)
	h.awaitCache(t, "CRDs", func(s *cache.Snapshot) bool { return len(s.CRDs) == 2 })
	h.clk.set(testutil.Now)
	h.e.Compute()
	a := get[HealthResponse](t, h.srv, healthPath, 200)
	b := get[HealthResponse](t, h.srv, "/api/v1/installs/kgw2.kgateway2/health", 200)
	skews := func(r HealthResponse) []string {
		var out []string
		for _, f := range r.Findings {
			if f.Code == health.CodeVersionSkew {
				out = append(out, f.Resource.Kind+" "+f.Resource.String())
			}
		}
		return out
	}
	if got := strings.Join(skews(a), ","); got != "CustomResourceDefinition gateways.gateway.networking.k8s.io" {
		t.Fatalf("install A 022: %s", got)
	}
	if got := strings.Join(skews(b), ","); got != "Gateway infra-b/edge" {
		t.Fatalf("install B 022: %s", got)
	}
}

// §14 rule 16: a caller who cannot view the pinning GatewayParameters gets the
// restricted detail and no evidence, so the GatewayParameters name never shows.
func TestPinnedParamsRestrictedAtTheAPI(t *testing.T) {
	gp := testutil.GatewayParameters("infra", "pinned", false)
	gp.Object["spec"] = map[string]any{"kube": map[string]any{"envoyContainer": map[string]any{"image": map[string]any{"tag": "v2.3.6"}}}}
	objs := skewWorld("v2.3.6", testutil.Old.Time)
	for _, o := range objs {
		if u, ok := o.(*unstructured.Unstructured); ok && u.GetKind() == "Gateway" && u.GetName() == "edge" {
			testutil.SetGatewayParameters(u, gp.GroupVersionKind().Group, "GatewayParameters", "pinned")
		}
	}
	hide := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Kind != "GatewayParameters" })
	h := newHarness(t, setup{proxyPods: true, authz: hide}, append(objs, gp)...)
	f := findingOf(h.at(t, testutil.Now), health.CodeVersionSkew)
	if f == nil || !f.EvidenceRestricted || f.Facts != nil || strings.Contains(f.Detail, "pinned") {
		t.Fatalf("finding %+v", f)
	}
	if body := string(wire(t, h.srv, healthPath)); strings.Contains(body, "pinned") {
		t.Fatalf("the GatewayParameters name leaked: %s", body)
	}
	all := newHarness(t, setup{proxyPods: true}, append(skewWorldPinned(gp), gp)...)
	if f := findingOf(all.at(t, testutil.Now), health.CodeVersionSkew); f == nil || !strings.Contains(f.Detail, "GatewayParameters infra/pinned") {
		t.Fatalf("visible pin: %+v", f)
	}
}

// skewWorldPinned is skewWorld with edge pointing at gp.
func skewWorldPinned(gp *unstructured.Unstructured) []runtime.Object {
	objs := skewWorld("v2.3.6", testutil.Old.Time)
	for _, o := range objs {
		if u, ok := o.(*unstructured.Unstructured); ok && u.GetKind() == "Gateway" && u.GetName() == "edge" {
			testutil.SetGatewayParameters(u, gp.GroupVersionKind().Group, "GatewayParameters", gp.GetName())
		}
	}
	return objs
}
