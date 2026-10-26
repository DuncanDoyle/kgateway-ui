package kgw23

import (
	"path"
	"slices"
	"strings"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// defaultMatch is the CRD default of an omitted matches list (rules §3.2).
func defaultMatch() objects.Match {
	return objects.Match{Path: &objects.PathMatch{Type: "PathPrefix", Value: "/"}}
}

// viewC computes the effective rules of child under one parent match (rules
// §6; filterDelegatedChildren). It returns every rule, kept or dropped, and
// whether any rule was kept, which keeps the child.
func viewC(child *objects.HTTPRouteView, parent objects.Match) (rules []computed.EffectiveRule, kept bool) {
	inherit := child.InheritParentMatcher
	for _, r := range child.Rules {
		eff := computed.EffectiveRule{Index: r.Index}
		ms, omitted := r.Matches, r.Matches == nil
		if omitted {
			ms = []objects.Match{defaultMatch()}
		}
		// Inherit mode takes the parent's match for an explicit empty list.
		if inherit && len(ms) == 0 {
			eff.Matches = append(eff.Matches, cloneMatch(parent))
		}
		// The reason is the first failing match's clause, built only for a dropped rule.
		var why computed.FitFailure
		failed := false
		for i, m := range ms {
			if inherit {
				eff.Matches = append(eff.Matches, merge(parent, m))
				continue
			}
			ok, clause, pv, cv := fit(parent, m)
			if ok {
				eff.Matches = append(eff.Matches, cloneMatch(m))
				continue
			}
			if !failed {
				if omitted && clause == computed.ClausePathPrefix {
					clause = computed.ClauseDefaultSlash
				}
				why, failed = computed.FitFailure{Rule: r.Index, Match: i, Clause: clause, Parent: pv, Child: cv}, true
			}
		}
		if len(ms) == 0 && !inherit {
			why, failed = computed.FitFailure{Rule: r.Index, Match: -1, Clause: computed.ClauseNoMatches, Parent: pathValue(parent)}, true
		}
		switch {
		case len(eff.Matches) > 0:
			eff.Kept, kept = true, true
		case failed:
			reason := why // only a dropped rule's reason escapes
			eff.Reason = &reason
		}
		rules = append(rules, eff)
	}
	return rules, kept
}

// fits reports whether child is a valid delegatee match under parent in
// default mode, and otherwise the clause that failed (rules §6;
// IsDelegatedRouteMatch).
func fits(parent, child objects.Match) (bool, computed.FitClause) {
	ok, clause, _, _ := fit(parent, child)
	return ok, clause
}

// fit is fits with the values the failing clause compared.
func fit(parent, child objects.Match) (ok bool, clause computed.FitClause, pv, cv string) {
	if parent.Path == nil || parent.Path.Type != "PathPrefix" {
		return false, computed.ClausePathType, pathType(parent), pathValue(child)
	}
	// Only a missing child path fails here; its type is not checked.
	if child.Path == nil || !strings.HasPrefix(child.Path.Value, parent.Path.Value) {
		return false, computed.ClausePathPrefix, parent.Path.Value, pathValue(child)
	}
	if k, ok := missing(parent.Headers, child.Headers); !ok {
		return false, computed.ClauseHeader, keyString(k), keyString(named(child.Headers, k.Name))
	}
	if k, ok := missing(parent.QueryParams, child.QueryParams); !ok {
		return false, computed.ClauseQuery, keyString(k), keyString(named(child.QueryParams, k.Name))
	}
	if parent.Method != "" && parent.Method != child.Method {
		return false, computed.ClauseMethod, parent.Method, child.Method
	}
	return true, "", "", ""
}

// missing returns the first parent entry the child lacks, compared on name,
// type and value as kgateway's reflect.DeepEqual does.
func missing(parent, child []objects.KeyMatch) (objects.KeyMatch, bool) {
	for _, p := range parent {
		if !slices.Contains(child, p) {
			return p, false
		}
	}
	return objects.KeyMatch{}, true
}

// named returns the first entry called name, or the zero value.
func named(ks []objects.KeyMatch, name string) objects.KeyMatch {
	for _, k := range ks {
		if k.Name == name {
			return k
		}
	}
	return objects.KeyMatch{}
}

// keyString renders a header or query match as name=value, with its type
// when it is not Exact. The zero value renders as "".
func keyString(k objects.KeyMatch) string {
	if k == (objects.KeyMatch{}) {
		return ""
	}
	s := k.Name + "=" + k.Value
	if k.Type != "Exact" {
		s += " (" + k.Type + ")"
	}
	return s
}

// pathType is used for the parent only: kgateway never checks the child's type.
func pathType(m objects.Match) string {
	if m.Path == nil {
		return ""
	}
	return m.Path.Type
}

func pathValue(m objects.Match) string {
	if m.Path == nil {
		return ""
	}
	return m.Path.Value
}

// merge merges parent into child in inherit mode (rules §6;
// mergeParentChildRouteMatch). The parent's path type is not checked.
func merge(parent, child objects.Match) objects.Match {
	out := cloneMatch(child)
	// A nil child path counts as Prefix "".
	if out.Path == nil {
		out.Path = &objects.PathMatch{Type: "PathPrefix"}
	}
	// kgateway dereferences the parent path unchecked; a nil one is never decoded.
	out.Path.Value = path.Join(pathValue(parent), out.Path.Value)
	out.Headers = mergeKeys(parent.Headers, child.Headers)
	out.QueryParams = mergeKeys(parent.QueryParams, child.QueryParams)
	if parent.Method != "" {
		out.Method = parent.Method
	}
	return out
}

// mergeKeys is the union by name with the parent winning, sorted by name
// (mergeHeaders, mergeQueries). As upstream's map does, the last parent
// entry of a name wins and the first child entry of a name wins.
func mergeKeys(parent, child []objects.KeyMatch) []objects.KeyMatch {
	merged := map[string]objects.KeyMatch{}
	for _, k := range parent {
		merged[k.Name] = k
	}
	for _, k := range child {
		if _, ok := merged[k.Name]; !ok {
			merged[k.Name] = k
		}
	}
	var out []objects.KeyMatch
	for _, k := range merged {
		out = append(out, k)
	}
	slices.SortFunc(out, func(a, b objects.KeyMatch) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// cloneMatch copies m so effective matches never alias the decoded view.
func cloneMatch(m objects.Match) objects.Match {
	if m.Path != nil {
		p := *m.Path
		m.Path = &p
	}
	m.Headers = slices.Clone(m.Headers)
	m.QueryParams = slices.Clone(m.QueryParams)
	return m
}
