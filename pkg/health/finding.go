// Package health turns cluster state and controller signals into findings and
// a health state per install (design §5).
package health

import (
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

// Severity of a finding.
type Severity string

// Severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// State is the rolled-up health of an install.
type State string

// States, from worst to best: error, unknown, warning, healthy.
const (
	StateError   State = "error"
	StateUnknown State = "unknown"
	StateWarning State = "warning"
	StateHealthy State = "healthy"
)

// Stable finding codes. Never reuse a code (design §5.1).
const (
	CodeXDSRejected         = "KGWUI-001"
	CodeConditionFalse      = "KGWUI-002"
	CodeRouteUnattached     = "KGWUI-003"
	CodeNamespaceNotWatched = "KGWUI-004"
	CodeControllerNotReady  = "KGWUI-005"
	CodeProxyNotReady       = "KGWUI-006"
	CodeScopeMismatch       = "KGWUI-007"
	CodeSignalUnavailable   = "KGWUI-008"
	CodeAmbiguousOwnership  = "KGWUI-009"
)

// Grace is how long a resource may lack current status before it is reported
// as not yet processed.
const Grace = 60 * time.Second

// Finding is one problem or notable fact about an install.
type Finding struct {
	Code     string       `json:"code"`
	Slug     string       `json:"slug"`
	Severity Severity     `json:"severity"`
	Install  string       `json:"install"`
	Resource *objects.Ref `json:"resource,omitempty"`
	Message  string       `json:"message"`
	Detail   string       `json:"detail,omitempty"`
	Since    *time.Time   `json:"since,omitempty"`
	// Restricted marks a finding on a resource the caller may not view. The
	// API keeps its code, severity and resource and empties Message, Detail
	// and Since (D-013). Checks never set it.
	Restricted bool `json:"restricted,omitempty"`
	// parent is the route parentRef a KGWUI-003 finding is about; it orders findings.
	parent objects.ParentRef
}

// CheckResult records whether a check could collect current input. A Required
// check that is not OK forces the state to unknown.
type CheckResult struct {
	Name        string    `json:"name"`
	OK          bool      `json:"ok"`
	Required    bool      `json:"required"`
	CollectedAt time.Time `json:"collectedAt"`
	Error       string    `json:"error,omitempty"`
	// Pending marks a check that is not OK only because resources are still
	// being processed inside the grace period.
	Pending bool `json:"pending,omitempty"`
}

// Input is everything a check may look at for one install.
type Input struct {
	Install    install.Resolved
	Owned      *install.Owned
	Assignment install.Assignment
	Snapshot   *cache.Snapshot
	// Served are the served kinds; they map a parentRef's kind to its Access.
	Served []kinds.Served
	// KindsChange is how the kinds served now differ from Served, which the cache watches.
	KindsChange kinds.Change
	Metrics     ctrlmetrics.Result
	HasMetrics  bool
	// MetricsMaxAge is the age beyond which Metrics is stale (3 × the metrics
	// interval); zero means no limit.
	MetricsMaxAge time.Duration
	// FirstSeen returns when the engine first saw u's current generation; the
	// grace period runs from it. Nil means u's creation time.
	FirstSeen func(u *unstructured.Unstructured) time.Time
	Verify    scopeverify.Result
	Now       time.Time
}

// firstSeen returns when the grace period for u's current generation started.
func (in Input) firstSeen(u *unstructured.Unstructured) time.Time {
	if in.FirstSeen != nil {
		return in.FirstSeen(u)
	}
	return u.GetCreationTimestamp().Time
}

// Check produces findings for one concern.
type Check interface {
	Name() string
	Run(in Input) ([]Finding, CheckResult)
}

// Report is the evaluated health of one install.
type Report struct {
	// Install is the install ID, so a client can reject a response for another install.
	Install     string           `json:"install"`
	State       State            `json:"state"`
	Findings    []Finding        `json:"findings"`
	Checks      []CheckResult    `json:"checks"`
	Counts      map[Severity]int `json:"counts"`
	CollectedAt time.Time        `json:"collectedAt"`
}
