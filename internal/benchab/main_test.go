package main

import (
	"strings"
	"testing"
)

// A log line splitting a result keeps its sample; header and BENCH lines are skipped.
func TestParse(t *testing.T) {
	in := `goos: linux
BenchmarkCompute/large-4   	      34	  32000000 ns/op	38034842 B/op	  130572 allocs/op
BenchmarkCompute/extreme-4   	2026/11/03 21:27:34 WARN delegation compute cap hit
       9	 112547662 ns/op	118446081 B/op	  435307 allocs/op
--- BENCH: BenchmarkCompute/extreme-4
    bench_test.go:80: measurement world: 103 Gateways
BenchmarkCompute/large-4   	      34	  34000000 ns/op	38034842 B/op	  130572 allocs/op
PASS
`
	got := parse(strings.NewReader(in))
	if len(got) != 2 || len(got["BenchmarkCompute/large"]) != 2 || got["BenchmarkCompute/extreme"][0] != 112547662 {
		t.Fatalf("parse = %v", got)
	}
}

// Head 20 % slower fails at 15 %; 10 % passes; head-only is new, base-only gone.
func TestCompare(t *testing.T) {
	base := map[string][]float64{"A": {100, 90, 110}, "B": {100, 100, 100}, "Gone": {1}}
	head := map[string][]float64{"A": {120, 119, 500}, "B": {110, 109, 111}, "New": {5}}
	rows, failed := compare(base, head, 0.15)
	want := map[string]string{"A": "slower", "B": "ok", "Gone": "gone", "New": "new"}
	if !failed || len(rows) != len(want) {
		t.Fatalf("failed %v, rows %+v", failed, rows)
	}
	for _, r := range rows {
		if r.verdict != want[r.name] {
			t.Errorf("%s: %s, want %s", r.name, r.verdict, want[r.name])
		}
	}
	if _, failed := compare(base, map[string][]float64{"B": {114}, "New": {1}}, 0.15); failed {
		t.Error("14 % slower and a new benchmark failed the gate")
	}
}
