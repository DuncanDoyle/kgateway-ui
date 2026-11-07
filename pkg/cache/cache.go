// Package cache keeps a shared, read-only informer cache of the kinds the UI
// watches and exposes it as immutable snapshots.
package cache

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	toolscache "k8s.io/client-go/tools/cache"

	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/trigger"
)

// Access reports whether a kind can currently be read.
type Access struct {
	OK  bool   `json:"ok"`
	Err string `json:"error,omitempty"`
	// Denied marks an Err the API server returned as 403 Forbidden.
	Denied bool `json:"denied,omitempty"`
}

// ListTimeout bounds every List call an informer makes, so a hung list ends
// as a recorded failure instead of keeping old evidence alive.
const ListTimeout = time.Minute

// GVRs under which the typed informers report their Access.
var (
	NamespacesGVR  = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	PodsGVR        = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	DeploymentsGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	// ProxyPodsGVR keys the proxy Pod watch, apart from the controller Pods' PodsGVR.
	ProxyPodsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods#proxy"}
)

// ProxyPodsKind is the kind a check declares in its InputSet to read Snapshot.ProxyPods.
var ProxyPodsKind = objects.GroupKind{Kind: "Pod#proxy"}

// Snapshot is an immutable view of the cache at TakenAt. Callers must not mutate it.
type Snapshot struct {
	// Objects lists each kind sorted by "namespace/name", so readers may
	// binary-search it (pkg/api does).
	Objects map[schema.GroupVersionResource][]*unstructured.Unstructured
	// Access holds every served kind plus NamespacesGVR, PodsGVR and
	// DeploymentsGVR, and the key of each optional watch that runs.
	Access      map[schema.GroupVersionResource]Access
	Namespaces  []*corev1.Namespace
	Pods        []*corev1.Pod
	Deployments []*appsv1.Deployment
	// ProxyPods are the Pods the proxy Pod selectors match.
	ProxyPods []*corev1.Pod
	// Events are the kept Warning Events of controller and proxy Pods, by UID.
	Events []EventFact
	// EventDeltas are the observed Event count increases of the last
	// EventWindow per Pod UID, oldest first.
	EventDeltas map[types.UID][]Observation
	// EventHistoryUnknown marks Pods with an Event whose earlier count was not observed.
	EventHistoryUnknown map[types.UID]bool
	// EventsCapped is true when MaxEvents made the cache drop Events.
	EventsCapped bool
	// CRDs are the Gateway API CRDs, by name.
	CRDs []CRDMeta
	// Services and EndpointSlices are the cut-down objects of the Service and
	// EndpointSlice watches, by namespace and name.
	Services       []ServiceFact
	EndpointSlices []SliceFact
	// Synced is true once every core informer (served kinds, Namespaces,
	// Pods and Deployments) has synced or failed. Optional watches and
	// list-only kinds never hold it back.
	Synced  bool
	TakenAt time.Time
	// Source is the fingerprint of exactly the objects and access states copied.
	Source Fingerprint
}

// Snapshotter returns the current snapshot.
type Snapshotter interface{ Snapshot() *Snapshot }

// Config configures a Cache. Pods and Deployments are only watched through
// the given label selectors, never cluster-wide (design §4.1).
type Config struct {
	Dynamic             dynamic.Interface
	Kube                kubernetes.Interface
	Served              []kinds.Served
	PodSelectors        []labels.Selector
	DeploymentSelectors []labels.Selector
	// ProxyPodSelectors select gateway proxy Pods, watched apart from controller Pods.
	ProxyPodSelectors []labels.Selector
	// Events watches Warning Events on controller and proxy Pods.
	Events bool
	// CRDs watches Gateway API CRD metadata through Metadata; a nil Metadata means no watch.
	CRDs     bool
	Metadata metadata.Interface
	// Endpoints watches Services and EndpointSlices cluster-wide, cut down at the informer.
	Endpoints bool
	// Now stamps snapshots; nil means time.Now.
	Now func() time.Time
	// Changes is notified on every informer event (initial-list adds
	// included), on access transitions and once after every informer has
	// settled; nil means no notifications (D-019).
	Changes *trigger.Signal
}

