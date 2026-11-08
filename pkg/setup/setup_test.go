package setup_test

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/metadata"
	metadatafake "k8s.io/client-go/metadata/fake"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/snellius/pkg/app"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/extension"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/setup"
)

// The test imports only pkg/ packages, as a module outside this repo would (E3).

var (
	t0      = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	created = metav1.NewTime(t0.Add(-time.Hour))
)

const controllerName = "kgateway.dev/kgateway"

// emptyScraper serves an empty /metrics payload.
type emptyScraper struct{}

func (emptyScraper) Scrape(context.Context, *corev1.Pod, int32) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func cond(typ string, st metav1.ConditionStatus, reason, msg string) map[string]any {
	return map[string]any{"type": typ, "status": string(st), "reason": reason, "message": msg,
		"observedGeneration": int64(1), "lastTransitionTime": created.UTC().Format(time.RFC3339)}
}

func object(apiVersion, kind, ns, name string, spec, status map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{"apiVersion": apiVersion, "kind": kind, "spec": spec, "status": status}}
	u.SetNamespace(ns)
	u.SetName(name)
	u.SetGeneration(1)
	u.SetUID(types.UID(kind + "/" + ns + "/" + name))
	u.SetCreationTimestamp(created)
	return u
}

var edgeRef = map[string]any{"group": "gateway.networking.k8s.io", "kind": "Gateway", "namespace": "infra", "name": "edge"}

// route is shop/name on infra/edge with backendRefs and one status parent of
// conds; no conds means no status from the controller.
func route(name string, backends []any, conds ...any) *unstructured.Unstructured {
	parents := []any{}
	if len(conds) > 0 {
		parents = append(parents, map[string]any{"parentRef": edgeRef, "controllerName": controllerName, "conditions": conds})
	}
	spec := map[string]any{"parentRefs": []any{edgeRef}, "rules": []any{map[string]any{"backendRefs": backends}}}
	return object("gateway.networking.k8s.io/v1", "HTTPRoute", "shop", name, spec, map[string]any{"parents": parents})
}

func trafficPolicy(ns, name string, spec map[string]any) *unstructured.Unstructured {
	return object("gateway.kgateway.dev/v1alpha1", "TrafficPolicy", ns, name, spec, map[string]any{"ancestors": []any{}})
}

