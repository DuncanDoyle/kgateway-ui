package objects

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// InheritParentMatcherAnnotation makes a delegated child inherit its parent's matcher.
const InheritParentMatcherAnnotation = "delegation.kgateway.dev/inherit-parent-matcher"

// DelegationLabelKey is the label a label-mode delegation ref selects on
// (kgateway api/labels/delegation.go:7).
const DelegationLabelKey = "delegation.kgateway.dev/label"

// delegationLabelGroup and delegationLabelKind identify a label-selector delegation ref.
const (
	delegationLabelGroup = "delegation.kgateway.dev"
	delegationLabelKind  = "label"
)

// HTTPRouteView is the decoded spec and status of an HTTPRoute.
type HTTPRouteView struct {
	Hostnames []string
	// ParentRefs are spec.parentRefs resolved; WrittenRefs are the same refs
	// as written, index-aligned.
	ParentRefs           []ParentRef
	WrittenRefs          []WrittenRef
	Rules                []Rule
	Parents              []RouteParentStatus
	Labels               map[string]string
	InheritParentMatcher bool
}

// Rule is one spec.rules entry.
type Rule struct {
	Index int
	Name  string
	// Matches is nil when omitted and empty for an explicit empty list.
	Matches     []Match
	Filters     []Filter
	BackendRefs []BackendRef
}

// Match is one HTTPRouteMatch.
type Match struct {
	Path        *PathMatch
	Headers     []KeyMatch
	QueryParams []KeyMatch
	Method      string
}

// PathMatch is a path match; Type defaults to PathPrefix and Value to "/".
type PathMatch struct {
	Type, Value string
}

// KeyMatch is a header or query parameter match; Type defaults to Exact.
type KeyMatch struct {
	Type, Name, Value string
}

// Filter is one HTTPRouteFilter. Config is the filter as written.
type Filter struct {
	Type         string
	ExtensionRef *Ref
	Mirror       *MirrorFilter
	Config       map[string]any
}

// MirrorFilter is a RequestMirror filter. Port is 0 when unset.
type MirrorFilter struct {
	Backend  Ref
	Port     int32
	Percent  *int32
	Fraction *Fraction
}

// Fraction is a mirror fraction; Denominator defaults to 100.
type Fraction struct {
	Numerator, Denominator int32
}

// BackendRef is one rule backendRef. Kind defaults to Service and Namespace
// to the route's. An absent weight is 1 with WeightDefaulted set; an absent
// port is 0.
type BackendRef struct {
	Ref             Ref
	Port            int32
	Weight          int32
	WeightDefaulted bool
	Filters         []Filter
	// Delegation is set when the ref delegates to child routes.
	Delegation *DelegationRef
}

// DelegationMode is how a delegation backendRef selects child routes.
type DelegationMode string

// Delegation modes (computed-rules §3.2).
const (
	DelegationName     DelegationMode = "name"
	DelegationWildcard DelegationMode = "wildcard"
	DelegationLabel    DelegationMode = "label"
)

// DelegationRef is a delegation backendRef. Namespace defaults to the parent
// route's; Name is set in name mode and Label in label mode.
type DelegationRef struct {
	Mode      DelegationMode
	Namespace string
	Name      string
	Label     string
}

// BackendTarget is a backend a route sends or mirrors traffic to.
type BackendTarget struct {
	Ref    Ref
	Mirror bool
}

// DecodeHTTPRoute decodes an HTTPRoute. It never fails.
func DecodeHTTPRoute(u *unstructured.Unstructured) *HTTPRouteView {
	rv, ns := view(u, vParentRefs|vParents), u.GetNamespace()
	v := &HTTPRouteView{
		Hostnames:   stringList(field(u, "spec", "hostnames")),
		ParentRefs:  parentRefs(rv, ns),
		WrittenRefs: writtenRefs(rv),
		Parents:     routeParents(rv, ns),
		Labels:      u.GetLabels(),
	}
	switch strings.ToLower(u.GetAnnotations()[InheritParentMatcherAnnotation]) {
	case "true", "yes", "enabled":
		v.InheritParentMatcher = true
	}
	if rules := list(field(u, "spec", "rules")); rules != nil {
		v.Rules = make([]Rule, 0, len(rules))
		for i, r := range rules {
			v.Rules = append(v.Rules, decodeRule(i, r, ns))
		}
	}
	return v
}

// decodeRule decodes one rule. Its lists decode entry by entry, so one
// malformed entry is a zero value and the rest still decode.
func decodeRule(i int, item any, ns string) Rule {
	r := Rule{Index: i}
	m, ok := item.(map[string]any)
	if !ok {
		return r
	}
	r.Name, _ = m["name"].(string)
	if ms, ok := m["matches"].([]any); ok {
		r.Matches = make([]Match, 0, len(ms))
		for _, x := range ms {
			r.Matches = append(r.Matches, decodeMatch(x))
		}
	}
	r.Filters = decodeFilters(m["filters"], ns)
	for _, b := range list(m["backendRefs"]) {
		r.BackendRefs = append(r.BackendRefs, decodeBackendRef(b, ns))
	}
	return r
}

type rawKeyMatch struct {
	Type  *string `json:"type"`
	Name  string  `json:"name"`
	Value string  `json:"value"`
}

