package health

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

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
func Evaluate(in Input, checks []Check) Report {
	r := Report{Install: in.Install.ID(), Counts: map[Severity]int{}, CollectedAt: in.Now}
	// Run every check first so the findings are allocated once.
	runs := make([][]Finding, len(checks))
	n := 0
	for i, c := range checks {
		fs, cr := c.Run(in)
		runs[i] = fs
		n += len(fs)
		r.Checks = append(r.Checks, cr)
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
	slices.SortStableFunc(r.Findings, compareFindings)
	for _, f := range r.Findings {
		r.Counts[f.Severity]++
	}
	r.State = RollUp(r.Findings, r.Checks)
	return r
}

func compareFindings(a, b Finding) int {
	return cmp.Or(
		cmp.Compare(rank(a.Severity), rank(b.Severity)),
		cmp.Compare(a.Code, b.Code),
		compareOptRefs(a.Resource, b.Resource),
		compareParents(a.parent, b.parent),
		cmp.Compare(a.Message, b.Message),
		cmp.Compare(a.Detail, b.Detail),
	)
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
func compareRefs(a, b objects.Ref) int {
	return cmp.Or(cmp.Compare(a.Group, b.Group), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
}

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
