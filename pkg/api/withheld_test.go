package api

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// withheldFinding is the 024 finding of severity sev on Gateway ns/name, or nil.
func withheldFinding(h HealthResponse, ns, name string, sev health.Severity) *FindingDTO {
	for i := range h.Findings {
		f := &h.Findings[i]
		if f.Code == health.CodeConfigWithheld && f.Severity == sev && f.Resource != nil &&
			f.Resource.Ref == (objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: ns, Name: name}) {
			return f
		}
	}
	return nil
}

func checkByName(h HealthResponse, name string) *health.CheckResult {
	i := slices.IndexFunc(h.Checks, func(c health.CheckResult) bool { return c.Name == name })
	if i < 0 {
		return nil
	}
	return &h.Checks[i]
}

// KGWUI-024 from the synthesised GA-format fixtures (R130) on two replicas:
// withheld proxies are an error, a later alerting release a warning, and the
// build_info version that differs from the Deployment's shows in "What was checked".
func TestConfigWithheldAtTheAPI(t *testing.T) {
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", ctrlCapture(t, "v2.5.0-withheld-synth", "kgw", "kgateway-0"), ctrlCapture(t, "v2.5.0", "kgw", "kgateway-1"))
	h := xdsHarness(t, setup{}, lm, nackWorld()...)
	r := h.at(t, testutil.Now)
	f := withheldFinding(r, "infra", "nack", health.SeverityError)
	if r.State != health.StateError || f == nil || f.Message != "Proxies get no config updates, because referenced clusters are missing" ||
		f.Detail != "The controller withholds config from 2 of this Gateway's proxies." {
		t.Fatalf("state %s, findings %+v", r.State, r.Findings)
	}
	if ev, ok := f.Facts.(*health.WithheldEvidence); !ok || ev.Withheld != 2 || strings.Join(ev.ReportedBy, ",") != "kgateway-0" {
		t.Fatalf("evidence %+v", f.Facts)
	}
	c := checkByName(r, "withheld config")
	if c == nil || !c.OK || c.Detail != "The controller reports version 2.5.0, but its Deployment says 2.4.7. The UI uses 2.5.0." {
		t.Fatalf("check %+v", c)
	}
	if body := string(wire(t, h.srv, healthPath)); !strings.Contains(body, `"kind": "withheld"`) || !strings.Contains(body, `"withheld": 2`) {
		t.Fatalf("body %s", body)
	}

	lm.set("kgw.kgateway", ctrlCapture(t, "v2.5.0-released-synth", "kgw", "kgateway-0"), ctrlCapture(t, "v2.5.0", "kgw", "kgateway-1"))
	r = h.at(t, testutil.Now.Add(time.Minute))
	w := withheldFinding(r, "infra", "nack", health.SeverityWarning)
	if withheldFinding(r, "infra", "nack", health.SeverityError) != nil || w == nil ||
		w.Message != "Some routes fail: config was released before every cluster was ready" ||
		w.Detail != "3 releases in the last 15 minutes (flip_release 1, warm_release 2)." || withheldFinding(r, "infra", "edge", health.SeverityWarning) != nil {
		t.Fatalf("findings %+v", r.Findings)
	}
	r = h.at(t, testutil.Now.Add(time.Minute+health.WithheldReleaseWindow))
	if withheldFinding(r, "infra", "nack", health.SeverityWarning) != nil {
		t.Fatalf("the warning outlived the window: %+v", r.Findings)
	}
}

