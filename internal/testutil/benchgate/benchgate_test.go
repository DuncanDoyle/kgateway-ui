package benchgate

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// childEnv makes TestMiss miss a budget in this process.
const childEnv = "BENCHGATE_CHILD"

// TestMiss misses one budget in a child per mode: reference and unset fail it,
// report passes and logs it, and an unknown mode fails.
func TestMiss(t *testing.T) {
	if os.Getenv(childEnv) != "" {
		Timing(t, "warm median %d ms, budget ≤ %d ms", 151, 150)
		return
	}
	for _, c := range []struct {
		mode, out string
		fails     bool
	}{{"", "warm median 151 ms, budget ≤ 150 ms", true}, {"reference", "warm median 151 ms", true},
		{"report", "over budget, reported only (SNELLIUS_BENCH_TIMING=report): warm median 151 ms", false}, {"fast", `SNELLIUS_BENCH_TIMING="fast"`, true}} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMiss$", "-test.v")
		cmd.Env = append(os.Environ(), childEnv+"=1", Env+"="+c.mode)
		out, err := cmd.CombinedOutput()
		if (err != nil) != c.fails || !strings.Contains(string(out), c.out) {
			t.Errorf("mode %q: err %v, want failure %v and %q in:\n%s", c.mode, err, c.fails, c.out, out)
		}
	}
}
