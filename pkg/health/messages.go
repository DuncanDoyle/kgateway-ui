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

// Draft wording for delegation findings (design §6.2); finalised in the wording pass.
const (
	// DelegatedNotAttachedMessage is KGWUI-011's message.
	DelegatedNotAttachedMessage = "Parent route does not use this route"
	// DelegatedNotAttachedDetail is KGWUI-011's status-trigger detail; %s is the parent.
	DelegatedNotAttachedDetail = "No status from HTTPRoute %s"
	// DelegationNoChildMessage replaces "Backend not found" on a parent whose
	// backendRefs all delegate (D-021 item 9).
	DelegationNoChildMessage = "No child route kept by a delegating rule"
	// DelegationNoChildDetail starts that finding's detail; the raw condition follows.
	DelegationNoChildDetail = "A delegating rule selects no child route the controller accepted"
)
