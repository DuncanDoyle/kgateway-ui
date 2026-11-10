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
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/process"
	"github.com/DuncanDoyle/snellius/pkg/scopeverify"
)

// Kinds declared only by name in InputSet.Kinds.
var (
	namespaceKind  = objects.GroupKind{Kind: "Namespace"}
	podKind        = objects.GroupKind{Kind: "Pod"}
	deploymentKind = objects.GroupKind{Group: "apps", Kind: "Deployment"}
)

// PerturbUndeclared returns in with every input set does not declare changed;
// an undeclared optional watch gets other data and a failing access.
// Install, Platform, ProxyMode, Synced and TakenAt stay; ShiftNow moves Now.
func PerturbUndeclared(in health.Input, set health.InputSet, seed int64) health.Input {
	out := in
	if in.Snapshot != nil {
		out.Snapshot = dropUndeclaredKinds(in, set)
		perturbOptional(out.Snapshot, in.Snapshot, set, seed)
	}
	if !set.Owned {
		out.Owned = nil
		out.Assignment = in.Assignment
		out.Assignment.PerInstall, out.Assignment.Ambiguous, out.Assignment.Unwatched = nil, nil, nil
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
	if !set.Process {
		out.Process = perturbedProcess(in.Process, seed)
	}
	if !set.KindsChange {
		out.KindsChange = kinds.Change{Added: []string{fmt.Sprintf("perturbed%d", seed)}}
		if !in.KindsChange.Empty() {
			out.KindsChange = kinds.Change{}
		}
	}
	if !set.Catalog {
		// Another catalog: one that knows every group, serving only a perturbed kind.
		out.Catalog = kinds.NewCatalog([]schema.GroupKind{{Group: fmt.Sprintf("perturbed%d.example", seed), Kind: "Service"}}, nil)
	}
	return out
}

// perturbedProcess is other process data than d: d's Gateways and the
// controller, each observed with one restarted, failing process.
func perturbedProcess(d *health.ProcessData, seed int64) *health.ProcessData {
	n, ready := int32(seed%1000)+5, false //nolint:gosec // bounded seed
	set := func(name string) process.Set {
		return process.Set{Observed: true, Desired: &n, Rollout: &process.Rollout{Reason: "ProgressDeadlineExceeded"},
			Processes: []process.Process{{ID: name, Name: name, Ready: &ready, Restarts: &n, Waiting: "CrashLoopBackOff", Version: "0.0.1",
				Termination: &process.Termination{Reason: "OOMKilled", ExitCode: 2, At: time.Unix(seed, 0)},
				Probe:       &process.ProbeFailures{Count: int(n), First: time.Unix(seed, 0), Last: time.Unix(seed, 0), HistoryUnknown: true}}}}
	}
	out := &health.ProcessData{Controller: set(fmt.Sprintf("perturbed-%d", seed)), Proxies: map[objects.Ref]process.Set{}, Print: uint64(seed)} //nolint:gosec // seeded test data
	if d != nil {
		for r := range d.Proxies {
			out.Proxies[r] = set(fmt.Sprintf("perturbed-%d-%s", seed, r.Name))
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
		case cache.ProxyPodsGVR:
			return declared(cache.ProxyPodsKind)
		case cache.EventsGVR:
			return declared(cache.EventsKind)
		case cache.CRDsGVR:
			return declared(cache.CRDsKind)
		case cache.ServicesGVR:
			return declared(cache.ServicesKind)
		case cache.EndpointSlicesGVR:
			return declared(cache.EndpointSlicesKind)
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

// perturbOptional replaces what out holds of each optional watch set does not
// declare: its data changes and its access reads as failing.
func perturbOptional(out, in *cache.Snapshot, set health.InputSet, seed int64) {
	why := cache.Access{Err: fmt.Sprintf("perturbed %d", seed)}
	uid := types.UID(fmt.Sprintf("perturbed-%d", seed))
	// A failing kind no check declares, as a later optional watch would add.
	out.Access[schema.GroupVersionResource{Group: "perturbed.example", Version: "v1", Resource: fmt.Sprintf("widgets%d", seed)}] = why
	if !slices.Contains(set.Kinds, cache.ProxyPodsKind) {
		out.Access[cache.ProxyPodsGVR] = why
		out.ProxyPods = nil
		if len(in.ProxyPods) == 0 {
			out.ProxyPods = in.Pods
		}
	}
	if !slices.Contains(set.Kinds, cache.EventsKind) {
		out.Access[cache.EventsGVR] = why
		out.Events = []cache.EventFact{{UID: uid, PodUID: uid, Reason: "Unhealthy", Count: int32(seed%1000) + 1}} //nolint:gosec // bounded seed
		out.EventDeltas = map[types.UID][]cache.Observation{uid: {{At: time.Unix(seed, 0), Reason: "Unhealthy", Delta: 3}}}
		out.EventHistoryUnknown = map[types.UID]bool{uid: true}
		out.EventsCapped = !in.EventsCapped
	}
	if !slices.Contains(set.Kinds, cache.CRDsKind) {
		out.Access[cache.CRDsGVR] = why
		out.CRDs = []cache.CRDMeta{{Name: fmt.Sprintf("perturbed%d.gateway.networking.k8s.io", seed), Group: "gateway.networking.k8s.io", BundleVersion: "v0.0.1"}}
	}
	if !slices.Contains(set.Kinds, cache.ServicesKind) {
		out.Access[cache.ServicesGVR] = why
		out.Services = nil
		if len(in.Services) == 0 {
			out.Services = []cache.ServiceFact{{Namespace: "perturbed", Name: fmt.Sprintf("svc%d", seed), Ports: []cache.ServicePort{{Port: 80}}}}
		}
	}
	if !slices.Contains(set.Kinds, cache.EndpointSlicesKind) {
		out.Access[cache.EndpointSlicesGVR] = why
		// Ready flips between none and some, so a read of either shows.
		out.EndpointSlices = []cache.SliceFact{{Namespace: "perturbed", Service: fmt.Sprintf("svc%d", seed), Ports: []cache.SlicePort{{Port: 80}}, Ready: []int{1}, Total: 1}}
		for _, s := range in.EndpointSlices {
			ready := make([]int, len(s.Ready))
			for i, n := range s.Ready {
				ready[i] = 1 - min(n, 1)
			}
			s.Ready = ready
			out.EndpointSlices = append(out.EndpointSlices, s)
		}
	}
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
// Each run starts from fresh States seeded with the same first-seen times, warmed by one run at
// in.Now for the Now shift; rt.States is unused.
func CheckDeclaredInputs(t testing.TB, c health.Check, in health.Input, rt health.Runtime, seed int64) {
	t.Helper()
	set := c.Inputs()
	checks := []health.Check{c}
	rt.States = health.NewStates()
	seen := in.Now.Add(-time.Hour)
	var ids []string
	for _, f := range health.Evaluate(in, checks, rt).Findings {
		ids = append(ids, f.ID)
	}
	fresh := func() *health.States {
		st := health.NewStates()
		for _, id := range ids {
			st.SeedSince(c.Name(), id, seen)
		}
		return st
	}
	rt.States = fresh()
	base := health.Evaluate(in, checks, rt)
	want := normalise(base)

	perturbed := map[string]health.Input{"undeclared kinds, metrics, verify, computed and kinds change": PerturbUndeclared(in, set, seed)}
	if !set.Clock {
		perturbed["Now"] = ShiftNow(in, base.Next, seed)
	}
	// warmed is fresh States that already ran in once, so a hold keeps the start it took then.
	warmed := func() *health.States {
		st := fresh()
		wrt := rt
		wrt.States = st
		health.Evaluate(in, checks, wrt)
		return st
	}
	for what, p := range perturbed {
		prt := rt
		prt.States = fresh()
		prt.Collected = PerturbCollected(rt.Collected, set, seed)
		want := want
		if what == "Now" {
			// A later Now before the next wake on a warmed Store changes nothing, a hold's until included.
			wrt := rt
			wrt.States = warmed()
			want = normalise(health.Evaluate(in, checks, wrt))
			prt.States = warmed()
		}
		got := evaluateRecovering(t, c, p, prt)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("check %q reads an input it does not declare (perturbed: %s, seed %d)\n declared: %+v\n before: %+v\n after:  %+v", c.Name(), what, seed, set, want, got)
		}
	}
}

// evaluateRecovering evaluates c and reports a panic as a failure naming c.
func evaluateRecovering(t testing.TB, c health.Check, in health.Input, rt health.Runtime) (out guardOutput) {
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
