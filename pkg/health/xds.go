package health

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
)

const (
	// RestartHold is how long a reject that disappears across a controller Pod
	// change is held before the Gateway reads clear (design §8.3).
	RestartHold = 60 * time.Second
	// xdsKey holds the xdsState in the xDS rejects check's Store.
	xdsKey = "xds"
	// proxyStatsKey is the proxy stats collector value (Task 42).
	proxyStatsKey = "proxy-stats/stats"
)

// Wording of KGWUI-001 (design §8.3); Task 51 runs the wording pass.
const (
	xdsStatusHealthyMessage = "Status says Programmed, but proxies reject its config"
	xdsRejectMessage        = "Proxies reject this Gateway's config"
	xdsTypeStreams          = "%s rejected on %d streams."
	xdsTypeStream           = "%s rejected on 1 stream."
	xdsActiveRejects        = "%.0f active rejects"
	xdsHoldError            = "A controller Pod restarted or was replaced; waiting up to 60 s for proxies to reconnect"
	xdsStaleError           = "controller metrics are stale (last collected at %s)"
)

// xdsCheck: KGWUI-001, one finding per Gateway with active rejects (design §8.3).
type xdsCheck struct{}

// Name implements Check.
func (xdsCheck) Name() string { return "xDS rejects" }

// Inputs implements Check: no Clock, as the restart hold and metrics
// staleness end through WakeAt; Deployments give the proxy counts.
func (xdsCheck) Inputs() InputSet {
	return InputSet{Owned: true, Metrics: true, Process: true, Kinds: []objects.GroupKind{{Group: "apps", Kind: "Deployment"}},
		Collected: []string{proxyStatsKey}}
}

// xdsState is the check's Store value: the controller Pods and when one last
// changed, the rejects_total of tracked types per Pod UID, and the Gateways
// that reject or are held.
type xdsState struct {
	pods      map[string]int32
	changedAt time.Time
	totals    ctrlmetrics.PodTotals
	gateways  map[ctrlmetrics.GatewayKey]*xdsGateway
}

// xdsGateway is one Gateway's rejects: lastSeen is the CollectedAt of the last
// scrape that showed them; holdEnd is set while they are held.
type xdsGateway struct {
	lastSeen, holdEnd time.Time
	types             map[string]xdsType
}

// xdsType is one rejected type's first-seen time and rejects_total growth since.
type xdsType struct {
	firstSeen time.Time
	growth    float64
}

// xdsStreams are one type's rejecting streams and the Pods that report them.
type xdsStreams struct {
	streams float64
	pods    []string
}

