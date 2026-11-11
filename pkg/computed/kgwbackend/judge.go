// Package kgwbackend is the kgateway pack's judgement of a kgateway Backend
// from its Accepted condition (Phase 6 design §11).
package kgwbackend

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// Judge returns b's verdict from its Accepted condition's own evidence, never
// from the controller version, with that condition (nil when absent). Evidence
// is current when observedGeneration is at or above the generation; a stale
// one or none (0, as kgateway 2.3 writes it) is unknown, never accepted.
func Judge(b *unstructured.Unstructured) (computed.BackendVerdict, *metav1.Condition) {
	cond := objects.FindCondition(objects.DecodeBackend(b).Conditions, "Accepted")
	if cond == nil || cond.ObservedGeneration == 0 || cond.ObservedGeneration < b.GetGeneration() {
		return computed.BackendUnknown, cond
	}
	switch cond.Status {
	case metav1.ConditionFalse:
		return computed.BackendRejected, cond
	case metav1.ConditionTrue:
		return computed.BackendAccepted, cond
	}
	return computed.BackendUnknown, cond
}
