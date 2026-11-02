package process

import (
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Kube is the Kubernetes Source: Pods, Deployments and Event deltas of the
// snapshot. It never reads a termination message.
type Kube struct{}

// proxyImage is the last path segment of the kgateway proxy image.
const proxyImage = "envoy-wrapper"

// Controller implements Source: the Pods of the install's controller
// Deployment, as Discover attributed them.
func (Kube) Controller(in install.Resolved, snap *cache.Snapshot) Set {
	d := in.Deployment
	if d == nil {
		return Set{}
	}
	s := Set{Desired: desired(d), Rollout: rollout(d)}
	if !snap.Access[cache.PodsGVR].OK {
		return s
	}
	s.Observed = true
	probes := snap.Access[cache.EventsGVR].OK
	for _, p := range in.Pods {
		s.Processes = append(s.Processes, fromPod(p, container(p, func(c corev1.Container) bool { return c.Name == in.Container }), snap, probes))
	}
	slices.SortFunc(s.Processes, func(a, b Process) int { return strings.Compare(a.Name, b.Name) })
	return s
}

// Proxies implements Source: the Pods gw's proxy Deployment selects. Without the
// Deployment or the proxy Pod watch they are not observed; a consumer tells a
// self-managed proxy apart by joining ProxyMode and Snapshot.Synced.
func (Kube) Proxies(in install.Resolved, gw objects.Ref, snap *cache.Snapshot) Set {
	d := ProxyDeployment(gw, snap.Deployments, in.ProxySelector)
	if d == nil {
		return Set{}
	}
	s := Set{Desired: desired(d), Rollout: rollout(d)}
	sel, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	// An empty selector would claim every Pod in the namespace.
	if !snap.Access[cache.ProxyPodsGVR].OK || err != nil || sel.Empty() {
		return s
	}
	s.Observed = true
	probes := snap.Access[cache.EventsGVR].OK
	for _, p := range snap.ProxyPods {
		if p.Namespace == d.Namespace && sel.Matches(labels.Set(p.Labels)) {
			s.Processes = append(s.Processes, fromPod(p, container(p, func(c corev1.Container) bool { return lastSegment(c.Image) == proxyImage }), snap, probes))
		}
	}
	slices.SortFunc(s.Processes, func(a, b Process) int { return strings.Compare(a.Name, b.Name) })
	return s
}

// ProxyDeployment is gw's proxy Deployment among those sel matches: owned by gw,
// else the only one labelled SafeGatewayLabelValue(gw.Name). A nil sel matches none.
func ProxyDeployment(gw objects.Ref, ds []*appsv1.Deployment, sel labels.Selector) *appsv1.Deployment {
	if sel == nil {
		return nil
	}
	label := SafeGatewayLabelValue(gw.Name)
	var byLabel []*appsv1.Deployment
	for _, d := range ds {
		if d.Namespace != gw.Namespace || !sel.Matches(labels.Set(d.Labels)) {
			continue
		}
		for _, o := range d.OwnerReferences {
			if o.Kind == "Gateway" && o.Name == gw.Name && strings.HasPrefix(o.APIVersion, objects.GatewayAPIGroup+"/") {
				return d
			}
		}
		if d.Labels["gateway.networking.k8s.io/gateway-name"] == label {
			byLabel = append(byLabel, d)
		}
	}
	// Several label matches cannot be told apart.
	if len(byLabel) != 1 {
		return nil
	}
	return byLabel[0]
}

// desired is d's replica count; the API server defaults an unset one to 1.
func desired(d *appsv1.Deployment) *int32 {
	n := int32(1)
	if d.Spec.Replicas != nil {
		n = *d.Spec.Replicas
	}
	return &n
}

// rollout reads d's Progressing condition: running while a generation is unobserved or the
// reason is not NewReplicaSetAvailable; ProgressDeadlineExceeded is stuck, not progressing.
func rollout(d *appsv1.Deployment) *Rollout {
	r := &Rollout{}
	var cond *appsv1.DeploymentCondition
	for i := range d.Status.Conditions {
		if d.Status.Conditions[i].Type == appsv1.DeploymentProgressing {
			cond = &d.Status.Conditions[i]
		}
	}
	if cond != nil {
		r.Reason = cond.Reason
	}
	switch {
	case cond != nil && cond.Reason == "ProgressDeadlineExceeded":
	case d.Generation > d.Status.ObservedGeneration, cond != nil && cond.Status == corev1.ConditionTrue && cond.Reason != "NewReplicaSetAvailable":
		r.Progressing = true
	case cond != nil && cond.Reason == "NewReplicaSetAvailable" && !cond.LastUpdateTime.IsZero():
		t := cond.LastUpdateTime.Time
		r.SettledAt = &t
	}
	return r
}

// container returns the first container of p that match accepts, else the first one.
func container(p *corev1.Pod, match func(corev1.Container) bool) *corev1.Container {
	for i := range p.Spec.Containers {
		if match(p.Spec.Containers[i]) {
			return &p.Spec.Containers[i]
		}
	}
	if len(p.Spec.Containers) > 0 {
		return &p.Spec.Containers[0]
	}
	return nil
}

// fromPod reads the process facts of p, with c as its main container. The
// status fields stay nil when p reports none; Probe is set only when probes is.
func fromPod(p *corev1.Pod, c *corev1.Container, snap *cache.Snapshot, probes bool) Process {
	out := Process{ID: string(p.UID), Name: p.Name}
	for _, cond := range p.Status.Conditions {
		if cond.Type == corev1.PodReady {
			ready := cond.Status == corev1.ConditionTrue
			out.Ready = &ready
		}
	}
	if c != nil {
		out.Version = strings.TrimPrefix(tag(c.Image), "v")
		for i := range p.Status.ContainerStatuses {
			if cs := &p.Status.ContainerStatuses[i]; cs.Name == c.Name {
				containerFacts(&out, cs)
			}
		}
	}
	if probes {
		out.Probe = probeFailures(p, snap)
	}
	return out
}

// containerFacts copies cs's restarts, start, waiting reason and last
// termination into out; the termination message is never read.
func containerFacts(out *Process, cs *corev1.ContainerStatus) {
	n := cs.RestartCount
	out.Restarts = &n
	if r := cs.State.Running; r != nil && !r.StartedAt.IsZero() {
		t := r.StartedAt.Time
		out.LastStart = &t
	}
	if w := cs.State.Waiting; w != nil {
		out.Waiting = w.Reason
	}
	term := cs.State.Terminated
	if term == nil {
		term = cs.LastTerminationState.Terminated
	}
	if term != nil {
		out.Termination = &Termination{Reason: term.Reason, ExitCode: term.ExitCode, At: term.FinishedAt.Time}
	}
}

// probeFailures sums p's observed probe failures in the window ending at
// snap.TakenAt. The type comes from the Pod spec, never an Event message.
func probeFailures(p *corev1.Pod, snap *cache.Snapshot) *ProbeFailures {
	f := &ProbeFailures{Type: probeType(p), HistoryUnknown: snap.EventHistoryUnknown[p.UID]}
	from := snap.TakenAt.Add(-cache.EventWindow)
	for _, o := range snap.EventDeltas[p.UID] {
		if o.Reason != ProbeReason || o.At.Before(from) {
			continue
		}
		f.Count += int(o.Delta)
		if f.First.IsZero() || o.At.Before(f.First) {
			f.First = o.At
		}
		if o.At.After(f.Last) {
			f.Last = o.At
		}
	}
	return f
}

// probeType is "liveness", "readiness" or "startup" when p's containers
// define exactly that one probe type, else "".
func probeType(p *corev1.Pod) string {
	var types []string
	add := func(defined bool, t string) {
		if defined && !slices.Contains(types, t) {
			types = append(types, t)
		}
	}
	for _, c := range p.Spec.Containers {
		add(c.LivenessProbe != nil, "liveness")
		add(c.ReadinessProbe != nil, "readiness")
		add(c.StartupProbe != nil, "startup")
	}
	if len(types) == 1 {
		return types[0]
	}
	return ""
}

// lastSegment is the last path segment of image's repository.
// "reg:5000/a/envoy-wrapper:v1@sha256:…" → "envoy-wrapper".
func lastSegment(image string) string {
	image, _, _ = strings.Cut(image, "@")
	last := image[strings.LastIndex(image, "/")+1:]
	name, _, _ := strings.Cut(last, ":")
	return name
}

// tag is image's tag, "" for a digest-only or untagged reference.
func tag(image string) string {
	image, _, _ = strings.Cut(image, "@")
	last := image[strings.LastIndex(image, "/")+1:]
	_, t, _ := strings.Cut(last, ":")
	return t
}

var _ Source = Kube{}
