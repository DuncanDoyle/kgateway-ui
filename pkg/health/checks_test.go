package health

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

const img = "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7"

type world struct {
	nss       []*corev1.Namespace
	objs      []*unstructured.Unstructured
	deploys   []*appsv1.Deployment
	ctrl      *appsv1.Deployment
	pods      []*corev1.Pod
	scope     string
	firstSeen func(u *unstructured.Unstructured) time.Time // nil = creation time
	fromPod   bool                                         // resolve the scope from the controller Pod env instead of scope
	metrics   *ctrlmetrics.Result                          // nil = not collected yet
	access    map[schema.GroupVersionResource]cache.Access // overrides on top of "everything readable"
	platform  platform.Info                                // zero = Kubernetes
}

func healthyWorld() *world {
	d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, 1)
	gw := testutil.GwRef("infra", "edge")
	return &world{
		nss:     []*corev1.Namespace{testutil.Namespace("infra", nil), testutil.Namespace("shop", nil)},
		ctrl:    d,
		deploys: []*appsv1.Deployment{d, testutil.ProxyDeployment("infra", "edge", 1, 1)},
		pods:    []*corev1.Pod{testutil.ControllerPod(d, "p", true)},
		scope:   "[]",
		objs: []*unstructured.Unstructured{
			testutil.GatewayClass("kgateway", testutil.Controller, true),
			testutil.Gateway("infra", "edge", "kgateway", nil),
			testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		},
		metrics: &ctrlmetrics.Result{CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{}, Managed: map[ctrlmetrics.ManagedKey]float64{}, ManagedPresent: true}},
	}
}

func (w *world) report(t *testing.T) Report {
	t.Helper()
	snap := &cache.Snapshot{Objects: testutil.ObjectsByGVR(t, w.objs...), Access: map[schema.GroupVersionResource]cache.Access{},
		Namespaces: w.nss, Deployments: w.deploys, Pods: w.pods, Synced: true, TakenAt: testutil.Now}
	served := testutil.ServedUpstream()
	for _, k := range served {
		snap.Access[k.GVR()] = cache.Access{OK: true}
	}
	for _, gvr := range []schema.GroupVersionResource{cache.NamespacesGVR, cache.PodsGVR, cache.DeploymentsGVR} {
		snap.Access[gvr] = cache.Access{OK: true}
	}
	for gvr, a := range w.access {
		snap.Access[gvr] = a
	}
	in, _ := install.UpstreamDetector{}.Detect(w.ctrl)
	in.Pods = w.pods
	res := install.Resolved{Install: in, Scope: install.ParseSelectors(w.scope)}
	if w.fromPod {
		res.Scope = install.NamespaceSelectorScope{}.Scope(in)
	}
	a := install.Assign(snap, served, []install.Resolved{res})
	hi := Input{Install: res, Owned: a.PerInstall[res.ID()], Assignment: a, Snapshot: snap, Served: served, Now: testutil.Now,
		MetricsMaxAge: 30 * time.Second, Verify: scopeverify.Result{Available: true}, Platform: w.platform}
	if w.metrics != nil {
		hi.Metrics, hi.HasMetrics = *w.metrics, true
	}
	hi.FirstSeen = w.firstSeen
	return Evaluate(hi, Upstream())
}

// has reports whether r has a finding with code on res; an empty sev or res matches any.
func has(r Report, code string, sev Severity, res string) bool {
	for _, f := range r.Findings {
		if f.Code == code && (sev == "" || f.Severity == sev) && (res == "" || (f.Resource != nil && f.Resource.String() == res)) {
			return true
		}
	}
	return false
}

func TestHealthyWorldIsHealthy(t *testing.T) {
	r := healthyWorld().report(t)
	if r.State != StateHealthy || len(r.Findings) != 0 {
		t.Fatalf("state=%s findings=%+v", r.State, r.Findings)
	}
	for _, c := range r.Checks {
		if c.Required && !c.OK {
			t.Fatalf("check %s not OK: %s", c.Name, c.Error)
		}
	}
}

