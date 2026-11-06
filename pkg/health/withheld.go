package health

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
)

// CodeConfigWithheld is KGWUI-024: kgateway 2.5's per-client metrics show
// withheld or early-released proxy config (design §8.6).
const CodeConfigWithheld = "KGWUI-024"

// WithheldReleaseWindow is how long an alerting bounded publish keeps KGWUI-024's warning.
const WithheldReleaseWindow = 15 * time.Minute

// withheldKey holds the withheldState in the check's Store.
const withheldKey = "withheld"

// releaseModes are the bounded-publish modes upstream says to alert on: routes fail.
var releaseModes = []string{"carry_expired", "flip_release", "warm_release"}

// Wording of KGWUI-024 (design §8.6). Draft wording, finalised in the wording pass.
const (
	withheldMessage        = "Proxies get no config updates, because referenced clusters are missing"
	withheldDetail         = "The controller withholds config from %d of this Gateway's proxies."
	withheldDetailOne      = "The controller withholds config from 1 of this Gateway's proxies."
	releasedMessage        = "Some routes fail: config was released before every cluster was ready"
	releasedDetail         = "%d releases in the last 15 minutes (%s)."
	releasedDetailOne      = "1 release in the last 15 minutes (%s)."
	withheldNotCollected   = "Controller metrics not collected yet"
	withheldVersionDetail  = "The controller reports version %s, but its Deployment says %s. The UI uses %s."
	withheldNoDeployDetail = "The controller reports version %s; its Deployment has no version. The UI uses %s."
)

// WithheldEvidence is one Gateway's per-client publication metrics behind
// KGWUI-024, summed over controller Pods.
type WithheldEvidence struct {
	// Withheld counts the Gateway's warm proxies still withheld after the publish budget.
	Withheld int `json:"withheld,omitempty"`
	// Releases are the alerting bounded publishes in the last 15 minutes, by mode.
	Releases []ModeCount `json:"releases,omitempty"`
	// ReportedBy are the controller Pods behind Withheld and Releases, sorted.
	ReportedBy []string `json:"reportedBy"`
}

// ModeCount is how many bounded publishes of one mode a Gateway had.
type ModeCount struct {
	Mode  string `json:"mode"`
	Count int    `json:"count"`
}

// EvidenceWithheld is the kind of WithheldEvidence.
const EvidenceWithheld = "withheld"

// EvidenceKind implements Evidence.
func (*WithheldEvidence) EvidenceKind() string { return EvidenceWithheld }

// MarshalJSON adds the kind tag.
func (e *WithheldEvidence) MarshalJSON() ([]byte, error) {
	type plain WithheldEvidence
	return json.Marshal(struct {
		Kind string `json:"kind"`
		*plain
	}{EvidenceWithheld, (*plain)(e)})
}

// withheldState is the check's Store value: bounded_publishes_total per Pod
// UID, and the alerting releases still inside the window.
type withheldState struct {
	totals   ctrlmetrics.PodBounded
	releases []release
}

// release is one scrape's growth of one alerting mode on one Gateway from one Pod.
type release struct {
	at  time.Time
	key ctrlmetrics.ModeKey
	pod string
	n   float64
}

// withheldCheck: KGWUI-024, an error per Gateway with withheld proxies and a
// warning per Gateway with an alerting release in the last 15 minutes.
type withheldCheck struct{}

// Name implements Check.
func (withheldCheck) Name() string { return "withheld config" }

// Inputs implements Check: the window and staleness end through WakeAt.
func (withheldCheck) Inputs() InputSet { return InputSet{Owned: true, Metrics: true} }

// Run implements Check. Missing, failed or stale metrics are not available and
// leave the Store as it is; a scrape without the 2.5 families is not on version.
func (c withheldCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	switch {
	case !in.HasMetrics:
		return nil, NotAvailable(c.Name(), AvailFailing, withheldNotCollected, in.Now)
	case in.Metrics.Err != "":
		return nil, NotAvailable(c.Name(), AvailFailing, in.Metrics.Err, in.Now)
	case in.Metrics.Stale(in.Now, in.MetricsMaxAge):
		return nil, NotAvailable(c.Name(), AvailFailing, fmt.Sprintf(xdsStaleError, in.Metrics.CollectedAt.UTC().Format(time.RFC3339)), in.Now)
	}
	if in.MetricsMaxAge > 0 {
		ctx.WakeAt(in.Metrics.CollectedAt.Add(in.MetricsMaxAge + time.Nanosecond))
	}
	pods := in.Metrics.Metrics.PerPod
	if !slices.ContainsFunc(pods, func(p ctrlmetrics.PodMetrics) bool { return p.PerClientPresent }) {
		ctx.State.Delete(withheldKey)
		return nil, NotAvailable(c.Name(), AvailNotOnVersion, "", in.Metrics.CollectedAt)
	}
	old, _ := Load[*withheldState](ctx.State, withheldKey)
	if old == nil {
		old = &withheldState{}
	}
	cur := &withheldState{}
	for _, r := range old.releases {
		if in.Now.Sub(r.at) < WithheldReleaseWindow {
			cur.releases = append(cur.releases, r)
		}
	}
	cur.totals = cur.track(in.Now, old.totals, pods)
	ctx.State.Set(withheldKey, cur)
	var fs []Finding
	var gws []*unstructured.Unstructured
	if in.Owned != nil {
		gws = in.Owned.Gateways
	}
	for _, gw := range gws {
		key := ctrlmetrics.GatewayKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}
		ev, first := cur.evidence(key, pods)
		if ev == nil {
			continue
		}
		r := ref(gw)
		// The Pod names are the controller Pods' data, so a caller who may not view them gets no evidence.
		var podRefs []objects.Ref
		for _, p := range ev.ReportedBy {
			podRefs = append(podRefs, process.PodRef(in.Install.Namespace, process.Process{Name: p}))
		}
		if ev.Withheld > 0 {
			d := withheldDetailOne
			if ev.Withheld != 1 {
				d = fmt.Sprintf(withheldDetail, ev.Withheld)
			}
			fs = append(fs, Finding{Code: CodeConfigWithheld, Slug: "xds-config-withheld", Severity: SeverityError, Install: in.Install.ID(), Resource: r,
				Message: withheldMessage, Detail: d, Key: []string{"withheld"}, Evidence: podRefs, Facts: ev})
		}
		if len(ev.Releases) > 0 {
			ctx.WakeAt(first.Add(WithheldReleaseWindow))
			fs = append(fs, Finding{Code: CodeConfigWithheld, Slug: "xds-config-withheld", Severity: SeverityWarning, Install: in.Install.ID(), Resource: r,
				Message: releasedMessage, Detail: releasedText(ev.Releases), Key: []string{"released"}, Evidence: podRefs, Facts: ev})
		}
	}
	res := ok(c.Name(), false, in.Metrics.CollectedAt)
	res.Detail = versionDisagreement(in)
	return fs, res
}

