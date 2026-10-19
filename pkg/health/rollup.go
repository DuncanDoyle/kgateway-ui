package health

import (
	"cmp"
	"slices"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

var sevRank = map[Severity]int{SeverityError: 0, SeverityWarning: 1, SeverityInfo: 2}

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
// does not reorder between polls.
func Evaluate(in Input, checks []Check) Report {
	r := Report{Install: in.Install.ID(), Counts: map[Severity]int{}, CollectedAt: in.Now, Findings: []Finding{}}
	for _, c := range checks {
		fs, cr := c.Run(in)
		r.Findings = append(r.Findings, fs...)
		r.Checks = append(r.Checks, cr)
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
		cmp.Compare(sevRank[a.Severity], sevRank[b.Severity]),
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

func compareParents(a, b objects.ParentRef) int {
	return cmp.Or(compareRefs(a.Ref(), b.Ref()), cmp.Compare(a.SectionName, b.SectionName), cmp.Compare(a.Port, b.Port))
}