// progress tracks whether one informer's cache is current (D-014). A
// failure (a failed List or Watch call, or a watch.Error event) makes the kind
// unavailable. It is available again after a successful classic Watch call
// opened after the latest failure (the watch resumes from the reflector's last
// resource version, or follows a relist), or after the initial-events-end
// bookmark of a watch-list stream opened after it. A successful List alone does
// not recover: the Watch call that follows must succeed too, which keeps a kind
// whose list is allowed but watch is forbidden unavailable. Events on a watch
// opened before the failure never recover it.
type progress struct {
	mu       sync.Mutex
	err      string          // failure not yet recovered from; "" when none
	denied   bool            // err is a 403
	failures uint64          // failures so far; a watch-list stream remembers the count it was opened at
	onChange func()          // called when err turns empty or non-empty; may be nil
	loops    *sync.WaitGroup // joins the watch wrappers' goroutines; may be nil
}

func (p *progress) failure(err error) {
	p.mu.Lock()
	changed := p.err == ""
	p.err = err.Error()
	p.denied = apierrors.IsForbidden(err)
	p.failures++
	p.mu.Unlock()
	p.changed(changed)
}

// recover clears the failure if none happened since the watch was opened at openedAt.
func (p *progress) recover(openedAt uint64) {
	p.mu.Lock()
	changed := false
	if p.failures == openedAt {
		changed = p.err != ""
		p.err, p.denied = "", false
	}
	p.mu.Unlock()
	p.changed(changed)
}

func (p *progress) changed(changed bool) {
	if changed && p.onChange != nil {
		p.onChange()
	}
}

func (p *progress) count() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.failures
}

// event records a watch.Error event as a failure, and the initial-events-end
// bookmark of a watch-list stream opened at openedAt as a recovery.
func (p *progress) event(e watch.Event, watchList bool, openedAt uint64) {
	switch e.Type {
	case watch.Error:
		p.failure(apierrors.FromObject(e.Object))
	case watch.Bookmark:
		if !watchList {
			return
		}
		if m, err := meta.Accessor(e.Object); err == nil && m.GetAnnotations()[metav1.InitialEventsAnnotationKey] == "true" {
			p.recover(openedAt)
		}
	}
}

// access reports whether the kind is current; synced is the informer's HasSynced.
func (p *progress) access(synced bool) Access {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.err != "":
		return Access{Err: p.err, Denied: p.denied}
	case !synced:
		return Access{}
	}
	return Access{OK: true}
}

// instrument wraps list and watch so p sees every outcome the reflector sees,
// including connection-refused and 429 retries and errors inside a stream. It
// never changes what the reflector does: no extra calls and no forced re-lists,
// so it adds no load while the API server is throttling.
func instrument(p *progress, list toolscache.ListWithContextFunc, watchFn toolscache.WatchFuncWithContext) *toolscache.ListWatch {
	return &toolscache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) {
			ctx, cancel := context.WithTimeout(ctx, ListTimeout)
			defer cancel()
			obj, err := list(ctx, o)
			if err != nil {
				p.failure(err)
			}
			return obj, err
		},
		WatchFuncWithContext: func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) {
			openedAt := p.count()
			w, err := watchFn(ctx, o)
			if err != nil {
				p.failure(err)
				return nil, err
			}
			watchList := o.SendInitialEvents != nil && *o.SendInitialEvents
			if !watchList {
				p.recover(openedAt) // a working classic watch recovers the kind
			}
			return observe(w, p.loops, func(e watch.Event) { p.event(e, watchList, openedAt) }), nil
		},
	}
}

// observed passes every event of in to its reader after calling f. Unlike
// watch.Filter, its loop also ends when the reader calls Stop mid-send.
type observed struct {
	in   watch.Interface
	out  chan watch.Event
	stop chan struct{}
	once sync.Once
}

func observe(in watch.Interface, loops *sync.WaitGroup, f func(watch.Event)) watch.Interface {
	w := &observed{in: in, out: make(chan watch.Event), stop: make(chan struct{})}
	goTracked(loops, func() { w.loop(f) })
	return w
}

func (w *observed) loop(f func(watch.Event)) {
	defer close(w.out)
	for e := range w.in.ResultChan() {
		f(e)
		select {
		case w.out <- e:
		case <-w.stop:
			for range w.in.ResultChan() { // drain until the stopped watch closes
			}
			return
		}
	}
}

