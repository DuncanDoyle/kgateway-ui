// Package kgw23 is the rule set for kgateway 2.3 controllers (rules §2).
package kgw23

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/computed/kgwbackend"
	"github.com/DuncanDoyle/snellius/pkg/computed/viewa"
)

type rules struct{}

// New returns the kgateway 2.3 rule set.
func New() computed.Rules { return rules{} }

// Version implements computed.Rules.
func (rules) Version() string { return "kgateway-2.3" }

// Label implements computed.Labeler.
func (rules) Label() string { return "kgateway 2.3" }

// Attachment implements computed.Rules: the shared view A with kgateway's route kinds.
func (rules) Attachment(in computed.AttachmentInput) computed.AttachmentResult {
	return viewa.Attach(in, viewA)
}

// Delegation implements computed.Delegator.
func (rules) Delegation(in computed.DelegationInput) computed.DelegationResult {
	return delegation(in)
}

// JudgeBackend implements computed.BackendJudge with the kgateway pack's judgement.
func (rules) JudgeBackend(b *unstructured.Unstructured) (computed.BackendVerdict, *metav1.Condition) {
	return kgwbackend.Judge(b)
}
