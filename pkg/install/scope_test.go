package install

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
)

func ns(name string, l map[string]string) *corev1.Namespace { return testutil.Namespace(name, l) }

func TestParseSelectorsMirrorsKgatewaySemantics(t *testing.T) {
	a := ns("a", map[string]string{"team": "a"})
	b := ns("b", map[string]string{"team": "b", "env": "prod"})
	c := ns("c", nil)
	cases := []struct {
		name, raw string
		mode      ScopeMode
		in        []*corev1.Namespace
		out       []*corev1.Namespace
	}{
		{"unset means all", "", ScopeAll, []*corev1.Namespace{a, b, c}, nil},
		{"empty list means all", "[]", ScopeAll, []*corev1.Namespace{a, b, c}, nil},
		{"empty selector matches all", "[{}]", ScopeSelectors, []*corev1.Namespace{a, b, c}, nil},
		{"selectors are OR'ed", `[{"matchLabels":{"team":"a"}},{"matchLabels":{"env":"prod"}}]`, ScopeSelectors, []*corev1.Namespace{a, b}, []*corev1.Namespace{c}},
		{"matchExpressions", `[{"matchExpressions":[{"key":"team","operator":"In","values":["b"]}]}]`, ScopeSelectors, []*corev1.Namespace{b}, []*corev1.Namespace{a, c}},
		{"invalid JSON is unknown", `[{`, ScopeUnknown, nil, nil},
		{"invalid operator is unknown", `[{"matchExpressions":[{"key":"team","operator":"Bogus"}]}]`, ScopeUnknown, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := ParseSelectors(tc.raw)
			if s.Mode != tc.mode {
				t.Fatalf("mode: want %s got %s (%s)", tc.mode, s.Mode, s.Err)
			}
			for _, n := range tc.in {
				if !s.Contains(n.Name, n) {
					t.Errorf("%s should be in scope", n.Name)
				}
			}
			for _, n := range tc.out {
				if s.Contains(n.Name, n) {
					t.Errorf("%s should be out of scope", n.Name)
				}
			}
		})
	}
}

func TestScopeAllContainsNamespaceMissingFromCache(t *testing.T) {
	// Objects can arrive before their Namespace is cached; "all" must not drop them.
	if !ParseSelectors("[]").Contains("late", nil) {
		t.Fatal("all-namespaces scope must contain a namespace not yet cached")
	}
	if ParseSelectors(`[{"matchLabels":{"team":"a"}}]`).Contains("late", nil) {
		t.Fatal("a selector scope cannot match a namespace whose labels are unknown")
	}
}

func TestNamespaceSelectorScopeReadsPodEnv(t *testing.T) {
	img := "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7"
	sel := `[{"matchLabels":{"team":"a"}}]`
	cases := []struct {
		name string
		env  []corev1.EnvVar
		mode ScopeMode
	}{
		{"literal value", []corev1.EnvVar{testutil.Env(DiscoveryNamespaceSelectorsEnv, sel)}, ScopeSelectors},
		{"duplicate env: last wins", []corev1.EnvVar{testutil.Env(DiscoveryNamespaceSelectorsEnv, sel), testutil.Env(DiscoveryNamespaceSelectorsEnv, "[]")}, ScopeAll},
		{"valueFrom is unknown", []corev1.EnvVar{{Name: DiscoveryNamespaceSelectorsEnv, ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{Key: "k"}}}}, ScopeUnknown},
		{"absent means all", nil, ScopeAll},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, 1, tc.env...)
			in, _ := UpstreamDetector{}.Detect(d)
			in.Pods = []*corev1.Pod{testutil.ControllerPod(d, "p", true)}
			if got := (NamespaceSelectorScope{}).Scope(in).Mode; got != tc.mode {
				t.Fatalf("want %s got %s", tc.mode, got)
			}
		})
	}
}

