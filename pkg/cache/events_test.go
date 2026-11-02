package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	kubefake "k8s.io/client-go/kubernetes/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
	clienttesting "k8s.io/client-go/testing"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
)

var proxySel = labels.SelectorFromSet(labels.Set{"app.kubernetes.io/managed-by": "kgateway", "app.kubernetes.io/component": "proxy"})

// optWorld is a cache with every optional watch over fake clients.
type optWorld struct {
	c    *Cache
	kube *kubefake.Clientset
	meta *metadatafake.FakeMetadataClient
	clk  *clocktesting.FakeClock
}

func ctrlPod() *corev1.Pod {
	return testutil.ControllerPod(testutil.ControllerDeployment("kgateway-system", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1), "kgateway-0", true)
}

func proxyPod() *corev1.Pod { return testutil.ProxyPod("infra", "edge", 0, "abc", 0) }

// strayPod belongs to no install: neither Pod selector matches it.
func strayPod() *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "web-0", UID: "pod/shop/web-0", Labels: map[string]string{"app": "web"}}}
}

func newMeta(t *testing.T, objs ...runtime.Object) *metadatafake.FakeMetadataClient {
	t.Helper()
	scheme := metadatafake.NewTestScheme()
	if err := metav1.AddMetaToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return metadatafake.NewSimpleMetadataClient(scheme, objs...)
}

// newOptWorld builds the cache; prepare runs before Start, and the cache is
// waited on until it and every optional watch settled.
func newOptWorld(t *testing.T, events, crds bool, typed []runtime.Object, meta []runtime.Object, prepare func(w *optWorld)) *optWorld {
	t.Helper()
	dyn, kube := testutil.Clients(t, typed...)
	w := &optWorld{kube: kube, meta: newMeta(t, meta...), clk: testutil.FakeClock()}
	w.c = New(Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream(), Now: w.clk.Now,
		PodSelectors: []labels.Selector{podSel}, DeploymentSelectors: []labels.Selector{deploySel},
		ProxyPodSelectors: []labels.Selector{proxySel}, Events: events, CRDs: crds, Metadata: w.meta})
	if prepare != nil {
		prepare(w)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w.c.Start(ctx)
	waitSynced(t, w.c)
	for _, gvr := range []schema.GroupVersionResource{ProxyPodsGVR, EventsGVR, CRDsGVR} {
		waitFor(t, "optional watch "+gvr.Resource+" settled", func() bool {
			a, ok := w.c.Snapshot().Access[gvr]
			return !ok || a.OK || a.Err != ""
		})
	}
	return w
}

