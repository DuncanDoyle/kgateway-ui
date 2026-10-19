package install

import (
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// DiscoveryNamespaceSelectorsEnv is the controller env var rendered from the
// Helm value discoveryNamespaceSelectors.
const DiscoveryNamespaceSelectorsEnv = "KGW_DISCOVERY_NAMESPACE_SELECTORS"

// ScopeMode says how a Scope decides namespace membership.
type ScopeMode string

// Scope modes.
const (
	ScopeAll       ScopeMode = "all"
	ScopeSelectors ScopeMode = "selectors"
	// ScopeUnknown means the scope could not be determined. Contains then
	// answers as if all namespaces were in scope so the UI still shows data,
	// and the health report is forced to "unknown" (design §5.3).
	ScopeUnknown ScopeMode = "unknown"
)

// Scope is the set of namespaces a controller install watches.
type Scope struct {
	Mode      ScopeMode              `json:"mode"`
	Selectors []metav1.LabelSelector `json:"selectors,omitempty"`
	Err       string                 `json:"error,omitempty"`
	matchers  []labels.Selector
}

// Contains reports whether namespace nsName (with object ns, which may be nil
// if the namespace is not cached yet) is in scope.
func (s Scope) Contains(nsName string, ns *corev1.Namespace) bool {
	switch s.Mode {
	case ScopeAll, ScopeUnknown:
		return true
	case ScopeSelectors:
		if ns == nil {
			return false
		}
		for _, m := range s.matchers {
			if m.Matches(labels.Set(ns.Labels)) {
				return true
			}
		}
	}
	return false
}

// ScopeProvider resolves the scope of an install (D-009). Downstreams may register their own.
type ScopeProvider interface {
	Name() string
	Scope(inst Install) Scope
}

// NamespaceSelectorScope mirrors kgateway's discoveryNamespaceSelectors: a JSON
// list of label selectors read from the controller Pod env, OR'ed, where an
// empty or absent list means all namespaces.
type NamespaceSelectorScope struct{}

// Name implements ScopeProvider.
func (NamespaceSelectorScope) Name() string { return "namespace-selectors" }

// Scope implements ScopeProvider. It reads the env of a controller Pod, not
// the Deployment, so a rollout in progress reflects what actually runs. Ready
// pods are preferred, but any pod's spec is used if none is ready.
func (NamespaceSelectorScope) Scope(inst Install) Scope {
	pod := pickPod(inst.Pods)
	if pod == nil {
		return Scope{Mode: ScopeUnknown, Err: "no controller pod found"}
	}
	var c *corev1.Container
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == inst.Container {
			c = &pod.Spec.Containers[i]
		}
	}
	if c == nil {
		return Scope{Mode: ScopeUnknown, Err: fmt.Sprintf("container %q not found in pod %s", inst.Container, pod.Name)}
	}
	var val string
	found := false
	for _, e := range c.Env { // duplicate entries: the last one wins, as in Kubernetes
		if e.Name != DiscoveryNamespaceSelectorsEnv {
			continue
		}
		if e.ValueFrom != nil {
			return Scope{Mode: ScopeUnknown, Err: DiscoveryNamespaceSelectorsEnv + " uses valueFrom, which the UI cannot resolve"}
		}
		val, found = e.Value, true
	}
	if !found {
		return Scope{Mode: ScopeAll}
	}
	return ParseSelectors(val)
}

func pickPod(pods []*corev1.Pod) *corev1.Pod {
	for _, p := range pods {
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				return p
			}
		}
	}
	if len(pods) > 0 {
		return pods[0]
	}
	return nil
}

// ParseSelectors parses the value of KGW_DISCOVERY_NAMESPACE_SELECTORS.
func ParseSelectors(raw string) Scope {
	if strings.TrimSpace(raw) == "" {
		return Scope{Mode: ScopeAll}
	}
	var sels []metav1.LabelSelector
	if err := json.Unmarshal([]byte(raw), &sels); err != nil {
		return Scope{Mode: ScopeUnknown, Err: "invalid " + DiscoveryNamespaceSelectorsEnv + ": " + err.Error()}
	}
	if len(sels) == 0 {
		return Scope{Mode: ScopeAll}
	}
	s := Scope{Mode: ScopeSelectors, Selectors: sels}
	for i := range sels {
		m, err := metav1.LabelSelectorAsSelector(&sels[i])
		if err != nil {
			return Scope{Mode: ScopeUnknown, Err: "invalid selector: " + err.Error()}
		}
		s.matchers = append(s.matchers, m)
	}
	return s
}

// Resolved is an Install with its resolved Scope.
type Resolved struct {
	Install
	Scope Scope
}

// Resolve applies p to every install.
func Resolve(insts []Install, p ScopeProvider) []Resolved {
	out := make([]Resolved, 0, len(insts))
	for _, in := range insts {
		out = append(out, Resolved{Install: in, Scope: p.Scope(in)})
	}
	return out
}