func (w *observed) ResultChan() <-chan watch.Event { return w.out }

// Stop stops the inner watch and ends the loop, even when nobody reads out.
func (w *observed) Stop() {
	w.once.Do(func() {
		close(w.stop)
		w.in.Stop()
	})
}

// tracked is an informer with the progress of its own List and Watch calls.
// reg is the change handler's registration; nil without Config.Changes or
// when registration failed.
type tracked struct {
	inf      toolscache.SharedIndexInformer
	progress *progress
	reg      toolscache.ResourceEventHandlerRegistration
}

// Cache is a set of informers whose contents are read through Snapshot.
type Cache struct {
	now     func() time.Time
	changes *trigger.Signal
	dyn     map[schema.GroupVersionResource]tracked
	// listOnly are the dyn kinds that never hold back Synced or Settled (D-034).
	listOnly map[schema.GroupVersionResource]bool
	ns       tracked
	pods     []tracked
	deploys  []tracked
	// Optional watches; nil or empty when off.
	proxyPods    []tracked
	events, crds *tracked
	services     *tracked
	slices       *tracked
	evf          *eventFilter
	uids         *podSet
	uidRegs      []toolscache.ResourceEventHandlerRegistration
	// stopOptional cancels the optional watches' context; optional tracks their goroutines.
	stopOptional func()
	optional     sync.WaitGroup
	// running tracks every goroutine Start launches, for Wait.
	running sync.WaitGroup
	// settled is set once every informer has settled, after the settle notify.
	settled atomic.Bool
	// handlerHook, if set, runs in each event handler before it notifies (tests).
	handlerHook atomic.Pointer[func()]
}

// handled is every event handler: the test hook, then the notification.
func (c *Cache) handled() {
	if h := c.handlerHook.Load(); h != nil {
		(*h)()
	}
	c.changes.Notify()
}

// informer builds an informer over an instrumented ListerWatcher. client tells
// the reflector whether the client supports watch-list semantics (fakes do not).
func (c *Cache) informer(client any, example runtime.Object, list toolscache.ListWithContextFunc, watchFn toolscache.WatchFuncWithContext) tracked {
	p := &progress{onChange: c.changes.Notify, loops: &c.running}
	lw := toolscache.ToListWatcherWithWatchListSemantics(instrument(p, list, watchFn), client)
	t := tracked{inf: toolscache.NewSharedIndexInformer(lw, example, 0, toolscache.Indexers{}), progress: p}
	if c.changes != nil {
		notify := func(any) { c.handled() }
		// Fails only on a stopped informer. Without a handler the kind still
		// settles on its own sync, and the confirm tick catches its changes.
		t.reg, _ = t.inf.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
			AddFunc: notify, UpdateFunc: func(_, _ any) { c.handled() }, DeleteFunc: notify})
	}
	return t
}

