package objects

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// BackendView is the decoded kgateway Backend.
type BackendView struct {
	Type       string
	Conditions []metav1.Condition
}

// RouteHostnames holds spec.hostnames of a GRPCRoute or TLSRoute.
type RouteHostnames struct {
	Hostnames []string
}

// DecodeBackend decodes a kgateway Backend. It never fails.
func DecodeBackend(u *unstructured.Unstructured) *BackendView {
	t, _ := field(u, "spec", "type").(string)
	return &BackendView{Type: t, Conditions: Conditions(u)}
}

// DecodeRouteHostnames decodes spec.hostnames of any route kind. It never fails.
func DecodeRouteHostnames(u *unstructured.Unstructured) *RouteHostnames {
	return &RouteHostnames{Hostnames: stringList(field(u, "spec", "hostnames"))}
}
