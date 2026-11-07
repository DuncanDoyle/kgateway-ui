package cache

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	toolscache "k8s.io/client-go/tools/cache"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// EventsGVR is the Access key of the Warning Events watch.
var EventsGVR = schema.GroupVersionResource{Version: "v1", Resource: "events"}

// EventsKind is the kind a check declares in its InputSet to read
// Snapshot.Events, EventDeltas, EventHistoryUnknown and EventsCapped.
var EventsKind = objects.GroupKind{Kind: "Event"}

const (
	// MaxEvents is how many Events the cache keeps; the oldest go first.
	MaxEvents = 10000
	// EventWindow is how long an observed count increase stays in Snapshot.EventDeltas.
	EventWindow = 15 * time.Minute
	// maxObservations bounds the observations kept per Event.
	maxObservations = 64
)

// keptReasons are the Event reasons the cache keeps.
var keptReasons = map[string]bool{"Unhealthy": true, "BackOff": true, "Failed": true, "FailedScheduling": true, "FailedMount": true}

// eventSelector is the server-side field selector of the Events watch.
var eventSelector = fields.AndSelectors(fields.OneTermEqualSelector("type", corev1.EventTypeWarning),
	fields.OneTermEqualSelector("involvedObject.kind", "Pod")).String()

// EventFact is what the cache keeps of a Warning Event on a watched Pod:
// reason, counts and times. The message is never kept.
type EventFact struct {
	UID, PodUID       types.UID
	Namespace, Reason string
	Count             int32
	First, Last       time.Time
	SeriesCount       int32
	SeriesLast        time.Time
}

// Observation is an increase of one Event's count by Delta, seen at At.
type Observation struct {
	At     time.Time
	Reason string
	Delta  int32
}

// EventDeltas turns Event counts into observed increases over EventWindow.
// Not safe for concurrent use.
type EventDeltas struct {
	byEvent map[types.UID]*eventHistory
	// unknown is, per Pod UID, when its uncounted history leaves the window.
	unknown map[types.UID]time.Time
	// start is when the UI began watching; earlier occurrences are never counted.
	start time.Time
}

type eventHistory struct {
	pod    types.UID
	reason string
	last   int32
	obs    []Observation
}

// counter is the Event's count: the series count for a series, else Count.
func counter(ev *corev1.Event) int32 {
	n := ev.Count
	if ev.Series != nil {
		n = ev.Series.Count
	}
	return max(n, 1)
}

// latest is the Event's most recent time.
func latest(ev *corev1.Event) time.Time {
	t := ev.LastTimestamp.Time
	for _, c := range []time.Time{ev.EventTime.Time, ev.FirstTimestamp.Time, ev.CreationTimestamp.Time} {
		if t.IsZero() {
			t = c
		}
	}
	if ev.Series != nil && ev.Series.LastObservedTime.After(t) {
		t = ev.Series.LastObservedTime.Time
	}
	return t
}

// observe records ev's count, seen at now. A first count above 1, or a first
// count of 1 from before start, is a baseline: it adds nothing and marks the Pod's history unknown.
func (d *EventDeltas) observe(ev *corev1.Event, now time.Time) {
	n, at := counter(ev), latest(ev)
	if at.IsZero() || at.After(now) {
		at = now
	}
	h, seen := d.byEvent[ev.UID]
	if !seen {
		h = &eventHistory{pod: ev.InvolvedObject.UID, reason: ev.Reason, last: n}
		d.byEvent[ev.UID] = h
		switch {
		case n > 1:
			d.unknown[h.pod] = latestOf(d.unknown[h.pod], now.Add(EventWindow))
		case at.Before(d.start):
			d.unknown[h.pod] = latestOf(d.unknown[h.pod], at.Add(EventWindow))
		default:
			h.obs = []Observation{{At: at, Reason: ev.Reason, Delta: 1}}
		}
		return
	}
	if delta := n - h.last; delta > 0 {
		h.obs = append(h.obs, Observation{At: at, Reason: ev.Reason, Delta: delta})
		if len(h.obs) > maxObservations {
			h.obs = slices.Delete(h.obs, 0, len(h.obs)-maxObservations)
		}
	}
	h.last = n
}

func latestOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// forgetPod drops every history and unknown mark of a Pod that is gone.
func (d *EventDeltas) forgetPod(pod types.UID) {
	maps.DeleteFunc(d.byEvent, func(_ types.UID, h *eventHistory) bool { return h.pod == pod })
	delete(d.unknown, pod)
}

// view drops what left the window and returns the rest per known Pod UID,
// oldest first, with the known Pods whose history is not fully counted.
func (d *EventDeltas) view(now time.Time, known func(types.UID) bool) (map[types.UID][]Observation, map[types.UID]bool) {
	var obs map[types.UID][]Observation
	var unknown map[types.UID]bool
	from := now.Add(-EventWindow)
	for _, h := range d.byEvent {
		h.obs = slices.DeleteFunc(h.obs, func(o Observation) bool { return o.At.Before(from) })
		if len(h.obs) > 0 && known(h.pod) {
			if obs == nil {
				obs = map[types.UID][]Observation{}
			}
			obs[h.pod] = append(obs[h.pod], h.obs...)
		}
	}
	maps.DeleteFunc(d.unknown, func(_ types.UID, until time.Time) bool { return !now.Before(until) })
	for pod := range d.unknown {
		if known(pod) {
			if unknown == nil {
				unknown = map[types.UID]bool{}
			}
			unknown[pod] = true
		}
	}
	for _, os := range obs {
		slices.SortFunc(os, func(a, b Observation) int {
			if c := a.At.Compare(b.At); c != 0 {
				return c
			}
			return strings.Compare(a.Reason, b.Reason)
		})
	}
	return obs, unknown
}

// podSet is the UIDs the controller and proxy Pod informers hold, counted per informer.
type podSet struct {
	mu sync.RWMutex
	n  map[types.UID]int
	// gone, if set, runs when a UID's count reaches 0, outside the lock.
	gone func(types.UID)
}

func uidOf(o any) (types.UID, bool) {
	if d, ok := o.(toolscache.DeletedFinalStateUnknown); ok {
		o = d.Obj
	}
	m, ok := o.(metav1.Object)
	if !ok {
		return "", false
	}
	return m.GetUID(), true
}

func (s *podSet) add(o any, by int) {
	uid, ok := uidOf(o)
	if !ok {
		return
	}
	s.mu.Lock()
	s.n[uid] += by
	gone := s.n[uid] <= 0
	if gone {
		delete(s.n, uid)
	}
	s.mu.Unlock()
	if gone && s.gone != nil {
		s.gone(uid)
	}
}

func (s *podSet) has(uid types.UID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.n[uid] > 0
}

func (s *podSet) handler() toolscache.ResourceEventHandlerFuncs {
	return toolscache.ResourceEventHandlerFuncs{
		AddFunc: func(o any) { s.add(o, 1) },
		UpdateFunc: func(prev, cur any) {
			// A relist can replace a Pod by a new one of the same name.
			a, _ := uidOf(prev)
			if b, _ := uidOf(cur); a != b {
				s.add(cur, 1)
				s.add(prev, -1)
			}
		},
		DeleteFunc: func(o any) { s.add(o, -1) },
	}
}

// keptEvent is a store key's Event and Pod UIDs and the Event's latest time.
type keptEvent struct {
	uid, pod types.UID
	at       time.Time
}

// eventFilter decides ahead of the store which Events the informer keeps
// (qualifying, at most max) and feeds the deltas.
type eventFilter struct {
	pods *podSet
	now  func() time.Time
	max  int

	mu     sync.Mutex
	kept   map[string]keptEvent // by store key
	capped bool
	deltas EventDeltas
	// streaming is set from a watch-list open until its initial-events-end
	// bookmark; streamed and streamCapped collect what that stream resent.
	streaming    bool
	streamed     map[types.UID]bool
	streamCapped bool
}

func newEventFilter(pods *podSet, now func() time.Time) *eventFilter {
	f := &eventFilter{pods: pods, now: now, max: MaxEvents, kept: map[string]keptEvent{},
		deltas: EventDeltas{byEvent: map[types.UID]*eventHistory{}, unknown: map[types.UID]time.Time{}, start: now()}}
	pods.gone = f.podGone
	return f
}

// podGone drops a vanished Pod's histories. Its stored Events stay until they
// change or are evicted first; snapshots leave them out.
func (f *eventFilter) podGone(pod types.UID) {
	f.mu.Lock()
	f.deltas.forgetPod(pod)
	f.mu.Unlock()
}

