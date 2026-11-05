package diagnose_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/diagnose"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// stepsInput is an install in kgw with the upstream proxy selector and the
// infra/edge proxy Deployment.
func stepsInput() diagnose.Input {
	res := install.Resolved{Install: install.Install{Namespace: "kgw", Name: "kgateway", Container: "kgateway",
		ProxySelector: install.UpstreamDetector{}.ProxySelector()}}
	return diagnose.Input{Install: res, Snapshot: &cache.Snapshot{Deployments: []*appsv1.Deployment{testutil.ProxyDeployment("infra", "edge", 2, 2)}}}
}

var edgeGW = objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}

// rejecting is 001 on infra/edge, reported by ctrl-b for one type and ctrl-a for another.
func rejecting() health.Finding {
	return health.Finding{Code: health.CodeXDSRejected, Severity: health.SeverityError, Resource: &edgeGW, Facts: &health.XDSRejectsEvidence{
		Types: []health.XDSTypeRejects{{TypeURL: "envoy.config.route.v3.RouteConfiguration", ReportedBy: []string{"ctrl-b"}},
			{TypeURL: "envoy.config.cluster.v3.Cluster", ReportedBy: []string{"ctrl-b", "ctrl-a"}}}}}
}

// restarted is 019 on subject with Pod pod's last exit code.
func restarted(subject objects.Ref, pod string, code int32) health.Finding {
	ns := subject.Namespace
	return health.Finding{Code: health.CodeProcessRestarting, Severity: health.SeverityWarning, Resource: &subject,
		Evidence: []objects.Ref{{Kind: "Pod", Namespace: ns, Name: pod}},
		Facts:    &health.ProcessEvidence{Pod: pod, Termination: &health.TerminationFacts{Reason: "Error", ExitCode: code}}}
}

// steps groups fs with the upstream families and returns each group's steps for a caller who sees everything.
func steps(t *testing.T, in diagnose.Input, c diagnose.Commands, fs ...health.Finding) [][]diagnose.NextStep {
	t.Helper()
	return stepsFor(t, in, c, nil, fs...)
}

// stepsFor is steps for a caller who sees what can allows.
func stepsFor(t *testing.T, in diagnose.Input, c diagnose.Commands, can func(objects.Ref) bool, fs ...health.Finding) [][]diagnose.NextStep {
	t.Helper()
	rep := &health.Report{Findings: fs}
	d := diagnose.Build(rep, in, diagnose.Families())
	out := make([][]diagnose.NextStep, len(d.Groups))
	for i, g := range d.Groups {
		out[i] = diagnose.Steps(g, rep.Findings, in, c, can)
	}
	return out
}

