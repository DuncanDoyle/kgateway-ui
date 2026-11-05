package health

import (
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
)

func TestNotAvailableResult(t *testing.T) {
	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	for _, why := range []Availability{AvailOff, AvailDenied, AvailFailing, AvailNotOnVersion, AvailNotObserved} {
		r := NotAvailable("events", why, "detail", at)
		if r.Name != "events" || r.OK || r.Required || r.Unavailable != why || r.Error != "detail" || !r.CollectedAt.Equal(at) || r.Pending {
			t.Errorf("%s: %+v", why, r)
		}
	}
}

// RollUp ignores a not-available result even when a check also marks it required.
func TestRollUpIgnoresNotAvailable(t *testing.T) {
	warn := Finding{Severity: SeverityWarning}
	na := NotAvailable("events", AvailDenied, "", time.Time{})
	required := na
	required.Required = true
	if s := RollUp(nil, []CheckResult{na, required}); s != StateHealthy {
		t.Errorf("not-available checks alone: %s, want healthy", s)
	}
	if s := RollUp([]Finding{warn}, []CheckResult{required}); s != StateWarning {
		t.Errorf("with a warning: %s, want warning", s)
	}
}

func TestAccessAvailability(t *testing.T) {
	cases := []struct {
		a    cache.Access
		want Availability
	}{
		{cache.Access{OK: true}, ""},
		{cache.Access{}, ""}, // still syncing: pending, not failing
		{cache.Access{Err: `events is forbidden: User "ui" cannot list resource "events"`}, AvailDenied},
		{cache.Access{Err: "access refused by policy", Denied: true}, AvailDenied}, // a 403 without the usual message
		{cache.Access{Err: "connection refused"}, AvailFailing},
	}
	for _, c := range cases {
		if got := AccessAvailability(c.a); got != c.want {
			t.Errorf("%+v: %q, want %q", c.a, got, c.want)
		}
	}
}

// The output print covers Unavailable, so a memoized reader sees a reason change.
func TestOutputPrintCoversUnavailable(t *testing.T) {
	off := NotAvailable("events", AvailOff, "", time.Time{})
	denied := NotAvailable("events", AvailDenied, "", time.Time{})
	if outputPrint(nil, off) == outputPrint(nil, denied) {
		t.Fatal("outputPrint ignores Unavailable")
	}
}
