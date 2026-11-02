// Package process is the process seam (D-016, D-029): a provider reports the
// controller replicas and Gateway proxies of an install, so a platform without
// Pods can supply the same facts. Kube is the Kubernetes provider.
package process

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Source reports the processes of one install: its controller replicas and
// the proxies of each Gateway it owns.
type Source interface {
	Controller(in install.Resolved, snap *cache.Snapshot) Set
	Proxies(in install.Resolved, gw objects.Ref, snap *cache.Snapshot) Set
}

// Set is the processes behind one role. Observed false means the provider
// cannot see them (a self-managed proxy, a static install): "not observed",
// never "failing" (D-016 R3). Desired and Rollout may still be set from the
// workload when its processes are not observed.
type Set struct {
	Observed bool
	// Desired is the desired replica count; nil when not applicable.
	Desired *int32
	// Rollout is nil when the provider has no rollout notion.
	Rollout   *Rollout
	Processes []Process
}

// Process is one controller replica or proxy. A nil field, or an empty
// Waiting or Version, means "not observed", which is never a failure.
type Process struct {
	// ID is stable within the provider; a Pod UID in Kubernetes.
	ID       string
	Name     string
	Ready    *bool
	Restarts *int32
	// LastStart is when the running container started.
	LastStart *time.Time
	// Termination is the last termination; never its message.
	Termination *Termination
	// Waiting is a reason such as CrashLoopBackOff or ImagePullBackOff; empty when running.
	Waiting string
	// Version is the image tag without a leading "v"; empty when unknown.
	Version string
	// Probe is nil when the provider has no probe events.
	Probe *ProbeFailures
}

// Rollout is the state of a workload's last rollout. Progressing is true while
// a rollout is under way; a stuck one is not progressing and carries its
// reason, such as ProgressDeadlineExceeded. SettledAt is when the last rollout
// finished; nil while one runs or when unknown.
type Rollout struct {
	Progressing bool
	Reason      string
	SettledAt   *time.Time
}

// Termination is how a process last ended.
type Termination struct {
	Reason   string
	ExitCode int32
	At       time.Time
}

// ProbeFailures are the probe failures observed on a process in the last
// cache.EventWindow, as Event count increases (design §9.2). Type is the one
// probe type the process defines, or "" ("a probe") when it defines several.
// First and Last are the oldest and newest observation; HistoryUnknown marks
// earlier failures that were not counted.
type ProbeFailures struct {
	Type           string
	Count          int
	First, Last    time.Time
	HistoryUnknown bool
}

// ProbeReason is the Event reason of a probe failure.
const ProbeReason = "Unhealthy"

// PodRef returns the ref of process p of a Set whose Pods are in namespace
// ns: the install's namespace for the controller, the Gateway's for proxies.
// Callers check it before they show p's name or facts (design §14 rule 16).
func PodRef(ns string, p Process) objects.Ref {
	return objects.Ref{Kind: "Pod", Namespace: ns, Name: p.Name}
}

// SafeGatewayLabelValue is the gateway-name label value kgateway writes for a
// Gateway: its name, or for a name over 63 characters the first 50 (trailing
// "-" trimmed), "-" and the first 12 hex digits of the name's SHA-256.
func SafeGatewayLabelValue(name string) string {
	if len(name) <= 63 {
		return name
	}
	h := sha256.Sum256([]byte(name))
	return strings.TrimSuffix(name[:50], "-") + "-" + hex.EncodeToString(h[:])[:12]
}
