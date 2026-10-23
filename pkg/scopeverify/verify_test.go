package scopeverify

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

func input(t *testing.T, now time.Time, managed map[ctrlmetrics.ManagedKey]float64, present bool) Input {
	t.Helper()
	gw := testutil.GwRef("shop", "edge")
	return inputWith(t, now, managed, present,
		testutil.Gateway("shop", "edge", "any-class", nil), // raw count: class is not considered
		testutil.HTTPRoute("shop", "two-parents", []objects.ParentRef{gw, testutil.GwRef("shop", "other")}),
		testutil.HTTPRoute("shop", "no-parents", nil),
		testutil.TrafficPolicy("shop", "tp"),
		testutil.HTTPRoute("blog", "out-of-scope", []objects.ParentRef{gw}),
	)
}

func inputWith(t *testing.T, now time.Time, managed map[ctrlmetrics.ManagedKey]float64, present bool, objs ...*unstructured.Unstructured) Input {
	t.Helper()
	snap := &cache.Snapshot{Objects: testutil.ObjectsByGVR(t, objs...), Synced: true,
		Namespaces: []*corev1.Namespace{testutil.Namespace("shop", map[string]string{"team": "a"}), testutil.Namespace("blog", nil)}}
	return Input{Install: install.Resolved{Install: install.Install{Namespace: "kgw", Name: "kgateway"}, Scope: install.ParseSelectors(`[{"matchLabels":{"team":"a"}}]`)},
		Snapshot: snap, Served: testutil.ServedUpstream(), HasMetrics: true, Now: now,
		Metrics: ctrlmetrics.Result{CollectedAt: now, Metrics: ctrlmetrics.Metrics{Managed: managed, ManagedPresent: present}}}
}

// Weights mirror the gauge: Gateway 1, route 1 per parentRef, policy 1.
var matching = map[ctrlmetrics.ManagedKey]float64{{Namespace: "shop", Resource: "Gateway"}: 1, {Namespace: "shop", Resource: "HTTPRoute"}: 2, {Namespace: "shop", Resource: "TrafficPolicy"}: 1}

func TestMatchingCountsHaveNoMismatch(t *testing.T) {
	r := NewManagedCountVerifier(20 * time.Second).Verify(input(t, testutil.Now, matching, true))
	if !r.Available || len(r.Mismatches) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestMismatchMustPersist(t *testing.T) {
	v := NewManagedCountVerifier(20 * time.Second)
	off := map[ctrlmetrics.ManagedKey]float64{{Namespace: "shop", Resource: "Gateway"}: 1, {Namespace: "shop", Resource: "HTTPRoute"}: 3, {Namespace: "shop", Resource: "TrafficPolicy"}: 1}
	if r := v.Verify(input(t, testutil.Now, off, true)); len(r.Mismatches) != 0 {
		t.Fatal("a fresh mismatch must not be reported yet")
	}
	r := v.Verify(input(t, testutil.Now.Add(25*time.Second), off, true))
	if len(r.Mismatches) != 1 || r.Mismatches[0].UI != 2 || r.Mismatches[0].Controller != 3 {
		t.Fatalf("persistent mismatch not reported: %+v", r)
	}
	if r := v.Verify(input(t, testutil.Now.Add(30*time.Second), matching, true)); len(r.Mismatches) != 0 {
		t.Fatal("mismatch must clear when counts agree")
	}
}

func TestAbsentFamilyWithObjectsIsUnavailable(t *testing.T) {
	r := NewManagedCountVerifier(20 * time.Second).Verify(input(t, testutil.Now, nil, false))
	if r.Available || r.Reason == "" {
		t.Fatalf("%+v", r)
	}
}

// Counts from an old scrape must not verify the current scope.
func TestStaleMetricsAreUnavailable(t *testing.T) {
	in := input(t, testutil.Now, matching, true)
	in.Metrics.CollectedAt = testutil.Now.Add(-5 * time.Minute)
	in.MetricsMaxAge = 30 * time.Second
	if r := NewManagedCountVerifier(20 * time.Second).Verify(in); r.Available {
		t.Fatalf("stale metrics must make the verifier unavailable: %+v", r)
	}
}

// A mismatch is reported only once it has lasted strictly longer than persist.
func TestMismatchPersistenceIsStrict(t *testing.T) {
	v := NewManagedCountVerifier(20 * time.Second)
	off := map[ctrlmetrics.ManagedKey]float64{{Namespace: "shop", Resource: "Gateway"}: 1, {Namespace: "shop", Resource: "HTTPRoute"}: 3, {Namespace: "shop", Resource: "TrafficPolicy"}: 1}
	matures := testutil.Now.Add(20 * time.Second)
	if r := v.Verify(input(t, testutil.Now, off, true)); !r.Matures.Equal(matures) {
		t.Fatalf("a pending mismatch must name its maturation instant %v: %+v", matures, r)
	}
	if r := v.Verify(input(t, testutil.Now.Add(20*time.Second), off, true)); len(r.Mismatches) != 0 || !r.Matures.Equal(matures) {
		t.Fatalf("a mismatch lasting exactly persist must not be reported: %+v", r)
	}
	if r := v.Verify(input(t, testutil.Now.Add(21*time.Second), off, true)); len(r.Mismatches) != 1 || !r.Matures.IsZero() {
		t.Fatalf("a mismatch lasting longer than persist must be reported, with nothing left pending: %+v", r)
	}
}

// An absent family in a successful scrape means 0 (D-012): with nothing in
// scope the counts match.
func TestAbsentFamilyWithoutObjectsIsMatch(t *testing.T) {
	r := NewManagedCountVerifier(20 * time.Second).Verify(inputWith(t, testutil.Now, nil, false))
	if !r.Available || len(r.Mismatches) != 0 {
		t.Fatalf("%+v", r)
	}
}

// Persistence needs continuous observation: an unavailable period restarts it.
func TestUnavailableRestartsPersistence(t *testing.T) {
	v := NewManagedCountVerifier(20 * time.Second)
	off := map[ctrlmetrics.ManagedKey]float64{{Namespace: "shop", Resource: "Gateway"}: 1, {Namespace: "shop", Resource: "HTTPRoute"}: 3, {Namespace: "shop", Resource: "TrafficPolicy"}: 1}
	v.Verify(input(t, testutil.Now, off, true))
	gap := input(t, testutil.Now.Add(10*time.Second), off, true)
	gap.Metrics.Err = "connection refused"
	v.Verify(gap)
	if r := v.Verify(input(t, testutil.Now.Add(25*time.Second), off, true)); len(r.Mismatches) != 0 {
		t.Fatalf("persistence must restart after an unavailable period: %+v", r)
	}
}
