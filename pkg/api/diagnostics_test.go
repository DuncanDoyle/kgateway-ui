package api

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/diagnose"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

const (
	diagPath  = base + "/diagnostics"
	procsPath = base + "/processes"
)

// authzSrv serves e's model to the callers a admits.
func authzSrv(t *testing.T, h *harness, a Authorizer, opts ...HandlerOption) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewHandler(h.e, fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}}, a, opts...))
	t.Cleanup(srv.Close)
	return srv
}

// deny admits every ref but those listed.
func deny(refs ...objects.Ref) Authorizer {
	return AuthorizerFunc(func(_ *http.Request, r objects.Ref) bool { return !slices.Contains(refs, r) })
}

func podRef(ns, name string) objects.Ref { return objects.Ref{Kind: "Pod", Namespace: ns, Name: name} }

var ctrlDeployment = objects.Ref{Group: "apps", Kind: "Deployment", Namespace: "kgw", Name: "kgateway"}

// rawJSON is GET path's body as plain JSON values.
func rawJSON(t *testing.T, srv *httptest.Server, path string) map[string]any {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("GET %s: %v\n%s", path, err, b)
	}
	return m
}

// rawFinding is the /health finding with id as plain JSON.
func rawFinding(t *testing.T, srv *httptest.Server, id string) map[string]any {
	t.Helper()
	for _, f := range rawJSON(t, srv, healthPath)["findings"].([]any) {
		if m := f.(map[string]any); m["id"] == id {
			return m
		}
	}
	t.Fatalf("no finding %s on /health", id)
	return nil
}

// noTyped fails t when m carries typed evidence, a suggested fix or next steps.
func noTyped(t *testing.T, what string, m map[string]any) {
	t.Helper()
	for _, k := range []string{"evidence", "suggestedFix", "nextSteps"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s carries %s: %v", what, k, m[k])
		}
	}
}

// A restricted finding carries no evidence, fix or steps on /health, /overview or its detail;
// a hidden dependency gives evidenceRestricted and no evidence.
func TestTypedEvidenceRestrictedOnHealthAndOverview(t *testing.T) {
	h := xdsHarness(t, setup{}, edgeRejects(t), nackWorld()...)
	nack := xdsFinding(h.at(t, testutil.Now), "infra", "nack")
	if nack == nil || nack.Facts == nil {
		t.Fatalf("001 on nack %+v", nack)
	}
	full := get[FindingDetail](t, h.srv, findingsBase+"/"+nack.ID, 200)
	if _, ok := full.Evidence.(*XDSEvidence); !ok || len(full.NextSteps) != 3 {
		t.Fatalf("full detail: evidence %T, %d steps", full.Evidence, len(full.NextSteps))
	}

	hidden := authzSrv(t, h, deny(testutil.GwRef("infra", "nack").Ref()))
	f := rawFinding(t, hidden, nack.ID)
	if f["restricted"] != true {
		t.Fatalf("hidden Gateway: %v", f)
	}
	noTyped(t, "restricted /health finding", f)
	ov, _ := json.Marshal(rawJSON(t, hidden, base+"/overview"))
	for _, k := range []string{`"evidence"`, `"suggestedFix"`, `"nextSteps"`} {
		if strings.Contains(string(ov), k) {
			t.Errorf("/overview carries %s: %s", k, ov)
		}
	}
	d := rawJSON(t, hidden, findingsBase+"/"+nack.ID)
	noTyped(t, "restricted detail", d)
	if d["finding"].(map[string]any)["restricted"] != true {
		t.Errorf("restricted detail %v", d)
	}

	noPods := AuthorizerFunc(func(_ *http.Request, r objects.Ref) bool { return r.Kind != "Pod" })
	srv := authzSrv(t, h, noPods)
	f = rawFinding(t, srv, nack.ID)
	if f["evidenceRestricted"] != true || f["detail"] != restrictedEvidence {
		t.Fatalf("hidden Pods: %v", f)
	}
	noTyped(t, "evidence-restricted /health finding", f)
	d = rawJSON(t, srv, findingsBase+"/"+nack.ID)
	if _, ok := d["evidence"]; ok || d["finding"].(map[string]any)["evidenceRestricted"] != true {
		t.Errorf("evidence-restricted detail %v", d)
	}
}

// cmdsNoCounters hides the port-forward step.
type cmdsNoCounters struct{ diagnose.Kubectl }

func (cmdsNoCounters) ProxyCounters(objects.Ref) string { return "" }

