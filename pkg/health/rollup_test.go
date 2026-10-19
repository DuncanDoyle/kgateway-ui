package health

import "testing"

func TestRollUpOrder(t *testing.T) {
	warn := Finding{Severity: SeverityWarning}
	errF := Finding{Severity: SeverityError}
	info := Finding{Severity: SeverityInfo}
	missing := CheckResult{Name: "metrics", Required: true, OK: false}
	optional := CheckResult{Name: "verify", Required: false, OK: false}
	cases := []struct {
		name string
		fs   []Finding
		cs   []CheckResult
		want State
	}{
		{"nothing is healthy", nil, nil, StateHealthy},
		{"info only is healthy", []Finding{info}, nil, StateHealthy},
		{"warning", []Finding{warn}, nil, StateWarning},
		{"unknown beats warning", []Finding{warn}, []CheckResult{missing}, StateUnknown},
		{"error beats unknown", []Finding{errF}, []CheckResult{missing}, StateError},
		{"optional check failing is not unknown", nil, []CheckResult{optional}, StateHealthy},
	}
	for _, tc := range cases {
		if got := RollUp(tc.fs, tc.cs); got != tc.want {
			t.Errorf("%s: want %s got %s", tc.name, tc.want, got)
		}
	}
}

type fixedCheck []Finding

func (fixedCheck) Name() string { return "fixed" }
func (c fixedCheck) Run(in Input) ([]Finding, CheckResult) {
	return c, CheckResult{Name: "fixed", OK: true, CollectedAt: in.Now}
}

// A downstream check cannot hide a finding behind a severity the roll-up does
// not know: Evaluate reports it as a warning and names the original severity.
func TestEvaluateFlagsUnrecognisedSeverity(t *testing.T) {
	r := Evaluate(Input{}, []Check{fixedCheck{{Code: "ACME-001", Severity: "critical", Message: "m"}, {Code: "ACME-002", Severity: SeverityInfo}}})
	if r.State != StateWarning || r.Counts["critical"] != 0 || r.Counts[SeverityWarning] != 1 {
		t.Fatalf("want warning with the finding counted as a warning: state=%s counts=%v", r.State, r.Counts)
	}
	f := r.Findings[0]
	if f.Code != "ACME-001" || f.Severity != SeverityWarning || f.Detail != `unrecognised severity "critical"` {
		t.Fatalf("finding must be a flagged warning, ordered first: %+v", r.Findings)
	}
}

// Severities outside the known set order after info.
func TestUnknownSeverityRanksLast(t *testing.T) {
	if compareFindings(Finding{Severity: "critical"}, Finding{Severity: SeverityInfo}) <= 0 {
		t.Fatal("an unknown severity must order after info")
	}
}