func TestChecks(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	cases := []struct {
		name   string
		mutate func(w *world)
		code   string
		sev    Severity
		res    string
		state  State
	}{
		{"001 xds rejected", func(w *world) {
			w.metrics.Metrics.Rejects[ctrlmetrics.GatewayKey{Namespace: "infra", Name: "edge"}] = 1
		}, CodeXDSRejected, SeverityError, "infra/edge", StateError},
		{"002 gateway not programmed", func(w *world) {
			w.objs[1] = testutil.Gateway("infra", "edge", "kgateway", []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionFalse, "AddressNotAssigned")})
		}, CodeConditionFalse, SeverityError, "infra/edge", StateError},
		{"002 route backend not found", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
		}, CodeConditionFalse, SeverityError, "shop/cart", StateError},
		{"002 rule replaced (2.4, optional condition present)", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs"),
				testutil.Cond("kgateway.dev/Programmed", metav1.ConditionFalse, "RouteRuleReplaced")}})
		}, CodeConditionFalse, SeverityError, "shop/cart", StateError},
		{"002 rule replaced on 2.3 (Accepted=False)", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionFalse, "RouteRuleReplaced"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}})
		}, CodeConditionFalse, SeverityError, "shop/cart", StateError},
		// Main only: v2.3.x and v2.4.x write no status for a missing target (see "003 policy without ancestors").
		{"002 policy target not found (main)", func(w *world) {
			w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{
				Ref: objects.ParentRef{Group: "gateway.kgateway.dev", Kind: "StatusSummary", Name: "summary"}, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "TargetNotFound")}}))
		}, CodeConditionFalse, SeverityError, "shop/tp", StateError},
		{"002 policy partially valid", func(w *world) {
			w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "PartiallyValid")}}))
		}, CodeConditionFalse, SeverityWarning, "shop/tp", StateWarning},
		{"002 generic policy rejected", func(w *world) {
			w.objs = append(w.objs, testutil.BackendTLSPolicy("shop", "btls", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "Invalid")}}))
		}, CodeConditionFalse, SeverityError, "shop/btls", StateError},
		{"002 policy overridden is info", func(w *world) {
			w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Valid"), testutil.Cond("Attached", metav1.ConditionFalse, "Overridden")}}))
		}, CodeConditionFalse, SeverityInfo, "shop/tp", StateHealthy},
		{"003 route not processed", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw})
		}, CodeRouteUnattached, SeverityWarning, "shop/cart", StateWarning},
		{"003 required condition absent", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")}})
		}, CodeRouteUnattached, SeverityWarning, "shop/cart", StateWarning},
		{"003 policy without ancestors", func(w *world) {
			w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp"))
		}, CodeRouteUnattached, SeverityWarning, "shop/tp", StateWarning},
		{"003 generic policy without ancestors", func(w *world) {
			w.objs = append(w.objs, testutil.BackendTLSPolicy("shop", "btls"))
		}, CodeRouteUnattached, SeverityWarning, "shop/btls", StateWarning},
		{"003 parent gateway does not exist", func(w *world) {
			w.objs = append(w.objs, testutil.HTTPRoute("shop", "orphan", []objects.ParentRef{testutil.GwRef("infra", "gone")}))
		}, CodeRouteUnattached, SeverityWarning, "shop/orphan", StateWarning},
		{"004 namespace not watched", func(w *world) {
			w.scope = `[{"matchLabels":{"team":"a"}}]`
			w.nss = []*corev1.Namespace{testutil.Namespace("infra", map[string]string{"team": "a"}), testutil.Namespace("shop", nil)}
		}, CodeNamespaceNotWatched, SeverityWarning, "shop/cart", StateWarning},
		{"005 controller not ready", func(w *world) {
			w.ctrl.Status.ReadyReplicas = 0
		}, CodeControllerNotReady, SeverityError, "kgw/kgateway", StateError},
		{"006 proxy not ready", func(w *world) {
			w.deploys[1] = testutil.ProxyDeployment("infra", "edge", 2, 1)
		}, CodeProxyNotReady, SeverityError, "infra/edge", StateError},
		{"008 metrics unavailable forces unknown", func(w *world) {
			w.metrics = &ctrlmetrics.Result{CollectedAt: testutil.Now, Err: "connection refused"}
		}, CodeSignalUnavailable, SeverityInfo, "", StateUnknown},
		{"008 metrics not collected yet forces unknown", func(w *world) { w.metrics = nil }, CodeSignalUnavailable, SeverityInfo, "", StateUnknown},
		{"008 stale metrics force unknown", func(w *world) {
			w.metrics.CollectedAt = testutil.Now.Add(-5 * time.Minute) // older than MetricsMaxAge (30 s)
		}, CodeSignalUnavailable, SeverityInfo, "", StateUnknown},
		{"008 namespaces not readable forces unknown", func(w *world) {
			w.access = map[schema.GroupVersionResource]cache.Access{cache.NamespacesGVR: {Err: "connection refused"}}
		}, CodeSignalUnavailable, SeverityInfo, "", StateUnknown},
		{"008 scope unknown forces unknown", func(w *world) { w.scope = "[{" }, CodeSignalUnavailable, SeverityInfo, "", StateUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := healthyWorld()
			tc.mutate(w)
			r := w.report(t)
			if !has(r, tc.code, tc.sev, tc.res) {
				t.Fatalf("missing %s/%s on %q; findings: %+v", tc.code, tc.sev, tc.res, r.Findings)
			}
			if r.State != tc.state {
				t.Fatalf("state: want %s got %s", tc.state, r.State)
			}
		})
	}
}

