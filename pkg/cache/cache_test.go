package cache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
)

// The production selectors: Deployments by kgateway + component, Pods by
// kgateway only.
var (
	deploySel = labels.SelectorFromSet(labels.Set{"kgateway": "kgateway", "app.kubernetes.io/component": "controller"})
	podSel    = labels.SelectorFromSet(labels.Set{"kgateway": "kgateway"})
)

func waitSynced(t *testing.T, c *Cache) *Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := c.Snapshot(); s.Synced {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("cache did not sync")
	return nil
}

func TestSnapshotContainsWatchedObjectsOnly(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	other := testutil.ControllerDeployment("apps", "web", "nginx", 1, 1)
	other.Labels = map[string]string{"app": "web"} // must not be cached: label-selected watches only
	dyn, kube := testutil.Clients(t,
		testutil.Namespace("shop", nil),
		d, testutil.ControllerPod(d, "kgateway-1", true), other,
		testutil.Gateway("shop", "edge", "kgateway", nil),
	)
	c := New(Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream(),
		PodSelectors: []labels.Selector{podSel}, DeploymentSelectors: []labels.Selector{deploySel}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	s := waitSynced(t, c)

	gw := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
	if len(s.Objects[gw]) != 1 || !s.Access[gw].OK {
		t.Fatalf("gateways: %d objects, access %+v", len(s.Objects[gw]), s.Access[gw])
	}
	if len(s.Deployments) != 1 || s.Deployments[0].Name != "kgateway" {
		t.Fatalf("only the label-selected controller Deployment may be cached, got %d", len(s.Deployments))
	}
	if len(s.Pods) != 1 || len(s.Namespaces) != 1 {
		t.Fatalf("pods=%d namespaces=%d", len(s.Pods), len(s.Namespaces))
	}
	for _, gvr := range []schema.GroupVersionResource{NamespacesGVR, PodsGVR, DeploymentsGVR} {
		if !s.Access[gvr].OK {
			t.Errorf("%s must be reported in Access: %+v", gvr.Resource, s.Access[gvr])
		}
	}
}

// waitFor polls cond until it holds or 15 s pass (reflector backoff starts at 0.8 s).
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// bookmark is a watch bookmark for obj's kind; initialEnd marks the end of a watch-list stream.
func bookmark(obj metav1.Object, initialEnd bool) watch.Event {
	obj.SetResourceVersion("999")
	if initialEnd {
		obj.SetAnnotations(map[string]string{metav1.InitialEventsAnnotationKey: "true"})
	}
	return watch.Event{Type: watch.Bookmark, Object: obj.(runtime.Object)}
}

// drain consumes w until it closes, as the reflector would.
func drain(w watch.Interface) {
	go func() {
		for range w.ResultChan() {
		}
	}()
}

// The failure and recovery rules (D-014), driven through the instrumented
// ListWatch itself. This also covers the watch-list catch-up, which the fake
// clients cannot produce through a reflector.
func TestInstrumentedListWatchRules(t *testing.T) {
	watchers := make(chan watch.Interface, 8)
	var listCalls atomic.Int32
	var failNext atomic.Pointer[error] // the next Watch call fails with it
	failOnce := func(err error) { failNext.Store(&err) }
	p := &progress{}
	lw := instrument(p,
		func(context.Context, metav1.ListOptions) (runtime.Object, error) {
			listCalls.Add(1)
			return &corev1.NamespaceList{}, nil
		},
		func(context.Context, metav1.ListOptions) (watch.Interface, error) {
			if e := failNext.Swap(nil); e != nil {
				return nil, *e
			}
			select {
			case w := <-watchers:
				return w, nil
			case <-time.After(testutil.RecvTimeout):
				return nil, errors.New("no fake watcher queued")
			}
		})
	ctx := context.Background()
	classic := metav1.ListOptions{}
	initial := true
	watchList := metav1.ListOptions{SendInitialEvents: &initial}
	ok := func() bool { return p.access(true).OK }
	open := func(o metav1.ListOptions) *watch.FakeWatcher {
		t.Helper()
		fw := watch.NewFake()
		watchers <- fw
		w, err := lw.WatchWithContext(ctx, o)
		if err != nil {
			t.Fatal(err)
		}
		drain(w)
		return fw
	}

	if a := p.access(false); a.OK || a.Err != "" {
		t.Fatalf("before the initial sync the kind is pending, got %+v", a)
	}
	if _, err := lw.ListWithContext(ctx, classic); err != nil {
		t.Fatal(err)
	}
	if !ok() {
		t.Fatal("the initial sync makes the kind current")
	}

	// An error event inside an established stream is a failure, and later
	// events on that already-open watch do not recover it.
	fw := open(classic)
	fw.Error(&metav1.Status{Status: metav1.StatusFailure, Code: 429, Reason: metav1.StatusReasonTooManyRequests, Message: "slow down"})
	waitFor(t, "the error event to count", func() bool { return strings.Contains(p.access(true).Err, "slow down") })
	before := open(classic) // a working watch recovers; fail again so the next check starts from a failure
	failOnce(errors.New("connection reset by peer"))
	if _, err := lw.WatchWithContext(ctx, classic); err == nil {
		t.Fatal("the injected Watch failure must surface")
	}
	before.Action(watch.Bookmark, bookmark(&corev1.Namespace{}, false).Object)
	before.Add(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "shop", ResourceVersion: "6"}}) // once taken, the bookmark was handled
	if ok() {
		t.Fatal("events on a watch opened before the failure do not recover it")
	}

	// A successful List alone does not recover; a failed Watch call after it keeps the kind unavailable.
	if _, err := lw.ListWithContext(ctx, classic); err != nil {
		t.Fatal(err)
	}
	if ok() {
		t.Fatal("a successful List alone must not recover")
	}
	failOnce(apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "", nil))
	if _, err := lw.WatchWithContext(ctx, classic); err == nil {
		t.Fatal("the injected Watch failure must surface")
	}
	if a := p.access(true); a.OK || !strings.Contains(a.Err, "forbidden") {
		t.Fatalf("a failed Watch call after a successful List keeps the kind unavailable, got %+v", a)
	}

	// A successful classic Watch call after the failure recovers, with no event and no List.
	lists := listCalls.Load()
	open(classic)
	if !ok() {
		t.Fatal("a successful classic Watch call after the failure must recover")
	}
	if listCalls.Load() != lists {
		t.Fatal("recovery through a watch must not need a List call")
	}

	// A watch-list stream recovers only on its initial-events-end bookmark.
	failOnce(fmt.Errorf("dial tcp 10.96.0.1:443: connect: %w", syscall.ECONNREFUSED))
	if _, err := lw.WatchWithContext(ctx, classic); err == nil {
		t.Fatal("the injected Watch failure must surface")
	}
	fw2 := open(watchList)
	if ok() {
		t.Fatal("opening a watch-list stream does not recover")
	}
	fw2.Add(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "shop", ResourceVersion: "5"}})
	fw2.Action(watch.Bookmark, bookmark(&corev1.Namespace{}, false).Object)
	fw2.Action(watch.Bookmark, bookmark(&corev1.Namespace{}, false).Object) // once taken, the earlier ones were handled
	if ok() {
		t.Fatal("initial events and regular bookmarks of a watch-list stream do not recover")
	}
	fw2.Action(watch.Bookmark, bookmark(&corev1.Namespace{}, true).Object)
	waitFor(t, "the initial-events-end bookmark to recover", ok)
}

