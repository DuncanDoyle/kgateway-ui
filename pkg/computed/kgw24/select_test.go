// Port of kgw23 for v2.4.7; keep in sync.

package kgw24

import (
	"reflect"
	"slices"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

func routeRef(ns, name string) objects.Ref {
	return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: ns, Name: name}
}

// delegRoute is an in-scope HTTPRoute ns/name with an optional delegation label.
func delegRoute(ns, name, label string) computed.DelegRoute {
	v := &objects.HTTPRouteView{}
	if label != "" {
		v.Labels = map[string]string{objects.DelegationLabelKey: label}
	}
	return computed.DelegRoute{Ref: routeRef(ns, name), View: v}
}

func known(v string) install.Setting { return install.Setting{Value: v, Known: true} }

// selectWorld is sorted by (namespace, name), as DelegationInput.Routes is.
func selectWorld() []computed.DelegRoute {
	return []computed.DelegRoute{
		delegRoute("all", "a1", "v"),
		delegRoute("orders", "o1", ""),
		delegRoute("orders", "o2", "v"),
		delegRoute("shop", "s1", "v"),
		delegRoute("shop", "s2", "w"),
		delegRoute("team-a", "t1", "v"),
	}
}

func refs(rs ...objects.Ref) []objects.Ref { return rs }

func TestFetchModes(t *testing.T) {
	unwatched := map[objects.Ref]bool{routeRef("blog", "posts"): true}
	s := newSelector(selectWorld(), unwatched, known(install.WildcardAll))
	for _, c := range []struct {
		name       string
		ref        objects.DelegationRef
		children   []objects.Ref
		unresolved computed.Unresolved
	}{
		{"name", objects.DelegationRef{Mode: objects.DelegationName, Namespace: "orders", Name: "o1"}, refs(routeRef("orders", "o1")), ""},
		{"missing", objects.DelegationRef{Mode: objects.DelegationName, Namespace: "orders", Name: "nope"}, nil, computed.UnresolvedMissing},
		{"out of scope", objects.DelegationRef{Mode: objects.DelegationName, Namespace: "blog", Name: "posts"}, nil, computed.UnresolvedUnwatched},
		// "*" and "" both decode to the wildcard mode (objects TestDecodeDelegationModes).
		{"wildcard", objects.DelegationRef{Mode: objects.DelegationWildcard, Namespace: "orders"}, refs(routeRef("orders", "o1"), routeRef("orders", "o2")), ""},
		{"wildcard empty ns", objects.DelegationRef{Mode: objects.DelegationWildcard, Namespace: "empty"}, nil, computed.UnresolvedMissing},
		// An unwatched namespace selects nothing; only a name ref says "not watched".
		{"wildcard unwatched ns", objects.DelegationRef{Mode: objects.DelegationWildcard, Namespace: "blog"}, nil, computed.UnresolvedMissing},
		{"label", objects.DelegationRef{Mode: objects.DelegationLabel, Namespace: "shop", Label: "v"}, refs(routeRef("shop", "s1")), ""},
		{"label other value", objects.DelegationRef{Mode: objects.DelegationLabel, Namespace: "shop", Label: "x"}, nil, computed.UnresolvedMissing},
		{"label unlabelled ns", objects.DelegationRef{Mode: objects.DelegationLabel, Namespace: "orders", Label: "w"}, nil, computed.UnresolvedMissing},
	} {
		t.Run(c.name, func(t *testing.T) {
			children, possible, unresolved, unknown := s.fetch(c.ref)
			if !reflect.DeepEqual(children, c.children) || unresolved != c.unresolved || possible != nil || unknown != "" {
				t.Fatalf("got %v %v %q %q, want %v %q", children, possible, unresolved, unknown, c.children, c.unresolved)
			}
		})
	}
}