// rejectingEdge starts nackWorld with clean metrics, adds shop/fresh on edge,
// then turns the captures' rejects on, so fresh changed shortly before them.
func rejectingEdge(t *testing.T, s setup) *harness {
	t.Helper()
	a, b := ctrlCapture(t, "v2.4.7-rejects-a", "kgw", "kgateway-0"), ctrlCapture(t, "v2.4.7-rejects-b", "kgw", "kgateway-1")
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", withoutRejects(a), withoutRejects(b))
	tp := testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: edgeRef, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")}})
	s.disputes = true
	h := xdsHarness(t, s, lm, nackWorld(tp)...)
	h.at(t, testutil.Now)
	createAndWait(h.ctx, t, h.dyn, h.cache, testutil.ServedUpstream(),
		testutil.HTTPRoute("shop", "fresh", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)))
	h.at(t, testutil.Now.Add(5*time.Second))
	lm.set("kgw.kgateway", a, b)
	h.at(t, testutil.Now.Add(10*time.Second))
	return h
}

func groupOn(t *testing.T, d Diagnostics, cause objects.Ref) GroupRow {
	t.Helper()
	for _, g := range d.Groups {
		if g.Cause != nil && g.Cause.Ref == cause {
			return g
		}
	}
	t.Fatalf("no group on %v in %+v", cause, d.Groups)
	return GroupRow{}
}

func refNames(rs []LinkedRef) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Kind+" "+r.Namespace+"/"+r.Name)
	}
	return out
}

// Rules 13–15: affected lists and recently changed objects hold visible objects
// only, steps name only visible objects, and a hidden cause makes a restricted row.
func TestGroupsAndNextStepsHideHiddenObjects(t *testing.T) {
	h := rejectingEdge(t, setup{})
	edge := edgeRef.Ref()
	g := groupOn(t, get[Diagnostics](t, h.srv, diagPath, 200), edge)
	if g.Restricted || g.Code != health.CodeXDSRejected || g.Members != 1 || g.Title == "" || len(g.NextSteps) != 3 ||
		!strings.Contains(g.NextSteps[0].Command, "kgateway-0") || !slices.Equal(refNames(g.Affected), []string{"HTTPRoute shop/cart", "HTTPRoute shop/fresh", "TrafficPolicy shop/tp"}) ||
		g.AffectedTotal != 3 || g.Since == nil || !g.Since.Equal(testutil.Now.Add(10*time.Second)) {
		t.Fatalf("edge group %+v", g)
	}
	f := xdsFinding(get[HealthResponse](t, h.srv, healthPath, 200), "infra", "edge")
	ev, ok := get[FindingDetail](t, h.srv, findingsBase+"/"+f.ID, 200).Evidence.(*XDSEvidence)
	if !ok || !slices.Equal(refNames(ev.RecentlyChanged), []string{"HTTPRoute shop/fresh"}) || ev.RecentlyChanged[0].Link != LinkDetail {
		t.Fatalf("detail evidence %+v", ev)
	}

	// A hidden reporting replica: nack's log step names the other one, and edge's,
	// reported by kgateway-0 alone, is left out.
	hidden := get[Diagnostics](t, authzSrv(t, h, deny(podRef("kgw", "kgateway-0"))), diagPath, 200)
	if g := groupOn(t, hidden, testutil.GwRef("infra", "nack").Ref()); len(g.NextSteps) != 3 || !strings.Contains(g.NextSteps[0].Command, "kgateway-1") {
		t.Fatalf("hidden kgateway-0: nack steps %+v", g.NextSteps)
	}
	if g := groupOn(t, hidden, edge); len(g.NextSteps) != 2 || g.NextSteps[0].Title != diagnose.StepProxyLog {
		t.Fatalf("hidden kgateway-0: edge steps %+v", g.NextSteps)
	}

	// A hidden recently changed object is left out without a count, on the detail and /health.
	srv := authzSrv(t, h, deny(route("shop", "fresh")))
	g = groupOn(t, get[Diagnostics](t, srv, diagPath, 200), edge)
	if !slices.Equal(refNames(g.Affected), []string{"HTTPRoute shop/cart", "TrafficPolicy shop/tp"}) || g.AffectedTotal != 2 {
		t.Fatalf("hidden fresh: group %+v", g)
	}
	dt := rawJSON(t, srv, findingsBase+"/"+f.ID)
	if ev := dt["evidence"].(map[string]any); ev["kind"] != "xds-rejects" || ev["recentlyChanged"] != nil {
		t.Fatalf("hidden fresh: evidence %v", ev)
	}
	if body := string(wire(t, srv, healthPath)); strings.Contains(body, `"fresh"`) {
		t.Fatalf("hidden fresh on /health: %s", body)
	}

	// A hidden cause: severity, code and the members count only.
	g = groupOn(t, get[Diagnostics](t, authzSrv(t, h, deny(edge)), diagPath, 200), edge)
	if !g.Restricted || g.Title != "" || g.Code != health.CodeXDSRejected || g.Severity != health.SeverityError || g.Members != 1 ||
		len(g.Affected) != 0 || g.AffectedTotal != 0 || g.Since != nil || g.NextSteps != nil || g.Cause.Link != LinkNone || g.Affected == nil {
		t.Fatalf("hidden edge: %+v", g)
	}

	// A command that returns "" hides its step; the others keep their order.
	g = groupOn(t, get[Diagnostics](t, authzSrv(t, h, nil, WithCommands(cmdsNoCounters{})), diagPath, 200), edge)
	if len(g.NextSteps) != 2 || g.NextSteps[0].Title != diagnose.StepEnvoyReason || g.NextSteps[1].Title != diagnose.StepProxyLog {
		t.Fatalf("no counters command: %+v", g.NextSteps)
	}
}

