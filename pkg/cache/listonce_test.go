package cache

import (
	"context"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// listWorld is the fixture world over fake clients, with every optional input on.
type listWorld struct {
	cfg  Config
	dyn  *dynamicfake.FakeDynamicClient
	kube *kubefake.Clientset
	meta *metadatafake.FakeMetadataClient
}

func newListWorld(t *testing.T) *listWorld {
	t.Helper()
	ctrl, proxy, stray := ctrlPod(), proxyPod(), strayPod()
	d := testutil.ControllerDeployment("kgateway-system", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	at := testutil.FakeClock().Now().Add(-time.Minute)
	typed := []runtime.Object{
		testutil.Namespace("kgateway-system", nil), testutil.Namespace("infra", nil), testutil.Namespace("shop", nil),
		d, ctrl, proxy, stray, testutil.ProxyDeployment("infra", "edge", 1, 1),
		testutil.GatewayClass("kgateway", "kgateway.dev/kgateway", true),
		testutil.Gateway("infra", "edge", "kgateway", nil),
		testutil.HTTPRoute("shop", "b", []objects.ParentRef{testutil.GwRef("infra", "edge")}),
		testutil.HTTPRoute("shop", "a", []objects.ParentRef{testutil.GwRef("infra", "edge")}),
		testutil.Service("shop", "web", nil), testutil.Service("infra", "edge", nil),
		testutil.EndpointSlice("shop", "web-1", "web", true, false), testutil.EndpointSlice("infra", "edge-1", "edge", true),
		testutil.WarningEvent(proxy, "p1", "Unhealthy", at), testutil.WarningEvent(ctrl, "c1", "BackOff", at),
		testutil.WarningEvent(stray, "s1", "BackOff", at),
	}
	dyn, kube := testutil.Clients(t, typed...)
	w := &listWorld{dyn: dyn.(*dynamicfake.FakeDynamicClient), kube: kube,
		meta: newMeta(t, testutil.CRDMetadata("gateways.gateway.networking.k8s.io", "v1.4.0", "standard"),
			testutil.CRDMetadata("other.example.com", "", ""))}
	clk := testutil.FakeClock()
	w.cfg = Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream(), Now: clk.Now,
		PodSelectors: []labels.Selector{podSel}, DeploymentSelectors: []labels.Selector{deploySel},
		ProxyPodSelectors: []labels.Selector{proxySel}, Events: true, CRDs: true, Metadata: w.meta, Endpoints: true}
	return w
}

// watched reports whether any fake client recorded a watch request.
func (w *listWorld) watched() bool {
	for _, acts := range [][]clienttesting.Action{w.dyn.Actions(), w.kube.Actions(), w.meta.Actions()} {
		for _, a := range acts {
			if a.GetVerb() == "watch" {
				return true
			}
		}
	}
	return false
}

// byKey is a copy of objs sorted by namespace and name.
func byKey[T metav1.Object](objs []T) []T {
	out := slices.Clone(objs)
	slices.SortFunc(out, func(a, b T) int { return compareKey(a.GetNamespace(), a.GetName(), b.GetNamespace(), b.GetName()) })
	return out
}

func TestListOnceMatchesCache(t *testing.T) {
	w := newListWorld(t)
	got, err := ListOnce(context.Background(), w.cfg)
	if err != nil {
		t.Fatal(err)
	}

	c := New(w.cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	var want *Snapshot
	waitFor(t, "every input settled with its objects", func() bool {
		want = c.Snapshot()
		return want.Synced && want.Source.Equal(got.Source)
	})

	if got.Mode != ModeList || want.Mode != ModeWatch {
		t.Fatalf("modes %q and %q, want %q and %q", got.Mode, want.Mode, ModeList, ModeWatch)
	}
	if !got.Synced {
		t.Fatalf("ListOnce snapshot not synced: %+v", got.Access)
	}
	// Typed objects come in store order on both paths; compare them by key.
	for name, pair := range map[string][2]any{
		"Objects": {got.Objects, want.Objects}, "Services": {got.Services, want.Services},
		"EndpointSlices": {got.EndpointSlices, want.EndpointSlices}, "ProxyPods": {byKey(got.ProxyPods), byKey(want.ProxyPods)},
		"Namespaces": {byKey(got.Namespaces), byKey(want.Namespaces)}, "Pods": {byKey(got.Pods), byKey(want.Pods)},
		"Deployments": {byKey(got.Deployments), byKey(want.Deployments)}, "Events": {got.Events, want.Events},
		"CRDs": {got.CRDs, want.CRDs}, "Access": {got.Access, want.Access},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s differ:\nlist  %+v\nwatch %+v", name, pair[0], pair[1])
		}
	}
	// The world is not trivially empty: every input was read.
	if len(got.Objects[httpRoutesGVR]) != 2 || len(got.Services) != 2 || len(got.EndpointSlices) != 2 ||
		len(got.ProxyPods) != 1 || len(got.Events) != 2 || len(got.CRDs) != 1 || len(got.Pods) != 1 {
		t.Fatalf("routes=%d services=%d slices=%d proxy=%d events=%d crds=%d pods=%d", len(got.Objects[httpRoutesGVR]),
			len(got.Services), len(got.EndpointSlices), len(got.ProxyPods), len(got.Events), len(got.CRDs), len(got.Pods))
	}
	if key(got.Objects[httpRoutesGVR][0]) != "shop/a" {
		t.Fatalf("routes not sorted by key: first is %s", key(got.Objects[httpRoutesGVR][0]))
	}
}

func TestListOnceAccess(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		denied     bool
		wantSynced bool
	}{
		{"forbidden", apierrors.NewForbidden(httpRoutesGVR.GroupResource(), "", nil), true, true},
		{"internal error", apierrors.NewInternalError(context.DeadlineExceeded), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newListWorld(t)
			w.dyn.PrependReactor("list", "httproutes", func(clienttesting.Action) (bool, runtime.Object, error) {
				return true, nil, tc.err
			})
			s, err := ListOnce(context.Background(), w.cfg)
			if err != nil {
				t.Fatalf("a partial result returns no error, got %v", err)
			}
			a := s.Access[httpRoutesGVR]
			if a.OK || a.Err == "" || a.Denied != tc.denied {
				t.Fatalf("httproutes access %+v, want failed with denied=%v", a, tc.denied)
			}
			if s.Synced != tc.wantSynced {
				t.Fatalf("Synced %v, want %v", s.Synced, tc.wantSynced)
			}
			if gw := s.Access[schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}]; !gw.OK {
				t.Fatalf("other kinds stay ok: gateways %+v", gw)
			}
			if w.watched() {
				t.Fatal("ListOnce made a watch request")
			}
		})
	}
}