// Run implements Check. Missing, failed or stale metrics, or a scrape without
// the resources_managed family while Gateways are owned, are KGWUI-008 and
// leave the Store as it is, so a failed scrape adds no rejects_total growth.
func (c xdsCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	if !in.HasMetrics {
		return unavailable(in, c.Name(), "controller metrics not collected yet")
	}
	if in.Metrics.Err != "" {
		return unavailable(in, c.Name(), in.Metrics.Err)
	}
	// Recomputing the model must not make an old zero-reject result look fresh.
	if in.Metrics.Stale(in.Now, in.MetricsMaxAge) {
		return unavailable(in, c.Name(), fmt.Sprintf(xdsStaleError, in.Metrics.CollectedAt.UTC().Format(time.RFC3339)))
	}
	if in.MetricsMaxAge > 0 {
		ctx.WakeAt(in.Metrics.CollectedAt.Add(in.MetricsMaxAge + time.Nanosecond))
	}
	// Every owned Gateway has a resources_managed series (D-012); without the
	// family the endpoint or metric names are wrong, and zero rejects proves nothing.
	m := &in.Metrics.Metrics
	if n := len(in.Owned.Gateways); n > 0 && !m.ManagedPresent {
		return unavailable(in, c.Name(), fmt.Sprintf("%s missing from controller metrics although the install owns %d Gateway(s); check the metrics endpoint",
			ctrlmetrics.ResourcesManaged, n))
	}
	old, _ := Load[*xdsState](ctx.State, xdsKey)
	if old == nil {
		old = &xdsState{}
	}
	cur := &xdsState{pods: controllerPods(in), changedAt: old.changedAt}
	if old.pods != nil && podsChanged(old.pods, cur.pods) {
		cur.changedAt = in.Now
	}
	var growth map[ctrlmetrics.TypeKey]float64
	growth, cur.totals = ctrlmetrics.TotalGrowth(old.totals, trackedTotals(old, m.PerPod))
	streams := rejectStreams(m.PerPod)
	var fs []Finding
	held := false
	for _, gw := range in.Owned.Gateways {
		key := ctrlmetrics.GatewayKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}
		prev := old.gateways[key]
		if m.Rejects[key] > 0 {
			g := &xdsGateway{lastSeen: in.Metrics.CollectedAt}
			fs = append(fs, xdsFinding(ctx, gw, key, prev, g, streams[key], growth))
			cur.keep(key, g)
			continue
		}
		if prev == nil {
			continue
		}
		// A reject gone after a controller Pod change since the scrape that showed it is held.
		end := prev.holdEnd
		if end.IsZero() && !cur.changedAt.IsZero() && !cur.changedAt.Before(prev.lastSeen) {
			end = in.Now.Add(RestartHold)
		}
		if !in.Now.Before(end) {
			continue
		}
		g := &xdsGateway{lastSeen: prev.lastSeen, holdEnd: end, types: make(map[string]xdsType, len(prev.types))}
		for t, ty := range prev.types {
			ty.growth += growth[ctrlmetrics.TypeKey{Gateway: key, TypeURL: t}]
			g.types[t] = ty
		}
		cur.keep(key, g)
		ctx.Retain(FindingID(in.Install.ID(), CodeXDSRejected, ref(gw), nil))
		ctx.WakeAt(end)
		held = true
	}
	ctx.State.Set(xdsKey, cur)
	if held {
		return fs, CheckResult{Name: c.Name(), Required: true, Pending: true, CollectedAt: in.Metrics.CollectedAt, Error: xdsHoldError}
	}
	return fs, ok(c.Name(), true, in.Metrics.CollectedAt)
}

func (s *xdsState) keep(k ctrlmetrics.GatewayKey, g *xdsGateway) {
	if s.gateways == nil {
		s.gateways = map[ctrlmetrics.GatewayKey]*xdsGateway{}
	}
	s.gateways[k] = g
}

// xdsFinding is 001 on gw. A Gateway first seen now takes its since entry as
// first seen, so types and the finding's since agree (design §5.1).
func xdsFinding(ctx RunContext, gw *unstructured.Unstructured, key ctrlmetrics.GatewayKey, prev, g *xdsGateway,
	types map[string]*xdsStreams, growth map[ctrlmetrics.TypeKey]float64,
) Finding {
	in := ctx.Input
	r := ref(gw)
	first := in.Now
	if prev == nil {
		if t, seen := ctx.State.Get(sincePrefix + FindingID(in.Install.ID(), CodeXDSRejected, r, nil)); seen {
			first, _ = t.(time.Time)
		}
	}
	ev := &XDSRejectsEvidence{Types: make([]XDSTypeRejects, 0, len(types)), StatusSaysHealthy: statusSaysHealthy(in, gw)}
	g.types = make(map[string]xdsType, len(types))
	var pods []string
	var detail strings.Builder
	for _, t := range slices.Sorted(maps.Keys(types)) {
		s := types[t]
		ty, known := xdsType{firstSeen: first}, false
		if prev != nil {
			if ty, known = prev.types[t]; known {
				ty.growth += growth[ctrlmetrics.TypeKey{Gateway: key, TypeURL: t}]
			} else {
				ty = xdsType{firstSeen: in.Now}
			}
		}
		g.types[t] = ty
		slices.Sort(s.pods)
		n := int(math.Round(s.streams))
		ev.Types = append(ev.Types, XDSTypeRejects{TypeURL: t, Streams: n, ReportedBy: s.pods, FirstSeen: ty.firstSeen.UTC(), TotalSinceFirstSeen: int(math.Round(ty.growth))})
		pods = append(pods, s.pods...)
		if detail.Len() > 0 {
			detail.WriteByte(' ')
		}
		if short := t[strings.LastIndexByte(t, '.')+1:]; n == 1 {
			fmt.Fprintf(&detail, xdsTypeStream, short)
		} else {
			fmt.Fprintf(&detail, xdsTypeStreams, short, n)
		}
	}
	if d := process.ProxyDeployment(*r, in.Snapshot.Deployments, in.Install.ProxySelector); d != nil {
		ev.Proxies = &ProxyCount{Ready: d.Status.ReadyReplicas, Desired: Desired(d)}
	}
	f := Finding{Code: CodeXDSRejected, Slug: "xds-rejected", Severity: SeverityError, Install: in.Install.ID(), Resource: r,
		Message: xdsRejectMessage, Detail: detail.String(), Facts: ev}
	if f.Detail == "" {
		f.Detail = fmt.Sprintf(xdsActiveRejects, in.Metrics.Metrics.Rejects[key])
	}
	if ev.StatusSaysHealthy {
		f.Message = xdsStatusHealthyMessage
	}
	slices.Sort(pods)
	for _, p := range slices.Compact(pods) {
		f.Evidence = append(f.Evidence, process.PodRef(in.Install.Namespace, process.Process{Name: p}))
	}
	return f
}