// Only False makes a present optional condition KGWUI-002; Unknown is no
// finding. Policy severities come only from the freshness-checked types.
func TestOptionalAndUncheckedConditionsAreNotErrors(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	cases := []struct {
		name   string
		mutate func(w *world)
	}{
		{"route optional Unknown", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs"),
				testutil.Cond("kgateway.dev/Programmed", metav1.ConditionUnknown, "Reconciling")}})
		}},
		{"listener optional Unknown", func(w *world) {
			w.objs[1] = testutil.Gateway("infra", "edge", "kgateway", nil, testutil.Listener{Name: "http", Conds: []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed"),
				testutil.Cond("ResolvedRefs", metav1.ConditionUnknown, "Reconciling")}})
		}},
		{"policy ancestor with an unchecked condition type False", func(w *world) {
			w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Valid"), testutil.Cond("example.com/Merged", metav1.ConditionFalse, "NotMerged")}}))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := healthyWorld()
			tc.mutate(w)
			r := w.report(t)
			if r.State != StateHealthy || len(r.Findings) != 0 {
				t.Fatalf("want healthy without findings: state=%s findings=%+v", r.State, r.Findings)
			}
		})
	}
}

// A crash-looping controller is an error and metrics are unknown; never healthy.
func TestCrashLoopingController(t *testing.T) {
	w := healthyWorld()
	w.ctrl.Status.ReadyReplicas = 0
	w.pods = []*corev1.Pod{testutil.ControllerPod(w.ctrl, "p", false)}
	w.metrics = &ctrlmetrics.Result{CollectedAt: testutil.Now, Err: "no ready controller pod to scrape"}
	r := w.report(t)
	if r.State != StateError || !has(r, CodeControllerNotReady, SeverityError, "") || !has(r, CodeSignalUnavailable, SeverityInfo, "") {
		t.Fatalf("state=%s findings=%+v", r.State, r.Findings)
	}
}

// Stale conditions describe an older spec, whether False or True.
func TestStaleConditionIsPendingNotError(t *testing.T) {
	w := healthyWorld()
	gw := testutil.GwRef("infra", "edge")
	route := testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}})
	route.SetGeneration(2) // conditions observed generation 1
	w.objs[2] = route
	r := w.report(t)
	if has(r, CodeConditionFalse, SeverityError, "shop/cart") {
		t.Fatal("a stale False condition must not be reported as a current error")
	}
	if !has(r, CodeRouteUnattached, SeverityWarning, "shop/cart") {
		t.Fatalf("a route stale for longer than the grace period must be 'not yet processed': %+v", r.Findings)
	}
}

// A stale True condition must not count as a current success.
func TestStaleTrueIsPendingNotHealthy(t *testing.T) {
	w := healthyWorld()
	gw := testutil.GwRef("infra", "edge")
	route := testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller))
	route.SetGeneration(2) // AcceptedBy's conditions observed generation 1
	w.objs[2] = route
	r := w.report(t)
	if r.State == StateHealthy || !has(r, CodeRouteUnattached, SeverityWarning, "shop/cart") {
		t.Fatalf("stale True must be 'not yet processed', got state=%s findings=%+v", r.State, r.Findings)
	}
	n := 0
	for _, f := range r.Findings {
		if f.Code == CodeRouteUnattached {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("one parent gets at most one KGWUI-003 finding, got %d", n)
	}
}

