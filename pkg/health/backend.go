package health

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/computed/kgwbackend"
)

// BackendNoEvidenceDetail is KGWUI-002's detail when a False Backend
// Accepted condition carries no observedGeneration (design §6.2). Draft
// wording, finalised in the wording pass.
const BackendNoEvidenceDetail = "the controller does not report which Backend version it checked"

// BackendState returns a Backend's state from the kgateway pack's judgement
// of its Accepted condition's own evidence, never from the controller
// version, with that condition (nil when absent). Without current evidence
// the state is unknown, never healthy. Core code reads computed.BackendJudge.
func BackendState(b *unstructured.Unstructured) (State, *metav1.Condition) {
	v, cond := kgwbackend.Judge(b)
	switch v {
	case computed.BackendRejected:
		return StateError, cond
	case computed.BackendAccepted:
		return StateHealthy, cond
	}
	return StateUnknown, cond
}

// backendCheck raises KGWUI-002 for in-use Backends whose Accepted condition
// is False. It is optional and never adds "not yet processed" (design §6.2).
// It is a kgateway pack check (Phase 6 design §11).
type backendCheck struct{}

// Name implements Check.
func (backendCheck) Name() string { return "backend status" }

// Inputs implements Check.
func (backendCheck) Inputs() InputSet { return AllInputs() }

// Run implements Check.
func (c backendCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	var fs []Finding
	if in.Owned != nil {
		for _, b := range in.Owned.Backends {
			v, cond := kgwbackend.Judge(b)
			if cond == nil || cond.Status != metav1.ConditionFalse {
				continue
			}
			// Keyed as conditionsCheck keys KGWUI-002: the owner and a rank, never the type.
			f := Finding{Code: CodeConditionFalse, Slug: "condition-false", Install: in.Install.ID(), Resource: ref(b),
				Message: message(*cond), Since: since(*cond), Key: []string{ownerSelf, ordinal(1)}}
			if v == computed.BackendRejected {
				f.Severity, f.Detail = SeverityError, detail(*cond, "")
			} else if cond.ObservedGeneration == 0 {
				f.Severity, f.Detail = SeverityWarning, detail(*cond, BackendNoEvidenceDetail)
			} else {
				continue // stale: no finding
			}
			fs = append(fs, f)
		}
	}
	return fs, ok(c.Name(), false, in.Now)
}