// New creates the informers. Nothing runs until Start.
func New(cfg Config) *Cache {
	c := &Cache{now: cfg.Now, changes: cfg.Changes, dyn: map[schema.GroupVersionResource]tracked{}, listOnly: map[schema.GroupVersionResource]bool{}}
	if c.now == nil {
		c.now = time.Now
	}
	for _, s := range cfg.Served {
		ri := cfg.Dynamic.Resource(s.GVR())
		c.listOnly[s.GVR()] = s.ListOnly
		c.dyn[s.GVR()] = c.informer(cfg.Dynamic, &unstructured.Unstructured{},
			func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) { return ri.List(ctx, o) },
			func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) { return ri.Watch(ctx, o) })
	}
	nss := cfg.Kube.CoreV1().Namespaces()
	c.ns = c.informer(cfg.Kube, &corev1.Namespace{},
		func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) { return nss.List(ctx, o) },
		func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) { return nss.Watch(ctx, o) })
	pods, deploys := cfg.Kube.CoreV1().Pods(""), cfg.Kube.AppsV1().Deployments("")
	podInformer := func(sel labels.Selector) tracked {
		return c.informer(cfg.Kube, &corev1.Pod{},
			func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) {
				o.LabelSelector = sel.String()
				return pods.List(ctx, o)
			},
			func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) {
				o.LabelSelector = sel.String()
				return pods.Watch(ctx, o)
			})
	}
	for _, sel := range cfg.PodSelectors {
		c.pods = append(c.pods, podInformer(sel))
	}
	for _, sel := range cfg.ProxyPodSelectors {
		c.proxyPods = append(c.proxyPods, podInformer(sel))
	}
	for _, sel := range cfg.DeploymentSelectors {
		c.deploys = append(c.deploys, c.informer(cfg.Kube, &appsv1.Deployment{},
			func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) {
				o.LabelSelector = sel.String()
				return deploys.List(ctx, o)
			},
			func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) {
				o.LabelSelector = sel.String()
				return deploys.Watch(ctx, o)
			}))
	}
	if cfg.Events {
		c.uids = &podSet{n: map[types.UID]int{}}
		for _, t := range append(slices.Clone(c.pods), c.proxyPods...) {
			reg, _ := t.inf.AddEventHandler(c.uids.handler()) // fails only on a stopped informer
			c.uidRegs = append(c.uidRegs, reg)
		}
		c.evf = newEventFilter(c.uids, c.now)
		t := c.eventInformer(cfg.Kube, c.evf)
		c.events = &t
	}
	if cfg.CRDs && cfg.Metadata != nil {
		t := c.crdInformer(cfg.Metadata)
		c.crds = &t
	}
	if cfg.Endpoints {
		svcs, es := c.serviceInformer(cfg.Kube), c.sliceInformer(cfg.Kube)
		c.services, c.slices = &svcs, &es
	}
	return c
}

// all returns the core informers: Namespaces, served kinds other than
// list-only ones, Pods and Deployments.
func (c *Cache) all() []tracked {
	out := []tracked{c.ns}
	for gvr, t := range c.dyn {
		if !c.listOnly[gvr] {
			out = append(out, t)
		}
	}
	return append(append(out, c.pods...), c.deploys...)
}

// syncPoll is how often Start checks whether every informer has settled.
const syncPoll = 50 * time.Millisecond

// Start runs all informers until ctx is cancelled. With Config.Changes it
// notifies once when every informer has settled: its handler has received the
// initial list, or its List or Watch failed. Later failures and recoveries
// notify on their own. Wait returns once the goroutines Start launched have exited.
func (c *Cache) Start(ctx context.Context) {
	all := c.all()
	for _, t := range all {
		c.run(false, func() { t.inf.RunWithContext(ctx) })
	}
	for gvr, t := range c.dyn {
		if c.listOnly[gvr] {
			c.run(false, func() { t.inf.RunWithContext(ctx) })
		}
	}
	octx, cancel := context.WithCancel(ctx)
	c.stopOptional = func() { cancel() }
	for _, t := range c.proxyPods {
		c.run(true, func() { t.inf.RunWithContext(octx) })
	}
	if c.crds != nil {
		c.run(true, func() { c.crds.inf.RunWithContext(octx) })
	}
	if c.events != nil {
		c.run(true, func() { c.runEvents(octx) })
	}
	for _, t := range []*tracked{c.services, c.slices} {
		if t != nil {
			c.run(true, func() { t.inf.RunWithContext(octx) })
		}
	}
	c.run(false, func() {
		settled := func(context.Context) (bool, error) {
			for _, t := range all {
				if !t.synced() && access(t).Err == "" {
					return false, nil
				}
			}
			return true, nil
		}
		if wait.PollUntilContextCancel(ctx, syncPoll, true, settled) == nil {
			c.changes.Notify() // a nil Signal ignores it
			c.settled.Store(true)
		}
	})
}

// Wait returns once every goroutine Start launched, watch loops included, has
// exited after ctx is cancelled. Call it after Start; before Start it returns at once.
func (c *Cache) Wait() { c.running.Wait() }

// goTracked runs f in a goroutine that wg joins; a nil wg joins nothing.
func goTracked(wg *sync.WaitGroup, f func()) {
	if wg == nil {
		go f()
		return
	}
	wg.Go(f)
}

// run starts f in a goroutine that Wait joins; an optional one is also
// tracked by c.optional.
func (c *Cache) run(optional bool, f func()) {
	c.running.Add(1)
	if optional {
		c.optional.Add(1)
	}
	go func() {
		defer c.running.Done()
		if optional {
			defer c.optional.Done()
		}
		f()
	}()
}