// A crash-looping controller (no ready Pod) still has a Pod spec;
// the scope is read from it rather than becoming unknown.
func TestScopeFromNotReadyPod(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 0,
		testutil.Env(DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"a"}}]`))
	in, _ := UpstreamDetector{}.Detect(d)
	in.Pods = []*corev1.Pod{testutil.ControllerPod(d, "p", false)}
	if got := (NamespaceSelectorScope{}).Scope(in).Mode; got != ScopeSelectors {
		t.Fatalf("want selectors from the not-ready pod's spec, got %s", got)
	}
}

// podWithEnv is a controller Pod whose selector env is val, ready or not.
func podWithEnv(d *appsv1.Deployment, name string, ready bool, val string) *corev1.Pod {
	p := testutil.ControllerPod(d, name, ready)
	for i := range p.Spec.Containers {
		p.Spec.Containers[i].Env = []corev1.EnvVar{testutil.Env(DiscoveryNamespaceSelectorsEnv, val)}
	}
	return p
}

// The source order is: Ready Pod, any Pod, Deployment template.
func TestScopeSourceOrder(t *testing.T) {
	img := "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7"
	tmpl := `[{"matchLabels":{"from":"template"}}]`
	newInstall := func(env ...corev1.EnvVar) (Install, *appsv1.Deployment) {
		d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, 0, env...)
		in, _ := UpstreamDetector{}.Detect(d)
		return in, d
	}
	label := func(s Scope) string {
		if s.Mode != ScopeSelectors || len(s.Selectors) != 1 {
			return string(s.Mode)
		}
		return s.Selectors[0].MatchLabels["from"]
	}
	t.Run("ready pod beats not-ready pod and template", func(t *testing.T) {
		in, d := newInstall(testutil.Env(DiscoveryNamespaceSelectorsEnv, tmpl))
		in.Pods = []*corev1.Pod{
			podWithEnv(d, "unready", false, `[{"matchLabels":{"from":"unready"}}]`),
			podWithEnv(d, "ready", true, `[{"matchLabels":{"from":"ready"}}]`)}
		s := (NamespaceSelectorScope{}).Scope(in)
		if label(s) != "ready" || s.Source != "pod ready" {
			t.Fatalf("got %s source %q", label(s), s.Source)
		}
	})
	t.Run("not-ready pod beats template", func(t *testing.T) {
		in, d := newInstall(testutil.Env(DiscoveryNamespaceSelectorsEnv, tmpl))
		in.Pods = []*corev1.Pod{podWithEnv(d, "unready", false, `[{"matchLabels":{"from":"unready"}}]`)}
		s := (NamespaceSelectorScope{}).Scope(in)
		if label(s) != "unready" || s.Source != "pod unready" {
			t.Fatalf("got %s source %q", label(s), s.Source)
		}
	})
	t.Run("template used with zero pods", func(t *testing.T) {
		in, _ := newInstall(testutil.Env(DiscoveryNamespaceSelectorsEnv, tmpl))
		s := (NamespaceSelectorScope{}).Scope(in)
		if label(s) != "template" || s.Source != SourceDeploymentTemplate {
			t.Fatalf("got %s source %q", label(s), s.Source)
		}
	})
	t.Run("template last env wins", func(t *testing.T) {
		in, _ := newInstall(testutil.Env(DiscoveryNamespaceSelectorsEnv, tmpl), testutil.Env(DiscoveryNamespaceSelectorsEnv, "[]"))
		if s := (NamespaceSelectorScope{}).Scope(in); s.Mode != ScopeAll || s.Source != SourceDeploymentTemplate {
			t.Fatalf("got %+v", s)
		}
	})
	t.Run("template valueFrom is unknown", func(t *testing.T) {
		in, _ := newInstall(corev1.EnvVar{Name: DiscoveryNamespaceSelectorsEnv,
			ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{Key: "k"}}})
		if s := (NamespaceSelectorScope{}).Scope(in); s.Mode != ScopeUnknown {
			t.Fatalf("got %+v", s)
		}
	})
	t.Run("nil deployment and no pods is unknown", func(t *testing.T) {
		in, _ := newInstall()
		in.Deployment = nil
		if s := (NamespaceSelectorScope{}).Scope(in); s.Mode != ScopeUnknown {
			t.Fatalf("got %+v", s)
		}
	})
}