// world raises each of KGWUI-013 to KGWUI-018 on one kgateway 2.4 install.
func world() []runtime.Object {
	ok := []any{cond("Accepted", "True", "Accepted", ""), cond("Programmed", "True", "Programmed", "")}
	conflicted := []any{cond("Accepted", "False", "HostnameConflict", ""), cond("Programmed", "False", "HostnameConflict", ""),
		cond("Conflicted", "True", "HostnameConflict", "")}
	kindsHTTP := []any{map[string]any{"group": "gateway.networking.k8s.io", "kind": "HTTPRoute"}}
	listener := func(name string) map[string]any {
		return map[string]any{"name": name, "port": int64(8080), "protocol": "HTTP", "allowedRoutes": map[string]any{"namespaces": map[string]any{"from": "All"}}}
	}
	gw := object("gateway.networking.k8s.io/v1", "Gateway", "infra", "edge",
		map[string]any{"gatewayClassName": "kgateway", "listeners": []any{listener("http"), listener("http-dup")}},
		map[string]any{"conditions": ok, "addresses": []any{map[string]any{"type": "IPAddress", "value": "10.0.0.1"}}, "listeners": []any{
			map[string]any{"name": "http", "attachedRoutes": int64(1), "conditions": ok, "supportedKinds": kindsHTTP},
			map[string]any{"name": "http-dup", "attachedRoutes": int64(0), "conditions": conflicted, "supportedKinds": []any{}}}})
	class := object("gateway.networking.k8s.io/v1", "GatewayClass", "", "kgateway", map[string]any{"controllerName": controllerName},
		map[string]any{"conditions": []any{cond("Accepted", "True", "Accepted", "")}})
	svc := func(ns, name string) []any {
		return []any{map[string]any{"name": name, "namespace": ns, "port": int64(80)}}
	}
	accepted := cond("Accepted", "True", "Accepted", "")
	objs := []runtime.Object{class, gw,
		// 013 and 014: the controller cannot resolve a backend, or may not reach another namespace.
		route("missing", svc("shop", "nope"), accepted, cond("ResolvedRefs", "False", "BackendNotFound", "Service shop/nope not found")),
		route("cross", svc("other", "svc"), accepted, cond("ResolvedRefs", "False", "RefNotPermitted", "")),
		// 015 on 2.4: kgateway.dev/Programmed carries the replaced rule.
		route("replaced", svc("shop", "web"), accepted, cond("ResolvedRefs", "True", "ResolvedRefs", ""),
			cond("kgateway.dev/Programmed", "False", "RouteRuleReplaced", "Replaced Rule (0): ext auth unavailable")),
		// 016: a missing Gateway target, and a selector in the global policy namespace that matches nothing.
		trafficPolicy("shop", "tp-gone", map[string]any{"targetRefs": []any{map[string]any{"group": "gateway.networking.k8s.io", "kind": "Gateway", "name": "gone"}}}),
		trafficPolicy("global", "tp-all", map[string]any{"targetSelectors": []any{map[string]any{"group": "gateway.networking.k8s.io", "kind": "HTTPRoute",
			"matchLabels": map[string]any{"app": "none"}}}}),
	}
	// 018: routes the controller never wrote status for hold KGWUI-003 warnings.
	for i := range 5 {
		objs = append(objs, route(fmt.Sprintf("silent-%d", i), svc("shop", "web")))
	}
	replicas := int32(1)
	podLabels := map[string]string{"kgateway": "kgateway", "app.kubernetes.io/name": "kgateway", "app.kubernetes.io/instance": "kgateway",
		"app.kubernetes.io/component": "controller"}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kgw", Name: "kgateway", UID: "deploy/kgw/kgateway", CreationTimestamp: created,
			Labels: map[string]string{"kgateway": "kgateway", "app.kubernetes.io/component": "controller", "app.kubernetes.io/version": "2.4.7"}},
		Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"kgateway": "kgateway"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: podLabels}, Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: "kgateway", Image: "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7",
				Env:   []corev1.EnvVar{{Name: install.GlobalPolicyEnv, Value: "global"}},
				Ports: []corev1.ContainerPort{{Name: "metrics", ContainerPort: 9092}}}}}}},
		Status: appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1, ObservedGeneration: 1},
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "kgw", Name: "kgateway-0", UID: "pod/kgw/kgateway-0", Labels: maps.Clone(podLabels)},
		Spec: dep.Spec.Template.Spec, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.1.0.5",
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	for _, ns := range []string{"kgw", "infra", "shop", "other", "global"} {
		objs = append(objs, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, UID: types.UID("ns/" + ns)}})
	}
	// A kept Warning Event: its add makes the app recompute
	// once the Events watch has read, as the other watches' adds do.
	ev := &corev1.Event{ObjectMeta: metav1.ObjectMeta{Namespace: "kgw", Name: "kgateway-0.1", UID: "event/kgw/kgateway-0.1"}, Type: corev1.EventTypeWarning,
		Reason: "FailedMount", InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "kgw", Name: "kgateway-0", UID: pod.UID}, Count: 1}
	return append(objs, dep, pod, ev)
}

