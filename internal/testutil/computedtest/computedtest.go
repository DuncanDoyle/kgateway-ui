// Package computedtest has test doubles for the computed views.
package computedtest

import "github.com/DuncanDoyle/kgateway-ui/pkg/computed"

// noRules is a computed rule set that computes nothing.
type noRules struct{}

func (noRules) Version() string { return "none" }

func (noRules) Attachment(computed.AttachmentInput) computed.AttachmentResult {
	return computed.AttachmentResult{}
}

func (noRules) Delegation(computed.DelegationInput) computed.DelegationResult {
	return computed.DelegationResult{}
}

// NoViews is an engine RulesFor that computes no views. Tests about other
// concerns use it when their fixtures report status the rules would dispute.
func NoViews(string) computed.Selection { return computed.Selection{Rules: noRules{}} }
