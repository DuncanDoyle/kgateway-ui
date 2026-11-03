package health

import (
	"fmt"
	"strings"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

// ProxyConditionAge is how long a Ready proxy must stay disconnected or
// warming before KGWUI-026 or 025 is raised (design §8.8).
const ProxyConditionAge = 60 * time.Second

// proxyStartsKey holds, per proxy Pod UID, when the condition was first seen.
const proxyStartsKey = "starts"

// Wording of KGWUI-025 and 026 and of the proxy stats gaps (design §8.8).
const (
	proxyWarmingMessage      = "Proxy is Ready, but some config is still loading"
	proxyWarmingDetail       = "Config is still loading on %s."
	proxyDisconnectedMessage = "Proxy lost its connection to the controller"
	proxyDisconnectedDetail  = "%s lost its connection to the controller. It keeps serving its last config."
	proxyStatsNotCollected   = "Proxy stats are not collected yet"
	proxyStatsUnavailable    = "Proxy stats are not available"
	proxyStatsGatewayGap     = "%d of %d Gateways have no proxy stats."
	proxyStatsPodGap         = "Could not read stats from 1 proxy."
	proxyStatsPodsGap        = "Could not read stats from %d proxies."
	proxyNamesMore           = "%s and %d more"
)

// proxyCondition is what a proxyStatsCheck looks for on a Ready proxy.
type proxyCondition int

const (
	proxyWarming proxyCondition = iota
	proxyDisconnected
)

// proxyStatsCheck: KGWUI-025 or 026, one per Gateway whose Ready proxies held
// the condition for ProxyConditionAge, from the proxy stats collector.
type proxyStatsCheck struct{ cond proxyCondition }

// Name implements Check.
func (c proxyStatsCheck) Name() string {
	if c.cond == proxyWarming {
		return "proxy config warming"
	}
	return "proxy connection"
}

// Inputs implements Check: process data says which proxies are Ready; the
// age ends through WakeAt.
func (proxyStatsCheck) Inputs() InputSet {
	return InputSet{Process: true, Collected: []string{proxyStatsKey}}
}

// holds reports whether p shows the condition.
func (c proxyStatsCheck) holds(p proxystats.Pod) bool {
	if c.cond == proxyWarming {
		return p.WarmingListeners != nil && *p.WarmingListeners > 0 || p.WarmingClusters != nil && *p.WarmingClusters > 0
	}
	return p.Connected != nil && !*p.Connected
}

// Run implements Check. A Pod whose scrape failed keeps its start but raises
// nothing; a Pod gone from the stats or no longer in the condition is dropped.
func (c proxyStatsCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	st, res, ok := proxyStatsOf(ctx, c.Name())
	if !ok {
		// After an outage a condition must be seen for the full age again.
		ctx.State.Set(proxyStartsKey, nil)
		return nil, res
	}
	old, _ := Load[map[string]time.Time](ctx.State, proxyStartsKey)
	starts := map[string]time.Time{}
	var fs []Finding
	gws := st[in.Install.ID()]
	gaps, failing := 0, 0
	for _, g := range gws {
		if !g.Observed {
			gaps++
			continue
		}
		var matured []proxystats.Pod
		var onset time.Time
		scraped, errs := 0, 0
		for _, p := range g.Pods {
			if p.Err != "" {
				errs++
				if t, seen := old[p.UID]; seen {
					starts[p.UID] = t
				}
				continue
			}
			scraped++
			if !c.holds(p) || !proxyReady(in, g.Gateway, p) {
				continue
			}
			t, seen := old[p.UID]
			if !seen {
				t = in.Now
			}
			starts[p.UID] = t
			if in.Now.Sub(t) >= ProxyConditionAge {
				matured = append(matured, p)
				if onset.IsZero() || t.Before(onset) {
					onset = t
				}
			} else {
				ctx.WakeAt(t.Add(ProxyConditionAge))
			}
		}
		// A Gateway with no Pod scraped has no stats; otherwise its failing Pods are counted.
		if scraped == 0 && errs > 0 {
			gaps++
		} else {
			failing += errs
		}
		if len(matured) > 0 {
			f := c.finding(in, g.Gateway, matured)
			f.Since = &onset
			fs = append(fs, f)
		}
	}
	ctx.State.Set(proxyStartsKey, starts)
	detail := gapDetail(gaps, len(gws), failing)
	if gaps > 0 && gaps == len(gws) {
		why := AvailNotObserved
		for _, g := range gws {
			if g.Observed {
				why = AvailFailing
			}
		}
		return fs, NotAvailable(c.Name(), why, detail, res.CollectedAt)
	}
	res.Detail = detail
	return fs, res
}

// finding is 025 or 026 on gw, naming the matured Pods.
func (c proxyStatsCheck) finding(in Input, gw objects.Ref, pods []proxystats.Pod) Finding {
	f := Finding{Code: CodeProxyDisconnected, Slug: "proxy-disconnected", Severity: SeverityWarning, Install: in.Install.ID(), Resource: &gw,
		Message: proxyDisconnectedMessage}
	detail := proxyDisconnectedDetail
	if c.cond == proxyWarming {
		f.Code, f.Slug, f.Message, detail = CodeProxyConfigWarming, "proxy-config-warming", proxyWarmingMessage, proxyWarmingDetail
	}
	ev := &ProxyStatsEvidence{Pods: make([]ProxyPodStats, 0, len(pods))}
	names := make([]string, 0, len(pods))
	for _, p := range pods {
		ev.Pods = append(ev.Pods, ProxyPodStats{Pod: p.Name, Connected: p.Connected, WarmingListeners: p.WarmingListeners,
			WarmingClusters: p.WarmingClusters, ServerState: p.ServerState})
		names = append(names, p.Name)
		f.Evidence = append(f.Evidence, process.PodRef(gw.Namespace, process.Process{Name: p.Name}))
	}
	f.Detail, f.Facts = fmt.Sprintf(detail, podNames(names)), ev
	return f
}

// proxyStatsOf returns the install-wide stats and an OK result stamped with
// their time; false with a not-available result when they are off, failing or absent.
func proxyStatsOf(ctx RunContext, name string) (proxystats.Stats, CheckResult, bool) {
	v, have := ctx.Collected(proxyStatsKey)
	st, isStats := v.Data.(proxystats.Stats)
	switch {
	case !have:
		return nil, NotAvailable(name, AvailFailing, proxyStatsNotCollected, ctx.Now), false
	case v.State == collect.Off:
		return nil, NotAvailable(name, AvailOff, v.Reason, v.At), false
	case v.State != collect.OK || !isStats:
		why := v.Reason
		if why == "" {
			why = proxyStatsUnavailable
		}
		return nil, NotAvailable(name, AvailFailing, why, v.At), false
	}
	return st, ok(name, false, v.At), true
}

// proxyReady reports whether p is Ready: its Pod's Ready condition when the
// process data knows it, else Envoy's own server.state "live".
func proxyReady(in Input, gw objects.Ref, p proxystats.Pod) bool {
	if in.Process != nil {
		for _, q := range in.Process.Proxies[gw].Processes {
			if q.ID == p.UID {
				return q.Ready != nil && *q.Ready
			}
		}
	}
	return p.ServerState == "live"
}

// gapDetail names how many Gateways have no proxy stats and how many Pods failed.
func gapDetail(gaps, gateways, failing int) string {
	var parts []string
	if gaps > 0 {
		parts = append(parts, fmt.Sprintf(proxyStatsGatewayGap, gaps, gateways))
	}
	switch {
	case failing == 1:
		parts = append(parts, proxyStatsPodGap)
	case failing > 1:
		parts = append(parts, fmt.Sprintf(proxyStatsPodsGap, failing))
	}
	return strings.Join(parts, " ")
}

// podNames joins up to three names, then counts the rest.
func podNames(names []string) string {
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf(proxyNamesMore, strings.Join(names[:3], ", "), len(names)-3)
}

// Envoy's xDS types as the controller's metrics name them.
const (
	listenerTypeURL = "envoy.config.listener.v3.Listener"
	clusterTypeURL  = "envoy.config.cluster.v3.Cluster"
	routeTypeURL    = "envoy.config.route.v3.RouteConfiguration"
	endpointTypeURL = "envoy.config.endpoint.v3.ClusterLoadAssignment"
)

// proxyRejects is 001's evidence from gw's scraped proxy Pods, and their refs.
// cluster.<name>.update_rejected counts that cluster's EDS updates.
func proxyRejects(gw *proxystats.Gateway) ([]ProxyRejects, []objects.Ref) {
	if gw == nil {
		return nil, nil
	}
	var out []ProxyRejects
	var refs []objects.Ref
	for _, p := range gw.Pods {
		if p.Err != "" {
			continue
		}
		out = append(out, ProxyRejects{Pod: p.Name, Types: []ProxyTypeRejects{
			typeRejects(listenerTypeURL, p.LDS),
			typeRejects(clusterTypeURL, p.CDS),
			{TypeURL: routeTypeURL, Rejecting: len(p.RDS) > 0, EnvoyResources: p.RDS, EnvoyResourcesTruncated: p.RDSTruncated},
			{TypeURL: endpointTypeURL, Rejecting: len(p.Clusters) > 0, EnvoyResources: p.Clusters, EnvoyResourcesTruncated: p.ClustersTruncated},
		}})
		refs = append(refs, process.PodRef(gw.Gateway.Namespace, process.Process{Name: p.Name}))
	}
	return out, refs
}

func typeRejects(url string, r proxystats.Rejects) ProxyTypeRejects {
	t := ProxyTypeRejects{TypeURL: url, Rejecting: r.Seen}
	if r.Seen && !r.Since.IsZero() {
		at := r.Since.UTC()
		t.RejectingSince = &at
	}
	return t
}

// proxyGateways indexes the install's proxy stats by Gateway; nil when the
// stats are not available.
func proxyGateways(ctx RunContext) map[objects.Ref]*proxystats.Gateway {
	st, _, ok := proxyStatsOf(ctx, "")
	if !ok {
		return nil
	}
	gws := st[ctx.Install.ID()]
	out := make(map[objects.Ref]*proxystats.Gateway, len(gws))
	for i := range gws {
		out[gws[i].Gateway] = &gws[i]
	}
	return out
}
