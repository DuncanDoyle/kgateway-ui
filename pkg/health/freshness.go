package health

import (
	"fmt"
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Freshness kinds of KGWUI-003 (design §10.1), in table order: an object
// that matches two takes the first.
const (
	FreshnessNeverWritten = "never-written"
	FreshnessStale        = "stale"
	FreshnessPending      = "pending"
	FreshnessNoAncestor   = "no-ancestor"
)

// freshnessRank is a kind's place in the §10.1 table.
var freshnessRank = map[string]int{FreshnessNeverWritten: 0, FreshnessStale: 1, FreshnessPending: 2, FreshnessNoAncestor: 3}

// noControllerStatus is the detail of a block the controller never wrote.
const noControllerStatus = "no status from the controller"

// blockFreshness returns why a status block of u is not current, its kind
// and, for a stale block, the lowest observed generation; "" when it is
// current. Kinds are tried in §10.1 order (D-006).
func blockFreshness(u *unstructured.Unstructured, cs []metav1.Condition, required, optional []string) (why, kind string, observed int64) {
	if crdDefaultOnly(cs) {
		return noControllerStatus, FreshnessNeverWritten, 0
	}
	types := append(slices.Clone(required), optional...)
	stale := false
	for _, t := range types {
		if c := objects.FindCondition(cs, t); c != nil && objects.IsStale(u, *c) && (!stale || c.ObservedGeneration < observed) {
			stale, observed = true, c.ObservedGeneration
		}
	}
	if stale {
		return "status describes an older generation", FreshnessStale, observed
	}
	for _, t := range required {
		if objects.FindCondition(cs, t) == nil {
			return fmt.Sprintf("no %s condition", t), FreshnessPending, 0
		}
	}
	for _, t := range types {
		if c := objects.FindCondition(cs, t); c != nil && c.Reason == "Pending" {
			return t + " is Pending", FreshnessPending, 0
		}
	}
	for _, t := range required {
		if objects.FindCondition(cs, t).Status == metav1.ConditionUnknown {
			return t + " is Unknown", FreshnessPending, 0
		}
	}
	return "", "", 0
}

// crdDefaultOnly reports whether every condition of a non-empty block is the
// Gateway API CRD default: reason Pending, "Waiting for controller", at the Unix epoch.
func crdDefaultOnly(cs []metav1.Condition) bool {
	return len(cs) > 0 && !slices.ContainsFunc(cs, func(c metav1.Condition) bool {
		return c.Reason != "Pending" || c.Message != "Waiting for controller" || !c.LastTransitionTime.Time.Equal(time.Unix(0, 0))
	})
}

// freshnessEvidence is KGWUI-003's evidence for u; a stale one quotes the
// observed and current generations and when the UI first saw the current one.
func (in Input) freshnessEvidence(u *unstructured.Unstructured, kind string, observed int64) *FreshnessEvidence {
	ev := &FreshnessEvidence{Freshness: kind}
	if kind == FreshnessStale {
		current, since := u.GetGeneration(), in.firstSeen(u)
		ev.Observed, ev.Current, ev.Since = &observed, &current, &since
	}
	return ev
}
