package install

import (
	"testing"

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
	in.Pods = nil
	if got := (NamespaceSelectorScope{}).Scope(in).Mode; got != ScopeUnknown {
		t.Fatalf("no pods at all must be unknown, got %s", got)
	}
}
