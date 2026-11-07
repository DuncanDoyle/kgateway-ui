package health

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
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
		return "Status describes an older generation", FreshnessStale, observed
	}
	for _, t := range required {
		if objects.FindCondition(cs, t) == nil {
			return fmt.Sprintf("No %s condition", t), FreshnessPending, 0
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

// CodeStatusNotProgressing is KGWUI-018: status as a whole stopped moving (design §10.2).
const CodeStatusNotProgressing = "KGWUI-018"

// progressHold is how long the 018 condition holds before it matures.
const progressHold = 5 * time.Minute

// Thresholds of 018: progressObjects stuck objects, or progressShare percent of
// objects with status with at least progressMin.
const (
	progressObjects = 10
	progressShare   = 10
	progressMin     = 3
)

// progressKey is the progress check's Store key.
const progressKey = "progress"

// EvidenceStatusProgress is the kind of StatusProgressEvidence.
const EvidenceStatusProgress = "status-progress"

// StatusProgressEvidence is what KGWUI-018 quotes: the stuck objects per §10.1
// kind and the oldest KGWUI-003 warning's since.
type StatusProgressEvidence struct {
	Kinds  []FreshnessCount `json:"kinds"`
	Oldest time.Time        `json:"oldest"`
}

// FreshnessCount is how many stuck objects have one §10.1 kind; an object
// with several 003s counts under its first kind in table order.
type FreshnessCount struct {
	Freshness string `json:"freshness"`
	Objects   int    `json:"objects"`
}

// EvidenceKind implements Evidence.
func (*StatusProgressEvidence) EvidenceKind() string { return EvidenceStatusProgress }

// MarshalJSON adds the kind tag.
func (e *StatusProgressEvidence) MarshalJSON() ([]byte, error) {
	type plain StatusProgressEvidence
	return json.Marshal(struct {
		Kind string `json:"kind"`
		*plain
	}{EvidenceStatusProgress, (*plain)(e)})
}

// freshnessLabel is each §10.1 kind in KGWUI-018's detail.
var freshnessLabel = map[string]string{FreshnessNeverWritten: "never written", FreshnessStale: "stale", FreshnessPending: "pending", FreshnessNoAncestor: "no ancestor"}

// progressCheck: KGWUI-018. It reads the resource status check's 003 warnings,
// so it runs after it; Clock, as each object's hold starts at Now.
type progressCheck struct{}

// Name implements Check.
func (progressCheck) Name() string { return "status progress" }

// Inputs implements Check: the 003s and Owned for the objects with status.
func (progressCheck) Inputs() InputSet {
	return InputSet{Owned: true, Clock: true, Checks: []string{conditionsCheck{}.Name()}}
}

// progressState is the check's Store value while the condition holds: its
// start and when each stuck object was first seen stuck.
type progressState struct {
	start time.Time
	stuck map[objects.Ref]time.Time
}

// longest keeps the first-seen time of each object in kinds, drops the others,
// and returns the earliest: when the longest-stuck object became stuck.
func (s *progressState) longest(kinds map[objects.Ref]string, now time.Time) time.Time {
	for r := range s.stuck {
		if _, ok := kinds[r]; !ok {
			delete(s.stuck, r)
		}
	}
	first := now
	for r := range kinds {
		at, ok := s.stuck[r]
		if !ok {
			at = now
			s.stuck[r] = at
		}
		if at.Before(first) {
			first = at
		}
	}
	return first
}

// Run implements Check.
func (c progressCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	res := ok(c.Name(), false, in.Now)
	out, _ := ctx.Output(conditionsCheck{}.Name())
	kinds, oldest := stuckObjects(out.Findings)
	stuck := len(kinds)
	// A controller that is not ready, or not known, raises 005 or nothing; never 018.
	d := in.Install.Deployment
	if stuck == 0 || d == nil || !overThreshold(stuck, in.withStatus()) {
		ctx.State.Delete(progressKey)
		return nil, res
	}
	if _, bad := notReady(d, "", "", "", "", true); bad {
		ctx.State.Delete(progressKey)
		return nil, res
	}
	st, _ := Load[*progressState](ctx.State, progressKey)
	if st == nil {
		st = &progressState{start: in.Now, stuck: map[objects.Ref]time.Time{}}
		ctx.State.Set(progressKey, st)
	}
	// Design §10.2: one object must have stayed stuck for the hold, not only the count.
	if due := st.longest(kinds, in.Now).Add(progressHold); in.Now.Before(due) {
		ctx.WakeAt(due)
		return nil, res
	}
	ev := &StatusProgressEvidence{Kinds: countKinds(kinds), Oldest: oldest}
	start := st.start
	return []Finding{{Code: CodeStatusNotProgressing, Slug: "status-not-progressing", Severity: SeverityWarning, Install: in.Install.ID(),
		Resource: &objects.Ref{Group: "apps", Kind: "Deployment", Namespace: d.Namespace, Name: d.Name},
		Message:  fmt.Sprintf("Status is not progressing on %d objects", stuck), Detail: progressDetail(ev), Since: &start, Facts: ev}}, res
}

// stuckObjects returns each object with a KGWUI-003 warning in fs and its
// first §10.1 kind, and the oldest of those warnings' since.
func stuckObjects(fs []Finding) (map[objects.Ref]string, time.Time) {
	kinds := map[objects.Ref]string{}
	var oldest time.Time
	for i := range fs {
		f := &fs[i]
		if f.Code != CodeRouteUnattached || f.Severity != SeverityWarning || f.Resource == nil {
			continue
		}
		kind := FreshnessNeverWritten
		if ev, ok := f.Facts.(*FreshnessEvidence); ok {
			kind = ev.Freshness
		}
		if prev, seen := kinds[*f.Resource]; !seen || freshnessRank[kind] < freshnessRank[prev] {
			kinds[*f.Resource] = kind
		}
		if f.Since != nil && (oldest.IsZero() || f.Since.Before(oldest)) {
			oldest = *f.Since
		}
	}
	return kinds, oldest
}

// countKinds counts kinds' objects per kind, in §10.1 order, leaving out zeros.
func countKinds(kinds map[objects.Ref]string) []FreshnessCount {
	n := make([]int, len(freshnessRank))
	for _, k := range kinds {
		n[freshnessRank[k]]++
	}
	var out []FreshnessCount
	for _, k := range []string{FreshnessNeverWritten, FreshnessStale, FreshnessPending, FreshnessNoAncestor} {
		if c := n[freshnessRank[k]]; c > 0 {
			out = append(out, FreshnessCount{Freshness: k, Objects: c})
		}
	}
	return out
}

// withStatus counts the objects the resource status check judges: every owned
// class, Gateway, ListenerSet and policy, and each route it does not ignore once.
func (in Input) withStatus() int {
	o := in.Owned
	n := len(o.GatewayClasses) + len(o.Gateways) + len(o.ListenerSets) + len(o.Policies)
	routes := make(map[objects.Ref]bool, len(o.Routes))
	for _, r := range o.Routes {
		if !in.ignored(r.Obj) {
			routes[objects.RefOf(r.Obj)] = true
		}
	}
	for _, u := range o.Unattached {
		if in.readable(u.Parent.Group, u.Parent.Kind) && !in.ignored(u.Route.Obj) {
			routes[objects.RefOf(u.Route.Obj)] = true
		}
	}
	return n + len(routes)
}

// overThreshold reports whether stuck of total objects start 018 (design §10.2).
func overThreshold(stuck, total int) bool {
	return stuck >= progressObjects || stuck >= progressMin && stuck*100 >= progressShare*max(total, stuck)
}

// progressDetail is 018's detail: the counts per kind.
func progressDetail(ev *StatusProgressEvidence) string {
	parts := make([]string, 0, len(ev.Kinds))
	for _, k := range ev.Kinds {
		parts = append(parts, fmt.Sprintf("%d %s", k.Objects, freshnessLabel[k.Freshness]))
	}
	return strings.Join(parts, ", ")
}
