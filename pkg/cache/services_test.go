package cache

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
)

// endpointsWorld is a cache with the Service and EndpointSlice watches over a fake client.
type endpointsWorld struct {
	c    *Cache
	kube *kubefake.Clientset
}

func newEndpointsWorld(t *testing.T, on bool, typed []runtime.Object, prepare func(kube *kubefake.Clientset)) *endpointsWorld {
	t.Helper()
	dyn, kube := testutil.Clients(t, typed...)
	w := &endpointsWorld{kube: kube}
	w.c = New(Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream(), Endpoints: on,
		PodSelectors: []labels.Selector{podSel}, DeploymentSelectors: []labels.Selector{deploySel}})
	if prepare != nil {
		prepare(kube)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w.c.Start(ctx)
	waitSynced(t, w.c)
	for _, gvr := range []schema.GroupVersionResource{ServicesGVR, EndpointSlicesGVR} {
		waitFor(t, "optional watch "+gvr.Resource+" settled", func() bool {
			a, ok := w.c.Snapshot().Access[gvr]
			return !ok || a.OK || a.Err != ""
		})
	}
	return w
}

func boolp(b bool) *bool { return &b }

func slice(ns, name, svc string, ports []discoveryv1.EndpointPort, ready ...*bool) *discoveryv1.EndpointSlice {
	es := testutil.EndpointSlice(ns, name, svc)
	es.Ports = ports
	for i, r := range ready {
		es.Endpoints = append(es.Endpoints, discoveryv1.Endpoint{Addresses: []string{"10.9.0." + string(rune('1'+i))},
			Conditions: discoveryv1.EndpointConditions{Ready: r}, NodeName: new(string)})
	}
	return es
}

func strp(s string) *string { return &s }
func i32p(i int32) *int32   { return &i }

// The Service keeps only its identity, type and ports; a named target port
// has no number and an unset one is the port. Labels, annotations, selector and IPs never reach the store.
func TestServiceTransformShape(t *testing.T) {
	svc := testutil.Service("shop", "cart", map[string]string{"team": "a"})
	svc.Annotations = map[string]string{"big": strings.Repeat("x", 512)}
	svc.Spec.Type, svc.Spec.ClusterIP = corev1.ServiceTypeClusterIP, "10.0.0.7"
	svc.Spec.Ports = []corev1.ServicePort{
		{Name: "http", Port: 80, TargetPort: intstr.FromString("web")},
		{Name: "metrics", Port: 9090, TargetPort: intstr.FromInt32(9091), Protocol: corev1.ProtocolUDP},
	}
	ext := testutil.Service("shop", "ext", nil)
	ext.Spec.Type = corev1.ServiceTypeExternalName
	ext.Spec.ExternalName = "example.com"
	w := newEndpointsWorld(t, true, []runtime.Object{svc, ext}, nil)
	s := w.c.Snapshot()
	want := []ServiceFact{
		{Namespace: "shop", Name: "cart", Type: "ClusterIP", Ports: []ServicePort{{"http", 80, 0, "TCP"}, {"metrics", 9090, 9091, "UDP"}}},
		{Namespace: "shop", Name: "ext", Type: "ExternalName", Ports: []ServicePort{{"http", 80, 80, "TCP"}}},
	}
	if len(s.Services) != 2 {
		t.Fatalf("Services %+v, want %+v", s.Services, want)
	}
	for i := range want {
		g := s.Services[i]
		if g.Namespace != want[i].Namespace || g.Name != want[i].Name || g.Type != want[i].Type || !slices.Equal(g.Ports, want[i].Ports) {
			t.Errorf("Service %d: %+v, want %+v", i, g, want[i])
		}
	}
	for _, o := range w.c.services.inf.GetStore().List() {
		if _, ok := o.(*serviceEntry); !ok {
			t.Fatalf("stored Service %T, want only its fact", o)
		}
	}
	if !s.Access[ServicesGVR].OK || !s.Access[EndpointSlicesGVR].OK {
		t.Fatalf("access %+v", s.Access)
	}
}