// Inside the grace period an unprocessed resource is
// "being processed" (info), and the install is unknown, never healthy.
func TestResourceInGraceIsBeingProcessedAndNotHealthy(t *testing.T) {
	w := healthyWorld()
	young := testutil.HTTPRoute("shop", "young", []objects.ParentRef{testutil.GwRef("infra", "edge")})
	young.SetCreationTimestamp(metav1.NewTime(testutil.Now.Add(-10 * time.Second)))
	w.objs = append(w.objs, young)
	r := w.report(t)
	if !has(r, CodeRouteUnattached, SeverityInfo, "shop/young") || has(r, CodeRouteUnattached, SeverityWarning, "shop/young") {
		t.Fatalf("a route inside the grace period is 'being processed' at info: %+v", r.Findings)
	}
	if r.State != StateUnknown {
		t.Fatalf("state: want unknown while a resource is being processed, got %s", r.State)
	}
	for _, c := range r.Checks {
		if c.Name == "resource status" && (c.OK || !c.Pending || c.Error != "1 resource is being processed by the controller") {
			t.Fatalf("resource status check: %+v", c)
		}
	}
}

// KGWUI-003 is per (resource, parentRef). Two missing
// parents give two findings, and a pending owned parent does not hide a missing one.
func TestNotProcessedIsPerParent(t *testing.T) {
	w := healthyWorld()
	gw := testutil.GwRef("infra", "edge")
	w.objs = append(w.objs,
		testutil.HTTPRoute("shop", "two-missing", []objects.ParentRef{testutil.GwRef("infra", "gone-1"), testutil.GwRef("infra", "gone-2")}),
		testutil.HTTPRoute("shop", "mixed", []objects.ParentRef{gw, testutil.GwRef("infra", "gone-3")})) // no status for edge either
	r := w.report(t)
	per := map[string]int{}
	for _, f := range r.Findings {
		if f.Code == CodeRouteUnattached && f.Resource != nil {
			per[f.Resource.String()]++
		}
	}
	if per["shop/two-missing"] != 2 || per["shop/mixed"] != 2 {
		t.Fatalf("want one KGWUI-003 per parent: %v; findings %+v", per, r.Findings)
	}
}

// Pending, a missing listener block, a status entry for
// another port, and observedGeneration 0 are all "not yet processed", never healthy.
func TestUnprocessedEvidenceIsNeverHealthy(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	gw443 := gw
	gw443.Port = 443
	cases := []struct {
		name   string
		mutate func(w *world)
		res    string
	}{
		{"policy Accepted=False/Pending", func(w *world) {
			w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "Pending"), testutil.Cond("Attached", metav1.ConditionFalse, "Pending")}}))
		}, "shop/tp"},
		{"gateway Programmed=Unknown with a non-Pending reason", func(w *world) {
			w.objs[1] = testutil.Gateway("infra", "edge", "kgateway", []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionUnknown, "NoResources")})
		}, "infra/edge"},
		{"route ResolvedRefs=Unknown", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionUnknown, "Unknown")}})
		}, "shop/cart"},
		{"gateway Programmed=Unknown/Pending", func(w *world) {
			w.objs[1] = testutil.Gateway("infra", "edge", "kgateway", []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionUnknown, "Pending")})
		}, "infra/edge"},
		{"listener without a status block", func(w *world) {
			testutil.AddSpecListener(w.objs[1], "https")
		}, "infra/edge"},
		{"status only for another port", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw443}, testutil.AcceptedBy(gw, testutil.Controller))
		}, "shop/cart"},
		{"observedGeneration 0", func(w *world) {
			p := testutil.AcceptedBy(gw, testutil.Controller)
			for i := range p.Conds {
				p.Conds[i].ObservedGeneration = 0
			}
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, p)
		}, "shop/cart"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := healthyWorld()
			tc.mutate(w)
			r := w.report(t)
			if !has(r, CodeRouteUnattached, SeverityWarning, tc.res) || has(r, CodeConditionFalse, "", tc.res) || r.State != StateWarning {
				t.Fatalf("want only 'not yet processed' (warning) on %s: state=%s findings=%+v", tc.res, r.State, r.Findings)
			}
		})
	}
}

