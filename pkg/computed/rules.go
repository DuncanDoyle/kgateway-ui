// Package computed is the seam for the UI's computed views (D-020): the rule
// interface, its inputs and results, and the states a cross-check reports.
// The version-specific rule sets live in subpackages; rulesets selects one.
package computed

// Rules computes the views for one controller rule set (design §3.4).
type Rules interface {
	// Version names the rule set, such as "kgateway-2.4".
	Version() string
	// Attachment computes view A (rules §4).
	Attachment(in AttachmentInput) AttachmentResult
	// Delegation computes views B and C (rules §5, §6).
	Delegation(in DelegationInput) DelegationResult
}

// Selection is the rule set chosen for a controller, with its label (rules §2).
type Selection struct {
	Rules Rules
	// Note is the selection label; it is part of every result's check note. Empty when the set matches.
	Note string
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
	// RuleSet is the rule set's Version; Note is its selection label.
	RuleSet, Note string
	Attachment    AttachmentResult
	// Delegation is views B and C (rules §5, §6).
	Delegation DelegationResult
	Checks     []CheckResult
}
