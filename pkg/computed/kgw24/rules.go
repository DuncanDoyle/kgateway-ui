// Package kgw24 is the rule set for kgateway 2.4 controllers (rules §2).
package kgw24

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/computed/kgwbackend"
	"github.com/DuncanDoyle/snellius/pkg/computed/viewa"
)

type rules struct{}

// New returns the kgateway 2.4 rule set.
func New() computed.Rules { return rules{} }

// Version implements computed.Rules.
func (rules) Version() string { return "kgateway-2.4" }

// Label implements computed.Labeler.
func (rules) Label() string { return "kgateway 2.4" }

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

// JudgeBackend implements computed.BackendJudge with the kgateway pack's judgement.
func (rules) JudgeBackend(b *unstructured.Unstructured) (computed.BackendVerdict, *metav1.Condition) {
	return kgwbackend.Judge(b)
}
