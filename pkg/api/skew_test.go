package api

import (
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	metadatafake "k8s.io/client-go/metadata/fake"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
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

