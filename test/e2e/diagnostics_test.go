//go:build e2e

package e2e

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
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
