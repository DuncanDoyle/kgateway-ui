package ctrlmetrics

import (
	"maps"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// type_url values carry no "type.googleapis.com/" prefix: the controller strips
// it (envoy_error.go on v2.3.11, v2.4.7 and main).
const sample = `# HELP kgateway_envoy_xds_rejects_active Number of xDS responses currently rejected by envoy proxy
# TYPE kgateway_envoy_xds_rejects_active gauge
kgateway_envoy_xds_rejects_active{gateway_name="edge",gateway_namespace="infra",type_url="envoy.config.listener.v3.Listener"} 1
kgateway_envoy_xds_rejects_active{gateway_name="edge",gateway_namespace="infra",type_url="envoy.config.route.v3.RouteConfiguration"} 1
kgateway_envoy_xds_rejects_active{gateway_name="ok",gateway_namespace="infra",type_url="envoy.config.listener.v3.Listener"} 0
# HELP kgateway_resources_managed Current number of resources managed
# TYPE kgateway_resources_managed gauge
kgateway_resources_managed{namespace="shop",parent="edge",resource="HTTPRoute"} 3
kgateway_resources_managed{namespace="shop",parent="internal",resource="HTTPRoute"} 2
kgateway_resources_managed{namespace="shop",parent="",resource="TrafficPolicy"} 1
`

func TestParseSumsByKey(t *testing.T) {
	m, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if m.Rejects[GatewayKey{"infra", "edge"}] != 2 || m.Rejects[GatewayKey{"infra", "ok"}] != 0 {
		t.Fatalf("rejects: %+v", m.Rejects)
	}
	if !m.ManagedPresent || m.Managed[ManagedKey{"shop", "HTTPRoute"}] != 5 || m.Managed[ManagedKey{"shop", "TrafficPolicy"}] != 1 {
		t.Fatalf("managed: %+v", m.Managed)
	}
}

func TestAbsentFamiliesMeanZeroNotError(t *testing.T) {
	m, err := Parse(strings.NewReader("# TYPE go_goroutines gauge\ngo_goroutines 12\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Rejects) != 0 || m.ManagedPresent {
		t.Fatalf("absent families must parse as empty: %+v", m)
	}
}

func TestAggregateSumsRejectsAndMaxesManaged(t *testing.T) {
	a := Metrics{Rejects: map[GatewayKey]float64{{"infra", "edge"}: 1}, Managed: map[ManagedKey]float64{{"shop", "HTTPRoute"}: 5}, ManagedPresent: true}
	b := Metrics{Rejects: map[GatewayKey]float64{{"infra", "edge"}: 1}, Managed: map[ManagedKey]float64{{"shop", "HTTPRoute"}: 5}, ManagedPresent: true}
	got := Aggregate([]Metrics{a, b})
	if got.Rejects[GatewayKey{"infra", "edge"}] != 2 || got.Managed[ManagedKey{"shop", "HTTPRoute"}] != 5 {
		t.Fatalf("got %+v", got)
	}
}

// labelNames returns the sorted label names of every series of family name.
func labelNames(t *testing.T, text, name string) [][]string {
	t.Helper()
	p := expfmt.NewTextParser(model.UTF8Validation)
	fams, err := p.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for _, s := range fams[name].GetMetric() {
		var ls []string
		for _, l := range s.GetLabel() {
			ls = append(ls, l.GetName())
		}
		sort.Strings(ls)
		out = append(out, ls)
	}
	return out
}

// Contract test: the metric names, labels and values the UI
// depends on, checked against real controller output for every supported version.
// The synthetic sample above is not evidence of the upstream contract.
func TestCapturedFixturesHonourTheContract(t *testing.T) {
	for _, f := range []string{"testdata/v2.4.7.txt", "testdata/v2.3.11.txt"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		m, err := Parse(strings.NewReader(string(b)))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !m.ManagedPresent {
			t.Errorf("%s: %s missing", f, ResourcesManaged)
		}
		for _, ls := range labelNames(t, string(b), ResourcesManaged) {
			if strings.Join(ls, ",") != "namespace,parent,resource" {
				t.Errorf("%s: %s labels %v, want namespace,parent,resource", f, ResourcesManaged, ls)
			}
		}
		gateways := 0.0
		for k, v := range m.Managed {
			if k.Resource == "Gateway" && k.Namespace != "" {
				gateways += v
			}
		}
		if gateways < 1 {
			t.Errorf("%s: the captured install manages a Gateway, but no resource=\"Gateway\" series counts it: %+v", f, m.Managed)
		}
		// Each capture holds an induced CDS NACK of app-a/gw; a capture without
		// it fails, so the test never passes vacuously.
		rejects := labelNames(t, string(b), XDSRejectsActive)
		if len(rejects) == 0 {
			t.Fatalf("%s: no %s series; recapture with an induced reject", f, XDSRejectsActive)
		}
		for _, ls := range rejects {
			if strings.Join(ls, ",") != "gateway_name,gateway_namespace,type_url" {
				t.Errorf("%s: %s labels %v", f, XDSRejectsActive, ls)
			}
		}
		if got := m.Rejects[GatewayKey{Namespace: "app-a", Name: "gw"}]; got != 1 {
			t.Errorf("%s: parsed rejects for app-a/gw = %v, want 1", f, got)
		}
		if !strings.Contains(string(b), `type_url="envoy.config.cluster.v3.Cluster"`) {
			t.Errorf("%s: the cipher-suite reject is a CDS NACK; type_url must be envoy.config.cluster.v3.Cluster without the type.googleapis.com/ prefix", f)
		}
	}
}

func parseFile(t *testing.T, f string) Metrics {
	t.Helper()
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("%s: %v", f, err)
	}
	return m
}

func TestParsePerTypeAndPod(t *testing.T) {
	cds, rds := "envoy.config.cluster.v3.Cluster", "envoy.config.route.v3.RouteConfiguration"
	nack, edge := GatewayKey{"infra", "nack"}, GatewayKey{"infra", "edge"}
	for _, tag := range []string{"v2.3.11", "v2.4.7"} {
		a := parseFile(t, "testdata/"+tag+"-rejects-a.txt")
		b := parseFile(t, "testdata/"+tag+"-rejects-b.txt")
		if len(a.PerPod) != 1 || len(b.PerPod) != 1 {
			t.Fatalf("%s: Parse yields one PodMetrics per scrape: %d, %d", tag, len(a.PerPod), len(b.PerPod))
		}
		pa := a.PerPod[0]
		if pa.Active[TypeKey{nack, cds}] != 1 || pa.Active[TypeKey{nack, rds}] != 1 || pa.Active[TypeKey{edge, cds}] != 1 {
			t.Errorf("%s: active per type: %+v", tag, pa.Active)
		}
		if pa.Total[TypeKey{nack, cds}] < 2 || pa.Total[TypeKey{nack, rds}] < 2 {
			t.Errorf("%s: total per type: %+v", tag, pa.Total)
		}
		if !pa.DroppedPresent || pa.Dropped != 0 {
			t.Errorf("%s: dropped_total is present and 0 in the capture: %+v", tag, pa)
		}
		// Summed over types, the per-Pod active streams equal Rejects per Gateway.
		sum := map[GatewayKey]float64{}
		for _, p := range a.PerPod {
			for k, v := range p.Active {
				sum[k.Gateway] += v
			}
		}
		if !maps.Equal(sum, a.Rejects) {
			t.Errorf("%s: summed PerPod active %v != Rejects %v", tag, sum, a.Rejects)
		}
		agg := Aggregate([]Metrics{a, b})
		if len(agg.PerPod) != 2 || agg.Rejects[nack] != 4 {
			t.Errorf("%s: aggregate keeps both Pods and sums Rejects: %+v", tag, agg)
		}
	}
}

func TestDroppedAbsentIsNotPresent(t *testing.T) {
	m, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.PerPod) != 1 || m.PerPod[0].DroppedPresent || m.PerPod[0].Dropped != 0 {
		t.Fatalf("an absent family is not present: %+v", m.PerPod)
	}
	d, _ := Parse(strings.NewReader("# TYPE kgateway_resources_updates_dropped_total counter\nkgateway_resources_updates_dropped_total 3\n"))
	if !d.PerPod[0].DroppedPresent || d.PerPod[0].Dropped != 3 {
		t.Fatalf("dropped: %+v", d.PerPod)
	}
}

