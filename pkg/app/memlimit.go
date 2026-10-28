package app

import (
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"
)

// MemoryLimitEnv holds the pod's memory limit in bytes; the chart sets it from limits.memory.
const MemoryLimitEnv = "KGWUI_MEMORY_LIMIT"

// ApplyMemoryLimit sets the Go memory target to 90 % of the limit in
// MemoryLimitEnv (D-025). An explicit GOMEMLIMIT wins; a missing, zero or
// unparsable limit is ignored and logged. It returns the target set, or zero.
// Run calls it first; repeated calls are harmless.
func ApplyMemoryLimit() int64 {
	return applyMemoryLimit(os.Getenv, debug.SetMemoryLimit)
}

func applyMemoryLimit(getenv func(string) string, set func(int64) int64) int64 {
	if getenv("GOMEMLIMIT") != "" {
		return 0
	}
	raw := getenv(MemoryLimitEnv)
	limit, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || limit <= 0 {
		slog.Info("no usable memory limit; keeping the Go default", "env", MemoryLimitEnv, "value", raw)
		return 0
	}
	target := limit / 10 * 9
	set(target)
	slog.Info("Go memory target set", "bytes", target, "podLimit", limit)
	return target
}
