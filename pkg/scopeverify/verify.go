// Package scopeverify checks that the UI's view of an install's scope matches
// what the controller itself reports (D-009 §3).
package scopeverify

import (
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Input is what a Verifier needs for one install.
type Input struct {
	Install    install.Resolved
	Snapshot   *cache.Snapshot
	Served     []kinds.Served
	Metrics    ctrlmetrics.Result
	HasMetrics bool
	// MetricsMaxAge is the age beyond which Metrics is stale; zero means no limit.
	MetricsMaxAge time.Duration
	Now           time.Time
}

// Mismatch is a (namespace, resource) whose counts have disagreed since Since.
type Mismatch struct {
	Namespace  string    `json:"namespace"`
	Resource   string    `json:"resource"`
	UI         float64   `json:"ui"`
	Controller float64   `json:"controller"`
	Since      time.Time `json:"since"`
}

// Result is a verification outcome. Available=false means the verifier could
// not run; it never decides health on its own (design §4.4).
type Result struct {
	Available  bool
	Reason     string
	Mismatches []Mismatch
}

// Verifier compares the UI's scope with the controller's. Downstreams may register their own.
type Verifier interface {
	Name() string
	Verify(in Input) Result
}

// ManagedCountVerifier compares per-(namespace, resource) counts with
// kgateway_resources_managed, weighting objects exactly as the gauge does.
type ManagedCountVerifier struct {
	persist time.Duration
	mu      sync.Mutex
	first   map[string]map[ctrlmetrics.ManagedKey]time.Time // install ID → key → first seen mismatching
}

// NewManagedCountVerifier reports mismatches that have lasted longer than
// persist; callers pass 2 × the metrics interval (design §4.4).
func NewManagedCountVerifier(persist time.Duration) *ManagedCountVerifier {
	return &ManagedCountVerifier{persist: persist, first: map[string]map[ctrlmetrics.ManagedKey]time.Time{}}
}

// Persist returns how long a mismatch must last before it is reported.
func (v *ManagedCountVerifier) Persist() time.Duration { return v.persist }

// Name implements Verifier.
func (*ManagedCountVerifier) Name() string { return "resources-managed" }

// Verify implements Verifier. Persistence needs continuous observation, so
// any unavailable result restarts it for the install.
func (v *ManagedCountVerifier) Verify(in Input) Result {
	v.mu.Lock()
	defer v.mu.Unlock()
	id := in.Install.ID()
	unavailable := func(why string) Result {
		delete(v.first, id)
		return Result{Reason: why}
	}
	switch {
	case !in.HasMetrics || in.Metrics.Err != "":
		return unavailable("controller metrics unavailable")
	case in.Metrics.Stale(in.Now, in.MetricsMaxAge):
		return unavailable("controller metrics are stale")
	case in.Install.Scope.Mode == install.ScopeUnknown:
		return unavailable("controller scope unknown")
	}
	ui := uiCounts(in)
	// An absent family means 0 (D-012); it is unavailable only when the UI expects more.
	if !in.Metrics.Metrics.ManagedPresent {
		if len(ui) > 0 {
			return unavailable(ctrlmetrics.ResourcesManaged + " not found in controller metrics")
		}
		delete(v.first, id)
		return Result{Available: true}
	}
	keys := map[ctrlmetrics.ManagedKey]bool{}
	for k := range ui {
		keys[k] = true
	}
	for k := range in.Metrics.Metrics.Managed {
		if counted(k.Resource, in.Served) {
			keys[k] = true
		}
	}
	prev := v.first[id]
	next := map[ctrlmetrics.ManagedKey]time.Time{}
	res := Result{Available: true}
	for k := range keys {
		u, c := ui[k], in.Metrics.Metrics.Managed[k]
		if u == c {
			continue
		}
		since, ok := prev[k]
		if !ok {
			since = in.Now
		}
		next[k] = since
		if in.Now.Sub(since) > v.persist {
			res.Mismatches = append(res.Mismatches, Mismatch{Namespace: k.Namespace, Resource: k.Resource, UI: u, Controller: c, Since: since})
		}
	}
	v.first[id] = next
	sort.Slice(res.Mismatches, func(i, j int) bool {
		a, b := res.Mismatches[i], res.Mismatches[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Resource < b.Resource
	})
	return res
}

// resourceLabel maps a served kind to the gauge's "resource" label; "" means
// the gauge does not count this kind.
func resourceLabel(k kinds.Served) string {
	switch {
	case k.Category == kinds.CategoryGateway, k.Category == kinds.CategoryRoute:
		return k.Kind
	case k.Category == kinds.CategoryListenerSet:
		return "XListenerSet" // both ListenerSet flavours are reported under this value
	case k.Category == kinds.CategoryPolicy && k.Group == kinds.KgatewayGroup:
		return k.Kind
	}
	return ""
}

// counted reports whether the gauge's resource label belongs to a served kind the UI counts.
func counted(resource string, served []kinds.Served) bool {
	for _, k := range served {
		if resourceLabel(k) == resource {
			return true
		}
	}
	return false
}

// uiCounts counts the in-scope objects per (namespace, resource label) with the gauge's weights.
func uiCounts(in Input) map[ctrlmetrics.ManagedKey]float64 {
	nsByName := map[string]*corev1.Namespace{}
	for _, n := range in.Snapshot.Namespaces {
		nsByName[n.Name] = n
	}
	out := map[ctrlmetrics.ManagedKey]float64{}
	for _, k := range in.Served {
		label := resourceLabel(k)
		if label == "" {
			continue
		}
		for _, o := range in.Snapshot.Objects[k.GVR()] {
			if !in.Install.Scope.Contains(o.GetNamespace(), nsByName[o.GetNamespace()]) {
				continue
			}
			w := 1.0
			if k.Category == kinds.CategoryRoute {
				w = float64(len(objects.RouteParentRefs(o)))
			}
			if w > 0 {
				out[ctrlmetrics.ManagedKey{Namespace: o.GetNamespace(), Resource: label}] += w
			}
		}
	}
	return out
}
