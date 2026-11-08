// Package kgw25 is the rule set for kgateway 2.5 controllers (rules §2). It is
// kgw24's set; cited source lines (V4) are v2.4.7, unchanged in behaviour at v2.5.0.
package kgw25

import (
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/computed/viewa"
)

type rules struct{}

// New returns the kgateway 2.5 rule set. The controller reports a missing
// targetRef itself (D-020 rule 5); its computed.PolicyTargeter judges selectors only.
func New() computed.Rules { return rules{} }

// Version implements computed.Rules.
func (rules) Version() string { return "kgateway-2.5" }

// Label implements computed.Labeler.
func (rules) Label() string { return "kgateway 2.5" }

// ReportsTargetNotFound implements computed.TargetStatusReporter: 2.5 controllers
// write TargetNotFound on policy ancestors (#14709), so KGWUI-016 comes from status.
func (rules) ReportsTargetNotFound() bool { return true }

// Attachment implements computed.Rules: the shared view A plus the isolation note.
func (rules) Attachment(in computed.AttachmentInput) computed.AttachmentResult {
	res := viewa.Attach(in, viewA)
	isolationNotes(in, &res)
	return res
}

// Delegation implements computed.Delegator.
func (rules) Delegation(in computed.DelegationInput) computed.DelegationResult {
	return delegation(in)
}
