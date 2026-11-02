// Package process is the process seam (D-016, D-029): providers report an
// install's controller and proxy processes; Kube is the Kubernetes one.
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

// Set is the processes behind one role. Observed false means "not observed",
// never "failing" (D-016 R3); Desired and Rollout may still be set.
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

// Rollout is a workload's last rollout: Progressing while one runs, a stuck one's
// Reason (ProgressDeadlineExceeded), and SettledAt when it finished, if known.
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

// ProbeFailures are the probe failures observed in the last cache.EventWindow
// (§9.2). Type is the one defined probe type, else "" ("a probe").
type ProbeFailures struct {
	Type           string
	Count          int
	First, Last    time.Time
	HistoryUnknown bool
}

// ProbeReason is the Event reason of a probe failure.
const ProbeReason = "Unhealthy"

// PodRef is p's Pod in ns (the install's or the Gateway's namespace), which
// callers check before showing p's name or facts (§14 rule 16).
func PodRef(ns string, p Process) objects.Ref {
	return objects.Ref{Kind: "Pod", Namespace: ns, Name: p.Name}
}

// SafeGatewayLabelValue is kgateway's gateway-name label value: the name, or past
// 63 characters its first 50 ("-" trimmed), "-" and 12 hex digits of its SHA-256.
func SafeGatewayLabelValue(name string) string {
	if len(name) <= 63 {
		return name
	}
	h := sha256.Sum256([]byte(name))
	return strings.TrimSuffix(name[:50], "-") + "-" + hex.EncodeToString(h[:])[:12]
}
