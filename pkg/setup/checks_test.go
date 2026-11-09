package setup_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/ctrllogs"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/extension"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/proxystats"
)

const (
	otherController = "example.com/gateway"
	classOnlyID     = "example.com~2Fgateway"
)

// packCodes are the codes of the kgateway pack (Phase 5 design §6).
var packCodes = []string{health.CodeXDSRejected, health.CodeScopeMismatch, health.CodeSelfManagedProxy, health.CodeDelegatedNotAttached,
	health.CodeComputedDisagrees, health.CodeRuleReplaced, health.CodePolicyTargetNotFound, health.CodeVersionSkew, health.CodeConfigWithheld,
	health.CodeProxyConfigWarming, health.CodeProxyDisconnected}

// packChecks are the checks of the kgateway pack (health.Kgateway).
var packChecks = []string{"xDS rejects", "scope matches controller", "version skew", "proxy config warming", "proxy connection", "computed views", "withheld config"}

// otherWorld is world plus a controller no pack claims, with what reads as
// kgateway semantics: a NACK-shaped Gateway status, a kgateway.dev/Programmed
// rule replacement, a backendRef to an HTTPRoute, policy targets that do not
// exist, and Envoy-looking proxy Pods.
func otherWorld() []runtime.Object {
	gw := map[string]any{"group": "gateway.networking.k8s.io", "kind": "Gateway", "namespace": "shop", "name": "other"}
	ok := cond("Accepted", "True", "Accepted", "")
	class := object("gateway.networking.k8s.io/v1", "GatewayClass", "", "other", map[string]any{"controllerName": otherController},
		map[string]any{"conditions": []any{ok}})
	listener := map[string]any{"name": "http", "port": int64(80), "protocol": "HTTP", "allowedRoutes": map[string]any{"namespaces": map[string]any{"from": "All"}}}
	gateway := object("gateway.networking.k8s.io/v1", "Gateway", "shop", "other", map[string]any{"gatewayClassName": "other", "listeners": []any{listener}},
		map[string]any{"conditions": []any{ok, cond("Programmed", "False", "Invalid", "Proxy rejected the listener config (NACK)")},
			"listeners": []any{map[string]any{"name": "http", "attachedRoutes": int64(1), "conditions": []any{ok, cond("Programmed", "True", "Programmed", "")},
				"supportedKinds": []any{map[string]any{"group": "gateway.networking.k8s.io", "kind": "HTTPRoute"}}}}})
	child := []any{map[string]any{"group": "gateway.networking.k8s.io", "kind": "HTTPRoute", "name": "child"}}
	web := object("gateway.networking.k8s.io/v1", "HTTPRoute", "shop", "web",
		map[string]any{"parentRefs": []any{gw}, "rules": []any{map[string]any{"backendRefs": child}}},
		map[string]any{"parents": []any{map[string]any{"parentRef": gw, "controllerName": otherController, "conditions": []any{ok,
			cond("ResolvedRefs", "True", "ResolvedRefs", ""),
			cond("kgateway.dev/Programmed", "False", "RouteRuleReplaced", "Replaced Rule (0): ext auth unavailable")}}}})
	leaf := object("gateway.networking.k8s.io/v1", "HTTPRoute", "shop", "child",
		map[string]any{"rules": []any{map[string]any{"backendRefs": []any{map[string]any{"name": "web", "port": int64(80)}}}}}, map[string]any{"parents": []any{}})
	tp := trafficPolicy("shop", "tp-other", map[string]any{"targetRefs": []any{
		map[string]any{"group": "gateway.networking.k8s.io", "kind": "HTTPRoute", "name": "gone"},
		map[string]any{"group": "gateway.networking.k8s.io", "kind": "Gateway", "name": "other", "sectionName": "nope"}}})
	// A route whose parentRef names web: a delegation candidate under kgateway's rules.
	webRef := map[string]any{"group": "gateway.networking.k8s.io", "kind": "HTTPRoute", "namespace": "shop", "name": "web"}
	declared := object("gateway.networking.k8s.io/v1", "HTTPRoute", "shop", "declared",
		map[string]any{"parentRefs": []any{webRef}, "rules": []any{map[string]any{"backendRefs": []any{map[string]any{"name": "web", "port": int64(80)}}}}},
		map[string]any{"parents": []any{}})
	objs := []runtime.Object{class, gateway, web, leaf, declared, tp}
	for i := range 2 {
		name := fmt.Sprintf("other-%d", i)
		objs = append(objs, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: name, UID: types.UID("pod/shop/" + name),
			Labels: map[string]string{"gateway.networking.k8s.io/gateway-name": "other", "app.kubernetes.io/name": "other"}},
			Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "envoy", Image: "cr.kgateway.dev/kgateway-dev/envoy-wrapper:v2.4.7"}}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}})
	}
	return append(world(), objs...)
}

// installOf is install id's model in m.
func installOf(t *testing.T, m *engine.Model, id string) engine.InstallModel {
	t.Helper()
	for _, im := range m.Installs {
		if im.Resolved.ID() == id {
			return im
		}
	}
	t.Fatalf("no install %s", id)
	return engine.InstallModel{}
}

