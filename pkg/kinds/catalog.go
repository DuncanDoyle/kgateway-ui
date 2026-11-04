package kinds

import (
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Catalog is every (group, kind) the API server serves, from discovery. The
// zero Catalog knows no group: every served state is unknown.
type Catalog struct {
	served map[schema.GroupKind]bool
	// byKind holds each kind's serving groups, sorted.
	byKind map[string][]string
	// unknown are groups whose discovery failed; a nil served map knows none.
	unknown map[string]bool
	key     string
}

// NewCatalog returns the Catalog of served kinds, where discovery failed for
// the unknown groups.
func NewCatalog(served []schema.GroupKind, unknown []string) Catalog {
	c := Catalog{served: map[schema.GroupKind]bool{}, byKind: map[string][]string{}, unknown: map[string]bool{}}
	for _, gk := range served {
		if !c.served[gk] {
			c.served[gk] = true
			c.byKind[gk.Kind] = append(c.byKind[gk.Kind], gk.Group)
		}
	}
	for _, g := range unknown {
		c.unknown[g] = true
	}
	parts := make([]string, 0, len(c.served)+len(c.unknown))
	for gk := range c.served {
		parts = append(parts, gk.Group+"/"+gk.Kind)
	}
	for g := range c.unknown {
		parts = append(parts, "?"+g)
	}
	for _, gs := range c.byKind {
		slices.Sort(gs)
	}
	slices.Sort(parts)
	c.key = "known;" + strings.Join(parts, ",")
	return c
}

// Served reports whether group serves kind.
func (c Catalog) Served(group, kind string) bool {
	return c.served[schema.GroupKind{Group: group, Kind: kind}]
}

// Known reports whether discovery listed group completely, so a kind it lacks
// is known not to be served. A group discovery never listed is known.
func (c Catalog) Known(group string) bool { return c.served != nil && !c.unknown[group] }

// GroupsWithKind returns the groups that serve kind, sorted.
func (c Catalog) GroupsWithKind(kind string) []string { return slices.Clone(c.byKind[kind]) }

// Key identifies the Catalog's contents; equal catalogs have equal keys.
func (c Catalog) Key() string { return c.key }

// groupsAndResources is the discovery call a Catalog needs; clients without it
// give the zero Catalog.
type groupsAndResources interface {
	ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error)
}

// discoverCatalog lists every served kind. A group whose discovery failed in
// part is unknown; false means no catalog could be built.
func discoverCatalog(dc DiscoveryClient) (Catalog, bool) {
	gr, ok := dc.(groupsAndResources)
	if !ok {
		return Catalog{}, true
	}
	groups, lists, err := gr.ServerGroupsAndResources()
	if err != nil && len(groups) == 0 {
		return Catalog{}, false
	}
	listed := map[string]bool{}
	var served []schema.GroupKind
	for _, l := range lists {
		if l == nil {
			continue
		}
		gv, perr := schema.ParseGroupVersion(l.GroupVersion)
		if perr != nil {
			continue
		}
		listed[l.GroupVersion] = true
		for _, r := range l.APIResources {
			if !strings.Contains(r.Name, "/") { // subresources are not kinds
				served = append(served, schema.GroupKind{Group: gv.Group, Kind: r.Kind})
			}
		}
	}
	// A partial error leaves out the failed group versions' lists.
	var unknown []string
	if err != nil {
		for _, g := range groups {
			for _, v := range g.Versions {
				if !listed[v.GroupVersion] {
					unknown = append(unknown, g.Name)
					break
				}
			}
		}
	}
	return NewCatalog(served, unknown), true
}
