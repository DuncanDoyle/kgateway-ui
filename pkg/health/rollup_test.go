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