// fakeClients serves every upstream kind at its first version, with objs in
// the dynamic tracker under their served GVR or in the typed clientset.
func fakeClients(t *testing.T, objs []runtime.Object) (*dynamicfake.FakeDynamicClient, *kubefake.Clientset) {
	t.Helper()
	listKinds := map[schema.GroupVersionResource]string{}
	byKind := map[schema.GroupKind]schema.GroupVersionResource{}
	byGV := map[string]*metav1.APIResourceList{}
	for _, d := range kinds.Upstream() {
		s := kinds.Served{Descriptor: d, Version: d.Versions[0]}
		listKinds[s.GVR()] = s.Kind + "List"
		byKind[schema.GroupKind{Group: s.Group, Kind: s.Kind}] = s.GVR()
		gv := schema.GroupVersion{Group: s.Group, Version: s.Version}.String()
		if byGV[gv] == nil {
			byGV[gv] = &metav1.APIResourceList{GroupVersion: gv}
		}
		byGV[gv].APIResources = append(byGV[gv].APIResources, metav1.APIResource{Name: s.Resource, Kind: s.Kind, Namespaced: s.Namespaced})
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds)
	var typed []runtime.Object
	for _, o := range objs {
		u, ok := o.(*unstructured.Unstructured)
		if !ok {
			typed = append(typed, o)
			continue
		}
		gvr, ok := byKind[u.GroupVersionKind().GroupKind()]
		if !ok {
			t.Fatalf("%s is not served", u.GroupVersionKind())
		}
		if err := dyn.Tracker().Create(gvr, u.DeepCopy(), u.GetNamespace()); err != nil {
			t.Fatal(err)
		}
	}
	kube := kubefake.NewSimpleClientset(typed...)
	fd := kube.Discovery().(*fakediscovery.FakeDiscovery)
	// Core Services are served too, so a backendRef's kind is known.
	fd.Resources = append(fd.Resources, &metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "services", Kind: "Service", Namespaced: true}}})
	for _, l := range byGV {
		fd.Resources = append(fd.Resources, l)
	}
	return dyn, kube
}

// oneShot is the kgwctl flow: build the config, settle the cache, collect
// metrics once and compute on demand.
type oneShot struct {
	eng *engine.Engine
	clk *clocktesting.FakeClock
}

func newOneShot(t *testing.T, ctx context.Context, objs []runtime.Object) *oneShot {
	t.Helper()
	return newOneShotWith(t, ctx, objs, extension.New())
}

// newOneShotWith is newOneShot over reg.
func newOneShotWith(t *testing.T, ctx context.Context, objs []runtime.Object, reg *extension.Registry) *oneShot {
	t.Helper()
	return oneShotSequence(t, ctx, objs, reg, emptyScraper{}, true)
}

// oneShotSequence is the ctrlmetrics package's one-shot sequence over objs and
// reg, scraping with s; without publish, no metrics reach the engine.
func oneShotSequence(t *testing.T, ctx context.Context, objs []runtime.Object, reg *extension.Registry, s ctrlmetrics.Scraper, publish bool) *oneShot {
	t.Helper()
	dyn, kube := fakeClients(t, objs)
	clk := clocktesting.NewFakeClock(t0)
	cfg, err := setup.Build(setup.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Metadata: fakeMetadata(t)},
		setup.Options{MetricsInterval: time.Hour, Clock: clk}, reg)
	if err != nil {
		t.Fatal(err)
	}
	ca := cache.New(cfg.Cache)
	ca.Start(ctx)
	settle(t, ctx, func() bool { return ca.Settled() && optionalSettled(ca.Snapshot()) })
	for _, gvr := range optional {
		if a := ca.Snapshot().Access[gvr]; !a.OK {
			t.Fatalf("optional watch %v: %+v; want it read", gvr, a)
		}
	}
	snap := ca.Snapshot()
	installs, errs := install.Discover(snap, reg.Detectors)
	installs = slices.DeleteFunc(installs, func(i install.Install) bool { return i.Impl != "kgateway" })
	if len(installs) == 0 || len(errs) > 0 {
		t.Fatalf("installs %v, errors %v", installs, errs)
	}
	if publish {
		ctrlmetrics.Publish(cfg.Collected, ctrlmetrics.CollectOnce(ctx, s, installs, setup.MetricsMaxAge(time.Hour)))
	}
	cfg.Engine.Snapshots = fixed{snap}
	return &oneShot{eng: engine.New(cfg.Engine), clk: clk}
}

