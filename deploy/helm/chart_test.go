// Package helm tests the rendered chart. It needs the helm CLI on PATH; tests
// skip without it unless REQUIRE_HELM=1.
package helm

import (
	"bytes"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
)

func render(t *testing.T, args ...string) []map[string]any {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("REQUIRE_HELM") == "1" {
			t.Fatalf("helm not on PATH and REQUIRE_HELM=1: %v", err)
		}
		t.Skip("helm not on PATH")
	}
	out, err := exec.Command("helm", append([]string{"template", "ui", "./kgateway-ui", "-n", "kgateway-ui"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	var docs []map[string]any
	for _, d := range bytes.Split(out, []byte("\n---")) {
		var m map[string]any
		if err := yaml.Unmarshal(d, &m); err == nil && m != nil {
			docs = append(docs, m)
		}
	}
	return docs
}

func kind(docs []map[string]any, k string) []map[string]any {
	var out []map[string]any
	for _, d := range docs {
		if d["kind"] == k {
			out = append(out, d)
		}
	}
	return out
}

func clusterRole(t *testing.T) rbacv1.ClusterRole {
	t.Helper()
	crs := kind(render(t), "ClusterRole")
	if len(crs) != 1 {
		t.Fatalf("want 1 ClusterRole, got %d", len(crs))
	}
	b, _ := yaml.Marshal(crs[0])
	var cr rbacv1.ClusterRole
	if err := yaml.Unmarshal(b, &cr); err != nil {
		t.Fatal(err)
	}
	return cr
}

// grants reports whether a rule allows verb on group/resource cluster-wide,
// honouring "*" in groups, resources and verbs. Rules with resourceNames do not count.
func grants(rules []rbacv1.PolicyRule, group, resource, verb string) bool {
	match := func(list []string, v string) bool { return slices.Contains(list, v) || slices.Contains(list, "*") }
	for _, r := range rules {
		if len(r.ResourceNames) == 0 && match(r.APIGroups, group) && match(r.Resources, resource) && match(r.Verbs, verb) {
			return true
		}
	}
	return false
}

func TestGrantsHandlesWildcards(t *testing.T) {
	rules := []rbacv1.PolicyRule{
		{APIGroups: []string{"gateway.kgateway.dev"}, Resources: []string{"*"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get"}},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, ResourceNames: []string{"one"}, Verbs: []string{"list"}},
	}
	for _, c := range []struct {
		group, resource, verb string
		want                  bool
	}{
		{"gateway.kgateway.dev", "gatewayparameters", "watch", true},
		{"gateway.kgateway.dev", "trafficpolicies", "delete", false},
		{"gateway.networking.k8s.io", "gateways", "get", false},
		{"", "pods", "get", true},
		{"", "pods", "list", false},
		{"apps", "deployments", "list", false},
	} {
		if got := grants(rules, c.group, c.resource, c.verb); got != c.want {
			t.Errorf("grants(%q, %q, %q) = %v, want %v", c.group, c.resource, c.verb, got, c.want)
		}
	}
}

func TestClusterRoleIsReadOnlyWithoutSecretsOrProxy(t *testing.T) {
	cr := clusterRole(t)
	for _, r := range cr.Rules {
		if slices.Contains(r.APIGroups, "*") {
			t.Errorf("apiGroup \"*\" must not be granted")
		}
		for _, v := range r.Verbs {
			if !slices.Contains([]string{"get", "list", "watch"}, v) {
				t.Errorf("verb %q is not read-only (D-001)", v)
			}
		}
		for _, res := range r.Resources {
			if res == "secrets" || res == "configmaps" || strings.Contains(res, "/proxy") || res == "*" && slices.Contains(r.APIGroups, "") {
				t.Errorf("resource %q must not be granted", res)
			}
		}
	}
	for _, res := range []string{"secrets", "configmaps", "pods/proxy", "services/proxy", "nodes/proxy"} {
		if grants(cr.Rules, "", res, "get") {
			t.Errorf("core %q must not be readable", res)
		}
	}
}

// TestClusterRoleGrantsEveryReadKind derives the kinds from the backend's
// registry, so a new kind without RBAC fails here.
func TestClusterRoleGrantsEveryReadKind(t *testing.T) {
	cr := clusterRole(t)
	want := []schema.GroupVersionResource{cache.NamespacesGVR, cache.PodsGVR, cache.DeploymentsGVR}
	for _, d := range kinds.Upstream() {
		want = append(want, schema.GroupVersionResource{Group: d.Group, Resource: d.Resource})
	}
	if !slices.ContainsFunc(want, func(g schema.GroupVersionResource) bool { return g.Resource == "gatewayparameters" }) {
		t.Fatal("kind registry lost gatewayparameters")
	}
	for _, g := range want {
		for _, v := range []string{"get", "list", "watch"} {
			if !grants(cr.Rules, g.Group, g.Resource, v) {
				t.Errorf("ClusterRole does not grant %s on %s (group %q)", v, g.Resource, g.Group)
			}
		}
	}
}

// TestClusterRoleCoversBackends: the explorer reads kgateway Backends.
func TestClusterRoleCoversBackends(t *testing.T) {
	cr := clusterRole(t)
	i := slices.IndexFunc(kinds.Upstream(), func(d kinds.Descriptor) bool { return d.Category == kinds.CategoryBackend })
	if i < 0 {
		t.Fatal("kind registry has no Backend")
	}
	d := kinds.Upstream()[i]
	for _, v := range []string{"get", "list", "watch"} {
		if !grants(cr.Rules, d.Group, d.Resource, v) {
			t.Errorf("ClusterRole does not grant %s on %s.%s", v, d.Resource, d.Group)
		}
	}
}

// TestClusterRoleOmitsUnreadKinds guards D-017 §5: nothing reads ReferenceGrants
// or CRDs, and discovery needs neither.
func TestClusterRoleOmitsUnreadKinds(t *testing.T) {
	cr := clusterRole(t)
	for _, c := range []struct{ group, resource string }{
		{"gateway.networking.k8s.io", "referencegrants"},
		{"apiextensions.k8s.io", "customresourcedefinitions"},
	} {
		for _, v := range []string{"get", "list", "watch"} {
			if grants(cr.Rules, c.group, c.resource, v) {
				t.Errorf("ClusterRole must not grant %s on %s (group %q)", v, c.resource, c.group)
			}
		}
	}
}

func TestHTTPRouteIsOptIn(t *testing.T) {
	if n := len(kind(render(t), "HTTPRoute")); n != 0 {
		t.Fatalf("HTTPRoute must be off by default, got %d", n)
	}
	docs := render(t, "--set", "httpRoute.enabled=true", "--set", "httpRoute.parentRefs[0].name=edge",
		"--set", "httpRoute.parentRefs[0].namespace=infra", "--set", "httpRoute.hostnames[0]=ui.example.com")
	routes := kind(docs, "HTTPRoute")
	if len(routes) != 1 {
		t.Fatalf("want HTTPRoute when enabled, got %d", len(routes))
	}
	b, _ := yaml.Marshal(routes[0])
	for _, want := range []string{"name: edge", "namespace: infra", "ui.example.com", "name: ui-kgateway-ui", "port: 80"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("HTTPRoute lacks %q:\n%s", want, b)
		}
	}
}

func TestHTTPRouteRequiresParentRefs(t *testing.T) {
	render(t) // skips without helm
	out, err := exec.Command("helm", "template", "ui", "./kgateway-ui", "-n", "kgateway-ui", "--set", "httpRoute.enabled=true").CombinedOutput()
	if err == nil {
		t.Fatalf("want render failure without parentRefs, got:\n%s", out)
	}
	if !strings.Contains(string(out), "httpRoute.parentRefs is required") {
		t.Fatalf("unclear error:\n%s", out)
	}
}

func TestExistingServiceAccountRequiresName(t *testing.T) {
	render(t) // skips without helm
	out, err := exec.Command("helm", "template", "ui", "./kgateway-ui", "-n", "kgateway-ui", "--set", "serviceAccount.create=false").CombinedOutput()
	if err == nil {
		t.Fatalf("want render failure without serviceAccount.name, got:\n%s", out)
	}
	if !strings.Contains(string(out), "serviceAccount.name is required") {
		t.Fatalf("unclear error:\n%s", out)
	}
	ds := kind(render(t, "--set", "serviceAccount.create=false", "--set", "serviceAccount.name=existing"), "Deployment")
	if len(ds) != 1 {
		t.Fatalf("want 1 Deployment, got %d", len(ds))
	}
	spec := ds[0]["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	if spec["serviceAccountName"] != "existing" {
		t.Fatalf("serviceAccountName = %v, want existing", spec["serviceAccountName"])
	}
}

func TestPinnedInstallIsPassedAsFlag(t *testing.T) {
	deploy := func(args ...string) string {
		ds := kind(render(t, args...), "Deployment")
		if len(ds) != 1 {
			t.Fatalf("want 1 Deployment, got %d", len(ds))
		}
		b, _ := yaml.Marshal(ds[0])
		return string(b)
	}
	if s := deploy(); strings.Contains(s, "--install") {
		t.Errorf("no pin by default:\n%s", s)
	}
	if s := deploy("--set", "install.pin=kgw/kgateway"); !strings.Contains(s, "--install=kgw/kgateway") {
		t.Fatalf("pin flag missing:\n%s", s)
	}
}

func TestPodIsHardened(t *testing.T) {
	ds := kind(render(t), "Deployment")
	if len(ds) != 1 {
		t.Fatalf("want 1 Deployment, got %d", len(ds))
	}
	b, _ := yaml.Marshal(ds[0])
	for _, want := range []string{"runAsNonRoot: true", "readOnlyRootFilesystem: true", "allowPrivilegeEscalation: false", "type: RuntimeDefault", "- ALL", "path: /readyz", "path: /healthz", "--listen=:8080", "containerPort: 8080"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("deployment lacks %q", want)
		}
	}
}

// containerArgs is the UI container's args as rendered with args.
func containerArgs(t *testing.T, args ...string) []string {
	t.Helper()
	ds := kind(render(t, args...), "Deployment")
	if len(ds) != 1 {
		t.Fatalf("want 1 Deployment, got %d", len(ds))
	}
	b, _ := yaml.Marshal(ds[0])
	var d struct {
		Spec struct {
			Template struct {
				Spec struct{ Containers []struct{ Args []string } }
			}
		}
	}
	if err := yaml.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	return d.Spec.Template.Spec.Containers[0].Args
}

func TestExtraArgsAppended(t *testing.T) {
	if got, want := containerArgs(t), []string{"--listen=:8080"}; !slices.Equal(got, want) {
		t.Errorf("default args = %v, want %v", got, want)
	}
	got := containerArgs(t, "--set", "install.pin=kgw/kgateway", "--set", "extraArgs={--poll-interval=10m}")
	if want := []string{"--listen=:8080", "--install=kgw/kgateway", "--poll-interval=10m"}; !slices.Equal(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestDelegationEdgeCapPassedAsFlag(t *testing.T) {
	if got := containerArgs(t); slices.ContainsFunc(got, func(a string) bool { return strings.HasPrefix(a, "--delegation-edge-cap") }) {
		t.Errorf("no edge cap flag by default: %v", got)
	}
	got := containerArgs(t, "--set", "delegation.edgeCap=20000", "--set", "extraArgs={--poll-interval=10m}")
	if want := []string{"--listen=:8080", "--delegation-edge-cap=20000", "--poll-interval=10m"}; !slices.Equal(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

// containerEnv is the UI container's env as rendered with args.
func containerEnv(t *testing.T, args ...string) []corev1.EnvVar {
	t.Helper()
	ds := kind(render(t, args...), "Deployment")
	if len(ds) != 1 {
		t.Fatalf("want 1 Deployment, got %d", len(ds))
	}
	b, _ := yaml.Marshal(ds[0])
	var d appsv1.Deployment
	if err := yaml.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	return d.Spec.Template.Spec.Containers[0].Env
}

// GOMEMLIMIT follows the container's memory limit through the downward API,
// and is unset without one (D-025).
func TestGOMEMLIMITFollowsTheMemoryLimit(t *testing.T) {
	env := containerEnv(t)
	i := slices.IndexFunc(env, func(e corev1.EnvVar) bool { return e.Name == "GOMEMLIMIT" })
	if i < 0 {
		t.Fatalf("no GOMEMLIMIT with the default 512Mi limit: %+v", env)
	}
	f := env[i].ValueFrom
	if env[i].Value != "" || f == nil || f.ResourceFieldRef == nil || f.ResourceFieldRef.Resource != "limits.memory" ||
		f.ResourceFieldRef.ContainerName != "kgateway-ui" || f.ResourceFieldRef.Divisor.String() != "1" {
		t.Fatalf("GOMEMLIMIT must be the container's limits.memory in bytes: %+v", env[i])
	}
	env = containerEnv(t, "--set", "resources.limits.memory=null")
	if slices.ContainsFunc(env, func(e corev1.EnvVar) bool { return e.Name == "GOMEMLIMIT" }) {
		t.Fatalf("GOMEMLIMIT must be unset without a memory limit: %+v", env)
	}
}
