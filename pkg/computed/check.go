package computed

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// CheckKind names what a cross-check compares (rules §9.1).
type CheckKind string

// Check kinds of view A.
const (
	KindListenerCount CheckKind = "listener-count"
	KindRouteAccepted CheckKind = "route-accepted"
)

// Identity is a check's identity: a rule set change gives a new one (rules §9.1).
type Identity struct {
	Install, RuleSet string
	Kind             CheckKind
	Subject          string
}

// Subject joins the subject parts of rules §9.1 with NUL.
func Subject(parts ...string) string { return strings.Join(parts, "\x00") }

// ReportKey is kgateway's status report key: a parentRef as written, with
// empty strings for omitted fields and no port (rules §7).
type ReportKey struct {
	Group, Kind, Namespace, Name, SectionName string
}

// KeyOf is w's report key. An omitted namespace stays empty, so it differs
// from an explicit one; the port is dropped.
func KeyOf(w objects.WrittenRef) ReportKey {
	return ReportKey{Group: w.Group, Kind: w.Kind, Namespace: w.Namespace, Name: w.Name, SectionName: w.SectionName}
}

// Class is a cross-check's evidence class (rules §8.1, §8.3).
type Class string

// Evidence classes.
const (
	ClassContradicting Class = "contradicting"
	ClassStale         Class = "stale"
	ClassMissing       Class = "missing"
	ClassUnexpected    Class = "unexpected"
	ClassCompatible    Class = "compatible"
	ClassNotObservable Class = "not-observable"
	ClassInconclusive  Class = "inconclusive"
	ClassUnknown       Class = "unknown"
)

// noteInconclusive is the note of an inconclusive check (rules §8.1).
const noteInconclusive = "The controller's status cannot confirm this"

// CheckResult is one cross-check of a computed result against status.
type CheckResult struct {
	ID       Identity
	Class    Class
	Verified bool
	Coverage []Coverage
	// Fingerprint hashes the inputs and evidence of rules §9.2.
	Fingerprint uint64
	// On is the object whose status disagrees (rules §9.3 step 5).
	On       objects.Ref
	Listener string
	Key      ReportKey
	Expected string
	Observed string
	// Deps and Contributors are read-only and may be shared.
	Deps          []objects.Ref
	Contributors  []objects.Ref
	ParentCurrent bool
	Note          string
	// State, Mature and Since (the window start) are set by the engine.
	State  State
	Mature bool
	Since  time.Time
}

// Candidate reports whether c can mature into KGWUI-012 (rules §9.3 step 1).
func (c CheckResult) Candidate() bool {
	switch c.Class {
	case ClassContradicting, ClassMissing:
		return true
	case ClassUnexpected:
		return c.Verified
	}
	return false
}

// attachmentReason reports whether reason is written by attachment (rules §8.1).
func attachmentReason(reason string) bool {
	switch Outcome(reason) {
	case OutcomeNotAllowed, OutcomeNoParent, OutcomeNoHostname:
		return true
	}
	return false
}

// checker holds one CheckAttachment call's lookups and scratch space.
type checker struct {
	id   Identity
	note string
	in   *AttachmentInput
	r    *AttachmentResult
	p    *printer
	// resources and routes index the input sorted by Ref.
	resources, routes []int
	// specs caches each resource's listener-spec hash; 0 means not yet computed.
	specs []uint64
	// byResource orders outcomes by resource, then route and parentRef.
	byResource []int
	// outRoute is each outcome's route index, or -1.
	outRoute []int
	sub      *printer
	hashes   []uint64
	names    []string
	group    []int
}

