package health

import (
	"slices"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/rulesets"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

const wrapper = "cr.kgateway.dev/kgateway-dev/envoy-wrapper"

// skewPod is a ready edge proxy Pod running image.
func skewPod(name, image string) *corev1.Pod {
	p := proxyPod(name, true, corev1.ContainerStatus{State: running(testutil.Old.Time)})
	p.Spec.Containers[0].Image = image
	return p
}

// skewInput is w's process Input with the edge proxy Pods, the Gateway API
// table and, when crds is not nil, the CRD watch on with those CRDs.
func skewInput(t *testing.T, w *world, crds []cache.CRDMeta, pods ...*corev1.Pod) Input {
	t.Helper()
	withProxy(w, int32(len(pods)), int32(len(pods)))
	in := processInput(t, w, pods...)
	in.GatewayAPI = rulesets.ExpectedGatewayAPI
	if crds != nil {
		in.Snapshot.CRDs, in.Snapshot.Access[cache.CRDsGVR] = crds, cache.Access{OK: true}
	}
	return in
}

func skewFindings(r Report) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Code == CodeVersionSkew {
			out = append(out, f)
		}
	}
	return out
}

func skewResult(t *testing.T, r Report) CheckResult {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == skewCheckName {
			return c
		}
	}
	t.Fatal("no version skew result")
	return CheckResult{}
}

// A proxy on another minor than the controller raises 022 on its Gateway; the
// evidence names the Pod, which the finding depends on (§14 rule 16).
func TestSkewMinor(t *testing.T) {
	r := Evaluate(skewInput(t, healthyWorld(), []cache.CRDMeta{}, skewPod("edge-0", wrapper+":v2.3.6")), Upstream(), Runtime{States: NewStates()})
	fs := skewFindings(r)
	if len(fs) != 1 || fs[0].Severity != SeverityWarning || fs[0].Resource == nil || fs[0].Resource.Kind != "Gateway" || fs[0].Resource.String() != "infra/edge" {
		t.Fatalf("want one 022 on infra/edge, got %+v", r.Findings)
	}
	f := fs[0]
	if f.Slug != "version-skew" || !strings.Contains(f.Detail, "Proxy 2.3.6, controller 2.4.7") {
		t.Fatalf("finding %+v", f)
	}
	ev, ok := f.Facts.(*VersionSkewEvidence)
	if !ok || ev.Controller != "2.4.7" || len(ev.Proxies) != 1 || ev.Proxies[0].Version != "2.3.6" || len(ev.Proxies[0].Pods) != 1 || ev.Proxies[0].Pods[0] != "edge-0" {
		t.Fatalf("evidence %+v", f.Facts)
	}
	if len(f.Evidence) != 1 || f.Evidence[0] != (objects.Ref{Kind: "Pod", Namespace: "infra", Name: "edge-0"}) {
		t.Fatalf("evidence refs %+v", f.Evidence)
	}
}

// A patch difference is evidence only: no finding.
func TestSkewPatchIsEvidenceOnly(t *testing.T) {
	r := Evaluate(skewInput(t, healthyWorld(), []cache.CRDMeta{}, skewPod("edge-0", wrapper+":v2.4.5")), Upstream(), Runtime{States: NewStates()})
	if fs := skewFindings(r); len(fs) != 0 {
		t.Fatalf("patch skew raised %+v", fs)
	}
}

// A digest-only proxy image is "version unknown": no finding, and the result says so.
func TestSkewDigestTag(t *testing.T) {
	r := Evaluate(skewInput(t, healthyWorld(), []cache.CRDMeta{}, skewPod("edge-0", wrapper+"@sha256:0123abcd")), Upstream(), Runtime{States: NewStates()})
	if fs := skewFindings(r); len(fs) != 0 {
		t.Fatalf("digest raised %+v", fs)
	}
	if res := skewResult(t, r); !strings.Contains(res.Detail, skewUnknownDetail) {
		t.Fatalf("result %+v", res)
	}
}