func TestListOnceErrors(t *testing.T) {
	w := newListWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s, err := ListOnce(ctx, w.cfg); err == nil {
		t.Fatalf("cancelled context: no error, snapshot %+v", s)
	}
	cfg := w.cfg
	cfg.Dynamic = nil
	if _, err := ListOnce(context.Background(), cfg); err == nil {
		t.Fatal("nil Dynamic: no error")
	}
	// A failure on every optional input is still a partial result, never an error.
	for _, r := range []string{"services", "endpointslices", "events", "pods"} {
		w.kube.PrependReactor("list", r, func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewServiceUnavailable("down")
		})
	}
	s, err := ListOnce(context.Background(), w.cfg)
	if err != nil {
		t.Fatalf("partial result returned an error: %v", err)
	}
	if s.Access[PodsGVR].OK || s.Access[ServicesGVR].OK || s.Synced {
		t.Fatalf("failed inputs must show in Access and hold back Synced: %+v", s.Access)
	}
}

func TestListOnceTakenAt(t *testing.T) {
	w := newListWorld(t)
	var lists atomic.Int64
	count := func(clienttesting.Action) (bool, runtime.Object, error) {
		lists.Add(1)
		return false, nil, nil
	}
	w.dyn.PrependReactor("list", "*", count)
	w.kube.PrependReactor("list", "*", count)
	w.meta.PrependReactor("list", "*", count)
	// The clock reads the number of LISTs made so far.
	w.cfg.Now = func() time.Time { return time.Unix(lists.Load(), 0) }
	s, err := ListOnce(context.Background(), w.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if n := lists.Load(); n == 0 || !s.TakenAt.Equal(time.Unix(n, 0)) {
		t.Fatalf("TakenAt %v, want the time after the last of %d LISTs", s.TakenAt, n)
	}
}
