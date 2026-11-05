// Package health turns cluster state and controller signals into findings and
// a health state per install (design §5).
package health

import (
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxymode"
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
	CodeXDSRejected          = "KGWUI-001"
	CodeConditionFalse       = "KGWUI-002"
	CodeRouteUnattached      = "KGWUI-003"
	CodeNamespaceNotWatched  = "KGWUI-004"
	CodeControllerNotReady   = "KGWUI-005"
	CodeProxyNotReady        = "KGWUI-006"
	CodeScopeMismatch        = "KGWUI-007"
	CodeSignalUnavailable    = "KGWUI-008"
	CodeAmbiguousOwnership   = "KGWUI-009"
	CodeSelfManagedProxy     = "KGWUI-010"
	CodeDelegatedNotAttached = "KGWUI-011"
	CodeComputedDisagrees    = "KGWUI-012"
	CodeRefUnresolved        = "KGWUI-013"
	CodeRefNotPermitted      = "KGWUI-014"
	CodeProcessRestarting    = "KGWUI-019"
	CodeProbeFailing         = "KGWUI-020"
	CodeRolloutStuck         = "KGWUI-021"
	CodeVersionSkew          = "KGWUI-022"
	CodeProxyConfigWarming   = "KGWUI-025"
	CodeProxyDisconnected    = "KGWUI-026"
)

// Grace is how long a resource may lack current status before it is reported
// as not yet processed.
const Grace = 60 * time.Second

// Finding is one problem or notable fact about an install.
type Finding struct {
	// ID is stable across recomputes and restarts and unique within the
	// install; Evaluate sets it from Code, Resource and Key (design §5.1).
	ID       string       `json:"id,omitempty"`
	Code     string       `json:"code"`
	Slug     string       `json:"slug"`
	Severity Severity     `json:"severity"`
	Install  string       `json:"install"`
	Resource *objects.Ref `json:"resource,omitempty"`
	Message  string       `json:"message"`
	Detail   string       `json:"detail,omitempty"`
	// Since is when the problem started: a condition's lastTransitionTime, or
	// for a finding returned without one, when the UI first saw its ID.
	Since *time.Time `json:"since,omitempty"`
	// Group is the ID of the group whose cause explains the finding (design §5.2).
	Group string `json:"group,omitempty"`
	// Key is the code's discriminator among findings on one resource (design
	// §5.1); Evaluate hashes it into ID and clears it. Never serialised.
	Key []string `json:"-"`
	// Restricted marks a finding on a resource the caller may not view. The
	// API keeps its code, severity and resource and empties Message, Detail
	// and Since (D-013). Checks never set it.
	Restricted bool `json:"restricted,omitempty"`
	// Evidence are the objects, other than Resource, whose data Detail or
	// Facts quote (KGWUI-011: the parent route). Never serialised.
	Evidence []objects.Ref `json:"-"`
	// Facts are the typed evidence; the API drops them for a caller who may
	// not view Resource and all of Evidence (design §14 rule 12).
	Facts Evidence `json:"evidence,omitempty"`
	// ResourceSince is when the engine first saw Resource's current
	// generation. It replaces Since for a caller who may not view all of
	// Evidence, so Since tells that caller nothing about it. Never serialised.
	ResourceSince *time.Time `json:"-"`
	// parent is the route parentRef a KGWUI-003 or KGWUI-011 finding is about;
	// it orders findings. A pointer keeps the other findings small.
	parent *objects.ParentRef
}

// CheckResult records whether a check could collect current input. A Required
// check that is not OK forces the state to unknown, unless Unavailable is set.
type CheckResult struct {
	Name        string    `json:"name"`
	OK          bool      `json:"ok"`
	Required    bool      `json:"required"`
	CollectedAt time.Time `json:"collectedAt"`
	Error       string    `json:"error,omitempty"`
	// Detail is extra context for an OK check, such as where a scope was read from.
	Detail string `json:"detail,omitempty"`
	// Pending marks a check that is not OK only because resources are still
	// being processed inside the grace period.
	Pending bool `json:"pending,omitempty"`
	// Unavailable says why the check's optional input is missing; RollUp ignores it.
	Unavailable Availability `json:"unavailable,omitempty"`
	// Gaps are the Gateways a per-Gateway input has nothing for, in Ref order.
	// Never serialised: a Gateway page reads its own entry after authorizing it.
	Gaps []GatewayGap `json:"-"`
	// Withheld are the Gateways whose proxies may get no config updates, in Ref order (R105).
	// Never serialised: a Gateway page reads its own entry after authorizing it.
	Withheld []WithheldGateway `json:"-"`
	// ControllerLogs says whether a check that reads the controller log read it
	// (LogsRead or LogsNotRead); empty for other checks. Never serialised.
	ControllerLogs string `json:"-"`
}