// A 024 error reaches the 3.2 summaries (phase review #4): the Gateway's proxy
// set on /processes is an error, the diagnostics data plane counts it as
// withheld, and its overview row reads "withheld" while it has no 001.
func TestConfigWithheldInSummaries(t *testing.T) {
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", ctrlCapture(t, "v2.5.0-withheld-synth", "kgw", "kgateway-0"), ctrlCapture(t, "v2.5.0", "kgw", "kgateway-1"))
	h := xdsHarness(t, setup{}, lm, nackWorld()...)
	r := h.at(t, testutil.Now)
	if withheldFinding(r, "infra", "nack", health.SeverityError) == nil || xdsFinding(r, "infra", "nack") != nil {
		t.Fatalf("want a 024 error and no 001 on nack: %+v", r.Findings)
	}
	p := get[ProcessesResponse](t, h.srv, procsPath+"?health=error", 200).Proxies
	if got := gatewayNames(p); !slices.Contains(got, "nack") {
		t.Fatalf("health=error: %v, want nack", got)
	}
	if d := get[Diagnostics](t, h.srv, diagPath, 200).DataPlane; d.Withheld != 1 {
		t.Fatalf("data plane %+v, want withheld 1", d)
	}
	if got := overviewDataPlane(t, h); got["nack"] != DataPlaneWithheld {
		t.Fatalf("overview data plane %v, want nack withheld", got)
	}
	if dp := get[GatewayDetail](t, h.srv, nackDetail, 200).DataPlane; dp == nil || !dp.ConfigWithheld || dp.Rejecting {
		t.Fatalf("gateway data plane %+v, want config withheld", dp)
	}
}

// D-060 item 1: a 024 warning (config released early) is no withheld Gateway
// anywhere, and a 001 beside a 024 error wins the Data plane cell.
func TestConfigWithheldWarningAndRejectPrecedence(t *testing.T) {
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", ctrlCapture(t, "v2.5.0-withheld-synth", "kgw", "kgateway-0"), ctrlCapture(t, "v2.5.0", "kgw", "kgateway-1"))
	h := xdsHarness(t, setup{}, lm, nackWorld()...)
	h.at(t, testutil.Now)
	lm.set("kgw.kgateway", ctrlCapture(t, "v2.5.0-released-synth", "kgw", "kgateway-0"), ctrlCapture(t, "v2.5.0", "kgw", "kgateway-1"))
	r := h.at(t, testutil.Now.Add(time.Minute))
	if withheldFinding(r, "infra", "nack", health.SeverityWarning) == nil || withheldFinding(r, "infra", "nack", health.SeverityError) != nil {
		t.Fatalf("want a 024 warning only on nack: %+v", r.Findings)
	}
	if got := overviewDataPlane(t, h); got["nack"] == DataPlaneWithheld {
		t.Fatalf("warning: overview data plane %v", got)
	}
	if d := get[Diagnostics](t, h.srv, diagPath, 200).DataPlane; d.Withheld != 0 {
		t.Fatalf("warning: data plane %+v, want withheld 0", d)
	}
	if dp := get[GatewayDetail](t, h.srv, nackDetail, 200).DataPlane; dp == nil || dp.ConfigWithheld {
		t.Fatalf("warning: gateway data plane %+v", dp)
	}

	lm = newLiveMetrics()
	lm.set("kgw.kgateway", ctrlCapture(t, "v2.5.0-withheld-synth", "kgw", "kgateway-0"), ctrlCapture(t, "v2.4.7-rejects-b", "kgw", "kgateway-1"))
	h = xdsHarness(t, setup{}, lm, nackWorld()...)
	r = h.at(t, testutil.Now)
	if withheldFinding(r, "infra", "nack", health.SeverityError) == nil || xdsFinding(r, "infra", "nack") == nil {
		t.Fatalf("want 001 and a 024 error on nack: %+v", r.Findings)
	}
	if got := overviewDataPlane(t, h); got["nack"] != DataPlaneRejects {
		t.Fatalf("001 and 024: overview data plane %v, want rejects", got)
	}
	if dp := get[GatewayDetail](t, h.srv, nackDetail, 200).DataPlane; dp == nil || !dp.Rejecting || !dp.ConfigWithheld {
		t.Fatalf("001 and 024: gateway data plane %+v", dp)
	}
}