// Ready counts per EndpointSlice port, an unset ready counts as ready, a
// false one does not, an unnamed port has the name "", and slices of one
// Service stay separate.
func TestSliceFactsCountReadyPerPortName(t *testing.T) {
	named := []discoveryv1.EndpointPort{{Name: strp("http"), Port: i32p(8080)}, {Name: strp("grpc"), Port: i32p(9000)}}
	a := slice("shop", "cart-a", "cart", named, nil, boolp(true), boolp(false))
	b := slice("shop", "cart-b", "cart", named, boolp(false))
	b.AddressType = discoveryv1.AddressTypeIPv6
	c := slice("shop", "single-c", "single", []discoveryv1.EndpointPort{{Port: i32p(8080)}}, nil)
	empty := slice("shop", "none-d", "none", []discoveryv1.EndpointPort{{Name: strp("http"), Port: i32p(80)}})
	w := newEndpointsWorld(t, true, []runtime.Object{a, b, c, empty}, nil)
	got := w.c.Snapshot().EndpointSlices
	want := []SliceFact{
		{Namespace: "shop", Service: "cart", Ports: []SlicePort{{"http", 8080}, {"grpc", 9000}}, Ready: []int{2, 2}, Total: 3, AddressType: "IPv4"},
		{Namespace: "shop", Service: "cart", Ports: []SlicePort{{"http", 8080}, {"grpc", 9000}}, Ready: []int{0, 0}, Total: 1, AddressType: "IPv6"},
		{Namespace: "shop", Service: "none", Ports: []SlicePort{{"http", 80}}, Ready: []int{0}, AddressType: "IPv4"},
		{Namespace: "shop", Service: "single", Ports: []SlicePort{{"", 8080}}, Ready: []int{1}, Total: 1, AddressType: "IPv4"},
	}
	if len(got) != len(want) {
		t.Fatalf("EndpointSlices %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Namespace != want[i].Namespace || got[i].Service != want[i].Service || !slices.Equal(got[i].Ports, want[i].Ports) || !slices.Equal(got[i].Ready, want[i].Ready) || got[i].Total != want[i].Total || got[i].AddressType != want[i].AddressType {
			t.Errorf("slice %d: %+v, want %+v", i, got[i], want[i])
		}
	}
	for _, o := range w.c.slices.inf.GetStore().List() {
		if _, ok := o.(*sliceEntry); !ok {
			t.Fatalf("stored EndpointSlice %T, want only its fact", o)
		}
	}
}

