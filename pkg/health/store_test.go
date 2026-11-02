package health

import (
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// funcCheck is a check whose Run and Inputs are funcs.
type funcCheck struct {
	name   string
	inputs InputSet
	run    func(ctx RunContext) ([]Finding, CheckResult)
}

func (c funcCheck) Name() string     { return c.name }
func (c funcCheck) Inputs() InputSet { return c.inputs }
func (c funcCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	if c.run == nil {
		return nil, CheckResult{Name: c.name, OK: true}
	}
	return c.run(ctx)
}

func TestStoreGetSetDeleteLoad(t *testing.T) {
	s := NewStore()
	if _, ok := s.Get("k"); ok || s.Len() != 0 {
		t.Fatal("a new Store must be empty")
	}
	s.Set("k", 3)
	if v, ok := Load[int](s, "k"); !ok || v != 3 || s.Len() != 1 {
		t.Fatalf("Load = %v, %v; Len %d", v, ok, s.Len())
	}
	if _, ok := Load[string](s, "k"); ok {
		t.Fatal("Load of the wrong type must report false")
	}
	s.Delete("k")
	if _, ok := s.Get("k"); ok || s.Len() != 0 {
		t.Fatal("Delete must remove the key")
	}
}

// D-032: a check reads and writes only its own Store, even under the same key.
func TestChecksCannotReadEachOthersStore(t *testing.T) {
	var read []bool
	writer := funcCheck{name: "writer", run: func(ctx RunContext) ([]Finding, CheckResult) {
		ctx.State.Set("secret", "mine")
		return nil, CheckResult{Name: "writer", OK: true}
	}}
	reader := funcCheck{name: "reader", run: func(ctx RunContext) ([]Finding, CheckResult) {
		_, ok := ctx.State.Get("secret")
		read = append(read, ok)
		return nil, CheckResult{Name: "reader", OK: true}
	}}
	states := NewStates()
	for range 2 {
		Evaluate(Input{}, []Check{writer, reader}, Runtime{States: states})
	}
	if len(read) != 2 || read[0] || read[1] {
		t.Fatalf("reader saw writer's Store: %v", read)
	}
}

// Output returns only an earlier check's output that Inputs().Checks declares;
// ValidateOrder rejects a declared check that is later or missing.
func TestOutputRequiresDeclaredEarlierCheck(t *testing.T) {
	first := funcCheck{name: "first", run: func(ctx RunContext) ([]Finding, CheckResult) {
		return []Finding{{Code: "ACME-001", Severity: SeverityInfo, Message: "m"}}, CheckResult{Name: "first", OK: true}
	}}
	var declared, undeclared, later bool
	var got CheckOutput
	reads := funcCheck{name: "reads", inputs: InputSet{Checks: []string{"first"}}, run: func(ctx RunContext) ([]Finding, CheckResult) {
		got, declared = ctx.Output("first")
		_, later = ctx.Output("last")
		return nil, CheckResult{Name: "reads", OK: true}
	}}
	peeks := funcCheck{name: "peeks", run: func(ctx RunContext) ([]Finding, CheckResult) {
		_, undeclared = ctx.Output("first")
		return nil, CheckResult{Name: "peeks", OK: true}
	}}
	last := funcCheck{name: "last"}
	checks := []Check{first, reads, peeks, last}
	if err := ValidateOrder(checks); err != nil {
		t.Fatalf("a declared earlier check is valid: %v", err)
	}
	Evaluate(Input{}, checks, Runtime{})
	if !declared || len(got.Findings) != 1 || got.Findings[0].Code != "ACME-001" || got.Result.Name != "first" {
		t.Fatalf("declared earlier output: %v %+v", declared, got)
	}
	if undeclared {
		t.Fatal("an undeclared check's output must not be readable")
	}
	if later {
		t.Fatal("a check not declared must not be readable, even when registered later")
	}

	backward := funcCheck{name: "backward", inputs: InputSet{Checks: []string{"last"}}}
	if err := ValidateOrder([]Check{first, backward, last}); err == nil || !strings.Contains(err.Error(), `"last"`) {
		t.Fatalf("a declared later check must fail by name, got %v", err)
	}
	missing := funcCheck{name: "missing", inputs: InputSet{Checks: []string{"nowhere"}}}
	if err := ValidateOrder([]Check{missing}); err == nil || !strings.Contains(err.Error(), `"nowhere"`) {
		t.Fatalf("a declared unregistered check must fail by name, got %v", err)
	}
	if err := ValidateOrder([]Check{first, funcCheck{name: "first"}}); err == nil {
		t.Fatal("two checks with one name would share a Store; ValidateOrder must reject them")
	}
	if err := ValidateOrder(Upstream()); err != nil {
		t.Fatalf("Upstream must validate: %v", err)
	}
}

// A grace read inside the grace period wakes the report at its end; WakeAt
// keeps the earliest future time.
func TestReportNextIsEarliestWake(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seen := now.Add(-10 * time.Second)
	u := &unstructured.Unstructured{Object: map[string]any{}}
	grace := funcCheck{name: "grace", run: func(ctx RunContext) ([]Finding, CheckResult) {
		ctx.firstSeen(u)
		return nil, CheckResult{Name: "grace", OK: true}
	}}
	wakes := funcCheck{name: "wakes", run: func(ctx RunContext) ([]Finding, CheckResult) {
		ctx.WakeAt(now.Add(-time.Second)) // already due: ignored
		ctx.WakeAt(now.Add(time.Hour))
		return nil, CheckResult{Name: "wakes", OK: true}
	}}
	in := Input{Now: now, FirstSeen: func(*unstructured.Unstructured) time.Time { return seen }}
	if r := Evaluate(in, []Check{wakes, grace}, Runtime{}); !r.Next.Equal(seen.Add(Grace)) {
		t.Fatalf("Next = %v, want the grace end %v", r.Next, seen.Add(Grace))
	}
	if r := Evaluate(in, []Check{wakes}, Runtime{}); !r.Next.Equal(now.Add(time.Hour)) {
		t.Fatalf("Next = %v, want %v", r.Next, now.Add(time.Hour))
	}
	in.FirstSeen = func(*unstructured.Unstructured) time.Time { return now.Add(-Grace) }
	if r := Evaluate(in, []Check{grace}, Runtime{}); !r.Next.IsZero() {
		t.Fatalf("a read past grace must not wake, got %v", r.Next)
	}
}

// A RunContext built without Evaluate (a downstream unit test) is safe to use.
func TestZeroRunContextIsSafe(t *testing.T) {
	var c RunContext
	c.WakeAt(time.Now())
	c.Retain("f1-x")
	if _, ok := c.Output("x"); ok {
		t.Fatal("a zero RunContext has no outputs")
	}
}