// Review Focus 4: skew compares parsed major.minor only; an unparseable tag
// is "version unknown" with no finding.
func TestSkewUnparseableAndPrereleaseTags(t *testing.T) {
	for _, tc := range []struct {
		name, ctrl, proxy string
		skew, unknown     bool
	}{
		{"rc vs GA", "v2.5.0", "v2.5.0-rc.1", false, false},
		{"custom suffix parses as 2.4", "v2.4.7", "v2.4.7-acme.1", false, false},
		{"custom suffix against 2.5", "v2.5.0", "v2.4.7-acme.1", true, false},
		{"latest", "v2.4.7", "latest", false, true},
		{"digest", "v2.4.7", "@sha256:0123abcd", false, true},
		{"controller on a digest uses the Deployment version", "@sha256:0123abcd", "v2.3.6", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := healthyWorld()
			img := "cr.kgateway.dev/kgateway-dev/kgateway:" + strings.TrimPrefix(tc.ctrl, "@")
			if strings.HasPrefix(tc.ctrl, "@") {
				img = "cr.kgateway.dev/kgateway-dev/kgateway" + tc.ctrl
			}
			w.ctrl.Spec.Template.Spec.Containers[0].Image = img
			w.pods[0] = testutil.ControllerPod(w.ctrl, "p", true)
			proxy := wrapper + ":" + tc.proxy
			if strings.HasPrefix(tc.proxy, "@") {
				proxy = wrapper + tc.proxy
			}
			r := Evaluate(skewInput(t, w, []cache.CRDMeta{}, skewPod("edge-0", proxy)), Upstream(), Runtime{States: NewStates()})
			if got := len(skewFindings(r)) > 0; got != tc.skew {
				t.Fatalf("skew %v, want %v: %+v", got, tc.skew, r.Findings)
			}
			if got := strings.Contains(skewResult(t, r).Detail, skewUnknownDetail); got != tc.unknown {
				t.Fatalf("unknown note %v, want %v: %q", got, tc.unknown, skewResult(t, r).Detail)
			}
		})
	}
	for v, want := range map[string]string{"2.4.7": "2.4", "2.5.0-rc.1": "2.5", "2.4.7-acme.1": "2.4", "2.4": "2.4", "latest": "", "": "", "v2.4.7": "2.4", "2.4.x": ""} {
		if got := versionMinor(v); got != want {
			t.Errorf("versionMinor(%q) = %q, want %q", v, got, want)
		}
	}
}

// settledAt marks d's rollout finished at at.
func settledAt(d *appsv1.Deployment, at time.Time) {
	d.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue,
		Reason: "NewReplicaSetAvailable", LastUpdateTime: metav1.NewTime(at)}}
}

// 022 is suppressed while the controller rolls out and 10 min after: none at
// 9:59, present at 10:01, and the report wakes at the end of the window.
func TestSkewRolloutSuppression(t *testing.T) {
	w := healthyWorld()
	st := NewStates()
	pod := skewPod("edge-0", wrapper+":v2.3.6")
	w.ctrl.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue, Reason: "ReplicaSetUpdated"}}
	r := Evaluate(skewInput(t, w, []cache.CRDMeta{}, pod), Upstream(), Runtime{States: st})
	if fs := skewFindings(r); len(fs) != 0 || !strings.Contains(skewResult(t, r).Detail, skewSuppressedDetail) {
		t.Fatalf("during the rollout: %+v, %+v", fs, skewResult(t, r))
	}
	done := testutil.Now
	settledAt(w.ctrl, done)
	for _, tc := range []struct {
		d    time.Duration
		want bool
	}{{9*time.Minute + 59*time.Second, false}, {10*time.Minute + time.Second, true}} {
		in := skewInput(t, w, []cache.CRDMeta{}, pod)
		in.Now = done.Add(tc.d)
		r := Evaluate(in, Upstream(), Runtime{States: st})
		if got := len(skewFindings(r)) == 1; got != tc.want {
			t.Fatalf("at settled + %s: 022 %v, want %v", tc.d, got, tc.want)
		}
		if !tc.want && !r.Next.Equal(done.Add(SkewSettle)) {
			t.Fatalf("Next %v, want settled + 10 min", r.Next)
		}
	}
}