// runEvents starts the Events informer once every Pod informer has handed
// its Pods to the UID set or failed, so listed Events find their Pods.
func (c *Cache) runEvents(ctx context.Context) {
	pods := append(slices.Clone(c.pods), c.proxyPods...)
	ready := func(context.Context) (bool, error) {
		for i, t := range pods {
			synced := t.inf.HasSynced()
			if reg := c.uidRegs[i]; reg != nil {
				synced = reg.HasSynced()
			}
			if !synced && access(t).Err == "" {
				return false, nil
			}
		}
		return true, nil
	}
	if wait.PollUntilContextCancel(ctx, syncPoll, true, ready) == nil {
		c.events.inf.RunWithContext(ctx)
	}
}

// Settled reports whether every informer has settled since Start and, with
// Config.Changes, the settle notification was sent. Later notifications come
// from events, failures and recoveries only.
func (c *Cache) Settled() bool { return c.settled.Load() }

// synced reports whether t's handler has received the initial list, or,
// without a handler, whether the informer has synced.
func (t tracked) synced() bool {
	if t.reg == nil {
		return t.inf.HasSynced()
	}
	return t.reg.HasSynced()
}

// access reports whether t is current. HasSynced records only the initial
// sync and stays true after later failures, so progress decides.
func access(t tracked) Access {
	return t.progress.access(t.inf.HasSynced())
}

// accessAll merges several informers of one kind: the first that is not OK wins.
func accessAll(ts []tracked) Access {
	for _, t := range ts {
		if a := access(t); !a.OK {
			return a
		}
	}
	return Access{OK: true}
}

// walk reads every store once and calls visit per object: each served kind,
// then Namespaces, then Pods and Deployments deduplicated by UID across their
// informers. It returns every kind's access and whether every informer has
// synced or failed. Snapshot and Fingerprint share it so they always agree.
func (c *Cache) walk(visit func(gvr schema.GroupVersionResource, o any)) (map[schema.GroupVersionResource]Access, bool) {
	acc := make(map[schema.GroupVersionResource]Access, len(c.dyn)+6)
	synced := true
	settled := func(a Access) bool { return a.OK || a.Err != "" }
	for gvr, t := range c.dyn {
		a := access(t)
		acc[gvr] = a
		synced = synced && (c.listOnly[gvr] || settled(a))
		for _, o := range t.inf.GetStore().List() {
			visit(gvr, o)
		}
	}
	acc[NamespacesGVR] = access(c.ns)
	acc[PodsGVR] = accessAll(c.pods)
	acc[DeploymentsGVR] = accessAll(c.deploys)
	synced = synced && settled(acc[NamespacesGVR])
	for _, o := range c.ns.inf.GetStore().List() {
		visit(NamespacesGVR, o)
	}
	seen := map[types.UID]bool{}
	for _, group := range []struct {
		gvr schema.GroupVersionResource
		ts  []tracked
	}{{PodsGVR, c.pods}, {DeploymentsGVR, c.deploys}} {
		for _, t := range group.ts {
			synced = synced && settled(access(t))
			for _, o := range t.inf.GetStore().List() {
				if uid := o.(metav1.Object).GetUID(); !seen[uid] {
					seen[uid] = true
					visit(group.gvr, o)
				}
			}
		}
	}
	c.walkOptional(acc, visit)
	return acc, synced
}

// walkOptional adds each running optional watch's access and objects.
func (c *Cache) walkOptional(acc map[schema.GroupVersionResource]Access, visit func(gvr schema.GroupVersionResource, o any)) {
	if len(c.proxyPods) > 0 {
		acc[ProxyPodsGVR] = accessAll(c.proxyPods)
		seen := map[types.UID]bool{}
		for _, t := range c.proxyPods {
			for _, o := range t.inf.GetStore().List() {
				if uid := o.(metav1.Object).GetUID(); !seen[uid] {
					seen[uid] = true
					visit(ProxyPodsGVR, o)
				}
			}
		}
	}
	for gvr, t := range map[schema.GroupVersionResource]*tracked{EventsGVR: c.events, CRDsGVR: c.crds, ServicesGVR: c.services, EndpointSlicesGVR: c.slices} {
		if t == nil {
			continue
		}
		acc[gvr] = access(*t)
		for _, o := range t.inf.GetStore().List() {
			// A vanished Pod's Events stay stored until they change; snapshots leave them out.
			if ev, ok := o.(*corev1.Event); ok && !c.uids.has(ev.InvolvedObject.UID) {
				continue
			}
			visit(gvr, o)
		}
	}
}

