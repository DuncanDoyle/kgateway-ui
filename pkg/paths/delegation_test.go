package paths_test

import (
	"fmt"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
)

// world25 is world with a 2.5 controller, whose rule set has the kgw25 Delegator.
func world25(t *testing.T, env []corev1.EnvVar, objs ...*unstructured.Unstructured) *paths.Topology {
	t.Helper()
	top, i := buildWith(t, worldSnap(t, "v2.5.0", env, objs...))
	if i.Rules.Delegator() == nil {
		t.Fatal("no Delegator")
	}
	return top
}

func route(ns, name string) objects.Ref {
	return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: ns, Name: name}
}

// prefixes is a rule matching each PathPrefix in ps, with backendRefs refs.
func prefixes(ps []string, refs ...any) map[string]any {
	ms := make([]map[string]any, len(ps))
	for i, p := range ps {
		ms[i] = map[string]any{"path": map[string]any{"type": "PathPrefix", "value": p}}
	}
	r := testutil.Rule(ms...)
	r["backendRefs"] = refs
	return r
}

// root is shop/api on infra/edge, with rules.
func root(rules ...map[string]any) *unstructured.Unstructured {
	return testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{testutil.GwRef("infra", "edge")}), rules...)
}

// child is shop/name with no parentRefs, inheriting its parent's matcher when inherit is set.
func child(name string, inherit bool, rules ...map[string]any) *unstructured.Unstructured {
	u := testutil.WithRules(testutil.HTTPRoute("shop", name, nil), rules...)
	if inherit {
		u.SetAnnotations(map[string]string{objects.InheritParentMatcherAnnotation: "true"})
	}
	return u
}

// delegated are top's rows on a chain longer than the root.
func delegated(top *paths.Topology) []paths.Row {
	var out []paths.Row
	for _, r := range top.Rows {
		if len(top.Chain(r)) > 1 {
			out = append(out, r)
		}
	}
	return out
}

// delegationRows are top's rows whose backend is a delegation.
func delegationRows(top *paths.Topology) []paths.Row {
	var out []paths.Row
	for _, r := range top.Rows {
		if b, ok := top.Backend(r); ok && b.Delegation != nil {
			out = append(out, r)
		}
	}
	return out
}

// TestDelegatedChildRows: a child with no Gateway parent gets rows under each
// of its root's attachments, with the chain root → child, the root's
// hostnames, the view C effective match rather than the written one, and
// deps covering the Gateway, both routes and the backend.
func TestDelegatedChildRows(t *testing.T) {
	r := testutil.WithHostnames(root(prefixes([]string{"/api"}, testutil.DelegateTo("shop", "cart"))), "shop.example.com")
	c := child("cart", true, prefixes([]string{"/cart"}, svc("c")))
	top := world25(t, nil, edge("one", "two"), r, c)
	rows := delegated(top)
	if len(rows) != 2 {
		t.Fatalf("delegated rows: %d of %d", len(rows), len(top.Rows))
	}
	gw := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}
	var listeners []string
	for _, row := range rows {
		listeners = append(listeners, top.Listener(row).Name)
		if ch := top.Chain(row); !slices.Equal(ch, []objects.Ref{route("shop", "api"), route("shop", "cart")}) {
			t.Fatalf("chain: %v", ch)
		}
		if b, ok := top.Backend(row); !ok || b.Ref != ref("shop", "c") {
			t.Fatalf("backend: %+v", b)
		}
		if h := top.Hostnames(row); !slices.Equal(h, []string{"shop.example.com"}) {
			t.Fatalf("hostnames: %v", h)
		}
		d := top.Delegation(row)
		if !slices.Equal(d.Provenance, []objects.Ref{route("shop", "api")}) {
			t.Fatalf("provenance: %v", d.Provenance)
		}
		if len(d.Matches) != 1 || d.Matches[0].Path == nil || d.Matches[0].Path.Value != "/api/cart" {
			t.Fatalf("matches: %+v", d.Matches)
		}
		if w := top.Rule(row).Matches; len(w) != 1 || w[0].Path.Value != "/cart" {
			t.Fatalf("written matches: %+v", w)
		}
		if d.Unknown != "" || d.Truncated || d.Unconfirmable || d.NotComputed {
			t.Fatalf("state: %+v", d)
		}
		deps := depRefs(top, row)
		for _, want := range []objects.Ref{gw, route("shop", "api"), route("shop", "cart"), ref("shop", "c")} {
			if !slices.Contains(deps, want) {
				t.Fatalf("deps %v lack %v", deps, want)
			}
		}
	}
	slices.Sort(listeners)
	if !slices.Equal(listeners, []string{"one", "two"}) {
		t.Fatalf("listeners: %v", listeners)
	}
	// A root route's rows have no delegation state.
	for _, row := range top.Rows {
		if len(top.Chain(row)) == 1 {
			if d := top.Delegation(row); d.Provenance != nil || d.Matches != nil || d.Unknown != "" {
				t.Fatalf("root row state: %+v", d)
			}
		}
	}
}