func (o *oneShot) at(now time.Time) *engine.Model {
	o.clk.SetTime(now)
	return o.eng.Compute()
}

// fixed returns one snapshot, as the one-shot sequence's step 4 says.
type fixed struct{ s *cache.Snapshot }

func (f fixed) Snapshot() *cache.Snapshot { return f.s }

// running is pkg/app over its own fakes, read through its loop's events.
type running struct {
	clk    *clocktesting.FakeClock
	events chan engine.LoopEvent
}

func newRunning(t *testing.T, ctx context.Context, objs []runtime.Object) *running {
	t.Helper()
	return newRunningWith(t, ctx, objs, emptyScraper{})
}

// newRunningWith is newRunning scraping with s.
func newRunningWith(t *testing.T, ctx context.Context, objs []runtime.Object, s ctrlmetrics.Scraper) *running {
	t.Helper()
	dyn, kube := fakeClients(t, objs)
	r := &running{clk: clocktesting.NewFakeClock(t0), events: make(chan engine.LoopEvent, 4096)}
	a, err := app.Build(app.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Metadata: fakeMetadata(t), Scraper: s},
		app.Options{PollInterval: time.Hour, MetricsInterval: time.Hour, ConfirmEvery: time.Hour, UI: fstest.MapFS{"index.html": {Data: []byte("x")}},
			Clock: r.clk, Diagnostics: app.Diagnostics{NoProxyStats: true}, OnLoop: func(e engine.LoopEvent) {
				if e.Kind == engine.EvPublished || e.Kind == engine.EvArmed {
					r.events <- e
				}
			}}, extension.New())
	if err != nil {
		t.Fatal(err)
	}
	a.Start(ctx)
	return r
}

// optional are the optional watches a single pass waits for.
var optional = []schema.GroupVersionResource{cache.ProxyPodsGVR, cache.CRDsGVR, cache.EventsGVR, cache.ServicesGVR, cache.EndpointSlicesGVR}

// optionalSettled is whether each optional watch in snap is OK or failed.
func optionalSettled(snap *cache.Snapshot) bool {
	for _, gvr := range optional {
		if a, ok := snap.Access[gvr]; ok && !a.OK && a.Err == "" {
			return false
		}
	}
	return true
}

// settledModel is whether m is over a settled cache, optional watches
// included, with metrics read.
func settledModel(m *engine.Model) bool {
	if !m.Snapshot.Synced || !optionalSettled(m.Snapshot) || len(m.Installs) != 1 {
		return false
	}
	v, _ := m.Collected.Get(ctrlmetrics.Key)
	_, _, read := ctrlmetrics.Lookup(v, m.Installs[0].Resolved.ID())
	return read
}

// fakeMetadata serves no CRDs, so the CRD metadata watch runs and reads empty.
func fakeMetadata(t *testing.T) metadata.Interface {
	t.Helper()
	scheme := metadatafake.NewTestScheme()
	if err := metav1.AddMetaToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return metadatafake.NewSimpleMetadataClient(scheme)
}

// first returns the app's first settled model. Its loop debounces triggers on
// its clock, so each time it arms a timer the clock moves 1 s.
func (r *running) first(t *testing.T) *engine.Model {
	t.Helper()
	for {
		switch e := r.next(t); e.Kind {
		case engine.EvArmed:
			r.clk.Step(engine.MaxDelay)
		case engine.EvPublished:
			if settledModel(e.Published.Model) {
				return e.Published.Model
			}
		}
	}
}

// after steps the app's clock to its model's next deadline and returns the
// first settled model computed from then on.
func (r *running) after(t *testing.T, m *engine.Model) *engine.Model {
	t.Helper()
	at := m.Next.At
	if m.Next.Strict {
		at = at.Add(time.Nanosecond)
	}
	r.clk.SetTime(at)
	for {
		if e := r.next(t); e.Kind == engine.EvPublished && !e.Published.Model.ComputedAt.Before(at) && settledModel(e.Published.Model) {
			return e.Published.Model
		}
	}
}

