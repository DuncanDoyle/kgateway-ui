package health

import (
	"fmt"
	"slices"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// InputSet names what a check reads; the engine fingerprints exactly these.
// Install, Platform and ProxyMode are always inputs.
type InputSet struct {
	// Kinds are kinds read beyond Owned, such as core Pod and Namespace or apps Deployment.
	Kinds []objects.GroupKind
	// Owned is the install's assignment: every served kind and Namespaces.
	Owned bool
	// Metrics is the install's controller metrics result.
	Metrics bool
	// Verify is the scope verifier result.
	Verify bool
	// Computed are the install's computed views.
	Computed bool
	// KindsChange is how the served kinds differ from the watched ones.
	KindsChange bool
	// Process is process data.
	Process bool
	// Clock marks a check that reads Now beyond its WakeAt deadlines; it is never memoized.
	Clock bool
	// Checks are the earlier checks whose output Run reads through RunContext.Output.
	Checks []string
	// Collected are collector keys, "<collector>/<key>".
	Collected []string
}

// AllInputs declares every input a Phase 1 or 2 check may read: Owned,
// Metrics, Verify, Computed, KindsChange and Clock. It names no collector keys.
func AllInputs() InputSet {
	return InputSet{Owned: true, Metrics: true, Verify: true, Computed: true, KindsChange: true, Clock: true}
}

// Store is a check's state across recomputes for one install. The engine owns
// it: it survives recomputes and is empty after a restart. It is not safe for
// concurrent use; the engine runs one check at a time. Reads of a nil Store
// find nothing; Set on a nil Store panics.
type Store struct{ m map[string]any }

// MaxStampedCollected is how many declared collector keys can be restamp
// references; the engine never memoizes a check that declares more.
const MaxStampedCollected = 61

// NewStore returns an empty Store.
func NewStore() *Store { return &Store{m: map[string]any{}} }

// Get returns the value under key.
func (s *Store) Get(key string) (any, bool) {
	if s == nil {
		return nil, false
	}
	v, ok := s.m[key]
	return v, ok
}

// Set stores v under key.
func (s *Store) Set(key string, v any) {
	if s == nil {
		panic("health: Set on a nil Store; run checks through Evaluate or set RunContext.State")
	}
	s.m[key] = v
}

// Delete removes key.
func (s *Store) Delete(key string) {
	if s != nil {
		delete(s.m, key)
	}
}

// Len returns the number of keys.
func (s *Store) Len() int {
	if s == nil {
		return 0
	}
	return len(s.m)
}

// Load returns the value under key as a T; false when it is absent or not a T.
func Load[T any](s *Store, key string) (T, bool) {
	v, _ := s.Get(key)
	t, ok := v.(T)
	return t, ok
}

// States holds one install's check Stores and memos, keyed by check name.
type States struct {
	stores map[string]*Store
	memos  map[string]*memo
	// ids memoise each check's finding IDs; groups the install's group IDs.
	ids    map[string]*IDs
	groups *IDs
	merged merged
}

// merged is the last Evaluate's findings and the memo each check had after it.
type merged struct {
	memos    []*memo
	findings []Finding
}

// NewStates returns States with no Stores.
func NewStates() *States {
	return &States{stores: map[string]*Store{}, memos: map[string]*memo{}, ids: map[string]*IDs{}, groups: newIDs()}
}

// GroupIDs returns the install's memoised group IDs; nil for nil s.
func (s *States) GroupIDs() *IDs {
	if s == nil {
		return nil
	}
	return s.groups
}

// checkIDs returns check's memoised finding IDs, creating them on first use; nil for nil s.
func (s *States) checkIDs(check string) *IDs {
	if s == nil {
		return nil
	}
	x := s.ids[check]
	if x == nil {
		x = newIDs()
		s.ids[check] = x
	}
	return x
}

// memo is a check's last run, reused while its input print holds and its
// wake time is not due (design §16.2). Findings are shared and read-only.
type memo struct {
	print    uint64
	findings []Finding
	result   CheckResult
	wake     time.Time
	stamp    stamp
	// out is the output print without CollectedAt, set when a later check reads this one.
	out uint64
	// keep are the finding IDs the run returned or retained; their since entries survive a skip.
	keep map[string]bool
}

// stamp is the set of reference times (eachRef order) a CollectedAt equalled,
// so a reused result is restamped as a fresh run would stamp it.
type stamp uint64

// eachRef calls f with each reference time a check declaring set may stamp:
// Now, TakenAt, the metrics' and each declared collected value's time.
func eachRef(in Input, set InputSet, col collect.Snapshot, f func(bit stamp, t time.Time)) {
	f(1, in.Now)
	if in.Snapshot != nil {
		f(1<<1, in.Snapshot.TakenAt)
	}
	if set.Metrics && in.HasMetrics {
		f(1<<2, in.Metrics.CollectedAt)
	}
	for i, k := range set.Collected[:min(len(set.Collected), MaxStampedCollected)] {
		if v, ok := col.Get(k); ok {
			f(1<<(3+i), v.At)
		}
	}
}

// sameTime reports whether a and b are one instant in one location, so they serialise alike.
func sameTime(a, b time.Time) bool { return a.Equal(b) && a.Location() == b.Location() }

// stampOf returns the reference times that at equals.
func stampOf(in Input, set InputSet, col collect.Snapshot, at time.Time) stamp {
	var s stamp
	eachRef(in, set, col, func(bit stamp, t time.Time) {
		if sameTime(t, at) {
			s |= bit
		}
	})
	return s
}

// restamp returns the CollectedAt a fresh run would stamp, or old for an
// empty s; false when s's times are missing or now differ.
func (s stamp) restamp(in Input, set InputSet, col collect.Snapshot, old time.Time) (time.Time, bool) {
	if s == 0 {
		return old, true
	}
	var at time.Time
	var seen stamp
	same := true
	eachRef(in, set, col, func(bit stamp, t time.Time) {
		if s&bit == 0 {
			return
		}
		if seen != 0 && !sameTime(t, at) {
			same = false
		}
		at, seen = t, seen|bit
	})
	return at, same && seen == s
}

// store returns check's Store, creating it on first use.
func (s *States) store(check string) *Store {
	st := s.stores[check]
	if st == nil {
		st = NewStore()
		s.stores[check] = st
	}
	return st
}

// Runtime is what Evaluate keeps across recomputes; nil States gives fresh Stores.
// Print keys a check's memo (false: always run); nil Print turns memoization off.
type Runtime struct {
	States    *States
	Print     func(InputSet) (uint64, bool)
	Collected collect.Snapshot
}

// CheckOutput is what a check returned in this evaluation; its data is read-only.
type CheckOutput struct {
	Findings []Finding
	Result   CheckResult
}

// RunContext is what a check runs with: the install's Input and its own Store.
type RunContext struct {
	Input
	// State is engine-owned per (install, check); it survives recomputes and
	// is cleared on restart.
	State *Store

	sink      *runSink
	inputs    InputSet
	outputs   map[string]CheckOutput
	collected collect.Snapshot
}

// runSink collects what one Run reports besides its findings.
type runSink struct {
	wake     time.Time
	retained map[string]bool
}

// WakeAt says the check's output may change at at; the engine recomputes by
// then. A time not after Now is ignored.
func (c RunContext) WakeAt(at time.Time) {
	if c.sink == nil || !at.After(c.Now) {
		return
	}
	if c.sink.wake.IsZero() || at.Before(c.sink.wake) {
		c.sink.wake = at
	}
}

// Retain keeps the since entry of findingID although this run does not return it.
func (c RunContext) Retain(findingID string) {
	if c.sink == nil {
		return
	}
	if c.sink.retained == nil {
		c.sink.retained = map[string]bool{}
	}
	c.sink.retained[findingID] = true
}

// Output returns the output of check in this evaluation. It is false unless
// Inputs().Checks declares check and check ran before this one.
func (c RunContext) Output(check string) (CheckOutput, bool) {
	if !slices.Contains(c.inputs.Checks, check) {
		return CheckOutput{}, false
	}
	o, ok := c.outputs[check]
	return o, ok
}

// Collected returns the collector value under key, "<collector>/<key>". It is
// false unless Inputs().Collected declares key and the collector has published it.
// The value's Data is read-only.
func (c RunContext) Collected(key string) (collect.Value, bool) {
	if !slices.Contains(c.inputs.Collected, key) {
		return collect.Value{}, false
	}
	return c.collected.Get(key)
}

// ValidateOrder reports a check whose Inputs().Checks names a check not
// registered before it, and two checks with one name, which would share a Store.
func ValidateOrder(checks []Check) error {
	seen := make(map[string]bool, len(checks))
	for _, c := range checks {
		for _, dep := range c.Inputs().Checks {
			if !seen[dep] {
				return fmt.Errorf("check %q reads check %q, which is not registered before it", c.Name(), dep)
			}
		}
		if seen[c.Name()] {
			return fmt.Errorf("check name %q is registered twice", c.Name())
		}
		seen[c.Name()] = true
	}
	return nil
}
