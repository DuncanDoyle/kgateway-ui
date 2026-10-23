// Package cache keeps a shared, read-only informer cache of the kinds the UI
// watches and exposes it as immutable snapshots.
package cache

import (
	"context"
	"sort"
	"sync"
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
	toolscache "k8s.io/client-go/tools/cache"

	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
)

// Access reports whether a kind can currently be read.
type Access struct {
	OK  bool   `json:"ok"`
	Err string `json:"error,omitempty"`
}

// ListTimeout bounds every List call an informer makes, so a hung list ends
// as a recorded failure instead of keeping old evidence alive.
const ListTimeout = time.Minute

// GVRs under which the typed informers report their Access.
var (
	NamespacesGVR  = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	PodsGVR        = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	DeploymentsGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
)

// Snapshot is an immutable view of the cache at TakenAt. Callers must not mutate it.
type Snapshot struct {
	Objects map[schema.GroupVersionResource][]*unstructured.Unstructured
	// Access holds every served kind plus NamespacesGVR, PodsGVR and DeploymentsGVR.
	Access      map[schema.GroupVersionResource]Access
	Namespaces  []*corev1.Namespace
	Pods        []*corev1.Pod
	Deployments []*appsv1.Deployment
	// Synced is true once every informer has synced or failed with an error.
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
	err      string // failure not yet recovered from; "" when none
	failures uint64 // failures so far; a watch-list stream remembers the count it was opened at
	onChange func() // called when err turns empty or non-empty; may be nil
}

func (p *progress) failure(err error) {
	p.mu.Lock()
	changed := p.err == ""
	p.err = err.Error()
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
		p.err = ""
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
		return Access{Err: p.err}
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
			return watch.Filter(w, func(e watch.Event) (watch.Event, bool) {
				p.event(e, watchList, openedAt)
				return e, true
			}), nil
		},
	}
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
	ns      tracked
	pods    []tracked
	deploys []tracked
}

// informer builds an informer over an instrumented ListerWatcher. client tells
// the reflector whether the client supports watch-list semantics (fakes do not).
func (c *Cache) informer(client any, example runtime.Object, list toolscache.ListWithContextFunc, watchFn toolscache.WatchFuncWithContext) tracked {
	p := &progress{onChange: c.changes.Notify}
	lw := toolscache.ToListWatcherWithWatchListSemantics(instrument(p, list, watchFn), client)
	t := tracked{inf: toolscache.NewSharedIndexInformer(lw, example, 0, toolscache.Indexers{}), progress: p}
	if c.changes != nil {
		notify := func(any) { c.changes.Notify() }
		// Fails only on a stopped informer. Without a handler the kind still
		// settles on its own sync, and the confirm tick catches its changes.
		t.reg, _ = t.inf.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
			AddFunc: notify, UpdateFunc: func(_, _ any) { c.changes.Notify() }, DeleteFunc: notify})
	}
	return t
}

// New creates the informers. Nothing runs until Start.
func New(cfg Config) *Cache {
	c := &Cache{now: cfg.Now, changes: cfg.Changes, dyn: map[schema.GroupVersionResource]tracked{}}
	if c.now == nil {
		c.now = time.Now
	}
	for _, s := range cfg.Served {
		ri := cfg.Dynamic.Resource(s.GVR())
		c.dyn[s.GVR()] = c.informer(cfg.Dynamic, &unstructured.Unstructured{},
			func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) { return ri.List(ctx, o) },
			func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) { return ri.Watch(ctx, o) })
	}
	nss := cfg.Kube.CoreV1().Namespaces()
	c.ns = c.informer(cfg.Kube, &corev1.Namespace{},
		func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) { return nss.List(ctx, o) },
		func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) { return nss.Watch(ctx, o) })
	pods, deploys := cfg.Kube.CoreV1().Pods(""), cfg.Kube.AppsV1().Deployments("")
	for _, sel := range cfg.PodSelectors {
		c.pods = append(c.pods, c.informer(cfg.Kube, &corev1.Pod{},
			func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) {
				o.LabelSelector = sel.String()
				return pods.List(ctx, o)
			},
			func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) {
				o.LabelSelector = sel.String()
				return pods.Watch(ctx, o)
			}))
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
	return c
}

func (c *Cache) all() []tracked {
	out := []tracked{c.ns}
	for _, t := range c.dyn {
		out = append(out, t)
	}
	return append(append(out, c.pods...), c.deploys...)
}

// syncPoll is how often Start checks whether every informer has settled.
const syncPoll = 50 * time.Millisecond

// Start runs all informers until ctx is cancelled. With Config.Changes it
// notifies once when every informer has settled: its handler has received the
// initial list, or its List or Watch failed. Later failures and recoveries
// notify on their own.
func (c *Cache) Start(ctx context.Context) {
	all := c.all()
	for _, t := range all {
		go t.inf.RunWithContext(ctx)
	}
	if c.changes == nil {
		return
	}
	go func() {
		settled := func(context.Context) (bool, error) {
			for _, t := range all {
				if !t.synced() && access(t).Err == "" {
					return false, nil
				}
			}
			return true, nil
		}
		if wait.PollUntilContextCancel(ctx, syncPoll, true, settled) == nil {
			c.changes.Notify()
		}
	}()
}

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
	acc := make(map[schema.GroupVersionResource]Access, len(c.dyn)+3)
	synced := true
	settled := func(a Access) bool { return a.OK || a.Err != "" }
	for gvr, t := range c.dyn {
		a := access(t)
		acc[gvr] = a
		synced = synced && settled(a)
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
	return acc, synced
}

// Snapshot copies the current cache contents and fingerprints, in the same
// pass, exactly what it copied.
func (c *Cache) Snapshot() *Snapshot {
	s := &Snapshot{Objects: map[schema.GroupVersionResource][]*unstructured.Unstructured{}, TakenAt: c.now()}
	var p printer
	s.Access, s.Synced = c.walk(func(gvr schema.GroupVersionResource, o any) {
		p.add(gvr, o.(metav1.Object))
		switch v := o.(type) {
		case *unstructured.Unstructured:
			s.Objects[gvr] = append(s.Objects[gvr], v)
		case *corev1.Namespace:
			s.Namespaces = append(s.Namespaces, v)
		case *corev1.Pod:
			s.Pods = append(s.Pods, v)
		case *appsv1.Deployment:
			s.Deployments = append(s.Deployments, v)
		}
	})
	for _, objs := range s.Objects {
		sort.Slice(objs, func(i, j int) bool { return key(objs[i]) < key(objs[j]) })
	}
	s.Source = p.finish(s.Access)
	return s
}

func key(u *unstructured.Unstructured) string { return u.GetNamespace() + "/" + u.GetName() }
