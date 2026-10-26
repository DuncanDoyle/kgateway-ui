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

// WildcardEnv is the controller env var that names the namespace whose delegation
// label selects routes in every namespace.
const WildcardEnv = "DELEGATION_WILDCARD_NAMESPACE"

// WildcardAll is the wildcard namespace's default: no namespace is special.
const WildcardAll = "all"

// Settings are the controller settings the UI reads.
type Settings struct {
	Experimental Setting
	// WildcardNamespace is the delegation wildcard namespace; "all" when absent or empty.
	WildcardNamespace Setting
}

// ReadSettings reads the controller settings from the same source as Scope (D-015).
// An absent experimental entry means "true", the controller's default; an
// absent or empty wildcard entry means "all".
func ReadSettings(inst Install) Settings {
	c, source, errMsg := controllerContainer(inst)
	if c == nil {
		s := Setting{Reason: errMsg, Source: source}
		return Settings{Experimental: s, WildcardNamespace: s}
	}
	return Settings{
		Experimental:      readEnv(c, source, ExperimentalEnv, "true"),
		WildcardNamespace: readEnv(c, source, WildcardEnv, WildcardAll),
	}
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
