package health

import (
	"cmp"
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
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

const (
	// RestartHold is how long a reject that disappears across a controller Pod
	// change is held before the Gateway reads clear (design §8.3).
	RestartHold = 60 * time.Second
	// xdsKey holds the xdsState in the xDS rejects check's Store.
	xdsKey = "xds"
	// proxyStatsKey is the proxy stats collector value.
	proxyStatsKey = "proxy-stats/stats"
	// RecentWindow is how long before a Gateway's first reject a change to an
	// object on it counts as recent (design §8.5).
	RecentWindow = 10 * time.Minute
	// MaxRecentlyChanged is how many recently changed objects 001 lists.
	MaxRecentlyChanged = 10
)

// Wording of KGWUI-001 (design §8.3).
const (
	xdsStatusHealthyMessage = "Status says Programmed, but proxies reject its config"
	xdsRejectMessage        = "Proxies reject this Gateway's config"
	xdsTypeStreams          = "%s rejected on %d streams."
	xdsTypeStream           = "%s rejected on 1 stream."
	xdsActiveRejects        = "%.0f active rejects"
	xdsHoldError            = "A controller Pod restarted or was replaced. Waiting up to 60 s for rejects to return"
	xdsStaleError           = "Controller metrics are stale. Last collected at %s"
)

// xdsCheck: KGWUI-001, one finding per Gateway with active rejects (design §8.3).
type xdsCheck struct{}

// Name implements Check.
func (xdsCheck) Name() string { return "xDS rejects" }

// Inputs implements Check: no Clock, as holds and staleness end through
// WakeAt; Deployments give proxy counts, proxy stats per-Pod evidence.
func (xdsCheck) Inputs() InputSet {
	return InputSet{Owned: true, Metrics: true, Process: true, Kinds: []objects.GroupKind{{Group: "apps", Kind: "Deployment"}},
		Collected: []string{proxyStatsKey}}
}

// xdsState is the check's Store value: controller Pods and their last change,
// tracked rejects_total per Pod UID, and the rejecting or held Gateways.
type xdsState struct {
	// start is the check's first successful run: objects first seen by then have no known change time.
	start     time.Time
	pods      map[string]int32
	changedAt time.Time
	totals    ctrlmetrics.PodTotals
	gateways  map[ctrlmetrics.GatewayKey]*xdsGateway
}

// xdsGateway is one Gateway's rejects; holdEnd is set while they are held.
type xdsGateway struct {
	holdEnd time.Time
	types   map[string]xdsType
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
	old, _ := Load[*xdsState](ctx.State, xdsKey)
	if old == nil {
		old = &xdsState{}
	}
	if !in.HasMetrics {
		return old.unavailable(ctx, c.Name(), "controller metrics not collected yet")
	}
	if in.Metrics.Err != "" {
		return old.unavailable(ctx, c.Name(), in.Metrics.Err)
	}
	// Recomputing the model must not make an old zero-reject result look fresh.
	if in.Metrics.Stale(in.Now, in.MetricsMaxAge) {
		return old.unavailable(ctx, c.Name(), fmt.Sprintf(xdsStaleError, in.Metrics.CollectedAt.UTC().Format(time.RFC3339)))
	}
	if in.MetricsMaxAge > 0 {
		ctx.WakeAt(in.Metrics.CollectedAt.Add(in.MetricsMaxAge + time.Nanosecond))
	}
	// Every owned Gateway has a resources_managed series (D-012); without the
	// family the endpoint or metric names are wrong, and zero rejects proves nothing.
	m := &in.Metrics.Metrics
	if n := len(in.Owned.Gateways); n > 0 && !m.ManagedPresent {
		return old.unavailable(ctx, c.Name(), fmt.Sprintf("%s missing from controller metrics although the install owns %d Gateway(s); check the metrics endpoint",
			ctrlmetrics.ResourcesManaged, n))
	}
	cur := &xdsState{start: old.start, pods: controllerPods(in), changedAt: old.changedAt}
	if cur.start.IsZero() {
		cur.start = in.Now
	}
	if old.pods != nil && podsChanged(old.pods, cur.pods) {
		cur.changedAt = in.Now
	}
	var growth map[ctrlmetrics.TypeKey]float64
	growth, cur.totals = ctrlmetrics.TotalGrowth(old.totals, trackedTotals(old, m.PerPod))
	streams := rejectStreams(m.PerPod)
	var proxies map[objects.Ref]*proxystats.Gateway
	var changes map[objects.Ref][]change
	if len(m.Rejects) > 0 {
		proxies = proxyGateways(ctx)
		changes = changesOn(in, m.Rejects, cur.start)
	}
	var fs []Finding
	held := false
	for _, gw := range in.Owned.Gateways {
		key := ctrlmetrics.GatewayKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}
		prev := old.gateways[key]
		if m.Rejects[key] > 0 {
			g := &xdsGateway{}
			fs = append(fs, xdsFinding(ctx, gw, key, prev, g, streams[key], growth, proxies, changes[*ref(gw)]))
			cur.keep(key, g)
			continue
		}
		if prev == nil {
			continue
		}
		// A reject gone within RestartHold of a controller Pod change is held.
		end := prev.holdEnd
		if end.IsZero() && !cur.changedAt.IsZero() && in.Now.Sub(cur.changedAt) < RestartHold {
			end = in.Now.Add(RestartHold)
		}
		if !in.Now.Before(end) {
			continue
		}
		g := &xdsGateway{holdEnd: end, types: make(map[string]xdsType, len(prev.types))}
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

// unavailable is KGWUI-008 for the check; the stored Gateways keep their since
// entries, so a failed scrape never restarts a reject's age.
func (s *xdsState) unavailable(ctx RunContext, name, why string) ([]Finding, CheckResult) {
	for _, gw := range ctx.Owned.Gateways {
		if s.gateways[ctrlmetrics.GatewayKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}] != nil {
			ctx.Retain(FindingID(ctx.Install.ID(), CodeXDSRejected, ref(gw), nil))
		}
	}
	return unavailable(ctx.Input, name, why)
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
	types map[string]*xdsStreams, growth map[ctrlmetrics.TypeKey]float64, proxies map[objects.Ref]*proxystats.Gateway, changes []change,
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
	var proxyPods []objects.Ref
	ev.ProxyStats, proxyPods = proxyRejects(proxies[*r])
	f.Evidence = append(f.Evidence, proxyPods...)
	onset := first
	for _, t := range ev.Types {
		if t.FirstSeen.Before(onset) {
			onset = t.FirstSeen
		}
	}
	ev.RecentlyChanged = recentlyChanged(changes, onset)
	return f
}

// change is an object on a rejecting Gateway and when its generation was first seen.
type change struct {
	ref  objects.Ref
	seen time.Time
}

// changesOn returns, per Gateway in gws, the owned routes, ListenerSets and
// policies on it (spec parents, a ListenerSet's Gateway, policy ancestors)
// whose current generation was first seen after start.
func changesOn(in Input, gws map[ctrlmetrics.GatewayKey]float64, start time.Time) map[objects.Ref][]change {
	out := map[objects.Ref][]change{}
	add := func(g objects.Ref, u *unstructured.Unstructured) {
		if g.Group != objects.GatewayAPIGroup || g.Kind != "Gateway" || gws[ctrlmetrics.GatewayKey{Namespace: g.Namespace, Name: g.Name}] <= 0 {
			return
		}
		if t := in.firstSeen(u); t.After(start) {
			out[g] = append(out[g], change{ref: objects.RefOf(u), seen: t})
		}
	}
	lsGW := make(map[objects.Ref]objects.Ref, len(in.Owned.ListenerSets))
	for _, u := range in.Owned.ListenerSets {
		g := in.views().ListenerSet(u).Parent.Ref()
		lsGW[objects.RefOf(u)] = g
		add(g, u)
	}
	gateway := func(r objects.Ref) objects.Ref {
		if g, ok := lsGW[r]; ok {
			return g
		}
		return r
	}
	for _, r := range in.Owned.Routes {
		for _, p := range r.Parents {
			add(gateway(p.Ref()), r.Obj)
		}
	}
	for _, p := range in.Owned.Policies {
		for _, a := range objects.PolicyAncestors(p.Obj) {
			add(gateway(a.AncestorRef.Ref()), p.Obj)
		}
	}
	return out
}

// recentlyChanged is cs first seen in the RecentWindow up to onset, newest
// first, ties by ref, at most MaxRecentlyChanged.
func recentlyChanged(cs []change, onset time.Time) []objects.Ref {
	from := onset.Add(-RecentWindow)
	in := make([]change, 0, len(cs))
	for _, c := range cs {
		if !c.seen.Before(from) && !c.seen.After(onset) {
			in = append(in, c)
		}
	}
	if len(in) == 0 {
		return nil
	}
	slices.SortFunc(in, func(a, b change) int { return cmp.Or(b.seen.Compare(a.seen), compareRefs(a.ref, b.ref)) })
	in = slices.CompactFunc(in, func(a, b change) bool { return a.ref == b.ref })
	out := make([]objects.Ref, 0, min(len(in), MaxRecentlyChanged))
	for _, c := range in[:min(len(in), MaxRecentlyChanged)] {
		out = append(out, c.ref)
	}
	return out
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
