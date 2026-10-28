package engine

import (
	"maps"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

type snapRef struct{ s *cache.Snapshot }

func (r *snapRef) Snapshot() *cache.Snapshot { return r.s }

// stateless is v's results without what persistence sets from history.
func stateless(v *computed.Views) computed.Views {
	out := *v
	out.Checks = slices.Clone(v.Checks)
	for i := range out.Checks {
		out.Checks[i].State, out.Checks[i].Mature, out.Checks[i].Since = "", false, time.Time{}
	}
	return out
}

// The views memo reuses results only for equal inputs: after each change the
// engine's views equal a fresh engine's, an unchanged snapshot or an
// annotation edit reuses them, and a change views read recomputes them.
func TestViewsMemoMatchesFreshEngine(t *testing.T) {
	now := testutil.Now
	clock := func() time.Time { return now }
	src := &snapRef{scale.Snapshot(scale.Typical, 1)}
	cfg := config(nil, metricsAt(now), clock)
	cfg.Snapshots, cfg.MetricsMaxAge = src, 0
	e := New(cfg)
	routes := gvr(t, "HTTPRoute")

	// replace returns src's snapshot with u in place of the object of the same ref.
	replace := func(g schema.GroupVersionResource, u *unstructured.Unstructured) *cache.Snapshot {
		s := *src.s
		s.Objects = maps.Clone(s.Objects)
		us := slices.Clone(s.Objects[g])
		i := slices.IndexFunc(us, func(x *unstructured.Unstructured) bool { return objects.RefOf(x) == objects.RefOf(u) })
		if i < 0 {
			t.Fatalf("no %s", objects.RefOf(u))
		}
		us[i] = u
		s.Objects[g] = us
		return &s
	}
	find := func(g schema.GroupVersionResource, ref objects.Ref) *unstructured.Unstructured {
		for _, u := range src.s.Objects[g] {
			if objects.RefOf(u) == ref {
				return u
			}
		}
		t.Fatalf("no %s", ref)
		return nil
	}
	rv := 1_000_000
	edited := func(u *unstructured.Unstructured, spec bool) *unstructured.Unstructured {
		c := u.DeepCopy()
		rv++
		c.SetResourceVersion(strconv.Itoa(rv))
		if spec {
			c.SetGeneration(c.GetGeneration() + 1)
		}
		return c
	}
	compute := func(step string) *computed.Views {
		t.Helper()
		got := e.Compute().Installs[0].Computed
		fcfg := cfg
		fcfg.Snapshots = &snapRef{src.s}
		want := New(fcfg).Compute().Installs[0].Computed
		if g, w := stateless(got), stateless(want); !reflect.DeepEqual(g, w) {
			for i := range min(len(g.Checks), len(w.Checks)) {
				if !reflect.DeepEqual(g.Checks[i], w.Checks[i]) {
					t.Logf("check %d:\n%+v\n%+v", i, g.Checks[i], w.Checks[i])
					break
				}
			}
			t.Fatalf("%s: views differ from a fresh engine's: attachment %v delegation %v checks %v (%d/%d)", step,
				reflect.DeepEqual(g.Attachment, w.Attachment), reflect.DeepEqual(g.Delegation, w.Delegation), reflect.DeepEqual(g.Checks, w.Checks), len(g.Checks), len(w.Checks))
		}
		return got
	}
	reused := func(a, b *computed.Views) bool {
		return &a.Attachment.Outcomes[0] == &b.Attachment.Outcomes[0] && &a.Delegation.Rows[0] == &b.Delegation.Rows[0]
	}

	v0 := compute("first")
	if len(v0.Delegation.Reach) == 0 || len(v0.Attachment.Outcomes) == 0 {
		t.Fatal("the world has no delegation")
	}
	if v := compute("unchanged"); !reused(v0, v) {
		t.Fatal("unchanged: views recomputed")
	}

	edge := v0.Delegation.Reach[0]
	child := find(routes, edge.Child)
	c := edited(child, false)
	c.SetAnnotations(map[string]string{"memo": "1"})
	src.s = replace(routes, c)
	if v := compute("annotation"); !reused(v0, v) {
		t.Fatal("annotation edit: views recomputed")
	}

	// A child's new path match is view C input.
	c = edited(child, true)
	ms, _, _ := unstructured.NestedSlice(c.Object, "spec", "rules")
	rule := ms[0].(map[string]any)
	rule["matches"] = []any{map[string]any{"path": map[string]any{"type": "PathPrefix", "value": "/memo-elsewhere"}}}
	if err := unstructured.SetNestedSlice(c.Object, ms, "spec", "rules"); err != nil {
		t.Fatal(err)
	}
	src.s = replace(routes, c)
	v1 := compute("child match")
	if reused(v0, v1) || reflect.DeepEqual(v0.Delegation.Rows, v1.Delegation.Rows) {
		t.Fatal("child match: delegation rows not recomputed")
	}

	// The parent losing its delegation rule is view B input.
	p := edited(find(routes, edge.Parent), true)
	unstructured.RemoveNestedField(p.Object, "spec", "rules")
	src.s = replace(routes, p)
	v2 := compute("parent rules")
	if reflect.DeepEqual(v1.Delegation.Reach, v2.Delegation.Reach) {
		t.Fatal("parent rules: reach not recomputed")
	}

	// Namespace labels are view A input (selector listeners).
	s := *src.s
	s.Namespaces = nil
	for _, n := range src.s.Namespaces {
		c := n.DeepCopy()
		c.Labels = nil
		s.Namespaces = append(s.Namespaces, c)
	}
	src.s = &s
	v3 := compute("namespace labels")
	if reflect.DeepEqual(v2.Attachment.Outcomes, v3.Attachment.Outcomes) {
		t.Fatal("namespace labels: outcomes not recomputed")
	}

	// A Gateway's listener status is view A input.
	gws := gvr(t, "Gateway")
	gw := edited(src.s.Objects[gws][0], false)
	unstructured.RemoveNestedField(gw.Object, "status")
	src.s = replace(gws, gw)
	v4 := compute("gateway status")
	if reflect.DeepEqual(v3.Checks, v4.Checks) {
		t.Fatal("gateway status: checks not recomputed")
	}
}
