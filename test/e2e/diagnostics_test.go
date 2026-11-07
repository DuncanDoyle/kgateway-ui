//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/diagnose"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

// Envoy xDS types as the controller metrics and the API name them.
const (
	typeCluster = "envoy.config.cluster.v3.Cluster"
	typeRoutes  = "envoy.config.route.v3.RouteConfiguration"
)

// finding is the finding with code on the object kind ns/name in h, or nil.
func finding(h api.HealthResponse, code, kind, id string) *api.FindingDTO {
	for i, f := range h.Findings {
		if f.Code == code && f.Resource != nil && f.Resource.Kind == kind && f.Resource.String() == id {
			return &h.Findings[i]
		}
	}
	return nil
}

// checkResult is the check named name in h, or nil.
func checkResult(h api.HealthResponse, name string) *health.CheckResult {
	for i := range h.Checks {
		if h.Checks[i].Name == name {
			return &h.Checks[i]
		}
	}
	return nil
}

// healthNow is one /health of install A.
func healthNow(t *testing.T) api.HealthResponse {
	t.Helper()
	var h api.HealthResponse
	get(t, install+"/health", &h)
	return h
}

// waitHealth polls /health every second until ok accepts a response, or fails after limit.
func waitHealth(t *testing.T, limit time.Duration, what string, ok func(api.HealthResponse) bool) api.HealthResponse {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		h := healthNow(t)
		if ok(h) {
			return h
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: not seen within %s; findings %s", what, limit, codes(h))
		}
		time.Sleep(time.Second)
	}
}

// codes lists h's findings as code@kind/ns/name, for failure messages.
func codes(h api.HealthResponse) string {
	var out []string
	for _, f := range h.Findings {
		if f.Resource != nil {
			out = append(out, f.Code+"@"+f.Resource.Kind+"/"+f.Resource.String())
		}
	}
	return strings.Join(out, " ")
}

// podNames lists the names of the Pods in ns matching selector, terminating ones left out.
func podNames(t *testing.T, ns, selector string) []string {
	t.Helper()
	var pods struct {
		Items []struct {
			Metadata struct {
				Name              string     `json:"name"`
				DeletionTimestamp *time.Time `json:"deletionTimestamp"`
			} `json:"metadata"`
		} `json:"items"`
	}
	kubectl(t, &pods, "-n", ns, "get", "pods", "-l", selector, "-o", "json")
	var out []string
	for _, p := range pods.Items {
		if p.Metadata.DeletionTimestamp == nil {
			out = append(out, p.Metadata.Name)
		}
	}
	return out
}

const (
	ctrlSelector = "app.kubernetes.io/name=kgateway"
	nackSelector = "gateway.networking.k8s.io/gateway-name=nack"
	nackID       = "infra/nack"
)

// rejecting reports a 001 on infra/nack that names both rejected types.
func rejecting(h api.HealthResponse) bool {
	f := finding(h, health.CodeXDSRejected, "Gateway", nackID)
	if f == nil {
		return false
	}
	ev, _ := f.Facts.(*health.XDSRejectsEvidence)
	return ev != nil && slices.ContainsFunc(ev.Types, func(x health.XDSTypeRejects) bool { return x.TypeURL == typeRoutes }) &&
		slices.ContainsFunc(ev.Types, func(x health.XDSTypeRejects) bool { return x.TypeURL == typeCluster })
}

// controllerSettled waits until install A has one ready controller Pod and 001 on infra/nack is back.
func controllerSettled(t *testing.T) {
	t.Helper()
	must(t, "-n", "kgw-a", "rollout", "status", "deploy/kgateway", "--timeout=180s")
	waitHealth(t, 3*time.Minute, "001 on infra/nack after the controller settled", func(h api.HealthResponse) bool {
		c := checkResult(h, "xDS rejects")
		return rejecting(h) && c != nil && c.OK
	})
}

// TestDataPlaneDiagnostics drives the NACK fixture on kind (design §21): KGWUI-001 with its evidence, a stuck
// proxy rollout (021, and 006 for a new Gateway), the restart hold, a container restart (019) and a
// controller scaled to zero (026). Subtests run in order and restore the cluster before the next.
func TestDataPlaneDiagnostics(t *testing.T) {
	if !nack() {
		t.Skip("needs the NACK fixture (NACK=1 test/e2e/setup.sh)")
	}
	controllerSettled(t)
	t.Run("rejects", testRejects)
	t.Run("stuck rollout", testStuckRollout)
	t.Run("restart hold", testRestartHold)
	t.Run("container restart", testContainerRestart)
	t.Run("controller scaled to zero", testControllerDown)
}

