package health_test

import (
	"fmt"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/healthtest"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
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
	}
}
