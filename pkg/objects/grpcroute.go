package objects

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

// GRPCMethodMatch is a GRPCRoute method match; Type defaults to Exact.
type GRPCMethodMatch struct {
	Type, Service, Method string
}

// GRPCMatch is one GRPCRouteMatch.
type GRPCMatch struct {
	Method  *GRPCMethodMatch
	Headers []KeyMatch
}

type rawGRPCMatch struct {
	Method *struct {
		Type    *string `json:"type"`
		Service string  `json:"service"`
		Method  string  `json:"method"`
	} `json:"method"`
	Headers []rawKeyMatch `json:"headers"`
}

func decodeGRPCMatch(item any) GRPCMatch {
	var r rawGRPCMatch
	if !decodeInto(item, &r) {
		return GRPCMatch{}
	}
	m := GRPCMatch{Headers: keyMatches(r.Headers)}
	if r.Method != nil {
		m.Method = &GRPCMethodMatch{Type: orDefault(r.Method.Type, "Exact"), Service: r.Method.Service, Method: r.Method.Method}
	}
	return m
}

// DecodeRouteRules decodes spec.rules of a GRPCRoute, TCPRoute or TLSRoute,
// with backendRefs and filters as for an HTTPRoute. Only a GRPCRoute has
// matches (GRPCMatches; Matches stays nil), and no ref is a delegation. It never fails.
func DecodeRouteRules(u *unstructured.Unstructured) []Rule {
	items, ns := list(field(u, "spec", "rules")), u.GetNamespace()
	if items == nil {
		return nil
	}
	grpc := u.GetKind() == "GRPCRoute"
	out := make([]Rule, 0, len(items))
	for i, item := range items {
		r := Rule{Index: i}
		if m, ok := item.(map[string]any); ok {
			r.Name, _ = m["name"].(string)
			if ms, ok := m["matches"].([]any); ok && grpc {
				r.GRPCMatches = make([]GRPCMatch, 0, len(ms))
				for _, x := range ms {
					r.GRPCMatches = append(r.GRPCMatches, decodeGRPCMatch(x))
				}
			}
			r.Filters = decodeFilters(m["filters"], ns)
			for _, b := range list(m["backendRefs"]) {
				br := decodeBackendRef(b, ns)
				br.Delegation = nil
				r.BackendRefs = append(r.BackendRefs, br)
			}
		}
		out = append(out, r)
	}
	return out
}
