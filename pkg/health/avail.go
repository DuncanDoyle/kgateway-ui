package health

import (
	"strings"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
)

// Availability says why a check whose optional input is missing could not
// run (design §7.1, D-034).
type Availability string

// The reasons a check is not available.
const (
	// AvailOff: the input's toggle is off.
	AvailOff Availability = "off"
	// AvailDenied: RBAC refused the watch or read.
	AvailDenied Availability = "denied"
	// AvailFailing: the watch or scrape errors.
	AvailFailing Availability = "failing"
	// AvailNotOnVersion: this controller version does not expose the input.
	AvailNotOnVersion Availability = "not-on-version"
	// AvailNotObserved: the UI has nothing to observe, such as the proxies
	// of a self-managed Gateway.
	AvailNotObserved Availability = "not-observed"
)

// NotAvailable returns the result of a check whose optional input is missing:
// not OK, not required and with no finding, so it neither raises KGWUI-008 nor
// turns the install unknown. detail is the reason "What was checked" shows.
func NotAvailable(name string, why Availability, detail string, at time.Time) CheckResult {
	return CheckResult{Name: name, CollectedAt: at, Error: detail, Unavailable: why}
}

// AccessAvailability returns why an optional watch with access a cannot feed
// its checks: AvailDenied for a refused watch, AvailFailing for any other
// error, and "" when a is OK or still syncing (not OK, no error).
func AccessAvailability(a cache.Access) Availability {
	switch {
	case a.Err == "":
		return ""
	case strings.HasPrefix(a.Err, "forbidden: ") || strings.Contains(a.Err, " is forbidden: "): // a 403's message
		return AvailDenied
	}
	return AvailFailing
}
