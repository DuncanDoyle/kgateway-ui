// Package proxymode decides whether the controller runs a Gateway's proxy as a
// Deployment or the Gateway is self-managed (design §14.3 H5). It mirrors
// controller logic, so it sits behind Resolver until the controller reports
// the outcome itself (design §14.6).
package proxymode

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Mode is how a Gateway's proxy is run.
type Mode string

// Modes.
const (
	// Managed means the controller deploys the proxy.
	Managed Mode = "managed"
	// SelfManaged means the proxy runs outside the controller's Deployments.
	SelfManaged Mode = "self-managed"
	// Unresolved means a referenced GatewayParameters could not be resolved;
	// the UI keeps the managed behaviour and kgateway reports InvalidParameters.
	Unresolved Mode = "unresolved"
)

// Resolver decides the Mode of an owned Gateway from the snapshot.
type Resolver interface {
	Resolve(gw *unstructured.Unstructured, snap *cache.Snapshot, served []kinds.Served) Mode
}

// GatewayParameters is the upstream Resolver. It follows kgateway's merge
// (kgateway 556b4ce, pkg/deployer/merge.go and
// pkg/kgateway/deployer/gateway_parameters.go): the GatewayClass parameters,
// then the Gateway's infrastructure parameters, where a selfManaged layer wins
// and a later kube layer does not undo it.
type GatewayParameters struct{}

const paramsKind = "GatewayParameters"

// Resolve implements Resolver.
func (GatewayParameters) Resolve(gw *unstructured.Unstructured, snap *cache.Snapshot, served []kinds.Served) Mode {
	class := find(snap, served, objects.GatewayAPIGroup, "GatewayClass", "", objects.GatewayClassName(gw))
	if class == nil {
		return Unresolved // kgateway cannot resolve parameters without the class either
	}
	classSelf := false
	if r, ok := paramsRef(class, "spec", "parametersRef"); ok {
		// Like kgateway, a class reference is not checked for group or kind.
		p := r.lookup(snap, served, r.namespace)
		if p == nil {
			return Unresolved
		}
		classSelf = selfManaged(p)
	}
	if r, ok := paramsRef(gw, "spec", "infrastructure", "parametersRef"); ok {
		// Gateway parameters live in the Gateway's namespace.
		p := r.resolve(snap, served, gw.GetNamespace())
		if p == nil {
			return Unresolved
		}
		if selfManaged(p) {
			return SelfManaged
		}
	}
	if classSelf {
		return SelfManaged
	}
	return Managed
}

type ref struct{ group, kind, namespace, name string }

// paramsRef reads the parametersRef at path, if there is one.
func paramsRef(u *unstructured.Unstructured, path ...string) (ref, bool) {
	v, found, _ := unstructured.NestedFieldNoCopy(u.Object, path...)
	m, isMap := v.(map[string]any)
	if !found || !isMap {
		return ref{}, false
	}
	s := func(k string) string { x, _ := m[k].(string); return x }
	return ref{s("group"), s("kind"), s("namespace"), s("name")}, true
}

// lookup returns the GatewayParameters r names in ns, or nil when r has no
// name or the object cannot be read.
func (r ref) lookup(snap *cache.Snapshot, served []kinds.Served, ns string) *unstructured.Unstructured {
	if r.name == "" {
		return nil
	}
	return find(snap, served, kinds.KgatewayGroup, paramsKind, ns, r.name)
}

// resolve is lookup for a Gateway reference, which must name GatewayParameters.
func (r ref) resolve(snap *cache.Snapshot, served []kinds.Served, ns string) *unstructured.Unstructured {
	if r.group != kinds.KgatewayGroup || r.kind != paramsKind {
		return nil
	}
	return r.lookup(snap, served, ns)
}

// find returns the object, or nil when its kind is not served or not readable,
// or the object is absent.
func find(snap *cache.Snapshot, served []kinds.Served, group, kind, ns, name string) *unstructured.Unstructured {
	for _, k := range served {
		if k.Group != group || k.Kind != kind {
			continue
		}
		if !snap.Access[k.GVR()].OK {
			return nil
		}
		for _, u := range snap.Objects[k.GVR()] {
			if u.GetNamespace() == ns && u.GetName() == name {
				return u
			}
		}
	}
	return nil
}

// selfManaged reports whether p sets spec.selfManaged (an empty object).
func selfManaged(p *unstructured.Unstructured) bool {
	v, found, _ := unstructured.NestedFieldNoCopy(p.Object, "spec", "selfManaged")
	return found && v != nil
}
