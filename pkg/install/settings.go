package install

import (
	"strconv"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
)

// ExperimentalEnv is the controller env var that enables experimental Gateway API features.
const ExperimentalEnv = "KGW_ENABLE_EXPERIMENTAL_GATEWAY_API_FEATURES"

// Setting is one controller setting as read from its container env.
type Setting struct {
	Value string
	// Known is false when the UI cannot tell the value; Reason then says why.
	Known  bool
	Reason string
	// Source is where the value was read, as in Scope.Source.
	Source string
}

// Bool parses the value. An unknown or unparsable value is not known.
func (s Setting) Bool() (v, known bool) {
	if !s.Known {
		return false, false
	}
	b, err := strconv.ParseBool(s.Value)
	if err != nil {
		return false, false
	}
	return b, true
}

// Settings are the controller settings the UI reads.
type Settings struct {
	Experimental Setting
}

// ReadSettings reads the controller settings from the same source as Scope (D-015).
// An absent experimental entry means "true", the controller's default.
func ReadSettings(inst Install) Settings {
	c, source, errMsg := controllerContainer(inst)
	if c == nil {
		return Settings{Experimental: Setting{Reason: errMsg, Source: source}}
	}
	last := -1
	for i := range c.Env { // duplicate entries: the last one wins, as in Kubernetes
		if c.Env[i].Name == ExperimentalEnv {
			last = i
		}
	}
	s := Setting{Source: source}
	switch {
	case last < 0 && len(c.EnvFrom) > 0:
		s.Reason = "container uses envFrom, which may set " + ExperimentalEnv + "; the UI cannot resolve it"
	case last < 0:
		s.Value, s.Known = "true", true
	case c.Env[last].ValueFrom != nil:
		s.Reason = ExperimentalEnv + " uses valueFrom, which the UI cannot resolve"
	default:
		s.Value, s.Known = c.Env[last].Value, true
	}
	return Settings{Experimental: s}
}

// NamespaceLabels returns the labels of every cached Namespace by name; ok is
// false when the Namespaces could not be read.
func NamespaceLabels(snap *cache.Snapshot) (labels map[string]map[string]string, ok bool) {
	labels = make(map[string]map[string]string, len(snap.Namespaces))
	for _, n := range snap.Namespaces {
		l := n.Labels
		if l == nil {
			l = map[string]string{}
		}
		labels[n.Name] = l
	}
	return labels, snap.Access[cache.NamespacesGVR].OK
}
