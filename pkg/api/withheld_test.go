package api

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
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