// Only status entries for current parentRefs of this install count.
// An entry left behind for a removed parentRef is ignored.
func TestStatusForRemovedParentIsIgnored(t *testing.T) {
	w := healthyWorld()
	gw, old := testutil.GwRef("infra", "edge"), testutil.GwRef("infra", "old")
	w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller),
		testutil.RouteParent{Ref: old, Controller: testutil.Controller, Conds: []metav1.Condition{
			testutil.Cond("Accepted", metav1.ConditionFalse, "NoMatchingParent"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}})
	if r := w.report(t); r.State != StateHealthy || len(r.Findings) != 0 {
		t.Fatalf("status for a removed parentRef must be ignored: state=%s findings=%+v", r.State, r.Findings)
	}
}

// A Gateway name over 63 characters has a truncated proxy label;
// the proxy is matched through its ownerReference.
func TestLongGatewayNameMatchesProxyByOwner(t *testing.T) {
	w := healthyWorld()
	long := "edge-a-very-long-gateway-name-that-exceeds-the-sixty-three-character-label-limit"
	w.objs[1] = testutil.Gateway("infra", long, "kgateway", nil)
	p := testutil.ProxyDeployment("infra", long, 1, 1)
	p.Labels["gateway.networking.k8s.io/gateway-name"] = long[:52] + "-0123456789"
	p.Name = long[:40]
	w.deploys[1] = p
	w.objs[2] = testutil.HTTPRoute("shop", "cart", nil)
	r := w.report(t)
	for _, f := range r.Findings {
		if f.Code == CodeProxyNotReady {
			t.Fatalf("proxy must be found via ownerReference: %+v", f)
		}
	}
}

func count(r Report, code, res string) int {
	n := 0
	for _, f := range r.Findings {
		if f.Code == code && f.Resource != nil && f.Resource.String() == res {
			n++
		}
	}
	return n
}

// A missing parent of a kind the cache cannot read may only be missing from
// the cache, so it is not reported as "parent not found".
func TestUnattachedParentOfUnreadableKindIsNotReported(t *testing.T) {
	w := healthyWorld()
	w.objs = append(w.objs, testutil.HTTPRoute("shop", "orphan", []objects.ParentRef{testutil.GwRef("infra", "gone")}))
	gws := testutil.ServedUpstream()[1].GVR()
	if gws.Resource != "gateways" {
		t.Fatalf("fixture: %v", gws)
	}
	w.access = map[schema.GroupVersionResource]cache.Access{gws: {Err: "watch failed"}}
	r := w.report(t)
	if has(r, CodeRouteUnattached, "", "shop/orphan") {
		t.Fatalf("parent of an unreadable kind must not be 'not found': %+v", r.Findings)
	}
	if r.State != StateUnknown {
		t.Fatalf("state: want unknown, got %s", r.State)
	}
}

// Policy ancestor status counts only for Gateways this install owns; the
// controller name alone is shared between installs.
func TestPolicyAncestorOfUnownedGatewayIsIgnored(t *testing.T) {
	gw, other := testutil.GwRef("infra", "edge"), testutil.GwRef("infra", "elsewhere")
	invalid := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "Invalid")}
	valid := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Valid")}

	w := healthyWorld()
	w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp",
		testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller, Conds: valid},
		testutil.PolicyAncestor{Ref: other, Controller: testutil.Controller, Conds: invalid}))
	if r := w.report(t); r.State != StateHealthy || len(r.Findings) != 0 {
		t.Fatalf("status for another install's Gateway must be ignored: state=%s findings=%+v", r.State, r.Findings)
	}

	// Status only for Gateways this install does not own: the policy belongs elsewhere.
	w = healthyWorld()
	w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: other, Controller: testutil.Controller, Conds: invalid}))
	if r := w.report(t); r.State != StateHealthy || len(r.Findings) != 0 {
		t.Fatalf("a policy processed for another install's Gateway gets no finding here: state=%s findings=%+v", r.State, r.Findings)
	}
}

