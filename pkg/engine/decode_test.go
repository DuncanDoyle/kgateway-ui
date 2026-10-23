package engine

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
)

// gw returns a Gateway with the given identity; empty uid or rv omits it.
func gw(name, uid, rv string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "Gateway",
		"metadata": map[string]any{"name": name, "namespace": "shop"},
		"spec":     map[string]any{"gatewayClassName": "kgateway"},
	}}
	if uid != "" {
		u.SetUID(types.UID(uid))
	}
	if rv != "" {
		u.SetResourceVersion(rv)
	}
	return u
}

func TestDecodeCacheReusesUnchangedObjects(t *testing.T) {
	c := newDecodeCache()
	first, second := c.Gateway(gw("a", "u1", "1")), c.Gateway(gw("a", "u1", "1"))
	if first != second {
		t.Fatal("same uid and resourceVersion decoded twice")
	}
}

func TestDecodeCacheMissesOnNewResourceVersion(t *testing.T) {
	c := newDecodeCache()
	first := c.Gateway(gw("a", "u1", "1"))
	if second := c.Gateway(gw("a", "u1", "2")); second == first {
		t.Fatal("new resourceVersion served the stale view")
	}
}

func TestDecodeCacheBypassWithoutKeys(t *testing.T) {
	c := newDecodeCache()
	for _, u := range []*unstructured.Unstructured{gw("a", "", "1"), gw("a", "u1", ""), gw("a", "", "")} {
		first, second := c.Gateway(u), c.Gateway(u)
		if first == second {
			t.Fatalf("object uid=%q rv=%q was cached", u.GetUID(), u.GetResourceVersion())
		}
	}
	if n := c.size(); n != 0 {
		t.Fatalf("bypassed objects left %d entries", n)
	}
}

func TestDecodeCachePrunedWithObserve(t *testing.T) {
	keep, drop := gw("keep", "u1", "1"), gw("drop", "u2", "1")
	e := New(config(snapshot(t, keep, drop), metricsAt(testutil.Now), nil))
	e.decode.Gateway(keep)
	e.decode.Gateway(drop)
	e.decode.HTTPRoute(drop)
	if n := e.decode.size(); n != 3 {
		t.Fatalf("seeded %d entries", n)
	}
	// Snapshot holds only keep, at a newer resourceVersion: both are stale.
	keep2 := gw("keep", "u1", "2")
	e.cfg.Snapshots = staticSnap{snapshot(t, keep2)}
	e.Compute()
	// The index decodes keep2 during Compute; no stale entry survives.
	if n, c := e.decode.size(), e.decode.gateways["u1"]; n > 1 || n == 1 && c.rv != "2" {
		t.Fatalf("%d entries survived observe (keep at rv %q)", n, c.rv)
	}
	// An unchanged live object keeps its entry.
	v := e.decode.Gateway(keep2)
	e.Compute()
	if e.decode.Gateway(keep2) != v {
		t.Fatal("live unchanged object was pruned")
	}
}
