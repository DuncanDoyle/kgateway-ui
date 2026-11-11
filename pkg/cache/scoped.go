package cache

import (
	"context"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/DuncanDoyle/snellius/pkg/endpoints"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// NewScopedReader reads a Service's EndpointSlices with a paged, namespaced
// LIST by the Service-name label, within endpoints.DefaultBounds' page and
// endpoint limits. It keeps nothing; wrap it in endpoints.Bounded.
func NewScopedReader(kube kubernetes.Interface) endpoints.ScopedReader {
	return scopedReader{kube: kube, b: endpoints.DefaultBounds}
}

type scopedReader struct {
	kube kubernetes.Interface
	b    endpoints.Bounds
}

// Endpoints reads svc's endpoints; every install shares the one cluster, so installID is unused.
func (r scopedReader) Endpoints(ctx context.Context, _ string, svc objects.Ref) (endpoints.Result, error) {
	list := r.kube.DiscoveryV1().EndpointSlices(svc.Namespace)
	opts := metav1.ListOptions{LabelSelector: discoveryv1.LabelServiceName + "=" + svc.Name, Limit: int64(r.b.PageSize)}
	res := endpoints.Result{Complete: endpoints.Complete}
	for page := 0; ; page++ {
		if page == r.b.MaxPages {
			res.Complete = endpoints.Partial
			break
		}
		l, err := list.List(ctx, opts)
		if err != nil {
			return endpoints.Result{Complete: endpoints.Failed, ReadAt: time.Now()}, err
		}
		if !r.add(&res, l.Items) {
			res.Complete = endpoints.Partial
			break
		}
		if l.Continue == "" {
			break
		}
		opts.Continue = l.Continue
	}
	res.ReadAt = time.Now()
	return res, nil
}

// add appends an item per endpoint and slice port, and reports false once MaxEndpoints would be passed.
func (r scopedReader) add(res *endpoints.Result, slices []discoveryv1.EndpointSlice) bool {
	for _, es := range slices {
		ports := es.Ports
		if len(ports) == 0 {
			ports = []discoveryv1.EndpointPort{{}}
		}
		for _, e := range es.Endpoints {
			if len(e.Addresses) == 0 {
				continue
			}
			// Unset conditions read as the EndpointSlice API defines them.
			ready := e.Conditions.Ready == nil || *e.Conditions.Ready
			serving := ready
			if e.Conditions.Serving != nil {
				serving = *e.Conditions.Serving
			}
			var pod string
			if e.TargetRef != nil && e.TargetRef.Kind == "Pod" {
				pod = e.TargetRef.Name
			}
			for _, p := range ports {
				if len(res.Items) == r.b.MaxEndpoints {
					return false
				}
				item := endpoints.Endpoint{Address: e.Addresses[0], Ready: ready, Serving: serving,
					Terminating: e.Conditions.Terminating != nil && *e.Conditions.Terminating, Pod: pod}
				if p.Name != nil {
					item.PortName = *p.Name
				}
				if p.Port != nil {
					item.Port = *p.Port
				}
				res.Items = append(res.Items, item)
			}
		}
	}
	return true
}