func (r *running) next(t *testing.T) engine.LoopEvent {
	t.Helper()
	select {
	case e := <-r.events:
		return e
	case <-time.After(10 * time.Second):
		t.Fatal("the app's loop went quiet")
		return engine.LoopEvent{}
	}
}

func settle(t *testing.T, ctx context.Context, settled func() bool) {
	t.Helper()
	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 10*time.Second, true, func(context.Context) (bool, error) { return settled(), nil }); err != nil {
		t.Fatal("cache did not settle")
	}
}

// findings is each finding of m's only install as one comparable line. Since
// is left out: the app's first sight trails the one-shot's by its debounce.
func findings(m *engine.Model) []string {
	var out []string
	for _, f := range m.Installs[0].Health.Findings {
		out = append(out, fmt.Sprintf("%s %s %s %v %q %q group=%s", f.ID, f.Code, f.Severity, f.Resource, f.Message, f.Detail, f.Group))
	}
	return out
}

// proxyStatsChecks read the proxy stats collector, which only pkg/app runs:
// off there, not collected for a one-shot caller.
var proxyStatsChecks = []string{"proxy config warming", "proxy connection"}

// checks is the install's state and each check's name, state and
// availability, the proxy stats checks left out.
func checks(m *engine.Model) []string {
	h := m.Installs[0].Health
	out := []string{"state " + string(h.State)}
	for _, c := range h.Checks {
		if slices.Contains(proxyStatsChecks, c.Name) {
			continue
		}
		out = append(out, fmt.Sprintf("%s ok=%v required=%v pending=%v unavailable=%q error=%q", c.Name, c.OK, c.Required, c.Pending, c.Unavailable, c.Error))
	}
	return out
}

func codes(m *engine.Model) map[string]bool {
	out := map[string]bool{}
	for _, f := range m.Installs[0].Health.Findings {
		out[f.Code] = true
	}
	return out
}

func matured(m *engine.Model) bool {
	c := codes(m)
	return c[health.CodePolicyTargetNotFound] && c[health.CodeStatusNotProgressing]
}

// follow computes again at each Model.Next until the time-based 016 and 018 matured.
func follow(t *testing.T, m *engine.Model, next func(*engine.Model) *engine.Model) *engine.Model {
	t.Helper()
	for pass := 0; !matured(m); pass++ {
		if m.Next.At.IsZero() || pass == 20 {
			t.Fatalf("not matured after %d passes; codes %v", pass, codes(m))
		}
		m = next(m)
	}
	return m
}

func same(t *testing.T, what string, got, want *engine.Model) {
	t.Helper()
	if g, w := findings(got), findings(want); !slices.Equal(g, w) {
		t.Fatalf("%s findings:\none-shot %s\napp      %s", what, strings.Join(g, "\n         "), strings.Join(w, "\n         "))
	}
	if g, w := checks(got), checks(want); !slices.Equal(g, w) {
		t.Fatalf("%s checks:\none-shot %s\napp      %s", what, strings.Join(g, "\n         "), strings.Join(w, "\n         "))
	}
}