// A rollout that ends without a condition time settles when the check first sees it end.
func TestSkewRolloutEndWithoutTime(t *testing.T) {
	w := healthyWorld()
	st := NewStates()
	pod := skewPod("edge-0", wrapper+":v2.3.6")
	w.ctrl.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue, Reason: "ReplicaSetUpdated"}}
	Evaluate(skewInput(t, w, []cache.CRDMeta{}, pod), Upstream(), Runtime{States: st})
	w.ctrl.Status.Conditions = nil
	seen := testutil.Now.Add(time.Minute)
	in := skewInput(t, w, []cache.CRDMeta{}, pod)
	in.Now = seen
	if fs := skewFindings(Evaluate(in, Upstream(), Runtime{States: st})); len(fs) != 0 {
		t.Fatalf("right after the rollout: %+v", fs)
	}
	in.Now = seen.Add(SkewSettle + time.Second)
	if fs := skewFindings(Evaluate(in, Upstream(), Runtime{States: st})); len(fs) != 1 {
		t.Fatalf("10 min after the rollout: %+v", fs)
	}
}

// A GatewayParameters that pins the proxy image is named as the likely cause
// and joins the finding's dependencies.
func TestSkewPinnedImage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(w *world)
		want  objects.Ref
	}{
		{"on the Gateway", func(w *world) {
			gp := testutil.GatewayParameters("infra", "pinned", false)
			gp.Object["spec"] = map[string]any{"kube": map[string]any{"envoyContainer": map[string]any{"image": map[string]any{"tag": "v2.3.6"}}}}
			testutil.SetGatewayParameters(w.objs[1], kinds.KgatewayGroup, "GatewayParameters", "pinned")
			w.objs = append(w.objs, gp)
		}, objects.Ref{Group: kinds.KgatewayGroup, Kind: "GatewayParameters", Namespace: "infra", Name: "pinned"}},
		{"on the class", func(w *world) {
			gp := testutil.GatewayParameters("kgw", "class-params", false)
			gp.Object["spec"] = map[string]any{"kube": map[string]any{"envoyContainer": map[string]any{"image": map[string]any{"digest": "sha256:0123"}}}}
			testutil.SetClassParameters(w.objs[0], kinds.KgatewayGroup, "GatewayParameters", "kgw", "class-params")
			w.objs = append(w.objs, gp)
		}, objects.Ref{Group: kinds.KgatewayGroup, Kind: "GatewayParameters", Namespace: "kgw", Name: "class-params"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := healthyWorld()
			tc.setup(w)
			fs := skewFindings(Evaluate(skewInput(t, w, []cache.CRDMeta{}, skewPod("edge-0", wrapper+":v2.3.6")), Upstream(), Runtime{States: NewStates()}))
			if len(fs) != 1 || !strings.Contains(fs[0].Detail, "GatewayParameters "+tc.want.Namespace+"/"+tc.want.Name) {
				t.Fatalf("findings %+v", fs)
			}
			found := false
			for _, r := range fs[0].Evidence {
				found = found || r == tc.want
			}
			if !found {
				t.Fatalf("evidence refs %+v lack %v", fs[0].Evidence, tc.want)
			}
		})
	}
	fs := skewFindings(Evaluate(skewInput(t, healthyWorld(), []cache.CRDMeta{}, skewPod("edge-0", wrapper+":v2.3.6")), Upstream(), Runtime{States: NewStates()}))
	if len(fs) != 1 || strings.Contains(fs[0].Detail, "GatewayParameters") {
		t.Fatalf("no pin, yet %+v", fs)
	}
}

// Two controller minors after a settled rollout raise 022 on the controller
// Deployment; during the rollout they do not.
func TestSkewControllerReplicas(t *testing.T) {
	w := healthyWorld()
	old := testutil.ControllerPod(w.ctrl, "q", true)
	old.Spec.Containers = slices.Clone(old.Spec.Containers)
	old.Spec.Containers[0].Image = "cr.kgateway.dev/kgateway-dev/kgateway:v2.3.11"
	w.pods = append(w.pods, old)
	settledAt(w.ctrl, testutil.Now.Add(-time.Hour))
	fs := skewFindings(Evaluate(skewInput(t, w, []cache.CRDMeta{}), Upstream(), Runtime{States: NewStates()}))
	if len(fs) != 1 || fs[0].Resource == nil || fs[0].Resource.Kind != "Deployment" || fs[0].Resource.Name != "kgateway" {
		t.Fatalf("want 022 on the controller Deployment, got %+v", fs)
	}
	ev, ok := fs[0].Facts.(*VersionSkewEvidence)
	if !ok || len(ev.Replicas) != 2 || len(fs[0].Evidence) != 2 {
		t.Fatalf("evidence %+v, refs %+v", fs[0].Facts, fs[0].Evidence)
	}
	w.ctrl.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue, Reason: "ReplicaSetUpdated"}}
	if fs := skewFindings(Evaluate(skewInput(t, w, []cache.CRDMeta{}), Upstream(), Runtime{States: NewStates()})); len(fs) != 0 {
		t.Fatalf("during the rollout: %+v", fs)
	}
}

