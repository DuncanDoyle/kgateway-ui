package app

import (
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"

	"github.com/DuncanDoyle/kgateway-ui/internal/memlimit"
)

// MemoryLimitEnv holds the pod's memory limit in bytes; the chart sets it from limits.memory.
const MemoryLimitEnv = "KGWUI_MEMORY_LIMIT"

// ApplyMemoryLimit sets the Go soft memory limit to memlimit.Default, or to 90 % of
// the limit in MemoryLimitEnv when that is lower (D-025, D-043). An explicit
// GOMEMLIMIT wins; a missing, zero or unparsable pod limit is logged and leaves
// the default. It returns the limit set, or zero. Run calls it first; repeated calls are harmless.
func ApplyMemoryLimit() int64 {
	return applyMemoryLimit(os.Getenv, debug.SetMemoryLimit)
}

func applyMemoryLimit(getenv func(string) string, set func(int64) int64) int64 {
	if getenv("GOMEMLIMIT") != "" {
		return 0
	}
	target := memlimit.Default
	raw := getenv(MemoryLimitEnv)
	if limit, err := strconv.ParseInt(raw, 10, 64); err != nil || limit <= 0 {
		slog.Info("no usable pod memory limit", "env", MemoryLimitEnv, "value", raw)
	} else {
		target = min(target, limit/10*9)
	}
	set(target)
	slog.Info("Go memory limit set", "bytes", target, "podLimit", raw)
	return target
}