// CheckAttachment cross-checks view A against the controller's status (rules
// §8.1–§8.4): one listener-count check per listener count, and one
// route-accepted check per (route, report key). It never modifies r.
func CheckAttachment(install string, sel Selection, in AttachmentInput, r AttachmentResult) []CheckResult {
	c := &checker{id: Identity{Install: install, RuleSet: sel.Rules.Version()}, note: sel.Note, in: &in, r: &r,
		p: newPrinter(), sub: newPrinter()}
	c.resources = sortedBy(len(in.Resources), func(i int) objects.Ref { return in.Resources[i].Ref })
	c.routes = sortedBy(len(in.Routes), func(i int) objects.Ref { return in.Routes[i].Ref })
	c.specs = make([]uint64, len(in.Resources))
	c.byResource = make([]int, len(r.Outcomes))
	for i := range c.byResource {
		c.byResource[i] = i
	}
	slices.SortFunc(c.byResource, func(a, b int) int {
		return cmp.Or(compareRef(r.Outcomes[a].Resource, r.Outcomes[b].Resource), cmp.Compare(a, b))
	})
	// Outcomes and c.routes are both sorted by route: match them in one pass.
	c.outRoute = make([]int, len(r.Outcomes))
	k := 0
	for i := range r.Outcomes {
		for k < len(c.routes) && compareRef(in.Routes[c.routes[k]].Ref, r.Outcomes[i].Route) < 0 {
			k++
		}
		c.outRoute[i] = -1
		if k < len(c.routes) && in.Routes[c.routes[k]].Ref == r.Outcomes[i].Route {
			c.outRoute[i] = c.routes[k]
		}
	}

	out := make([]CheckResult, 0, len(r.Counts)+len(r.Outcomes))
	out = c.counts(out)
	return c.accepted(out)
}

// sortedBy returns 0..n-1 sorted by ref, then index, so the first of duplicates wins a lookup.
func sortedBy(n int, ref func(int) objects.Ref) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	slices.SortFunc(idx, func(a, b int) int { return cmp.Or(compareRef(ref(a), ref(b)), cmp.Compare(a, b)) })
	return idx
}

// find returns the input index of ref in idx, or -1.
func find(idx []int, ref objects.Ref, at func(int) objects.Ref) int {
	k, ok := slices.BinarySearchFunc(idx, ref, func(i int, t objects.Ref) int { return compareRef(at(i), t) })
	if !ok {
		return -1
	}
	return idx[k]
}

func (c *checker) resource(ref objects.Ref) int {
	return find(c.resources, ref, func(i int) objects.Ref { return c.in.Resources[i].Ref })
}

func (c *checker) result(kind CheckKind, subject string, on objects.Ref) CheckResult {
	id := c.id
	id.Kind, id.Subject = kind, subject
	return CheckResult{ID: id, On: on, Note: c.note}
}

// counts checks each listener count against attachedRoutes. Counts come
// grouped by resource, so candidate hashes are computed once per resource.
func (c *checker) counts(out []CheckResult) []CheckResult {
	counts := c.r.Counts
	for i := 0; i < len(counts); {
		ref := counts[i].Resource
		j := i + 1
		for j < len(counts) && counts[j].Resource == ref {
			j++
		}
		ri := c.resource(ref)
		var res *Resource
		var routes, nsLabels uint64
		if ri >= 0 {
			res = &c.in.Resources[ri]
			routes, nsLabels = c.candidatePrints(ref)
		}
		for k := i; k < j; k++ {
			out = append(out, c.count(&counts[k], res, routes, nsLabels))
		}
		i = j
	}
	return out
}

// count is one listener-count check (rules §8.1, §8.3).
func (c *checker) count(lc *ListenerCount, res *Resource, routes, nsLabels uint64) CheckResult {
	r := lc.Resource
	out := c.result(KindListenerCount, Subject(r.Group, r.Kind, r.Namespace, r.Name, lc.Listener), r)
	out.Listener, out.Deps = lc.Listener, lc.Deps
	if lc.Unknown != "" || res == nil {
		out.Class = ClassUnknown
		return out
	}
	out.Expected = strconv.Itoa(lc.Count)
	var st *objects.ListenerStatus
	for i := range res.ListenerStatus {
		if res.ListenerStatus[i].Name == lc.Listener {
			st = &res.ListenerStatus[i]
			break
		}
	}
	for i := range res.Listeners {
		if res.Listeners[i].Name == lc.Listener {
			out.Fingerprint = listenerCountPrint(c.p, res, &res.Listeners[i], routes, nsLabels, c.in.Experimental, st)
			break
		}
	}
	switch {
	case st == nil:
		// Phase 1's KGWUI-003 owns a missing entry (rules §8.3).
		out.Class, out.Observed = ClassStale, "none"
	case !current(st.Conditions, res.Generation):
		out.Class, out.Observed = ClassStale, strconv.Itoa(int(st.AttachedRoutes))
	case int(st.AttachedRoutes) == lc.Count:
		out.Class, out.Observed, out.Coverage = ClassCompatible, out.Expected, []Coverage{CoverageCount}
	default:
		out.Class, out.Observed = ClassContradicting, strconv.Itoa(int(st.AttachedRoutes))
	}
	return out
}