func TestLabelWildcardNamespace(t *testing.T) {
	everywhere := refs(routeRef("all", "a1"), routeRef("orders", "o2"), routeRef("shop", "s1"), routeRef("team-a", "t1"))
	label := func(ns string) objects.DelegationRef {
		return objects.DelegationRef{Mode: objects.DelegationLabel, Namespace: ns, Label: "v"}
	}
	for _, c := range []struct {
		name     string
		wildcard install.Setting
		ns       string
		children []objects.Ref
	}{
		{"default all", known("all"), "all", everywhere},
		{"default other ns", known("all"), "team-a", refs(routeRef("team-a", "t1"))},
		{"set team-a", known("team-a"), "team-a", everywhere},
		{"set team-a, ns all", known("team-a"), "all", refs(routeRef("all", "a1"))},
		// ReadSettings turns an empty value into "all"; the selector does too.
		{"empty means all", known(""), "all", everywhere},
	} {
		t.Run(c.name, func(t *testing.T) {
			children, possible, unresolved, unknown := newSelector(selectWorld(), nil, c.wildcard).fetch(label(c.ns))
			if !reflect.DeepEqual(children, c.children) || possible != nil || unresolved != "" || unknown != "" {
				t.Fatalf("got %v %v %q %q, want %v", children, possible, unresolved, unknown, c.children)
			}
		})
	}

	t.Run("valueFrom", func(t *testing.T) {
		s := newSelector(selectWorld(), nil, install.Setting{Reason: "valueFrom"})
		// The ref's own namespace is selected whatever the setting; the rest is only possible.
		for _, c := range []struct {
			ns                 string
			children, possible []objects.Ref
		}{
			{"all", refs(routeRef("all", "a1")), refs(routeRef("orders", "o2"), routeRef("shop", "s1"), routeRef("team-a", "t1"))},
			{"shop", refs(routeRef("shop", "s1")), refs(routeRef("all", "a1"), routeRef("orders", "o2"), routeRef("team-a", "t1"))},
			{"empty", nil, everywhere},
		} {
			children, possible, unresolved, unknown := s.fetch(label(c.ns))
			if !reflect.DeepEqual(children, c.children) || unresolved != "" || unknown != computed.UnknownSetting || !reflect.DeepEqual(possible, c.possible) {
				t.Fatalf("%s: got %v %v %q %q", c.ns, children, possible, unresolved, unknown)
			}
		}
		// With no labelled route elsewhere the selection is known: only shop/s2 has label w.
		children, possible, unresolved, unknown := s.fetch(objects.DelegationRef{Mode: objects.DelegationLabel, Namespace: "shop", Label: "w"})
		if !reflect.DeepEqual(children, refs(routeRef("shop", "s2"))) || possible != nil || unresolved != "" || unknown != "" {
			t.Fatalf("label w: got %v %v %q %q", children, possible, unresolved, unknown)
		}
		// Name and wildcard refs do not read the setting.
		children, _, _, unknown = s.fetch(objects.DelegationRef{Mode: objects.DelegationWildcard, Namespace: "shop"})
		if len(children) != 2 || unknown != "" {
			t.Fatalf("wildcard ref under unknown setting: %v %q", children, unknown)
		}
	})
}

func TestPermits(t *testing.T) {
	parent := routeRef("shop", "api")
	gw, hr := objects.GatewayAPIGroup, "HTTPRoute"
	for _, c := range []struct {
		name   string
		refs   []objects.WrittenRef
		ok     bool
		allows computed.Allows
	}{
		{"empty list", nil, true, computed.AllowsAny},
		{"explicit, own ns", []objects.WrittenRef{{Group: gw, Kind: hr, Name: "api"}}, true, computed.AllowsThis},
		{"explicit ns", []objects.WrittenRef{{Group: gw, Kind: hr, Namespace: "shop", Name: "api"}}, true, computed.AllowsThis},
		{"nil group", []objects.WrittenRef{{Kind: hr, Name: "api"}}, false, computed.AllowsOther},
		{"nil kind", []objects.WrittenRef{{Group: gw, Name: "api"}}, false, computed.AllowsOther},
		{"section and port ignored", []objects.WrittenRef{{Group: gw, Kind: hr, Name: "api", SectionName: "http", Port: 8080, PortSet: true}}, true, computed.AllowsThis},
		{"another parent", []objects.WrittenRef{{Group: gw, Kind: hr, Name: "other"}}, false, computed.AllowsOther},
		{"another ns", []objects.WrittenRef{{Group: gw, Kind: hr, Namespace: "orders", Name: "api"}}, false, computed.AllowsOther},
		{"gateway parent only", []objects.WrittenRef{{Name: "edge", Namespace: "infra"}}, false, computed.AllowsOther},
		// Group is compared case-sensitively, as upstream does.
		{"group other case", []objects.WrittenRef{{Group: "Gateway.networking.k8s.io", Kind: hr, Name: "api"}}, false, computed.AllowsOther},
		// A written "" decodes like an omitted field; neither matches upstream.
		{"empty-string group", []objects.WrittenRef{{Group: "", Kind: hr, Name: "api"}}, false, computed.AllowsOther},
		{"empty-string kind", []objects.WrittenRef{{Group: gw, Kind: "", Name: "api"}}, false, computed.AllowsOther},
		{"second ref matches", []objects.WrittenRef{{Group: gw, Kind: hr, Name: "other"}, {Group: gw, Kind: hr, Name: "api"}}, true, computed.AllowsThis},
	} {
		t.Run(c.name, func(t *testing.T) {
			ok, allows := permits(&objects.HTTPRouteView{WrittenRefs: c.refs}, "shop", parent)
			if ok != c.ok || allows != c.allows {
				t.Fatalf("got %v %q, want %v %q", ok, allows, c.ok, c.allows)
			}
		})
	}
	// The child's own namespace is the default, not the parent's.
	if ok, _ := permits(&objects.HTTPRouteView{WrittenRefs: []objects.WrittenRef{{Group: gw, Kind: hr, Name: "api"}}}, "orders", parent); ok {
		t.Fatal("child in orders with no ref namespace permitted shop/api")
	}
}

// Unwatched routes are never candidates, not even as uncertain points.
func TestPossibleExcludesUnwatched(t *testing.T) {
	unwatched := map[objects.Ref]bool{routeRef("blog", "posts"): true} // carries label v, outside the scope
	s := newSelector(selectWorld(), unwatched, install.Setting{Reason: "valueFrom"})
	_, possible, _, unknown := s.fetch(objects.DelegationRef{Mode: objects.DelegationLabel, Namespace: "all", Label: "v"})
	if unknown != computed.UnknownSetting || slices.Contains(possible, routeRef("blog", "posts")) || len(possible) != 3 {
		t.Fatalf("possible %v, unknown %q", possible, unknown)
	}
}
