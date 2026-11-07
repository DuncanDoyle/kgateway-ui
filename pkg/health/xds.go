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

	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/ctrllogs"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/process"
	"github.com/DuncanDoyle/snellius/pkg/proxystats"
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
	// WithheldAfter is how long after the latest start a deferral line counts,
	// and how far apart two such lines must be (R105).
	WithheldAfter = 60 * time.Second
	// WithheldWindow is how long after the last such line the Gateway reads withheld.
	WithheldWindow = 15 * time.Minute
)

// WithheldMessage labels withheld config in a Gateway's Data plane section (R105).
const WithheldMessage = "Proxies may not be getting config updates"

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
// WakeAt; Deployments give proxy counts, proxy stats per-Pod evidence and
// controller logs the rejects' log events.
func (xdsCheck) Inputs() InputSet {
	return InputSet{Owned: true, Metrics: true, Process: true, Kinds: []objects.GroupKind{{Group: "apps", Kind: "Deployment"}},
		Collected: []string{proxyStatsKey, ctrllogs.Key}}
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

// Run implements Check: the rejects, and on every result the Gateways whose
// config may be withheld, which the controller log tells without metrics.
func (c xdsCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	fs, res := c.rejects(ctx)
	res.Withheld, res.ControllerLogs = withheld(ctx)
	return fs, res
}

// rejects is 001. Missing, failed or stale metrics, or a scrape without
// the resources_managed family while Gateways are owned, are KGWUI-008 and
// leave the Store as it is, so a failed scrape adds no rejects_total growth.
func (c xdsCheck) rejects(ctx RunContext) ([]Finding, CheckResult) {
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
	lg := xdsLogs{start: cur.start, slack: in.MetricsMaxAge, namer: in.Rules.EnvoyNamer()}
	if len(m.Rejects) > 0 {
		proxies = proxyGateways(ctx)
		changes = changesOn(in, m.Rejects, cur.start)
		lg.events, lg.state = installLogs(ctx)
		lg.byGateway = byGateway(lg.events, nil)
	}
	var fs []Finding
	held := false
	for _, gw := range in.Owned.Gateways {
		key := ctrlmetrics.GatewayKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}
		prev := old.gateways[key]
		if m.Rejects[key] > 0 {
			g := &xdsGateway{}
			fs = append(fs, xdsFinding(ctx, gw, key, prev, g, streams[key], growth, proxies, changes[*ref(gw)], lg))
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
// first seen, so types and the finding's since agree (design §5.1); a log
// line of the reject lowers both, and the lowered since outlives the line.
func xdsFinding(ctx RunContext, gw *unstructured.Unstructured, key ctrlmetrics.GatewayKey, prev, g *xdsGateway,
	types map[string]*xdsStreams, growth map[ctrlmetrics.TypeKey]float64, proxies map[objects.Ref]*proxystats.Gateway, changes []change, lg xdsLogs,
) Finding {
	in := ctx.Input
	r := ref(gw)
	sinceKey := sincePrefix + FindingID(in.Install.ID(), CodeXDSRejected, r, nil)
	first := in.Now
	stored, hasSince := ctx.State.Get(sinceKey)
	if prev == nil && hasSince {
		first, _ = stored.(time.Time)
	}
	ev := &XDSRejectsEvidence{Types: make([]XDSTypeRejects, 0, len(types)), StatusSaysHealthy: statusSaysHealthy(in, gw), ControllerLogs: lg.state}
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
		if le, ok := lg.event(*r, t, ty.firstSeen); ok {
			ty.firstSeen = minTime(ty.firstSeen, le.Since)
			ev.LogEvents = append(ev.LogEvents, le)
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
	ev.ProxyStats, proxyPods = proxyRejects(proxies[*r], in.Rules.EnvoyNamer())
	f.Evidence = append(f.Evidence, proxyPods...)
	onset := first
	for _, t := range ev.Types {
		if t.FirstSeen.Before(onset) {
			onset = t.FirstSeen
		}
	}
	// Only a log line moves onset before the since entry; identify reads the entry.
	if old, _ := stored.(time.Time); onset.Before(in.Now) && (!hasSince || onset.Before(old)) {
		ctx.State.Set(sinceKey, onset)
	}
	ev.RecentlyChanged = recentlyChanged(changes, onset)
	return f
}

// xdsLogs are the controller log events 001 reads for one install.
type xdsLogs struct {
	events []ctrllogs.Event
	// byGateway indexes events per Gateway, oldest first.
	byGateway map[objects.Ref][]int32
	state     string
	// start is the check's first run: before it the UI saw no metrics, so a line then may begin the reject.
	start time.Time
	// slack is how long before first seen a line still belongs to the reject: one scrape's lag.
	slack time.Duration
	namer computed.EnvoyNamer
}

// event is the one log event of gw's rejected type t (no type URL prefix),
// whichever replica logged its lines: lines from firstSeen less slack on, and
// lines from before start when t was rejecting at the first run. A type first
// seen later started while the UI watched, so an older line is another episode.
// False when there is none.
func (lg xdsLogs) event(gw objects.Ref, t string, firstSeen time.Time) (XDSLogEvent, bool) {
	from := firstSeen.Add(-lg.slack)
	backfill := !firstSeen.After(lg.start)
	var out XDSLogEvent
	for _, i := range lg.byGateway[gw] {
		e := &lg.events[i]
		ofType := e.TypeURL != "" && strings.TrimPrefix(e.TypeURL, xdsTypePrefix) == t
		// A line before from counts only as backfill from before the first run.
		early := e.Time.Before(from)
		if !ofType || early && (!backfill || !e.Time.Before(lg.start)) {
			continue
		}
		if out.TypeURL == "" || e.Time.Before(out.Since) {
			out.TypeURL, out.Since = t, e.Time
		}
		// Kept once each: a Gateway's lines repeat the same few classes and names.
		if !slices.Contains(out.Classes, e.Class) {
			out.Classes = append(out.Classes, e.Class)
		}
		for _, n := range e.Names {
			if !slices.Contains(out.EnvoyResources, n) {
				out.EnvoyResources = append(out.EnvoyResources, n)
			}
			if o, mapped := lg.object(n); mapped {
				if out.Objects == nil {
					out.Objects = map[string]objects.Ref{}
				}
				out.Objects[n] = o
			}
		}
	}
	if out.TypeURL == "" {
		return XDSLogEvent{}, false
	}
	slices.Sort(out.Classes)
	out.Classes = slices.Compact(out.Classes)
	slices.Sort(out.EnvoyResources)
	out.EnvoyResources = slices.Compact(out.EnvoyResources)
	return out, true
}

// object maps Envoy name n to its object through the rule set's naming function.
func (lg xdsLogs) object(n string) (objects.Ref, bool) {
	if lg.namer == nil {
		return objects.Ref{}, false
	}
	return lg.namer.Object(n)
}

// byGateway indexes the events keep accepts by Gateway, each list oldest first
// as events are; nil keep accepts every event with a Gateway. One backing array holds the lists.
func byGateway(events []ctrllogs.Event, keep func(*ctrllogs.Event) bool) map[objects.Ref][]int32 {
	n := map[objects.Ref]int{}
	total := 0
	for i := range events {
		if e := &events[i]; e.Gateway != (objects.Ref{}) && (keep == nil || keep(e)) {
			n[e.Gateway]++
			total++
		}
	}
	if total == 0 {
		return nil
	}
	buf := make([]int32, total)
	out := make(map[objects.Ref][]int32, len(n))
	at := 0
	for g, c := range n {
		out[g], at = buf[at:at:at+c], at+c
	}
	for i := range events {
		if e := &events[i]; e.Gateway != (objects.Ref{}) && (keep == nil || keep(e)) {
			out[e.Gateway] = append(out[e.Gateway], int32(i)) //nolint:gosec // at most ctrllogs.MaxEvents
		}
	}
	return out
}

// xdsTypePrefix is the prefix the metrics strip from a type URL and log events keep.
const xdsTypePrefix = "type.googleapis.com/"

// installLogs is the install's derived controller log events, oldest first,
// and LogsRead; nil and LogsNotRead unless the install's log reading is on.
func installLogs(ctx RunContext) ([]ctrllogs.Event, string) {
	v, have := ctx.Collected(ctrllogs.Key)
	if !have || v.State != collect.OK {
		return nil, LogsNotRead
	}
	logs, _ := v.Data.(ctrllogs.Logs)
	l, ok := logs[ctx.Install.ID()]
	if !ok || l.State != ctrllogs.StateOn {
		return nil, LogsNotRead
	}
	return l.Events, LogsRead
}

// withheld are the owned Gateways whose snapshot deferral recurred: two
// deferral lines at least WithheldAfter apart, both at least WithheldAfter
// after the latest start of every controller Pod and of the Gateway's proxy
// Pods, the last within WithheldWindow (R105). A startup burst or one
// reconnect never qualifies; unknown starts never do. It also returns
// whether the log was read, as no withheld Gateway means nothing otherwise.
func withheld(ctx RunContext) ([]WithheldGateway, string) {
	events, state := installLogs(ctx)
	if state != LogsRead || ctx.Process == nil {
		return nil, state
	}
	ctrl, known := latestStart(ctx.Process.Controller)
	if !known {
		return nil, state
	}
	deferrals := byGateway(events, deferral)
	if len(deferrals) == 0 {
		return nil, state
	}
	var out []WithheldGateway
	for _, gw := range ctx.Owned.Gateways {
		r := *ref(gw)
		if len(deferrals[r]) == 0 {
			continue
		}
		proxies, known := latestStart(ctx.Process.Proxies[r])
		if !known {
			continue
		}
		from := ctrl
		if proxies.After(from) {
			from = proxies
		}
		from = from.Add(WithheldAfter)
		var w WithheldGateway
		for _, i := range deferrals[r] {
			e := &events[i]
			if e.Time.Before(from) {
				continue
			}
			if w.First.IsZero() || e.Time.Before(w.First) {
				w.First = e.Time
			}
			if e.Time.After(w.Last) {
				w.Last = e.Time
			}
		}
		if w.First.IsZero() || w.Last.Sub(w.First) < WithheldAfter || !ctx.Now.Before(w.Last.Add(WithheldWindow)) {
			continue
		}
		w.Gateway = r
		ctx.WakeAt(w.Last.Add(WithheldWindow))
		out = append(out, w)
	}
	slices.SortFunc(out, func(a, b WithheldGateway) int { return compareRefs(a.Gateway, b.Gateway) })
	return out, state
}

// deferral reports whether e is a snapshot-deferral line: a Gateway and no
// other field, unlike rejects, deployer errors and panics.
func deferral(e *ctrllogs.Event) bool {
	return e.Gateway != (objects.Ref{}) && e.TypeURL == "" && e.Class == "" && e.Frame == "" && e.Object == nil
}

// latestStart is the latest start of s's processes; false when s is not
// observed, has none, or one has no known start.
func latestStart(s process.Set) (time.Time, bool) {
	if !s.Observed || len(s.Processes) == 0 {
		return time.Time{}, false
	}
	var at time.Time
	for _, p := range s.Processes {
		if p.LastStart == nil {
			return time.Time{}, false
		}
		if p.LastStart.After(at) {
			at = *p.LastStart
		}
	}
	return at, true
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
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