// Two installs share a controllerName with disjoint Gateway namespaces. A
// policy in both scopes that targets only A's Gateway is healthy in A and
// absent from B: no finding there and not owned.
func TestPolicyOfSharedControllerNameIsAbsentFromOtherInstall(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("infra-a", map[string]string{"a": "yes"}), testutil.Namespace("infra-b", map[string]string{"b": "yes"}),
		testutil.Namespace("shop", map[string]string{"a": "yes", "b": "yes"})}
	ca, cb := testutil.ControllerDeployment("kgw-a", "kgateway", img, 1, 1), testutil.ControllerDeployment("kgw-b", "kgateway", img, 1, 1)
	pods := []*corev1.Pod{testutil.ControllerPod(ca, "p", true), testutil.ControllerPod(cb, "p", true)}
	objs := []*unstructured.Unstructured{testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.Gateway("infra-a", "edge", "kgateway", nil), testutil.Gateway("infra-b", "edge", "kgateway", nil),
		testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: testutil.GwRef("infra-a", "edge"), Controller: testutil.Controller,
			Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Valid")}})}
	snap := &cache.Snapshot{Objects: testutil.ObjectsByGVR(t, objs...), Access: map[schema.GroupVersionResource]cache.Access{},
		Namespaces: nss, Pods: pods, Synced: true, TakenAt: testutil.Now,
		Deployments: []*appsv1.Deployment{ca, cb, testutil.ProxyDeployment("infra-a", "edge", 1, 1), testutil.ProxyDeployment("infra-b", "edge", 1, 1)}}
	served := testutil.ServedUpstream()
	for _, k := range served {
		snap.Access[k.GVR()] = cache.Access{OK: true}
	}
	for _, gvr := range []schema.GroupVersionResource{cache.NamespacesGVR, cache.PodsGVR, cache.DeploymentsGVR} {
		snap.Access[gvr] = cache.Access{OK: true}
	}
	var insts []install.Resolved
	for _, c := range []struct {
		d     *appsv1.Deployment
		scope string
	}{{ca, `[{"matchLabels":{"a":"yes"}}]`}, {cb, `[{"matchLabels":{"b":"yes"}}]`}} {
		in, _ := install.UpstreamDetector{}.Detect(c.d)
		insts = append(insts, install.Resolved{Install: in, Scope: install.ParseSelectors(c.scope)})
	}
	a := install.Assign(snap, served, insts)
	for _, res := range insts {
		hi := Input{Install: res, Owned: a.PerInstall[res.ID()], Assignment: a, Snapshot: snap, Served: served, Now: testutil.Now,
			MetricsMaxAge: 30 * time.Second, Verify: scopeverify.Result{Available: true}, HasMetrics: true,
			Metrics: ctrlmetrics.Result{CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{}, ManagedPresent: true}}}
		r := Evaluate(hi, Upstream())
		if r.State != StateHealthy || len(r.Findings) != 0 {
			t.Fatalf("%s: state=%s findings=%+v", res.ID(), r.State, r.Findings)
		}
		if n := len(hi.Owned.Policies); (res.Namespace == "kgw-a") != (n == 1) {
			t.Fatalf("%s owns %d policies", res.ID(), n)
		}
	}
}

// KGWUI-003 is one per (resource, parentRef), even when spec repeats a parentRef.
func TestNotProcessedDedupesRepeatedParentRef(t *testing.T) {
	w := healthyWorld()
	gone, gw := testutil.GwRef("infra", "gone"), testutil.GwRef("infra", "edge")
	w.objs = append(w.objs,
		testutil.HTTPRoute("shop", "twice-missing", []objects.ParentRef{gone, gone}),
		testutil.HTTPRoute("shop", "twice-owned", []objects.ParentRef{gw, gw}))
	r := w.report(t)
	if n, m := count(r, CodeRouteUnattached, "shop/twice-missing"), count(r, CodeRouteUnattached, "shop/twice-owned"); n != 1 || m != 1 {
		t.Fatalf("want one KGWUI-003 per (resource, parentRef), got %d and %d: %+v", n, m, r.Findings)
	}
}

// A selector env set through valueFrom leaves the scope unknown, which
// surfaces as KGWUI-008 and an unknown install.
func TestScopeFromValueFromIsSignalUnavailable(t *testing.T) {
	w := healthyWorld()
	w.ctrl.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: install.DiscoveryNamespaceSelectorsEnv,
		ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{Key: "selectors"}}}}
	w.pods = []*corev1.Pod{testutil.ControllerPod(w.ctrl, "p", true)}
	w.fromPod = true
	r := w.report(t)
	found := false
	for _, f := range r.Findings {
		if f.Code == CodeSignalUnavailable && strings.Contains(f.Detail, "valueFrom") {
			found = true
		}
	}
	if !found || r.State != StateUnknown {
		t.Fatalf("state=%s findings=%+v", r.State, r.Findings)
	}
	for _, c := range r.Checks {
		if c.Name == "controller scope" && c.OK {
			t.Fatalf("controller scope check must not be OK: %+v", c)
		}
	}
}

