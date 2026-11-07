package health_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/DuncanDoyle/snellius/internal/testutil/healthtest"
	"github.com/DuncanDoyle/snellius/pkg/health"
)

// guardSeeds are the seeds the guard perturbs with.
var guardSeeds = []int64{1, 2, 3, 7, 42}

// TestChecksReadOnlyDeclaredInputs runs each upstream check twice over every
// fixture world, the second time with every input it does not declare
// perturbed. The findings and CheckResult must not change (D-032).
func TestChecksReadOnlyDeclaredInputs(t *testing.T) {
	worlds := health.GuardWorlds(t)
	for _, c := range health.Upstream() {
		for name, in := range worlds {
			for _, seed := range guardSeeds {
				t.Run(fmt.Sprintf("%s/%s/seed%d", c.Name(), name, seed), func(t *testing.T) { healthtest.CheckDeclaredInputs(t, c, in, health.Runtime{}, seed) })
			}
		}
		for name, w := range health.GuardCollectedWorlds(t) {
			for _, seed := range guardSeeds {
				t.Run(fmt.Sprintf("%s/%s/seed%d", c.Name(), name, seed), func(t *testing.T) { healthtest.CheckDeclaredInputs(t, c, w.In, w.RT, seed) })
			}
		}
	}
}

// TestChecksReadOnlyDeclaredInputsAfterAMinute runs the guard on a Store that
// saw the same inputs 61 s earlier, so matured conditions show.
func TestChecksReadOnlyDeclaredInputsAfterAMinute(t *testing.T) {
	for _, c := range health.Upstream() {
		set := c.Inputs()
		for name, w := range health.GuardCollectedWorlds(t) {
			for _, seed := range guardSeeds {
				t.Run(fmt.Sprintf("%s/%s/seed%d", c.Name(), name, seed), func(t *testing.T) {
					run := func(perturb bool) ([]health.Finding, []health.CheckResult) {
						early, late, rt := w.In, w.In, w.RT
						early.Now = late.Now.Add(-61 * time.Second)
						if perturb {
							early, late = healthtest.PerturbUndeclared(early, set, seed), healthtest.PerturbUndeclared(late, set, seed)
							rt.Collected = healthtest.PerturbCollected(rt.Collected, set, seed)
						}
						rt.States = health.NewStates()
						health.Evaluate(early, []health.Check{c}, rt)
						r := health.Evaluate(late, []health.Check{c}, rt)
						for i := range r.Checks {
							r.Checks[i].CollectedAt = time.Time{}
						}
						return r.Findings, r.Checks
					}
					fs, cs := run(false)
					pfs, pcs := run(true)
					if !reflect.DeepEqual(fs, pfs) || !reflect.DeepEqual(cs, pcs) {
						t.Errorf("check %q reads an input it does not declare (seed %d)\n before: %+v %+v\n after:  %+v %+v", c.Name(), seed, fs, cs, pfs, pcs)
					}
				})
			}
		}
	}
}