// track records the alerting releases since prev at now and returns the totals for next time.
func (s *withheldState) track(now time.Time, prev ctrlmetrics.PodBounded, pods []ctrlmetrics.PodMetrics) ctrlmetrics.PodBounded {
	next := ctrlmetrics.PodBounded{}
	for _, p := range pods {
		last, seen := prev[p.UID]
		next[p.UID] = maps.Clone(p.Bounded)
		if !seen {
			continue // a Pod first seen is a baseline
		}
		growth, _ := ctrlmetrics.BoundedGrowth(ctrlmetrics.PodBounded{p.UID: last}, []ctrlmetrics.PodMetrics{p})
		for _, k := range slices.SortedFunc(maps.Keys(growth), compareModeKey) {
			if slices.Contains(releaseModes, k.Mode) {
				s.releases = append(s.releases, release{at: now, key: k, pod: p.Name, n: growth[k]})
			}
		}
	}
	return next
}

// evidence is gw's withheld proxies and windowed releases, and the first
// release's time; nil when it has neither.
func (s *withheldState) evidence(gw ctrlmetrics.GatewayKey, pods []ctrlmetrics.PodMetrics) (*WithheldEvidence, time.Time) {
	ev := &WithheldEvidence{}
	withheld := 0.0
	for _, p := range pods {
		if n := p.Withheld[gw]; n > 0 {
			withheld += n
			ev.ReportedBy = append(ev.ReportedBy, p.Name)
		}
	}
	ev.Withheld = int(math.Round(withheld))
	counts := map[string]float64{}
	var first time.Time
	for _, r := range s.releases {
		if r.key.Gateway != gw {
			continue
		}
		counts[r.key.Mode] += r.n
		if first.IsZero() || r.at.Before(first) {
			first = r.at
		}
		ev.ReportedBy = append(ev.ReportedBy, r.pod)
	}
	for _, m := range slices.Sorted(maps.Keys(counts)) {
		ev.Releases = append(ev.Releases, ModeCount{Mode: m, Count: int(math.Round(counts[m]))})
	}
	if ev.Withheld == 0 && len(ev.Releases) == 0 {
		return nil, time.Time{}
	}
	slices.Sort(ev.ReportedBy)
	ev.ReportedBy = slices.Compact(ev.ReportedBy)
	return ev, first
}

func compareModeKey(a, b ctrlmetrics.ModeKey) int {
	return cmp.Or(cmp.Compare(a.Gateway.Namespace, b.Gateway.Namespace), cmp.Compare(a.Gateway.Name, b.Gateway.Name), cmp.Compare(a.Mode, b.Mode))
}

// releasedText is the warning's detail: the total and each mode with its count.
func releasedText(rs []ModeCount) string {
	total := 0
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		total += r.Count
		parts = append(parts, fmt.Sprintf("%s %d", r.Mode, r.Count))
	}
	if total == 1 {
		return fmt.Sprintf(releasedDetailOne, strings.Join(parts, ", "))
	}
	return fmt.Sprintf(releasedDetail, total, strings.Join(parts, ", "))
}

// versionDisagreement names both versions when build_info and the Deployment
// disagree; "" when they agree or the controller reports none.
func versionDisagreement(in Input) string {
	rep, dep := in.Install.ReportedVersion, strings.TrimPrefix(in.Install.Version, "v")
	switch {
	case rep == "" || rep == dep:
		return ""
	case dep == "":
		return fmt.Sprintf(withheldNoDeployDetail, rep, in.Install.KgatewayVersion())
	}
	return fmt.Sprintf(withheldVersionDetail, rep, dep, in.Install.KgatewayVersion())
}
