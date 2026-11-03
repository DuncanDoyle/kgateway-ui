package diagnose

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
)

// Commands fills in the next-step commands (design §8.5). A method that
// returns "" hides its step, so a platform without kubectl overrides or hides them.
type Commands interface {
	// ControllerLog reads a controller Pod's log since a duration ago, filtered by each grep.
	ControllerLog(pod objects.Ref, since time.Duration, grep ...string) string
	// ProxyLog reads a proxy's log since a duration ago, filtered case-insensitively by grep.
	ProxyLog(proxy objects.Ref, since time.Duration, grep string) string
	// ProxyCounters port-forwards to a proxy's admin port and reads its update counters.
	ProxyCounters(proxy objects.Ref) string
	// PreviousLog reads the previous container's log of a Pod.
	PreviousLog(pod objects.Ref, container string) string
}

// Kubectl is the upstream Commands: the kubectl commands of design §8.5,
// every value passed through Quote.
type Kubectl struct{}

// ControllerLog implements Commands.
func (Kubectl) ControllerLog(pod objects.Ref, since time.Duration, grep ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "kubectl logs -n %s %s --since=%s", Quote(pod.Namespace), Quote(target(pod)), duration(since))
	for _, g := range grep {
		b.WriteString(" | grep " + Quote(g))
	}
	return b.String()
}

// ProxyLog implements Commands.
func (Kubectl) ProxyLog(proxy objects.Ref, since time.Duration, grep string) string {
	return fmt.Sprintf("kubectl logs -n %s %s --since=%s | grep -i %s", Quote(proxy.Namespace), Quote(target(proxy)), duration(since), Quote(grep))
}

// ProxyCounters implements Commands.
func (Kubectl) ProxyCounters(proxy objects.Ref) string {
	return fmt.Sprintf("kubectl port-forward -n %s %s 19000:19000\ncurl -s localhost:19000/stats | grep -E %s",
		Quote(proxy.Namespace), Quote(target(proxy)), Quote("update_(rejected|success)"))
}

// PreviousLog implements Commands.
func (Kubectl) PreviousLog(pod objects.Ref, container string) string {
	cmd := fmt.Sprintf("kubectl logs --previous -n %s %s", Quote(pod.Namespace), Quote(target(pod)))
	if container != "" {
		cmd += " -c " + Quote(container)
	}
	return cmd
}

// target is kubectl's name for r: "deploy/<name>" for a Deployment, else the name.
func target(r objects.Ref) string {
	if r.Group == "apps" && r.Kind == "Deployment" {
		return "deploy/" + r.Name
	}
	return r.Name
}

// duration is d as kubectl's --since value, in whole hours or minutes where it divides.
func duration(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return fmt.Sprintf("%ds", d/time.Second)
}

// Quote returns s as one POSIX shell word: unchanged when every byte is in
// [A-Za-z0-9_./:=-], else in single quotes with each quote escaped.
func Quote(s string) string {
	safe := s != ""
	for i := 0; safe && i < len(s); i++ {
		c := s[i]
		safe = c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("_./:=-", c) >= 0
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// NextStep is one command a user can run to learn more (design §13.3). Needs
// names the permissions it requires; Refs are the objects its command names.
type NextStep struct {
	Title, Command string
	Needs          []string
	Note           string
	Refs           []objects.Ref
}

// Wording of the next steps (design §8.5).
const (
	StepEnvoyReason   = "See Envoy's reason"
	StepProxyLog      = "See it on the proxy"
	StepProxyCounters = "Check the proxy's counters"
	StepPanicTrace    = "Read the panic's stack trace"
	StepShareNote     = "Log lines and admin output can contain configuration values, including secrets. Share them with care."
)

// stepsSince is how far back the log commands read.
const stepsSince = time.Hour

// Permissions the steps need, as "<resource>/<subresource>".
const (
	needsLog         = "pods/log"
	needsPortForward = "pods/portforward"
)

// Steps returns g's next steps in order, built from its members in findings.
// Data plane: the controller log of the first replica that reported a reject,
// then the proxy log and counters. Process: the previous log of a controller
// Pod that last exited with code 2. A command c returns "" for is left out.
func Steps(g Group, findings []health.Finding, in Input, c Commands) []NextStep {
	var out []NextStep
	add := func(title, cmd, needs string, refs ...objects.Ref) {
		if cmd != "" {
			out = append(out, NextStep{Title: title, Command: cmd, Needs: []string{needs}, Note: StepShareNote, Refs: refs})
		}
	}
	for _, i := range g.Members {
		f := &findings[i]
		if f.Resource == nil {
			continue
		}
		switch ev := f.Facts.(type) {
		case *health.XDSRejectsEvidence:
			if f.Code != health.CodeXDSRejected {
				continue
			}
			gw := *f.Resource
			if pod, ok := reporter(ev, in.Install.Namespace); ok {
				add(StepEnvoyReason, c.ControllerLog(pod, stepsSince, "xds error", gw.Name), needsLog, pod, gw)
			}
			if in.Snapshot == nil {
				continue
			}
			if d := process.ProxyDeployment(gw, in.Snapshot.Deployments, in.Install.ProxySelector); d != nil {
				proxy := objects.Ref{Group: "apps", Kind: "Deployment", Namespace: d.Namespace, Name: d.Name}
				add(StepProxyLog, c.ProxyLog(proxy, stepsSince, "rejected"), needsLog, proxy, gw)
				add(StepProxyCounters, c.ProxyCounters(proxy), needsPortForward, proxy, gw)
			}
		case *health.ProcessEvidence:
			// Only the controller is a Go process; its subject is the controller Deployment.
			if f.Code != health.CodeProcessRestarting || f.Resource.Kind != "Deployment" || ev.Termination == nil || ev.Termination.ExitCode != 2 {
				continue
			}
			pod := process.PodRef(f.Resource.Namespace, process.Process{Name: ev.Pod})
			add(StepPanicTrace, c.PreviousLog(pod, in.Install.Container), needsLog, pod)
		}
	}
	return out
}

// reporter is the first controller Pod, by name, that reports one of ev's rejects.
func reporter(ev *health.XDSRejectsEvidence, ns string) (objects.Ref, bool) {
	var names []string
	for _, t := range ev.Types {
		names = append(names, t.ReportedBy...)
	}
	if len(names) == 0 {
		return objects.Ref{}, false
	}
	return process.PodRef(ns, process.Process{Name: slices.Min(names)}), true
}

// VisibleSteps returns the steps whose every named object can is true for, in
// order (design §14 rule 15). It never changes steps.
func VisibleSteps(steps []NextStep, can func(objects.Ref) bool) []NextStep {
	out := make([]NextStep, 0, len(steps))
	for _, s := range steps {
		if !slices.ContainsFunc(s.Refs, func(r objects.Ref) bool { return !can(r) }) {
			out = append(out, s)
		}
	}
	return out
}
