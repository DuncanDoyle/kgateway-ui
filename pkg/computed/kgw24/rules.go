// Package kgw24 is the rule set for kgateway 2.4 controllers (rules §2).
package kgw24

import "github.com/DuncanDoyle/kgateway-ui/pkg/computed"

type rules struct{}

// New returns the kgateway 2.4 rule set.
func New() computed.Rules { return rules{} }

// Version implements computed.Rules.
func (rules) Version() string { return "kgateway-2.4" }

// Attachment implements computed.Rules: 2.3's view A plus the isolation note.
func (rules) Attachment(in computed.AttachmentInput) computed.AttachmentResult {
	res := attach(in)
	isolationNotes(in, &res)
	return res
}

// Delegation implements computed.Rules.
func (rules) Delegation(in computed.DelegationInput) computed.DelegationResult {
	return delegation(in)
}