// Unsafe values are single-quoted; the §8.5 commands name the first reporting replica.
func TestCommandsQuoteValues(t *testing.T) {
	for in, want := range map[string]string{
		"edge": "edge", "kgw/ctrl-a.1:x=y_z": "kgw/ctrl-a.1:x=y_z", "": "''", "xds error": "'xds error'",
		"it's": `'it'\''s'`, "$(rm -rf /)": "'$(rm -rf /)'", "a;b|c": "'a;b|c'", "*": "'*'", "-rf": "'-rf'", "a-b": "a-b",
	} {
		if got := diagnose.Quote(in); got != want {
			t.Errorf("Quote(%q) = %s, want %s", in, got, want)
		}
	}
	k := diagnose.Kubectl{}
	odd := objects.Ref{Kind: "Pod", Namespace: "kgw", Name: "ctrl a"}
	if got, want := k.ControllerLog(odd, time.Hour, "xds error", "edge"), `kubectl logs -n kgw 'ctrl a' --since=1h | grep 'xds error' | grep edge`; got != want {
		t.Errorf("ControllerLog = %s, want %s", got, want)
	}
	if got, want := k.ControllerLog(odd, 30*time.Second, "--version"), `kubectl logs -n kgw 'ctrl a' --since=30s | grep -e '--version'`; got != want {
		t.Errorf("dash pattern: ControllerLog = %s, want %s", got, want)
	}
	proxy := objects.Ref{Group: "apps", Kind: "Deployment", Namespace: "in fra", Name: "edge"}
	if got, want := k.ProxyLog(proxy, 90*time.Minute, "rejected"), `kubectl logs -n 'in fra' deploy/edge --since=90m | grep -i rejected`; got != want {
		t.Errorf("ProxyLog = %s, want %s", got, want)
	}
	if got, want := k.ProxyCounters(proxy), "kubectl port-forward -n 'in fra' deploy/edge 19000:19000\ncurl -s localhost:19000/stats | grep -E 'update_(rejected|success)'"; got != want {
		t.Errorf("ProxyCounters = %s, want %s", got, want)
	}
	if got, want := k.PreviousLog(odd, "kgateway"), `kubectl logs --previous -n kgw 'ctrl a' -c kgateway`; got != want {
		t.Errorf("PreviousLog = %s, want %s", got, want)
	}

	gs := steps(t, stepsInput(), k, rejecting())
	if len(gs) != 1 || len(gs[0]) != 3 {
		t.Fatalf("want three data plane steps, got %+v", gs)
	}
	ctrlA := objects.Ref{Kind: "Pod", Namespace: "kgw", Name: "ctrl-a"}
	edgeDeploy := objects.Ref{Group: "apps", Kind: "Deployment", Namespace: "infra", Name: "edge"}
	want := []diagnose.NextStep{
		{Title: diagnose.StepEnvoyReason, Command: "kubectl logs -n kgw ctrl-a --since=1h | grep 'xds error' | grep edge",
			Needs: []string{"pods/log"}, Note: diagnose.StepShareNote, Refs: []objects.Ref{ctrlA, edgeGW}},
		{Title: diagnose.StepProxyLog, Command: "kubectl logs -n infra deploy/edge --since=1h | grep -i rejected",
			Needs: []string{"pods/log"}, Note: diagnose.StepShareNote, Refs: []objects.Ref{edgeDeploy, edgeGW}},
		{Title: diagnose.StepProxyCounters, Command: k.ProxyCounters(edgeDeploy),
			Needs: []string{"pods/portforward"}, Note: diagnose.StepPortForwardNote + " " + diagnose.StepShareNote, Refs: []objects.Ref{edgeDeploy, edgeGW}},
	}
	if !reflect.DeepEqual(gs[0], want) {
		t.Fatalf("steps\n%+v\nwant\n%+v", gs[0], want)
	}
	// Same inputs, same steps.
	if again := steps(t, stepsInput(), k, rejecting()); !reflect.DeepEqual(again, gs) {
		t.Fatalf("steps differ between runs: %+v", again)
	}
}

// hide is Kubectl with ProxyLog returning "" and, optionally, PreviousLog too.
type hide struct {
	diagnose.Kubectl
	previous bool
}

func (hide) ProxyLog(objects.Ref, time.Duration, string) string { return "" }

func (h hide) PreviousLog(p objects.Ref, c string) string {
	if h.previous {
		return ""
	}
	return h.Kubectl.PreviousLog(p, c)
}

// An empty command hides its step; only a controller exit code 2 gets the previous log.
func TestStepsHiddenWhenCommandEmpty(t *testing.T) {
	gs := steps(t, stepsInput(), hide{}, rejecting())
	if len(gs) != 1 || len(gs[0]) != 2 || gs[0][0].Title != diagnose.StepEnvoyReason || gs[0][1].Title != diagnose.StepProxyCounters {
		t.Fatalf("want the reason and counter steps, got %+v", gs)
	}

	ctrl := objects.Ref{Group: "apps", Kind: "Deployment", Namespace: "kgw", Name: "kgateway"}
	gs = steps(t, stepsInput(), diagnose.Kubectl{}, restarted(ctrl, "kgateway-0", 2))
	want := []diagnose.NextStep{{Title: diagnose.StepPanicTrace, Command: "kubectl logs --previous -n kgw kgateway-0 -c kgateway",
		Needs: []string{"pods/log"}, Note: diagnose.StepShareNote, Refs: []objects.Ref{{Kind: "Pod", Namespace: "kgw", Name: "kgateway-0"}}}}
	if len(gs) != 1 || !reflect.DeepEqual(gs[0], want) {
		t.Fatalf("exit code 2 steps %+v, want %+v", gs, want)
	}
	if gs := steps(t, stepsInput(), hide{previous: true}, restarted(ctrl, "kgateway-0", 2)); len(gs) != 1 || len(gs[0]) != 0 {
		t.Fatalf("hidden previous log: %+v", gs)
	}
	if gs := steps(t, stepsInput(), diagnose.Kubectl{}, restarted(ctrl, "kgateway-0", 137)); len(gs) != 1 || len(gs[0]) != 0 {
		t.Fatalf("exit code 137 steps %+v", gs)
	}
	if gs := steps(t, stepsInput(), diagnose.Kubectl{}, restarted(edgeGW, "edge-0", 2)); len(gs) != 1 || len(gs[0]) != 0 {
		t.Fatalf("proxy restart steps %+v", gs)
	}
	// A self-managed Gateway has no proxy Deployment: only the controller step.
	in := stepsInput()
	in.Snapshot = &cache.Snapshot{}
	if gs := steps(t, in, diagnose.Kubectl{}, rejecting()); len(gs) != 1 || len(gs[0]) != 1 || gs[0][0].Title != diagnose.StepEnvoyReason {
		t.Fatalf("no proxy Deployment: %+v", gs)
	}
}