// testRejects checks 001 on infra/nack: the stream counts, the status-says-healthy message, the reporting
// replica in the log command, and a rejecting proxy Pod with the Envoy route config from its stats.
func testRejects(t *testing.T) {
	ctrl := podNames(t, "kgw-a", ctrlSelector)
	proxies := podNames(t, "infra", nackSelector)
	if len(ctrl) != 1 || len(proxies) != 2 {
		t.Fatalf("want 1 controller Pod and 2 nack proxies, got %v and %v", ctrl, proxies)
	}
	// Proxy stats name an Envoy resource only when its counter grew since the last scrape; the controller re-pushes often.
	h := waitHealth(t, 2*time.Minute, "001 on infra/nack with an Envoy route config from proxy stats", func(h api.HealthResponse) bool {
		f := finding(h, health.CodeXDSRejected, "Gateway", nackID)
		ev, _ := factsOf(f).(*health.XDSRejectsEvidence)
		return rejecting(h) && ev != nil && slices.ContainsFunc(ev.ProxyStats, func(p health.ProxyRejects) bool {
			return slices.ContainsFunc(p.Types, func(x health.ProxyTypeRejects) bool { return x.TypeURL == typeRoutes && len(x.EnvoyResources) > 0 })
		})
	})
	f := finding(h, health.CodeXDSRejected, "Gateway", nackID)
	ev := f.Facts.(*health.XDSRejectsEvidence)
	if f.Severity != health.SeverityError || f.Message != "Status says Programmed, but proxies reject its config" || !ev.StatusSaysHealthy {
		t.Errorf("001 on %s: severity %s, message %q, statusSaysHealthy %v", nackID, f.Severity, f.Message, ev.StatusSaysHealthy)
	}
	for _, x := range ev.Types {
		// Each of the two proxies holds one stream to the single controller replica.
		if x.Streams != len(proxies) || !slices.Equal(x.ReportedBy, ctrl) {
			t.Errorf("001 on %s, %s: %d streams reported by %v; want %d by %v", nackID, x.TypeURL, x.Streams, x.ReportedBy, len(proxies), ctrl)
		}
	}
	if ev.Proxies == nil || ev.Proxies.Ready != 2 || ev.Proxies.Desired != 2 {
		t.Errorf("001 on %s: proxies %+v, want 2/2", nackID, ev.Proxies)
	}
	for _, p := range ev.ProxyStats {
		if !slices.Contains(proxies, p.Pod) {
			t.Errorf("001 on %s: proxy stats of Pod %s, not one of %v", nackID, p.Pod, proxies)
		}
		for _, x := range p.Types {
			if len(x.EnvoyResources) > 0 {
				t.Logf("Envoy %s names from proxy %s: %v", x.TypeURL, p.Pod, x.EnvoyResources)
			}
			if x.TypeURL == typeRoutes && len(x.EnvoyResources) > 0 && !slices.Contains(x.EnvoyResources, "listener~8080") {
				t.Errorf("proxy %s: route configs %v, want listener~8080", p.Pod, x.EnvoyResources)
			}
		}
	}
	// Clusters reach every proxy on 2.3 and 2.4, so edge rejects the Cluster too, but never the route config.
	if e := finding(h, health.CodeXDSRejected, "Gateway", "infra/edge"); e != nil {
		for _, x := range e.Facts.(*health.XDSRejectsEvidence).Types {
			if x.TypeURL != typeCluster {
				t.Errorf("001 on infra/edge rejects %s; only the Cluster reaches it", x.TypeURL)
			}
		}
	}

	// The next steps name the replica that reported the reject.
	wantLog := fmt.Sprintf("kubectl logs -n kgw-a %s --since=1h | grep 'xds error' | grep nack", ctrl[0])
	var d api.Diagnostics
	get(t, install+"/diagnostics", &d)
	i := slices.IndexFunc(d.Groups, func(g api.GroupRow) bool {
		return g.Code == health.CodeXDSRejected && g.Cause != nil && g.Cause.String() == nackID
	})
	if i < 0 {
		t.Fatalf("diagnostics: no 001 group caused by %s in %+v", nackID, d.Groups)
	}
	g := d.Groups[i]
	if len(g.NextSteps) == 0 || g.NextSteps[0].Command != wantLog || !slices.Contains(g.NextSteps[0].Needs, "pods/log") {
		t.Errorf("001 group next steps %+v; want first %q needing pods/log", g.NextSteps, wantLog)
	}
	var affected []string
	for _, a := range g.Affected {
		affected = append(affected, a.Kind+" "+a.String())
	}
	if !slices.Contains(affected, "HTTPRoute shop/nack-regex") || !slices.Contains(affected, "HTTPRoute shop/nack-cluster") {
		t.Errorf("001 group affected %v; want both nack routes", affected)
	}
	if d.DataPlane.Rejecting < 1 || d.DataPlane.Unavailable {
		t.Errorf("diagnostics data plane %+v", d.DataPlane)
	}

	var gw api.GatewayDetail
	get(t, install+"/gateways/infra/nack", &gw)
	dp := gw.DataPlane
	if dp == nil || !dp.Rejecting || !dp.StatusSaysHealthy || dp.Unavailable || !dp.ProxyStats.Available || len(dp.Rejects) != 2 {
		t.Fatalf("Gateway detail data plane %+v", dp)
	}
	if len(dp.NextSteps) == 0 || dp.NextSteps[0].Command != wantLog {
		t.Errorf("Gateway detail next steps %+v; want first %q", dp.NextSteps, wantLog)
	}
	if dp.Snapshot != nil && dp.Snapshot.By != "controller" {
		t.Errorf("snapshot by %q, want controller", dp.Snapshot.By)
	}
	if gw.Proxies == nil || !gw.Proxies.Observed || deref(gw.Proxies.Ready, 0) != 2 || len(gw.Proxies.Processes) != 2 {
		t.Errorf("Gateway detail proxies %+v", gw.Proxies)
	}
}

// factsOf is f's evidence; nil when f is nil.
func factsOf(f *api.FindingDTO) health.Evidence {
	if f == nil {
		return nil
	}
	return f.Facts
}

// testStuckRollout: a new Gateway whose proxy image cannot be pulled is 006, never 021, as no replica is
// ready; once it runs, a rollout to that image beside the ready old Pod passes its deadline and is 021.
func testStuckRollout(t *testing.T) {
	const id = "infra/stuck"
	const fixture = "fixtures/diagnostics.yaml"
	t.Cleanup(func() {
		if out, err := kubectlRun(t, "delete", "-f", fixture, "--ignore-not-found", "--wait=true"); err != nil {
			t.Errorf("cleanup: delete %s: %v: %s", fixture, err, out)
		}
		waitHealth(t, 2*time.Minute, "no finding on infra/stuck after cleanup", func(h api.HealthResponse) bool {
			return !slices.ContainsFunc(h.Findings, func(f api.FindingDTO) bool { return f.Resource != nil && f.Resource.String() == id })
		})
	})
	must(t, "apply", "-f", fixture)

	// No ready replica: 006 with the waiting reason, and no 021 even once the deadline passed.
	waitRollout(t, "ProgressDeadlineExceeded")
	h := waitHealth(t, 2*time.Minute, "006 on infra/stuck", func(h api.HealthResponse) bool {
		f := finding(h, health.CodeProxyNotReady, "Gateway", id)
		return f != nil && pullFailure(f.Detail)
	})
	if f := finding(h, health.CodeProxyNotReady, "Gateway", id); f.Severity != health.SeverityError {
		t.Errorf("006 on %s: severity %s, want error", id, f.Severity)
	}
	if f := finding(h, health.CodeRolloutStuck, "Gateway", id); f != nil {
		t.Errorf("021 on %s with no ready replica: %+v", id, f.Finding)
	}

	must(t, "-n", "infra", "patch", "gatewayparameters", "stuck-proxy", "--type=merge", "-p", `{"spec":{"kube":{"envoyContainer":null}}}`)
	waitDeployReady(t, "infra", "stuck")
	waitHealth(t, 2*time.Minute, "006 on infra/stuck cleared", func(h api.HealthResponse) bool {
		return finding(h, health.CodeProxyNotReady, "Gateway", id) == nil
	})

	// The old Pod stays ready while the new one cannot pull: 021 with the waiting reason as evidence.
	must(t, "-n", "infra", "patch", "gatewayparameters", "stuck-proxy", "--type=merge", "-p",
		`{"spec":{"kube":{"envoyContainer":{"image":{"tag":"does-not-exist"}}}}}`)
	h = waitHealth(t, 3*time.Minute, "021 on infra/stuck", func(h api.HealthResponse) bool {
		return finding(h, health.CodeRolloutStuck, "Gateway", id) != nil
	})
	f := finding(h, health.CodeRolloutStuck, "Gateway", id)
	ev, _ := f.Facts.(*health.ProcessEvidence)
	if f.Severity != health.SeverityWarning || !strings.Contains(f.Detail, "ProgressDeadlineExceeded") || ev == nil || !pullFailure(ev.Waiting) {
		t.Errorf("021 on %s: severity %s, detail %q, evidence %+v", id, f.Severity, f.Detail, ev)
	}
	if f := finding(h, health.CodeProxyNotReady, "Gateway", id); f != nil {
		t.Errorf("006 on %s beside a ready replica: %+v", id, f.Finding)
	}
}

// waitRollout waits until the UI reports infra/stuck's rollout with reason.
func waitRollout(t *testing.T, reason string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		var p api.ProcessesResponse
		get(t, install+"/processes?q=stuck", &p)
		for _, g := range p.Proxies.Items {
			if g.Gateway.String() == "infra/stuck" && g.Set.Rollout != nil && g.Set.Rollout.Reason == reason {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("rollout of infra/stuck never reported %s: %+v", reason, p.Proxies.Items)
		}
		time.Sleep(time.Second)
	}
}

