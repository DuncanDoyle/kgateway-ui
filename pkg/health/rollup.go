package health

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

var sevRank = map[Severity]int{SeverityError: 0, SeverityWarning: 1, SeverityInfo: 2}

// rank orders severities; one outside the known set ranks last.
func rank(s Severity) int {
	if r, ok := sevRank[s]; ok {
		return r
	}
	return len(sevRank)
}

// RollUp computes the state: error > unknown > warning > healthy (design §5.3).
func RollUp(fs []Finding, cs []CheckResult) State {
	for _, f := range fs {
		if f.Severity == SeverityError {
			return StateError
		}
	}
	for _, c := range cs {
		if c.Required && !c.OK {
			return StateUnknown
		}
	}
	for _, f := range fs {
		if f.Severity == SeverityWarning {
			return StateWarning
		}
	}
	return StateHealthy
}

// Evaluate runs checks and assembles a Report. Findings are in a fixed
// order (severity, code, resource, parentRef, message, detail), so a list
// does not reorder between polls. A finding with a severity outside error,
// warning and info becomes a warning whose detail names the original, so a
// check can neither hide a problem from the roll-up nor invent a severity.
// Checks run in order, each with its Store from rt.States; a grace read
// inside the grace period wakes the report at the grace end (Report.Next).
func Evaluate(in Input, checks []Check, rt Runtime) Report {
	r := Report{Install: in.Install.ID(), Counts: map[Severity]int{}, CollectedAt: in.Now}
	var outputs map[string]CheckOutput
	inputs := make([]InputSet, len(checks))
	for i, c := range checks {
		if inputs[i] = c.Inputs(); len(inputs[i].Checks) > 0 && outputs == nil {
			outputs = make(map[string]CheckOutput, len(checks))
		}
	}
	// Run every check first so the findings are allocated once.
	runs := make([][]Finding, len(checks))
	n := 0
	for i, c := range checks {
		ctx := runContext(in, c, inputs[i], rt, outputs)
		fs, cr := c.Run(ctx)
		runs[i] = fs
		n += len(fs)
		r.Checks = append(r.Checks, cr)
		if outputs != nil {
			outputs[c.Name()] = CheckOutput{Findings: fs, Result: cr}
		}
		if w := ctx.sink.wake; !w.IsZero() && (r.Next.IsZero() || w.Before(r.Next)) {
			r.Next = w
		}
	}
	r.Findings = make([]Finding, 0, n)
	for _, fs := range runs {
		for _, f := range fs {
			if _, known := sevRank[f.Severity]; !known {
				note := fmt.Sprintf("unrecognised severity %q", f.Severity)
				f.Detail = strings.TrimSuffix(note+": "+f.Detail, ": ")
				f.Severity = SeverityWarning
			}
			r.Findings = append(r.Findings, f)
		}
	}
	r.Findings = sortFindings(r.Findings)
	for _, f := range r.Findings {
		r.Counts[f.Severity]++
	}
	r.State = RollUp(r.Findings, r.Checks)
	return r
}

// runContext returns c's RunContext: its Store, a fresh sink, and in with
// FirstSeen wrapped so a read inside the grace period wakes at its end.
func runContext(in Input, c Check, inputs InputSet, rt Runtime, outputs map[string]CheckOutput) RunContext {
	var st *Store
	if rt.States != nil {
		st = rt.States.store(c.Name())
	} else {
		st = NewStore()
	}
	ctx := RunContext{Input: in, State: st, sink: &runSink{}, inputs: inputs, outputs: outputs}
	sink, base := ctx.sink, in
	ctx.FirstSeen = func(u *unstructured.Unstructured) time.Time {
		t := base.firstSeen(u)
		if end := t.Add(Grace); in.Now.Before(end) { // in grace while now − t < Grace
			if sink.wake.IsZero() || end.Before(sink.wake) {
				sink.wake = end
			}
		}
		return t
	}
	return ctx
}

// sortFindings returns fs sorted stably by compareFindings. It sorts
// pointers, as moving a whole Finding per swap dominated a large report's sort.
func sortFindings(fs []Finding) []Finding {
	ps := make([]*Finding, len(fs))
	for i := range fs {
		ps[i] = &fs[i]
	}
	slices.SortStableFunc(ps, func(a, b *Finding) int { return compareFindings(*a, *b) })
	sorted := make([]Finding, len(fs))
	for i, p := range ps {
		sorted[i] = *p
	}
	return sorted
}

// compareFindings orders by severity, code, resource, parentRef, message and
// detail. Each key is compared only when the earlier ones tie.
func compareFindings(a, b Finding) int {
	if a.Severity != b.Severity {
		if c := cmp.Compare(rank(a.Severity), rank(b.Severity)); c != 0 {
			return c
		}
	}
	if c := cmp.Compare(a.Code, b.Code); c != 0 {
		return c
	}
	if c := compareOptRefs(a.Resource, b.Resource); c != 0 {
		return c
	}
	if a.parent != nil || b.parent != nil {
		if c := compareParents(a.parent, b.parent); c != 0 {
			return c
		}
	}
	if c := cmp.Compare(a.Message, b.Message); c != 0 {
		return c
	}
	return cmp.Compare(a.Detail, b.Detail)
}

// compareOptRefs orders findings without a resource first.
func compareOptRefs(a, b *objects.Ref) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	return compareRefs(*a, *b)
}

// compareRefs orders by group, kind, namespace and name.
func compareRefs(a, b objects.Ref) int { return a.Compare(b) }

func compareParents(pa, pb *objects.ParentRef) int {
	var a, b objects.ParentRef
	if pa != nil {
		a = *pa
	}
	if pb != nil {
		b = *pb
	}
	return cmp.Or(compareRefs(a.Ref(), b.Ref()), cmp.Compare(a.SectionName, b.SectionName), cmp.Compare(a.Port, b.Port))
}
