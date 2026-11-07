package health

import (
	"strings"
	"time"

	"github.com/DuncanDoyle/snellius/pkg/cache"
)

// Availability says why a check's optional input is missing (D-034).
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
	// AvailNotObserved: nothing to observe, such as a self-managed Gateway's proxies.
	AvailNotObserved Availability = "not-observed"
)

// NotAvailable returns a not-OK, not-required result with no finding; detail
// is the reason "What was checked" shows.
func NotAvailable(name string, why Availability, detail string, at time.Time) CheckResult {
	return CheckResult{Name: name, CollectedAt: at, Error: detail, Unavailable: why}
}

// AccessAvailability returns why an optional watch with access a cannot feed
// its checks; "" when a is OK or still syncing.
func AccessAvailability(a cache.Access) Availability {
	switch {
	case a.Err == "":
		return ""
	case a.Denied, strings.HasPrefix(a.Err, "forbidden: "), strings.Contains(a.Err, " is forbidden: "): // message match is a fallback
		return AvailDenied
	}
	return AvailFailing
}
