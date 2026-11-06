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

func clusterRole(t *testing.T, args ...string) rbacv1.ClusterRole {
	t.Helper()
	crs := kind(render(t, args...), "ClusterRole")
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

// Checks the default render only; rbac.extraRules is the operator's choice.
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

// TestClusterRoleCoversListOnlyKinds: ReferenceGrants, DirectResponses and
// GatewayExtensions are listed and shown, so they are readable (D-056).
func TestClusterRoleCoversListOnlyKinds(t *testing.T) {
	cr := clusterRole(t)
	for _, g := range []schema.GroupResource{{Group: "gateway.networking.k8s.io", Resource: "referencegrants"},
		{Group: "gateway.kgateway.dev", Resource: "directresponses"}, {Group: "gateway.kgateway.dev", Resource: "gatewayextensions"}} {
		for _, v := range []string{"get", "list", "watch"} {
			if !grants(cr.Rules, g.Group, g.Resource, v) {
				t.Errorf("ClusterRole does not grant %s on %s", v, g)
			}
		}
	}
}

// grantsAll reports whether the rules allow get, list and watch on group/resource.
func grantsAll(rules []rbacv1.PolicyRule, group, resource string) bool {
	for _, v := range []string{"get", "list", "watch"} {
		if !grants(rules, group, resource, v) {
			return false
		}
	}
	return true
}

func TestEventsRuleFollowsToggle(t *testing.T) {
	if !grantsAll(clusterRole(t).Rules, "", "events") {
		t.Error("events must be readable by default")
	}
	if grants(clusterRole(t, "--set", "diagnostics.events=false").Rules, "", "events", "get") {
		t.Error("diagnostics.events=false must drop the events rule")
	}
}

func TestCRDRuleFollowsToggle(t *testing.T) {
	if !grantsAll(clusterRole(t).Rules, "apiextensions.k8s.io", "customresourcedefinitions") {
		t.Error("CRDs must be readable by default")
	}
	if grants(clusterRole(t, "--set", "diagnostics.crds=false").Rules, "apiextensions.k8s.io", "customresourcedefinitions", "get") {
		t.Error("diagnostics.crds=false must drop the CRD rule")
	}
}

func TestEndpointsRulesFollowToggle(t *testing.T) {
	on := clusterRole(t).Rules
	if !grantsAll(on, "", "services") || !grantsAll(on, "discovery.k8s.io", "endpointslices") {
		t.Error("services and endpointslices must be readable by default")
	}
	off := clusterRole(t, "--set", "diagnostics.endpoints=false").Rules
	if grants(off, "", "services", "get") || grants(off, "discovery.k8s.io", "endpointslices", "get") {
		t.Error("diagnostics.endpoints=false must drop both rules")
	}
}

// Defaults match the binary's, so only a non-default toggle becomes a flag.
func TestTogglesPassedAsFlags(t *testing.T) {
	if got, want := containerArgs(t), []string{"--listen=:8080"}; !slices.Equal(got, want) {
		t.Errorf("default args = %v, want %v", got, want)
	}
	got := containerArgs(t,
		"--set", "diagnostics.events=false", "--set", "diagnostics.crds=false",
		"--set", "diagnostics.proxyStats=false", "--set", "diagnostics.endpoints=false",
		"--set", "diagnostics.controllerLogs=true")
	want := []string{"--listen=:8080", "--diagnostics-events=false", "--diagnostics-crds=false",
		"--diagnostics-proxy-stats=false", "--diagnostics-endpoints=false", "--diagnostics-controller-logs=true"}
	if !slices.Equal(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

// pods/log is never in the ClusterRole; controllerLogs adds a Role and a
// RoleBinding to the UI's ServiceAccount only in each listed namespace.
func TestControllerLogsRoleIsOptInAndNamespaced(t *testing.T) {
	if r := kind(render(t), "Role"); len(r) != 0 {
		t.Errorf("no Role by default, got %d", len(r))
	}
	off := render(t, "--set", "diagnostics.controllerLogsNamespaces={kgateway-system,other}")
	if n := len(kind(off, "Role")) + len(kind(off, "RoleBinding")); n != 0 {
		t.Errorf("namespaces alone must not render a Role or RoleBinding, got %d", n)
	}
	args := []string{"--set", "diagnostics.controllerLogs=true", "--set", "diagnostics.controllerLogsNamespaces={kgateway-system,other}"}
	docs := render(t, args...)
	if grants(clusterRole(t, args...).Rules, "", "pods/log", "get") {
		t.Error("pods/log must not be in the ClusterRole")
	}
	var seen []string
	for _, d := range kind(docs, "Role") {
		b, _ := yaml.Marshal(d)
		var r rbacv1.Role
		if err := yaml.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		if len(r.Rules) != 1 || !slices.Equal(r.Rules[0].Resources, []string{"pods/log"}) || !slices.Equal(r.Rules[0].Verbs, []string{"get"}) {
			t.Errorf("Role %s/%s must grant only get on pods/log: %+v", r.Namespace, r.Name, r.Rules)
		}
		seen = append(seen, r.Namespace)
	}
	slices.Sort(seen)
	if !slices.Equal(seen, []string{"kgateway-system", "other"}) {
		t.Errorf("Role namespaces = %v", seen)
	}
	var bound []string
	for _, d := range kind(docs, "RoleBinding") {
		b, _ := yaml.Marshal(d)
		var rb rbacv1.RoleBinding
		if err := yaml.Unmarshal(b, &rb); err != nil {
			t.Fatal(err)
		}
		if rb.RoleRef.Kind != "Role" || rb.RoleRef.Name != "ui-kgateway-ui-logs" {
			t.Errorf("RoleBinding %s/%s must bind the logs Role: %+v", rb.Namespace, rb.Name, rb.RoleRef)
		}
		if want := []rbacv1.Subject{{Kind: "ServiceAccount", Name: "ui-kgateway-ui", Namespace: "kgateway-ui"}}; !slices.Equal(rb.Subjects, want) {
			t.Errorf("RoleBinding %s/%s subjects = %+v", rb.Namespace, rb.Name, rb.Subjects)
		}
		bound = append(bound, rb.Namespace)
	}
	slices.Sort(bound)
	if !slices.Equal(bound, []string{"kgateway-system", "other"}) {
		t.Errorf("RoleBinding namespaces = %v", bound)
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

// The container's memory limit reaches the binary as KGWUI_MEMORY_LIMIT, and
// the chart sets GOMEMLIMIT only from the opt-in memoryLimit (D-025, D-043).
func TestMemoryLimitEnv(t *testing.T) {
	named := func(n string) func(corev1.EnvVar) bool { return func(e corev1.EnvVar) bool { return e.Name == n } }
	env := containerEnv(t)
	i := slices.IndexFunc(env, named("KGWUI_MEMORY_LIMIT"))
	if i < 0 {
		t.Fatalf("no KGWUI_MEMORY_LIMIT with the default 512Mi limit: %+v", env)
	}
	f := env[i].ValueFrom
	if env[i].Value != "" || f == nil || f.ResourceFieldRef == nil || f.ResourceFieldRef.Resource != "limits.memory" ||
		f.ResourceFieldRef.ContainerName != "kgateway-ui" || f.ResourceFieldRef.Divisor.String() != "1" {
		t.Fatalf("KGWUI_MEMORY_LIMIT must be the container's limits.memory in bytes: %+v", env[i])
	}
	if slices.ContainsFunc(env, named("GOMEMLIMIT")) {
		t.Fatalf("the chart must not set GOMEMLIMIT: %+v", env)
	}
	env = containerEnv(t, "--set", "resources.limits.memory=null")
	if slices.ContainsFunc(env, named("KGWUI_MEMORY_LIMIT")) {
		t.Fatalf("KGWUI_MEMORY_LIMIT must be absent without a memory limit: %+v", env)
	}
	for _, args := range [][]string{{"--set", "memoryLimit=300MiB"}, {"--set", "memoryLimit=300MiB", "--set", "resources.limits.memory=null"}} {
		env = containerEnv(t, args...)
		if i := slices.IndexFunc(env, named("GOMEMLIMIT")); i < 0 || env[i].Value != "300MiB" {
			t.Fatalf("%v: want GOMEMLIMIT=300MiB: %+v", args, env)
		}
	}
}

// TestExtraRulesAppended: rbac.extraRules is the operator's choice and can
// widen RBAC; the default render stays read-only (see the read-only test).
func TestExtraRulesAppended(t *testing.T) {
	base := len(clusterRole(t).Rules)
	docs := render(t, "--set-json", `rbac.extraRules=[{"apiGroups":["example.dev"],"resources":["widgets"],"verbs":["get","delete"]}]`)
	crs := kind(docs, "ClusterRole")
	if len(crs) != 1 {
		t.Fatalf("want 1 ClusterRole, got %d", len(crs))
	}
	b, _ := yaml.Marshal(crs[0])
	var cr rbacv1.ClusterRole
	if err := yaml.Unmarshal(b, &cr); err != nil {
		t.Fatal(err)
	}
	if len(cr.Rules) != base+1 {
		t.Fatalf("rules = %d, want %d", len(cr.Rules), base+1)
	}
	last := cr.Rules[len(cr.Rules)-1]
	if !slices.Equal(last.Resources, []string{"widgets"}) || !slices.Equal(last.Verbs, []string{"get", "delete"}) {
		t.Errorf("extra rule not appended last: %+v", last)
	}
}

func TestExtraEnvVolumesAndContainers(t *testing.T) {
	deployment := func(args ...string) appsv1.Deployment {
		ds := kind(render(t, args...), "Deployment")
		if len(ds) != 1 {
			t.Fatalf("want 1 Deployment, got %d", len(ds))
		}
		b, _ := yaml.Marshal(ds[0])
		var d appsv1.Deployment
		if err := yaml.Unmarshal(b, &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	def := deployment().Spec.Template.Spec
	if len(def.Containers) != 1 || len(def.Volumes) != 0 || len(def.Containers[0].VolumeMounts) != 0 {
		t.Fatalf("defaults must add no containers, volumes or mounts: %+v", def)
	}
	d := deployment(
		"--set-json", `extraEnv=[{"name":"ADDON","value":"1"}]`,
		"--set-json", `extraVolumes=[{"name":"data","emptyDir":{}}]`,
		"--set-json", `extraVolumeMounts=[{"name":"data","mountPath":"/data"}]`,
		"--set-json", `extraContainers=[{"name":"addon","image":"example/addon:1"}]`,
	).Spec.Template.Spec
	if len(d.Containers) != 2 || d.Containers[0].Name != "kgateway-ui" || d.Containers[1].Name != "addon" {
		t.Fatalf("containers = %+v", d.Containers)
	}
	if len(d.Containers[1].VolumeMounts) != 0 {
		t.Errorf("the sidecar must get no volumeMounts: %+v", d.Containers[1].VolumeMounts)
	}
	ui := d.Containers[0]
	if !slices.ContainsFunc(ui.Env, func(e corev1.EnvVar) bool { return e.Name == "ADDON" && e.Value == "1" }) ||
		!slices.ContainsFunc(ui.Env, func(e corev1.EnvVar) bool { return e.Name == "KGWUI_MEMORY_LIMIT" }) {
		t.Errorf("extraEnv must join the memory-limit env: %+v", ui.Env)
	}
	if len(ui.VolumeMounts) != 1 || ui.VolumeMounts[0].MountPath != "/data" {
		t.Errorf("volumeMounts = %+v", ui.VolumeMounts)
	}
	if len(d.Volumes) != 1 || d.Volumes[0].Name != "data" {
		t.Errorf("volumes = %+v", d.Volumes)
	}
	// extraEnv alone, without a memory limit, still renders an env list.
	env := containerEnv(t, "--set", "resources.limits.memory=null", "--set-json", `extraEnv=[{"name":"ADDON","value":"1"}]`)
	if len(env) != 1 || env[0].Name != "ADDON" {
		t.Errorf("env = %+v", env)
	}
}