// keep reports whether ev qualifies; the server-side selector is checked again,
// since not every client applies it.
func (f *eventFilter) keep(ev *corev1.Event) bool {
	return ev.Type == corev1.EventTypeWarning && ev.InvolvedObject.Kind == "Pod" && keptReasons[ev.Reason] && f.pods.has(ev.InvolvedObject.UID)
}

func storeKey(ev *corev1.Event) string { return ev.Namespace + "/" + ev.Name }

func kept(ev *corev1.Event) keptEvent {
	return keptEvent{uid: ev.UID, pod: ev.InvolvedObject.UID, at: latest(ev)}
}

// list keeps the newest qualifying Events of the complete list l in place and
// resets the kept set and histories to them.
func (f *eventFilter) list(l *corev1.EventList) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l.Items = slices.DeleteFunc(l.Items, func(ev corev1.Event) bool { return !f.keep(&ev) })
	f.capped = len(l.Items) > f.max
	if f.capped {
		slices.SortFunc(l.Items, func(a, b corev1.Event) int { return latest(&b).Compare(latest(&a)) })
		l.Items = l.Items[:f.max]
	}
	f.kept = make(map[string]keptEvent, len(l.Items))
	uids := make(map[types.UID]bool, len(l.Items))
	now := f.now()
	for i := range l.Items {
		ev := &l.Items[i]
		f.kept[storeKey(ev)] = kept(ev)
		uids[ev.UID] = true
		f.deltas.observe(ev, now)
	}
	maps.DeleteFunc(f.deltas.byEvent, func(uid types.UID, _ *eventHistory) bool { return !uids[uid] })
	f.streaming = false
}

// reset starts a watch-list stream, which resends every Event before its
// initial-events-end bookmark.
func (f *eventFilter) reset() {
	f.mu.Lock()
	f.kept = map[string]keptEvent{}
	f.streaming, f.streamed, f.streamCapped = true, map[types.UID]bool{}, false
	f.mu.Unlock()
}

// endStream ends a watch-list stream: histories of Events it did not resend
// go, and the cap reads what the stream dropped.
func (f *eventFilter) endStream() {
	if !f.streaming {
		return
	}
	maps.DeleteFunc(f.deltas.byEvent, func(uid types.UID, _ *eventHistory) bool { return !f.streamed[uid] })
	f.capped, f.streaming, f.streamed = f.streamCapped, false, nil
}

// event maps one watch event to what the store receives: nothing, the event,
// or a deletion of the oldest kept Event before it when the cap is reached.
func (f *eventFilter) event(e watch.Event) []watch.Event {
	ev, ok := e.Object.(*corev1.Event)
	if ok && e.Type == watch.Bookmark && ev.Annotations[metav1.InitialEventsAnnotationKey] == "true" {
		f.mu.Lock()
		f.endStream()
		f.mu.Unlock()
	}
	if !ok || (e.Type != watch.Added && e.Type != watch.Modified && e.Type != watch.Deleted) {
		return []watch.Event{e}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := storeKey(ev)
	prev, had := f.kept[key]
	if had && (e.Type == watch.Deleted || !f.keep(ev) || prev.uid != ev.UID) {
		delete(f.kept, key)
		delete(f.deltas.byEvent, prev.uid)
		if e.Type == watch.Deleted || !f.keep(ev) {
			return []watch.Event{{Type: watch.Deleted, Object: ev}}
		}
		had = false
	}
	if e.Type == watch.Deleted || !f.keep(ev) {
		return nil
	}
	var out []watch.Event
	k := kept(ev)
	if !had && len(f.kept) >= f.max {
		f.capped, f.streamCapped = true, true
		oldKey, old := f.oldest()
		if f.pods.has(old.pod) && k.at.Before(old.at) {
			return nil
		}
		delete(f.kept, oldKey)
		delete(f.deltas.byEvent, old.uid)
		ns, name, _ := strings.Cut(oldKey, "/")
		out = append(out, watch.Event{Type: watch.Deleted, Object: &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, ResourceVersion: ev.ResourceVersion}}})
	}
	f.kept[key] = k
	if f.streaming {
		f.streamed[ev.UID] = true
	}
	f.deltas.observe(ev, f.now())
	return append(out, e)
}

