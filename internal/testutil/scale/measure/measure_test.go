package measure

import (
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrllogs"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

// The worst world's stub stats reach the store for two proxies per Gateway,
// disconnected, warming and with rejects that grow on every scrape; the log
// reader's backfill fills the install's ring, every event naming the rejected cluster.
func TestWorstWorldScrapes(t *testing.T) {
	tier := scale.Typical.Worst()
	w := New(t, tier, testutil.FakeClock())
	snap := w.Live.Cache.Snapshot()
	in, _ := install.UpstreamDetector{}.Detect(testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1))
	in.Pods = snap.Pods
	var gws []objects.Ref
	for _, u := range append(testutil.ScaleLongGatewayObjects(tier.Shape()), scale.World(tier, 1)...) {
		if u.GetKind() == "Gateway" {
			gws = append(gws, objects.RefOf(u))
		}
	}
	w.SetInstalls(func() ([]proxystats.Install, *cache.Snapshot) {
		return []proxystats.Install{{Resolved: install.Resolved{Install: in}, Gateways: gws}}, snap
	})
	w.Scrape(t)
	w.Scrape(t)
	st, ok := w.Store.Snapshot().Values[proxystats.Key].Data.(proxystats.Stats)
	if !ok || len(st[in.ID()]) != tier.Gateways+3 {
		t.Fatalf("stats for %d Gateways", len(st[in.ID()]))
	}
	for _, g := range st[in.ID()] {
		if len(g.Pods) != tier.ProxyPodsPerGateway {
			t.Fatalf("%v: %d Pods", g.Gateway, len(g.Pods))
		}
		for _, p := range g.Pods {
			if p.Err != "" || p.Connected == nil || *p.Connected || *p.WarmingClusters != 1 || !p.LDS.Seen || len(p.Clusters) != 1 {
				t.Fatalf("%v %s: %+v", g.Gateway, p.Name, p)
			}
		}
	}
	logs, _ := w.Store.Snapshot().Values[ctrllogs.Key].Data.(ctrllogs.Logs)
	l := logs[in.ID()]
	if l.State != ctrllogs.StateOn || len(l.Events) != ctrllogs.MaxEvents {
		t.Fatalf("logs %s with %d events", l.State, len(l.Events))
	}
	for _, e := range l.Events {
		if len(e.Names) != 1 || e.Names[0] != "kube_"+e.Gateway.Namespace+"_"+e.Gateway.Name+"_8080" {
			t.Fatalf("event %+v", e)
		}
	}
}
