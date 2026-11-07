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

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Access keys of the Service and EndpointSlice watches.
var (
	ServicesGVR       = schema.GroupVersionResource{Version: "v1", Resource: "services"}
	EndpointSlicesGVR = schema.GroupVersionResource{Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices"}
)

// The kinds a check declares in its InputSet to read Snapshot.Services and Snapshot.EndpointSlices.
var (
	ServicesKind       = objects.GroupKind{Kind: "Service"}
	EndpointSlicesKind = objects.GroupKind{Group: "discovery.k8s.io", Kind: "EndpointSlice"}
)

// ServiceFact is what the cache keeps of a Service.
type ServiceFact struct {
	Namespace, Name, Type string
	Ports                 []ServicePort
}

// ServicePort is one Service port. Target is the numeric target port, the
// port itself when unset, and 0 when the target port is a name. Protocol
// is "TCP" when unset.
type ServicePort struct {
	Name         string
	Port, Target int32
	Protocol     string
}

// SlicePort is one EndpointSlice port; an unnamed port has the name "".
type SlicePort struct {
	Name string
	Port int32
}

// SliceFact is what the cache keeps of an EndpointSlice. A Service may have
// several, so slices are never merged. Ready[i] counts the slice's ready
// endpoints for Ports[i]; an endpoint with ready unset counts as ready.
// Total counts every endpoint, ready or not. AddressType is the slice's
// family, such as "IPv4"; a dual-stack Service has a slice per family.
type SliceFact struct {
	Namespace, Service string
	Ports              []SlicePort
	Ready              []int
	Total              int
	AddressType        string
}

// serviceEntry is what a Service store holds: the fact, the resource version
// the informer compares, and the fingerprint hash of (uid, resourceVersion).
type serviceEntry struct {
	print uint64
	rv    string
	fact  ServiceFact
}

// sliceEntry is serviceEntry for an EndpointSlice, with the slice's own name.
type sliceEntry struct {
	print    uint64
	rv, name string
	fact     SliceFact
}

// GetObjectMeta gives the informer the key and resource version it reads; it has no UID.
func (e *serviceEntry) GetObjectMeta() metav1.Object {
	return &metav1.ObjectMeta{Namespace: e.fact.Namespace, Name: e.fact.Name, ResourceVersion: e.rv}
}

// GetObjectMeta gives the informer the key and resource version it reads; it has no UID.
func (e *sliceEntry) GetObjectMeta() metav1.Object {
	return &metav1.ObjectMeta{Namespace: e.fact.Namespace, Name: e.name, ResourceVersion: e.rv}
}

func (e *serviceEntry) objPrint() uint64 { return e.print }
func (e *sliceEntry) objPrint() uint64   { return e.print }

// known returns the constant for a common enum value, so entries share its bytes.
func known(s string, consts ...string) string {
	for _, c := range consts {
		if s == c {
			return c
		}
	}
	return s
}

// stripService keeps only a Service's fact; labels, selector and IPs never reach the store.
func stripService(o any) (any, error) {
	svc, ok := o.(*corev1.Service)
	if !ok {
		return o, nil
	}
	return &serviceEntry{print: objHash(svc.UID, svc.ResourceVersion), rv: svc.ResourceVersion, fact: serviceFact(svc)}, nil
}

func serviceFact(svc *corev1.Service) ServiceFact {
	f := ServiceFact{Namespace: svc.Namespace, Name: svc.Name,
		Type: known(string(svc.Spec.Type), "ClusterIP", "NodePort", "LoadBalancer", "ExternalName"), Ports: make([]ServicePort, 0, len(svc.Spec.Ports))}
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
		proto := known(string(p.Protocol), "TCP", "UDP", "SCTP")
		if proto == "" {
			proto = string(corev1.ProtocolTCP)
		}
		f.Ports = append(f.Ports, ServicePort{Name: p.Name, Port: p.Port, Target: target, Protocol: proto})
	}
	return f
}

// stripSlice keeps only an EndpointSlice's fact; addresses, nodes and targets never reach the store.
func stripSlice(o any) (any, error) {
	es, ok := o.(*discoveryv1.EndpointSlice)
	if !ok {
		return o, nil
	}
	return &sliceEntry{print: objHash(es.UID, es.ResourceVersion), rv: es.ResourceVersion, name: es.Name, fact: sliceFact(es)}, nil
}

func sliceFact(es *discoveryv1.EndpointSlice) SliceFact {
	ready := 0
	for _, e := range es.Endpoints {
		if e.Conditions.Ready == nil || *e.Conditions.Ready {
			ready++
		}
	}
	f := SliceFact{Namespace: es.Namespace, Service: es.Labels[discoveryv1.LabelServiceName], Ports: make([]SlicePort, len(es.Ports)),
		Ready: make([]int, len(es.Ports)), Total: len(es.Endpoints), AddressType: known(string(es.AddressType), "IPv4", "IPv6", "FQDN")}
	for i, p := range es.Ports {
		if p.Name != nil {
			f.Ports[i].Name = *p.Name
		}
		if p.Port != nil {
			f.Ports[i].Port = *p.Port
		}
		// Every endpoint of a slice serves all its ports.
		f.Ready[i] = ready
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