// brokenRoute is an accepted route on edge whose refs do not resolve (KGWUI-002).
func brokenRoute(name string) *unstructured.Unstructured {
	return testutil.HTTPRoute("shop", name, []objects.ParentRef{edgeRef}, testutil.RouteParent{Ref: edgeRef, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
}

// The first 10 groups with a problem in rank order, the first 5 affected
// objects of each, and the verdict over every finding; info groups are left out.
func TestDiagnosticsTopTen(t *testing.T) {
	var extra []runtime.Object
	for _, n := range []string{"b01", "b02", "b03", "b04", "b05", "b06", "b07", "b08", "b09", "b10", "b11", "b12"} {
		extra = append(extra, brokenRoute(n))
	}
	h := xdsHarness(t, setup{disputes: true, late: []*unstructured.Unstructured{testutil.HTTPRoute("shop", "young", []objects.ParentRef{edgeRef})}},
		edgeRejects(t), nackWorld(extra...)...)
	r := h.at(t, testutil.Now)
	d := get[Diagnostics](t, h.srv, diagPath, 200)
	m := h.e.Current().Model.Installs[0]
	var want []string
	for _, g := range m.Diagnosis.Groups {
		if g.Ranked && g.Severity != health.SeverityInfo {
			want = append(want, g.ID)
		}
	}
	var got []string
	for _, g := range d.Groups {
		got = append(got, g.ID)
	}
	if len(want) < 14 || d.GroupTotal != len(want) || !slices.Equal(got, want[:10]) {
		t.Fatalf("groups %v total %d, want the first 10 of %v", got, d.GroupTotal, want)
	}
	if !hasSev(r, health.CodeRouteUnattached, health.SeverityInfo, "") {
		t.Fatal("no info finding to leave out")
	}
	eg := groupOn(t, d, edgeRef.Ref())
	if len(eg.Affected) != 5 || eg.AffectedTotal != 13 || !slices.IsSortedFunc(eg.Affected, func(a, b LinkedRef) int { return compareAffected(a.Ref, b.Ref) }) {
		t.Fatalf("edge affected %v of %d", refNames(eg.Affected), eg.AffectedTotal)
	}
	if d.Groups[0].Class != diagnose.ClassDataPlane || d.Groups[0].Severity != health.SeverityError {
		t.Fatalf("first group %+v", d.Groups[0])
	}
	var oldest *time.Time
	for _, f := range r.Findings {
		if f.Severity != health.SeverityInfo && f.Since != nil && (oldest == nil || f.Since.Before(*oldest)) {
			oldest = f.Since
		}
	}
	v := d.Verdict
	if v.State != r.State || v.Problems != r.Counts[health.SeverityError]+r.Counts[health.SeverityWarning] || !maps.Equal(v.Counts, r.Counts) ||
		v.OldestSince == nil || !v.OldestSince.Equal(*oldest) || v.Reason != "" {
		t.Fatalf("verdict %+v, health %s %v oldest %v", v, r.State, r.Counts, oldest)
	}
	if d.DataPlane != (DataPlaneSummary{Gateways: 2, Rejecting: 2}) || len(d.Checks) != len(r.Checks) || d.Install != "kgw.kgateway" || !d.CollectedAt.Equal(r.CollectedAt) {
		t.Fatalf("data plane %+v, %d checks", d.DataPlane, len(d.Checks))
	}
	if c := d.Processes.Controller; !c.Observed || c.Desired == nil || *c.Desired != 2 || c.Ready == nil || *c.Ready != 2 || c.Version != "2.4.7" {
		b, _ := json.Marshal(c)
		t.Fatalf("controller summary %s", b)
	}
	if rawJSON(t, h.srv, diagPath+"?x=1")["error"] == nil {
		t.Fatal("unknown parameter accepted")
	}

	// Unknown: the reason names the required check.
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", ctrlCapture(t, "v2.4.7-rejects-a", "kgw", "kgateway-0"))
	lm.fail("kgw.kgateway", "scrape kgateway-1: connection refused")
	u := xdsHarness(t, setup{}, lm, nackWorld()...)
	u.at(t, testutil.Now)
	if d := get[Diagnostics](t, u.srv, diagPath, 200); d.Verdict.State != health.StateUnknown || d.Verdict.Reason != "xDS rejects" || !d.DataPlane.Unavailable {
		t.Fatalf("unknown verdict %+v, data plane %+v", d.Verdict, d.DataPlane)
	}
}

// gwProxy is a proxy Deployment for infra/name with its selector and Pod template labels.
func gwProxy(name string, desired, ready int32) *appsv1.Deployment {
	d := testutil.ProxyDeployment("infra", name, desired, ready)
	sel := map[string]string{"app.kubernetes.io/name": name, "gateway.networking.k8s.io/gateway-name": name}
	d.Spec.Selector = &metav1.LabelSelector{MatchLabels: sel}
	d.Spec.Template.Labels = maps.Clone(sel)
	maps.Copy(d.Spec.Template.Labels, map[string]string{"kgateway": "kube-gateway", "app.kubernetes.io/component": "proxy"})
	return d
}

// gwPod is a ready Pod of d on tag.
func gwPod(d *appsv1.Deployment, name, tag string) *corev1.Pod {
	p := edgePod(d, name, true, corev1.ContainerStatus{State: runningSince(testutil.Old.Time)})
	p.Spec.Containers[0].Image = "cr.kgateway.dev/kgateway-dev/envoy-wrapper:" + tag
	return p
}

// procsWorld: edge's proxy runs a patch behind the controller and restarts
// (KGWUI-019); calm's proxy matches it; lone is self-managed.
func procsWorld(t *testing.T, s setup) *harness {
	t.Helper()
	ed, cd := edgeProxy(1, 1), gwProxy("calm", 1, 1)
	edge0 := gwPod(ed, "edge-0", "v2.4.5")
	lone := testutil.Gateway("infra", "lone", "kgateway", nil)
	testutil.SetGatewayParameters(lone, "gateway.kgateway.dev", "GatewayParameters", "sm")
	objs := append(procWorld(nil, ed, edge0, gwPod(cd, "calm-0", "v2.4.7")), cd,
		testutil.Gateway("infra", "calm", "kgateway", nil), lone, testutil.GatewayParameters("infra", "sm", true))
	s.proxyPods, s.disputes = true, true
	h := newHarness(t, s, objs...)
	h.at(t, testutil.Now)
	restart := testutil.Now.Add(-time.Minute)
	edge0.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "envoy", RestartCount: 1, State: runningSince(restart), LastTerminationState: exitedAt("Error", 1, restart)}}
	edge0.ResourceVersion = ""
	p, err := h.kube.CoreV1().Pods("infra").Get(h.ctx, "edge-0", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p.Status = edge0.Status
	h.updatePod(t, p)
	h.at(t, testutil.Now)
	return h
}

func gatewayNames(p Page[GatewayProcesses]) []string {
	out := make([]string, 0, len(p.Items))
	for _, g := range p.Items {
		out = append(out, g.Gateway.Name)
	}
	return out
}

// The processes filters each select the Gateways they name, the page pages,
// and a bad value is a 400.
func TestProcessesFilters(t *testing.T) {
	h := procsWorld(t, setup{})
	all := get[ProcessesResponse](t, h.srv, procsPath, 200)
	if got := gatewayNames(all.Proxies); !slices.Equal(got, []string{"calm", "edge", "lone"}) || all.Proxies.Total != 3 || all.Install != "kgw.kgateway" {
		t.Fatalf("all %v total %d", got, all.Proxies.Total)
	}
	byName := map[string]GatewayProcesses{}
	for _, g := range all.Proxies.Items {
		byName[g.Gateway.Name] = g
	}
	if e := byName["edge"]; e.Health != health.StateWarning || !e.Skew || len(e.Set.Processes) != 1 || e.Set.Processes[0].Version != "2.4.5" ||
		e.Set.Processes[0].RestartsRecent == nil || *e.Set.Processes[0].RestartsRecent != 1 || e.Gateway.Link != LinkDetail {
		t.Fatalf("edge %+v", e)
	}
	if c := byName["calm"]; c.Health != health.StateHealthy || c.Skew || !c.Set.Observed || c.Set.Ready == nil || *c.Set.Ready != 1 {
		t.Fatalf("calm %+v", c)
	}
	if l := byName["lone"]; l.Health != health.StateUnknown || l.Set.Observed || l.Set.Processes == nil {
		t.Fatalf("lone %+v", l)
	}
	if c := all.Controller; !c.Observed || len(c.Processes) != 1 || c.Processes[0].Name != "kgateway-0" || c.Processes[0].Version != "2.4.7" {
		t.Fatalf("controller %+v", c)
	}
	for q, want := range map[string][]string{
		"health=warning": {"edge"}, "health=healthy": {"calm"}, "health=unknown": {"lone"}, "health=error": {},
		"health=healthy&health=unknown": {"calm", "lone"},
		"skew=true":                     {"edge"}, "skew=false": {"calm", "lone"}, "restarts=true": {"edge"}, "restarts=false": {"calm", "lone"},
		"q=CAL": {"calm"}, "q=infra/l": {"lone"}, "skew=false&restarts=false&q=lo": {"lone"},
	} {
		p := get[ProcessesResponse](t, h.srv, procsPath+"?"+q, 200).Proxies
		if got := gatewayNames(p); !slices.Equal(got, want) || p.Total != len(want) {
			t.Errorf("%s: %v total %d, want %v", q, got, p.Total, want)
		}
	}
	p1 := get[ProcessesResponse](t, h.srv, procsPath+"?limit=2", 200).Proxies
	p2 := get[ProcessesResponse](t, h.srv, procsPath+"?limit=2&cursor="+p1.Next, 200).Proxies
	back := get[ProcessesResponse](t, h.srv, procsPath+"?limit=2&cursor="+p2.Prev, 200).Proxies
	if !slices.Equal(gatewayNames(p1), []string{"calm", "edge"}) || !slices.Equal(gatewayNames(p2), []string{"lone"}) || p2.Next != "" ||
		!slices.Equal(gatewayNames(back), []string{"calm", "edge"}) {
		t.Fatalf("pages %v %v %v", gatewayNames(p1), gatewayNames(p2), gatewayNames(back))
	}
	for _, q := range []string{"health=bogus", "skew=maybe", "restarts=1", "skew=true&skew=false", "q=a&q=b", "limit=0", "limit=x",
		"cursor=nope", "sort=name", "x=1", "cursor=" + affectedCursorFor(t)} {
		if rawJSON(t, h.srv, procsPath+"?"+q)["error"] == nil {
			t.Errorf("%s: no 400", q)
		}
		get[ErrorResponse](t, h.srv, procsPath+"?"+q, 400)
	}
}

// affectedCursorFor is a well-formed affected cursor, which the processes list must refuse.
func affectedCursorFor(*testing.T) string {
	return encodeRefCursor(sortAffected, false, route("shop", "cart"))
}

// Rule 16: a hidden Pod leaves its set's counts only, a hidden controller
// Deployment restricts the controller set, a hidden Gateway is not listed.
func TestProcessSetCountsOnlyWhenPodHidden(t *testing.T) {
	h := procsWorld(t, setup{})
	srv := authzSrv(t, h, deny(podRef("infra", "edge-0"), podRef("kgw", "kgateway-0"), testutil.GwRef("infra", "calm").Ref()))
	r := get[ProcessesResponse](t, srv, procsPath, 200)
	if got := gatewayNames(r.Proxies); !slices.Equal(got, []string{"edge", "lone"}) || r.Proxies.Total != 2 || r.Proxies.DeniedInInstall != 1 {
		t.Fatalf("hidden calm: %v total %d denied %d", got, r.Proxies.Total, r.Proxies.DeniedInInstall)
	}
	e := r.Proxies.Items[0]
	if !e.Set.Observed || len(e.Set.Processes) != 0 || e.Set.Ready == nil || *e.Set.Ready != 1 || e.Set.Desired == nil || *e.Set.Desired != 1 || e.Health != health.StateWarning {
		t.Fatalf("hidden edge-0: %+v", e)
	}
	if c := r.Controller; c.Restricted || len(c.Processes) != 0 || c.Ready == nil || *c.Ready != 1 {
		t.Fatalf("hidden kgateway-0: %+v", c)
	}
	if body := string(wire(t, srv, procsPath)); strings.Contains(body, "edge-0") || strings.Contains(body, "kgateway-0") {
		t.Fatalf("hidden Pod names in %s", body)
	}

	// Skew needs a visible proxy: edge-0 hidden reads no skew, and skew=true leaves edge out.
	if e.Skew || len(get[ProcessesResponse](t, srv, procsPath+"?skew=true", 200).Proxies.Items) != 0 ||
		get[Diagnostics](t, srv, diagPath, 200).Processes.Proxies.Skew != 0 {
		t.Fatalf("hidden edge-0 reads skew: %+v", e)
	}
	// A hidden controller and edge-0: no version is learnt through either.
	hid := authzSrv(t, h, deny(ctrlDeployment, podRef("infra", "edge-0")))
	if p := get[ProcessesResponse](t, hid, procsPath+"?skew=true", 200).Proxies; len(p.Items) != 0 {
		t.Fatalf("hidden controller and edge-0: skew rows %v", gatewayNames(p))
	}

	srv = authzSrv(t, h, deny(ctrlDeployment))
	r = get[ProcessesResponse](t, srv, procsPath, 200)
	if c := r.Controller; !c.Restricted || len(c.Processes) != 0 || c.Rollout != nil || c.Desired == nil || *c.Desired != 1 {
		t.Fatalf("hidden controller Deployment: %+v", c)
	}
	if c := get[Diagnostics](t, srv, diagPath, 200).Processes.Controller; !c.Restricted || c.Version != "" || c.Desired == nil {
		t.Fatalf("hidden controller summary %+v", c)
	}
	d := get[Diagnostics](t, h.srv, diagPath, 200).Processes
	if d.Proxies != (ProxySummary{State: health.StateUnknown, Gateways: 3, Ready: 2, Skew: 1, Restarting: 1, NotObserved: 1}) || d.Controller.Version != "2.4.7" {
		t.Fatalf("summaries %+v", d)
	}
}

// evidenceRow is one row of the §14 rule 12 dependency table: a world raising
// code with evidence of kind, and the dependency whose hiding restricts it.
type evidenceRow struct {
	code, kind string
	hide       objects.Ref
	world      func(t *testing.T) *harness
	// gw names the 001's Gateway; other codes have one finding.
	gw string
}

// The dependency table at the finding detail and /health: the full caller
// gets the typed evidence, one without the dependency gets evidenceRestricted.
func TestEvidenceDependencyTable(t *testing.T) {
	restarted := restartedWorld
	probing := func(t *testing.T) *harness {
		h, pod := probeWorld(t, false)
		ev := testutil.WarningEvent(pod, "probe", "Unhealthy", testutil.Now)
		h.putEvent(t, ev, true, 1, false)
		ev.Count = 3
		h.putEvent(t, ev, false, 3, false)
		h.at(t, testutil.Now)
		return h
	}
	stuck := func(t *testing.T) *harness {
		d := edgeProxy(1, 1)
		d.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded"}}
		old := edgePod(d, "edge-old", true, corev1.ContainerStatus{State: runningSince(testutil.Old.Time)})
		h := newHarness(t, setup{proxyPods: true}, procWorld(nil, d, old, edgePod(d, "edge-new", false, corev1.ContainerStatus{State: waiting("ImagePullBackOff")}))...)
		h.at(t, testutil.Now)
		return h
	}
	skewed := func(t *testing.T) *harness {
		h := newHarness(t, setup{proxyPods: true}, skewWorld("v2.3.6", testutil.Old.Time)...)
		h.at(t, testutil.Now)
		return h
	}
	proxyCond := func(apply func(*proxystats.PodStats)) func(t *testing.T) *harness {
		return func(t *testing.T) *harness { return proxyStatsWorld(t, apply) }
	}
	rejects := func(t *testing.T) *harness {
		lm := newLiveMetrics()
		lm.set("kgw.kgateway", ctrlCapture(t, "v2.4.7-rejects-a", "kgw", "kgateway-0"), ctrlCapture(t, "v2.4.7-rejects-b", "kgw", "kgateway-1"))
		sh := newStatsHarness(t, setup{}, lm, nackWorld()...)
		sh.src.set(gwRef("infra", "nack"), pod("nack-0", nil))
		sh.at(t, testutil.Now)
		return sh.harness
	}
	no, warm := false, int64(1)
	for _, row := range []evidenceRow{
		{health.CodeXDSRejected, health.EvidenceXDSRejects, podRef("kgw", "kgateway-1"), rejects, "nack"},
		{health.CodeXDSRejected, health.EvidenceXDSRejects, podRef("infra", "nack-0"), rejects, "nack"},
		{health.CodeProcessRestarting, health.EvidenceProcess, podRef("kgw", "kgateway-0"), restarted, ""},
		{health.CodeProbeFailing, health.EvidenceProcess, podRef("kgw", "kgateway-0"), probing, ""},
		{health.CodeRolloutStuck, health.EvidenceProcess, podRef("infra", "edge-new"), stuck, ""},
		{health.CodeVersionSkew, health.EvidenceVersionSkew, podRef("infra", "edge-0"), skewed, ""},
		{health.CodeProxyConfigWarming, health.EvidenceProxyStats, podRef("infra", "edge-0"), proxyCond(func(p *proxystats.PodStats) { p.WarmingClusters = &warm }), ""},
		{health.CodeProxyDisconnected, health.EvidenceProxyStats, podRef("infra", "edge-0"), proxyCond(func(p *proxystats.PodStats) { p.Connected = &no }), ""},
	} {
		t.Run(row.code+" "+row.hide.Name, func(t *testing.T) {
			h := row.world(t)
			r := get[HealthResponse](t, h.srv, healthPath, 200)
			f := findingOf(r, row.code)
			if row.gw != "" {
				f = xdsFinding(r, "infra", row.gw)
			}
			if f == nil {
				t.Fatalf("no %s", row.code)
			}
			full := rawJSON(t, h.srv, findingsBase+"/"+f.ID)
			if ev, _ := full["evidence"].(map[string]any); ev["kind"] != row.kind {
				t.Fatalf("full detail evidence %v", full["evidence"])
			}
			srv := authzSrv(t, h, deny(row.hide))
			d := rawJSON(t, srv, findingsBase+"/"+f.ID)
			if _, ok := d["evidence"]; ok || d["finding"].(map[string]any)["evidenceRestricted"] != true {
				t.Fatalf("hidden %v: detail %v", row.hide, d)
			}
			if hf := rawFinding(t, srv, f.ID); hf["evidenceRestricted"] != true || hf["evidence"] != nil {
				t.Fatalf("hidden %v: /health %v", row.hide, hf)
			}
		})
	}
}

// The 019 detail of a controller panic carries the previous-log step, naming the
// Pod and container; hiding the Pod drops it with the evidence.
func TestFindingDetailProcessNextStep(t *testing.T) {
	h := restartedWorld(t)
	f := findingOf(get[HealthResponse](t, h.srv, healthPath, 200), health.CodeProcessRestarting)
	d := get[FindingDetail](t, h.srv, findingsBase+"/"+f.ID, 200)
	if len(d.NextSteps) != 1 || d.NextSteps[0].Title != diagnose.StepPanicTrace || d.NextSteps[0].Command != "kubectl logs --previous -n kgw kgateway-0 -c kgateway" ||
		!slices.Equal(d.NextSteps[0].Needs, []string{"pods/log"}) {
		t.Fatalf("steps %+v", d.NextSteps)
	}
	if d := get[FindingDetail](t, authzSrv(t, h, deny(podRef("kgw", "kgateway-0"))), findingsBase+"/"+f.ID, 200); d.NextSteps != nil || d.Evidence != nil {
		t.Fatalf("hidden Pod: %+v", d)
	}
	g := get[Diagnostics](t, h.srv, diagPath, 200)
	if pg := groupOn(t, g, ctrlDeployment); pg.Code != health.CodeProcessRestarting || len(pg.NextSteps) != 1 || pg.Class != diagnose.ClassProcesses {
		t.Fatalf("process group %+v", pg)
	}
	if g.Processes.Proxies.Restarting != 0 || g.Processes.Controller.Version != "2.4.7" {
		t.Fatalf("summaries %+v", g.Processes)
	}
}

// diagGoldenWorld is rejectingEdge with proxy stats: shop/fresh-n on nack
// changed before the rejects, and nack-0's listener counters grow at the reject.
func diagGoldenWorld(t *testing.T) *harness {
	t.Helper()
	a, b := ctrlCapture(t, "v2.4.7-rejects-a", "kgw", "kgateway-0"), ctrlCapture(t, "v2.4.7-rejects-b", "kgw", "kgateway-1")
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", withoutRejects(a), withoutRejects(b))
	tp := testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: edgeRef, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")}})
	sh := newStatsHarness(t, setup{disputes: true}, lm, nackWorld(tp)...)
	nack := testutil.GwRef("infra", "nack")
	base := rejectsCounters(proxystats.Pair{Rejected: 3, Success: 8}, 4, 1)
	sh.src.set(nack.Ref(), pod("nack-0", base), pod("nack-1", nil))
	sh.at(t, testutil.Now)
	createAndWait(sh.ctx, t, sh.dyn, sh.cache, testutil.ServedUpstream(), testutil.HTTPRoute("shop", "fresh-n", []objects.ParentRef{nack}))
	sh.at(t, testutil.Now.Add(5*time.Second))
	lm.set("kgw.kgateway", a, b)
	sh.src.set(nack.Ref(), pod("nack-0", rejectsCounters(proxystats.Pair{Rejected: 4, Success: 9}, 5, 2)), pod("nack-1", nil))
	sh.at(t, testutil.Now.Add(10*time.Second))
	return sh.harness
}

// restartedWorld: the controller Pod restarted a minute ago with exit code 2 (KGWUI-019).
func restartedWorld(t *testing.T) *harness {
	t.Helper()
	restart := testutil.Now.Add(-time.Minute)
	h := newHarness(t, setup{}, procWorld(container(corev1.ContainerStatus{State: runningSince(testutil.Old.Time)}), nil)...)
	h.at(t, testutil.Now)
	p := h.ctrlPod(t)
	container(corev1.ContainerStatus{RestartCount: 1, State: runningSince(restart), LastTerminationState: exitedAt("Error", 2, restart)})(p)
	h.updatePod(t, p)
	h.at(t, testutil.Now)
	return h
}

// proxyStatsWorld: edge-0, Ready, reports apply's stats for 61 s (KGWUI-025 or 026).
func proxyStatsWorld(t *testing.T, apply func(*proxystats.PodStats)) *harness {
	t.Helper()
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", cleanMetrics("pod/kgw/kgateway-0", "kgateway-0"))
	sh := newStatsHarness(t, setup{proxyPods: true}, lm, edgeStatsWorld()...)
	sh.src.set(gwRef("infra", "edge"), pod("edge-0", apply))
	sh.at(t, testutil.Now)
	sh.at(t, testutil.Now.Add(61*time.Second))
	return sh.harness
}

// A group row's title quotes its top member only when the caller may view that member's resource.
func TestGroupTitleNeedsTheMember(t *testing.T) {
	gw, hidden := edgeRef.Ref(), route("shop", "cart")
	fs := []health.Finding{{Code: health.CodeConditionFalse, Severity: health.SeverityError, Resource: &hidden, Message: "hidden message"}}
	g := &diagnose.Group{ID: "g", Cause: &gw, Members: []int{0}, Severity: health.SeverityError}
	h := &handler{commands: diagnose.Kubectl{}}
	v := &view{a: deny(hidden), seen: map[objects.Ref]bool{}}
	l := linkerFor(v, &engine.Model{}, &engine.InstallModel{})
	if row := h.groupRow(v, l, g, fs, 0, diagnose.Input{}); row.Restricted || row.Title != hiddenTitle {
		t.Fatalf("row %+v", row)
	}
}

// The process summaries carry the server's state: a controller at 0/0 desired is KGWUI-005's
// error, never healthy; a restricted caller keeps that state with the counts.
func TestProcessSummaryState(t *testing.T) {
	zero := newHarness(t, setup{}, replicaWorld(0, 0, 1, 1)...)
	zero.at(t, testutil.Now)
	c := get[Diagnostics](t, zero.srv, diagPath, 200).Processes.Controller
	if c.State != health.StateError || c.Desired == nil || *c.Desired != 0 {
		b, _ := json.Marshal(c)
		t.Fatalf("controller at 0/0: %s", b)
	}
	if c := get[Diagnostics](t, authzSrv(t, zero, deny(ctrlDeployment)), diagPath, 200).Processes.Controller; !c.Restricted || c.State != health.StateError {
		t.Fatalf("hidden controller at 0/0: %+v", c)
	}

	ok := newHarness(t, setup{}, world()...)
	ok.at(t, testutil.Now)
	if p := get[Diagnostics](t, ok.srv, diagPath, 200).Processes; p.Controller.State != health.StateHealthy {
		t.Fatalf("ready controller %+v", p.Controller)
	}

	// Proxies: error > unknown > warning > healthy, as Gateway rows (D-006). lone is unobserved, so
	// the line is unknown beside edge's warning and, with edge hidden, beside calm's healthy.
	h := procsWorld(t, setup{})
	if p := get[Diagnostics](t, h.srv, diagPath, 200).Processes.Proxies; p.State != health.StateUnknown {
		t.Fatalf("proxies %+v", p)
	}
	noEdge := authzSrv(t, h, deny(testutil.GwRef("infra", "edge").Ref()))
	if p := get[Diagnostics](t, noEdge, diagPath, 200).Processes.Proxies; p.State != health.StateUnknown || p.Gateways != 2 {
		t.Fatalf("healthy calm and unobserved lone %+v", p)
	}
	onlyCalm := authzSrv(t, h, deny(testutil.GwRef("infra", "edge").Ref(), testutil.GwRef("infra", "lone").Ref()))
	if p := get[Diagnostics](t, onlyCalm, diagPath, 200).Processes.Proxies; p.State != health.StateHealthy || p.Gateways != 1 {
		t.Fatalf("healthy calm alone %+v", p)
	}
	hidden := authzSrv(t, h, deny(testutil.GwRef("infra", "edge").Ref(), testutil.GwRef("infra", "calm").Ref(), testutil.GwRef("infra", "lone").Ref()))
	if p := get[Diagnostics](t, hidden, diagPath, 200).Processes.Proxies; p.State != health.StateUnknown || p.Gateways != 0 {
		t.Fatalf("every Gateway hidden %+v", p)
	}
}