// A bundle other than the expected one raises one 022 on the representative
// CRD; a CRD of another version or channel raises its own.
func TestSkewCRDBundleAndChannel(t *testing.T) {
	crd := func(name, bundle, channel string) cache.CRDMeta {
		_, g, _ := strings.Cut(name, ".")
		return cache.CRDMeta{Name: name, Group: g, BundleVersion: bundle, Channel: channel}
	}
	old := []cache.CRDMeta{crd("gatewayclasses.gateway.networking.k8s.io", "v1.5.1", "standard"), crd("gateways.gateway.networking.k8s.io", "v1.5.1", "standard"),
		crd("httproutes.gateway.networking.k8s.io", "v1.5.1", "standard")}
	fs := skewFindings(Evaluate(skewInput(t, healthyWorld(), old), Upstream(), Runtime{States: NewStates()}))
	if len(fs) != 1 || fs[0].Resource == nil || fs[0].Resource.Name != "gateways.gateway.networking.k8s.io" || fs[0].Resource.Kind != "CustomResourceDefinition" {
		t.Fatalf("want one 022 on the Gateway CRD, got %+v", fs)
	}
	if ev, ok := fs[0].Facts.(*VersionSkewEvidence); !ok || ev.Expected != "v1.6" || len(ev.CRDs) != 1 || ev.CRDs[0].BundleVersion != "v1.5.1" {
		t.Fatalf("evidence %+v", fs[0].Facts)
	}
	mixed := []cache.CRDMeta{crd("gatewayclasses.gateway.networking.k8s.io", "v1.6.1", "standard"), crd("gateways.gateway.networking.k8s.io", "v1.6.1", "standard"),
		crd("httproutes.gateway.networking.k8s.io", "v1.6.1", "experimental"), crd("grpcroutes.gateway.networking.k8s.io", "v1.6.0", "standard"),
		crd("unannotated.gateway.networking.k8s.io", "", "")}
	fs = skewFindings(Evaluate(skewInput(t, healthyWorld(), mixed), Upstream(), Runtime{States: NewStates()}))
	var names []string
	for _, f := range fs {
		names = append(names, f.Resource.Name)
		if len(f.Evidence) != 1 || f.Evidence[0].Name != "gateways.gateway.networking.k8s.io" {
			t.Fatalf("%s: evidence refs %+v", f.Resource.Name, f.Evidence)
		}
	}
	if strings.Join(names, ",") != "grpcroutes.gateway.networking.k8s.io,httproutes.gateway.networking.k8s.io" {
		t.Fatalf("want 022 on grpcroutes and httproutes, got %v", names)
	}
	if fs := skewFindings(Evaluate(skewInput(t, healthyWorld(), mixed[:2]), Upstream(), Runtime{States: NewStates()})); len(fs) != 0 {
		t.Fatalf("matching CRDs raised %+v", fs)
	}
}

// CRDs off: the CRD part is not available (D-034), and the process findings stand (R42).
func TestSkewCRDsOff(t *testing.T) {
	r := Evaluate(skewInput(t, healthyWorld(), nil, skewPod("edge-0", wrapper+":v2.3.6")), Upstream(), Runtime{States: NewStates()})
	res := skewResult(t, r)
	if res.Unavailable != AvailOff || !strings.Contains(res.Detail, skewCRDsOffDetail) || res.Required {
		t.Fatalf("result %+v", res)
	}
	if fs := skewFindings(r); len(fs) != 1 {
		t.Fatalf("proxy skew dropped: %+v", r.Findings)
	}
	if r.State == StateUnknown || has(r, CodeSignalUnavailable, "", "") {
		t.Fatalf("CRDs off made the install unknown or raised 008: %+v", r)
	}
}
