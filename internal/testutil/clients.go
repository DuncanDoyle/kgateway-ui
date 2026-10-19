package testutil

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
)

// ServedUpstream resolves every upstream descriptor to its first version.
func ServedUpstream() []kinds.Served {
	var out []kinds.Served
	for _, d := range kinds.Upstream() {
		out = append(out, kinds.Served{Descriptor: d, Version: d.Versions[0]})
	}
	return out
}

// Clients returns fake dynamic and typed clients seeded with objs. Unstructured
// objects go to the dynamic client under the served GVR of their kind;
// everything else goes to the typed client. Fake discovery serves every
// upstream kind at its first version.
func Clients(t *testing.T, objs ...runtime.Object) (dynamic.Interface, *kubefake.Clientset) {
	t.Helper()
	return clients(t, ServedUpstream(), objs)
}

// ClientsWith is Clients with extra kinds served at their first version, for
// downstream-extension tests.
func ClientsWith(t *testing.T, extra []kinds.Descriptor, objs ...runtime.Object) (dynamic.Interface, *kubefake.Clientset) {
	t.Helper()
	served := ServedUpstream()
	for _, d := range extra {
		served = append(served, kinds.Served{Descriptor: d, Version: d.Versions[0]})
	}
	return clients(t, served, objs)
}

// clients builds the fakes for served: discovery, list kinds and tracker GVRs.
func clients(t *testing.T, served []kinds.Served, objs []runtime.Object) (*dynamicfake.FakeDynamicClient, *kubefake.Clientset) {
	t.Helper()
	listKinds := map[schema.GroupVersionResource]string{}
	byKind := map[schema.GroupKind]schema.GroupVersionResource{}
	byGV := map[string]*metav1.APIResourceList{}
	for _, s := range served {
		listKinds[s.GVR()] = s.Kind + "List"
		byKind[schema.GroupKind{Group: s.Group, Kind: s.Kind}] = s.GVR()
		gv := schema.GroupVersion{Group: s.Group, Version: s.Version}.String()
		if byGV[gv] == nil {
			byGV[gv] = &metav1.APIResourceList{GroupVersion: gv}
		}
		byGV[gv].APIResources = append(byGV[gv].APIResources, metav1.APIResource{Name: s.Resource, Kind: s.Kind, Namespaced: s.Namespaced})
	}
	// The dynamic fake's constructor guesses resources from kinds ("gatewaies"),
	// so objects are added to its tracker under the served GVR instead.
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds)
	var typed []runtime.Object
	for _, o := range objs {
		u, ok := o.(*unstructured.Unstructured)
		if !ok {
			typed = append(typed, o)
			continue
		}
		gvr, ok := byKind[u.GroupVersionKind().GroupKind()]
		if !ok {
			t.Fatalf("testutil.Clients: %s is not a served kind", u.GroupVersionKind())
		}
		if err := dyn.Tracker().Create(gvr, u, u.GetNamespace()); err != nil {
			t.Fatalf("testutil.Clients: add %s %s/%s: %v", u.GetKind(), u.GetNamespace(), u.GetName(), err)
		}
	}
	kube := kubefake.NewSimpleClientset(typed...)
	fd := kube.Discovery().(*fakediscovery.FakeDiscovery)
	for _, l := range byGV {
		fd.Resources = append(fd.Resources, l)
	}
	return dyn, kube
}

// ObjectsByGVR groups objs under the upstream served GVR of their kind, as in
// cache.Snapshot.Objects. It fails the test for an object of an unserved kind.
func ObjectsByGVR(t *testing.T, objs ...*unstructured.Unstructured) map[schema.GroupVersionResource][]*unstructured.Unstructured {
	t.Helper()
	byKind := map[schema.GroupKind]schema.GroupVersionResource{}
	for _, s := range ServedUpstream() {
		byKind[schema.GroupKind{Group: s.Group, Kind: s.Kind}] = s.GVR()
	}
	out := map[schema.GroupVersionResource][]*unstructured.Unstructured{}
	for _, o := range objs {
		gvr, ok := byKind[o.GroupVersionKind().GroupKind()]
		if !ok {
			t.Fatalf("testutil.ObjectsByGVR: %s is not a served kind", o.GroupVersionKind())
		}
		out[gvr] = append(out[gvr], o)
	}
	return out
}
