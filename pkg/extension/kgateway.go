package extension

import (
	"context"
	"slices"

	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/ctrllogs"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kgateway"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/proxystats"
)

// The kgateway pack's collectors. Each covers only the installs its pack
// claimed and builds its body in Start, so a zero CollectorEnv tells its name.

func metricsCollector(env CollectorEnv) Collector { return packMetrics{env} }

func proxyStatsCollector(env CollectorEnv) Collector { return packProxyStats{env} }

func controllerLogsCollector(env CollectorEnv) Collector { return packLogs{env} }

// packMetrics scrapes the controller metrics from the first discovered
// install on, so the first scrape does not wait a full interval.
type packMetrics struct{ env CollectorEnv }

func (packMetrics) Name() string { return ctrlmetrics.CollectorName }

func (m packMetrics) Start(ctx context.Context, d Deps) error {
	if m.env.Discovered != nil {
		select {
		case <-ctx.Done():
			return nil
		case <-m.env.Discovered:
		}
	}
	s := m.env.Scraper
	if s == nil {
		s = ctrlmetrics.ScraperFor(m.env.Kube, m.env.InCluster)
	}
	opts := []ctrlmetrics.CollectorOption{ctrlmetrics.WithMaxAge(m.env.MaxAge)}
	if m.env.Clock != nil {
		opts = append(opts, ctrlmetrics.WithClock(m.env.Clock))
	}
	c := ctrlmetrics.NewCollector(s, opts...)
	installs := func() []install.Install { return kgateway.Claimed(m.env.Installs(), m.env.Pack) }
	return c.Run(ctx, m.env.Interval, installs, d.Publish)
}

// packProxyStats reads the proxies' Envoy stats over the pack's ProxyStats
// source, or proxystats.Kube (design §8.8).
type packProxyStats struct{ env CollectorEnv }

func (packProxyStats) Name() string { return proxystats.CollectorName }

func (p packProxyStats) Start(ctx context.Context, d Deps) error {
	src := p.env.Impl.ProxyStats
	if src == nil {
		src = proxystats.KubeFor(p.env.Kube, p.env.InCluster)
	}
	installs := func() ([]proxystats.Install, *cache.Snapshot) { return proxyStatsInstalls(p.env.Current(), p.env.Pack) }
	c := proxystats.New(proxystats.Config{Source: src, Every: p.env.Interval, Clock: p.env.Clock, Installs: installs, Off: p.env.NoProxyStats})
	return c.Run(ctx, d.Publish, d.Fail)
}

// packLogs reads the controller logs through pods/log when ControllerLogs is
// on, with names only from the latest proxy stats (D-037); off, it returns at once.
type packLogs struct{ env CollectorEnv }

func (packLogs) Name() string { return ctrllogs.CollectorName }

func (l packLogs) Start(ctx context.Context, d Deps) error {
	if !l.env.ControllerLogs {
		return nil
	}
	installs := func() ([]ctrllogs.Install, bool) { return logInstalls(l.env.Current(), l.env.Pack) }
	stats := func() (proxystats.Stats, ctrllogs.StatsState) { return kgateway.LatestStats(l.env.Collected) }
	c := ctrllogs.New(ctrllogs.Config{Source: ctrllogs.Kube{Client: l.env.Kube}, Every: l.env.Interval, Clock: l.env.Clock, Installs: installs, Stats: stats})
	return c.Run(ctx, d.Publish, d.Fail)
}

// gateways are the refs of the install's owned Gateways.
func gateways(im engine.InstallModel) []objects.Ref {
	if im.Owned == nil {
		return nil
	}
	out := make([]objects.Ref, 0, len(im.Owned.Gateways))
	for _, g := range im.Owned.Gateways {
		out = append(out, objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: g.GetNamespace(), Name: g.GetName()})
	}
	return out
}

// proxyStatsInstalls are p's installs of pack with their Gateways, and p's
// snapshot; none until p is over a synced cache, which can lack proxy Pods before.
func proxyStatsInstalls(p *engine.Published, pack string) ([]proxystats.Install, *cache.Snapshot) {
	if p == nil || p.Model == nil || p.Model.Snapshot == nil || !p.Model.Snapshot.Synced {
		return nil, nil
	}
	out := make([]proxystats.Install, 0, len(p.Model.Installs))
	for _, im := range p.Model.Installs {
		if im.Resolved.Impl == pack {
			out = append(out, proxystats.Install{Resolved: im.Resolved, Gateways: gateways(im)})
		}
	}
	return out, p.Model.Snapshot
}

// logInstalls are p's installs of pack with their log parser and Gateways;
// false until p is over a synced cache. A deployer error keeps its object only
// when it is a Gateway of the install or a Deployment the UI sees.
func logInstalls(p *engine.Published, pack string) ([]ctrllogs.Install, bool) {
	if p == nil || p.Model == nil || p.Model.Snapshot == nil || !p.Model.Snapshot.Synced {
		return nil, false
	}
	snap := p.Model.Snapshot
	out := make([]ctrllogs.Install, 0, len(p.Model.Installs))
	for _, im := range p.Model.Installs {
		if im.Resolved.Impl != pack {
			continue
		}
		in := ctrllogs.Install{Resolved: im.Resolved, Parser: im.Rules.LogParser(), Gateways: gateways(im)}
		gws := in.Gateways
		in.Known = func(r objects.Ref) bool {
			if r.Kind == "Deployment" && (r.Group == "" || r.Group == "apps") {
				for _, d := range snap.Deployments {
					if d.Namespace == r.Namespace && d.Name == r.Name {
						return true
					}
				}
				return false
			}
			return slices.Contains(gws, r)
		}
		out = append(out, in)
	}
	return out, true
}
