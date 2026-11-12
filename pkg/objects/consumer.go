package objects

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

// Consumer is a non-route object that names a backend, with the role it uses
// the backend in and the JSON path of the reference.
type Consumer struct {
	Consumer   Ref
	Role       string
	BackendRef Ref
	// Port is the written backendRef port, 0 when unset.
	Port  int
	Field string
}

// DecodeBackendRefAt decodes the Gateway API backendRef object at path in u,
// with the core group, kind Service and u's namespace as defaults. ok is false
// when there is no such object or it names nothing. It never fails.
func DecodeBackendRefAt(u *unstructured.Unstructured, path ...string) (ref Ref, port int, ok bool) {
	var r rawObjRef
	if !decodeInto(field(u, path...), &r) || r.Name == "" {
		return Ref{}, 0, false
	}
	return r.ref("Service", u.GetNamespace()), int(deref(r.Port)), true
}