// TestDelegatedTruncated: past view C's per-row context cap, the retained
// contexts give rows marked Truncated, and so does the delegation row.
func TestDelegatedTruncated(t *testing.T) {
	n := computed.DefaultBounds.ContextsPerRow + 1
	ps := make([]string, n)
	for i := range ps {
		ps[i] = fmt.Sprintf("/api/%d", i)
	}
	top := world25(t, nil, edge("http"), root(prefixes(ps, testutil.DelegateTo("shop", "cart"))),
		child("cart", true, prefixes([]string{"/cart"}, svc("c"))))
	rows := delegated(top)
	if len(rows) != computed.DefaultBounds.ContextsPerRow {
		t.Fatalf("delegated rows: %d", len(rows))
	}
	for _, row := range rows {
		if !top.Delegation(row).Truncated {
			t.Fatalf("row not truncated: %+v", top.Delegation(row))
		}
	}
	ds := delegationRows(top)
	if len(ds) != 1 || !top.Delegation(ds[0]).Truncated {
		t.Fatalf("delegation rows: %d", len(ds))
	}
}

// TestDelegatedUnknown: a label ref under an unreadable wildcard setting,
// with the label elsewhere, is setting-unknown in view C; its delegation row says so.
func TestDelegatedUnknown(t *testing.T) {
	unread := corev1.EnvVar{Name: install.WildcardEnv, ValueFrom: &corev1.EnvVarSource{
		ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "cm"}, Key: "k"}}}
	labelled := func(u *unstructured.Unstructured) *unstructured.Unstructured {
		u.SetLabels(map[string]string{objects.DelegationLabelKey: "team"})
		return u
	}
	elsewhere := labelled(testutil.WithRules(testutil.HTTPRoute("other", "x", nil), prefixes([]string{"/api/x"}, svc("x"))))
	top := world25(t, []corev1.EnvVar{unread}, edge("http"), root(prefixes([]string{"/api"}, testutil.DelegateLabel("shop", "team"))),
		labelled(child("cart", false, prefixes([]string{"/api/cart"}, svc("c")))), elsewhere)
	ds := delegationRows(top)
	if len(ds) != 1 || top.Delegation(ds[0]).Unknown != computed.UnknownSetting {
		t.Fatalf("delegation rows: %d, state %+v", len(ds), top.Delegation(ds[0]))
	}
	// The own-namespace child is certain; the possible one has no rows.
	rows := delegated(top)
	if len(rows) != 1 || top.Chain(rows[0])[1] != route("shop", "cart") {
		t.Fatalf("delegated rows: %d", len(rows))
	}
}

// TestDelegatedUnconfirmable: a child attached under one rule and dropped
// under another is unconfirmable on the dropped rule; only that delegation
// row says so, and only the attached rule has child rows.
func TestDelegatedUnconfirmable(t *testing.T) {
	top := world25(t, nil, edge("http"), root(prefixes([]string{"/api"}, testutil.DelegateTo("shop", "cart")),
		prefixes([]string{"/other"}, testutil.DelegateTo("shop", "cart"))), child("cart", false, prefixes([]string{"/api/cart"}, svc("c"))))
	ds := delegationRows(top)
	if len(ds) != 2 {
		t.Fatalf("delegation rows: %d", len(ds))
	}
	got := map[int]bool{}
	for _, r := range ds {
		got[top.Rule(r).Index] = top.Delegation(r).Unconfirmable
	}
	if got[0] || !got[1] {
		t.Fatalf("unconfirmable by rule: %v", got)
	}
	rows := delegated(top)
	if len(rows) != 1 || top.Delegation(rows[0]).Matches[0].Path.Value != "/api/cart" {
		t.Fatalf("delegated rows: %d", len(rows))
	}
}

