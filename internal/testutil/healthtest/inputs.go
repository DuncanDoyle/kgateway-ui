// Package healthtest has test helpers for the health check contract. It is
// apart from testutil, whose callers pkg/health imports.
package healthtest

import (
	"fmt"
	"maps"
	"math/rand"
	"reflect"
	"slices"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

// Kinds declared only by name in InputSet.Kinds.
var (
	namespaceKind  = objects.GroupKind{Kind: "Namespace"}
	podKind        = objects.GroupKind{Kind: "Pod"}
	deploymentKind = objects.GroupKind{Group: "apps", Kind: "Deployment"}
)

// PerturbUndeclared returns in with every input set does not declare changed.
// Install, Platform, ProxyMode, Synced and TakenAt stay; ShiftNow moves Now.
func PerturbUndeclared(in health.Input, set health.InputSet, seed int64) health.Input {
	rng := rand.New(rand.NewSource(seed)) // nolint:gosec // seeded test data, not security
	out := in
	if in.Snapshot != nil {
		out.Snapshot = dropUndeclaredKinds(in, set)
	}
	if !set.Owned {
		out.Owned = nil
		out.Assignment = in.Assignment
		out.Assignment.PerInstall, out.Assignment.Ambiguous, out.Assignment.Unwatched = nil, nil, nil
	}
	if !set.Metrics {
		out.Metrics = ctrlmetrics.Result{CollectedAt: in.Now.Add(-time.Duration(1+rng.Intn(1000)) * time.Hour), Err: fmt.Sprintf("perturbed %d", seed)}
		out.HasMetrics = !in.HasMetrics
	}
	if !set.Verify {
		out.Verify = scopeverify.Result{Available: !in.Verify.Available, Reason: fmt.Sprintf("perturbed %d", seed),
			Mismatches: []scopeverify.Mismatch{{}}}
	}
	if !set.Computed {
		out.Computed = nil
		if in.Computed == nil {
			out.Computed = &computed.Views{RuleSet: fmt.Sprintf("perturbed %d", seed)}
		}
	}
	if !set.KindsChange {
		out.KindsChange = kinds.Change{Added: []string{fmt.Sprintf("perturbed%d", seed)}}
		if !in.KindsChange.Empty() {
			out.KindsChange = kinds.Change{}
		}
	}
	return out
}

// dropUndeclaredKinds copies in.Snapshot without the kinds set does not declare.
// Pods and Deployments need a Kinds entry; other kinds also pass with Owned.
func dropUndeclaredKinds(in health.Input, set health.InputSet) *cache.Snapshot {
	s := *in.Snapshot
	declared := func(gk objects.GroupKind) bool { return slices.Contains(set.Kinds, gk) }
	gks := map[schema.GroupVersionResource]objects.GroupKind{}
	for _, sv := range in.Served {
		gks[sv.GVR()] = objects.GroupKind{Group: sv.Group, Kind: sv.Kind}
	}
	keep := func(gvr schema.GroupVersionResource) bool {
		switch gvr {
		case cache.NamespacesGVR:
			return set.Owned || declared(namespaceKind)
		case cache.PodsGVR:
			return declared(podKind)
		case cache.DeploymentsGVR:
			return declared(deploymentKind)
		}
		gk, served := gks[gvr]
		return set.Owned || (served && declared(gk))
	}
	s.Objects = map[schema.GroupVersionResource][]*unstructured.Unstructured{}
	for gvr, objs := range in.Snapshot.Objects {
		if keep(gvr) {
			s.Objects[gvr] = objs
		}
	}
	s.Access = map[schema.GroupVersionResource]cache.Access{}
	for gvr, a := range in.Snapshot.Access {
		if keep(gvr) {
			s.Access[gvr] = a
		}
	}
	if !keep(cache.NamespacesGVR) {
		s.Namespaces = nil
	}
	if !keep(cache.PodsGVR) {
		s.Pods = nil
	}
	if !keep(cache.DeploymentsGVR) {
		s.Deployments = nil
	}
	return &s
}

// ShiftNow moves Now by a seeded amount in [0, next-Now), or [0, 10m) when next is zero.
func ShiftNow(in health.Input, next time.Time, seed int64) health.Input {
	window := 10 * time.Minute
	if !next.IsZero() {
		window = next.Sub(in.Now)
	}
	if window > 0 {
		in.Now = in.Now.Add(time.Duration(rand.New(rand.NewSource(seed)).Int63n(int64(window)))) // nolint:gosec // seeded test data
	}
	return in
}

// PerturbCollected replaces every collector key set does not declare and adds one.
func PerturbCollected(s collect.Snapshot, set health.InputSet, seed int64) collect.Snapshot {
	out := collect.Snapshot{Values: map[string]collect.Value{}}
	for k, v := range s.Values {
		if slices.Contains(set.Collected, k) {
			out.Values[k] = v
			continue
		}
		v.Data, v.State, v.Reason = fmt.Sprintf("perturbed %d", seed), collect.OK, ""
		out.Values[k] = v
	}
	maps.Copy(out.Values, map[string]collect.Value{
		fmt.Sprintf("perturbed/key%d", seed): {State: collect.OK, Data: seed},
	})
	return out
}

// CheckDeclaredInputs fails t when c's output changes under a perturbation of
// the inputs it does not declare. It names the check and the input.
func CheckDeclaredInputs(t *testing.T, c health.Check, in health.Input, rt health.Runtime, seed int64) {
	t.Helper()
	set := c.Inputs()
	checks := []health.Check{c}
	// One States for every run: the engine's first-seen since is not a check input.
	if rt.States == nil {
		rt.States = health.NewStates()
	}
	base := health.Evaluate(in, checks, rt)
	want := normalise(base)

	perturbed := map[string]health.Input{"undeclared kinds, metrics, verify, computed and kinds change": PerturbUndeclared(in, set, seed)}
	if !set.Clock {
		perturbed["Now"] = ShiftNow(in, base.Next, seed)
	}
	for what, p := range perturbed {
		prt := rt
		prt.Collected = PerturbCollected(rt.Collected, set, seed)
		got := evaluateRecovering(t, c, p, prt)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("check %q reads an input it does not declare (perturbed: %s, seed %d)\n declared: %+v\n before: %+v\n after:  %+v", c.Name(), what, seed, set, want, got)
		}
	}
}

// evaluateRecovering evaluates c and reports a panic as a failure naming c.
func evaluateRecovering(t *testing.T, c health.Check, in health.Input, rt health.Runtime) (out guardOutput) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("check %q panicked on perturbed undeclared input: %v", c.Name(), r)
		}
	}()
	return normalise(health.Evaluate(in, []health.Check{c}, rt))
}

// guardOutput is what the guard compares: the findings and the CheckResult
// with its CollectedAt timestamp zeroed (the engine restamps it on a memo hit).
type guardOutput struct {
	Findings []health.Finding
	Checks   []health.CheckResult
}

func normalise(r health.Report) guardOutput {
	out := guardOutput{Findings: r.Findings, Checks: append([]health.CheckResult(nil), r.Checks...)}
	for i := range out.Checks {
		out.Checks[i].CollectedAt = time.Time{}
	}
	return out
}
