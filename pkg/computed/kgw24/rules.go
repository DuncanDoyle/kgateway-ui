// Package kgw24 is the rule set for kgateway 2.4 controllers (rules §2).
package kgw24

import "github.com/DuncanDoyle/kgateway-ui/pkg/computed"

type rules struct{}

// New returns the kgateway 2.4 rule set.
func New() computed.Rules { return rules{} }

// Version implements computed.Rules.
func (rules) Version() string { return "kgateway-2.4" }

// Attachment implements computed.Rules.
func (rules) Attachment(computed.AttachmentInput) computed.AttachmentResult {
	return computed.AttachmentResult{}
}

// Delegation implements computed.Rules.
func (rules) Delegation(computed.DelegationInput) computed.DelegationResult {
	return computed.DelegationResult{}
}
