package cache

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

// Access keys of the Service and EndpointSlice watches.
var (
	ServicesGVR       = schema.GroupVersionResource{Version: "v1", Resource: "services"}
	EndpointSlicesGVR = schema.GroupVersionResource{Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices"}
)

// ServiceFact is what the cache keeps of a Service.
type ServiceFact struct {
	Namespace, Name, Type string
	Ports                 []ServicePort
}

// ServicePort is one Service port. Target is the numeric target port, the
// port itself when unset, and 0 when the target port is a name.
type ServicePort struct {
	Name         string
	Port, Target int32
}

// SlicePort is one EndpointSlice port; an unnamed port has the name "".
type SlicePort struct {
	Name string
	Port int32
}

// SliceFact is what the cache keeps of an EndpointSlice. A Service may have
// several, so slices are never merged. Ready counts the slice's ready
// endpoints per port name; an endpoint with ready unset counts as ready.
type SliceFact struct {
	Namespace, Service string
	Ports              []SlicePort
	Ready              map[string]int
}

// stripService keeps only the identity, type and ports of a Service.
func stripService(o any) (any, error) {
	svc, ok := o.(*corev1.Service)
	if !ok {
		return o, nil
	}
	out := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: svc.Namespace, Name: svc.Name, UID: svc.UID, ResourceVersion: svc.ResourceVersion},
		Spec: corev1.ServiceSpec{Type: svc.Spec.Type}}
	for _, p := range svc.Spec.Ports {
		out.Spec.Ports = append(out.Spec.Ports, corev1.ServicePort{Name: p.Name, Port: p.Port, TargetPort: p.TargetPort})
	}
	return out, nil
}

func serviceFact(svc *corev1.Service) ServiceFact {
	f := ServiceFact{Namespace: svc.Namespace, Name: svc.Name, Type: string(svc.Spec.Type)}
	for _, p := range svc.Spec.Ports {
		var target int32
		switch tp := p.TargetPort; {
		case tp.Type == intstr.String:
			// A named target port has no number here.
		case tp.IntVal == 0:
			target = p.Port
		default:
			target = tp.IntVal
		}
		f.Ports = append(f.Ports, ServicePort{Name: p.Name, Port: p.Port, Target: target})
	}
	return f
}

// stripSlice keeps the owning Service, the ports and each endpoint's ready
// condition; addresses, nodes and targets never reach the store.
func stripSlice(o any) (any, error) {
	es, ok := o.(*discoveryv1.EndpointSlice)
	if !ok {
		return o, nil
	}
	out := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Namespace: es.Namespace, Name: es.Name, UID: es.UID, ResourceVersion: es.ResourceVersion}}
	if svc, ok := es.Labels[discoveryv1.LabelServiceName]; ok {
		out.Labels = map[string]string{discoveryv1.LabelServiceName: svc}
	}
	for _, p := range es.Ports {
		out.Ports = append(out.Ports, discoveryv1.EndpointPort{Name: p.Name, Port: p.Port})
	}
	out.Endpoints = make([]discoveryv1.Endpoint, len(es.Endpoints))
	for i, e := range es.Endpoints {
		out.Endpoints[i].Conditions.Ready = e.Conditions.Ready
	}
	return out, nil
}

func sliceFact(es *discoveryv1.EndpointSlice) SliceFact {
	ready := 0
	for _, e := range es.Endpoints {
		if e.Conditions.Ready == nil || *e.Conditions.Ready {
			ready++
		}
	}
	f := SliceFact{Namespace: es.Namespace, Service: es.Labels[discoveryv1.LabelServiceName], Ready: make(map[string]int, len(es.Ports))}
	for _, p := range es.Ports {
		var name string
		if p.Name != nil {
			name = *p.Name
		}
		var port int32
		if p.Port != nil {
			port = *p.Port
		}
		f.Ports = append(f.Ports, SlicePort{Name: name, Port: port})
		f.Ready[name] = ready
	}
	return f
}

// serviceInformer watches every Service, cut down to a ServiceFact's fields.
func (c *Cache) serviceInformer(kube kubernetes.Interface) tracked {
	svcs := kube.CoreV1().Services("")
	t := c.informer(kube, &corev1.Service{},
		func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) { return svcs.List(ctx, o) },
		func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) { return svcs.Watch(ctx, o) })
	_ = t.inf.SetTransform(stripService) // fails only on a started informer
	return t
}

// sliceInformer watches every EndpointSlice, cut down to a SliceFact's fields.
func (c *Cache) sliceInformer(kube kubernetes.Interface) tracked {
	es := kube.DiscoveryV1().EndpointSlices("")
	t := c.informer(kube, &discoveryv1.EndpointSlice{},
		func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) { return es.List(ctx, o) },
		func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) { return es.Watch(ctx, o) })
	_ = t.inf.SetTransform(stripSlice) // fails only on a started informer
	return t
}