// TestNoDelegator: without a Delegator, a backendRef to an HTTPRoute is an
// ordinary ref of kind HTTPRoute, and there are no child rows.
func TestNoDelegator(t *testing.T) {
	_, i := buildWith(t, worldSnap(t, "v2.5.0", nil, edge("http"), root(prefixes([]string{"/api"}, testutil.DelegateTo("shop", "cart"))),
		child("cart", true, prefixes([]string{"/cart"}, svc("c")))))
	top := paths.Build(paths.Input{Index: i.Index, Views: i.Computed, Decode: objects.Direct{}, Delegates: false})
	if len(top.Rows) != 1 {
		t.Fatalf("rows: %d", len(top.Rows))
	}
	b, ok := top.Backend(top.Rows[0])
	if !ok || b.Delegation != nil || b.Ref != route("shop", "cart") {
		t.Fatalf("backend: %+v", b)
	}
	if d := top.Delegation(top.Rows[0]); d.Unknown != "" || d.Truncated || d.Provenance != nil {
		t.Fatalf("state: %+v", d)
	}
}

// TestDelegatedRefsMerge: two refs of one rule that select the same child
// give the child's rows once per attachment.
func TestDelegatedRefsMerge(t *testing.T) {
	top := world25(t, nil, edge("http"), root(prefixes([]string{"/api"}, testutil.DelegateTo("shop", "cart"), testutil.DelegateWildcard("shop"))),
		child("cart", true, prefixes([]string{"/cart"}, svc("c"))))
	if n := len(delegationRows(top)); n != 2 {
		t.Fatalf("delegation rows: %d", n)
	}
	if rows := delegated(top); len(rows) != 1 {
		t.Fatalf("delegated rows: %d", len(rows))
	}
}

// unknownWorld has a rule whose label ref is setting-unknown with the label
// elsewhere, then a name ref to shop/plain: the rule's later refs inherit the
// uncertainty, so view C reaches plain only under unknown contexts.
func unknownWorld(t *testing.T) *paths.Topology {
	t.Helper()
	unread := corev1.EnvVar{Name: install.WildcardEnv, ValueFrom: &corev1.EnvVarSource{
		ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "cm"}, Key: "k"}}}
	labelled := func(u *unstructured.Unstructured) *unstructured.Unstructured {
		u.SetLabels(map[string]string{objects.DelegationLabelKey: "team"})
		return u
	}
	elsewhere := labelled(testutil.WithRules(testutil.HTTPRoute("other", "x", nil), prefixes([]string{"/api/x"}, svc("x"))))
	plain := child("plain", false, prefixes([]string{"/api/a"}, svc("a")), prefixes([]string{"/api/b"}, svc("b")))
	return world25(t, []corev1.EnvVar{unread}, edge("http"),
		root(prefixes([]string{"/api"}, testutil.DelegateLabel("shop", "team"), testutil.DelegateTo("shop", "plain"))),
		labelled(child("cart", false, prefixes([]string{"/api/cart"}, svc("c")))), elsewhere, plain)
}

