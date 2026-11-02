// Package ctrlmetrics scrapes and interprets the kgateway controller's
// Prometheus metrics. All metric names the UI depends on are declared here.
package ctrlmetrics

import (
	"cmp"
	"io"
	"maps"
	"slices"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// Metric families the UI depends on (verified on kgateway v2.3.x, v2.4.x and main).
const (
	XDSRejectsActive = "kgateway_envoy_xds_rejects_active"
	ResourcesManaged = "kgateway_resources_managed"
	// XDSRejectsTotal counts rejected xDS responses. On v2.3 and v2.4 it grows
	// on every re-push; on v2.5 and later it counts episodes.
	XDSRejectsTotal = "kgateway_envoy_xds_rejects_total"
	// UpdatesDropped counts dropped resources-metric updates. Above zero, the
	// kgateway_resources_* gauges are invalid until the controller restarts.
	UpdatesDropped = "kgateway_resources_updates_dropped_total"
)

// GatewayKey identifies a Gateway in metric labels.
type GatewayKey struct{ Namespace, Name string }

// ManagedKey identifies a resources_managed series after summing over "parent".
type ManagedKey struct{ Namespace, Resource string }

// TypeKey identifies an xDS reject series: one Gateway and one resource type.
type TypeKey struct {
	Gateway GatewayKey
	TypeURL string
}

// PodMetrics is what one controller Pod reports. Parse leaves UID and Name
// empty; the collector fills them from the scraped Pod.
type PodMetrics struct {
	UID, Name string
	// Active and Total are rejects_active and rejects_total per Gateway and type.
	Active, Total map[TypeKey]float64
	// Dropped is updates_dropped_total; DroppedPresent is false when the
	// family is absent, which never means zero.
	Dropped        float64
	DroppedPresent bool
}

// Metrics is the interpreted subset of one or more controller scrapes.
type Metrics struct {
	// Rejects is the number of active xDS rejects per Gateway, summed over type_url.
	Rejects map[GatewayKey]float64
	// Managed is kgateway_resources_managed summed over the parent label.
	Managed map[ManagedKey]float64
	// ManagedPresent is true if the family appeared in the scrape. An absent
	// family with resources in scope suggests a rename upstream.
	ManagedPresent bool
	// PerPod holds each controller Pod's samples, sorted by Pod name.
	PerPod []PodMetrics
}

// Parse interprets a text-format /metrics payload. A gauge that has never been
// set has no series, so an absent family means zero, not an error.
func Parse(r io.Reader) (Metrics, error) {
	m := Metrics{Rejects: map[GatewayKey]float64{}, Managed: map[ManagedKey]float64{}}
	pod := PodMetrics{Active: map[TypeKey]float64{}, Total: map[TypeKey]float64{}}
	p := expfmt.NewTextParser(model.UTF8Validation)
	fams, err := p.TextToMetricFamilies(r)
	if err != nil {
		return m, err
	}
	if f, ok := fams[XDSRejectsActive]; ok {
		for _, s := range f.GetMetric() {
			l := lbls(s)
			gw := GatewayKey{l["gateway_namespace"], l["gateway_name"]}
			m.Rejects[gw] += s.GetGauge().GetValue()
			pod.Active[TypeKey{gw, l["type_url"]}] += s.GetGauge().GetValue()
		}
	}
	if f, ok := fams[XDSRejectsTotal]; ok {
		for _, s := range f.GetMetric() {
			l := lbls(s)
			pod.Total[TypeKey{GatewayKey{l["gateway_namespace"], l["gateway_name"]}, l["type_url"]}] += s.GetCounter().GetValue()
		}
	}
	if f, ok := fams[UpdatesDropped]; ok {
		pod.DroppedPresent = len(f.GetMetric()) > 0
		for _, s := range f.GetMetric() {
			pod.Dropped += s.GetCounter().GetValue()
		}
	}
	m.PerPod = []PodMetrics{pod}
	if f, ok := fams[ResourcesManaged]; ok {
		m.ManagedPresent = true
		for _, s := range f.GetMetric() {
			l := lbls(s)
			m.Managed[ManagedKey{l["namespace"], l["resource"]}] += s.GetGauge().GetValue()
		}
	}
	return m, nil
}

func lbls(s *dto.Metric) map[string]string {
	out := map[string]string{}
	for _, l := range s.GetLabel() {
		out[l.GetName()] = l.GetValue()
	}
	return out
}

// Aggregate combines per-replica scrapes. Rejects are summed, because each
// proxy's xDS stream is served by one replica. Managed counts are maxed,
// because every replica computes the full set (D-012). PerPod is concatenated
// and sorted by Pod name, then UID.
func Aggregate(replicas []Metrics) Metrics {
	out := Metrics{Rejects: map[GatewayKey]float64{}, Managed: map[ManagedKey]float64{}}
	for _, r := range replicas {
		for k, v := range r.Rejects {
			out.Rejects[k] += v
		}
		for k, v := range r.Managed {
			out.Managed[k] = max(out.Managed[k], v)
		}
		out.ManagedPresent = out.ManagedPresent || r.ManagedPresent
		out.PerPod = append(out.PerPod, r.PerPod...)
	}
	slices.SortStableFunc(out.PerPod, func(a, b PodMetrics) int { return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.UID, b.UID)) })
	return out
}

// PodTotals is the last seen rejects_total per Pod UID.
type PodTotals map[string]map[TypeKey]float64

// TotalGrowth returns the rejects_total growth since prev, summed over Pods per
// type, and the totals to pass next time. Growth is taken per Pod UID: a Pod
// seen for the first time sets a baseline, a counter that went down is a reset,
// and neither counts as growth, so a restarted replica is never negative. A UID
// absent from cur is forgotten, so callers skip Err results (an empty cur
// forgets every UID). A series new under a seen UID counts its full value.
func TotalGrowth(prev PodTotals, cur []PodMetrics) (growth map[TypeKey]float64, next PodTotals) {
	growth, next = map[TypeKey]float64{}, PodTotals{}
	for _, p := range cur {
		last, seen := prev[p.UID]
		next[p.UID] = maps.Clone(p.Total)
		if !seen {
			continue
		}
		for k, v := range p.Total {
			if d := v - last[k]; d > 0 {
				growth[k] += d
			}
		}
	}
	return growth, next
}