func TestAggregateSortsPodsByName(t *testing.T) {
	mk := func(n string) Metrics { return Metrics{PerPod: []PodMetrics{{UID: "u-" + n, Name: n}}} }
	got := Aggregate([]Metrics{mk("c"), mk("a"), mk("b")})
	if got.PerPod[0].Name != "a" || got.PerPod[1].Name != "b" || got.PerPod[2].Name != "c" {
		t.Fatalf("not sorted: %+v", got.PerPod)
	}
}

func TestCounterDeltaPerPodUIDNeverNegative(t *testing.T) {
	nack := TypeKey{GatewayKey{"infra", "nack"}, "envoy.config.cluster.v3.Cluster"}
	edge := TypeKey{GatewayKey{"infra", "edge"}, "envoy.config.cluster.v3.Cluster"}
	pod := func(uid, f string) PodMetrics {
		p := parseFile(t, f).PerPod[0]
		p.UID, p.Name = uid, "ctl-"+uid
		return p
	}
	a, b := pod("uid-a", "testdata/v2.4.7-rejects-a.txt"), pod("uid-b", "testdata/v2.4.7-rejects-b.txt")
	g, prev := TotalGrowth(nil, []PodMetrics{a, b})
	if len(g) != 0 {
		t.Fatalf("first sight of a Pod is a baseline, not growth: %v", g)
	}
	// Replica a is deleted and restarts as uid-c with a reset counter (edge 2 -> 1).
	c := pod("uid-c", "testdata/v2.4.7-restart.txt")
	g, prev = TotalGrowth(prev, []PodMetrics{b, c})
	for k, v := range g {
		if v < 0 {
			t.Errorf("negative growth %v for %+v", v, k)
		}
	}
	if g[edge] != 0 {
		t.Errorf("reset read as growth: %v", g)
	}
	if _, ok := prev["uid-a"]; ok {
		t.Errorf("a vanished UID's value must be forgotten: %v", prev)
	}
	// The same UID grows: only the delta counts; a lower value is a reset, never negative.
	c2 := c
	c2.Total = map[TypeKey]float64{nack: c.Total[nack] + 5, edge: 0}
	g, _ = TotalGrowth(prev, []PodMetrics{b, c2})
	if g[nack] != 5 || g[edge] != 0 {
		t.Errorf("growth: %v", g)
	}
}
