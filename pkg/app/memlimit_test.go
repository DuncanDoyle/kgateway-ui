package app

import "testing"

func TestApplyMemoryLimit(t *testing.T) {
	const gi = 1 << 30
	for _, tc := range []struct {
		name string
		env  map[string]string
		want int64
	}{
		{"90 percent of the limit", map[string]string{MemoryLimitEnv: "536870912"}, 536870912 / 10 * 9},
		{"explicit GOMEMLIMIT wins", map[string]string{MemoryLimitEnv: "536870912", "GOMEMLIMIT": "1GiB"}, 0},
		{"missing", map[string]string{}, 0},
		{"zero", map[string]string{MemoryLimitEnv: "0"}, 0},
		{"negative", map[string]string{MemoryLimitEnv: "-5"}, 0},
		{"unparsable", map[string]string{MemoryLimitEnv: "512Mi"}, 0},
		{"large", map[string]string{MemoryLimitEnv: "8589934592"}, 8 * gi / 10 * 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var set int64
			got := applyMemoryLimit(func(k string) string { return tc.env[k] }, func(n int64) int64 { set = n; return 0 })
			if got != tc.want || set != tc.want {
				t.Errorf("target %d, set %d, want %d", got, set, tc.want)
			}
		})
	}
}
