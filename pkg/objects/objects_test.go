package objects

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func route() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "HTTPRoute",
		"metadata": map[string]any{"name": "cart", "namespace": "shop", "generation": int64(3)},
		"spec": map[string]any{"parentRefs": []any{
			map[string]any{"name": "edge", "namespace": "infra"},
			map[string]any{"name": "ls1", "kind": "ListenerSet", "sectionName": "https"},
		}},
		"status": map[string]any{"parents": []any{map[string]any{
			"parentRef":      map[string]any{"name": "edge", "namespace": "infra"},
			"controllerName": "kgateway.dev/kgateway",
			"conditions": []any{map[string]any{"type": "Accepted", "status": "True", "reason": "Accepted",
				"observedGeneration": int64(2), "lastTransitionTime": "2026-10-17T09:00:00Z", "message": ""}},
		}}},
	}}
}

func TestRouteParentRefsAppliesDefaults(t *testing.T) {
	got := RouteParentRefs(route())
	want := []ParentRef{
		{Group: "gateway.networking.k8s.io", Kind: "Gateway", Namespace: "infra", Name: "edge"},
		{Group: "gateway.networking.k8s.io", Kind: "ListenerSet", Namespace: "shop", Name: "ls1", SectionName: "https"},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRouteParentsAndStaleness(t *testing.T) {
	r := route()
	ps := RouteParents(r)
	if len(ps) != 1 || ps[0].ControllerName != "kgateway.dev/kgateway" || ps[0].ParentRef.Kind != "Gateway" {
		t.Fatalf("unexpected %+v", ps)
	}
	c := FindCondition(ps[0].Conditions, "Accepted")
	if c == nil || !IsStale(r, *c) {
		t.Fatal("observedGeneration 2 < generation 3 must be stale")
	}
}

// Round 2, finding 3: a condition without observedGeneration gives no evidence
// that it describes the current spec, so it is stale.
func TestMissingObservedGenerationIsStale(t *testing.T) {
	r := route()
	c := metav1.Condition{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 0}
	if !IsStale(r, c) {
		t.Fatal("observedGeneration 0 on an object at generation 3 must be stale")
	}
	c.ObservedGeneration = 3
	if IsStale(r, c) {
		t.Fatal("observedGeneration equal to generation is current")
	}
}

// Round 2, finding 5: the port is part of the parent identity for status
// matching, and unset differs from set; Ref() drops it.
func TestParentRefKeepsPort(t *testing.T) {
	u := route()
	u.Object["spec"] = map[string]any{"parentRefs": []any{
		map[string]any{"name": "edge", "namespace": "infra", "port": int64(443)},
		map[string]any{"name": "edge", "namespace": "infra"},
	}}
	got := RouteParentRefs(u)
	if got[0].Port != 443 || got[1].Port != 0 || got[0] == got[1] {
		t.Fatalf("port must be kept and distinguish parents: %+v", got)
	}
	if got[0].Ref() != got[1].Ref() {
		t.Fatal("Ref() identifies the Gateway, without port")
	}
}

func TestGatewaySpecListeners(t *testing.T) {
	gw := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "Gateway",
		"metadata": map[string]any{"name": "edge", "namespace": "infra"},
		"spec":     map[string]any{"listeners": []any{map[string]any{"name": "http"}, map[string]any{"name": "https"}}},
	}}
	if got := GatewaySpecListeners(gw); len(got) != 2 || got[0] != "http" || got[1] != "https" {
		t.Fatalf("spec listeners: %v", got)
	}
}

func TestRefOf(t *testing.T) {
	got := RefOf(route())
	if got != (Ref{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Namespace: "shop", Name: "cart"}) || got.String() != "shop/cart" {
		t.Fatalf("got %+v %q", got, got.String())
	}
}

// Assignment resolves parents by their full identity (finding 6): the group
// is part of the key, and the section name is not.
func TestParentRefIdentityKeepsGroup(t *testing.T) {
	got := RouteParentRefs(route())[1].Ref()
	if got != (Ref{Group: "gateway.networking.k8s.io", Kind: "ListenerSet", Namespace: "shop", Name: "ls1"}) {
		t.Fatalf("got %+v", got)
	}
	foreign := ParentRef{Group: "example.com", Kind: "Gateway", Namespace: "infra", Name: "edge"}
	if foreign.Ref() == RouteParentRefs(route())[0].Ref() {
		t.Fatal("a foreign-group Gateway must not share an identity with a Gateway API Gateway")
	}
}
