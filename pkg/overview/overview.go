// Package overview builds the install page's header, count tiles and gateway list.
package overview

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// ReadyCount is ready versus desired replicas.
type ReadyCount struct {
	Ready   int32 `json:"ready"`
	Desired int32 `json:"desired"`
}

// ScopeSummary describes an install's namespace scope.
type ScopeSummary struct {
	Mode      string                 `json:"mode"`
	Selectors []metav1.LabelSelector `json:"selectors,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Watched   int                    `json:"watched"`
	Total     int                    `json:"total"`
}

// Header is the install page header.
type Header struct {
	ID              string       `json:"id"`
	Namespace       string       `json:"namespace"`
	Name            string       `json:"name"`
	Version         string       `json:"version"`
	Edition         string       `json:"edition"`
	ControllerNames []string     `json:"controllerNames"`
	Controller      ReadyCount   `json:"controller"`
	Scope           ScopeSummary `json:"scope"`
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
	Ref            objects.Ref  `json:"ref"`
	Class          string       `json:"class"`
	State          health.State `json:"state"`
	Listeners      int          `json:"listeners"`
	AttachedRoutes int32        `json:"attachedRoutes"`
	Addresses      []string     `json:"addresses"`
}

// Overview is the install page model.
type Overview struct {
	Header   Header       `json:"header"`
	Counts   []Count      `json:"counts"`
	Gateways []GatewayRow `json:"gateways"`
}

// Build assembles the overview of one install from the objects it owns. It is
// pure, so the API can rebuild it over the owned objects a caller may see.
func Build(res install.Resolved, owned *install.Owned, snap *cache.Snapshot, served []kinds.Served, rep health.Report) Overview {
	o := Overview{Header: header(res, snap), Counts: []Count{}, Gateways: []GatewayRow{}}
	byKind := map[schema.GroupKind]int{}
	add := func(u *unstructured.Unstructured) {
		r := objects.RefOf(u)
		byKind[schema.GroupKind{Group: r.Group, Kind: r.Kind}]++
	}
	if owned != nil {
		for _, us := range [][]*unstructured.Unstructured{owned.GatewayClasses, owned.Gateways, owned.ListenerSets} {
			for _, u := range us {
				add(u)
			}
		}
		for _, os := range [][]install.OwnedObject{owned.Routes, owned.Policies} {
			for _, x := range os {
				add(x.Obj)
			}
		}
	}
	// One tile per served kind, GatewayClasses included.
	for _, k := range served {
		o.Counts = append(o.Counts, Count{Group: k.Group, Kind: k.Kind, Category: string(k.Category), DisplayName: k.DisplayName,
			Count: byKind[schema.GroupKind{Group: k.Group, Kind: k.Kind}], Access: snap.Access[k.GVR()]})
	}
	if owned == nil {
		return o
	}
	for _, g := range owned.Gateways {
		row := GatewayRow{Ref: objects.RefOf(g), Class: objects.GatewayClassName(g), Addresses: objects.GatewayAddresses(g), State: rowState(g, rep)}
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

func header(res install.Resolved, snap *cache.Snapshot) Header {
	h := Header{ID: res.ID(), Namespace: res.Namespace, Name: res.Name, Version: res.Version, Edition: res.Edition, ControllerNames: res.ControllerNames,
		Scope: ScopeSummary{Mode: string(res.Scope.Mode), Selectors: res.Scope.Selectors, Error: res.Scope.Err, Total: len(snap.Namespaces)}}
	if h.ControllerNames == nil {
		h.ControllerNames = []string{}
	}
	if d := res.Deployment; d != nil {
		h.Controller = ReadyCount{Ready: d.Status.ReadyReplicas, Desired: 1}
		if d.Spec.Replicas != nil {
			h.Controller.Desired = *d.Spec.Replicas
		}
	}
	for _, n := range snap.Namespaces {
		if res.Scope.Contains(n.Name, n) {
			h.Scope.Watched++
		}
	}
	return h
}

// rowState is the worst finding on the gateway; if a required signal is
// missing and there is no error, the row is unknown, never green (D-006).
func rowState(g *unstructured.Unstructured, rep health.Report) health.State {
	ref := objects.RefOf(g)
	worst := health.StateHealthy
	for _, f := range rep.Findings {
		if f.Resource == nil || *f.Resource != ref {
			continue
		}
		switch f.Severity {
		case health.SeverityError:
			return health.StateError
		case health.SeverityWarning:
			worst = health.StateWarning
		}
	}
	for _, c := range rep.Checks {
		if c.Required && !c.OK {
			return health.StateUnknown
		}
	}
	return worst
}
