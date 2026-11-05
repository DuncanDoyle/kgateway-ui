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

// progressHold is how long the 018 condition holds before it matures; it is
// also the window of the status syncs the evidence quotes.
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
// kind, the oldest KGWUI-003 warning's since, and the status syncs over the
// last 5 min. Syncs is nil when unknown, which never means zero (D-006).
type StatusProgressEvidence struct {
	Kinds  []FreshnessCount `json:"kinds"`
	Oldest time.Time        `json:"oldest"`
	Syncs  *int64           `json:"syncs,omitempty"`
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
// so it runs after it; Clock, as the syncs window moves with Now.
type progressCheck struct{}

// Name implements Check.
func (progressCheck) Name() string { return "status progress" }

// Inputs implements Check: the 003s, Owned for the objects with status, and the syncs metric.
func (progressCheck) Inputs() InputSet {
	return InputSet{Owned: true, Metrics: true, Clock: true, Checks: []string{conditionsCheck{}.Name()}}
}

// progressState is the check's Store value while the condition holds: its
// start, and status_syncs_total per Pod UID at each metrics collection since.
type progressState struct {
	start   time.Time
	samples []syncSample
}

// syncSample is status_syncs_total per Pod UID, collected at at.
type syncSample struct {
	at   time.Time
	pods map[string]float64
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
		st = &progressState{start: in.Now}
		ctx.State.Set(progressKey, st)
	}
	fresh := st.record(in)
	if due := st.start.Add(progressHold); in.Now.Before(due) {
		ctx.WakeAt(due)
		return nil, res
	}
	ev := &StatusProgressEvidence{Kinds: countKinds(kinds), Oldest: oldest}
	if fresh {
		ev.Syncs = st.syncs()
	}
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

// progressDetail is 018's detail: the counts per kind, then the syncs when known.
func progressDetail(ev *StatusProgressEvidence) string {
	parts := make([]string, 0, len(ev.Kinds))
	for _, k := range ev.Kinds {
		parts = append(parts, fmt.Sprintf("%d %s", k.Objects, freshnessLabel[k.Freshness]))
	}
	d := strings.Join(parts, ", ")
	if ev.Syncs != nil {
		d += fmt.Sprintf(". Status syncs in the last 5 min: %d", *ev.Syncs)
	}
	return d
}

// record keeps in's status_syncs_total per Pod UID as a sample and reports
// whether in's metrics are current and carry the family.
func (s *progressState) record(in Input) bool {
	m := in.Metrics
	if !in.HasMetrics || m.Err != "" || m.Stale(in.Now, in.MetricsMaxAge) {
		return false
	}
	pods := map[string]float64{}
	for _, p := range m.Metrics.PerPod {
		if p.StatusSyncsPresent {
			pods[p.UID] = p.StatusSyncs
		}
	}
	if len(pods) == 0 {
		return false
	}
	if n := len(s.samples); n == 0 || !s.samples[n-1].at.Equal(m.CollectedAt) {
		s.samples = append(s.samples, syncSample{at: m.CollectedAt, pods: pods})
	}
	return true
}

// syncs returns the status syncs over the progressHold before the newest
// sample, and drops the samples older than the window needs. It is nil when no
// sample is that old. Growth is per Pod UID: a counter that went down was
// reset and counts its new value; a UID seen for the first time sets a baseline.
func (s *progressState) syncs() *int64 {
	last := s.samples[len(s.samples)-1].at
	base := -1
	for i, x := range s.samples {
		if !x.at.After(last.Add(-progressHold)) {
			base = i
		}
	}
	if base < 0 {
		return nil
	}
	s.samples = s.samples[base:]
	var g float64
	for i := 1; i < len(s.samples); i++ {
		for uid, v := range s.samples[i].pods {
			if p, seen := s.samples[i-1].pods[uid]; seen {
				if v >= p {
					g += v - p
				} else {
					g += v
				}
			}
		}
	}
	n := int64(g)
	return &n
}