// statusSaysHealthy reports whether gw's status is current and Programmed=True.
func statusSaysHealthy(in Input, gw *unstructured.Unstructured) bool {
	cs := in.views().Gateway(gw).Conditions
	if pendingReason(gw, cs, gatewayRequired, nil) != "" {
		return false
	}
	c := objects.FindCondition(cs, "Programmed")
	return c != nil && c.Status == metav1.ConditionTrue
}

// controllerPods are the install's controller Pod IDs and restart counts, from
// process data and the scraped Pods; a scraped Pod alone has 0 restarts.
func controllerPods(in Input) map[string]int32 {
	out := map[string]int32{}
	if in.Process != nil {
		for _, p := range in.Process.Controller.Processes {
			if p.ID == "" {
				continue
			}
			out[p.ID] = 0
			if p.Restarts != nil {
				out[p.ID] = *p.Restarts
			}
		}
	}
	for _, p := range in.Metrics.Metrics.PerPod {
		if _, known := out[p.UID]; !known && p.UID != "" {
			out[p.UID] = 0
		}
	}
	return out
}

// podsChanged reports a Pod ID in cur that old lacks, or a higher restart count.
func podsChanged(old, cur map[string]int32) bool {
	for id, n := range cur {
		if prev, known := old[id]; !known || n > prev {
			return true
		}
	}
	return false
}

// trackedTotals is pods with Total cut to the types the check tracks after
// this run: those of s's Gateways and those active now.
func trackedTotals(s *xdsState, pods []ctrlmetrics.PodMetrics) []ctrlmetrics.PodMetrics {
	keep := func(k ctrlmetrics.TypeKey) bool {
		if g := s.gateways[k.Gateway]; g != nil {
			if _, ok := g.types[k.TypeURL]; ok {
				return true
			}
		}
		for _, p := range pods {
			if p.Active[k] > 0 {
				return true
			}
		}
		return false
	}
	out := make([]ctrlmetrics.PodMetrics, len(pods))
	for i, p := range pods {
		out[i] = ctrlmetrics.PodMetrics{UID: p.UID, Total: map[ctrlmetrics.TypeKey]float64{}}
		for k, v := range p.Total {
			if keep(k) {
				out[i].Total[k] = v
			}
		}
	}
	return out
}

// rejectStreams sums rejects_active per Gateway and type over pods, with the
// Pods that report a positive value.
func rejectStreams(pods []ctrlmetrics.PodMetrics) map[ctrlmetrics.GatewayKey]map[string]*xdsStreams {
	out := map[ctrlmetrics.GatewayKey]map[string]*xdsStreams{}
	for _, p := range pods {
		for k, v := range p.Active {
			if v <= 0 {
				continue
			}
			byType := out[k.Gateway]
			if byType == nil {
				byType = map[string]*xdsStreams{}
				out[k.Gateway] = byType
			}
			s := byType[k.TypeURL]
			if s == nil {
				s = &xdsStreams{}
				byType[k.TypeURL] = s
			}
			s.streams += v
			s.pods = append(s.pods, p.Name)
		}
	}
	return out
}
