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

// Scope is the set of namespaces a controller install watches. A Scope built
// by hand (for example by a downstream ScopeProvider) needs only Mode and Selectors.
type Scope struct {
	Mode      ScopeMode              `json:"mode"`
	Selectors []metav1.LabelSelector `json:"selectors,omitempty"`
	Err       string                 `json:"error,omitempty"`
	// Source says where the scope was read from, such as "pod <name>" or
	// "deployment template"; empty for a hand-built Scope.
	Source string `json:"source,omitempty"`
	// matchers are Selectors compiled by ParseSelectors; nil means compile on use.
	matchers []labels.Selector
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
		ms := s.matchers
		if ms == nil {
			ms = compile(s.Selectors)
		}
		for _, m := range ms {
			if m.Matches(labels.Set(ns.Labels)) {
				return true
			}
		}
	}
	return false
}

// compile converts sels to selectors, skipping invalid ones: an invalid
// selector matches no namespace.
func compile(sels []metav1.LabelSelector) []labels.Selector {
	out := make([]labels.Selector, 0, len(sels))
	for i := range sels {
		if m, err := metav1.LabelSelectorAsSelector(&sels[i]); err == nil {
			out = append(out, m)
		}
	}
	return out
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

// SourceDeploymentTemplate is the Scope.Source used when no controller Pod exists.
const SourceDeploymentTemplate = "deployment template"

// Scope implements ScopeProvider. It reads the env from, in order: a Ready
// controller Pod, any controller Pod, then the Deployment's Pod template when
// no Pod exists (for example scaled to zero). A Pod comes first because it
// reflects what is running during a rollout; the template is only what will run.
func (NamespaceSelectorScope) Scope(inst Install) Scope {
	c, source, errMsg := controllerContainer(inst)
	if c == nil {
		return Scope{Mode: ScopeUnknown, Source: source, Err: errMsg}
	}
	var last *corev1.EnvVar
	for i := range c.Env { // duplicate entries: the last one wins, as in Kubernetes
		if c.Env[i].Name == DiscoveryNamespaceSelectorsEnv {
			last = &c.Env[i]
		}
	}
	var sc Scope
	switch {
	case last == nil && len(c.EnvFrom) > 0:
		// The env may come from a ConfigMap or Secret the UI does not read.
		// An explicit entry would win, since env is applied after envFrom.
		sc = Scope{Mode: ScopeUnknown, Err: "container uses envFrom, which may set " + DiscoveryNamespaceSelectorsEnv + "; the UI cannot resolve it"}
	case last == nil:
		sc = Scope{Mode: ScopeAll}
	case last.ValueFrom != nil:
		sc = Scope{Mode: ScopeUnknown, Err: DiscoveryNamespaceSelectorsEnv + " uses valueFrom, which the UI cannot resolve"}
	default:
		sc = ParseSelectors(last.Value)
	}
	sc.Source = source
	return sc
}

// controllerContainer finds the controller container in the first of a Ready
// Pod, any Pod, or the Deployment template. On failure c is nil and errMsg says why.
func controllerContainer(inst Install) (c *corev1.Container, source, errMsg string) {
	var (
		spec  corev1.PodSpec
		where string
	)
	if pod := pickPod(inst.Pods); pod != nil {
		spec, source, where = pod.Spec, "pod "+pod.Name, "pod "+pod.Name
	} else if inst.Deployment != nil {
		spec, source, where = inst.Deployment.Spec.Template.Spec, SourceDeploymentTemplate, "the Deployment template"
	} else {
		return nil, "", "no controller pod or deployment found"
	}
	for i := range spec.Containers {
		if spec.Containers[i].Name == inst.Container {
			c = &spec.Containers[i]
		}
	}
	if c == nil {
		return nil, source, fmt.Sprintf("container %q not found in %s", inst.Container, where)
	}
	return c, source, ""
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
	Scope    Scope
	Settings Settings
}

// Resolve applies p to every install.
func Resolve(insts []Install, p ScopeProvider) []Resolved {
	out := make([]Resolved, 0, len(insts))
	for _, in := range insts {
		out = append(out, Resolved{Install: in, Scope: p.Scope(in), Settings: ReadSettings(in)})
	}
	return out
}
