package cache

import (
	"context"
	"reflect"
	"slices"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

var gatewayClassesGVR = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}

// nsWorld is namespaces a, b and c, each with an HTTPRoute, a Service, an
// EndpointSlice and a controller Pod, plus a GatewayClass, over fake clients.
type nsWorld struct {
	cfg  Config
	dyn  *dynamicfake.FakeDynamicClient
	kube *kubefake.Clientset
}

func newNSWorld(t *testing.T, namespaces []string, services bool) *nsWorld {
	t.Helper()
	typed := []runtime.Object{testutil.GatewayClass("kgateway", "kgateway.dev/kgateway", true)}
	for _, ns := range []string{"a", "b", "c"} {
		typed = append(typed, testutil.Namespace(ns, nil), testutil.HTTPRoute(ns, "r", []objects.ParentRef{testutil.GwRef(ns, "gw")}))
		pod := ctrlPod()
		pod.Namespace, pod.UID = ns, pod.UID+types.UID("-"+ns)
		typed = append(typed, pod)
		if services {
			typed = append(typed, testutil.Service(ns, "web", nil), testutil.EndpointSlice(ns, "web-1", "web", true))
		}
	}
	dyn, kube := testutil.Clients(t, typed...)
	return &nsWorld{dyn: dyn.(*dynamicfake.FakeDynamicClient), kube: kube,
		cfg: Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream(), PodSelectors: []labels.Selector{podSel},
			Endpoints: true, Namespaces: namespaces, Now: testutil.FakeClock().Now}}
}

// deny makes LISTs of resource in namespace ns fail with err on w's typed client.
func (w *nsWorld) deny(resource, ns string, err error) {
	w.kube.PrependReactor("list", resource, func(a clienttesting.Action) (bool, runtime.Object, error) {
		if a.GetNamespace() == ns {
			return true, nil, err
		}
		return false, nil, nil
	})
}

// listedIn returns the namespaces w's clients listed resource in, in order of first use.
func (w *nsWorld) listedIn(resource string) []string {
	var out []string
	for _, a := range slices.Concat(w.dyn.Actions(), w.kube.Actions()) {
		if a.GetVerb() == "list" && a.GetResource().Resource == resource && !slices.Contains(out, a.GetNamespace()) {
			out = append(out, a.GetNamespace())
		}
	}
	slices.Sort(out)
	return out
}

// bothPaths returns, per mode, the snapshot of a world from build: a started
// Cache's once ready holds, and ListOnce's. Each path gets its own world.
func bothPaths(t *testing.T, build func() *nsWorld, ready func(*Snapshot) bool) map[string]*Snapshot {
	t.Helper()
	listed, err := ListOnce(context.Background(), build().cfg)
	if err != nil {
		t.Fatal(err)
	}
	c := New(build().cfg)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Start(ctx)
	var watched *Snapshot
	waitFor(t, "the informer cache ready", func() bool {
		watched = c.Snapshot()
		return watched.Synced && ready(watched)
	})
	return map[string]*Snapshot{ModeList: listed, ModeWatch: watched}
}

func names[T interface{ GetNamespace() string }](objs []T) []string {
	var out []string
	for _, o := range objs {
		if !slices.Contains(out, o.GetNamespace()) {
			out = append(out, o.GetNamespace())
		}
	}
	slices.Sort(out)
	return out
}

func TestNamespacesInformerPerNamespace(t *testing.T) {
	// Unsorted, with a duplicate and an empty entry: the cache reads a and b.
	want := []string{"a", "b"}
	build := func() *nsWorld { return newNSWorld(t, []string{"b", "a", "", "b"}, true) }
	allOK := func(s *Snapshot) bool { return s.Access[ServicesGVR].OK && s.Access[EndpointSlicesGVR].OK }
	for mode, s := range bothPaths(t, build, allOK) {
		t.Run(mode, func(t *testing.T) {
			if !reflect.DeepEqual(s.RequestedNamespaces, want) {
				t.Fatalf("RequestedNamespaces %v, want %v", s.RequestedNamespaces, want)
			}
			if got := names(s.Objects[httpRoutesGVR]); !reflect.DeepEqual(got, want) {
				t.Fatalf("routes from %v, want %v", got, want)
			}
			if got := names(s.Pods); !reflect.DeepEqual(got, want) {
				t.Fatalf("pods from %v, want %v", got, want)
			}
			var svcs, eps []string
			for _, f := range s.Services {
				svcs = append(svcs, f.Namespace)
			}
			for _, f := range s.EndpointSlices {
				eps = append(eps, f.Namespace)
			}
			if !reflect.DeepEqual(svcs, want) || !reflect.DeepEqual(eps, want) {
				t.Fatalf("services from %v, slices from %v, want %v", svcs, eps, want)
			}
			// Cluster-scoped kinds stay cluster-wide.
			if len(s.Objects[gatewayClassesGVR]) != 1 || len(s.Namespaces) != 3 {
				t.Fatalf("%d GatewayClasses and %d Namespaces, want 1 and 3", len(s.Objects[gatewayClassesGVR]), len(s.Namespaces))
			}
			for _, gvr := range []schema.GroupVersionResource{httpRoutesGVR, PodsGVR, ServicesGVR, EndpointSlicesGVR} {
				if d := s.NamespaceAccess[gvr]; len(d) != 2 || !d["a"].OK || !d["b"].OK || !s.Access[gvr].OK {
					t.Fatalf("%s access %+v, by namespace %+v", gvr.Resource, s.Access[gvr], d)
				}
			}
			for _, gvr := range []schema.GroupVersionResource{gatewayClassesGVR, NamespacesGVR} {
				if _, ok := s.NamespaceAccess[gvr]; ok || !s.Access[gvr].OK {
					t.Fatalf("cluster-scoped %s: access %+v, by namespace %+v", gvr.Resource, s.Access[gvr], s.NamespaceAccess[gvr])
				}
			}
		})
	}
	// One LIST per namespace for namespaced inputs; cluster-scoped ones list cluster-wide.
	w := build()
	if _, err := ListOnce(context.Background(), w.cfg); err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{"httproutes", "pods", "services", "endpointslices"} {
		if got := w.listedIn(r); !reflect.DeepEqual(got, want) {
			t.Errorf("%s listed in %q, want %q", r, got, want)
		}
	}
	for _, r := range []string{"gatewayclasses", "namespaces"} {
		if got := w.listedIn(r); !reflect.DeepEqual(got, []string{""}) {
			t.Errorf("%s listed in %q, want cluster-wide", r, got)
		}
	}
}

