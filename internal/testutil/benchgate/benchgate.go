// Package benchgate decides how a bench budget fails (D-044). Deterministic
// budgets fail everywhere; absolute time budgets fail only on the reference machine.
package benchgate

import (
	"fmt"
	"os"
	"testing"
)

// Env selects the timing mode: "reference" (the default) gates, "report" logs.
const Env = "KGWUI_BENCH_TIMING"

// Reference reports whether absolute time budgets gate in this run.
func Reference(tb testing.TB) bool {
	tb.Helper()
	switch v := os.Getenv(Env); v {
	case "", "reference":
		return true
	case "report":
		return false
	default:
		tb.Fatalf("%s=%q, want reference or report", Env, v)
		return true
	}
}

// Timing fails tb on the reference machine and logs the miss in report mode.
func Timing(tb testing.TB, format string, args ...any) {
	tb.Helper()
	if Reference(tb) {
		tb.Errorf(format, args...)
		return
	}
	tb.Logf("over budget, reported only (%s=report): %s", Env, fmt.Sprintf(format, args...))
}
