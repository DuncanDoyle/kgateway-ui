// Package computed is the seam for the UI's computed views (D-020): the rule
// interface, its inputs and results, and the states a cross-check reports.
// The version-specific rule sets live in subpackages; rulesets selects one.
package computed

// Rules computes the views for one controller rule set (design §3.4). A rule
// set that computes delegation also implements Delegator.
type Rules interface {
	// Version names the rule set, such as "kgateway-2.4".
	Version() string
	// Attachment computes view A (rules §4).
	Attachment(in AttachmentInput) AttachmentResult
}

// Delegator is an optional interface of a Rules: it computes views B and C
// (rules §5, §6). A rule set without it has no delegation; view A stays.
type Delegator interface {
	Delegation(in DelegationInput) DelegationResult
}

// Delegator returns the selected rule set's delegator, or nil when it has none.
// A wrapping rule set forwards it to keep delegation.
func (s Selection) Delegator() Delegator {
	d, _ := s.Rules.(Delegator)
	return d
}

// Labeler is an optional interface of a Rules: its display name, such as "kgateway 2.4".
type Labeler interface {
	Label() string
}

// Label is the selected rule set's display name: its Label, else its Version; "" without one.
func (s Selection) Label() string {
	if l, ok := s.Rules.(Labeler); ok {
		return l.Label()
	}
	if s.Rules == nil {
		return ""
	}
	return s.Rules.Version()
}

// Selection is the rule set chosen for a controller, with its label (rules §2).
type Selection struct {
	Rules Rules
	// Note is the selection label; it is part of every result's check note. Empty when the set matches.
	Note string
	// GatewayAPI maps a kgateway minor to its Gateway API bundle minor; nil means rulesets.ExpectedGatewayAPI.
	GatewayAPI func(kgatewayMinor string) (bundle string, ok bool)
}

// UnknownReason says why a result could not be computed (rules §3.3).
type UnknownReason string

// Unknown reasons, as served on the wire.
const (
	UnknownNamespaceLabels   UnknownReason = "namespace-labels-unavailable"
	UnknownListenerSetStatus UnknownReason = "listenerset-no-status"
	UnknownSetting           UnknownReason = "setting-unknown"
	UnknownNotReached        UnknownReason = "not-reached"
	UnknownContext           UnknownReason = "context-unknown"
	UnknownTooLarge          UnknownReason = "too-large"
	// UnknownScope: the install's discovery scope is unknown (user, 2026-10-24).
	UnknownScope UnknownReason = "scope-unknown"
)

// State is a cross-check's state on the wire (rules §8.4).
type State string

// Check states. StateRestricted is set at serve time (design §7.4).
const (
	StateChecked    State = "checked"
	StateDisagrees  State = "disagrees"
	StatePending    State = "pending"
	StateUnknown    State = "unknown"
	StateRestricted State = "restricted"
)

// Coverage says what a passing cross-check verified (rules §8.4).
type Coverage string

// Coverage labels.
const (
	CoverageCount  Coverage = "count-agrees"
	CoverageStatus Coverage = "status-compatible"
	CoverageNone   Coverage = "not-verified"
)

// Views are one install's computed views and their cross-checks (D-020).
// The engine sets each check's State, Mature and Since.
type Views struct {
	// RuleSet is the rule set's Version, Label its display name and Note its selection label.
	RuleSet, Label, Note string
	Attachment           AttachmentResult
	// Delegation is views B and C (rules §5, §6).
	Delegation DelegationResult
	Checks     []CheckResult
	// Generation changes only when Attachment or Delegation is recomputed;
	// equal generations from one process hold the same results.
	Generation uint64
}
