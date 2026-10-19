package ctrlmetrics

import (
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
