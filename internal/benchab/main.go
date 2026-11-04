// Command benchab is the timing regression gate of the bench workflow (D-044).
// It builds a package's bench test binary from a base and a head checkout, runs
// their benchmarks interleaved on one machine, and fails when a head median is
// more than -threshold slower than the base median. A benchmark only head has is "new".
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

func main() {
	base := flag.String("base", "", "base checkout directory (required)")
	head := flag.String("head", ".", "head checkout directory")
	pkg := flag.String("pkg", "./pkg/engine", "package whose benchmarks run")
	bench := flag.String("bench", ".", "-test.bench regexp")
	count := flag.Int("count", 5, "interleaved runs per side")
	threshold := flag.Float64("threshold", 0.15, "fail when head's median is more than this fraction slower")
	out := flag.String("out", "", "directory for the raw bench-ab-base.txt and bench-ab-head.txt (empty: none)")
	flag.Parse()
	if *base == "" {
		fmt.Fprintln(os.Stderr, "benchab: -base is required")
		os.Exit(2)
	}
	tmp, err := os.MkdirTemp("", "benchab")
	check(err)
	defer func() { _ = os.RemoveAll(tmp) }()
	sides := []*side{{name: "base", dir: *base}, {name: "head", dir: *head}}
	for _, s := range sides {
		s.bin = filepath.Join(tmp, s.name+".test")
		build := exec.Command("go", "test", "-c", "-tags", "bench", "-o", s.bin, *pkg) //nolint:gosec // flags from make bench-ab
		build.Dir, build.Stdout, build.Stderr = s.dir, os.Stderr, os.Stderr
		check(build.Run())
	}
	for i := range *count {
		// Alternate the order so drift over the job does not favour one side.
		order := sides
		if i%2 == 1 {
			order = []*side{sides[1], sides[0]}
		}
		for _, s := range order {
			fmt.Fprintf(os.Stderr, "benchab: run %d/%d %s\n", i+1, *count, s.name)
			run := exec.Command(s.bin, "-test.run", "^$", "-test.bench", *bench, "-test.benchmem", "-test.count", "1") //nolint:gosec // the binary just built
			run.Dir, run.Stderr = filepath.Join(s.dir, *pkg), os.Stderr
			o, err := run.Output()
			s.raw.Write(o)
			check(err)
		}
	}
	if *out != "" {
		for _, s := range sides {
			check(os.WriteFile(filepath.Join(*out, "bench-ab-"+s.name+".txt"), s.raw.Bytes(), 0o600))
		}
	}
	rows, failed := compare(parse(&sides[0].raw), parse(&sides[1].raw), *threshold)
	fmt.Print(report(rows, *threshold))
	if failed {
		_ = os.RemoveAll(tmp)
		os.Exit(1)
	}
}

type side struct {
	name, dir, bin string
	raw            bytes.Buffer
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "benchab:", err)
		os.Exit(2)
	}
}

// procs is the -GOMAXPROCS suffix of a benchmark name.
var procs = regexp.MustCompile(`-\d+$`)

// parse returns the ns/op samples per benchmark name in Go benchmark output. A
// log line can split a result line, so a name with no result waits for the next numbers.
func parse(r io.Reader) map[string][]float64 {
	out := map[string][]float64{}
	pending := ""
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		name := pending
		if strings.HasPrefix(f[0], "Benchmark") {
			name, pending, f = procs.ReplaceAllString(f[0], ""), procs.ReplaceAllString(f[0], ""), f[1:]
		}
		if name == "" || len(f) < 3 {
			continue
		}
		if _, err := strconv.Atoi(f[0]); err != nil {
			continue
		}
		for i := 1; i+1 < len(f); i += 2 {
			if f[i+1] == "ns/op" {
				if v, err := strconv.ParseFloat(f[i], 64); err == nil {
					out[name] = append(out[name], v)
					pending = ""
				}
			}
		}
	}
	return out
}

// row is one benchmark's verdict.
type row struct {
	name       string
	base, head float64 // medians in ns/op; base 0 when absent
	verdict    string  // ok, slower, new or gone
}

// compare matches head's benchmarks to base's; failed is true when one is slower than threshold.
func compare(base, head map[string][]float64, threshold float64) (rows []row, failed bool) {
	for name, h := range head {
		r := row{name: name, head: median(h), verdict: "new"}
		if b, ok := base[name]; ok {
			r.base, r.verdict = median(b), "ok"
			if r.head > r.base*(1+threshold) {
				r.verdict, failed = "slower", true
			}
		}
		rows = append(rows, r)
	}
	for name, b := range base {
		if _, ok := head[name]; !ok {
			rows = append(rows, row{name: name, base: median(b), verdict: "gone"})
		}
	}
	slices.SortFunc(rows, func(a, b row) int { return strings.Compare(a.name, b.name) })
	return rows, failed
}

func median(v []float64) float64 {
	s := slices.Sorted(slices.Values(v))
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// report is the gate's table, one row per benchmark.
func report(rows []row, threshold float64) string {
	lines := []string{fmt.Sprintf("timing regression gate: head median vs base median, fails above +%.0f %%", 100*threshold),
		fmt.Sprintf("%-72s %14s %14s %8s  %s", "benchmark", "base ns/op", "head ns/op", "delta", "verdict")}
	for _, r := range rows {
		delta, b, h := "", "-", "-"
		if r.base > 0 {
			b = strconv.FormatFloat(r.base, 'f', 0, 64)
		}
		if r.head > 0 {
			h = strconv.FormatFloat(r.head, 'f', 0, 64)
		}
		if r.base > 0 && r.head > 0 {
			delta = fmt.Sprintf("%+.1f%%", 100*(r.head/r.base-1))
		}
		lines = append(lines, fmt.Sprintf("%-72s %14s %14s %8s  %s", r.name, b, h, delta, r.verdict))
	}
	return strings.Join(lines, "\n") + "\n"
}
