package health

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// BackendNoEvidenceDetail is KGWUI-002's detail when a False Backend
// Accepted condition carries no observedGeneration (design §6.2). Draft
// wording, finalised in the wording pass.
const BackendNoEvidenceDetail = "the controller does not report which Backend version it checked"

// backendAccepted returns the Accepted condition and its evidence: current
// (observedGeneration at or above the generation), stale (lower, above 0), or
// none (0, as kgateway 2.3 writes it). A nil condition means absent.
func backendAccepted(b *unstructured.Unstructured) (cond *metav1.Condition, current, stale bool) {
	cond = objects.FindCondition(objects.DecodeBackend(b).Conditions, "Accepted")
	if cond == nil || cond.ObservedGeneration == 0 {
		return cond, false, false
	}
	current = cond.ObservedGeneration >= b.GetGeneration()
	return cond, current, !current
}

// BackendState returns a Backend's state from its Accepted condition's own
// evidence, never from the controller version, with that condition (nil when
// absent). Without current evidence the state is unknown, never healthy.
func BackendState(b *unstructured.Unstructured) (State, *metav1.Condition) {
	cond, current, _ := backendAccepted(b)
	switch {
	case !current:
		return StateUnknown, cond
	case cond.Status == metav1.ConditionFalse:
		return StateError, cond
	case cond.Status == metav1.ConditionTrue:
		return StateHealthy, cond
	}
	return StateUnknown, cond
}

// backendCheck raises KGWUI-002 for in-use Backends whose Accepted condition
// is False. It is optional and never adds "not yet processed" (design §6.2).
type backendCheck struct{}

// Name implements Check.
func (backendCheck) Name() string { return "backend status" }

// Run implements Check.
func (c backendCheck) Run(in Input) ([]Finding, CheckResult) {
	var fs []Finding
	if in.Owned != nil {
		for _, b := range in.Owned.Backends {
			cond, current, _ := backendAccepted(b)
			if cond == nil || cond.Status != metav1.ConditionFalse {
				continue
			}
			f := Finding{Code: CodeConditionFalse, Slug: "condition-false", Install: in.Install.ID(), Resource: ref(b),
				Message: message(*cond), Since: since(*cond)}
			if current {
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
