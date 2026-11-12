package engine

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// A scoped snapshot with Services denied in b only: the Service backend in a
// is judged as on a cluster-wide snapshot, the one in b is never judged and
// the check says it is not available.
func TestDeniedNamespaceIsUnknown(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	resolved := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}
	route := testutil.HTTPRoute("shop", "web", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: resolved})
	route.Object["spec"].(map[string]any)["rules"] = []any{map[string]any{"backendRefs": []any{
		map[string]any{"name": "web", "namespace": "a", "port": int64(8080)},
		map[string]any{"name": "web", "namespace": "b", "port": int64(8080)}}}}

	snap := snapshot(t, route)
	ok, denied := cache.Access{OK: true}, cache.Access{Err: `services is forbidden: User "u" cannot list resource "services" in namespace "b"`, Denied: true}
	// b's Service and slice are in the snapshot, so only the access detail can keep b from a finding.
	for _, ns := range []string{"a", "b"} {
		snap.Services = append(snap.Services, cache.ServiceFact{Namespace: ns, Name: "web", Type: "ClusterIP", Ports: []cache.ServicePort{{Name: "http", Port: 8080, Target: 8080}}})
		snap.EndpointSlices = append(snap.EndpointSlices, cache.SliceFact{Namespace: ns, Service: "web", Ports: []cache.SlicePort{{Name: "http", Port: 8080}}, Ready: []int{0}, Total: 1})
	}
	snap.RequestedNamespaces = []string{"a", "b"}
	snap.Access[cache.ServicesGVR], snap.Access[cache.EndpointSlicesGVR] = denied, ok
	snap.NamespaceAccess = map[schema.GroupVersionResource]map[string]cache.Access{
		cache.ServicesGVR: {"a": ok, "b": denied}, cache.EndpointSlicesGVR: {"a": ok, "b": ok}}

	now := testutil.Now
	e := New(config(snap, metricsAt(testutil.Now), func() time.Time { return now }))
	e.Compute()
	now = now.Add(health.EndpointsHold + health.Grace + time.Second)
	rep := e.Compute().Installs[0].Health

	if !hasFinding(rep, health.CodeBackendNoReadyEndpoints, "a/web", "") {
		t.Fatalf("the Service in a is judged as today: %v", neutralFindings(rep))
	}
	if hasFinding(rep, health.CodeBackendNoReadyEndpoints, "b/web", "") {
		t.Fatalf("the Service in denied b is judged: %v", neutralFindings(rep))
	}
	if chk := checkOf(rep, "backend endpoints"); chk == nil || chk.OK || chk.Unavailable != health.AvailDenied ||
		chk.Error != "Services or EndpointSlices are not available in namespaces b" {
		t.Fatalf("backend endpoints check %+v, want not available (denied) naming b", chk)
	}

	// With every requested namespace unreadable, nothing is judged, as for a cluster-wide denial.
	snap.NamespaceAccess[cache.ServicesGVR] = map[string]cache.Access{"a": denied, "b": denied}
	now = testutil.Now
	e = New(config(snap, metricsAt(testutil.Now), func() time.Time { return now }))
	e.Compute()
	now = now.Add(health.EndpointsHold + health.Grace + time.Second)
	if rep := e.Compute().Installs[0].Health; hasFinding(rep, health.CodeBackendNoReadyEndpoints, "", "") {
		t.Fatalf("all namespaces denied: %v", neutralFindings(rep))
	}

	// Every requested namespace readable, refs to c, which was never requested: c is
	// neither judged nor explained, though c/web is absent from the snapshot.
	snap.Access[cache.ServicesGVR] = ok
	snap.NamespaceAccess[cache.ServicesGVR] = map[string]cache.Access{"a": ok, "b": ok}
	route.Object["spec"].(map[string]any)["rules"] = []any{map[string]any{"backendRefs": []any{
		map[string]any{"name": "web", "namespace": "c", "port": int64(8080)}}}}
	unresolved := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}
	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: unresolved})
	broken.Object["spec"].(map[string]any)["rules"] = []any{map[string]any{"backendRefs": []any{
		map[string]any{"name": "web", "namespace": "c", "port": int64(8080)}}}}
	scoped := snapshot(t, route, broken)
	scoped.Services, scoped.EndpointSlices = snap.Services, snap.EndpointSlices
	scoped.RequestedNamespaces, scoped.NamespaceAccess = snap.RequestedNamespaces, snap.NamespaceAccess
	scoped.Access[cache.ServicesGVR], scoped.Access[cache.EndpointSlicesGVR] = ok, ok
	now = testutil.Now
	e = New(config(scoped, metricsAt(testutil.Now), func() time.Time { return now }))
	e.Compute()
	now = now.Add(health.EndpointsHold + health.Grace + time.Second)
	rep = e.Compute().Installs[0].Health
	if chk := checkOf(rep, "backend endpoints"); chk == nil || chk.OK || chk.Unavailable != health.AvailNotObserved ||
		chk.Error != "Services or EndpointSlices are not available in namespaces c" {
		t.Fatalf("backend endpoints check %+v, want not available naming c", chk)
	}
	var refs bool
	for _, f := range rep.Findings {
		if f.Code != health.CodeRefUnresolved {
			continue
		}
		refs = true
		if ev, _ := f.Facts.(*health.RefsEvidence); ev == nil || ev.Explanation == health.RefExplainServiceMissing {
			t.Fatalf("a Service in an unrequested namespace is explained: %+v", f)
		}
	}
	if !refs {
		t.Fatalf("no KGWUI-013 for shop/broken: %v", neutralFindings(rep))
	}
}

// checkOf is rep's result of check name, or nil.
func checkOf(rep health.Report, name string) *health.CheckResult {
	for i := range rep.Checks {
		if rep.Checks[i].Name == name {
			return &rep.Checks[i]
		}
	}
	return nil
}

// A class-only install's controller is never seen. In a scoped snapshot that is
// "controller not observed"; in a cluster-wide one its scope stays assumed.
func TestClassOnlyNotObservedWhenScoped(t *testing.T) {
	for _, tc := range []struct {
		requested []string
		want      string
	}{
		{nil, install.SourceAssumed},
		{[]string{"infra", "shop"}, install.SourceNotObserved},
	} {
		snap := twoControllers(t)
		snap.RequestedNamespaces = tc.requested
		m := New(classOnlyConfig(snap, true)).Compute()
		var found bool
		for _, i := range m.Installs {
			if i.Resolved.Impl != "" {
				if i.Resolved.Scope.Source == install.SourceNotObserved {
					t.Fatalf("detected install %s reports %q", i.Resolved.ID(), i.Resolved.Scope.Source)
				}
				continue
			}
			found = true
			if s := i.Resolved.Scope; s.Source != tc.want || s.Mode != install.ScopeAll {
				t.Fatalf("requested %v: class-only scope %+v, want source %q", tc.requested, s, tc.want)
			}
			if i.Owned == nil || len(i.Owned.Gateways) != 1 {
				t.Fatalf("requested %v: assignment still runs on what is visible: %+v", tc.requested, i.Owned)
			}
		}
		if !found {
			t.Fatal("no class-only install")
		}
	}
}
