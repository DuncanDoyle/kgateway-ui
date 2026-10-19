// Package kinds describes the Kubernetes resource kinds the UI understands and
// resolves which of them the cluster actually serves.
package kinds

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Category groups kinds by the role they play in the UI.
type Category string

// Categories understood by the UI.
const (
	CategoryGatewayClass Category = "gatewayclass"
	CategoryGateway      Category = "gateway"
	CategoryListenerSet  Category = "listenerset"
	CategoryRoute        Category = "route"
	CategoryPolicy       Category = "policy"
	// CategoryWorkload marks kinds read through typed informers (Deployments).
	// They are never watched dynamically or counted; findings link to them.
	CategoryWorkload Category = "workload"
)

// Descriptor describes a kind the UI can watch. Versions are in preference order.
type Descriptor struct {
	Group       string   `json:"group"`
	Kind        string   `json:"kind"`
	Resource    string   `json:"resource"`
	Versions    []string `json:"versions"`
	Namespaced  bool     `json:"namespaced"`
	Category    Category `json:"category"`
	DisplayName string   `json:"displayName"`
}

// Served is a Descriptor resolved to the version the cluster serves.
type Served struct {
	Descriptor
	Version string `json:"version"`
}

// GVR returns the group/version/resource used to watch this kind.
func (s Served) GVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: s.Group, Version: s.Version, Resource: s.Resource}
}

// DiscoveryClient is the subset of the discovery API that Resolve needs.
type DiscoveryClient interface {
	ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error)
}

// Dedupe returns descs with one descriptor per group and kind: a later
// descriptor replaces an earlier one in the earlier one's position.
func Dedupe(descs []Descriptor) []Descriptor {
	at := map[schema.GroupKind]int{}
	var out []Descriptor
	for _, d := range descs {
		gk := schema.GroupKind{Group: d.Group, Kind: d.Kind}
		if i, ok := at[gk]; ok {
			out[i] = d
			continue
		}
		at[gk] = len(out)
		out = append(out, d)
	}
	return out
}

// Resolve returns, in input order, the descriptors whose resource is served in
// one of their versions, resolved to the first such version. Kinds that are not
// served (CRD not installed, or removed in this kgateway version) are omitted.
// Duplicates are removed as by Dedupe.
func Resolve(dc DiscoveryClient, descs []Descriptor) ([]Served, error) {
	descs = Dedupe(descs)
	cache := map[string]*metav1.APIResourceList{}
	get := func(gv string) (*metav1.APIResourceList, error) {
		if l, ok := cache[gv]; ok {
			return l, nil
		}
		l, err := dc.ServerResourcesForGroupVersion(gv)
		if apierrors.IsNotFound(err) {
			l, err = &metav1.APIResourceList{}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("discover %s: %w", gv, err)
		}
		cache[gv] = l
		return l, nil
	}
	var out []Served
	for _, d := range descs {
		for _, v := range d.Versions {
			l, err := get(schema.GroupVersion{Group: d.Group, Version: v}.String())
			if err != nil {
				return nil, err
			}
			if has(l, d.Resource) {
				out = append(out, Served{Descriptor: d, Version: v})
				break
			}
		}
	}
	return out, nil
}

func has(l *metav1.APIResourceList, resource string) bool {
	for _, r := range l.APIResources {
		if r.Name == resource {
			return true
		}
	}
	return false
}

// ByCategory returns the served kinds in category c.
func ByCategory(served []Served, c Category) []Served {
	var out []Served
	for _, s := range served {
		if s.Category == c {
			out = append(out, s)
		}
	}
	return out
}
