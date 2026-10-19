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
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	toolscache "k8s.io/client-go/tools/cache"

	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
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
}

func (p *progress) failure(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err.Error()
	p.failures++
}

// recover clears the failure if none happened since the watch was opened at openedAt.
func (p *progress) recover(openedAt uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failures == openedAt {
		p.err = ""
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
type tracked struct {
	inf      toolscache.SharedIndexInformer
	progress *progress
}

// Cache is a set of informers whose contents are read through Snapshot.
type Cache struct {
	now     func() time.Time
	dyn     map[schema.GroupVersionResource]tracked
	ns      tracked
	pods    []tracked
	deploys []tracked
}

// informer builds an informer over an instrumented ListerWatcher. client tells
// the reflector whether the client supports watch-list semantics (fakes do not).
func (c *Cache) informer(client any, example runtime.Object, list toolscache.ListWithContextFunc, watchFn toolscache.WatchFuncWithContext) tracked {
	p := &progress{}
	lw := toolscache.ToListWatcherWithWatchListSemantics(instrument(p, list, watchFn), client)
	return tracked{inf: toolscache.NewSharedIndexInformer(lw, example, 0, toolscache.Indexers{}), progress: p}
}

// New creates the informers. Nothing runs until Start.
func New(cfg Config) *Cache {
	c := &Cache{now: time.Now, dyn: map[schema.GroupVersionResource]tracked{}}
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

// Start runs all informers until ctx is cancelled.
func (c *Cache) Start(ctx context.Context) {
	for _, t := range c.all() {
		go t.inf.RunWithContext(ctx)
	}
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

// Snapshot copies the current cache contents.
func (c *Cache) Snapshot() *Snapshot {
	s := &Snapshot{Objects: map[schema.GroupVersionResource][]*unstructured.Unstructured{}, Access: map[schema.GroupVersionResource]Access{}, Synced: true, TakenAt: c.now()}
	settled := func(a Access) bool { return a.OK || a.Err != "" }
	for gvr, t := range c.dyn {
		a := access(t)
		s.Access[gvr] = a
		s.Synced = s.Synced && settled(a)
		for _, o := range t.inf.GetStore().List() {
			s.Objects[gvr] = append(s.Objects[gvr], o.(*unstructured.Unstructured))
		}
		sort.Slice(s.Objects[gvr], func(i, j int) bool { return key(s.Objects[gvr][i]) < key(s.Objects[gvr][j]) })
	}
	s.Access[NamespacesGVR] = access(c.ns)
	s.Access[PodsGVR] = accessAll(c.pods)
	s.Access[DeploymentsGVR] = accessAll(c.deploys)
	s.Synced = s.Synced && settled(s.Access[NamespacesGVR])
	for _, o := range c.ns.inf.GetStore().List() {
		s.Namespaces = append(s.Namespaces, o.(*corev1.Namespace))
	}
	seen := map[types.UID]bool{}
	for _, t := range c.pods {
		s.Synced = s.Synced && settled(access(t))
		for _, o := range t.inf.GetStore().List() {
			if p := o.(*corev1.Pod); !seen[p.UID] {
				seen[p.UID] = true
				s.Pods = append(s.Pods, p)
			}
		}
	}
	for _, t := range c.deploys {
		s.Synced = s.Synced && settled(access(t))
		for _, o := range t.inf.GetStore().List() {
			if d := o.(*appsv1.Deployment); !seen[d.UID] {
				seen[d.UID] = true
				s.Deployments = append(s.Deployments, d)
			}
		}
	}
	return s
}

func key(u *unstructured.Unstructured) string { return u.GetNamespace() + "/" + u.GetName() }