// Snapshot copies the current cache contents and fingerprints, in the same
// pass, exactly what it copied.
func (c *Cache) Snapshot() *Snapshot {
	s := &Snapshot{Objects: map[schema.GroupVersionResource][]*unstructured.Unstructured{}, TakenAt: c.now()}
	var svcs []*serviceEntry
	var eps []*sliceEntry
	var p printer
	s.Access, s.Synced = c.walk(func(gvr schema.GroupVersionResource, o any) {
		p.add(gvr, o)
		if gvr == ProxyPodsGVR {
			s.ProxyPods = append(s.ProxyPods, o.(*corev1.Pod))
			return
		}
		switch v := o.(type) {
		case *unstructured.Unstructured:
			s.Objects[gvr] = append(s.Objects[gvr], v)
		case *corev1.Namespace:
			s.Namespaces = append(s.Namespaces, v)
		case *corev1.Pod:
			s.Pods = append(s.Pods, v)
		case *appsv1.Deployment:
			s.Deployments = append(s.Deployments, v)
		case *corev1.Event:
			s.Events = append(s.Events, eventFact(v))
		case *metav1.PartialObjectMetadata:
			s.CRDs = append(s.CRDs, crdMeta(v))
		case *serviceEntry:
			svcs = append(svcs, v)
		case *sliceEntry:
			eps = append(eps, v)
		}
	})
	for _, objs := range s.Objects {
		sortByKey(objs)
	}
	slices.SortFunc(s.Events, func(a, b EventFact) int { return strings.Compare(string(a.UID), string(b.UID)) })
	slices.SortFunc(s.CRDs, func(a, b CRDMeta) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(svcs, func(a, b *serviceEntry) int {
		return compareKey(a.fact.Namespace, a.fact.Name, b.fact.Namespace, b.fact.Name)
	})
	slices.SortFunc(eps, func(a, b *sliceEntry) int { return compareKey(a.fact.Namespace, a.name, b.fact.Namespace, b.name) })
	// The facts' ports and ready counts are shared with the stores, which replace an entry rather than change it.
	if len(svcs) > 0 {
		s.Services = make([]ServiceFact, len(svcs))
		for i, v := range svcs {
			s.Services[i] = v.fact
		}
	}
	if len(eps) > 0 {
		s.EndpointSlices = make([]SliceFact, len(eps))
		for i, v := range eps {
			s.EndpointSlices[i] = v.fact
		}
	}
	if c.evf != nil {
		s.EventDeltas, s.EventHistoryUnknown, s.EventsCapped = c.evf.view(s.TakenAt)
	}
	s.Source = p.finish(s.Access)
	return s
}

func key(u *unstructured.Unstructured) string { return u.GetNamespace() + "/" + u.GetName() }

// compareKey orders as strings.Compare(ans+"/"+an, bns+"/"+bn) without building
// the keys; a namespace never contains "/".
func compareKey(ans, an, bns, bn string) int {
	if ans == bns {
		return strings.Compare(an, bn)
	}
	for i := range min(len(ans), len(bns)) {
		if ans[i] != bns[i] {
			return cmp.Compare(ans[i], bns[i])
		}
	}
	if len(ans) < len(bns) {
		return cmp.Compare('/', bns[len(ans)])
	}
	return cmp.Compare(ans[len(bns)], '/')
}

// sortByKey orders objs by key, computing each key once: reading it from the
// object's maps on every comparison dominated a snapshot.
func sortByKey(objs []*unstructured.Unstructured) {
	type keyed struct {
		k string
		u *unstructured.Unstructured
	}
	ks := make([]keyed, len(objs))
	for i, u := range objs {
		ks[i] = keyed{key(u), u}
	}
	slices.SortFunc(ks, func(a, b keyed) int { return strings.Compare(a.k, b.k) })
	for i, k := range ks {
		objs[i] = k.u
	}
}