// oldest returns the kept Event to evict: one of a vanished Pod first, else the earliest.
func (f *eventFilter) oldest() (string, keptEvent) {
	var key string
	var old keptEvent
	oldLive := true
	for k, v := range f.kept {
		live := f.pods.has(v.pod)
		better := key == "" || (!live && oldLive) ||
			(live == oldLive && (v.at.Before(old.at) || v.at.Equal(old.at) && k < key))
		if better {
			key, old, oldLive = k, v, live
		}
	}
	return key, old
}

// view is the deltas of known Pods at now, the known Pods with uncounted
// history and whether the cap dropped Events.
func (f *eventFilter) view(now time.Time) (map[types.UID][]Observation, map[types.UID]bool, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	obs, unknown := f.deltas.view(now, f.pods.has)
	return obs, unknown, f.capped
}

// stripEvent keeps only what EventFact and the store need; the message,
// labels, annotations and source never reach the store.
func stripEvent(o any) (any, error) {
	ev, ok := o.(*corev1.Event)
	if !ok {
		return o, nil
	}
	out := &corev1.Event{ObjectMeta: metav1.ObjectMeta{Namespace: ev.Namespace, Name: ev.Name, UID: ev.UID, ResourceVersion: ev.ResourceVersion},
		InvolvedObject: corev1.ObjectReference{UID: ev.InvolvedObject.UID}, Reason: ev.Reason, Count: ev.Count,
		FirstTimestamp: ev.FirstTimestamp, LastTimestamp: ev.LastTimestamp, EventTime: ev.EventTime}
	if ev.Series != nil {
		out.Series = &corev1.EventSeries{Count: ev.Series.Count, LastObservedTime: ev.Series.LastObservedTime}
	}
	return out, nil
}

// eventFact projects a stored Event.
func eventFact(ev *corev1.Event) EventFact {
	f := EventFact{UID: ev.UID, PodUID: ev.InvolvedObject.UID, Namespace: ev.Namespace, Reason: ev.Reason, Count: ev.Count,
		First: ev.FirstTimestamp.Time, Last: ev.LastTimestamp.Time}
	if f.First.IsZero() {
		f.First = ev.EventTime.Time
	}
	if f.Last.IsZero() {
		f.Last = ev.EventTime.Time
	}
	if ev.Series != nil {
		f.SeriesCount, f.SeriesLast = ev.Series.Count, ev.Series.LastObservedTime.Time
	}
	return f
}

// eventInformer watches Warning Events on Pods through f.
func (c *Cache) eventInformer(kube kubernetes.Interface, f *eventFilter) tracked {
	evs := kube.CoreV1().Events("")
	t := c.informer(kube, &corev1.Event{},
		func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) {
			// One unpaged list: list replaces the kept set and histories with it.
			o.FieldSelector, o.Limit, o.Continue = eventSelector, 0, ""
			l, err := evs.List(ctx, o)
			if err != nil {
				return nil, err
			}
			f.list(l)
			return l, nil
		},
		func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) {
			o.FieldSelector = eventSelector
			w, err := evs.Watch(ctx, o)
			if err != nil {
				return nil, err
			}
			if o.SendInitialEvents != nil && *o.SendInitialEvents {
				f.reset()
			}
			return newFilterWatch(w, &c.running, f.event), nil
		})
	_ = t.inf.SetTransform(stripEvent) // fails only on a started informer
	return t
}

// filterWatch passes each event of in through fn, which may drop it or send several.
type filterWatch struct {
	in   watch.Interface
	out  chan watch.Event
	done chan struct{}
	once sync.Once
}

func newFilterWatch(in watch.Interface, loops *sync.WaitGroup, fn func(watch.Event) []watch.Event) watch.Interface {
	w := &filterWatch{in: in, out: make(chan watch.Event), done: make(chan struct{})}
	goTracked(loops, func() {
		defer close(w.out)
		for e := range in.ResultChan() {
			for _, o := range fn(e) {
				select {
				case w.out <- o:
				case <-w.done:
					return
				}
			}
		}
	})
	return w
}

// Stop implements watch.Interface.
func (w *filterWatch) Stop() { w.once.Do(func() { close(w.done); w.in.Stop() }) }

// ResultChan implements watch.Interface.
func (w *filterWatch) ResultChan() <-chan watch.Event { return w.out }