// Findings come in a fixed order: severity, code, resource, parentRef, so the
// UI list does not reorder between polls.
func TestFindingsOrderIsDeterministic(t *testing.T) {
	build := func() *world {
		w := healthyWorld()
		w.objs = append(w.objs,
			testutil.HTTPRoute("shop", "b", []objects.ParentRef{testutil.GwRef("infra", "gone-2"), testutil.GwRef("infra", "gone-1")}),
			testutil.HTTPRoute("shop", "a", []objects.ParentRef{testutil.GwRef("infra", "gone-3")}),
			testutil.HTTPRoute("shop", "c", []objects.ParentRef{withSection("infra", "gone-4", "z"), withSection("infra", "gone-4", "a")}))
		w.ctrl.Status.ReadyReplicas = 0
		w.access = map[schema.GroupVersionResource]cache.Access{}
		for _, k := range kinds.ByCategory(testutil.ServedUpstream(), kinds.CategoryPolicy) {
			w.access[k.GVR()] = cache.Access{Err: "forbidden"}
		}
		return w
	}
	first := build().report(t)
	for i := 0; i < 20; i++ {
		if got := build().report(t); !reflect.DeepEqual(got.Findings, first.Findings) {
			t.Fatalf("order changed between runs:\n%+v\n%+v", first.Findings, got.Findings)
		}
	}
	var got []string
	for _, f := range first.Findings {
		k := string(f.Severity) + " " + f.Code
		if f.Resource != nil {
			k += " " + f.Resource.String()
		}
		if f.Code == CodeRouteUnattached {
			k += " " + f.parent.Name + " " + f.parent.SectionName
		}
		got = append(got, k)
	}
	want := []string{
		"error KGWUI-005 kgw/kgateway",
		"warning KGWUI-003 shop/a gone-3 ",
		"warning KGWUI-003 shop/b gone-1 ",
		"warning KGWUI-003 shop/b gone-2 ",
		"warning KGWUI-003 shop/c gone-4 a",
		"warning KGWUI-003 shop/c gone-4 z",
	}
	for range kinds.ByCategory(testutil.ServedUpstream(), kinds.CategoryPolicy) {
		want = append(want, "info KGWUI-008")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order:\n got %q\nwant %q", got, want)
	}
	for i := 1; i < len(first.Findings); i++ {
		if a, b := first.Findings[i-1], first.Findings[i]; a.Code == CodeSignalUnavailable && b.Code == CodeSignalUnavailable && a.Message > b.Message {
			t.Fatalf("KGWUI-008 findings not ordered: %q before %q", a.Message, b.Message)
		}
	}
}

func withSection(ns, name, section string) objects.ParentRef {
	p := testutil.GwRef(ns, name)
	p.SectionName = section
	return p
}

// Every method of the unexported check types carries a doc comment.
func TestCheckMethodsAreDocumented(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "checks.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv != nil && fn.Doc == nil {
			t.Errorf("method %s has no doc comment", fn.Name.Name)
		}
	}
}

// backendAncestor is the ancestorRef kgateway writes for a policy attached to
// a backend: the backend itself, in the core group.
func backendAncestor(t *testing.T, u *unstructured.Unstructured) *unstructured.Unstructured {
	t.Helper()
	as, _, _ := unstructured.NestedSlice(u.Object, "status", "ancestors")
	for _, a := range as {
		a.(map[string]any)["ancestorRef"] = map[string]any{"group": "", "kind": "Service", "namespace": "shop", "name": "svc"}
	}
	if err := unstructured.SetNestedSlice(u.Object, as, "status", "ancestors"); err != nil {
		t.Fatal(err)
	}
	return u
}

