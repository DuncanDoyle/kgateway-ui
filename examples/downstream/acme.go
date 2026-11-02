package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/extension"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxymode"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

// acmePolicy is acme's own policy kind. Its objects are the widgets.
var acmePolicy = kinds.Descriptor{
	Group: "acme.example.com", Kind: "AcmePolicy", Resource: "acmepolicies",
	Versions: []string{"v1"}, Namespaced: true, Category: kinds.CategoryPolicy, DisplayName: "AcmePolicy",
}

var acmePolicyGVR = schema.GroupVersionResource{Group: acmePolicy.Group, Version: "v1", Resource: acmePolicy.Resource}

// acmeProxyMode reads the self-managed flag from acme's own parameters kind.
type acmeProxyMode struct{}

func (acmeProxyMode) Resolve(gw *unstructured.Unstructured, snap *cache.Snapshot, served []kinds.Served) proxymode.Mode {
	return proxymode.Managed
}

// widgetsEvery is how often acmeWidgets publishes.
const widgetsEvery = 5 * time.Second

// acmeWidgets publishes the widget count every widgetsEvery. It stands in
// for a scraper or a prober.
type acmeWidgets struct{ clock clock.WithTicker }

func (*acmeWidgets) Name() string { return "acme-widgets" }

func (w *acmeWidgets) Start(ctx context.Context, d extension.Deps) error {
	t := w.clock.NewTicker(widgetsEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C():
			if p := d.Current(); p != nil {
				n := len(p.Model.Snapshot.Objects[acmePolicyGVR])
				// Expires after six missed publishes, so a stuck collector reads unavailable.
				d.Publish("widgets", n, uint64(n), 6*widgetsEvery)
			}
		}
	}
}

// acmeWidgetCheck raises ACME-001 when the collector counts no widgets.
type acmeWidgetCheck struct{}

func (acmeWidgetCheck) Name() string { return "acme widgets" }

// Inputs declares the one collector key Run reads.
func (acmeWidgetCheck) Inputs() health.InputSet {
	return health.InputSet{Collected: []string{"acme-widgets/widgets"}}
}

func (c acmeWidgetCheck) Run(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
	res := health.CheckResult{Name: c.Name(), CollectedAt: ctx.Now}
	v, ok := ctx.Collected("acme-widgets/widgets")
	if !ok || v.State != collect.OK {
		// Not required, so a missing count never turns the install unknown.
		res.Error = "widget count not available"
		if v.Reason != "" {
			res.Error += ": " + v.Reason
		}
		return nil, res
	}
	res.OK = true
	if n, _ := v.Data.(int); n > 0 {
		return nil, res
	}
	return []health.Finding{{Code: "ACME-001", Slug: "no-widgets", Severity: health.SeverityWarning, Install: ctx.Install.ID(),
		Message: "No acme widgets found", Detail: "Create an AcmePolicy to add a widget."}}, res
}

// widget is one AcmePolicy in GET /api/ext/acme/widgets.
type widget struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// widgetList is the body of GET /api/ext/acme/widgets.
type widgetList struct {
	Widgets []widget `json:"widgets"`
}

// acmeHandlers serves GET /api/ext/acme/widgets: the widgets the caller may see.
func acmeHandlers(r extension.Router, d extension.Deps) {
	r.Handle("GET /widgets", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		p := d.Current()
		if p == nil {
			api.WriteError(w, http.StatusServiceUnavailable, "the UI is still reading the cluster")
			return
		}
		all := []widget{}
		for _, u := range p.Model.Snapshot.Objects[acmePolicyGVR] {
			all = append(all, widget{Namespace: u.GetNamespace(), Name: u.GetName()})
		}
		visible, _ := api.Visible(req, d.Authorizer, all, func(x widget) objects.Ref {
			return objects.Ref{Group: acmePolicy.Group, Kind: acmePolicy.Kind, Namespace: x.Namespace, Name: x.Name}
		})
		api.WriteJSON(w, widgetList{Widgets: visible})
	}))
}

// acmeCapabilities reports the widgets feature in /api/v1/meta.
func acmeCapabilities() []api.Capability {
	return []api.Capability{{Name: "acme-widgets", State: api.CapOn}}
}

// acmeProxyStats is a proxy stats source for acme's proxies, which run one
// per Gateway outside Pods and report JSON. fetch reads one proxy's stats.
type acmeProxyStats struct {
	fetch func(ctx context.Context, addr string) ([]byte, error)
}

// acmeStats is the JSON an acme proxy reports.
type acmeStats struct {
	Connected       bool  `json:"connected"`
	ListenerRejects int64 `json:"listenerRejects"`
}

func (acmeProxyStats) Targets(_ install.Resolved, gw objects.Ref, _ *cache.Snapshot) ([]proxystats.Target, bool) {
	return []proxystats.Target{{ID: "acme/" + gw.Namespace + "/" + gw.Name, Name: gw.Name + "-node", Gateway: gw,
		Addr: gw.Name + "." + gw.Namespace + ".acme.internal:7000"}}, true
}

func (s acmeProxyStats) Fetch(ctx context.Context, t proxystats.Target) ([]byte, error) {
	return s.fetch(ctx, t.Addr)
}

func (acmeProxyStats) Interpret(t proxystats.Target, body []byte) (proxystats.PodStats, error) {
	var j acmeStats
	if err := json.Unmarshal(body, &j); err != nil {
		return proxystats.PodStats{}, err
	}
	return proxystats.PodStats{UID: t.ID, Name: t.Name, Gateway: t.Gateway, Connected: &j.Connected, LDS: proxystats.Pair{Rejected: j.ListenerRejects}}, nil
}