// Steps name only visible objects (design §14 rule 15): a hidden replica gives
// way to a visible one, and a restricted finding gets no steps.
func TestStepsNameOnlyVisibleObjects(t *testing.T) {
	hidden := func(rs ...objects.Ref) func(objects.Ref) bool {
		return func(r objects.Ref) bool { return !slices.Contains(rs, r) }
	}
	ctrlA, ctrlB := objects.Ref{Kind: "Pod", Namespace: "kgw", Name: "ctrl-a"}, objects.Ref{Kind: "Pod", Namespace: "kgw", Name: "ctrl-b"}
	edgeDeploy := objects.Ref{Group: "apps", Kind: "Deployment", Namespace: "infra", Name: "edge"}
	k := diagnose.Kubectl{}
	// Evidence-restricted: ctrl-a hidden, so ctrl-b's log is the step.
	gs := stepsFor(t, stepsInput(), k, hidden(ctrlA), rejecting())
	if len(gs[0]) != 3 || gs[0][0].Command != "kubectl logs -n kgw ctrl-b --since=1h | grep 'xds error' | grep edge" || !reflect.DeepEqual(gs[0][0].Refs, []objects.Ref{ctrlB, edgeGW}) {
		t.Fatalf("hidden ctrl-a: %+v", gs[0])
	}
	if gs := stepsFor(t, stepsInput(), k, hidden(ctrlA, ctrlB), rejecting()); len(gs[0]) != 2 || gs[0][0].Title != diagnose.StepProxyLog {
		t.Fatalf("every replica hidden: %+v", gs[0])
	}
	if gs := stepsFor(t, stepsInput(), k, hidden(edgeDeploy), rejecting()); len(gs[0]) != 1 || gs[0][0].Title != diagnose.StepEnvoyReason {
		t.Fatalf("hidden proxy Deployment: %+v", gs[0])
	}
	// Restricted: the Gateway is hidden.
	if gs := stepsFor(t, stepsInput(), k, hidden(edgeGW), rejecting()); len(gs[0]) != 0 {
		t.Fatalf("hidden Gateway: %+v", gs[0])
	}
	ctrl := objects.Ref{Group: "apps", Kind: "Deployment", Namespace: "kgw", Name: "kgateway"}
	pod := objects.Ref{Kind: "Pod", Namespace: "kgw", Name: "kgateway-0"}
	for name, can := range map[string]func(objects.Ref) bool{"restricted": hidden(ctrl), "evidence-restricted": hidden(pod)} {
		if gs := stepsFor(t, stepsInput(), k, can, restarted(ctrl, "kgateway-0", 2)); len(gs[0]) != 0 {
			t.Fatalf("%s 019: %+v", name, gs[0])
		}
	}
}

// Design §10.2: 018's step greps the controller log for status, through the
// Deployment, once for the whole group; a hidden Deployment gets no step.
func TestStatusNotProgressingStep(t *testing.T) {
	ctrl := objects.Ref{Group: "apps", Kind: "Deployment", Namespace: "kgw", Name: "kgateway"}
	route := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "cart"}
	fs := []health.Finding{
		{Code: health.CodeStatusNotProgressing, Severity: health.SeverityWarning, Resource: &ctrl, Facts: &health.StatusProgressEvidence{}},
		{Code: health.CodeRouteUnattached, Severity: health.SeverityWarning, Resource: &route, Facts: &health.FreshnessEvidence{Freshness: health.FreshnessStale}},
	}
	gs := steps(t, stepsInput(), diagnose.Kubectl{}, fs...)
	want := []diagnose.NextStep{{Title: diagnose.StepStatusLog, Command: "kubectl logs -n kgw deploy/kgateway --since=1h | grep status",
		Needs: []string{"pods/log"}, Note: diagnose.StepShareNote, Refs: []objects.Ref{ctrl}}}
	if len(gs) != 1 || !reflect.DeepEqual(gs[0], want) {
		t.Fatalf("steps %+v, want %+v", gs, want)
	}
	if gs := stepsFor(t, stepsInput(), diagnose.Kubectl{}, func(r objects.Ref) bool { return r != ctrl }, fs...); len(gs[0]) != 0 {
		t.Fatalf("hidden controller: %+v", gs[0])
	}
}
