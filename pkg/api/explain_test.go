package api

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// explainWorld is world plus routes on infra/edge whose ResolvedRefs is
// False: shop/denied names store/cart (RefNotPermitted, one candidate);
// shop/twice names shop/dup-svc twice (BackendNotFound, both exist);
// shop/stale names shop/stale-svc with status one generation behind;
// shop/mixed names the missing shop/gone-svc and shop/peer-svc, which
// exists; shop/fine names store/cart and resolves. extra joins them.
func explainWorld(extra ...runtime.Object) []runtime.Object {
	svc := func(name string) *corev1.Service {
		return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: name, UID: types.UID("svc-shop-" + name)},
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Ports: []corev1.ServicePort{{Name: "http", Port: 80}}}}
	}
	cart := map[string]any{"name": "cart", "namespace": "store", "port": int64(80)}
	stale := refsRouteAs("stale", "BackendNotFound", "", svcRef("stale-svc"))
	stale.SetGeneration(2)
	fine := testutil.WithRules(testutil.HTTPRoute("shop", "fine", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		map[string]any{"backendRefs": []any{cart}})
	return world(append([]runtime.Object{refsRouteAs("denied", "RefNotPermitted", "", cart), refsRouteAs("twice", "BackendNotFound", "", svcRef("dup-svc"), svcRef("dup-svc")),
		stale, refsRouteAs("mixed", "BackendNotFound", "", svcRef("gone-svc"), svcRef("peer-svc")), fine,
		svc("dup-svc"), svc("stale-svc"), svc("peer-svc")}, extra...)...)
}

// explainSetup reads endpoints from a fixed reader, so the golden's read time is fixed.
func explainSetup(authz Authorizer) setup {
	return setup{endpoints: true, authz: authz, catalog: fixCatalog(nil), opts: []HandlerOption{WithScopedReader(&countingReader{})}}
}

func explanations(t *testing.T, srv *httptest.Server, ns, name string) []RefExplanationDTO {
	t.Helper()
	return get[BackendDetail](t, srv, backendsURL+"/-/Service/"+ns+"/"+name, http.StatusOK).Explanations
}

// §10: a 014 left with one candidate is attributed, with the route's reason
// and the ReferenceGrant the finding suggests.
func TestRefExplanationAttributed(t *testing.T) {
	_, srv, _ := startWith(t, explainSetup(nil), explainWorld()...)
	got := explanations(t, srv, "store", "cart")
	if len(got) != 1 {
		t.Fatalf("store/cart explanations %+v", got)
	}
	e := got[0]
	if e.Route.Name != "denied" || e.Kind != RefAttributed || e.Reason != "RefNotPermitted" || e.Rule != 0 || e.Ref != 0 ||
		e.Path != "spec.rules[0].backendRefs[0]" || e.Explanation != refExplainNotPermitted {
		t.Fatalf("explanation %+v", e)
	}
	want := fixDetail(t, srv, health.CodeRefNotPermitted, "shop", "denied").SuggestedFix
	if want == nil || !reflect.DeepEqual(e.Fix, want) {
		t.Fatalf("fix %+v, want the finding's %+v", e.Fix, want)
	}
}

// §10: a route that names the backend twice cannot single one out, so each
// ref is a candidate quoting the route's reason, with no fix.
func TestRefExplanationCandidates(t *testing.T) {
	_, srv, _ := startWith(t, explainSetup(nil), explainWorld()...)
	got := explanations(t, srv, "shop", "dup-svc")
	if len(got) != 2 {
		t.Fatalf("dup-svc explanations %+v", got)
	}
	for k, e := range got {
		if e.Route.Name != "twice" || e.Kind != RefCandidate || e.Reason != "BackendNotFound" || e.Rule != 0 || e.Ref != k ||
			e.Explanation != "This route reports `ResolvedRefs=False` (BackendNotFound) for one of these refs." || e.Fix != nil {
			t.Fatalf("explanation %d: %+v", k, e)
		}
	}
}

