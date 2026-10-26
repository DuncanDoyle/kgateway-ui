package computed

import (
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Acceptance is a listener-bearing resource's own acceptance (rules §3.1).
type Acceptance int

// Acceptance values. A Gateway is always Accepted.
const (
	Accepted Acceptance = iota
	Denied
	NoStatus
)

// Resource is a Gateway or ListenerSet whose listeners routes attach to.
type Resource struct {
	Ref objects.Ref
	// Parent is a ListenerSet's Gateway; zero for a Gateway.
	Parent         objects.Ref
	Generation     int64
	Listeners      []objects.Listener
	ListenerStatus []objects.ListenerStatus
	Acceptance     Acceptance
}

// Route is a route as view A reads it. Written is index-aligned with ParentRefs.
type Route struct {
	Ref        objects.Ref
	Generation int64
	Hostnames  []string
	ParentRefs []objects.ParentRef
	Written    []objects.WrittenRef
	Parents    []objects.RouteParentStatus
	// ExperimentalOnly is true when the controller watches this route only with experimental features on.
	ExperimentalOnly bool
}

// AttachmentInput is everything view A reads for one install (rules §4.6).
type AttachmentInput struct {
	Resources []Resource
	Routes    []Route
	// NamespaceLabels holds each Namespace's labels; it is only meaningful when NamespacesOK.
	NamespaceLabels map[string]map[string]string
	NamespacesOK    bool
	Experimental    install.Setting
	ControllerNames []string
	// ScopeUnknown makes every count and outcome unknown: the candidates are a guess (rules §3.3).
	ScopeUnknown bool
}

// Outcome is a parentRef's attachment outcome; rejections use the controller's reasons.
type Outcome string

// Attachment outcomes.
const (
	OutcomeAccepted   Outcome = "accepted"
	OutcomeNotAllowed Outcome = "NotAllowedByListeners"
	OutcomeNoParent   Outcome = "NoMatchingParent"
	OutcomeNoHostname Outcome = "NoMatchingListenerHostname"
)

// FactCode names the listener property a fact compares.
type FactCode string

// Fact codes.
const (
	FactKind          FactCode = "kind"
	FactNamespace     FactCode = "namespace"
	FactSelectorError FactCode = "selector-error"
	FactPort          FactCode = "port"
	FactSection       FactCode = "section"
	FactHostname      FactCode = "hostname"
)

// Fact is one reason a listener did not take a parentRef.
type Fact struct {
	Listener  string
	Code      FactCode
	Want, Got string
}

// Member is a route attached to a listener through one parentRef.
type Member struct {
	Resource  objects.Ref
	Listener  string
	Route     objects.Ref
	ParentRef int
	// Hostnames are the effective hostnames on this listener.
	Hostnames []string
	Deps      []objects.Ref
}

// RefOutcome is the computed outcome of one parentRef. Rule names the deciding
// stage: allowed-routes, section-and-port or hostname-intersection.
type RefOutcome struct {
	Route     objects.Ref
	ParentRef int
	Resource  objects.Ref
	Outcome   Outcome
	Listeners []string
	Facts     []Fact
	Unknown   UnknownReason
	Rule      string
	Deps      []objects.Ref
}

// ListenerCount is a listener's computed attachedRoutes (rule listener-count).
type ListenerCount struct {
	Resource objects.Ref
	Listener string
	Count    int
	Unknown  UnknownReason
	// Error is the listener's selector error. It is computed only: kgateway
	// never reports it, and the §8 cross-check never compares it.
	Error string
	// IsolationNote marks a count that plain-HTTP listener isolation may not program (rules §4.5).
	IsolationNote bool
}

// AttachmentResult is view A for one install. Its Deps slices are read-only and
// may be shared between entries: never sort or append to them in place.
type AttachmentResult struct {
	Members  []Member
	Outcomes []RefOutcome
	Counts   []ListenerCount
}