// KGWUI-024 from the real 2.5.0 pair: flip_release 0 -> 15 on infra/withheld
// between two scrapes is a warning on that Gateway only.
func TestConfigReleasedRealCaptureAtTheAPI(t *testing.T) {
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", ctrlCapture(t, "v2.5.0-released-a", "kgw", "kgateway-0"), ctrlCapture(t, "v2.5.0", "kgw", "kgateway-1"))
	h := xdsHarness(t, setup{}, lm, nackWorld(testutil.Gateway("infra", "withheld", "kgateway", nil), testutil.ProxyDeployment("infra", "withheld", 1, 1))...)
	if r := h.at(t, testutil.Now); slices.ContainsFunc(r.Findings, func(f FindingDTO) bool { return f.Code == health.CodeConfigWithheld }) {
		t.Fatalf("before: %+v", r.Findings)
	}
	lm.set("kgw.kgateway", ctrlCapture(t, "v2.5.0-released-b", "kgw", "kgateway-0"), ctrlCapture(t, "v2.5.0", "kgw", "kgateway-1"))
	r := h.at(t, testutil.Now.Add(time.Minute))
	w := withheldFinding(r, "infra", "withheld", health.SeverityWarning)
	if w == nil || withheldFinding(r, "infra", "withheld", health.SeverityError) != nil || withheldFinding(r, "infra", "edge", health.SeverityWarning) != nil ||
		w.Detail != "15 releases in the last 15 minutes (flip_release 15)." {
		t.Fatalf("after: %+v", r.Findings)
	}
}

// On 2.3 and 2.4 the check is not available on this version, raises no
// KGWUI-008 and leaves the install healthy.
func TestConfigWithheldNotOnVersionAtTheAPI(t *testing.T) {
	for _, v := range []string{"v2.3.11", "v2.4.7"} {
		lm := newLiveMetrics()
		lm.set("kgw.kgateway", withoutRejects(ctrlCapture(t, v+"-rejects-a", "kgw", "kgateway-0")), withoutRejects(ctrlCapture(t, v+"-rejects-b", "kgw", "kgateway-1")))
		h := xdsHarness(t, setup{}, lm, nackWorld()...)
		r := h.at(t, testutil.Now)
		c := checkByName(r, "withheld config")
		if c == nil || c.OK || c.Required || c.Unavailable != health.AvailNotOnVersion || c.Error != "" {
			t.Errorf("%s: check %+v", v, c)
		}
		for _, f := range r.Findings {
			if f.Code == health.CodeConfigWithheld || f.Code == health.CodeSignalUnavailable && strings.Contains(f.Message, "withheld") {
				t.Errorf("%s: finding %+v", v, f)
			}
		}
	}
}

// A caller who may not view the controller Pods gets no 024 evidence.
func TestConfigWithheldEvidenceRestricted(t *testing.T) {
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", ctrlCapture(t, "v2.5.0-withheld-synth", "kgw", "kgateway-0"))
	noPods := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Kind != "Pod" })
	h := xdsHarness(t, setup{authz: noPods}, lm, nackWorld()...)
	if f := withheldFinding(h.at(t, testutil.Now), "infra", "nack", health.SeverityError); f == nil || !f.EvidenceRestricted || f.Facts != nil {
		t.Fatalf("hidden controller Pods: %+v", f)
	}
}

// With build_info v2.5.0 beside a Deployment on 2.4.7, the install header, 022's
// controller version and the rule set all name 2.5 (R134).
func TestOneEffectiveVersionAtTheAPI(t *testing.T) {
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", withoutRejects(ctrlCapture(t, "v2.5.0", "kgw", "kgateway-0")))
	h := xdsHarness(t, setup{proxyPods: true}, lm, skewWorld("v2.4.7", testutil.Old.Time)...)
	r := h.at(t, testutil.Now)
	f := findingOf(r, health.CodeVersionSkew)
	if f == nil || skewFacts(t, f).Controller != "2.5.0" {
		t.Fatalf("022 %+v", f)
	}
	insts := get[[]InstallSummary](t, h.srv, "/api/v1/installs", 200)
	if rules := h.e.Current().Model.Installs[0].Rules.Rules.Version(); len(insts) != 1 || insts[0].Version != "2.5.0" || rules != "kgateway-2.5" {
		t.Fatalf("header %+v, rules %s", insts, rules)
	}
}