// current reports whether cs is non-empty and every condition observed gen.
func current(cs []metav1.Condition, gen int64) bool {
	if len(cs) == 0 {
		return false
	}
	for _, x := range cs {
		if x.ObservedGeneration != gen {
			return false
		}
	}
	return true
}

// candidatePrints hashes a resource's candidate routes as a multiset, and
// the labels of their namespaces (rules §9.2), independent of input order.
func (c *checker) candidatePrints(res objects.Ref) (routes, nsLabels uint64) {
	outs := c.r.Outcomes
	lo, _ := slices.BinarySearchFunc(c.byResource, res, func(i int, t objects.Ref) int { return compareRef(outs[i].Resource, t) })
	c.hashes, c.names = c.hashes[:0], c.names[:0]
	for k := lo; k < len(c.byResource) && outs[c.byResource[k]].Resource == res; k++ {
		o := &outs[c.byResource[k]]
		// Outcomes of one route are adjacent; hash each route once.
		if k > lo && outs[c.byResource[k-1]].Route == o.Route {
			continue
		}
		ri := c.outRoute[c.byResource[k]]
		if ri < 0 {
			continue
		}
		rt := &c.in.Routes[ri]
		c.sub.start()
		c.sub.route(rt, res)
		c.hashes = append(c.hashes, c.sub.sum())
		c.names = append(c.names, rt.Ref.Namespace)
	}
	slices.Sort(c.hashes)
	c.sub.start()
	for _, h := range c.hashes {
		c.sub.u64(h)
	}
	routes = c.sub.sum()

	slices.Sort(c.names)
	c.names = slices.Compact(c.names)
	c.sub.start()
	c.sub.flag(c.in.NamespacesOK)
	for _, ns := range c.names {
		l, ok := c.in.NamespaceLabels[ns]
		c.sub.str(ns)
		c.sub.flag(ok)
		c.sub.labels(l)
	}
	return routes, c.sub.sum()
}

// listenerSpecs hashes resource ri's listener specs, once per call.
func (c *checker) listenerSpecs(ri int) uint64 {
	if c.specs[ri] == 0 {
		c.sub.start()
		ls := c.in.Resources[ri].Listeners
		for i := range ls {
			c.sub.listener(&ls[i])
		}
		c.specs[ri] = c.sub.sum() | 1 // never 0, which marks "not computed"
	}
	return c.specs[ri]
}

// accepted checks each (route, report key) against the route's Accepted
// condition. Outcomes come sorted by route, then parentRef.
func (c *checker) accepted(out []CheckResult) []CheckResult {
	outs := c.r.Outcomes
	for i := 0; i < len(outs); {
		ref := outs[i].Route
		j := i + 1
		for j < len(outs) && outs[j].Route == ref {
			j++
		}
		ri := c.outRoute[i]
		if ri < 0 {
			i = j
			continue
		}
		rt := &c.in.Routes[ri]
		// Group the route's refs by report key, keeping parentRef order within a key.
		c.group = c.group[:0]
		for k := i; k < j; k++ {
			c.group = append(c.group, k)
		}
		slices.SortStableFunc(c.group, func(a, b int) int {
			return compareKey(c.key(rt, outs[a].ParentRef), c.key(rt, outs[b].ParentRef))
		})
		for a := 0; a < len(c.group); {
			key := c.key(rt, outs[c.group[a]].ParentRef)
			b := a + 1
			for b < len(c.group) && c.key(rt, outs[c.group[b]].ParentRef) == key {
				b++
			}
			out = append(out, c.routeAccepted(rt, key, c.group[a:b]))
			a = b
		}
		i = j
	}
	return out
}

// key is the report key of rt's parentRef p; a route without written refs has empty keys.
func (c *checker) key(rt *Route, p int) ReportKey {
	if p < len(rt.Written) {
		return KeyOf(rt.Written[p])
	}
	return ReportKey{}
}

