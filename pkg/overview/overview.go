// Package overview builds the install page's header, count tiles and gateway list.
package overview

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/platform"
)

// ReadinessState is the controller readiness a provider reports (D-016).
type ReadinessState string

// Readiness states. Not observed means the platform cannot see readiness,
// which is not a failure.
const (
	ReadinessReady       ReadinessState = "ready"
	ReadinessNotReady    ReadinessState = "not-ready"
	ReadinessUnknown     ReadinessState = "unknown"
	ReadinessNotObserved ReadinessState = "not-observed"
)

// Readiness is the controller's readiness; replica counts are present only
// where the platform runs workloads.
type Readiness struct {
	State   ReadinessState `json:"state"`
	Ready   *int32         `json:"ready,omitempty"`
	Desired *int32         `json:"desired,omitempty"`
}

// ScopeSummary describes an install's namespace scope.
type ScopeSummary struct {
	Mode      string                 `json:"mode"`
	Selectors []metav1.LabelSelector `json:"selectors,omitempty"`
	Error     string                 `json:"error,omitempty"`
	// Source is where the scope was read from, for example "deployment template".
	Source  string `json:"source,omitempty"`
	Watched int    `json:"watched"`
	Total   int    `json:"total"`
}

// Header is the install page header. Namespace, Name and Scope are absent
// where the platform has no such notion (D-016).
type Header struct {
	ID              string        `json:"id"`
	DisplayName     string        `json:"displayName"`
	Namespace       string        `json:"namespace,omitempty"`
	Name            string        `json:"name,omitempty"`
	Version         string        `json:"version"`
	Edition         string        `json:"edition"`
	ControllerNames []string      `json:"controllerNames"`
	Readiness       Readiness     `json:"readiness"`
	Scope           *ScopeSummary `json:"scope,omitempty"`
}

// Count is one count tile.
type Count struct {
	Group       string       `json:"group"`
	Kind        string       `json:"kind"`
	Category    string       `json:"category"`
	DisplayName string       `json:"displayName"`
	Count       int          `json:"count"`
	Access      cache.Access `json:"access"`
}

// GatewayRow is one row of the gateway table.
type GatewayRow struct {
	Ref   objects.Ref  `json:"ref"`
	Class string       `json:"class"`
	State health.State `json:"state"`
	// Reason says why State is not healthy (D-063).
	Reason         *health.RowReason `json:"reason,omitempty"`
	Listeners      int               `json:"listeners"`
	AttachedRoutes int32             `json:"attachedRoutes"`
	Addresses      []string          `json:"addresses"`
}

// Overview is the install page model.
type Overview struct {
	Header   Header       `json:"header"`
	Counts   []Count      `json:"counts"`
	Gateways []GatewayRow `json:"gateways"`
}

// Build assembles the overview of one install from the objects it owns. It is
// pure, so the API can rebuild it over the owned objects a caller may see.
func Build(res install.Resolved, owned *install.Owned, snap *cache.Snapshot, served []kinds.Served, rep health.Report, plat platform.Info) Overview {
	if plat.IsZero() {
		plat = platform.Kubernetes() // same default as health
	}
	o := Overview{Header: header(res, snap, plat), Counts: []Count{}, Gateways: []GatewayRow{}}
	byKind := map[schema.GroupKind]int{}
	add := func(u *unstructured.Unstructured) {
		r := objects.RefOf(u)
		byKind[schema.GroupKind{Group: r.Group, Kind: r.Kind}]++
	}
	if owned != nil {
		for _, us := range [][]*unstructured.Unstructured{owned.GatewayClasses, owned.Gateways, owned.ListenerSets, owned.Backends} {
			for _, u := range us {
				add(u)
			}
		}
		for _, os := range [][]install.OwnedObject{owned.Routes, owned.Policies, owned.Listed} {
			for _, x := range os {
				add(x.Obj)
			}
		}
	}
	// One tile per served kind, GatewayClasses included.
	for _, k := range served {
		if k.Category == kinds.CategoryParameters {
			continue // read to resolve proxies, not part of the inventory
		}
		o.Counts = append(o.Counts, Count{Group: k.Group, Kind: k.Kind, Category: string(k.Category), DisplayName: k.DisplayName,
			Count: byKind[schema.GroupKind{Group: k.Group, Kind: k.Kind}], Access: snap.Access[k.GVR()]})
	}
	if owned == nil {
		return o
	}
	worst, unknown := severities(owned.Gateways, rep)
	for _, g := range owned.Gateways {
		ref := objects.RefOf(g)
		row := GatewayRow{Ref: ref, Class: objects.GatewayClassName(g), Addresses: objects.GatewayAddresses(g), State: worst[ref].State(unknown)}
		row.Reason = worst[ref].Reason(row.State)
		if row.Addresses == nil {
			row.Addresses = []string{}
		}
		for _, l := range objects.GatewayListeners(g) {
			row.Listeners++
			row.AttachedRoutes += l.AttachedRoutes
		}
		o.Gateways = append(o.Gateways, row)
	}
	return o
}

func header(res install.Resolved, snap *cache.Snapshot, plat platform.Info) Header {
	h := Header{ID: res.ID(), DisplayName: res.Display(), Namespace: res.Namespace, Name: res.Name, Version: res.ControllerVersion(), Edition: res.Edition,
		ControllerNames: res.ControllerNames, Readiness: Readiness{State: ReadinessNotObserved}}
	if h.ControllerNames == nil {
		h.ControllerNames = []string{}
	}
	// Same rule as KGWUI-005: fewer ready replicas than desired, or 0 desired, is not ready.
	if d := res.Deployment; d != nil && plat.Has(platform.Workloads) {
		ready, desired := d.Status.ReadyReplicas, health.Desired(d)
		h.Readiness = Readiness{State: ReadinessReady, Ready: &ready, Desired: &desired}
		if desired == 0 || ready < desired {
			h.Readiness.State = ReadinessNotReady
		}
	}
	if plat.Has(platform.Scope) {
		sc := ScopeSummary{Mode: string(res.Scope.Mode), Selectors: res.Scope.Selectors, Error: res.Scope.Err, Source: res.Scope.Source, Total: len(snap.Namespaces)}
		for _, n := range snap.Namespaces {
			if res.Scope.Contains(n.Name, n) {
				sc.Watched++
			}
		}
		h.Scope = &sc
	}
	return h
}

// severities maps each of gws to its findings; unknown is whether a
// required signal is missing.
func severities(gws []*unstructured.Unstructured, rep health.Report) (map[objects.Ref]explore.Worst, bool) {
	worst := make(map[objects.Ref]explore.Worst, len(gws))
	for _, g := range gws {
		worst[objects.RefOf(g)] = explore.Worst{}
	}
	for i := range rep.Findings {
		f := &rep.Findings[i]
		if f.Resource == nil {
			continue
		}
		if w, ok := worst[*f.Resource]; ok {
			worst[*f.Resource] = w.Add(f)
		}
	}
	unknown := slices.ContainsFunc(rep.Checks, func(c health.CheckResult) bool { return c.Required && !c.OK })
	return worst, unknown
}

// RowState is a state from the worst finding; if a required signal is missing
// and there is no error, it is unknown, never green (D-006).
func RowState(worst health.State, unknown bool) health.State {
	switch {
	case worst == health.StateError:
		return health.StateError
	case unknown:
		return health.StateUnknown
	case worst == "":
		return health.StateHealthy
	}
	return worst
}