func TestNamespaceAccessAggregate(t *testing.T) {
	forbidden := apierrors.NewForbidden(ServicesGVR.GroupResource(), "", nil)
	denied := func(s *Snapshot) bool { return s.NamespaceAccess[ServicesGVR]["b"].Err != "" }
	build := func() *nsWorld {
		w := newNSWorld(t, []string{"a", "b"}, true)
		w.deny("services", "b", forbidden)
		return w
	}
	for mode, s := range bothPaths(t, build, denied) {
		t.Run(mode, func(t *testing.T) {
			if a := s.Access[ServicesGVR]; a.OK || !a.Denied {
				t.Fatalf("aggregate services access %+v, want denied", a)
			}
			d := s.NamespaceAccess[ServicesGVR]
			if len(d) != 2 || !d["a"].OK || !d["b"].Denied {
				t.Fatalf("services by namespace %+v, want a ok and b denied", d)
			}
			if !s.Access[EndpointSlicesGVR].OK {
				t.Fatalf("endpointslices %+v, want ok", s.Access[EndpointSlicesGVR])
			}
		})
	}

	// The worst state wins: failed over denied over syncing.
	w := newNSWorld(t, []string{"a", "b"}, true)
	w.deny("endpointslices", "a", apierrors.NewInternalError(context.DeadlineExceeded))
	w.deny("endpointslices", "b", apierrors.NewForbidden(EndpointSlicesGVR.GroupResource(), "", nil))
	s, err := ListOnce(context.Background(), w.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if a := s.Access[EndpointSlicesGVR]; a.OK || a.Denied || a.Err == "" {
		t.Fatalf("aggregate endpointslices access %+v, want failed", a)
	}
	if got := worst(Access{}, Access{Err: "x", Denied: true}, Access{OK: true}); !got.Denied {
		t.Fatalf("denied must win over syncing: %+v", got)
	}
	if got := worst(Access{OK: true}, Access{}); got.OK || got.Err != "" {
		t.Fatalf("syncing must win over ok: %+v", got)
	}

	// Fingerprints cover the detail: all-ok differs, and so does the same denial in another namespace.
	print := func(deny string) Fingerprint {
		w := newNSWorld(t, []string{"a", "b"}, false)
		if deny != "" {
			w.deny("services", deny, forbidden)
		}
		s, err := ListOnce(context.Background(), w.cfg)
		if err != nil {
			t.Fatal(err)
		}
		return s.Source
	}
	inA, inB, none := print("a"), print("b"), print("")
	if inB.Equal(none) || inA.Equal(inB) {
		t.Fatal("fingerprints must differ with the namespace access")
	}
	if !reflect.DeepEqual(inA.Kinds, inB.Kinds) || !reflect.DeepEqual(inA.Access, inB.Access) {
		t.Fatal("the two denials differ only in their namespace")
	}
}

func TestNamespacesNilUnchanged(t *testing.T) {
	ready := func(s *Snapshot) bool { return s.Access[ServicesGVR].OK && s.Access[EndpointSlicesGVR].OK }
	base := bothPaths(t, func() *nsWorld { return newNSWorld(t, nil, true) }, ready)
	empty := bothPaths(t, func() *nsWorld { return newNSWorld(t, []string{}, true) }, ready)
	for _, mode := range []string{ModeList, ModeWatch} {
		b, e := base[mode], empty[mode]
		for name, s := range map[string]*Snapshot{"nil": b, "empty": e} {
			if s.NamespaceAccess != nil || s.RequestedNamespaces != nil || s.Source.NamespaceAccess != nil {
				t.Fatalf("%s %s: namespace detail %+v %v %+v", mode, name, s.NamespaceAccess, s.RequestedNamespaces, s.Source.NamespaceAccess)
			}
			if got := names(s.Objects[httpRoutesGVR]); len(got) != 3 || len(s.Services) != 3 || len(s.Pods) != 3 {
				t.Fatalf("%s %s: routes from %v, %d services, %d pods; want every namespace", mode, name, got, len(s.Services), len(s.Pods))
			}
		}
		if !b.Source.Equal(e.Source) || !reflect.DeepEqual(b.Source, e.Source) || !reflect.DeepEqual(b.Access, e.Access) {
			t.Fatalf("%s: nil and empty Namespaces differ", mode)
		}
	}
	if !base[ModeList].Source.Equal(base[ModeWatch].Source) {
		t.Fatal("list and watch fingerprints differ")
	}
	w := newNSWorld(t, nil, true)
	if _, err := ListOnce(context.Background(), w.cfg); err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{"httproutes", "pods", "services", "endpointslices"} {
		if got := w.listedIn(r); !reflect.DeepEqual(got, []string{""}) {
			t.Errorf("%s listed in %q, want cluster-wide", r, got)
		}
	}
}