type rawMatch struct {
	Path *struct {
		Type  *string `json:"type"`
		Value *string `json:"value"`
	} `json:"path"`
	Headers     []rawKeyMatch `json:"headers"`
	QueryParams []rawKeyMatch `json:"queryParams"`
	Method      string        `json:"method"`
}

func keyMatches(in []rawKeyMatch) []KeyMatch {
	if in == nil {
		return nil
	}
	out := make([]KeyMatch, 0, len(in))
	for _, k := range in {
		out = append(out, KeyMatch{Type: orDefault(k.Type, "Exact"), Name: k.Name, Value: k.Value})
	}
	return out
}

func decodeMatch(item any) Match {
	var r rawMatch
	if !decodeInto(item, &r) {
		return Match{}
	}
	m := Match{Headers: keyMatches(r.Headers), QueryParams: keyMatches(r.QueryParams), Method: r.Method}
	// The CRD defaults an omitted path to PathPrefix "/".
	m.Path = &PathMatch{Type: "PathPrefix", Value: "/"}
	if r.Path != nil {
		m.Path = &PathMatch{Type: orDefault(r.Path.Type, "PathPrefix"), Value: orDefault(r.Path.Value, "/")}
	}
	return m
}

type rawFilter struct {
	Type         string `json:"type"`
	ExtensionRef *struct {
		Group string `json:"group"`
		Kind  string `json:"kind"`
		Name  string `json:"name"`
	} `json:"extensionRef"`
	RequestMirror *struct {
		BackendRef rawObjRef `json:"backendRef"`
		Percent    *int32    `json:"percent"`
		Fraction   *struct {
			Numerator   int32  `json:"numerator"`
			Denominator *int32 `json:"denominator"`
		} `json:"fraction"`
	} `json:"requestMirror"`
}

// decodeFilters keeps the order as written. Config is a copy of the filter
// as written, so callers never alias the cached object.
func decodeFilters(v any, ns string) []Filter {
	items := list(v)
	if items == nil {
		return nil
	}
	out := make([]Filter, 0, len(items))
	for _, item := range items {
		var r rawFilter
		if !decodeInto(item, &r) {
			out = append(out, Filter{})
			continue
		}
		f := Filter{Type: r.Type, Config: copyValue(item).(map[string]any)}
		if e := r.ExtensionRef; e != nil {
			f.ExtensionRef = &Ref{Group: e.Group, Kind: e.Kind, Namespace: ns, Name: e.Name}
		}
		if m := r.RequestMirror; m != nil {
			f.Mirror = &MirrorFilter{Backend: m.BackendRef.ref("Service", ns), Port: deref(m.BackendRef.Port), Percent: m.Percent}
			if m.Fraction != nil {
				d := int32(100)
				if m.Fraction.Denominator != nil {
					d = *m.Fraction.Denominator
				}
				f.Mirror.Fraction = &Fraction{Numerator: m.Fraction.Numerator, Denominator: d}
			}
		}
		out = append(out, f)
	}
	return out
}

type rawBackendRef struct {
	rawObjRef `json:",inline"`
	Weight    *int32 `json:"weight"`
}

func decodeBackendRef(item any, ns string) BackendRef {
	var r rawBackendRef
	if !decodeInto(item, &r) {
		return BackendRef{}
	}
	b := BackendRef{Ref: r.ref("Service", ns), Port: deref(r.Port), Weight: 1, WeightDefaulted: true, Delegation: delegation(r.rawObjRef, ns)}
	if r.Weight != nil {
		b.Weight, b.WeightDefaulted = *r.Weight, false
	}
	b.Filters = decodeFilters(item.(map[string]any)["filters"], ns)
	return b
}

// delegation classifies r as kgateway does (computed-rules §3.2): group and
// kind must both be written; anything else is an ordinary backend. The
// namespace follows kgateway's strOr: a written namespace wins, even "".
func delegation(r rawObjRef, ns string) *DelegationRef {
	if r.Group == nil || r.Kind == nil {
		return nil
	}
	if r.Namespace != nil {
		ns = *r.Namespace
	}
	switch {
	case *r.Group == GatewayAPIGroup && *r.Kind == "HTTPRoute":
		if r.Name == "" || r.Name == "*" {
			return &DelegationRef{Mode: DelegationWildcard, Namespace: ns}
		}
		return &DelegationRef{Mode: DelegationName, Namespace: ns, Name: r.Name}
	case *r.Group == delegationLabelGroup && *r.Kind == delegationLabelKind:
		return &DelegationRef{Mode: DelegationLabel, Namespace: ns, Label: r.Name}
	}
	return nil
}

// BackendTargets returns every non-delegation backendRef and every
// RequestMirror target of v, in the order written. A delegation ref's
// filters become policies on its children and send no traffic, so they are skipped.
func BackendTargets(v *HTTPRouteView) []BackendTarget {
	var out []BackendTarget
	mirrors := func(fs []Filter) {
		for _, f := range fs {
			if f.Mirror != nil {
				out = append(out, BackendTarget{Ref: f.Mirror.Backend, Mirror: true})
			}
		}
	}
	for _, r := range v.Rules {
		mirrors(r.Filters)
		for _, b := range r.BackendRefs {
			if b.Delegation == nil {
				out = append(out, BackendTarget{Ref: b.Ref})
				mirrors(b.Filters)
			}
		}
	}
	return out
}