// A policy attached to a backend has the backend as ancestor; its status
// counts by controller name, so a rejected backend policy is an error.
func TestBackendPolicyStatusCounts(t *testing.T) {
	invalid := testutil.PolicyAncestor{Controller: testutil.Controller, Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "Invalid")}}
	for _, p := range []*unstructured.Unstructured{
		testutil.Policy("gateway.kgateway.dev/v1alpha1", "BackendConfigPolicy", "shop", "bcp", invalid),
		testutil.BackendTLSPolicy("shop", "bcp", invalid),
	} {
		t.Run(p.GetKind(), func(t *testing.T) {
			w := healthyWorld()
			w.objs = append(w.objs, backendAncestor(t, p))
			r := w.report(t)
			if !has(r, CodeConditionFalse, SeverityError, "shop/bcp") || r.State != StateError {
				t.Fatalf("state=%s findings=%+v", r.State, r.Findings)
			}
			for _, f := range r.Findings {
				if f.Code == CodeConditionFalse && !strings.Contains(f.Detail, "ancestor Service shop/svc") {
					t.Fatalf("detail must name the ancestor with its namespace: %q", f.Detail)
				}
			}
		})
	}
}

// A missing proxy gets the same grace as status: from when the engine first
// saw the Gateway's generation, not from its creation. Inside it the proxy
// check is pending, so the install is unknown, not healthy (D-017 §3).
func TestMissingProxyGraceStartsAtFirstSeen(t *testing.T) {
	w := healthyWorld()
	w.deploys = w.deploys[:1]
	w.firstSeen = func(*unstructured.Unstructured) time.Time { return testutil.Now.Add(-10 * time.Second) }
	r := w.report(t)
	if has(r, CodeProxyNotReady, "", "infra/edge") {
		t.Fatalf("a proxy missing inside the grace period is not reported: %+v", r.Findings)
	}
	if r.State != StateUnknown {
		t.Fatalf("state: want unknown inside the grace period, got %s", r.State)
	}
	for _, c := range r.Checks {
		if c.Name == "gateway proxies" && (!c.Pending || c.OK || c.Error != "1 proxy is being deployed") {
			t.Fatalf("proxy check must be pending: %+v", c)
		}
	}
}

// Access findings name the resource with its group, kubectl style.
func TestAccessFindingNamesGroup(t *testing.T) {
	w := healthyWorld()
	w.access = map[schema.GroupVersionResource]cache.Access{testutil.ServedUpstream()[1].GVR(): {Err: "forbidden"}, cache.NamespacesGVR: {Err: "forbidden"}}
	r := w.report(t)
	msgs := map[string]bool{}
	for _, f := range r.Findings {
		msgs[f.Message] = true
	}
	if !msgs["No access to gateways.gateway.networking.k8s.io"] || !msgs["No access to namespaces"] {
		t.Fatalf("messages: %v", msgs)
	}
}

// Replica counts in KGWUI-005 and KGWUI-006 are workload wording (D-016). Only
// the zero Info defaults to Kubernetes; an unnamed Info is judged by its capabilities.
func TestReadinessWordingFollowsWorkloads(t *testing.T) {
	cases := []struct {
		name       string
		plat       platform.Info
		ctrl, prox string
	}{
		{"zero is Kubernetes", platform.Info{}, "Controller not ready (0 of 1 replicas)", "Proxy not ready (1 of 2 replicas)"},
		{"Kubernetes", platform.Kubernetes(), "Controller not ready (0 of 1 replicas)", "Proxy not ready (1 of 2 replicas)"},
		{"named without workloads", platform.Info{Name: "Test"}, "Controller not ready", "Proxy not ready"},
		{"unnamed with workloads", platform.Info{Capabilities: []string{platform.Workloads}}, "Controller not ready (0 of 1 replicas)", "Proxy not ready (1 of 2 replicas)"},
		{"unnamed without workloads", platform.Info{Capabilities: []string{platform.Scope}}, "Controller not ready", "Proxy not ready"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := healthyWorld()
			w.platform = tc.plat
			w.ctrl.Status.ReadyReplicas = 0
			w.deploys[1] = testutil.ProxyDeployment("infra", "edge", 2, 1)
			got := map[string]string{}
			for _, f := range w.report(t).Findings {
				got[f.Code] = f.Message
			}
			if got[CodeControllerNotReady] != tc.ctrl || got[CodeProxyNotReady] != tc.prox {
				t.Fatalf("want %q and %q, got %q and %q", tc.ctrl, tc.prox, got[CodeControllerNotReady], got[CodeProxyNotReady])
			}
		})
	}
}
