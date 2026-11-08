// Package scopeverify checks that the UI's view of an install's scope matches
// what the controller itself reports (D-009 §3).
package scopeverify

import (
	"slices"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// Input is what a Verifier needs for one install.
type Input struct {
	Install  install.Resolved
	Snapshot *cache.Snapshot
	Served   []kinds.Served
	// Collected are the collector values this computation read; a verifier
	// reads its controller's data from them, such as ctrlmetrics.Key.
	Collected collect.Snapshot
	Now       time.Time
	// Views decodes routes to count their parentRefs; nil decodes directly.
	Views objects.Decoder
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
	// Matures, if set, is the earliest instant after which a pending mismatch
	// will be reported (a strict "after" deadline). The engine recomputes then (D-019).
	Matures time.Time
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
	cv, _ := in.Collected.Get(ctrlmetrics.Key)
	met, maxAge, has := ctrlmetrics.Lookup(cv, id)
	switch {
	case !has || met.Err != "":
		return unavailable("controller metrics unavailable")
	case met.Stale(in.Now, maxAge):
		return unavailable("controller metrics are stale")
	case in.Install.Scope.Mode == install.ScopeUnknown:
		return unavailable("controller scope unknown")
	case slices.ContainsFunc(met.Metrics.PerPod, func(p ctrlmetrics.PodMetrics) bool { return p.Dropped > 0 }):
		// Once updates were dropped, resources_managed is invalid until a restart (design §8.1).
		return unavailable("scope verification unavailable: the controller dropped metric updates")
	}
	ui, skip := uiCounts(in)
	// An absent family means 0 (D-012); it is unavailable only when the UI expects more.
	if !met.Metrics.ManagedPresent {
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
	exp := experimentalLabels(in)
	for k := range met.Metrics.Managed {
		if counted(k.Resource, in.Served) && !skip[k] && !exp[k.Resource] {
			keys[k] = true
		}
	}
	prev := v.first[id]
	next := map[ctrlmetrics.ManagedKey]time.Time{}
	res := Result{Available: true}
	for k := range keys {
		u, c := ui[k], met.Metrics.Managed[k]
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
		} else if at := since.Add(v.persist); res.Matures.IsZero() || at.Before(res.Matures) {
			res.Matures = at
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
	case k.Category == kinds.CategoryPolicy && k.Group == kinds.KgatewayGroup && !k.ListOnly:
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

// experimentalLabels returns the gauge labels whose served kinds are all
// experimental-only for this install. Their series are left out in every
// namespace (D-018 §4); a label shared with a stable kind is skipped per
// namespace in uiCounts instead.
func experimentalLabels(in Input) map[string]bool {
	ctrl := in.Install.KgatewayVersion()
	stable, out := map[string]bool{}, map[string]bool{}
	for _, k := range in.Served {
		if label := resourceLabel(k); label != "" {
			if k.ExperimentalOnly(ctrl) {
				out[label] = true
			} else {
				stable[label] = true
			}
		}
	}
	for label := range stable {
		delete(out, label)
	}
	return out
}

// uiCounts counts the in-scope objects per (namespace, resource label) with the
// gauge's weights. It leaves out, and returns as skip, every key holding an
// experimental-only object: the gauge counts those only with experimental
// features on, which the UI does not read (D-017 §4). Once the upstream scope
// endpoint reports enabled kinds, the verifier uses it instead.
func uiCounts(in Input) (out map[ctrlmetrics.ManagedKey]float64, skip map[ctrlmetrics.ManagedKey]bool) {
	nsByName := map[string]*corev1.Namespace{}
	for _, n := range in.Snapshot.Namespaces {
		nsByName[n.Name] = n
	}
	dec := in.Views
	if dec == nil {
		dec = objects.Direct{}
	}
	out, skip = map[ctrlmetrics.ManagedKey]float64{}, map[ctrlmetrics.ManagedKey]bool{}
	for _, k := range in.Served {
		label := resourceLabel(k)
		if label == "" {
			continue
		}
		exp := k.ExperimentalOnly(in.Install.KgatewayVersion())
		for _, o := range in.Snapshot.Objects[k.GVR()] {
			if !in.Install.Scope.Contains(o.GetNamespace(), nsByName[o.GetNamespace()]) {
				continue
			}
			key := ctrlmetrics.ManagedKey{Namespace: o.GetNamespace(), Resource: label}
			if exp {
				skip[key] = true // ListenerSet shares this label, so it is skipped here too
				continue
			}
			w := 1.0
			if k.Category == kinds.CategoryRoute {
				w = float64(len(routeParentRefs(dec, k, o)))
			}
			if w > 0 {
				out[key] += w
			}
		}
	}
	for key := range skip {
		delete(out, key)
	}
	return out, skip
}

// routeParentRefs returns o's resolved spec.parentRefs through dec.
func routeParentRefs(dec objects.Decoder, k kinds.Served, o *unstructured.Unstructured) []objects.ParentRef {
	if k.Group == objects.GatewayAPIGroup && k.Kind == "HTTPRoute" {
		return dec.HTTPRoute(o).ParentRefs
	}
	return dec.Route(o).ParentRefs
}