// waitDeployReady waits until ns/name has rolled out every replica. kubectl rollout status
// fails once the short progress deadline passes, so this reads the status itself.
func waitDeployReady(t *testing.T, ns, name string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for {
		var d struct {
			Metadata struct{ Generation int64 } `json:"metadata"`
			Spec     struct{ Replicas int32 }   `json:"spec"`
			Status   struct {
				ObservedGeneration                                  int64
				Replicas, UpdatedReplicas, ReadyReplicas, Available int32
			} `json:"status"`
		}
		kubectl(t, &d, "-n", ns, "get", "deploy/"+name, "-o", "json")
		st := d.Status
		if st.ObservedGeneration == d.Metadata.Generation && st.Replicas == d.Spec.Replicas &&
			st.UpdatedReplicas == d.Spec.Replicas && st.ReadyReplicas == d.Spec.Replicas {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("deploy %s/%s never rolled out: %+v", ns, name, d)
		}
		time.Sleep(time.Second)
	}
}

// pullFailure reports whether s names an image pull failure.
func pullFailure(s string) bool {
	return strings.Contains(s, "ImagePullBackOff") || strings.Contains(s, "ErrImagePull")
}

// testRestartHold deletes the controller Pod (R37), then removes nack's rejecting objects once the
// replacement is ready: a reject gone within 60 s of a controller Pod change is held, so infra/nack reads
// unknown, never clear, and the install never reads healthy. Re-applied during the hold, the reject keeps its since.
func testRestartHold(t *testing.T) {
	const fixture = "fixtures/nack.yaml"
	t.Cleanup(func() {
		must(t, "apply", "-f", fixture)
		controllerSettled(t)
	})
	before := finding(healthNow(t), health.CodeXDSRejected, "Gateway", nackID)
	if before == nil || before.Since == nil {
		t.Fatalf("no 001 with since on %s before the delete", nackID)
	}
	old := podNames(t, "kgw-a", ctrlSelector)
	must(t, "-n", "kgw-a", "delete", "pod", old[0], "--wait=false")
	must(t, "-n", "kgw-a", "rollout", "status", "deploy/kgateway", "--timeout=180s")
	must(t, "-n", "shop", "delete", "httproute/nack-regex", "backendconfigpolicy/nack-cipher")

	// sample fails the test when infra/nack reads clear or the install healthy; it returns the 001 and the check.
	sample := func() (*api.FindingDTO, *health.CheckResult) {
		h := healthNow(t)
		c := checkResult(h, "xDS rejects")
		f := finding(h, health.CodeXDSRejected, "Gateway", nackID)
		if c == nil || f == nil && c.OK {
			t.Fatalf("the reject on %s read clear during the restart hold: check %+v, findings %s", nackID, c, codes(h))
		}
		if h.State == health.StateHealthy {
			t.Fatalf("install healthy during the restart hold: checks %+v", h.Checks)
		}
		var ov api.OverviewResponse
		get(t, install+"/overview", &ov)
		for _, g := range ov.Gateways {
			if g.Ref.String() == nackID && g.DataPlane == api.DataPlaneOK {
				t.Fatalf("overview: %s data plane ok during the restart hold", nackID)
			}
		}
		return f, c
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		f, c := sample()
		if f == nil && c.Pending && c.Error != "" {
			t.Logf("held: %s", c.Error)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the reject on %s was never held after the controller Pod was replaced", nackID)
		}
		time.Sleep(time.Second)
	}
	var d api.GatewayDetail
	get(t, install+"/gateways/infra/nack", &d)
	if d.DataPlane == nil || d.DataPlane.Rejecting || !d.DataPlane.Unavailable {
		t.Errorf("Gateway detail during the hold: data plane %+v, want unavailable", d.DataPlane)
	}

	must(t, "apply", "-f", fixture)
	deadline = time.Now().Add(2 * time.Minute)
	for {
		f, c := sample()
		if f != nil && c.OK {
			if !f.Since.Equal(*before.Since) {
				t.Errorf("001 on %s: since %s after the hold, want %s kept", nackID, f.Since, before.Since)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the reject on %s never returned during the hold", nackID)
		}
		time.Sleep(time.Second)
	}
}

// testContainerRestart kills the controller process inside its container (R37): the Pod stays, its
// container restarts, and KGWUI-019 names that Pod on the controller Deployment.
func testContainerRestart(t *testing.T) {
	t.Cleanup(func() { controllerSettled(t) })
	pods := podNames(t, "kgw-a", ctrlSelector)
	if len(pods) != 1 {
		t.Fatalf("controller Pods %v, want 1", pods)
	}
	pod := pods[0]
	// The proxy image is already on the node and has a shell; the controller image has none.
	out, err := kubectlRun(t, "-n", "infra", "get", "deploy/nack", "-o", "jsonpath={.spec.template.spec.containers[0].image}")
	if err != nil {
		t.Fatal(err)
	}
	must(t, "-n", "kgw-a", "debug", "pod/"+pod, "--image="+string(out), "--image-pull-policy=IfNotPresent",
		"--target=controller", "--profile=general", "--", "sh", "-c", "kill 1")

	h := waitHealth(t, 3*time.Minute, "019 on the controller Deployment", func(h api.HealthResponse) bool {
		return finding(h, health.CodeProcessRestarting, "Deployment", "kgw-a/kgateway") != nil
	})
	f := finding(h, health.CodeProcessRestarting, "Deployment", "kgw-a/kgateway")
	ev, _ := f.Facts.(*health.ProcessEvidence)
	if f.Severity != health.SeverityWarning || ev == nil || ev.Pod != pod || ev.Termination == nil {
		t.Fatalf("019: severity %s, evidence %+v; want a warning on Pod %s with its termination", f.Severity, ev, pod)
	}
	t.Logf("019: %s; %s", f.Message, f.Detail)

	var p api.ProcessesResponse
	get(t, install+"/processes", &p)
	i := slices.IndexFunc(p.Controller.Processes, func(x api.ProcessDTO) bool { return x.Name == pod })
	if i < 0 || deref(p.Controller.Processes[i].Restarts, 0) < 1 {
		t.Errorf("processes: controller %+v; want Pod %s with a restart", p.Controller.Processes, pod)
	}
}

// testControllerDown scales the controller to zero: the proxies lose their xDS stream, and after
// 60 s on Ready proxies KGWUI-026 names them. Rejects cannot be read meanwhile, so nack's data plane is unknown.
func testControllerDown(t *testing.T) {
	t.Cleanup(func() {
		must(t, "-n", "kgw-a", "scale", "deploy/kgateway", "--replicas=1")
		controllerSettled(t)
		waitHealth(t, 2*time.Minute, "026 cleared", func(h api.HealthResponse) bool {
			return !slices.ContainsFunc(h.Findings, func(f api.FindingDTO) bool { return f.Code == health.CodeProxyDisconnected })
		})
	})
	start := healthNow(t).CollectedAt
	must(t, "-n", "kgw-a", "scale", "deploy/kgateway", "--replicas=0")
	h := waitHealth(t, 4*time.Minute, "026 on infra/nack", func(h api.HealthResponse) bool {
		return finding(h, health.CodeProxyDisconnected, "Gateway", nackID) != nil
	})
	// collectedAt and start are both the UI's clock; the proxies disconnected after start.
	if d := h.CollectedAt.Sub(start); d < time.Minute {
		t.Errorf("026 on %s %s after the scale-down, want at least 60 s", nackID, d)
	}
	f := finding(h, health.CodeProxyDisconnected, "Gateway", nackID)
	ev, _ := f.Facts.(*health.ProxyStatsEvidence)
	proxies := podNames(t, "infra", nackSelector)
	if f.Severity != health.SeverityWarning || ev == nil || len(ev.Pods) == 0 {
		t.Fatalf("026 on %s: severity %s, evidence %+v", nackID, f.Severity, ev)
	}
	for _, p := range ev.Pods {
		if !slices.Contains(proxies, p.Pod) || p.Connected == nil || *p.Connected {
			t.Errorf("026 evidence %+v; want disconnected Pods of %v", p, proxies)
		}
	}
	t.Logf("026: %s; %s", f.Message, f.Detail)
	if c := checkResult(h, "xDS rejects"); c == nil || c.OK {
		t.Errorf("xDS rejects check %+v with no controller, want not OK", c)
	}
}

// region Analyzer (fixtures/analyzer.yaml)

const (
	noGrantID   = "shop/no-grant"
	wrongPortID = "shop/wrong-port"
	tpGatewayID = "shop/tp-missing-gateway"
	edgeID      = "infra/edge"
)

// findingDetail is the part of GET …/findings/{id} TestAnalyzer reads.
type findingDetail struct {
	Finding      api.FindingRow    `json:"finding"`
	SuggestedFix *api.SuggestedFix `json:"suggestedFix"`
}

// detailOf is the finding detail of f.
func detailOf(t *testing.T, f *api.FindingDTO) findingDetail {
	t.Helper()
	var d findingDetail
	get(t, install+"/findings/"+url.PathEscape(f.ID), &d)
	return d
}

// waitFinding waits until /health has a finding with code on kind id, and returns it with its response.
func waitFinding(t *testing.T, limit time.Duration, code, kind, id string) (api.HealthResponse, *api.FindingDTO) {
	t.Helper()
	h := waitHealth(t, limit, code+" on "+kind+" "+id, func(h api.HealthResponse) bool { return finding(h, code, kind, id) != nil })
	return h, finding(h, code, kind, id)
}

// parentCond is the condition typ that the controller wrote on route id for Gateway infra/edge.
func parentCond(t *testing.T, id, typ string) *kcond {
	t.Helper()
	var r kobj
	ns, name, _ := strings.Cut(id, "/")
	kubectl(t, &r, "-n", ns, "get", "httproutes.gateway.networking.k8s.io/"+name, "-o", "json")
	e := ourEntry(r, objects.ParentRef{Group: gwGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"})
	return findCond(e.condsOrNil(), typ)
}

// refsOf is f's refs evidence; it fails the test when f has none.
func refsOf(t *testing.T, f *api.FindingDTO) *health.RefsEvidence {
	t.Helper()
	ev, ok := f.Facts.(*health.RefsEvidence)
	if !ok {
		t.Fatalf("%s on %s: evidence %#v, want refs", f.Code, f.Resource, f.Facts)
	}
	return ev
}

// TestAnalyzer checks the 3.3 findings on fixtures/analyzer.yaml (design §7): 013 quoting the
// controller, 014 with its ReferenceGrant (created as the UI says, the finding then clears), the
// computed 016 after grace and hold, 017 with the kept listeners, and the groups they form.
func TestAnalyzer(t *testing.T) {
	t.Run("ref unresolved", testRefUnresolved)
	t.Run("ref not permitted", testRefNotPermitted)
	t.Run("policy target not found", testPolicyTargetNotFound)
	t.Run("listener conflicted", testListenerConflicted)
}

// testRefUnresolved: a backendRef to a missing Service port is 013 with explanation 3 (the Service's ports)
// before the controller's reason, attributed to its one ref, with the one-port fix; it joins that Service's Missing backend group.
func testRefUnresolved(t *testing.T) {
	h, f := waitFinding(t, 3*time.Minute, health.CodeRefUnresolved, "HTTPRoute", wrongPortID)
	c := parentCond(t, wrongPortID, "ResolvedRefs")
	if c == nil || c.Status != "False" {
		t.Fatalf("%s: controller wrote ResolvedRefs %+v, want False", wrongPortID, c)
	}
	t.Logf("controller: ResolvedRefs=False, reason %s: %s", c.Reason, c.Message)
	want := fmt.Sprintf("Service `shop/httpbin` has no port 9999 (ports: 8000). ResolvedRefs=False, reason %s: %s", c.Reason, c.Message)
	if f.Severity != health.SeverityError || !strings.HasPrefix(f.Detail, want) {
		t.Errorf("013 on %s: severity %s, detail %q; want error starting %q", wrongPortID, f.Severity, f.Detail, want)
	}
	if has(h, health.CodeConditionFalse, wrongPortID) {
		t.Errorf("013 and 002 both on %s (D-030)", wrongPortID)
	}
	ev := refsOf(t, f)
	httpbin := objects.Ref{Kind: "Service", Namespace: "shop", Name: "httpbin"}
	loc := []health.RefLocation{{Path: "spec.rules[0].backendRefs[0]", Ref: httpbin}}
	if ev.Explanation != health.RefExplainServicePort || !ev.Attributed || !slices.Equal(ev.Candidates, loc) || !slices.Equal(ev.Ports, []int32{8000}) {
		t.Errorf("013 on %s: evidence %+v; want explanation 3 with ports [8000], attributed to %+v", wrongPortID, ev, loc)
	}
	const wantYAML = "group: \"\"\nkind: Service\nname: httpbin\nnamespace: shop\nport: 8000\n"
	fx := detailOf(t, f).SuggestedFix
	if fx == nil || fx.Title != "Set the port to `8000`" || fx.Target.Ref != routeRef(wrongPortID) || fx.Create ||
		fx.Path != loc[0].Path || fx.YAML != wantYAML || fx.Basis != fmt.Sprintf(diagnose.FixPortBasis, "httpbin") {
		t.Errorf("013 on %s: fix %+v; want port 8000 at %s with YAML\n%s", wrongPortID, fx, loc[0].Path, wantYAML)
	}
	g := groupOf(t, f.Group)
	if g.Code != health.CodeRefUnresolved || g.Cause == nil || g.Cause.Ref != httpbin || !affects(g, routeRef(wrongPortID)) {
		t.Errorf("013 on %s: group %+v; want the Missing backend group of Service shop/httpbin", wrongPortID, g)
	}

	// base.yaml's shop/broken names a Service that does not exist: explanation 2, and no fix.
	_, b := waitFinding(t, 3*time.Minute, health.CodeRefUnresolved, "HTTPRoute", "shop/broken")
	if ev := refsOf(t, b); ev.Explanation != health.RefExplainServiceMissing || !strings.HasPrefix(b.Detail, "Service `shop/missing` does not exist. ") {
		t.Errorf("013 on shop/broken: explanation %q, detail %q; want explanation 2", ev.Explanation, b.Detail)
	}
	if fx := detailOf(t, b).SuggestedFix; fx != nil {
		t.Errorf("013 on shop/broken: fix %+v; want none", fx)
	}
}

// testRefNotPermitted: a backendRef to another namespace with no grant is 014 with the
// ReferenceGrant to create. Created with kubectl create -f, as the UI says, it clears the 014.
func testRefNotPermitted(t *testing.T) {
	_, f := waitFinding(t, 3*time.Minute, health.CodeRefNotPermitted, "HTTPRoute", noGrantID)
	if c := parentCond(t, noGrantID, "ResolvedRefs"); c == nil || c.Status != "False" || c.Reason != "RefNotPermitted" {
		t.Fatalf("%s: controller wrote ResolvedRefs %+v, want False/RefNotPermitted", noGrantID, c)
	}
	web := objects.Ref{Kind: "Service", Namespace: "store", Name: "web"}
	ev := refsOf(t, f)
	if want := []health.RefLocation{{Path: "spec.rules[0].backendRefs[0]", Ref: web}}; f.Severity != health.SeverityError ||
		ev.Explanation != health.RefExplainController || !ev.Attributed || !slices.Equal(ev.Candidates, want) {
		t.Errorf("014 on %s: severity %s, evidence %+v; want an error attributed to %+v", noGrantID, f.Severity, ev, want)
	}

	const grant = "allow-shop-httproute-service-web"
	const wantYAML = `apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
metadata:
  name: ` + grant + `
  namespace: store
spec:
  from:
  - group: gateway.networking.k8s.io
    kind: HTTPRoute
    namespace: shop
  to:
  - group: ""
    kind: Service
    name: web
`
	d := detailOf(t, f)
	fx := d.SuggestedFix
	if fx == nil {
		t.Fatalf("014 on %s: no suggested fix", noGrantID)
	}
	target := objects.Ref{Group: gwGroup, Kind: "ReferenceGrant", Namespace: "store", Name: grant}
	if fx.Title != "Create a ReferenceGrant in `store`" || fx.Target.Ref != target || !fx.Create || fx.Path != "" ||
		fx.YAML != wantYAML || fx.Basis != diagnose.FixGrantBasis {
		t.Errorf("014 fix %+v; want the grant %s with YAML\n%s", fx, target, wantYAML)
	}

	g := groupOf(t, f.Group)
	shape := &api.GrantShape{FromNamespace: "shop", ToNamespace: "store", ToGroup: "", ToKind: "Service"}
	if g.Code != health.CodeRefNotPermitted || g.Cause != nil || !reflect.DeepEqual(g.Grant, shape) || !affects(g, routeRef(noGrantID)) {
		t.Errorf("014 on %s: group %+v; want the Not permitted group %+v", noGrantID, g, shape)
	}

	// The UI's create command, on the file the UI downloads.
	file := filepath.Join(t.TempDir(), "referencegrant-"+grant+".yaml")
	if err := os.WriteFile(file, []byte(fx.YAML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		must(t, "-n", "store", "delete", "referencegrant/"+grant, "--ignore-not-found")
		waitFinding(t, 3*time.Minute, health.CodeRefNotPermitted, "HTTPRoute", noGrantID)
	})
	must(t, "create", "-f", file)
	waitHealth(t, 3*time.Minute, "014 on "+noGrantID+" cleared by the grant", func(h api.HealthResponse) bool {
		return !slices.ContainsFunc(h.Findings, func(f api.FindingDTO) bool { return f.Resource != nil && f.Resource.String() == noGrantID })
	})
	if c := parentCond(t, noGrantID, "ResolvedRefs"); c == nil || c.Status != "True" {
		t.Errorf("%s with the grant: controller wrote ResolvedRefs %+v, want True", noGrantID, c)
	}
}

// testPolicyTargetNotFound re-creates a TrafficPolicy whose only target is a missing Gateway. On 2.3 and 2.4
// the UI computes 016 once the target stayed missing for the grace and the hold, and it replaces the 003.
// kgateway 2.5 writes TargetNotFound itself, so 016 is an error from status and nothing is computed.
func testPolicyTargetNotFound(t *testing.T) {
	const fixture = "fixtures/analyzer.yaml"
	minor := kgwMinor(t)
	// Applied again on any exit, so a failure leaves the fixture whole for the next tests.
	t.Cleanup(func() { must(t, "apply", "-f", fixture) })
	must(t, "-n", "shop", "delete", "trafficpolicy/tp-missing-gateway", "--wait=true")
	waitHealth(t, 2*time.Minute, "no finding on "+tpGatewayID+" after the delete", func(h api.HealthResponse) bool {
		return !slices.ContainsFunc(h.Findings, func(f api.FindingDTO) bool { return f.Resource != nil && f.Resource.String() == tpGatewayID })
	})
	applied := time.Now()
	must(t, "apply", "-f", fixture)
	var h api.HealthResponse
	if fromStatus(minor) {
		h = waitHealth(t, 3*time.Minute, "016 from status on "+tpGatewayID, func(h api.HealthResponse) bool {
			return finding(h, health.CodePolicyTargetNotFound, "TrafficPolicy", tpGatewayID) != nil
		})
		c := targetNotFound(t, tpGatewayID)
		if c == nil {
			t.Fatalf("%s: kgateway %s wrote no Accepted=False/TargetNotFound", tpGatewayID, minor)
		}
		// One 016 from status: a computed one beside it would be a duplicate (D-020 rule 5).
		if n := len(slices.DeleteFunc(slices.Clone(h.Findings), func(f api.FindingDTO) bool {
			return f.Code != health.CodePolicyTargetNotFound || f.Resource == nil || f.Resource.String() != tpGatewayID
		})); n != 1 {
			t.Errorf("%d 016 findings on %s, want 1: %s", n, tpGatewayID, codes(h))
		}
		f := finding(h, health.CodePolicyTargetNotFound, "TrafficPolicy", tpGatewayID)
		want := fmt.Sprintf("Accepted=False, reason TargetNotFound: %s", c.Message)
		if f.Severity != health.SeverityError || f.Message != health.PolicyTargetNotFoundMessage || !strings.HasPrefix(f.Detail, want) ||
			strings.Contains(f.Detail, "Computed by the UI") {
			t.Errorf("016 on %s: severity %s, message %q, detail %q; want an error from status starting %q", tpGatewayID, f.Severity, f.Message, f.Detail, want)
		}
	} else {
		// The UI first sees the policy after the apply; the creation time is cut to the second.
		due := applied.Add(health.Grace + health.TargetHold - time.Second)
		h = waitHealth(t, 4*time.Minute, "016 on "+tpGatewayID, func(h api.HealthResponse) bool {
			f := finding(h, health.CodePolicyTargetNotFound, "TrafficPolicy", tpGatewayID)
			if f != nil && time.Now().Before(due) {
				t.Fatalf("016 on %s %s after the apply, before the grace and the hold", tpGatewayID, time.Since(applied))
			}
			return f != nil
		})
		f := finding(h, health.CodePolicyTargetNotFound, "TrafficPolicy", tpGatewayID)
		want := fmt.Sprintf("targetRefs[0] Gateway `nope`: not found in namespace shop. Computed by the UI's policy targets rule. Rule set kgateway-%s.", minor)
		if f.Severity != health.SeverityWarning || f.Message != health.PolicyTargetNotFoundMessage || f.Detail != want {
			t.Errorf("016 on %s: severity %s, message %q, detail %q; want a warning with %q", tpGatewayID, f.Severity, f.Message, f.Detail, want)
		}
	}
	t.Logf("016 on %s %s after the apply", tpGatewayID, time.Since(applied).Round(time.Second))
	for _, code := range []string{health.CodeRouteUnattached, health.CodeComputedDisagrees, health.CodeConditionFalse} {
		if finding(h, code, "TrafficPolicy", tpGatewayID) != nil {
			t.Errorf("%s beside 016 on %s", code, tpGatewayID)
		}
	}
}

// fromStatus reports whether kgateway minor writes TargetNotFound, so the UI reads 016 from status (D-020 rule 5).
func fromStatus(minor string) bool { return minor != "2.3" && minor != "2.4" }

// targetNotFound is the Accepted=False/TargetNotFound condition kgateway wrote on TrafficPolicy id, or nil.
func targetNotFound(t *testing.T, id string) *kcond {
	t.Helper()
	var p struct {
		Status struct {
			Ancestors []struct {
				ControllerName string  `json:"controllerName"`
				Conditions     []kcond `json:"conditions"`
			} `json:"ancestors"`
		} `json:"status"`
	}
	ns, name, _ := strings.Cut(id, "/")
	kubectl(t, &p, "-n", ns, "get", "trafficpolicies.gateway.kgateway.dev/"+name, "-o", "json")
	for _, a := range p.Status.Ancestors {
		if c := findCond(a.Conditions, "Accepted"); a.ControllerName == ours && c != nil && c.Status == "False" && c.Reason == "TargetNotFound" {
			return c
		}
	}
	return nil
}

// testListenerConflicted: kgateway marks edge's ListenerSet listeners conflicted, and 017 names only the
// listeners the controller kept: the same hostname for HostnameConflict, another protocol for ProtocolConflict.
func testListenerConflicted(t *testing.T) {
	edgeHTTP := health.ConflictingListener{Owner: objects.Ref{Group: gwGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}, Name: "http", Protocol: "HTTP"}
	edgeHTTPS := health.ConflictingListener{Owner: edgeHTTP.Owner, Name: "https", Protocol: "HTTPS", Hostname: "*.example.com"}
	cases := []struct {
		set, listener, reason, rule string
		port                        int32
		others                      []health.ConflictingListener
		end                         string
	}{
		{"infra/extra", "clash", "HostnameConflict", health.ListenerHostnameRule, 8080, []health.ConflictingListener{edgeHTTP},
			"Same port and hostname as listener `http` of Gateway infra/edge. Computed by the UI's listener port and hostname rule."},
		{"infra/analyzer", "proto", "ProtocolConflict", health.ListenerProtocolRule, 8443, []health.ConflictingListener{edgeHTTPS},
			"On port 8443 the controller kept HTTPS: listener `https` of Gateway infra/edge. Computed by the UI's listener port and protocol rule."},
	}
	for _, c := range cases {
		var s kobj
		ns, name, _ := strings.Cut(c.set, "/")
		kubectl(t, &s, "-n", ns, "get", "listenersets.gateway.networking.k8s.io/"+name, "-o", "json")
		var cond *kcond
		for _, l := range s.Status.Listeners {
			if l.Name == c.listener {
				cond = findCond(l.Conditions, "Conflicted")
			}
		}
		if cond == nil || cond.Status != "True" || cond.Reason != c.reason {
			t.Errorf("ListenerSet %s listener %s: controller wrote Conflicted %+v, want True/%s", c.set, c.listener, cond, c.reason)
			continue
		}
		h := waitHealth(t, 2*time.Minute, "017 on "+c.set, func(h api.HealthResponse) bool { return conflictOn(h, c.set, c.listener) != nil })
		f := conflictOn(h, c.set, c.listener)
		ev := f.Facts.(*health.ListenerConflictEvidence)
		if f.Severity != health.SeverityError || ev.Port != c.port || ev.Reason != c.reason || ev.Rule != c.rule ||
			!slices.Equal(ev.Others, c.others) || ev.OthersTotal != len(c.others) || !strings.HasSuffix(f.Detail, c.end) {
			t.Errorf("017 on %s listener %s: severity %s, evidence %+v, detail %q; want others %+v and %q",
				c.set, c.listener, f.Severity, ev, f.Detail, c.others, c.end)
		}
	}
}

// conflictOn is the 017 on listener l of ListenerSet id in h, or nil.
func conflictOn(h api.HealthResponse, id, l string) *api.FindingDTO {
	for i, f := range h.Findings {
		ev, ok := f.Facts.(*health.ListenerConflictEvidence)
		if f.Code == health.CodeListenerConflicted && f.Resource != nil && f.Resource.Kind == "ListenerSet" && f.Resource.String() == id && ok && ev.Listener == l {
			return &h.Findings[i]
		}
	}
	return nil
}

// groupOf is the row of group id in the grouped findings view.
func groupOf(t *testing.T, id string) api.GroupRow {
	t.Helper()
	var p api.Page[api.GroupRow]
	get(t, install+"/findings?view=groups&limit=200", &p)
	i := slices.IndexFunc(p.Items, func(g api.GroupRow) bool { return g.ID == id })
	if i < 0 {
		t.Fatalf("group %s not in the grouped view: %+v", id, p.Items)
	}
	return p.Items[i]
}

// affects reports whether g lists r as affected.
func affects(g api.GroupRow, r objects.Ref) bool {
	return slices.ContainsFunc(g.Affected, func(a api.LinkedRef) bool { return a.Ref == r })
}

// region Backends (fixtures/backends.yaml)

const idleID = "shop/idle"

// idle023 is the 023 on Service shop/idle port 8000 in h, or nil.
func idle023(h api.HealthResponse) *api.FindingDTO {
	f := finding(h, health.CodeBackendNoReadyEndpoints, "Service", idleID)
	if ev, _ := factsOf(f).(*health.EndpointsEvidence); ev == nil || ev.Port != 8000 {
		return nil
	}
	return f
}

// scaleIdle scales shop/idle to n replicas and waits until they all roll out.
func scaleIdle(t *testing.T, n int) {
	t.Helper()
	must(t, "-n", "shop", "scale", "deploy/idle", fmt.Sprintf("--replicas=%d", n))
	waitDeployReady(t, "shop", "idle")
}

// TestBackendReadiness checks KGWUI-023 on fixtures/backends.yaml (design §11): a resolved route to a
// Service with no ready endpoint is a warning after the hold; a ready endpoint clears it, and the hold restarts.
func TestBackendReadiness(t *testing.T) {
	var meta api.MetaResponse
	get(t, "/api/v1/meta", &meta)
	if !slices.Contains(meta.CapabilityStates, api.Capability{Name: "endpoints", State: api.CapOn}) {
		t.Fatalf("meta: endpoints %+v, want on", meta.CapabilityStates)
	}
	t.Cleanup(func() { scaleIdle(t, 0) })

	h := waitHealth(t, 3*time.Minute, "023 on "+idleID, func(h api.HealthResponse) bool { return idle023(h) != nil })
	if c := parentCond(t, idleID, "ResolvedRefs"); c == nil || c.Status != "True" {
		t.Fatalf("%s: controller wrote ResolvedRefs %+v, want True", idleID, c)
	}
	f := idle023(h)
	ev := f.Facts.(*health.EndpointsEvidence)
	if f.Severity != health.SeverityWarning || f.Message != "Requests to this backend get 503" ||
		f.Detail != "Service `shop/idle` has no ready endpoints for port 8000." || ev.Service != "idle" || ev.Ready != 0 || ev.Total != 0 {
		t.Errorf("023 on %s: severity %s, message %q, detail %q, evidence %+v", idleID, f.Severity, f.Message, f.Detail, ev)
	}
	if c := checkResult(h, "backend endpoints"); c == nil || !c.OK || c.Unavailable != "" {
		t.Errorf("backend endpoints check %+v, want OK", c)
	}
	if g := groupOf(t, f.Group); g.Code != health.CodeBackendNoReadyEndpoints || !affects(g, routeRef(idleID)) {
		t.Errorf("023 on %s: group %+v; want the Empty backend group with route %s", idleID, g, idleID)
	}

	// One ready endpoint clears it within a recompute (Review Focus 3).
	scaleIdle(t, 1)
	waitHealth(t, 2*time.Minute, "023 on "+idleID+" cleared", func(h api.HealthResponse) bool { return idle023(h) == nil })

	// Back to zero: the hold restarts, so 023 returns no sooner than EndpointsHold after the scale-down.
	scaled := time.Now()
	scaleIdle(t, 0)
	due := scaled.Add(health.EndpointsHold - time.Second)
	waitHealth(t, 2*time.Minute, "023 on "+idleID+" after the scale-down", func(h api.HealthResponse) bool {
		f := idle023(h)
		if f != nil && time.Now().Before(due) {
			t.Fatalf("023 on %s %s after the scale-down, before the hold", idleID, time.Since(scaled))
		}
		return f != nil
	})
	t.Logf("023 on %s %s after the scale-down", idleID, time.Since(scaled).Round(time.Second))
}

// region kgateway version (Task 86)

// kgwTag is install A's controller image tag, such as "v2.5.0".
func kgwTag(t *testing.T) string {
	t.Helper()
	out, err := kubectlRun(t, "-n", "kgw-a", "get", "deploy/kgateway", "-o", "jsonpath={.spec.template.spec.containers[0].image}")
	if err != nil {
		t.Fatal(err)
	}
	img := string(out)
	return img[strings.LastIndex(img, ":")+1:]
}

// TestKgatewayVersion checks the version-dependent parts on each CI row: the install's version and
// rule set, and KGWUI-024, which reads 2.5's per-client metrics and is not on version before it.
func TestKgatewayVersion(t *testing.T) {
	minor, tag := kgwMinor(t), kgwTag(t)
	t.Logf("kgateway %s (minor %s)", tag, minor)
	var insts []api.InstallSummary
	get(t, "/api/v1/installs", &insts)
	if len(insts) == 0 || strings.TrimPrefix(insts[0].Version, "v") != strings.TrimPrefix(tag, "v") {
		t.Errorf("install version %+v, want %s", insts, tag)
	}
	var gw api.GatewayDetail
	get(t, install+"/gateways/infra/edge", &gw)
	if want := "kgateway-" + minor; gw.RuleSet != want {
		t.Errorf("Gateway infra/edge rule set %q, want %q", gw.RuleSet, want)
	}

	// The check settles once controller metrics are scraped.
	h := waitHealth(t, 2*time.Minute, "withheld config check with metrics", func(h api.HealthResponse) bool {
		c := checkResult(h, "withheld config")
		return c != nil && c.Unavailable != health.AvailFailing
	})
	c := checkResult(h, "withheld config")
	if h.State == health.StateUnknown {
		t.Errorf("install unknown: checks %+v", h.Checks)
	}
	if slices.ContainsFunc(h.Findings, func(f api.FindingDTO) bool { return f.Code == health.CodeConfigWithheld }) {
		t.Errorf("024 on kind, which has no translation lag: %s", codes(h))
	}
	if fromStatus(minor) {
		// R130: available with no finding; a version note would mean build_info and the tag disagree.
		if !c.OK || c.Unavailable != "" || c.Detail != "" {
			t.Errorf("withheld config on %s: %+v, want available and OK", tag, c)
		}
	} else if c.Unavailable != health.AvailNotOnVersion {
		t.Errorf("withheld config on %s: %+v, want not on version", tag, c)
	}
}

// TestListOnlyKinds: DirectResponse, GatewayExtension and ReferenceGrant are served kinds; explorer.yaml's
// ReferenceGrant is listed and shown, and raises nothing (it never decides resolution).
func TestListOnlyKinds(t *testing.T) {
	var meta api.MetaResponse
	get(t, "/api/v1/meta", &meta)
	for _, k := range []string{"gateway.kgateway.dev/DirectResponse", "gateway.kgateway.dev/GatewayExtension", "gateway.networking.k8s.io/ReferenceGrant"} {
		if !slices.ContainsFunc(meta.Kinds, func(s kinds.Served) bool { return s.Group+"/"+s.Kind == k }) {
			t.Errorf("meta kinds lack %s", k)
		}
	}
	const grant = "shop/explorer-httpbin"
	var u map[string]any
	get(t, install+"/resources/gateway.networking.k8s.io/ReferenceGrant/"+grant, &u)
	if md, _ := u["metadata"].(map[string]any); md["name"] != "explorer-httpbin" {
		t.Errorf("ReferenceGrant %s: metadata %+v", grant, u["metadata"])
	}
	for _, f := range healthNow(t).Findings {
		if f.Resource != nil && f.Resource.Kind == "ReferenceGrant" {
			t.Errorf("%s on ReferenceGrant %s", f.Code, f.Resource)
		}
	}
}

// region Controller log signals

// envoyNACKTexts are fixed parts of the Envoy messages behind nack.yaml's rejects, which kgateway logs verbatim.
var envoyNACKTexts = []string{"RE2 program size", "Failed to initialize cipher suites"}

// controllerLogs reports whether setup.sh installed the UI with diagnostics.controllerLogs (CONTROLLER_LOGS=1).
func controllerLogs(t *testing.T) bool {
	return slices.Contains(uiArgs(t), "--diagnostics-controller-logs=true")
}

// TestControllerLogSignals reads 001 on infra/nack with the controller log on (design §8.9): each
// rejected type has its error class, a derived Envoy name only when the Gateway's proxy stats report
// it, and no API body quotes the Envoy message the controller logged.
func TestControllerLogSignals(t *testing.T) {
	if !nack() || !controllerLogs(t) {
		t.Skip("needs the NACK fixture and the controller log on (NACK=1 CONTROLLER_LOGS=1 test/e2e/setup.sh)")
	}
	var meta api.MetaResponse
	get(t, "/api/v1/meta", &meta)
	if !slices.Contains(meta.CapabilityStates, api.Capability{Name: "controllerLogs", State: api.CapOn}) {
		t.Fatalf("meta: controllerLogs %+v, want on", meta.CapabilityStates)
	}
	controllerSettled(t)

	classes := map[string]string{typeRoutes: "regex", typeCluster: "tls"}
	h := waitHealth(t, 3*time.Minute, "001 on infra/nack with a log event per rejected type", func(h api.HealthResponse) bool {
		ev, _ := factsOf(finding(h, health.CodeXDSRejected, "Gateway", nackID)).(*health.XDSRejectsEvidence)
		return ev != nil && len(ev.LogEvents) == len(classes)
	})
	ev := finding(h, health.CodeXDSRejected, "Gateway", nackID).Facts.(*health.XDSRejectsEvidence)
	if ev.ControllerLogs != health.LogsRead {
		t.Errorf("001 on %s: controllerLogs %q, want %q", nackID, ev.ControllerLogs, health.LogsRead)
	}
	names := proxyStatNames(t)
	if !slices.ContainsFunc(ev.LogEvents, func(le health.XDSLogEvent) bool { return len(le.EnvoyResources) > 0 }) {
		// A CDS reject never grows cluster.<name>.update_rejected (an EDS counter) and nack's RDS message names
		// no route config, so no name can be kept here; pkg/ctrllogs unit tests cover the keep path.
		t.Log("derived Envoy names: the keep path is not exercised on kind; only classes are checked")
	}
	for _, le := range ev.LogEvents {
		t.Logf("log event %s: classes %v, Envoy names %v", le.TypeURL, le.Classes, le.EnvoyResources)
		if want, ok := classes[le.TypeURL]; !ok || !slices.Equal(le.Classes, []string{want}) {
			t.Errorf("001 on %s, log event %s: classes %v, want [%s]", nackID, le.TypeURL, le.Classes, want)
		}
		for _, n := range le.EnvoyResources {
			if !slices.Contains(names, n) {
				t.Errorf("001 on %s, log event %s: Envoy name %q is in no proxy's stats %v", nackID, le.TypeURL, n, names)
			}
		}
		// The CDS line names nack's cluster; it is kept exactly when the proxies report it.
		const cluster = "kube_shop_httpbin-nack_8000"
		if le.TypeURL == typeCluster && slices.Contains(names, cluster) != slices.Contains(le.EnvoyResources, cluster) {
			t.Errorf("001 on %s: cluster names %v; proxy stats report %s: %v", nackID, le.EnvoyResources, cluster, slices.Contains(names, cluster))
		}
	}

	var gw api.GatewayDetail
	get(t, install+"/gateways/infra/nack", &gw)
	if gw.DataPlane == nil || gw.DataPlane.ControllerLogs != health.LogsRead {
		t.Errorf("Gateway detail data plane %+v, want the controller log read", gw.DataPlane)
	}

	// The log holds the Envoy messages, so their absence from the API means the UI kept none of their text.
	logs, err := kubectlRun(t, "-n", "kgw-a", "logs", "deploy/kgateway", "--since=1h")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range envoyNACKTexts {
		if !strings.Contains(string(logs), s) {
			t.Fatalf("controller log has no %q: the leak check would prove nothing", s)
		}
	}
	for _, p := range apiPaths(t) {
		b := body(t, p)
		for _, s := range envoyNACKTexts {
			if strings.Contains(b, s) {
				t.Errorf("GET %s quotes the Envoy message %q", p, s)
			}
		}
	}
}

// proxyStatNames are the route config and cluster names the nack proxies report, read with the UI's query.
func proxyStatNames(t *testing.T) []string {
	t.Helper()
	var out []string
	q := url.Values{"filter": {proxystats.StatsFilter}}.Encode() + "&usedonly"
	for _, pod := range podNames(t, "infra", nackSelector) {
		b, err := kubectlRun(t, "get", "--raw", fmt.Sprintf("/api/v1/namespaces/infra/pods/%s:%d/proxy/stats?%s", pod, proxystats.StatsPort, q))
		if err != nil {
			t.Fatalf("proxy stats of %s: %v", pod, err)
		}
		ps, err := proxystats.ParseEnvoy(b)
		if err != nil {
			t.Fatal(err)
		}
		out = append(append(out, slices.Collect(maps.Keys(ps.RDS))...), slices.Collect(maps.Keys(ps.Clusters))...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// apiPaths are install A's list endpoints and the detail of every object and finding they list.
func apiPaths(t *testing.T) []string {
	t.Helper()
	paths := []string{"/api/v1/meta", "/api/v1/installs"}
	for _, p := range []string{"/overview", "/health", "/diagnostics", "/processes", "/findings?limit=200", "/findings?view=groups&limit=200",
		"/gateways?limit=200", "/listenersets?limit=200", "/routes?limit=200"} {
		paths = append(paths, install+p)
	}
	var fs api.Page[api.FindingRow]
	get(t, install+"/findings?limit=200", &fs)
	for _, f := range fs.Items {
		paths = append(paths, install+"/findings/"+url.PathEscape(f.ID))
	}
	// An object links its detail view, or its YAML when its kind has none.
	add := func(l api.LinkedRef, detail string) {
		switch l.Link {
		case api.LinkDetail:
			paths = append(paths, install+detail)
		case api.LinkYAML:
			paths = append(paths, install+"/resources/"+l.Group+"/"+l.Kind+"/"+l.String())
		}
	}
	var gws api.Page[api.GatewayListRow]
	get(t, install+"/gateways?limit=200", &gws)
	for _, g := range gws.Items {
		add(g.Ref, "/gateways/"+g.Ref.String())
	}
	var sets api.Page[api.ListenerSetRow]
	get(t, install+"/listenersets?limit=200", &sets)
	for _, s := range sets.Items {
		add(s.Ref, "/listenersets/"+s.Ref.Group+"/"+s.Ref.Kind+"/"+s.Ref.String())
	}
	var trail []string
	for _, r := range routeList(t, "") {
		add(r.Ref, "/routes/"+r.Ref.Group+"/"+r.Ref.Kind+"/"+r.Ref.String())
		if r.Ref.Link == api.LinkDetail && r.Ref.Kind == "HTTPRoute" {
			paths = append(paths, install+"/routes/"+r.Ref.Group+"/"+r.Ref.Kind+"/"+r.Ref.String()+"/delegation?limit=200")
		}
	}
	// The breadcrumb labels of nack's objects, the ones the log lines are about.
	for _, r := range []objects.Ref{{Group: gwGroup, Kind: "Gateway", Namespace: "infra", Name: "nack"}, routeRef("shop/nack-regex"), routeRef("shop/nack-cluster")} {
		trail = append(trail, "ref="+refParam(r))
	}
	return append(paths, install+"/refs?"+strings.Join(trail, "&"))
}

// body is one 200 response body of path.
func body(t *testing.T, path string) string {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d, %v", path, resp.StatusCode, err)
	}
	return string(b)
}