// D-014 through the real informers and fake clients. An error event
// inside an established watch makes the kind unavailable. A retriable error
// (429) makes the reflector resume a classic watch without listing; a
// non-retriable one (403) makes it relist first. Either way the next
// successful Watch call recovers the kind.
func TestStreamErrorAfterSyncRecoversOnTheNextWatch(t *testing.T) {
	hr := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	tooMany := metav1.Status{Status: metav1.StatusFailure, Code: 429, Reason: metav1.StatusReasonTooManyRequests, Message: "slow down"}
	forbidden := metav1.Status{Status: metav1.StatusFailure, Code: 403, Reason: metav1.StatusReasonForbidden, Message: "watch forbidden"}
	cases := []struct {
		name    string
		gvr     schema.GroupVersionResource
		typed   bool
		status  metav1.Status
		relists bool
	}{
		{"httproutes 429: resumed watch, no list", hr, false, tooMany, false},
		{"httproutes 403: relist, then a watch", hr, false, forbidden, true},
		{"namespaces 429: resumed watch, no list", NamespacesGVR, true, tooMany, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dyn, kube := testutil.Clients(t, testutil.Namespace("shop", nil))
			var lists atomic.Int32
			watchers := make(chan *watch.FakeWatcher, 64)
			onWatch := func(clienttesting.Action) (bool, watch.Interface, error) {
				w := watch.NewFake()
				watchers <- w
				return true, w, nil
			}
			onList := func(clienttesting.Action) (bool, runtime.Object, error) {
				lists.Add(1)
				return false, nil, nil // fall through to the tracker
			}
			if tc.typed {
				kube.PrependWatchReactor(tc.gvr.Resource, onWatch)
				kube.PrependReactor("list", tc.gvr.Resource, onList)
			} else {
				dyn.(*dynamicfake.FakeDynamicClient).PrependWatchReactor(tc.gvr.Resource, onWatch)
				dyn.(*dynamicfake.FakeDynamicClient).PrependReactor("list", tc.gvr.Resource, onList)
			}
			c := New(Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream(),
				PodSelectors: []labels.Selector{podSel}, DeploymentSelectors: []labels.Selector{deploySel}})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.Start(ctx)
			waitSynced(t, c)

			n := lists.Load()
			status := tc.status
			testutil.Recv(t, watchers, "the established watch").Error(&status)
			waitFor(t, "the error event to make the kind unavailable", func() bool { return !c.Snapshot().Access[tc.gvr].OK })
			testutil.Recv(t, watchers, "the next successful Watch call") // resumed, or opened after a relist
			waitFor(t, "recovery on the next successful watch", func() bool { return c.Snapshot().Access[tc.gvr].OK })
			if got := lists.Load() > n; got != tc.relists {
				t.Fatalf("relisted=%v, want %v", got, tc.relists)
			}
		})
	}
}

