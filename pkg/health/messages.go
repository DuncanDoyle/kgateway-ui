package health

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// reasonMessages turns condition reasons into operator language.
var reasonMessages = map[string]string{
	"BackendNotFound":            "Backend not found",
	"RefNotPermitted":            "Reference not permitted (missing ReferenceGrant)",
	"InvalidKind":                "Unsupported backend kind",
	"NoMatchingParent":           "No matching listener on the parent",
	"NotAllowedByListeners":      "Not allowed by the Gateway's listeners",
	"NoMatchingListenerHostname": "No listener hostname matches",
	"UnsupportedValue":           "Unsupported value in the route",
	"RouteRuleDropped":           "Rule dropped because of invalid configuration",
	"RouteRuleReplaced":          "Rule replaced by a direct response",
	"Invalid":                    "Invalid configuration",
	"TargetNotFound":             "Policy target not found",
	"PartiallyValid":             "Partially valid; some settings were ignored",
	"Overridden":                 "Overridden by another policy",
	"AddressNotAssigned":         "No address assigned",
	"InvalidParameters":          "Invalid GatewayClass parameters",
	"PortUnavailable":            "Listener port unavailable",
	"HostnameConflict":           "Listener hostname conflicts with another listener",
	"ProtocolConflict":           "Listener protocol conflicts with another listener",
	"InvalidCertificateRef":      "Invalid certificate reference",
}

// message returns operator language for c, falling back to "<Type> is <Status> (<Reason>)".
func message(c metav1.Condition) string {
	if m, ok := reasonMessages[c.Reason]; ok {
		return m
	}
	return fmt.Sprintf("%s is %s (%s)", c.Type, c.Status, c.Reason)
}

// detail returns the raw condition for the secondary detail field.
func detail(c metav1.Condition, extra string) string {
	d := fmt.Sprintf("%s=%s, reason %s", c.Type, c.Status, c.Reason)
	if c.Message != "" {
		d += ": " + c.Message
	}
	if extra != "" {
		d += " (" + extra + ")"
	}
	return d
}

// Wording for delegation findings (design §6.2), from the wording pass (Task 31).
const (
	// DelegatedNotAttachedMessage is KGWUI-011's message.
	DelegatedNotAttachedMessage = "No status for the named parent route"
	// DelegatedNotAttachedDetail is KGWUI-011's status-trigger detail; %s is the parent.
	DelegatedNotAttachedDetail = "No status entry for parent HTTPRoute %s"
	// DelegationNoChildMessage replaces "Backend not found" on a parent whose
	// backendRefs all delegate (D-021 item 9).
	DelegationNoChildMessage = "A delegating rule has no accepted child route"
	// DelegationNoChildDetail starts that finding's detail; the raw condition follows.
	DelegationNoChildDetail = "A delegating rule selects no child route the controller accepted"
	// ComputedNotAttachedMessage is the message of KGWUI-011's computed and orphan triggers (Task 67).
	ComputedNotAttachedMessage = "A parent route does not attach this route"
	// OrphanDetail is the orphan trigger's detail (Task 67).
	OrphanDetail = "The UI computes that no parent route in this install keeps this route. The route has no parentRefs."
	// computedDropped and computedForbidden are the computed trigger's
	// reason; %s is the parent (Task 67).
	computedDropped   = "The UI computes that HTTPRoute %s drops this route: no rule fits the parent's matches."
	computedForbidden = "The UI computes that HTTPRoute %s selects this route, but the route's parentRefs do not permit that parent."
)

// Wording for KGWUI-012 (design §6.2), from the wording passes (Tasks 48 and 67).
const (
	// ComputedDisagreesMessage is KGWUI-012's message.
	ComputedDisagreesMessage = "The UI's computed view disagrees with the controller"
	// countDisagrees is a listener-count detail: listener, computed, controller.
	countDisagrees = "Listener `%s`: computed %s, controller reports %s."
	// parentDisagrees is a route-accepted detail: parent, computed, controller.
	parentDisagrees = "Parent %s: computed %s, controller reports %s."
	// ruleSetDetail ends every detail with the rule set.
	ruleSetDetail = " Rule set %s."
	// childDisagrees is a child-entry detail: parent, its delegated match (atMatch or ""), computed, controller.
	childDisagrees = "Status entry for parent HTTPRoute `%s`%s: computed %s, controller reports %s."
	// resolvedDisagrees is a parent-resolvedrefs detail: the parent's report
	// key, the match that key delegated under (atMatch or ""), computed, controller.
	resolvedDisagrees = "ResolvedRefs under parent %s%s: computed %s, controller reports %s."
	// atMatch quotes a delegated match after the ancestor's name; matches is
	// one or more backticked matches joined with matchOr.
	atMatch = " at %s"
	matchOr = " or "
	// matchMore ends a match list cut at maxMatches.
	matchMore = " or more"
)