// §10, D-014: status older than the spec is waiting for the controller, never a failure.
func TestRefExplanationWaiting(t *testing.T) {
	_, srv, _ := startWith(t, explainSetup(nil), explainWorld()...)
	got := explanations(t, srv, "shop", "stale-svc")
	if len(got) != 1 || got[0].Kind != RefWaiting || got[0].Explanation != refExplainWaiting || got[0].Reason != "" || got[0].Fix != nil {
		t.Fatalf("stale-svc explanations %+v", got)
	}
}

// §10: a Service explanation is attributed in plain words.
func TestRefExplanationServiceMissing(t *testing.T) {
	_, srv, _ := startWith(t, explainSetup(nil), explainWorld()...)
	got := explanations(t, srv, "shop", "gone-svc")
	if len(got) != 1 || got[0].Kind != RefAttributed || got[0].Explanation != "Service `shop/gone-svc` does not exist." || got[0].Reason != "BackendNotFound" {
		t.Fatalf("gone-svc explanations %+v", got)
	}
	if got := explanations(t, srv, "shop", "peer-svc"); len(got) != 0 {
		t.Fatalf("peer-svc resolves, explanations %+v", got)
	}
}

// §13: an explanation names its route, so a hidden route's is left out; an
// attribution that rests on a hidden Service quotes only the route's status.
func TestRefExplanationRestricted(t *testing.T) {
	_, srv, _ := startWith(t, explainSetup(hideKinds("HTTPRoute", "denied")), explainWorld()...)
	if got := explanations(t, srv, "store", "cart"); len(got) != 0 {
		t.Fatalf("hidden route explained: %+v", got)
	}
	_, srv, _ = startWith(t, explainSetup(hideKinds("Service", "peer-svc")), explainWorld()...)
	got := explanations(t, srv, "shop", "gone-svc")
	if len(got) != 1 || got[0].Kind != RefCandidate || got[0].Reason != "BackendNotFound" || got[0].Fix != nil ||
		got[0].Explanation != "This route reports `ResolvedRefs=False` (BackendNotFound) for one of these refs." {
		t.Fatalf("gone-svc with peer-svc hidden: %+v", got)
	}
}

// §10: a mirror is attributed at its own path, a backendRef's mirror with
// that ref's index and a rule filter's with -1.
func TestRefExplanationMirrors(t *testing.T) {
	onRef := svcRef("main-svc")
	onRef["filters"] = []any{mirrorFilter("mir-a-svc")}
	viaRef := refsRouteAs("mirror-ref", "BackendNotFound", "", onRef)
	viaRule := refsRouteAs("mirror-rule", "BackendNotFound", "", svcRef("main-svc"))
	viaRule.Object["spec"].(map[string]any)["rules"].([]any)[0].(map[string]any)["filters"] = []any{mirrorFilter("mir-b-svc")}
	main := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "main-svc", UID: "svc-shop-main-svc"},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Ports: []corev1.ServicePort{{Name: "http", Port: 80}}}}
	_, srv, _ := startWith(t, explainSetup(nil), explainWorld(viaRef, viaRule, main)...)
	for _, tc := range []struct {
		svc, route, path string
		ref              int
	}{
		{"mir-a-svc", "mirror-ref", "spec.rules[0].backendRefs[0].filters[0].requestMirror.backendRef", 0},
		{"mir-b-svc", "mirror-rule", "spec.rules[0].filters[0].requestMirror.backendRef", -1},
	} {
		got := explanations(t, srv, "shop", tc.svc)
		if len(got) != 1 || got[0].Kind != RefAttributed || got[0].Route.Name != tc.route || got[0].Path != tc.path || got[0].Ref != tc.ref ||
			got[0].Explanation != "Service `shop/"+tc.svc+"` does not exist." {
			t.Fatalf("%s explanations %+v", tc.svc, got)
		}
	}
}