// ids are the install's finding IDs, in order.
func ids(im engine.InstallModel) []string {
	out := make([]string, 0, len(im.Health.Findings))
	for _, f := range im.Health.Findings {
		out = append(out, f.ID)
	}
	return out
}

// A pack's checks run only on the installs it claimed: a class-only install
// raises no kgateway-pack code and lists no kgateway-pack check, and the
// kgateway install next to it keeps its findings (Phase 5 design §6, §4.3).
func TestClassOnlyInstallRunsOnlyCoreChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	objs := otherWorld()
	// A minute on, so held and in-grace findings have matured.
	one := oneShotSequence(t, ctx, objs, extension.New(), emptyScraper{}, true, true)
	off := oneShotSequence(t, ctx, objs, extension.New(), emptyScraper{}, true, false)
	one.at(t0)
	off.at(t0)
	on, base := one.at(t0.Add(time.Minute)), off.at(t0.Add(time.Minute))
	co := installOf(t, on, classOnlyID)
	if len(co.Owned.Declared) != 0 || co.Computed != nil {
		t.Errorf("class-only install has delegation candidates %v or computed views %v", co.Owned.Declared, co.Computed)
	}
	for _, f := range co.Health.Findings {
		if slices.Contains(packCodes, f.Code) {
			t.Errorf("class-only install raised %s: %s %v %q", f.Code, f.Message, f.Resource, f.Detail)
		}
	}
	for _, c := range co.Health.Checks {
		if slices.Contains(packChecks, c.Name) {
			t.Errorf("class-only install ran kgateway check %q", c.Name)
		}
	}
	if got, want := ids(installOf(t, on, "kgw.kgateway")), ids(installOf(t, base, "kgw.kgateway")); !slices.Equal(got, want) {
		t.Fatalf("kgateway findings with class-only installs on:\n%v\nwant as with them off:\n%v", got, want)
	}
	if !slices.ContainsFunc(installOf(t, on, "kgw.kgateway").Health.Checks, func(c health.CheckResult) bool { return c.Name == "xDS rejects" }) {
		t.Fatal("the kgateway install lost its kgateway checks")
	}
}

// classOnlyHealth is the class-only install's findings and check results over
// otherWorld at t0, then again after a minute, with the collected store holding
// pack data or not.
func classOnlyHealth(t *testing.T, packData bool) [2][]string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	one := oneShotSequence(t, ctx, otherWorld(), extension.New(), emptyScraper{}, true, true)
	if packData {
		gw := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "shop", Name: "other"}
		no := false
		res := ctrlmetrics.Results{ByInstall: map[string]ctrlmetrics.Result{classOnlyID: {CollectedAt: t0, Metrics: ctrlmetrics.Metrics{
			Rejects: map[ctrlmetrics.GatewayKey]float64{{Namespace: "shop", Name: "other"}: 1}, ManagedPresent: true}}}}
		store := one.cfg.Collected
		store.Publish(ctrlmetrics.Key, res, res.Fingerprint()^1, 0) // over the one-shot's own scrape
		store.Publish(proxystats.Key, proxystats.Stats{classOnlyID: {{Gateway: gw, Observed: true,
			Pods: []proxystats.Pod{{UID: "pod/shop/other-0", Name: "other-0", Connected: &no}}}}}, 1, 0)
		store.Publish(ctrllogs.Key, ctrllogs.Logs{classOnlyID: {State: ctrllogs.StateOn, Events: []ctrllogs.Event{
			{Time: t0, Gateway: gw, TypeURL: "type.googleapis.com/envoy.config.listener.v3.Listener"}}}}, 1, 0)
		if v, _ := store.Snapshot().Get(proxystats.Key); v.State != collect.OK {
			t.Fatal("proxy stats not published")
		}
	}
	var out [2][]string
	for i, at := range []int{0, 60} {
		m := one.at(t0.Add(time.Duration(at) * time.Second))
		co := installOf(t, m, classOnlyID)
		for _, f := range co.Health.Findings {
			out[i] = append(out[i], fmt.Sprintf("%s %s %v %q %q", f.ID, f.Code, f.Resource, f.Message, f.Detail))
		}
		for _, c := range co.Health.Checks {
			out[i] = append(out[i], fmt.Sprintf("%s ok=%v unavailable=%q error=%q detail=%q", c.Name, c.OK, c.Unavailable, c.Error, c.Detail))
		}
	}
	return out
}

// Phase 5 design §7.4: the core checks on a class-only install give equal
// findings with and without metrics, controller-log events and proxy stats in
// the collected values, the memoized recompute included.
func TestCoreChecksIgnorePackData(t *testing.T) {
	with, without := classOnlyHealth(t, true), classOnlyHealth(t, false)
	for i, what := range []string{"first compute", "memoized recompute"} {
		if !slices.Equal(with[i], without[i]) {
			t.Errorf("%s with pack data:\n%v\nwithout:\n%v", what, with[i], without[i])
		}
	}
}
