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
	for _, tag := range []string{"v2.3.11", "v2.4.7", "v2.5.0"} {
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

// Contract on the 2.5.0 GA captures: the families the UI reads keep their
// labels; v2.5.0.txt is healthy, so it has no reject series.
func TestCaptured250Contract(t *testing.T) {
	read := func(f string) string {
		b, err := os.ReadFile("testdata/" + f)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	healthy := read("v2.5.0.txt")
	m := parseFile(t, "testdata/v2.5.0.txt")
	if !m.ManagedPresent || len(m.Rejects) != 0 || len(m.PerPod) != 1 || !m.PerPod[0].DroppedPresent {
		t.Errorf("healthy capture: %+v", m)
	}
	if got := m.Snapshot[GatewayKey{"infra", "edge"}]; got != (SnapshotResources{Listeners: 1, Routes: 1, Clusters: 4, Endpoints: 5}) {
		t.Errorf("edge snapshot %+v", got)
	}
	// 2.5 drops status_syncs_total (kgateway #14519), so 018 has no sync count there.
	if m.PerPod[0].StatusSyncsPresent {
		t.Errorf("status_syncs_total is not exported on 2.5.0")
	}
	want := map[string]string{
		"kgateway_build_info":                          "goarch,goos,goversion,revision,version",
		"kgateway_xds_snapshot_perclient_defers_total": "gateway,namespace,reason",
		"kgateway_xds_snapshot_deferred_clients":       "gateway,namespace",
		ResourcesManaged:                               "namespace,parent,resource",
	}
	for name, ls := range want {
		got := labelNames(t, healthy, name)
		if len(got) == 0 {
			t.Errorf("%s: no series", name)
		}
		for _, l := range got {
			if strings.Join(l, ",") != ls {
				t.Errorf("%s labels %v, want %s", name, l, ls)
			}
		}
	}
	if !strings.Contains(healthy, `kgateway_build_info{goarch="arm64",goos="linux",goversion="go1.27.3",revision="f858390fdb26e65fdd9e2f954febcc9c68a4cc1b",version="v2.5.0"} 1`) {
		t.Error("build_info carries the version in its version label")
	}
	for _, f := range []string{"v2.5.0-rejects-a.txt", "v2.5.0-rejects-b.txt"} {
		for _, fam := range []string{XDSRejectsActive, XDSRejectsTotal} {
			for _, l := range labelNames(t, read(f), fam) {
				if strings.Join(l, ",") != "gateway_name,gateway_namespace,type_url" {
					t.Errorf("%s: %s labels %v", f, fam, l)
				}
			}
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
	rds := TypeKey{nack.Gateway, "envoy.config.route.v3.RouteConfiguration"}
	c3 := c2
	c3.Total = map[TypeKey]float64{nack: c2.Total[nack], rds: 7}
	_, p2 := TotalGrowth(prev, []PodMetrics{b, c2})
	if g, _ = TotalGrowth(p2, []PodMetrics{b, c3}); g[rds] != 7 {
		t.Errorf("a series new under a seen UID counts in full: %v", g)
	}
}

// The snapshot per Gateway is the controller's build, from every captured version;
// replicas are maxed, since each builds the snapshots of the proxies it serves.
func TestParseSnapshotResources(t *testing.T) {
	for f, want := range map[string]SnapshotResources{
		"testdata/v2.3.11.txt": {Listeners: 1, Routes: 1, Clusters: 2, Endpoints: 2},
		"testdata/v2.4.7.txt":  {Listeners: 1, Routes: 1, Clusters: 2, Endpoints: 3},
	} {
		if got := parseFile(t, f).Snapshot[GatewayKey{"app-a", "gw"}]; got != want {
			t.Errorf("%s: snapshot %+v, want %+v", f, got, want)
		}
	}
	a, b := parseFile(t, "testdata/v2.4.7-rejects-a.txt"), parseFile(t, "testdata/v2.4.7-rejects-b.txt")
	if got := Aggregate([]Metrics{a, b}).Snapshot[GatewayKey{"infra", "edge"}]; got != (SnapshotResources{Listeners: 1, Routes: 1, Clusters: 4, Endpoints: 5}) {
		t.Errorf("aggregate maxes each resource: %+v", got)
	}
	if m, _ := Parse(strings.NewReader(sample)); len(m.Snapshot) != 0 {
		t.Errorf("an absent family is no snapshot: %+v", m.Snapshot)
	}
}

// Contract: status_syncs_total is summed over its series per Pod, on every
// supported version; a scrape without the family is not present, never zero.
func TestParseStatusSyncs(t *testing.T) {
	for _, tag := range []string{"v2.3.11", "v2.4.7"} {
		f := "testdata/" + tag + ".txt"
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, ls := range labelNames(t, string(b), StatusSyncsTotal) {
			if strings.Join(ls, ",") != "name,namespace,result,syncer" {
				t.Errorf("%s: %s labels %v, want name,namespace,result,syncer", f, StatusSyncsTotal, ls)
			}
		}
		if p := parseFile(t, f).PerPod[0]; !p.StatusSyncsPresent || p.StatusSyncs != 4 {
			t.Errorf("%s: status syncs %+v, want 4 summed over the capture's three series", f, p)
		}
	}
	if p := parseFile(t, "testdata/v2.3.11-rejects-a.txt").PerPod[0]; p.StatusSyncsPresent || p.StatusSyncs != 0 {
		t.Errorf("a capture without the family is not present: %+v", p)
	}
}

// sampleValues returns family name's samples keyed by their label pairs, "k=v,…" in label order.
func sampleValues(t *testing.T, text, name string) map[string]float64 {
	t.Helper()
	tp := expfmt.NewTextParser(model.UTF8Validation)
	fams, err := tp.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, s := range fams[name].GetMetric() {
		var ls []string
		for _, l := range s.GetLabel() {
			ls = append(ls, l.GetName()+"="+l.GetValue())
		}
		v := s.GetGauge().GetValue() + s.GetCounter().GetValue() + s.GetUntyped().GetValue()
		out[strings.Join(ls, ",")] = v
	}
	return out
}

// Contract on the real 2.5.0 captures: build_info's version label and the
// per-client series they hold; no withheld or bounded series without a held client.
func TestCaptured250PerClient(t *testing.T) {
	// Replica b serves no edge stream, so it has no edge series.
	for f, gws := range map[string]int{"v2.5.0.txt": 2, "v2.5.0-rejects-a.txt": 2, "v2.5.0-rejects-b.txt": 1} {
		b, err := os.ReadFile("testdata/" + f)
		if err != nil {
			t.Fatal(err)
		}
		text := string(b)
		p := parseFile(t, "testdata/"+f).PerPod[0]
		if p.BuildVersion != "v2.5.0" || !p.PerClientPresent || len(p.Withheld) != 0 || len(p.Bounded) != 0 {
			t.Errorf("%s: %+v", f, p)
		}
		if got := sampleValues(t, text, BuildInfo); len(got) != 1 {
			t.Errorf("%s: build_info %v", f, got)
		}
		if got := sampleValues(t, text, DeferredClients); got["gateway=nack,namespace=infra"] != 0 || len(got) != gws {
			t.Errorf("%s: deferred_clients %v", f, got)
		}
		for _, fam := range []string{PerClientWithheld, PerClientBounded} {
			if got := sampleValues(t, text, fam); len(got) != 0 {
				t.Errorf("%s: %s has series without a held client: %v", f, fam, got)
			}
		}
	}
	if got := sampleValues(t, mustRead(t, "testdata/v2.5.0.txt"), PerClientDefers); len(got) != 1 || got["gateway=nack,namespace=infra,reason=missing_endpoints"] != 1 {
		t.Errorf("defers_total on the healthy capture: %v", got)
	}
	if got := sampleValues(t, mustRead(t, "testdata/v2.5.0-rc.1-perclient.txt"), BuildInfo); len(got) != 1 {
		t.Errorf("rc.1 build_info: %v", got)
	}
	if v, ok := parseFile(t, "testdata/v2.5.0-rc.1-perclient.txt").BuildVersion(); v != "v2.5.0-rc.1" || !ok {
		t.Errorf("rc.1 version %q", v)
	}
}

func mustRead(t *testing.T, f string) string {
	t.Helper()
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 2.3 and 2.4 export neither build_info nor the per-client families.
func TestPerClientAbsentBefore25(t *testing.T) {
	for _, f := range []string{"v2.3.11.txt", "v2.3.11-rejects-a.txt", "v2.4.7.txt", "v2.4.7-rejects-a.txt", "v2.4.7-restart.txt"} {
		m := parseFile(t, "testdata/"+f)
		if p := m.PerPod[0]; p.BuildVersion != "" || p.PerClientPresent || p.Withheld != nil || p.Bounded != nil || m.PerPod[0].BuildVersion != "" {
			t.Errorf("%s: %+v", f, p)
		}
	}
}

// The synthesised fixtures (R130) parse warm_withheld per Gateway, ignore
// within_budget, and keep every bounded mode per Gateway.
func TestParsePerClientSynthesised(t *testing.T) {
	nack, edge := GatewayKey{"infra", "nack"}, GatewayKey{"infra", "edge"}
	p := parseFile(t, "testdata/v2.5.0-withheld-synth.txt").PerPod[0]
	if !maps.Equal(p.Withheld, map[GatewayKey]float64{nack: 2}) {
		t.Errorf("withheld %v", p.Withheld)
	}
	if !maps.Equal(p.Bounded, map[ModeKey]float64{{edge, "first_publish"}: 1, {nack, "warm_release"}: 1}) {
		t.Errorf("bounded %v", p.Bounded)
	}
	q := parseFile(t, "testdata/v2.5.0-released-synth.txt").PerPod[0]
	if len(q.Withheld) != 0 || q.Bounded[ModeKey{nack, "warm_release"}] != 3 || q.Bounded[ModeKey{nack, "flip_release"}] != 1 {
		t.Errorf("released %+v", q)
	}
	for _, f := range []string{"v2.5.0-withheld-synth.txt", "v2.5.0-released-synth.txt"} {
		text := mustRead(t, "testdata/"+f)
		for fam, want := range map[string]string{PerClientWithheld: "gateway,namespace,state", PerClientBounded: "gateway,mode,namespace"} {
			for _, l := range labelNames(t, text, fam) {
				if strings.Join(l, ",") != want {
					t.Errorf("%s: %s labels %v, want %s", f, fam, l, want)
				}
			}
		}
	}
}

// BuildVersion is the version the reporting Pods share; Pods that disagree are not agreed.
func TestBuildVersionAcrossPods(t *testing.T) {
	mk := func(vs ...string) Metrics {
		var ms []Metrics
		for i, v := range vs {
			ms = append(ms, Metrics{PerPod: []PodMetrics{{UID: string(rune('a' + i)), BuildVersion: v}}})
		}
		return Aggregate(ms)
	}
	for _, tc := range []struct {
		vs     []string
		want   string
		agreed bool
	}{{nil, "", true}, {[]string{"v2.5.0"}, "v2.5.0", true}, {[]string{"v2.5.0", ""}, "v2.5.0", true}, {[]string{"v2.5.0", "v2.5.0"}, "v2.5.0", true},
		{[]string{"v2.5.0", "v2.5.1"}, "", false}} {
		if got, agreed := mk(tc.vs...).BuildVersion(); got != tc.want || agreed != tc.agreed {
			t.Errorf("%v: %q %v, want %q %v", tc.vs, got, agreed, tc.want, tc.agreed)
		}
	}
}

// Bounded growth is per Pod UID: a first sight is a baseline and a lower value a reset, never growth.
func TestBoundedGrowthPerPodUID(t *testing.T) {
	nack := GatewayKey{"infra", "nack"}
	warm, flip := ModeKey{nack, "warm_release"}, ModeKey{nack, "flip_release"}
	pod := func(uid, f string) PodMetrics {
		p := parseFile(t, "testdata/"+f).PerPod[0]
		p.UID = uid
		return p
	}
	g, prev := BoundedGrowth(nil, []PodMetrics{pod("u1", "v2.5.0-withheld-synth.txt")})
	if len(g) != 0 {
		t.Fatalf("first sight is a baseline: %v", g)
	}
	g, prev = BoundedGrowth(prev, []PodMetrics{pod("u1", "v2.5.0-released-synth.txt")})
	if g["u1"][warm] != 2 || g["u1"][flip] != 1 || g["u1"][ModeKey{GatewayKey{"infra", "edge"}, "first_publish"}] != 1 {
		t.Errorf("growth %v", g)
	}
	// The container restarts under the same UID: counters reset, which is not growth.
	g, prev = BoundedGrowth(prev, []PodMetrics{pod("u1", "v2.5.0-withheld-synth.txt")})
	if len(g) != 0 {
		t.Errorf("reset read as growth: %v", g)
	}
	// A new Pod is a baseline; the vanished UID is forgotten.
	_, prev = BoundedGrowth(prev, []PodMetrics{pod("u2", "v2.5.0-released-synth.txt")})
	if _, ok := prev["u1"]; ok || prev["u2"][warm] != 3 {
		t.Errorf("next %v", prev)
	}
}