func eventNames(s *Snapshot) []string {
	var out []string
	for _, e := range s.Events {
		_, name, _ := strings.Cut(string(e.UID), "/"+e.Namespace+"/")
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// update writes ev with a new resourceVersion, as the API server would.
func (w *optWorld) update(t *testing.T, ev *corev1.Event) {
	t.Helper()
	ev.ResourceVersion += "1"
	if _, err := w.kube.CoreV1().Events(ev.Namespace).Update(context.Background(), ev, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func (w *optWorld) create(t *testing.T, ev *corev1.Event) {
	t.Helper()
	if _, err := w.kube.CoreV1().Events(ev.Namespace).Create(context.Background(), ev, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
}

// Events of Pods outside every install, of other reasons, types or kinds
// never reach the store, from the list or the watch.
func TestEventsTransformKeepsOnlyInstallPods(t *testing.T) {
	ctrl, proxy, stray := ctrlPod(), proxyPod(), strayPod()
	at := testutil.Now.Add(-time.Minute)
	normal := testutil.WarningEvent(ctrl, "normal", "Unhealthy", at)
	normal.Type = corev1.EventTypeNormal
	node := testutil.WarningEvent(ctrl, "node", "Unhealthy", at)
	node.InvolvedObject.Kind = "Node"
	w := newOptWorld(t, true, false, []runtime.Object{ctrl, proxy, stray,
		testutil.WarningEvent(ctrl, "ctrl-probe", "Unhealthy", at), testutil.WarningEvent(proxy, "proxy-backoff", "BackOff", at),
		testutil.WarningEvent(stray, "stray-probe", "Unhealthy", at), testutil.WarningEvent(ctrl, "ctrl-other", "NodeNotReady", at),
		normal, node}, nil, nil)
	waitFor(t, "the Pods' UIDs", func() bool { return w.c.uids.has(ctrl.UID) && w.c.uids.has(proxy.UID) })
	if got, want := eventNames(w.c.Snapshot()), []string{"ctrl-probe", "proxy-backoff"}; !slices.Equal(got, want) {
		t.Fatalf("listed Events %v, want %v", got, want)
	}
	w.create(t, testutil.WarningEvent(stray, "stray-late", "BackOff", testutil.Now))
	w.create(t, testutil.WarningEvent(proxy, "proxy-late", "Failed", testutil.Now))
	waitFor(t, "the proxy Pod's new Event", func() bool { return slices.Contains(eventNames(w.c.Snapshot()), "proxy-late") })
	if got, want := eventNames(w.c.Snapshot()), []string{"ctrl-probe", "proxy-backoff", "proxy-late"}; !slices.Equal(got, want) {
		t.Fatalf("watched Events %v, want %v", got, want)
	}
	if n := len(w.c.events.inf.GetStore().List()); n != 3 {
		t.Fatalf("the store holds %d Events, want 3", n)
	}
}

// Review Focus 5: an Event that arrives before its Pod is dropped; its next
// update after the Pod arrives sets a baseline, and later increments count.
func TestEventBeforePodIsKeptOnNextUpdate(t *testing.T) {
	proxy := proxyPod()
	ev := testutil.WarningEvent(proxy, "probe", "Unhealthy", testutil.Now.Add(-time.Minute))
	w := newOptWorld(t, true, false, []runtime.Object{ctrlPod(), ev}, nil, nil)
	if s := w.c.Snapshot(); len(s.Events) != 0 {
		t.Fatalf("an Event of an unknown Pod was kept: %+v", s.Events)
	}
	if _, err := w.kube.CoreV1().Pods(proxy.Namespace).Create(context.Background(), proxy, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the proxy Pod's UID", func() bool { return w.c.uids.has(proxy.UID) })
	ev.Count = 2
	w.update(t, ev)
	waitFor(t, "the updated Event", func() bool { s := w.c.Snapshot(); return len(s.Events) == 1 && s.Events[0].Count == 2 })
	s := w.c.Snapshot()
	if len(s.EventDeltas[proxy.UID]) != 0 || !s.EventHistoryUnknown[proxy.UID] {
		t.Fatalf("baseline: deltas %+v, unknown %v; want none and unknown", s.EventDeltas, s.EventHistoryUnknown)
	}
	ev.Count, ev.LastTimestamp = 3, metav1.NewTime(testutil.Now)
	w.update(t, ev)
	waitFor(t, "the increment", func() bool { return len(w.c.Snapshot().EventDeltas[proxy.UID]) == 1 })
	if got, want := w.c.Snapshot().EventDeltas[proxy.UID], []Observation{{At: testutil.Now, Reason: "Unhealthy", Delta: 1}}; !slices.Equal(got, want) {
		t.Fatalf("deltas %+v, want %+v", got, want)
	}
}

// An Event first seen with count 50 contributes nothing and marks the Pod's
// history unknown for one window.
func TestFirstSeenCountIsUnknown(t *testing.T) {
	ctrl := ctrlPod()
	ev := testutil.WarningEvent(ctrl, "probe", "Unhealthy", testutil.Now.Add(-time.Minute))
	ev.Count = 50
	fresh := testutil.WarningEvent(ctrl, "fresh", "BackOff", testutil.Now.Add(-time.Minute))
	w := newOptWorld(t, true, false, []runtime.Object{ctrl, ev, fresh}, nil, nil)
	s := w.c.Snapshot()
	if got, want := s.EventDeltas[ctrl.UID], []Observation{{At: testutil.Now.Add(-time.Minute), Reason: "BackOff", Delta: 1}}; !slices.Equal(got, want) {
		t.Fatalf("deltas %+v, want only the fresh Event's %+v", got, want)
	}
	if !s.EventHistoryUnknown[ctrl.UID] {
		t.Fatal("history not marked unknown")
	}
	w.clk.Step(EventWindow + time.Second)
	if s := w.c.Snapshot(); s.EventHistoryUnknown[ctrl.UID] || len(s.EventDeltas) != 0 {
		t.Fatalf("after a window: unknown %v, deltas %+v; want neither", s.EventHistoryUnknown, s.EventDeltas)
	}
}

// Review finding 7: a series counts by its series count only, never by
// Count and the series count together.
func TestSeriesCountedOnce(t *testing.T) {
	ctrl := ctrlPod()
	ev := testutil.WarningEvent(ctrl, "probe", "Unhealthy", testutil.Now.Add(-time.Minute))
	w := newOptWorld(t, true, false, []runtime.Object{ctrl, ev}, nil, nil)
	sum := func() (n int32) {
		for _, o := range w.c.Snapshot().EventDeltas[ctrl.UID] {
			n += o.Delta
		}
		return n
	}
	ev.Count, ev.Series = 2, &corev1.EventSeries{Count: 2, LastObservedTime: metav1.NewMicroTime(testutil.Now.Add(-30 * time.Second))}
	w.update(t, ev)
	waitFor(t, "the second occurrence", func() bool { return sum() == 2 })
	ev.Series = &corev1.EventSeries{Count: 3, LastObservedTime: metav1.NewMicroTime(testutil.Now)}
	w.update(t, ev)
	waitFor(t, "the third occurrence", func() bool { return sum() >= 3 })
	if got := w.c.Snapshot().EventDeltas[ctrl.UID]; len(got) != 3 || sum() != 3 {
		t.Fatalf("deltas %+v, want three observations of 1", got)
	}
}

// An Event message never reaches the store or the snapshot.
func TestEventTransformKeepsNoMessage(t *testing.T) {
	ctrl := ctrlPod()
	ev := testutil.WarningEvent(ctrl, "probe", "Unhealthy", testutil.Now)
	ev.Message = "Readiness probe failed: token=secret-value"
	ev.Annotations = map[string]string{"note": "secret-value"}
	ev.Source = corev1.EventSource{Component: "kubelet", Host: "secret-value"}
	w := newOptWorld(t, true, false, []runtime.Object{ctrl, ev}, nil, nil)
	stored := w.c.events.inf.GetStore().List()
	if len(stored) != 1 {
		t.Fatalf("store holds %d Events, want 1", len(stored))
	}
	b, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret-value") {
		t.Fatalf("the stored Event keeps the secret: %s", b)
	}
	if got := fmt.Sprintf("%+v", w.c.Snapshot()); strings.Contains(got, "secret-value") {
		t.Fatal("the snapshot keeps the secret")
	}
}

// At most max Events are kept: the oldest go, an Event older than every
// kept one is not added, and the snapshot says the cap dropped Events.
func TestEventsCap(t *testing.T) {
	ctrl := ctrlPod()
	typed := []runtime.Object{ctrl}
	for i := range 5 {
		typed = append(typed, testutil.WarningEvent(ctrl, fmt.Sprintf("e%d", i), "Unhealthy", testutil.Now.Add(time.Duration(i-5)*time.Minute)))
	}
	w := newOptWorld(t, true, false, typed, nil, func(w *optWorld) { w.c.evf.max = 3 })
	s := w.c.Snapshot()
	if got, want := eventNames(s), []string{"e2", "e3", "e4"}; !slices.Equal(got, want) || !s.EventsCapped {
		t.Fatalf("listed %v capped %v, want %v capped", got, s.EventsCapped, want)
	}
	w.create(t, testutil.WarningEvent(ctrl, "old", "Unhealthy", testutil.Now.Add(-time.Hour)))
	w.create(t, testutil.WarningEvent(ctrl, "e5", "Unhealthy", testutil.Now))
	waitFor(t, "the newest Event", func() bool { return slices.Contains(eventNames(w.c.Snapshot()), "e5") })
	if got, want := eventNames(w.c.Snapshot()), []string{"e3", "e4", "e5"}; !slices.Equal(got, want) {
		t.Fatalf("after the watch %v, want %v", got, want)
	}
	if n := len(w.c.events.inf.GetStore().List()); n != 3 {
		t.Fatalf("the store holds %d Events, want 3", n)
	}
}

// Only the Gateway API groups' CRDs are kept, with their bundle annotations only.
func TestCRDMetadataKeepsGatewayGroups(t *testing.T) {
	gw := testutil.CRDMetadata("gateways.gateway.networking.k8s.io", "v1.4.0", "standard")
	gw.Annotations["kubectl.kubernetes.io/last-applied-configuration"] = `{"spec":"large"}`
	other := testutil.CRDMetadata("widgets.example.com", "", "")
	w := newOptWorld(t, false, true, nil, []runtime.Object{gw, other,
		testutil.CRDMetadata("xlistenersets.gateway.networking.x-k8s.io", "v1.4.0", "experimental")}, nil)
	want := []CRDMeta{{"gateways.gateway.networking.k8s.io", "gateway.networking.k8s.io", "v1.4.0", "standard"},
		{"xlistenersets.gateway.networking.x-k8s.io", "gateway.networking.x-k8s.io", "v1.4.0", "experimental"}}
	if got := w.c.Snapshot().CRDs; !slices.Equal(got, want) {
		t.Fatalf("CRDs %+v, want %+v", got, want)
	}
	for _, o := range w.c.crds.inf.GetStore().List() {
		if a := o.(*metav1.PartialObjectMetadata).Annotations; len(a) != 2 {
			t.Fatalf("stored annotations %v, want the two bundle annotations", a)
		}
	}
	for _, m := range []*metav1.PartialObjectMetadata{testutil.CRDMetadata("foos.example.com", "", ""),
		testutil.CRDMetadata("httproutes.gateway.networking.k8s.io", "v1.4.0", "standard")} {
		if _, err := w.meta.Resource(CRDsGVR).(metadatafake.MetadataClient).CreateFake(m, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "the watched Gateway API CRD", func() bool { return len(w.c.Snapshot().CRDs) == 3 })
	if n := len(w.c.crds.inf.GetStore().List()); n != 3 {
		t.Fatalf("the store holds %d CRDs, want 3", n)
	}
}

// With Events and CRDs off, neither is listed or watched and neither has an
// Access entry; on, both are, the Events with the Warning-on-Pods selector.
func TestToggleOffStartsNoWatch(t *testing.T) {
	off := newOptWorld(t, false, false, []runtime.Object{ctrlPod()}, nil, nil)
	for _, a := range off.kube.Actions() {
		if a.GetResource().Resource == "events" {
			t.Fatalf("events off, yet %s events", a.GetVerb())
		}
	}
	if n := len(off.meta.Actions()); n != 0 {
		t.Fatalf("CRDs off, yet %d metadata calls", n)
	}
	if s := off.c.Snapshot(); hasAccess(s, EventsGVR) || hasAccess(s, CRDsGVR) {
		t.Fatalf("off watches have Access entries: %v", s.Access)
	}
	on := newOptWorld(t, true, true, []runtime.Object{ctrlPod()}, nil, nil)
	waitFor(t, "the Events watch", func() bool {
		return slices.ContainsFunc(on.kube.Actions(), func(a clienttesting.Action) bool {
			w, ok := a.(clienttesting.WatchAction)
			return ok && a.GetResource().Resource == "events" && w.GetWatchRestrictions().Fields.String() == fields.ParseSelectorOrDie(eventSelector).String()
		})
	})
	waitFor(t, "the CRD watch", func() bool {
		return slices.ContainsFunc(on.meta.Actions(), func(a clienttesting.Action) bool {
			return a.GetVerb() == "watch" && a.GetResource() == CRDsGVR
		})
	})
}

func hasAccess(s *Snapshot, gvr schema.GroupVersionResource) bool { _, ok := s.Access[gvr]; return ok }

// A denied optional watch reports its error under its own key; the core
// kinds stay readable and the cache syncs.
func TestDeniedWatchReportsAccess(t *testing.T) {
	deny := func(gr schema.GroupResource) func(clienttesting.Action) (bool, runtime.Object, error) {
		return func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(gr, "", nil)
		}
	}
	w := newOptWorld(t, true, true, []runtime.Object{ctrlPod(), proxyPod()}, nil, func(w *optWorld) {
		w.kube.PrependReactor("list", "events", deny(schema.GroupResource{Resource: "events"}))
		w.kube.PrependReactor("list", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
			if a.(clienttesting.ListAction).GetListRestrictions().Labels.String() != proxySel.String() {
				return false, nil, nil
			}
			return deny(schema.GroupResource{Resource: "pods"})(a)
		})
		w.meta.PrependReactor("list", "customresourcedefinitions", deny(CRDsGVR.GroupResource()))
	})
	s := w.c.Snapshot()
	for _, gvr := range []schema.GroupVersionResource{ProxyPodsGVR, EventsGVR, CRDsGVR} {
		if a := s.Access[gvr]; a.OK || !strings.Contains(a.Err, "forbidden") {
			t.Errorf("%s access %+v, want forbidden", gvr.Resource, a)
		}
	}
	for _, gvr := range []schema.GroupVersionResource{NamespacesGVR, PodsGVR, DeploymentsGVR} {
		if !s.Access[gvr].OK {
			t.Errorf("core %s access %+v, want OK", gvr.Resource, s.Access[gvr])
		}
	}
	if len(s.Pods) != 1 || len(s.ProxyPods) != 0 {
		t.Fatalf("controller Pods %d, proxy Pods %d; want 1 and 0", len(s.Pods), len(s.ProxyPods))
	}
	if s.Source.Access[EventsGVR.String()] != s.Access[EventsGVR] {
		t.Fatal("the fingerprint does not carry the Events access")
	}
}

// I1: the reflector pages its lists, but the Events list asks for one page,
// so the kept set, histories and cap come from the whole list.
func TestEventsListIsNotPaged(t *testing.T) {
	ctrl := ctrlPod()
	typed := []runtime.Object{ctrl}
	for i := range 3 {
		typed = append(typed, testutil.WarningEvent(ctrl, fmt.Sprintf("e%d", i), "Unhealthy", testutil.Now.Add(time.Duration(i-3)*time.Minute)))
	}
	var limits []int64
	w := newOptWorld(t, true, false, typed, nil, func(w *optWorld) {
		w.c.evf.max = 2
		w.kube.PrependReactor("list", "events", func(a clienttesting.Action) (bool, runtime.Object, error) {
			o := a.(clienttesting.ListActionImpl).ListOptions
			limits = append(limits, o.Limit)
			if o.Limit == 0 {
				return false, nil, nil
			}
			// A paging server: one Event per page.
			all, err := w.kube.Tracker().List(corev1.SchemeGroupVersion.WithResource("events"), corev1.SchemeGroupVersion.WithKind("Event"), "")
			if err != nil {
				return true, nil, err
			}
			l := all.(*corev1.EventList)
			i := 0
			if o.Continue != "" {
				if _, err := fmt.Sscan(o.Continue, &i); err != nil {
					return true, nil, err
				}
			}
			page := &corev1.EventList{Items: l.Items[i : i+1]}
			if i+1 < len(l.Items) {
				page.Continue = fmt.Sprint(i + 1)
			}
			return true, page, nil
		})
	})
	s := w.c.Snapshot()
	if got, want := eventNames(s), []string{"e1", "e2"}; !slices.Equal(got, want) || !s.EventsCapped {
		t.Fatalf("listed %v capped %v, want %v capped", got, s.EventsCapped, want)
	}
	if n := len(s.EventDeltas[ctrl.UID]); n != 2 {
		t.Fatalf("%d observations, want one per kept Event", n)
	}
	if len(limits) == 0 || slices.ContainsFunc(limits, func(l int64) bool { return l != 0 }) {
		t.Fatalf("list limits %v, want only 0", limits)
	}
}

// Review Focus 2: a deleted Pod's Events and deltas leave the snapshot at once,
// and its stored Events are evicted before any live Pod's.
func TestVanishedPodLeavesNoEvents(t *testing.T) {
	ctrl, proxy := ctrlPod(), proxyPod()
	busy := testutil.WarningEvent(proxy, "busy", "Unhealthy", testutil.Now.Add(-time.Second))
	busy.Count = 9
	w := newOptWorld(t, true, false, []runtime.Object{ctrl, proxy, busy,
		testutil.WarningEvent(proxy, "probe", "Unhealthy", testutil.Now.Add(-time.Second)),
		testutil.WarningEvent(ctrl, "old", "BackOff", testutil.Now.Add(-10*time.Minute))}, nil, func(w *optWorld) { w.c.evf.max = 3 })
	s := w.c.Snapshot()
	if len(s.Events) != 3 || len(s.EventDeltas[proxy.UID]) != 1 || !s.EventHistoryUnknown[proxy.UID] {
		t.Fatalf("before: events %v, deltas %+v, unknown %v", eventNames(s), s.EventDeltas, s.EventHistoryUnknown)
	}
	if err := w.kube.CoreV1().Pods(proxy.Namespace).Delete(context.Background(), proxy.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the Pod to vanish", func() bool { return !w.c.uids.has(proxy.UID) })
	s = w.c.Snapshot()
	if got := eventNames(s); !slices.Equal(got, []string{"old"}) || len(s.EventDeltas) != 1 || s.EventDeltas[proxy.UID] != nil || len(s.EventHistoryUnknown) != 0 {
		t.Fatalf("after: events %v, deltas %+v, unknown %v; want only the live Pod's", got, s.EventDeltas, s.EventHistoryUnknown)
	}
	if !s.Source.Equal(w.c.Fingerprint()) {
		t.Fatal("the fingerprint counts the hidden Events")
	}
	w.create(t, testutil.WarningEvent(ctrl, "new", "BackOff", testutil.Now))
	waitFor(t, "the new Event", func() bool { return slices.Contains(eventNames(w.c.Snapshot()), "new") })
	if got := eventNames(w.c.Snapshot()); !slices.Equal(got, []string{"new", "old"}) {
		t.Fatalf("events %v, want the live Pod's old one kept over the vanished Pod's", got)
	}
}

// M5: a Pod's unknown history stays for its window after the Event goes.
func TestUnknownHistoryOutlivesTheEvent(t *testing.T) {
	ctrl := ctrlPod()
	ev := testutil.WarningEvent(ctrl, "probe", "Unhealthy", testutil.Now)
	ev.Count = 50
	w := newOptWorld(t, true, false, []runtime.Object{ctrl, ev}, nil, nil)
	if err := w.kube.CoreV1().Events(ev.Namespace).Delete(context.Background(), ev.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the deletion", func() bool { return len(w.c.Snapshot().Events) == 0 })
	if !w.c.Snapshot().EventHistoryUnknown[ctrl.UID] {
		t.Fatal("unknown history dropped with the Event")
	}
	w.clk.Step(EventWindow)
	if w.c.Snapshot().EventHistoryUnknown[ctrl.UID] {
		t.Fatal("unknown history outlived its window")
	}
}

// M1: a watch-list stream resends every Event; at its initial-events-end
// bookmark, histories of Events it did not resend go and the cap is re-read.
// The fakes do not stream, so this drives the filter directly.
func TestWatchListStreamPrunesAtBookmark(t *testing.T) {
	ctrl := ctrlPod()
	pods := &podSet{n: map[types.UID]int{ctrl.UID: 1}}
	f := newEventFilter(pods, func() time.Time { return testutil.Now })
	f.max = 2
	evs := make([]*corev1.Event, 3)
	for i := range evs {
		evs[i] = testutil.WarningEvent(ctrl, fmt.Sprintf("e%d", i), "Unhealthy", testutil.Now.Add(time.Duration(i-3)*time.Minute))
	}
	l := &corev1.EventList{Items: []corev1.Event{*evs[0], *evs[1], *evs[2]}}
	f.list(l)
	if !f.capped || len(f.deltas.byEvent) != 2 {
		t.Fatalf("after the list: capped %v, %d histories", f.capped, len(f.deltas.byEvent))
	}
	f.reset()
	f.event(watch.Event{Type: watch.Added, Object: evs[2]})
	bookmark := &corev1.Event{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "9", Annotations: map[string]string{metav1.InitialEventsAnnotationKey: "true"}}}
	if out := f.event(watch.Event{Type: watch.Bookmark, Object: bookmark}); len(out) != 1 {
		t.Fatalf("bookmark not passed on: %v", out)
	}
	if f.capped || len(f.deltas.byEvent) != 1 || f.deltas.byEvent[evs[2].UID] == nil {
		t.Fatalf("after the stream: capped %v, histories %v; want only e2, not capped", f.capped, f.deltas.byEvent)
	}
}