func TestForbiddenNamespacesAreReported(t *testing.T) {
	dyn, kube := testutil.Clients(t, testutil.Namespace("shop", nil))
	kube.PrependReactor("list", "namespaces", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "", nil)
	})
	c := New(Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream()})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	s := waitSynced(t, c)
	if s.Access[NamespacesGVR].OK || s.Access[NamespacesGVR].Err == "" {
		t.Fatalf("namespaces must be reported as not accessible, got %+v", s.Access[NamespacesGVR])
	}
}

func TestForbiddenKindIsReportedAndDoesNotBlockSync(t *testing.T) {
	dyn, kube := testutil.Clients(t, testutil.Namespace("shop", nil))
	dyn.(*dynamicfake.FakeDynamicClient).PrependReactor("list", "httproutes", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: "httproutes"}, "", nil)
	})
	c := New(Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream()})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	s := waitSynced(t, c)
	hr := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	if s.Access[hr].OK || s.Access[hr].Err == "" {
		t.Fatalf("httproutes must be reported as not accessible, got %+v", s.Access[hr])
	}
}

// TakenAt keeps time.Now's monotonic reading; the API converts it to UTC
// when it writes JSON, so durations never see a wall-clock jump.
func TestSnapshotTakenAtKeepsMonotonicReading(t *testing.T) {
	dyn, kube := testutil.Clients(t)
	c := New(Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream()})
	if at := c.Snapshot().TakenAt; !strings.Contains(at.String(), " m=") {
		t.Fatalf("TakenAt lost its monotonic reading: %s", at)
	}
}
