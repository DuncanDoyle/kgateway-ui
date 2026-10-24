package install

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
)

func settingsInstall(env []corev1.EnvVar, envFrom bool, ready ...bool) Install {
	d := testutil.ControllerDeployment("kgateway-system", "kgateway", "img:1", 1, 1, env...)
	if envFrom {
		d.Spec.Template.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{{}}
	}
	inst := Install{Namespace: d.Namespace, Name: d.Name, Container: "kgateway", Deployment: d}
	for i, r := range ready {
		inst.Pods = append(inst.Pods, testutil.ControllerPod(d, "pod-"+string(rune('a'+i)), r))
	}
	return inst
}

func TestReadExperimental(t *testing.T) {
	cases := []struct {
		name    string
		env     []corev1.EnvVar
		envFrom bool
		want    Setting // Reason compared with strings.Contains
	}{
		{"absent", nil, false, Setting{Value: "true", Known: true}},
		{"explicit false", []corev1.EnvVar{testutil.Env(ExperimentalEnv, "false")}, false, Setting{Value: "false", Known: true}},
		{"last duplicate wins", []corev1.EnvVar{testutil.Env(ExperimentalEnv, "false"), testutil.Env(ExperimentalEnv, "true")}, false, Setting{Value: "true", Known: true}},
		{"valueFrom", []corev1.EnvVar{{Name: ExperimentalEnv, ValueFrom: &corev1.EnvVarSource{}}}, false, Setting{Reason: "valueFrom"}},
		{"envFrom without entry", nil, true, Setting{Reason: "envFrom"}},
		{"entry beats envFrom", []corev1.EnvVar{testutil.Env(ExperimentalEnv, "false")}, true, Setting{Value: "false", Known: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ReadSettings(settingsInstall(tc.env, tc.envFrom, true)).Experimental
			if got.Value != tc.want.Value || got.Known != tc.want.Known || !strings.Contains(got.Reason, tc.want.Reason) {
				t.Fatalf("want %+v got %+v", tc.want, got)
			}
			if got.Source != "pod pod-a" {
				t.Errorf("source: %q", got.Source)
			}
		})
	}
}

func TestSettingsSourceOrder(t *testing.T) {
	d := testutil.ControllerDeployment("ns", "kgw", "img:1", 1, 1, testutil.Env(ExperimentalEnv, "true"))
	notReady := testutil.ControllerPod(d, "old", false)
	notReady.Spec.Containers = []corev1.Container{{Name: "kgateway", Env: []corev1.EnvVar{testutil.Env(ExperimentalEnv, "false")}}}
	ready := testutil.ControllerPod(d, "new", true)
	inst := Install{Namespace: "ns", Name: "kgw", Container: "kgateway", Deployment: d}

	inst.Pods = []*corev1.Pod{notReady, ready}
	if s := ReadSettings(inst).Experimental; s.Source != "pod new" || s.Value != "true" {
		t.Errorf("ready pod should win: %+v", s)
	}
	inst.Pods = []*corev1.Pod{notReady}
	if s := ReadSettings(inst).Experimental; s.Source != "pod old" || s.Value != "false" {
		t.Errorf("non-ready pod beats template: %+v", s)
	}
	inst.Pods = nil
	if s := ReadSettings(inst).Experimental; s.Source != SourceDeploymentTemplate || s.Value != "true" {
		t.Errorf("template: %+v", s)
	}
	if s := ReadSettings(Install{Container: "kgateway"}).Experimental; s.Known || s.Reason == "" {
		t.Errorf("no workload must be unknown: %+v", s)
	}
}

func TestSettingBool(t *testing.T) {
	cases := []struct {
		val          string
		known, value bool
	}{{"true", true, true}, {"False", true, false}, {"1", true, true}}
	for _, tc := range cases {
		v, known := Setting{Value: tc.val, Known: true}.Bool()
		if v != tc.value || known != tc.known {
			t.Errorf("%q: got (%v,%v)", tc.val, v, known)
		}
	}
	// An unparsable value is not known, so Bool returns (false, false).
	if v, known := (Setting{Value: "maybe", Known: true}).Bool(); v || known {
		t.Errorf("maybe: got (%v,%v)", v, known)
	}
	if _, known := (Setting{Value: "true"}).Bool(); known {
		t.Error("an unknown Setting must not be known")
	}
}

func TestNamespaceLabels(t *testing.T) {
	snap := &cache.Snapshot{
		Namespaces: []*corev1.Namespace{ns("a", map[string]string{"team": "a"}), ns("bare", nil)},
		Access:     map[schema.GroupVersionResource]cache.Access{cache.NamespacesGVR: {OK: true}},
	}
	got, ok := NamespaceLabels(snap)
	if !ok || got["a"]["team"] != "a" {
		t.Fatalf("labels: %v ok=%v", got, ok)
	}
	if l, present := got["bare"]; !present || len(l) != 0 {
		t.Errorf("unlabelled namespace must be present and empty: %v", got)
	}
	snap.Access[cache.NamespacesGVR] = cache.Access{OK: false}
	if _, ok := NamespaceLabels(snap); ok {
		t.Error("failed access must report ok=false")
	}
}