// TestDelegatedUnknownContext: a child view C reaches only under unknown
// contexts gets one row per written rule, unknown and with no effective match.
func TestDelegatedUnknownContext(t *testing.T) {
	top := unknownWorld(t)
	var rows []paths.Row
	for _, r := range delegated(top) {
		if top.Chain(r)[1] == route("shop", "plain") {
			rows = append(rows, r)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("plain rows: %d", len(rows))
	}
	for i, r := range rows {
		d := top.Delegation(r)
		if top.Rule(r).Index != i || d.Unknown == "" || d.Matches != nil {
			t.Fatalf("row %d: rule %d, state %+v", i, top.Rule(r).Index, d)
		}
	}
}

// TestEmptyOutcomeIsUnknown: a context with no outcome counts as unknown
// (computed.Context), so its child's written rules are unknown rows.
func TestEmptyOutcomeIsUnknown(t *testing.T) {
	_, i := buildWith(t, worldSnap(t, "v2.5.0", nil, edge("http"), root(prefixes([]string{"/api"}, testutil.DelegateTo("shop", "cart"))),
		child("cart", true, prefixes([]string{"/cart"}, svc("c")), prefixes([]string{"/more"}, svc("d")))))
	views := *i.Computed
	views.Delegation.Rows = slices.Clone(views.Delegation.Rows)
	for k := range views.Delegation.Rows {
		row := &views.Delegation.Rows[k]
		row.Contexts = slices.Clone(row.Contexts)
		for c := range row.Contexts {
			row.Contexts[c].Outcome, row.Contexts[c].Rules = "", nil
		}
	}
	// The topology reads the index's copy of the rows.
	ix := *i.Index
	ix.DelegRows = slices.Clone(ix.DelegRows)
	for k := range ix.DelegRows {
		ix.DelegRows[k].Row = views.Delegation.Rows[k]
	}
	top := paths.Build(paths.Input{Index: &ix, Views: &views, Decode: objects.Direct{}, Delegates: true})
	rows := delegated(top)
	if len(rows) != 2 {
		t.Fatalf("delegated rows: %d", len(rows))
	}
	for _, r := range rows {
		if d := top.Delegation(r); d.Unknown == "" || d.Matches != nil {
			t.Fatalf("state: %+v", d)
		}
	}
}

// Every row of a child reached under a root whose eligibility reads a
// Namespace carries that Namespace, whether the root is eligible or not
// (design §7.4 rule 4, §13).
func TestDelegatedRowsCarryRootEligibility(t *testing.T) {
	ns := objects.Ref{Kind: "Namespace", Name: "shop"}
	for _, zone := range []string{"in", "out"} {
		sel := objects.Listener{Name: "sel", Port: 8080, Protocol: "HTTP", AllowedRoutes: objects.AllowedRoutes{Namespaces: objects.RouteNamespaces{
			From: "Selector", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"zone": "in"}}}}}
		edge2 := testutil.GatewayWithListeners("infra", "edge2", "kgateway", []objects.Listener{sel}, testutil.Listener{Name: "sel", Conds: ready})
		r2 := testutil.WithRules(testutil.HTTPRoute("shop", "r2", []objects.ParentRef{testutil.GwRef("infra", "edge2")}),
			prefixes([]string{"/x"}, testutil.DelegateTo("shop", "mid")))
		snap := worldSnap(t, "v2.5.0", nil, edge("http"), edge2, root(prefixes([]string{"/x"}, testutil.DelegateTo("shop", "mid"))), r2,
			child("mid", false, prefixes([]string{"/x/m"}, testutil.DelegateTo("shop", "kid"))), child("kid", false, prefixes([]string{"/x/m/k"}, svc("k"))))
		for i, n := range snap.Namespaces {
			if n.Name == "shop" {
				snap.Namespaces[i] = testutil.Namespace("shop", map[string]string{"zone": zone})
			}
		}
		snap.Access[cache.NamespacesGVR] = cache.Access{OK: true}
		top, _ := buildWith(t, snap)
		var kids int
		for _, r := range top.Rows {
			if c := top.Chain(r); c[len(c)-1] != route("shop", "kid") {
				continue
			}
			kids++
			if !slices.ContainsFunc(top.Deps(r), func(d int32) bool { return top.Refs[d] == ns }) {
				var ds []objects.Ref
				for _, d := range top.Deps(r) {
					ds = append(ds, top.Refs[d])
				}
				t.Errorf("zone %s: kid row via %v lacks the shop Namespace: %v", zone, top.Chain(r), ds)
			}
		}
		if kids == 0 {
			t.Errorf("zone %s: no kid rows", zone)
		}
	}
}
