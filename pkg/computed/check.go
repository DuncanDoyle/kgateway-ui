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

// Check kinds of views B and C.
const (
	KindChildEntry         CheckKind = "child-entry"
	KindParentResolvedRefs CheckKind = "parent-resolvedrefs"
)

// Trigger kinds of KGWUI-011 (rules §9.1): the computed trigger per (route,
// parent) and the orphan trigger per route.
const (
	KindDelegatedNotAttached CheckKind = "delegated-not-attached"
	KindOrphan               CheckKind = "orphan-not-attached"
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
	// ClassTrigger is a KGWUI-011 trigger whose predicate holds (our call:
	// rules §8.3 gives it no class). It matures but never makes a check pending.
	ClassTrigger Class = "trigger"
)

// noteInconclusive is the note of an inconclusive check (rules §8.1).
const noteInconclusive = "The controller's status cannot confirm this"

// CheckResult is one cross-check of a computed result against status.
type CheckResult struct {
	ID    Identity
	Class Class
	// Unknown says why a ClassUnknown check could not be made, when a reason applies.
	Unknown  UnknownReason
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

// Parent is the parent route of a child-entry or delegated-not-attached
// check, read from its subject; ok is false for other kinds.
func (c CheckResult) Parent() (objects.Ref, bool) {
	if c.ID.Kind != KindChildEntry && c.ID.Kind != KindDelegatedNotAttached {
		return objects.Ref{}, false
	}
	// Subject: child group, kind, namespace, name, then the parent's.
	p := strings.SplitN(c.ID.Subject, "\x00", 9)
	if len(p) < 8 {
		return objects.Ref{}, false
	}
	return objects.Ref{Group: p[4], Kind: p[5], Namespace: p[6], Name: p[7]}, true
}

// Candidate reports whether c can mature into KGWUI-012 (rules §9.3 step 1).
func (c CheckResult) Candidate() bool {
	switch c.Class {
	case ClassContradicting, ClassMissing, ClassTrigger:
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
	// A count is a fact of its listener, shown to whoever sees it (design §7.4 rule 5): no Deps.
	out.Listener = lc.Listener
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

func compareRef(a, b objects.Ref) int { return a.Compare(b) }

// noteIncomplete is the note of a negative check on an incomplete aggregate (rules §8.5).
const noteIncomplete = "Not all paths computed"

// Child entry expectations, as CheckResult.Expected shows them (rules §8.2).
const (
	expectEntry    = "entry"
	expectRejected = "False/" + reasonUnsupportedValue
	expectNone     = "none"
)

// reasonUnsupportedValue is the Accepted reason of a hostname-rejected child (rules §8.1).
const reasonUnsupportedValue = "UnsupportedValue"

// delegChecker holds one CheckDelegation call's lookups and scratch space.
type delegChecker struct {
	id   Identity
	note string
	in   *DelegationInput
	r    *DelegationResult
	p    *printer
	sub  *printer
	// byChild are the child rows' indexes, by (child, parent), then row order.
	byChild []int
	// kids caches each input route's child-input hash; 0 means not yet computed.
	kids []uint64
	keys []ReportKey
	idx  []int
	// seen are the (parent, key) pairs of one route's leftover entries.
	seen []RouteKey
	// hashes and parentsSeen are one orphan's per-parent prints.
	hashes      []uint64
	parentsSeen []objects.Ref
}

// CheckDelegation cross-checks views B and C against the controller's status
// (rules §8.1–§8.5): one child-entry check per (child, parent, report key),
// including entries no row expects, and one parent-resolvedrefs check per
// (parent, reporter key). Completeness comes from r.Complete. It sets each
// row's Unconfirmable and otherwise never modifies r.
func CheckDelegation(install string, sel Selection, in DelegationInput, r *DelegationResult) []CheckResult {
	c := &delegChecker{id: Identity{Install: install, RuleSet: sel.Rules.Version()}, note: sel.Note, in: &in, r: r,
		p: newPrinter(), sub: newPrinter(), kids: make([]uint64, len(in.Routes))}
	rows := r.Rows
	c.byChild = make([]int, 0, len(rows))
	for i := range rows {
		if rows[i].Key.Child != (objects.Ref{}) {
			c.byChild = append(c.byChild, i)
		}
	}
	slices.SortFunc(c.byChild, func(a, b int) int {
		return cmp.Or(rows[a].Key.Child.Compare(rows[b].Key.Child), rows[a].Key.Parent.Compare(rows[b].Key.Parent), cmp.Compare(a, b))
	})
	out := make([]CheckResult, 0, len(c.byChild)+len(r.Signals))
	out = c.childEntries(out)
	out = c.leftEntries(out)
	out = c.parents(out)
	if in.ScopeUnknown {
		return out // no trigger while the scope is unknown (D-024)
	}
	return c.triggers(out)
}

func (c *delegChecker) result(kind CheckKind, subject string, on objects.Ref) CheckResult {
	id := c.id
	id.Kind, id.Subject = kind, subject
	return CheckResult{ID: id, On: on, Note: c.note}
}

// withNote sets out's note to n, followed by the selection note.
func (c *delegChecker) withNote(out *CheckResult, n string) {
	out.Note = n
	if c.note != "" {
		out.Note += ". " + c.note
	}
}

// route is ref's index in the input routes, or -1.
func (c *delegChecker) route(ref objects.Ref) int {
	i, ok := slices.BinarySearchFunc(c.in.Routes, ref, func(r DelegRoute, t objects.Ref) int { return r.Ref.Compare(t) })
	if !ok {
		return -1
	}
	return i
}

// entry is this controller's status entry of route ri under key, or nil.
func (c *delegChecker) entry(ri int, key ReportKey) *objects.RouteParentStatus {
	if ri < 0 {
		return nil
	}
	ps := c.in.Routes[ri].View.Parents
	for i := range ps {
		if KeyOf(ps[i].Written) == key && slices.Contains(c.in.ControllerNames, ps[i].ControllerName) {
			return &ps[i]
		}
	}
	return nil
}

// childAgg is a child-entry aggregate over the rows of one (child, parent).
type childAgg struct {
	// positive: some context attached or rejected; rejected: the positive ones were rejected.
	positive, rejected, forbidden bool
	// incomplete: a row is unknown or not fully computed, or the child is in a reach closure.
	incomplete bool
	digest     uint64
}

func (c *delegChecker) aggregate(group []int, child objects.Ref, key ReportKey) childAgg {
	var a childAgg
	for _, g := range group {
		row := &c.r.Rows[g]
		switch row.Outcome {
		case OutcomeAttached, OutcomePartial:
			a.positive = true
		case OutcomeRejectedHostnames:
			a.positive, a.rejected = true, true
		case OutcomeForbidden:
			a.forbidden = true
		case OutcomeUnknown:
			a.incomplete = true
		}
		a.incomplete = a.incomplete || row.SomeNotComputed
		a.digest += row.ContextDigest // wrapping sum, as the rows' own
	}
	a.incomplete = a.incomplete || c.r.Complete.IncompleteKey(child, key)
	return a
}

// childEntries checks each (child, parent) of the rows against the child's
// entry under its report key (rules §8.2 child-entry).
func (c *delegChecker) childEntries(out []CheckResult) []CheckResult {
	rows := c.r.Rows
	for i := 0; i < len(c.byChild); {
		k := rows[c.byChild[i]].Key
		j := i + 1
		for j < len(c.byChild) && rows[c.byChild[j]].Key.Child == k.Child && rows[c.byChild[j]].Key.Parent == k.Parent {
			j++
		}
		out = append(out, c.childEntry(c.byChild[i:j]))
		i = j
	}
	return out
}

// childEntry is one child-entry check over the rows (indices) of one (child, parent).
func (c *delegChecker) childEntry(group []int) CheckResult {
	first := &c.r.Rows[group[0]]
	child, parent, key := first.Key.Child, first.Key.Parent, first.ChildKey
	out := c.childResult(child, parent, key, group)
	ci := c.route(child)
	agg := c.aggregate(group, child, key)
	// The entry that confirms a positive row cannot confirm a sibling's drop (rules §8.2).
	for _, g := range group {
		o := c.r.Rows[g].Outcome
		c.r.Rows[g].Unconfirmable = agg.positive && (o == OutcomeDropped || o == OutcomeCycle)
	}
	out.ParentCurrent = c.parentCurrent(parent, group)
	if c.in.ScopeUnknown {
		out.Class, out.Unknown = ClassUnknown, UnknownScope
		return out
	}
	e := c.entry(ci, key)
	out.Fingerprint = childEntryPrint(c.p, c.childInputs(ci), c.rowRefs(group, false), agg.digest, c.in.Wildcard, agg.incomplete, e)
	switch {
	case !first.WritesEntry && !agg.forbidden:
		// 2.3 writes no entry under this key (rules §7, §11.3).
		out.Class, out.Coverage = ClassNotObservable, []Coverage{CoverageNone}
		return out
	case !agg.positive:
		if agg.incomplete {
			out.Expected = expectNone
			return c.unknownIncomplete(out)
		}
		return c.leftover(out, ci, e)
	}
	out.Expected = expectEntry
	if agg.rejected {
		out.Expected = expectRejected
	}
	if e == nil {
		out.Class, out.Observed = ClassMissing, expectNone
		return out
	}
	acc, stale := c.observeChild(&out, ci, e)
	if stale {
		out.Class = ClassStale
		return out
	}
	unsupported := acc != nil && acc.Status == metav1.ConditionFalse && acc.Reason == reasonUnsupportedValue
	switch {
	case agg.rejected == unsupported:
		// An attachment's later stages may write any other Accepted (rules §8.1).
		out.Class, out.Coverage = ClassCompatible, []Coverage{CoverageStatus}
	case agg.incomplete:
		// A context not computed could have written the other value (rules §8.5).
		c.inconclusive(&out)
	case agg.rejected && (acc == nil || acc.Status == metav1.ConditionFalse):
		// No Accepted, or another reason: not what the hostname stage writes.
		c.inconclusive(&out)
	default:
		out.Class = ClassContradicting
	}
	return out
}

// childResult starts a child-entry check on child under parent and key, with
// the union of the deps of group's rows, child and parent.
func (c *delegChecker) childResult(child, parent objects.Ref, key ReportKey, group []int) CheckResult {
	out := c.result(KindChildEntry, Subject(child.Group, child.Kind, child.Namespace, child.Name,
		parent.Group, parent.Kind, parent.Namespace, parent.Name,
		key.Group, key.Kind, key.Namespace, key.Name, key.SectionName), child)
	out.Key = key
	out.Deps = c.unionDeps(group, nil, nil, child, parent)
	out.Contributors = routesOf(out.Deps)
	return out
}

func (c *delegChecker) inconclusive(out *CheckResult) {
	out.Class, out.Coverage = ClassInconclusive, []Coverage{CoverageNone}
	c.withNote(out, noteInconclusive)
}

func (c *delegChecker) unknownIncomplete(out CheckResult) CheckResult {
	out.Class = ClassUnknown
	c.withNote(&out, noteIncomplete)
	return out
}

// observeChild sets out.Observed from e and returns its Accepted condition
// and whether e is stale for the child's generation.
func (c *delegChecker) observeChild(out *CheckResult, ci int, e *objects.RouteParentStatus) (*metav1.Condition, bool) {
	acc := objects.FindCondition(e.Conditions, "Accepted")
	switch {
	case acc == nil:
		out.Observed = expectEntry
	case acc.Status == metav1.ConditionFalse:
		out.Observed = "False/" + acc.Reason
	default:
		out.Observed = string(acc.Status)
	}
	return acc, entryGeneration(e, acc) != c.in.Routes[ci].Generation
}

// entryGeneration is the generation e's Accepted condition observed, or its oldest condition's.
func entryGeneration(e *objects.RouteParentStatus, acc *metav1.Condition) int64 {
	if acc != nil {
		return acc.ObservedGeneration
	}
	return minObserved(e.Conditions)
}

// leftover classifies an entry no context expects (rules §8.3): none is
// compatible; a current one is unexpected and verified for every cause of
// the drop or deselection, a forbidden child's included (D-021 amendment).
func (c *delegChecker) leftover(out CheckResult, ci int, e *objects.RouteParentStatus) CheckResult {
	out.Expected = expectNone
	if e == nil {
		out.Class, out.Coverage, out.Observed = ClassCompatible, []Coverage{CoverageStatus}, expectNone
		return out
	}
	if _, stale := c.observeChild(&out, ci, e); stale {
		out.Class = ClassStale
		return out
	}
	out.Class, out.Verified = ClassUnexpected, true
	return out
}

// leftEntries checks this controller's entries on in-scope routes for an
// owned HTTPRoute parent that no row group claims: a deselection (D-021
// item 1) or a key the child no longer reports under.
func (c *delegChecker) leftEntries(out []CheckResult) []CheckResult {
	rows := c.r.Rows
	for ci := range c.in.Routes {
		rt := &c.in.Routes[ci]
		c.seen = c.seen[:0]
		for i := range rt.View.Parents {
			e := &rt.View.Parents[i]
			parent := e.ParentRef.Ref()
			if parent.Group != objects.GatewayAPIGroup || parent.Kind != "HTTPRoute" || !slices.Contains(c.in.ControllerNames, e.ControllerName) {
				continue
			}
			if pi := c.route(parent); pi < 0 || !c.in.Routes[pi].Owned {
				continue // only an owned parent's entries are this install's to explain
			}
			key := KeyOf(e.Written)
			// Entries differing only by port share a key, and so one check (rules §7).
			if slices.Contains(c.seen, RouteKey{Route: parent, Key: key}) {
				continue
			}
			c.seen = append(c.seen, RouteKey{Route: parent, Key: key})
			group := c.group(rt.Ref, parent)
			if len(group) > 0 && rows[group[0]].ChildKey == key {
				continue // checked by childEntry
			}
			o := c.childResult(rt.Ref, parent, key, group)
			if c.in.ScopeUnknown {
				o.Class, o.Unknown = ClassUnknown, UnknownScope
				out = append(out, o)
				continue
			}
			agg := c.aggregate(group, rt.Ref, key)
			o.Fingerprint = childEntryPrint(c.p, c.childInputs(ci), c.rowRefs(group, false), agg.digest, c.in.Wildcard, agg.incomplete, e)
			if agg.incomplete {
				o.Expected = expectNone
				out = append(out, c.unknownIncomplete(o))
				continue
			}
			out = append(out, c.leftover(o, ci, e))
		}
	}
	return out
}

// group is the indices in byChild of the rows of (child, parent).
func (c *delegChecker) group(child, parent objects.Ref) []int {
	rows := c.r.Rows
	lo, _ := slices.BinarySearchFunc(c.byChild, [2]objects.Ref{child, parent}, func(i int, t [2]objects.Ref) int {
		return cmp.Or(rows[i].Key.Child.Compare(t[0]), rows[i].Key.Parent.Compare(t[1]))
	})
	hi := lo
	for hi < len(c.byChild) && rows[c.byChild[hi]].Key.Child == child && rows[c.byChild[hi]].Key.Parent == parent {
		hi++
	}
	return c.byChild[lo:hi]
}

// parentCurrent reports whether parent has a current entry under every
// reporter key of the group's contexts (rules §9.3 step 3).
func (c *delegChecker) parentCurrent(parent objects.Ref, group []int) bool {
	pi := c.route(parent)
	if pi < 0 {
		return false
	}
	seen := false
	for _, g := range group {
		for _, k := range c.r.Rows[g].Reporters {
			e := c.entry(pi, k)
			if e == nil || !current(e.Conditions, c.in.Routes[pi].Generation) {
				return false
			}
			seen = true
		}
	}
	return seen
}

// unionDeps is the sorted union of the rows' Deps, extra and refs; with key,
// the rows' deps under that reporter key only. It shares the rows' deps when
// they are one set holding the others, and is a new slice otherwise; either
// way it is read-only.
func (c *delegChecker) unionDeps(rows []int, key *ReportKey, extra []objects.Ref, refs ...objects.Ref) []objects.Ref {
	of := func(g int) []objects.Ref {
		if row := &c.r.Rows[g]; key != nil {
			if i, ok := slices.BinarySearchFunc(row.Reporters, *key, ReportKey.Compare); ok {
				return row.KeyDeps[i]
			}
			return nil
		}
		return c.r.Rows[g].Deps
	}
	if len(rows) > 0 {
		first := of(rows[0])
		holds := func(r objects.Ref) bool { _, ok := slices.BinarySearchFunc(first, r, compareRef); return ok }
		if !slices.ContainsFunc(rows[1:], func(g int) bool { return !slices.Equal(of(g), first) }) &&
			!slices.ContainsFunc(extra, func(r objects.Ref) bool { return !holds(r) }) &&
			!slices.ContainsFunc(refs, func(r objects.Ref) bool { return !holds(r) }) {
			return first
		}
	}
	n := len(extra) + len(refs)
	for _, g := range rows {
		n += len(of(g))
	}
	out := make([]objects.Ref, 0, n)
	for _, g := range rows {
		out = append(out, of(g)...)
	}
	out = append(append(out, extra...), refs...)
	slices.SortFunc(out, compareRef)
	return slices.Clip(slices.Compact(out))
}

// routesOf is the HTTPRoutes of the sorted deps, a subslice: refs sort by
// group and kind first, so they are one run.
func routesOf(deps []objects.Ref) []objects.Ref {
	route := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute"}
	lo, _ := slices.BinarySearchFunc(deps, route, compareRef)
	hi := lo
	for hi < len(deps) && deps[hi].Group == route.Group && deps[hi].Kind == route.Kind {
		hi++
	}
	return deps[lo:hi:hi]
}

// childInputs is route ci's child-input hash (rules §9.2), once per call; 0 when ci is -1.
func (c *delegChecker) childInputs(ci int) uint64 {
	if ci < 0 {
		return 0
	}
	if c.kids[ci] == 0 {
		c.sub.start()
		c.sub.childRoute(c.in.Routes[ci].View)
		c.kids[ci] = c.sub.sum() | 1 // never 0, which marks "not computed"
	}
	return c.kids[ci]
}

// rowRefs hashes the rows' refs as written, with their indexes and children;
// with keep, also each selected child's inputs and, for a label ref, its
// label: what decides whether the child is kept (rules §9.2).
func (c *delegChecker) rowRefs(rows []int, keep bool) uint64 {
	if keep {
		// childInputs shares c.sub, so fill its cache before encoding.
		for _, g := range rows {
			if ch := c.r.Rows[g].Key.Child; ch != (objects.Ref{}) {
				c.childInputs(c.route(ch))
			}
		}
	}
	s := c.sub
	s.start()
	for _, g := range rows {
		row := &c.r.Rows[g]
		s.num(int64(row.Key.Rule))
		s.num(int64(row.Key.Ref))
		s.delegationRef(row.RefAsWritten)
		s.ref(row.Key.Child)
		if ci := c.route(row.Key.Child); keep && ci >= 0 {
			s.u64(c.kids[ci])
			if row.RefAsWritten.Mode == objects.DelegationLabel {
				l, ok := c.in.Routes[ci].View.Labels[objects.DelegationLabelKey]
				s.flag(ok)
				s.str(l)
			}
		}
	}
	return s.sum()
}

// parents checks each parent's ResolvedRefs under each reporter key of its
// contexts, and under each of a root's uncertain keys (rules §8.2
// parent-resolvedrefs). Rows come sorted by parent.
func (c *delegChecker) parents(out []CheckResult) []CheckResult {
	rows, uk := c.r.Rows, c.r.UncertainKeys
	for i := 0; i < len(rows); {
		parent := rows[i].Key.Parent
		j := i + 1
		for j < len(rows) && rows[j].Key.Parent == parent {
			j++
		}
		c.keys = c.keys[:0]
		for k := i; k < j; k++ {
			for _, key := range rows[k].Reporters {
				c.addKey(key)
			}
		}
		u, _ := slices.BinarySearchFunc(uk, parent, func(rk RouteKey, t objects.Ref) int { return rk.Route.Compare(t) })
		for ; u < len(uk) && uk[u].Route == parent; u++ {
			c.addKey(uk[u].Key)
		}
		for _, key := range c.keys {
			out = append(out, c.parentResolvedRefs(parent, key, i, j))
		}
		i = j
	}
	return out
}

// addKey adds key to the sorted c.keys once.
func (c *delegChecker) addKey(key ReportKey) {
	if i, ok := slices.BinarySearchFunc(c.keys, key, ReportKey.Compare); !ok {
		c.keys = slices.Insert(c.keys, i, key)
	}
}

// parentResolvedRefs is one parent-resolvedrefs check of parent under key,
// over its rows [lo, hi) that have a context under key (rules §8.2).
func (c *delegChecker) parentResolvedRefs(parent objects.Ref, key ReportKey, lo, hi int) CheckResult {
	out := c.result(KindParentResolvedRefs, Subject(parent.Group, parent.Kind, parent.Namespace, parent.Name,
		key.Group, key.Kind, key.Namespace, key.Name, key.SectionName), parent)
	out.Key = key
	var digest uint64
	c.idx = c.idx[:0]
	for k := lo; k < hi; k++ {
		row := &c.r.Rows[k]
		if i, ok := slices.BinarySearchFunc(row.Reporters, key, ReportKey.Compare); ok {
			c.idx = append(c.idx, k)
			digest += row.KeyDigests[i]
		}
	}
	sig := c.signal(parent, key)
	var sigDeps []objects.Ref
	if sig != nil {
		sigDeps = sig.Deps
	}
	out.Deps = c.unionDeps(c.idx, &key, sigDeps, parent)
	out.Contributors = routesOf(out.Deps)
	pi := c.route(parent)
	e := c.entry(pi, key)
	var view *objects.HTTPRouteView
	var gen int64
	if pi >= 0 {
		view, gen = c.in.Routes[pi].View, c.in.Routes[pi].Generation
	}
	out.ParentCurrent = e != nil && current(e.Conditions, gen)
	if c.in.ScopeUnknown {
		out.Class, out.Unknown = ClassUnknown, UnknownScope
		return out
	}
	if c.unwritten(parent, key) {
		// 2.3 never writes the parent's entry under this key (rules §7, §11.3).
		out.Class, out.Coverage = ClassNotObservable, []Coverage{CoverageNone}
		return out
	}
	incomplete := c.r.Complete.IncompleteKey(parent, key)
	plain := view != nil && delegationOnly(view)
	// The reason is compared only when nothing else can have written it (rules §8.2).
	reason := sig != nil && !incomplete && len(sig.Reasons) == 1 && plain
	out.Fingerprint = parentResolvedRefsPrint(c.p, view, digest, c.rowRefs(c.idx, true), c.in.Wildcard, incomplete, e, reason)
	switch {
	case sig == nil && incomplete:
		out.Expected = "True"
		return c.unknownIncomplete(out)
	case sig == nil:
		out.Expected = "True"
	case reason:
		out.Expected = "False/" + sig.Reasons[0]
	default:
		out.Expected = "False"
	}
	var rr *metav1.Condition
	if e != nil {
		rr = objects.FindCondition(e.Conditions, "ResolvedRefs")
	}
	switch {
	case rr == nil:
		// A missing entry is Phase 1's KGWUI-003, or the parent's own child-entry check.
		out.Class, out.Observed = ClassStale, expectNone
		return out
	case rr.Status == metav1.ConditionFalse:
		out.Observed = "False/" + rr.Reason
	default:
		out.Observed = string(rr.Status)
	}
	failed := rr.Status == metav1.ConditionFalse
	switch {
	case rr.ObservedGeneration != gen:
		out.Class = ClassStale
	case sig != nil && !failed, reason && rr.Reason != sig.Reasons[0]:
		out.Class = ClassContradicting
	case sig == nil && failed && plain:
		// Only delegation refs, none of them failed, and the aggregate is complete.
		out.Class = ClassContradicting
	default:
		out.Class, out.Coverage = ClassCompatible, []Coverage{CoverageStatus}
	}
	return out
}

// unwritten reports whether key is route's report key under some parent
// whose status builder never writes it (Row.WritesEntry false).
func (c *delegChecker) unwritten(route objects.Ref, key ReportKey) bool {
	rows := c.r.Rows
	i, _ := slices.BinarySearchFunc(c.byChild, route, func(g int, t objects.Ref) int { return rows[g].Key.Child.Compare(t) })
	for ; i < len(c.byChild) && rows[c.byChild[i]].Key.Child == route; i++ {
		if row := &rows[c.byChild[i]]; row.ChildKey == key && !row.WritesEntry && row.Outcome != OutcomeForbidden {
			return true
		}
	}
	return false
}

// signal is the parent signal of (parent, key), or nil.
func (c *delegChecker) signal(parent objects.Ref, key ReportKey) *ParentSignal {
	i, ok := slices.BinarySearchFunc(c.r.Signals, parent, func(s ParentSignal, t objects.Ref) int { return s.Parent.Compare(t) })
	if !ok {
		return nil
	}
	for ; i < len(c.r.Signals) && c.r.Signals[i].Parent == parent; i++ {
		if c.r.Signals[i].Key == key {
			return &c.r.Signals[i]
		}
	}
	return nil
}

// delegationOnly reports whether every backendRef of v delegates and no
// filter is an ExtensionRef: only delegation can then fail ResolvedRefs (rules §8.2).
func delegationOnly(v *objects.HTTPRouteView) bool {
	for _, r := range v.Rules {
		if slices.ContainsFunc(r.Filters, extensionRef) {
			return false
		}
		for _, b := range r.BackendRefs {
			if b.Delegation == nil || slices.ContainsFunc(b.Filters, extensionRef) {
				return false
			}
		}
	}
	return true
}

func extensionRef(f objects.Filter) bool { return f.Type == "ExtensionRef" || f.ExtensionRef != nil }

// triggers adds KGWUI-011's computed and orphan trigger checks whose
// predicates hold (design §6.2, rules §9.3). Each is ClassTrigger: it matures
// 30 s after its window starts and never makes a check pending.
func (c *delegChecker) triggers(out []CheckResult) []CheckResult {
	rows := c.r.Rows
	for i := 0; i < len(c.byChild); {
		child := rows[c.byChild[i]].Key.Child
		j := i + 1
		for j < len(c.byChild) && rows[c.byChild[j]].Key.Child == child {
			j++
		}
		if ci := c.route(child); ci >= 0 {
			out = c.childTriggers(out, ci, c.byChild[i:j])
		}
		i = j
	}
	return out
}

// childTriggers checks the rows (indices, by parent) that select route ci:
// one computed trigger per (route, parent), or one orphan trigger.
func (c *delegChecker) childTriggers(out []CheckResult, ci int, rows []int) []CheckResult {
	rt := &c.in.Routes[ci]
	if c.r.Complete.Incomplete(rt.Ref) {
		return out
	}
	// An orphan: no parentRefs, no entry, only wildcard refs of owned parents,
	// each dropping or forbidding it in every context, each complete.
	orphan := len(rt.View.ParentRefs) == 0 && !c.anyEntry(ci)
	for a := 0; a < len(rows); {
		parent := c.r.Rows[rows[a]].Key.Parent
		b := a + 1
		for b < len(rows) && c.r.Rows[rows[b]].Key.Parent == parent {
			b++
		}
		group := rows[a:b]
		ok, named, forbidden := c.unkept(parent, group)
		orphan = orphan && ok && !named
		if ok && named && !c.eligible(ci, parent) {
			out = append(out, c.notAttached(ci, parent, group, forbidden))
		}
		a = b
	}
	if orphan {
		out = append(out, c.orphan(ci, rows))
	}
	return out
}

// unkept reports whether owned parent keeps none of group's rows, complete:
// each is dropped or forbidden in every context. named: a name or label ref
// selects the child; forbidden: a row is forbidden.
func (c *delegChecker) unkept(parent objects.Ref, group []int) (ok, named, forbidden bool) {
	if pi := c.route(parent); pi < 0 || !c.in.Routes[pi].Owned || c.r.Complete.Incomplete(parent) {
		return false, false, false
	}
	for _, g := range group {
		row := &c.r.Rows[g]
		switch row.Outcome {
		case OutcomeForbidden:
			forbidden = true
		case OutcomeDropped:
		default:
			return false, false, false
		}
		named = named || row.RefAsWritten.Mode != objects.DelegationWildcard
	}
	return true, named, forbidden
}

// eligible reports whether route ci has an entry from this install's
// controllers for parent that its parentRefs permit (design §4.2).
func (c *delegChecker) eligible(ci int, parent objects.Ref) bool {
	v := c.in.Routes[ci].View
	for i := range v.Parents {
		e := &v.Parents[i]
		if e.ParentRef.Ref() == parent && slices.Contains(c.in.ControllerNames, e.ControllerName) &&
			(len(v.ParentRefs) == 0 || slices.Contains(v.ParentRefs, e.ParentRef)) {
			return true
		}
	}
	return false
}

// anyEntry reports whether route ci has an entry from this install's controllers.
func (c *delegChecker) anyEntry(ci int) bool {
	return slices.ContainsFunc(c.in.Routes[ci].View.Parents, func(e objects.RouteParentStatus) bool {
		return slices.Contains(c.in.ControllerNames, e.ControllerName)
	})
}

// notAttached is the computed trigger of route ci under parent; its
// fingerprint is child-entry's with no entry (rules §9.2).
func (c *delegChecker) notAttached(ci int, parent objects.Ref, group []int, forbidden bool) CheckResult {
	child := c.in.Routes[ci].Ref
	out := c.result(KindDelegatedNotAttached, Subject(child.Group, child.Kind, child.Namespace, child.Name,
		parent.Group, parent.Kind, parent.Namespace, parent.Name), child)
	out.Class, out.Expected, out.Observed = ClassTrigger, string(OutcomeDropped), expectNone
	if forbidden {
		out.Expected = string(OutcomeForbidden)
	}
	out.Deps = c.unionDeps(group, nil, nil, child, parent)
	out.Contributors = routesOf(out.Deps)
	out.Fingerprint = c.unkeptPrint(ci, group)
	return out
}

// unkeptPrint is the child-entry fingerprint of group's rows on route ci, complete and with no entry.
func (c *delegChecker) unkeptPrint(ci int, group []int) uint64 {
	var digest uint64
	for _, g := range group {
		digest += c.r.Rows[g].ContextDigest
	}
	return childEntryPrint(c.p, c.childInputs(ci), c.rowRefs(group, false), digest, c.in.Wildcard, false, nil)
}

// orphan is the orphan trigger of route ci over every selecting row (rules
// §9.2): its parentRefs, each selecting parent and ref, and each parent's
// child-entry inputs.
func (c *delegChecker) orphan(ci int, rows []int) CheckResult {
	child := c.in.Routes[ci].Ref
	out := c.result(KindOrphan, Subject(child.Group, child.Kind, child.Namespace, child.Name), child)
	out.Class, out.Observed = ClassTrigger, expectNone
	out.Deps = c.unionDeps(rows, nil, nil, child)
	out.Contributors = routesOf(out.Deps)
	// childEntryPrint uses c.p, so each parent's print is taken before the total.
	c.hashes, c.parentsSeen = c.hashes[:0], c.parentsSeen[:0]
	for a := 0; a < len(rows); {
		parent := c.r.Rows[rows[a]].Key.Parent
		b := a + 1
		for b < len(rows) && c.r.Rows[rows[b]].Key.Parent == parent {
			b++
		}
		c.hashes = append(c.hashes, c.unkeptPrint(ci, rows[a:b]))
		c.parentsSeen = append(c.parentsSeen, parent)
		a = b
	}
	p := c.p
	p.start()
	p.flag(len(c.in.Routes[ci].View.ParentRefs) == 0)
	for i, h := range c.hashes {
		p.ref(c.parentsSeen[i])
		p.u64(h)
	}
	out.Fingerprint = p.sum()
	return out
}
