package healthtest

import (
	"fmt"
	"testing"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/health"
)

// storeCheck runs run with its Store; it declares no inputs.
type storeCheck struct {
	run func(ctx health.RunContext) string
}

func (storeCheck) Name() string            { return "store check" }
func (storeCheck) Inputs() health.InputSet { return health.InputSet{} }
func (c storeCheck) Run(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
	return []health.Finding{{Code: "ACME-001", Severity: health.SeverityInfo, Message: "m", Detail: c.run(ctx)}},
		health.CheckResult{Name: "store check", OK: true}
}

// recorder is a testing.TB that records failures instead of failing.
type recorder struct {
	testing.TB
	failed bool
}

func (r *recorder) Errorf(string, ...any) { r.failed = true }

// guardFails reports whether the guard fails c over an empty world.
func guardFails(t *testing.T, c health.Check) bool {
	r := &recorder{TB: t}
	CheckDeclaredInputs(r, c, health.Input{Now: testutil.Now}, health.Runtime{}, 1)
	return r.failed
}

// A check that stores an undeclared read and replays it fails the guard:
// each evaluation starts from a fresh Store.
func TestGuardCatchesReplayedUndeclaredRead(t *testing.T) {
	replay := storeCheck{func(ctx health.RunContext) string {
		if v, ok := health.Load[string](ctx.State, "verify"); ok {
			return v
		}
		v := fmt.Sprint(ctx.Verify.Available, ctx.Verify.Reason)
		ctx.State.Set("verify", v)
		return v
	}}
	if !guardFails(t, replay) {
		t.Fatal("a check replaying an undeclared verify read passed the guard")
	}
}

// A check whose output follows its own Store passes: every evaluation starts from the same state.
func TestGuardAllowsStatefulCheck(t *testing.T) {
	counter := storeCheck{func(ctx health.RunContext) string {
		n, _ := health.Load[int](ctx.State, "runs")
		ctx.State.Set("runs", n+1)
		return fmt.Sprint(n + 1)
	}}
	if guardFails(t, counter) {
		t.Fatal("a check counting its own runs failed the guard")
	}
}