// WithheldGateway is a Gateway whose snapshot deferral recurred after its
// proxies and the controller started: the first and last such line.
type WithheldGateway struct {
	Gateway     objects.Ref
	First, Last time.Time
}

// GatewayGap says why a check's input is not available for one Gateway.
type GatewayGap struct {
	Gateway     objects.Ref
	Unavailable Availability
	Reason      string
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
	// Catalog is every kind the API server serves; the zero Catalog knows none.
	Catalog    kinds.Catalog
	Metrics    ctrlmetrics.Result
	HasMetrics bool
	// MetricsMaxAge is the age beyond which Metrics is stale (3 × the metrics
	// interval); zero means no limit.
	MetricsMaxAge time.Duration
	// FirstSeen returns when the engine first saw u's current generation; the
	// grace period runs from it. Nil means u's creation time.
	FirstSeen func(u *unstructured.Unstructured) time.Time
	Verify    scopeverify.Result
	Now       time.Time
	// Platform decides the wording of readiness findings; zero means Kubernetes.
	Platform platform.Info
	// ProxyMode decides whether a Gateway is self-managed; nil means proxymode.GatewayParameters{}.
	ProxyMode proxymode.Resolver
	// Computed are the install's computed views with their check states; nil means none.
	Computed *computed.Views
	// Views decodes Gateways, ListenerSets and routes; nil means objects.Direct.
	// Its views are shared and read-only.
	Views objects.Decoder
	// Process is the install's process data; a check declares InputSet.Process to read it.
	Process *ProcessData
	// GatewayAPI is the rule-set selection's Gateway API table (design §9.4); nil means none.
	GatewayAPI func(kgatewayMinor string) (bundle string, ok bool)
	// Rules is the install's rule set; one without a PolicyTargeter raises no computed KGWUI-016.
	Rules computed.Selection
}

// ProcessData is one install's controller and per-Gateway proxy processes
// (§9.1); Print hashes both, and process.PodRef gives each Pod's ref.
type ProcessData struct {
	Controller process.Set
	Proxies    map[objects.Ref]process.Set
	Print      uint64
}

// views returns in.Views, or objects.Direct when it is unset.
func (in Input) views() objects.Decoder {
	if in.Views == nil {
		return objects.Direct{}
	}
	return in.Views
}

// routeParents returns route u's status.parents, decoded through in.Views.
func (in Input) routeParents(u *unstructured.Unstructured) []objects.RouteParentStatus {
	if gvk := u.GroupVersionKind(); gvk.Group == objects.GatewayAPIGroup && gvk.Kind == "HTTPRoute" {
		return in.views().HTTPRoute(u).Parents
	}
	return in.views().Route(u).Parents
}

// firstSeen returns when the grace period for u's current generation started.
func (in Input) firstSeen(u *unstructured.Unstructured) time.Time {
	if in.FirstSeen != nil {
		return in.FirstSeen(u)
	}
	return u.GetCreationTimestamp().Time
}

// proxyMode returns how gw's proxy is run.
func (in Input) proxyMode(gw *unstructured.Unstructured) proxymode.Mode {
	r := in.ProxyMode
	if r == nil {
		r = proxymode.GatewayParameters{}
	}
	return r.Resolve(gw, in.Snapshot, in.Served)
}

// Check produces findings for one concern. Evaluate runs checks in
// registration order.
type Check interface {
	// Name identifies the check and its Store; it is unique among the registered checks.
	Name() string
	// Inputs names what Run reads. Reading an undeclared input is a bug.
	Inputs() InputSet
	Run(ctx RunContext) ([]Finding, CheckResult)
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
	// Next is the earliest time after CollectedAt at which a check's output
	// may change; zero means none.
	Next time.Time `json:"-"`
}