// A slice update changes the Ready counts and the fingerprint matches the snapshot's source.
func TestSliceUpdateAndFingerprint(t *testing.T) {
	a := slice("shop", "cart-a", "cart", []discoveryv1.EndpointPort{{Name: strp("http"), Port: i32p(8080)}}, boolp(false))
	w := newEndpointsWorld(t, true, []runtime.Object{testutil.Service("shop", "cart", nil), a}, nil)
	s := w.c.Snapshot()
	if !s.Source.Equal(w.c.Fingerprint()) {
		t.Fatal("fingerprint differs from the snapshot source")
	}
	if n := s.Source.Kinds[EndpointSlicesGVR.String()].Count; n != 1 {
		t.Fatalf("fingerprint counts %d slices, want 1", n)
	}
	up := a.DeepCopy()
	up.ResourceVersion += "1"
	up.Endpoints[0].Conditions.Ready = nil
	if _, err := w.kube.DiscoveryV1().EndpointSlices("shop").Update(context.Background(), up, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the slice update", func() bool { return w.c.Snapshot().EndpointSlices[0].Ready[0] == 1 })
	if w.c.Snapshot().Source.Equal(s.Source) {
		t.Fatal("the fingerprint did not change with the slice")
	}
}

// With the toggle off no Service or EndpointSlice is listed or watched and neither has an Access entry.
func TestEndpointsToggleOffStartsNoWatch(t *testing.T) {
	off := newEndpointsWorld(t, false, []runtime.Object{testutil.Service("shop", "cart", nil)}, nil)
	for _, a := range off.kube.Actions() {
		if r := a.GetResource().Resource; r == "services" || r == "endpointslices" {
			t.Fatalf("endpoints off, yet %s %s", a.GetVerb(), r)
		}
	}
	if s := off.c.Snapshot(); hasAccess(s, ServicesGVR) || hasAccess(s, EndpointSlicesGVR) || len(s.Services) != 0 {
		t.Fatalf("off watches leave traces: %v", s.Access)
	}
	on := newEndpointsWorld(t, true, nil, nil)
	for _, r := range []string{"services", "endpointslices"} {
		if !slices.ContainsFunc(on.kube.Actions(), func(a clienttesting.Action) bool { return a.GetVerb() == "watch" && a.GetResource().Resource == r }) {
			t.Errorf("no watch on %s", r)
		}
	}
}

// A denied watch reports Denied under its own key; a failing one does not. The
// core kinds stay readable, so cluster access has nothing to report.
func TestEndpointsDeniedAndFailing(t *testing.T) {
	w := newEndpointsWorld(t, true, nil, func(kube *kubefake.Clientset) {
		kube.PrependReactor("list", "services", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "services"}, "", nil)
		})
		kube.PrependReactor("list", "endpointslices", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewInternalError(errors.New("etcd timeout"))
		})
	})
	s := w.c.Snapshot()
	if a := s.Access[ServicesGVR]; a.OK || !a.Denied {
		t.Errorf("services access %+v, want denied", a)
	}
	if a := s.Access[EndpointSlicesGVR]; a.OK || a.Denied || a.Err == "" {
		t.Errorf("endpointslices access %+v, want failing, not denied", a)
	}
	for _, gvr := range []schema.GroupVersionResource{NamespacesGVR, PodsGVR, DeploymentsGVR} {
		if !s.Access[gvr].OK {
			t.Errorf("core %s access %+v, want OK", gvr.Resource, s.Access[gvr])
		}
	}
	if !s.Synced {
		t.Error("an optional watch held the snapshot back")
	}
}

// A Service or EndpointSlice watch that errors after it worked reads as failing,
// then OK again on the next successful watch.
func TestEndpointsWatchRecoversAfterError(t *testing.T) {
	for _, gvr := range []schema.GroupVersionResource{ServicesGVR, EndpointSlicesGVR} {
		t.Run(gvr.Resource, func(t *testing.T) {
			watchers := make(chan *watch.FakeWatcher, 64)
			w := newEndpointsWorld(t, true, nil, func(kube *kubefake.Clientset) {
				kube.PrependWatchReactor(gvr.Resource, func(clienttesting.Action) (bool, watch.Interface, error) {
					fw := watch.NewFake()
					watchers <- fw
					return true, fw, nil
				})
			})
			if !w.c.Snapshot().Access[gvr].OK {
				t.Fatalf("access %+v, want OK", w.c.Snapshot().Access[gvr])
			}
			status := metav1.Status{Status: metav1.StatusFailure, Code: 429, Reason: metav1.StatusReasonTooManyRequests, Message: "slow down"}
			testutil.Recv(t, watchers, "the established watch").Error(&status)
			waitFor(t, "the error to make the watch fail", func() bool { return w.c.Snapshot().Access[gvr].Err != "" })
			testutil.Recv(t, watchers, "the next successful Watch call")
			waitFor(t, "recovery", func() bool { return w.c.Snapshot().Access[gvr].OK })
		})
	}
}

// Facts sort by "namespace/name", also when one namespace prefixes another.
func TestCompareKeyMatchesJoinedKeys(t *testing.T) {
	keys := [][2]string{{"ns", "b"}, {"ns-1", "a"}, {"ns", "a"}, {"n", "z"}, {"ns.x", "a"}, {"ns0", "a"}, {"a", "ns"}}
	for _, a := range keys {
		for _, b := range keys {
			if got, want := compareKey(a[0], a[1], b[0], b[1]), strings.Compare(a[0]+"/"+a[1], b[0]+"/"+b[1]); got != want {
				t.Errorf("compareKey(%v, %v) = %d, want %d", a, b, got, want)
			}
		}
	}
}
