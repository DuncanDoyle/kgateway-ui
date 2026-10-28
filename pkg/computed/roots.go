package computed

import (
	"slices"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// StatusTarget finds a route's status root (rules §5.1): the target and report
// key, as written, of its first current Accepted=True entry from the install's
// controllers for one of its Gateway or ListenerSet parents.
type StatusTarget func(r DelegRoute) (objects.Ref, ReportKey, bool)

// Roots takes the roots of views B and C from view A att (rules §5.1, §5.6).
// A top-level route is a root when an outcome is accepted. Otherwise, with
// every outcome unknown, a status entry found by status makes it a status
// root. Any other route with an unknown outcome is an unknown root; one with
// only known rejections is not a root (rules §5.1, §8.5).
func Roots(routes []DelegRoute, att AttachmentResult, status StatusTarget) []Root {
	// View A sorts its outcomes by route; each route's are then a sub-slice, in order.
	byRoute := func(a, b RefOutcome) int { return compareRef(a.Route, b.Route) }
	all := att.Outcomes
	if !slices.IsSortedFunc(all, byRoute) {
		all = slices.SortedStableFunc(slices.Values(all), byRoute)
	}
	top := 0
	for _, r := range routes {
		if r.TopLevel {
			top++
		}
	}
	out := make([]Root, 0, top)
	for _, r := range routes {
		if !r.TopLevel {
			continue
		}
		lo, _ := slices.BinarySearchFunc(all, r.Ref, func(o RefOutcome, t objects.Ref) int { return compareRef(o.Route, t) })
		hi := lo
		for hi < len(all) && all[hi].Route == r.Ref {
			hi++
		}
		outs := all[lo:hi:hi]
		if len(outs) == 0 {
			continue
		}
		var deps []objects.Ref
		addDeps := func(ds ...objects.Ref) {
			for _, d := range ds {
				if !slices.Contains(deps, d) {
					deps = append(deps, d)
				}
			}
		}
		accepted, allUnknown, anyUnknown := false, true, false
		for _, o := range outs {
			if o.Unknown == "" {
				allUnknown = false
			} else {
				anyUnknown = true
			}
			if o.Outcome == OutcomeAccepted && o.Unknown == "" {
				accepted = true
				addDeps(o.Deps...)
			}
		}
		switch {
		case accepted:
			// The union over every outcome, not only the accepted ones (rules §5.6).
			for _, o := range outs {
				addDeps(o.Deps...)
			}
			keys := refKeys(r.View, outs, func(o RefOutcome) bool { return o.Outcome == OutcomeAccepted && o.Unknown == "" }, nil)
			out = append(out, Root{Route: r.Ref, Deps: deps, Keys: keys,
				UncertainKeys: refKeys(r.View, outs, func(o RefOutcome) bool { return o.Unknown != "" }, keys)})
		case !anyUnknown:
		default:
			root := Root{Route: r.Ref, Unknown: true, Deps: []objects.Ref{r.Ref}}
			if allUnknown {
				if target, key, ok := status(r); ok {
					keys := []ReportKey{key}
					root = Root{Route: r.Ref, FromStatus: true, Deps: []objects.Ref{r.Ref, target}, Keys: keys,
						UncertainKeys: refKeys(r.View, outs, func(RefOutcome) bool { return true }, keys)}
				}
			}
			out = append(out, root)
		}
	}
	if len(out) == 0 {
		return nil // no roots is nil, not empty
	}
	return out
}

// refKeys are the report keys, as written, of the parentRefs whose outcomes
// pass keep, in spec order, without repeats and without those in skip.
// Rejected parentRefs and other controllers' Gateways (no outcome) get none.
func refKeys(v *objects.HTTPRouteView, outs []RefOutcome, keep func(RefOutcome) bool, skip []ReportKey) []ReportKey {
	var idx []int
	for _, o := range outs {
		if keep(o) && o.ParentRef < len(v.WrittenRefs) {
			idx = append(idx, o.ParentRef)
		}
	}
	slices.Sort(idx)
	var out []ReportKey
	for _, i := range idx {
		if k := KeyOf(v.WrittenRefs[i]); !slices.Contains(out, k) && !slices.Contains(skip, k) {
			out = append(out, k)
		}
	}
	return out
}
