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
