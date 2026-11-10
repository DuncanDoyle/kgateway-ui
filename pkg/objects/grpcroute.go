package objects

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

// decodeRouteRules decodes spec.rules of a GRPCRoute, TCPRoute or TLSRoute.
// Only a GRPCRoute has matches; none of them delegates.
func decodeRouteRules(kind string, rules []any, ns string) []Rule {
	if rules == nil {
		return nil
	}
	out := make([]Rule, 0, len(rules))
	for i, item := range rules {
		r := decodeRule(i, item, ns)
		r.Matches = nil
		for j := range r.BackendRefs {
			r.BackendRefs[j].Delegation = nil
		}
		if m, ok := item.(map[string]any); ok && kind == "GRPCRoute" {
			if ms, ok := m["matches"].([]any); ok {
				r.GRPCMatches = make([]GRPCMatch, 0, len(ms))
				for _, x := range ms {
					r.GRPCMatches = append(r.GRPCMatches, decodeGRPCMatch(x))
				}
			}
		}
		out = append(out, r)
	}
	return out
}
