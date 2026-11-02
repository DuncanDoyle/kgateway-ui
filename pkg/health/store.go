package health

import (
	"fmt"
	"slices"
	"time"

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
// Metrics, Verify, Computed, KindsChange and Clock.
func AllInputs() InputSet {
	return InputSet{Owned: true, Metrics: true, Verify: true, Computed: true, KindsChange: true, Clock: true}
}

// Store is a check's state across recomputes for one install. The engine owns
// it: it survives recomputes and is empty after a restart. It is not safe for
// concurrent use; the engine runs one check at a time. Reads of a nil Store
// find nothing; Set on a nil Store panics.
type Store struct{ m map[string]any }

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

// States holds one install's check Stores, keyed by check name.
type States struct{ stores map[string]*Store }

// NewStates returns States with no Stores.
func NewStates() *States { return &States{stores: map[string]*Store{}} }

// store returns check's Store, creating it on first use.
func (s *States) store(check string) *Store {
	st := s.stores[check]
	if st == nil {
		st = NewStore()
		s.stores[check] = st
	}
	return st
}

// Runtime is what Evaluate keeps across recomputes. Nil States gives each run
// fresh Stores (tests); nil Print turns memoization off.
type Runtime struct {
	States *States
	Print  func(InputSet) uint64
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

	sink    *runSink
	inputs  InputSet
	outputs map[string]CheckOutput
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
