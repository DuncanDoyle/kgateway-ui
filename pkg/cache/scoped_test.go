package cache

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/snellius/pkg/endpoints"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

var cartRef = objects.Ref{Kind: "Service", Namespace: "shop", Name: "cart"}

// pagedSlices serves slices as an API server pages a LIST: Limit items per
// page and a continue token while more remain. With endless set, every page has a token.
type pagedSlices struct {
	slices  []discoveryv1.EndpointSlice
	endless bool
	lists   []metav1.ListOptions
	ns      []string
}

func (p *pagedSlices) client() *kubefake.Clientset {
	kube := kubefake.NewClientset()
	kube.PrependReactor("list", "endpointslices", func(a clienttesting.Action) (bool, runtime.Object, error) {
		la := a.(clienttesting.ListActionImpl)
		opts := la.GetListOptions()
		p.lists, p.ns = append(p.lists, opts), append(p.ns, la.GetNamespace())
		from := 0
		if opts.Continue != "" {
			from, _ = strconv.Atoi(opts.Continue)
		}
		to := min(from+int(opts.Limit), len(p.slices))
		out := &discoveryv1.EndpointSliceList{Items: p.slices[from:to]}
		if to < len(p.slices) || p.endless {
			out.Continue = strconv.Itoa(to)
		}
		return true, out, nil
	})
	return kube
}

// cartSlices makes n slices of Service cart with per endpoints each on one port named http.
func cartSlices(n, per int) []discoveryv1.EndpointSlice {
	out := make([]discoveryv1.EndpointSlice, n)
	for i := range out {
		es := slice("shop", fmt.Sprintf("cart-%d", i), "cart", []discoveryv1.EndpointPort{{Name: strp("http"), Port: i32p(8080)}})
		for j := range per {
			es.Endpoints = append(es.Endpoints, discoveryv1.Endpoint{Addresses: []string{fmt.Sprintf("10.%d.%d.%d", i/250, i%250, j)}})
		}
		out[i] = *es
	}
	return out
}

// The reader lists one namespace by the Service-name label, ten slices a
// page, for at most ten pages and 1,000 endpoints; beyond either it is partial.
func TestScopedReaderPages(t *testing.T) {
	for _, tc := range []struct {
		name         string
		p            pagedSlices
		pages, items int
		complete     string
	}{
		{"three pages", pagedSlices{slices: cartSlices(30, 2)}, 3, 60, endpoints.Complete},
		{"a continue past page ten", pagedSlices{slices: cartSlices(100, 1), endless: true}, 10, 100, endpoints.Partial},
		{"over 1,000 endpoints", pagedSlices{slices: cartSlices(11, 100)}, 2, 1000, endpoints.Partial},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := NewScopedReader(tc.p.client()).Endpoints(context.Background(), "install-1", cartRef)
			if err != nil {
				t.Fatal(err)
			}
			if res.Complete != tc.complete || len(res.Items) != tc.items || res.ReadAt.IsZero() {
				t.Errorf("result %s with %d items at %v, want %s with %d", res.Complete, len(res.Items), res.ReadAt, tc.complete, tc.items)
			}
			if len(tc.p.lists) != tc.pages {
				t.Errorf("%d pages read, want %d", len(tc.p.lists), tc.pages)
			}
			for i, o := range tc.p.lists {
				if o.LabelSelector != "kubernetes.io/service-name=cart" || o.Limit != 10 || tc.p.ns[i] != "shop" {
					t.Errorf("page %d: namespace %q, options %+v", i, tc.p.ns[i], o)
				}
			}
		})
	}
}

// Each address gets an item per slice port; an unset ready is ready, an unset
// serving follows ready, and a Pod target is named.
func TestScopedReaderEndpoints(t *testing.T) {
	es := slice("shop", "cart-a", "cart", []discoveryv1.EndpointPort{{Name: strp("http"), Port: i32p(8080)}, {Name: strp("grpc"), Port: i32p(9000)}})
	es.Endpoints = []discoveryv1.Endpoint{
		{Addresses: []string{"10.0.0.1"}, TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "cart-x2k9p"}},
		{Addresses: []string{"10.0.0.2"}, Conditions: discoveryv1.EndpointConditions{Ready: boolp(false), Serving: boolp(true), Terminating: boolp(true)},
			TargetRef: &corev1.ObjectReference{Kind: "Node", Name: "n1"}},
	}
	unnamed := slice("shop", "cart-b", "cart", []discoveryv1.EndpointPort{{Port: i32p(7000)}})
	unnamed.Endpoints = []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.3"}, Conditions: discoveryv1.EndpointConditions{Ready: boolp(false)}}}
	p := pagedSlices{slices: []discoveryv1.EndpointSlice{*es, *unnamed}}
	res, err := NewScopedReader(p.client()).Endpoints(context.Background(), "install-1", cartRef)
	want := []endpoints.Endpoint{
		{Address: "10.0.0.1", Port: 8080, PortName: "http", Ready: true, Serving: true, Pod: "cart-x2k9p"},
		{Address: "10.0.0.1", Port: 9000, PortName: "grpc", Ready: true, Serving: true, Pod: "cart-x2k9p"},
		{Address: "10.0.0.2", Port: 8080, PortName: "http", Serving: true, Terminating: true},
		{Address: "10.0.0.2", Port: 9000, PortName: "grpc", Serving: true, Terminating: true},
		{Address: "10.0.0.3", Port: 7000},
	}
	if err != nil || res.Complete != endpoints.Complete || !slices.Equal(res.Items, want) {
		t.Fatalf("result %+v, %v\nwant items %+v", res, err, want)
	}
}

// A failed LIST is a failed result with the error.
func TestScopedReaderFails(t *testing.T) {
	kube := kubefake.NewClientset()
	kube.PrependReactor("list", "endpointslices", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})
	res, err := NewScopedReader(kube).Endpoints(context.Background(), "install-1", cartRef)
	if err == nil || res.Complete != endpoints.Failed || len(res.Items) != 0 {
		t.Fatalf("result %+v, %v; want failed with the error", res, err)
	}
}