// routeAccepted is one route-accepted check over the outcomes (indices) that share key (rules §8.2).
func (c *checker) routeAccepted(rt *Route, key ReportKey, group []int) CheckResult {
	r := rt.Ref
	out := c.result(KindRouteAccepted, Subject(r.Group, r.Kind, r.Namespace, r.Name,
		key.Group, key.Kind, key.Namespace, key.Name, key.SectionName), r)
	out.Key = key
	first := &c.r.Outcomes[group[0]]
	out.Deps = c.deps(group)
	acc := c.acceptedCondition(rt, key)
	if ri := c.resource(first.Resource); ri >= 0 {
		res := &c.in.Resources[ri]
		selector := slices.ContainsFunc(res.Listeners, func(l objects.Listener) bool { return l.AllowedRoutes.Namespaces.From == "Selector" })
		labels, ok := c.in.NamespaceLabels[r.Namespace]
		out.Fingerprint = routeAcceptedPrint(c.p, rt, first.Resource, c.listenerSpecs(ri), selector, c.in.NamespacesOK && ok, labels, acc)
	}

	same := true
	for _, g := range group {
		o := &c.r.Outcomes[g]
		if o.Unknown != "" {
			out.Class = ClassUnknown
			return out
		}
		same = same && o.Outcome == first.Outcome
	}
	if !same {
		// Which write survived depends on order (rules §8.1).
		return c.inconclusive(out)
	}
	out.Expected = string(first.Outcome)
	switch {
	case acc == nil:
		// A missing top-level entry is Phase 1's KGWUI-003 (rules §8.3).
		out.Class, out.Observed = ClassStale, "none"
		return out
	case acc.Status == metav1.ConditionFalse:
		out.Observed = "False/" + acc.Reason
	default:
		out.Observed = string(acc.Status)
	}
	rejected := acc.Status == metav1.ConditionFalse
	switch {
	case acc.ObservedGeneration != rt.Generation:
		out.Class = ClassStale
	case first.Outcome == OutcomeAccepted && (!rejected || !attachmentReason(acc.Reason)):
		// A later stage's reason is compatible with an attachment (rules §8.1).
		out.Class, out.Coverage = ClassCompatible, []Coverage{CoverageStatus}
	case first.Outcome == OutcomeAccepted:
		out.Class = ClassContradicting
	case rejected && acc.Reason == string(first.Outcome):
		out.Class, out.Coverage = ClassCompatible, []Coverage{CoverageStatus}
	case rejected && !attachmentReason(acc.Reason):
		// A later stage may have overwritten the attachment reason (rules §8.1).
		return c.inconclusive(out)
	default:
		out.Class = ClassContradicting
	}
	return out
}

func (c *checker) inconclusive(out CheckResult) CheckResult {
	out.Class, out.Coverage = ClassInconclusive, []Coverage{CoverageNone}
	out.Note = noteInconclusive
	if c.note != "" {
		out.Note += ". " + c.note
	}
	return out
}

// acceptedCondition is this controller's Accepted condition under key, or nil.
func (c *checker) acceptedCondition(rt *Route, key ReportKey) *metav1.Condition {
	for i := range rt.Parents {
		p := &rt.Parents[i]
		if KeyOf(p.Written) == key && slices.Contains(c.in.ControllerNames, p.ControllerName) {
			return objects.FindCondition(p.Conditions, "Accepted")
		}
	}
	return nil
}

// deps shares the outcomes' Deps when they are equal, and merges a copy otherwise.
func (c *checker) deps(group []int) []objects.Ref {
	first := c.r.Outcomes[group[0]].Deps
	n := len(first)
	equal := true
	for _, g := range group[1:] {
		d := c.r.Outcomes[g].Deps
		n += len(d)
		equal = equal && slices.Equal(d, first)
	}
	if equal {
		return first
	}
	merged := make([]objects.Ref, 0, n)
	for _, g := range group {
		merged = append(merged, c.r.Outcomes[g].Deps...)
	}
	slices.SortFunc(merged, compareRef)
	return slices.Clip(slices.Compact(merged))
}

func compareKey(a, b ReportKey) int {
	return cmp.Or(cmp.Compare(a.Group, b.Group), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Namespace, b.Namespace),
		cmp.Compare(a.Name, b.Name), cmp.Compare(a.SectionName, b.SectionName))
}

func compareRef(a, b objects.Ref) int {
	return cmp.Or(cmp.Compare(a.Group, b.Group), cmp.Compare(a.Kind, b.Kind),
		cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
}