// D-053, E3: a one-shot caller built through pkg/setup gets the findings and
// ids pkg/app serves over the same objects. A single pass reads the time-based
// 016 and 018 as pending; computing again at Model.Next matures them.
func TestOneShotMatchesTheApp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	objs := world()
	one, ui := newOneShot(t, ctx, objs), newRunning(t, ctx, objs)

	first, uiFirst := one.at(t0), ui.first(t)
	if c := codes(first); c[health.CodePolicyTargetNotFound] || c[health.CodeStatusNotProgressing] || first.Next.At.IsZero() {
		t.Fatalf("first pass: codes %v, next %v; want 016 and 018 pending and a next pass", c, first.Next)
	}
	same(t, "first pass", first, uiFirst)
	// Without the collector, proxy stats read as not collected, never healthy (D-006).
	for _, c := range first.Installs[0].Health.Checks {
		if slices.Contains(proxyStatsChecks, c.Name) && (c.OK || c.Unavailable != health.AvailFailing) {
			t.Errorf("%s: %+v; want not collected", c.Name, c)
		}
	}

	last := follow(t, first, func(m *engine.Model) *engine.Model {
		at := m.Next.At
		if m.Next.Strict {
			at = at.Add(time.Nanosecond)
		}
		return one.at(at)
	})
	uiLast := follow(t, uiFirst, func(m *engine.Model) *engine.Model { return ui.after(t, m) })
	same(t, "matured", last, uiLast)
	for _, c := range []string{health.CodeRefUnresolved, health.CodeRefNotPermitted, health.CodeRuleReplaced, health.CodePolicyTargetNotFound,
		health.CodeListenerConflicted, health.CodeStatusNotProgressing} {
		if !codes(last)[c] {
			t.Errorf("%s not raised; codes %v", c, codes(last))
		}
	}
}

// Build rejects what pkg/app rejects, before any client is used.
func TestBuildRejectsBadOptions(t *testing.T) {
	dyn, kube := fakeClients(t, nil)
	c := setup.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery()}
	for _, tc := range []struct {
		name string
		o    setup.Options
		reg  *extension.Registry
	}{
		{"zero metrics interval", setup.Options{}, extension.New()},
		{"negative edge cap", setup.Options{MetricsInterval: time.Second, DelegationEdgeCap: -1}, extension.New()},
		{"bad pinned", setup.Options{MetricsInterval: time.Second, Pinned: "a/b/c"}, extension.New()},
		{"registry error", setup.Options{MetricsInterval: time.Second}, extension.New(extension.WithChecks(extension.UpstreamPrefix))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := setup.Build(c, tc.o, tc.reg); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

// The config carries the UI's timings: metrics are stale after three intervals,
// and the default scope verifier, rule selection and catalog are set.
func TestBuildSetsTheUITimings(t *testing.T) {
	dyn, kube := fakeClients(t, nil)
	cfg, err := setup.Build(setup.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery()},
		setup.Options{MetricsInterval: 10 * time.Second, NoEvents: true}, extension.New())
	if err != nil {
		t.Fatal(err)
	}
	e := cfg.Engine
	if setup.MetricsMaxAge(10*time.Second) != 30*time.Second {
		t.Errorf("metrics max age %v", setup.MetricsMaxAge(10*time.Second))
	}
	if e.Verifier == nil || e.RulesFor == nil || e.Catalog == nil || e.KindsChange == nil || e.Collected == nil || cfg.Collected == nil || e.Triggers != cfg.Changes {
		t.Errorf("engine config incomplete: %+v", e)
	}
	if cfg.Cache.Events || !cfg.Cache.CRDs || len(cfg.Cache.PodSelectors) == 0 || len(cfg.Cache.ProxyPodSelectors) == 0 || cfg.Cache.Changes != cfg.Changes {
		t.Errorf("cache config %+v", cfg.Cache)
	}
	if vs := e.Collected().Values; len(vs) != 0 {
		t.Errorf("the store must start empty: %v", vs)
	}
}

// The Service and EndpointSlice watches are on by default and off with NoEndpoints.
func TestBuildEndpointsToggle(t *testing.T) {
	for _, off := range []bool{false, true} {
		dyn, kube := fakeClients(t, nil)
		cfg, err := setup.Build(setup.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery()},
			setup.Options{MetricsInterval: 10 * time.Second, NoEndpoints: off}, extension.New())
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Cache.Endpoints == off {
			t.Errorf("NoEndpoints=%v: Cache.Endpoints=%v", off, cfg.Cache.Endpoints)
		}
	}
}
