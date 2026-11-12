package api

import (
	"fmt"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/diagnose"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// Explanation clauses (design §7.5), one per fact code, from the wording pass
// (Task 48). Clauses hold no ";": explain joins them with "; ".
const (
	explainHostname      = "listener %s has %s, route has %s"
	explainSameNamespace = "listener %s allows routes from namespace %s, route is in %s"
	explainSelector      = "listener %s allows namespaces with %s, route's namespace has %s"
	explainNoLabels      = "listener %s allows namespaces with %s, route's namespace has no labels"
	explainKind          = "listener %s does not allow %s (allows %s)"
	explainSelectorError = "listener %s has an invalid namespace selector: %s"
	explainPort          = "parentRef wants port %s, listener %s is on %s"
	explainSection       = "parentRef names section %s, not listener %s"
	explainOther         = "listener %s: %s"
)

// explain is the explanation of a rejected parentRef from its listener facts.
// Only the deciding stage's facts are told, one clause each in the rules'
// order: hostname facts, else port and section facts, else the rest (rules §4.1).
func explain(fs []computed.Fact) string {
	stage := 0
	for _, f := range fs {
		stage = max(stage, factStage(f.Code))
	}
	parts := make([]string, 0, len(fs))
	for _, f := range fs {
		if factStage(f.Code) == stage {
			parts = append(parts, explainFact(f))
		}
	}
	return strings.Join(parts, "; ")
}

// factStage orders fact codes by the stage of rules §4.1 that records them.
func factStage(c computed.FactCode) int {
	switch c {
	case computed.FactHostname:
		return 2
	case computed.FactPort, computed.FactSection:
		return 1
	}
	return 0
}

// explainFact is one fact as a clause. A namespace fact compares labels when
// Got is a label set, which a namespace name never is.
func explainFact(f computed.Fact) string {
	switch f.Code {
	case computed.FactHostname:
		return fmt.Sprintf(explainHostname, f.Listener, f.Want, f.Got)
	case computed.FactNamespace:
		switch {
		case f.Got == "":
			return fmt.Sprintf(explainNoLabels, f.Listener, f.Want)
		case strings.Contains(f.Got, "="):
			return fmt.Sprintf(explainSelector, f.Listener, f.Want, f.Got)
		}
		return fmt.Sprintf(explainSameNamespace, f.Listener, f.Want, f.Got)
	case computed.FactKind:
		return fmt.Sprintf(explainKind, f.Listener, f.Got, f.Want)
	case computed.FactSelectorError:
		return fmt.Sprintf(explainSelectorError, f.Listener, f.Got)
	case computed.FactPort:
		return fmt.Sprintf(explainPort, f.Want, f.Listener, f.Got)
	case computed.FactSection:
		return fmt.Sprintf(explainSection, f.Want, f.Got)
	}
	return fmt.Sprintf(explainOther, f.Listener, f.Code)
}

// ruleListenerCount is the rule of a listener's computed count.
const ruleListenerCount = string(computed.KindListenerCount)

// facts renders view A for the caller of one request (design §7.3, §7.4 rules 4–6).
type facts struct {
	ix *explore.Index
	v  *view
	l  linker
}

// label is a fact of rule in state st, with the rule set's note.
func (f facts) label(rule string, st computed.State) Computed {
	return Computed{By: ComputedBy, Rules: f.ix.RuleSet, Label: f.ix.RuleLabel, Rule: rule, State: string(st), Note: f.ix.RuleNote}
}

// restricted is a fact with a hidden dependency (rule 4). It names no rule,
// reason or note: the deciding rule can vary with the hidden data. Only the
// rule set, which is the install's, stays.
func (f facts) restricted() Computed {
	return Computed{By: ComputedBy, Rules: f.ix.RuleSet, Label: f.ix.RuleLabel, State: string(computed.StateRestricted)}
}

// unknown is a fact that could not be computed, with why.
func (f facts) unknown(rule string, why computed.UnknownReason) Computed {
	c := f.label(rule, computed.StateUnknown)
	c.Reason = string(why)
	return c
}

// checked is a computed fact labelled by its cross-check, with the check's
// own note, which includes the rule set's (rules §8.1). A fact no check
// covers is checked and not verified (rules §8.3, not observable).
func (f facts) checked(rule string, c *computed.CheckResult) Computed {
	if c == nil {
		out := f.label(rule, computed.StateChecked)
		out.Coverage = []string{string(computed.CoverageNone)}
		return out
	}
	out := f.label(rule, checkState(c))
	out.Coverage, out.Note = coverage(c.Coverage), c.Note
	return out
}

// checkState is c's state as the engine set it; pending until it has one.
func checkState(c *computed.CheckResult) computed.State {
	if c == nil {
		return computed.StateChecked
	}
	if c.State == "" {
		return computed.StatePending
	}
	return c.State
}

// coverage is cs on the wire; nil when empty, so it is omitted.
func coverage(cs []computed.Coverage) []string {
	if len(cs) == 0 {
		return nil
	}
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = string(c)
	}
	return out
}

// memberCoverage is what a listener membership's checks verified (rules
// §8.4): the listener's count-agrees, the ref's status-compatible, and
// not-verified when the ref matched several listeners or nothing else applies.
func memberCoverage(listener, ref computed.CheckResult, matched int) []computed.Coverage {
	var out []computed.Coverage
	if slices.Contains(listener.Coverage, computed.CoverageCount) {
		out = append(out, computed.CoverageCount)
	}
	if slices.Contains(ref.Coverage, computed.CoverageStatus) {
		out = append(out, computed.CoverageStatus)
	}
	if matched > 1 || len(out) == 0 {
		out = append(out, computed.CoverageNone)
	}
	return out
}

// memberState is disagrees or pending when either check is, else checked.
func memberState(listener, ref *computed.CheckResult) computed.State {
	ls, rs := checkState(listener), checkState(ref)
	switch {
	case ls == computed.StateDisagrees || rs == computed.StateDisagrees:
		return computed.StateDisagrees
	case ls == computed.StatePending || rs == computed.StatePending:
		return computed.StatePending
	}
	return computed.StateChecked
}

// attachment is view A's outcome of one parentRef for the caller.
func (f facts) attachment(rf *explore.RefFacts) Attachment {
	rule := rf.Out.Rule
	switch {
	case !f.v.allowDeps(f.ix, rf.Deps):
		return Attachment{Listeners: []ListenerAttachment{}, Computed: f.restricted()}
	case rf.Out.Unknown != "":
		return Attachment{Listeners: []ListenerAttachment{}, Computed: f.unknown(rule, rf.Out.Unknown)}
	}
	out := Attachment{Outcome: string(rf.Out.Outcome), Listeners: make([]ListenerAttachment, 0, len(rf.Listeners)), Computed: f.checked(rule, rf.Check)}
	if rf.Out.Outcome != computed.OutcomeAccepted {
		out.Explanation = explain(rf.Out.Facts)
	}
	res := f.l.linked(rf.Out.Resource)
	for i := range rf.Listeners {
		out.Listeners = append(out.Listeners, f.membership(res, rf, &rf.Listeners[i]))
	}
	return out
}

// membership is one listener of an accepted parentRef. A listener whose count
// is unknown presents no members, so the membership is unknown too. Listener
// facts are shown to every caller who sees the listener (rule 5), so the
// count's check and unknown state apply whatever its candidates. The note is
// the ref check's.
func (f facts) membership(res LinkedRef, rf *explore.RefFacts, h *explore.ListenerHit) ListenerAttachment {
	rule := rf.Out.Rule
	out := ListenerAttachment{Resource: res, Listener: h.Listener}
	// Every listener of a computed resource has a count; without one only the ref's check applies.
	lf, ok := f.ix.Listeners[explore.ListenerKey{Resource: rf.Out.Resource, Listener: h.Listener}]
	switch {
	case !f.v.allowDeps(f.ix, h.Deps):
		out.Computed = f.restricted()
		return out
	case ok && !lf.Known:
		out.Computed = f.unknown(rule, lf.Unknown)
		return out
	}
	lc := lf.Check
	var lv, rv computed.CheckResult
	if lc != nil {
		lv = *lc
	}
	if rf.Check != nil {
		rv = *rf.Check
	}
	st := memberState(lc, rf.Check)
	out.Computed = f.label(rule, st)
	out.Computed.Coverage = coverage(memberCoverage(lv, rv, len(rf.Listeners)))
	if rf.Check != nil {
		out.Computed.Note = rf.Check.Note
	}
	if st == computed.StateChecked || st == computed.StateDisagrees {
		out.EffectiveHostnames = h.Hostnames
	}
	return out
}

// listener adds view A's count of listener dto of resource, omitted when
// unknown, with no isolation note then. A count is a fact of its listener: it
// includes members the caller cannot view, as attachedRoutes does (rule 5).
func (f facts) listener(resource objects.Ref, dto *ListenerDTO) {
	lf, ok := f.ix.Listeners[explore.ListenerKey{Resource: resource, Listener: dto.Name}]
	if !ok {
		return
	}
	var c Computed
	if lf.Known {
		n := lf.Count
		dto.ComputedRoutes, dto.IsolationNote = &n, lf.IsolationNote
		c = f.checked(ruleListenerCount, lf.Check)
	} else {
		c = f.unknown(ruleListenerCount, lf.Unknown)
	}
	dto.Computed = &c
}

// stateRank orders check states for a merge, worst highest (design §7.3):
// disagrees > pending > unknown > restricted > checked.
func stateRank(st string) int {
	switch computed.State(st) {
	case computed.StateDisagrees:
		return 4
	case computed.StatePending:
		return 3
	case computed.StateUnknown:
		return 2
	case computed.StateRestricted:
		return 1
	}
	return 0
}

// mergeComputed is a and b as one fact: the worst state with its reason and
// note, a's on a tie, and the union of their coverage. A restricted side
// makes it restricted, since the union would depend on that side.
func mergeComputed(a, b Computed) Computed {
	switch {
	case a.State == string(computed.StateRestricted):
		return a
	case b.State == string(computed.StateRestricted):
		return b
	}
	out := a
	if stateRank(b.State) > stateRank(a.State) {
		out = b
	}
	var cov []string
	for _, c := range []computed.Coverage{computed.CoverageCount, computed.CoverageStatus, computed.CoverageNone} {
		if slices.Contains(a.Coverage, string(c)) || slices.Contains(b.Coverage, string(c)) {
			cov = append(cov, string(c))
		}
	}
	out.Coverage = cov
	return out
}

// mergeAttachment is the list's one Attachment for two written parentRefs
// that resolve to the same parent (design §7.3). They share outcome,
// listeners and deps and differ only in their checks, which merge worst first.
func mergeAttachment(a, b Attachment) Attachment {
	a.Computed = mergeComputed(a.Computed, b.Computed)
	if st := computed.State(a.Computed.State); st == computed.StateRestricted || st == computed.StateUnknown {
		return Attachment{Listeners: []ListenerAttachment{}, Computed: a.Computed}
	}
	for i := range a.Listeners {
		m := &a.Listeners[i]
		for _, o := range b.Listeners {
			if o.Listener == m.Listener && o.Resource == m.Resource {
				m.Computed = mergeComputed(m.Computed, o.Computed)
				if m.EffectiveHostnames == nil {
					m.EffectiveHostnames = o.EffectiveHostnames
				}
			}
		}
		// Hostnames go with checked or disagrees only (design §7.3).
		if st := computed.State(m.Computed.State); st != computed.StateChecked && st != computed.StateDisagrees {
			m.EffectiveHostnames = nil
		}
	}
	return a
}

// Rules of views B and C's computed facts; the rule sets name none (our call).
const (
	ruleDelegation = "delegation"
	ruleEffective  = "effective-matchers"
)

// View B and C text, from the wording pass (Task 67). Outcome reasons are
// sentences without a final period, like 2.3's notes; fit clauses hold no
// ";", since a partial row joins its reason and clause with "; ".
const (
	reasonPartial           = "Attached on some paths only"
	reasonForbidden         = "The route's parentRefs do not permit this parent"
	reasonRejectedHostnames = "A delegated route may not set hostnames"
	reasonCycle             = "A delegation loop fails this ref"
	reasonMissing           = "No such route"
	reasonUnwatched         = "Namespace not watched"
	reasonDroppedSome       = "No computed path attaches it. Others not computed"
	noteSomeNotComputed     = "Some paths not computed"
	noteUnconfirmable       = "Status cannot confirm this path"
	// rowFit prefixes a row's fit clause with the child's rule that failed, counted from 1.
	rowFit = "the route's rule %d: %s"
)

// Fit clauses (rules §6). Values carry no backticks: the Why cell and path
// lines render text as is.
const (
	fitPathType     = "the parent's path match is %s, not PathPrefix"
	fitNoPath       = "the parent has no path match"
	fitPathPrefix   = "path %s is outside the parent's %s"
	fitDefaultSlash = "no matches, so it defaults to /, which is outside the parent's %s"
	fitKey          = "the parent needs %s %s, the rule has %s"
	fitKeyNone      = "the parent needs %s %s, the rule has none"
	fitNoMatches    = "an empty match list fits no parent match"
)

// explainFit is the clause a child rule failed, as text; "" for nil.
func explainFit(f *computed.FitFailure) string {
	if f == nil {
		return ""
	}
	// The compared values are display text: clipped, never the computed match.
	parent, child := computed.ClipDisplay(f.Parent, computed.MaxDisplayBytes), computed.ClipDisplay(f.Child, computed.MaxDisplayBytes)
	key := func(what string) string {
		if child == "" {
			return fmt.Sprintf(fitKeyNone, what, parent)
		}
		return fmt.Sprintf(fitKey, what, parent, child)
	}
	switch f.Clause {
	case computed.ClausePathType:
		if parent == "" {
			return fitNoPath
		}
		return fmt.Sprintf(fitPathType, parent)
	case computed.ClausePathPrefix:
		return fmt.Sprintf(fitPathPrefix, child, parent)
	case computed.ClauseDefaultSlash:
		return fmt.Sprintf(fitDefaultSlash, parent)
	case computed.ClauseHeader:
		return key("header")
	case computed.ClauseQuery:
		return key("query parameter")
	case computed.ClauseMethod:
		return key("method")
	case computed.ClauseNoMatches:
		return fitNoMatches
	}
	return string(f.Clause)
}

// explainRow is a row's or context's reason: the outcome's text, the failing
// fit clause with its child rule, or why the ref selected nothing. An
// unknown outcome has text only when some path dropped the child; its
// unknown reason is the Computed's.
func explainRow(o computed.RowOutcome, f *computed.FitFailure, u computed.Unresolved) string {
	fit := ""
	if f != nil {
		fit = fmt.Sprintf(rowFit, f.Rule+1, explainFit(f))
	}
	switch o {
	case computed.OutcomeDropped:
		return fit
	case computed.OutcomePartial:
		if fit == "" {
			return reasonPartial
		}
		return reasonPartial + "; " + fit
	case computed.OutcomeForbidden:
		return reasonForbidden
	case computed.OutcomeRejectedHostnames:
		return reasonRejectedHostnames
	case computed.OutcomeCycle:
		return reasonCycle
	case computed.OutcomeUnresolved:
		if u == computed.UnresolvedUnwatched {
			return reasonUnwatched
		}
		return reasonMissing
	case computed.OutcomeUnknown:
		if f != nil {
			return reasonDroppedSome
		}
	}
	return ""
}

// rowNote is what a row adds to its check's note (rules §5.5, design §5.2).
func rowNote(r *computed.Row) string {
	switch {
	case r.SomeNotComputed:
		return noteSomeNotComputed
	case r.Unconfirmable:
		return noteUnconfirmable
	}
	return ""
}

// joinNotes joins two notes as sentences; either may be empty.
func joinNotes(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + ". " + b
}

// delegation is a view B row's Computed for the caller: restricted when a
// dependency other than the routes on its paths is hidden (design §7.4
// rules 4, 4b); unknown with the row's reason; else its child-entry check,
// with the row's own note (PF14).
func (f facts) delegation(r *explore.DelegRow) Computed {
	if !f.v.allowDeps(f.ix, r.Deps) {
		return f.restricted()
	}
	var c Computed
	if r.Outcome == computed.OutcomeUnknown {
		c = f.unknown(ruleDelegation, r.Unknown)
	} else {
		c = f.checked(ruleDelegation, r.Check)
		if c.State == string(computed.StateUnknown) && r.Check.Unknown != "" {
			c.Reason = string(r.Check.Unknown)
		}
	}
	c.Note = joinNotes(c.Note, rowNote(&r.Row))
	return c
}

// effective is a view C edge's Computed for the caller: never verified by
// the controller (rules §8.4), unknown when a context is, restricted when a
// dependency other than the routes on its paths is hidden.
func (f facts) effective(e *computed.EffectiveEdge, deps explore.DepSet) Computed {
	if !f.v.allowDeps(f.ix, deps) {
		return f.restricted()
	}
	c := f.label(ruleEffective, computed.StateChecked)
	for i := range e.Contexts {
		if u := e.Contexts[i].Unknown; u != "" {
			c = f.unknown(ruleEffective, u)
			break
		}
	}
	c.Coverage = []string{string(computed.CoverageNone)}
	return c
}

// Kinds of a reference explanation (design §10).
const (
	RefAttributed = "attributed"
	RefCandidate  = "candidate"
	RefWaiting    = "waiting"
)

// Reference explanation wording (design §10). A candidate quotes the route's
// reason and guesses nothing.
const (
	refExplainNotPermitted = "The controller does not permit this ref to another namespace."
	refExplainNotFound     = "The controller cannot find this backend."
	refExplainInvalidKind  = "The controller does not support this backend kind."
	refExplainCandidate    = "This route reports `ResolvedRefs=False` (%s) for one of these refs."
	refExplainWaiting      = "Waiting for the controller: the route's status is older than its spec."
)

// refUse is one place a route writes a backend: rule and backendRefs index
// (-1 for a rule filter's mirror), the path KGWUI-013/014 name, and the port.
type refUse struct {
	rule, ref int
	path      string
	port      int32
}

// refExplanation is one explanation before the caller's visibility applies;
// f is the finding it rests on, nil when waiting.
type refExplanation struct {
	route        objects.Ref
	use          refUse
	kind, reason string
	text         string
	f            *health.Finding
}

// refExplanations explains each ref of routes to backend that the route's
// status reports unresolved (design §10), routes in order and refs in spec
// order. It reads the 013/014 attribution and evaluates no ReferenceGrant.
func refExplanations(i *engine.InstallModel, routes []objects.Ref, backend objects.Ref) []refExplanation {
	ix := i.Index
	if ix == nil || len(routes) == 0 {
		return nil
	}
	at := make(map[objects.Ref]int, len(routes))
	for k, r := range routes {
		at[r] = k
	}
	found := make([][]*health.Finding, len(routes))
	for k := range i.Health.Findings {
		f := &i.Health.Findings[k]
		if f.Resource == nil || f.Code != health.CodeRefUnresolved && f.Code != health.CodeRefNotPermitted {
			continue
		}
		if j, ok := at[*f.Resource]; ok {
			found[j] = append(found[j], f)
		}
	}
	var out []refExplanation
	for k, route := range routes {
		id, ok := ix.Row(route)
		if !ok {
			continue
		}
		row := &ix.Routes[id]
		uses := refUses(row, backend)
		if len(uses) == 0 {
			continue
		}
		reason, stale := resolvedRefs(row, i.Resolved.ControllerNames)
		for _, u := range uses {
			e, ok := explainUse(found[k], u, reason)
			if !ok && stale {
				e, ok = refExplanation{kind: RefWaiting, text: refExplainWaiting}, true
			}
			if ok {
				e.route, e.use = route, u
				out = append(out, e)
			}
		}
	}
	return out
}

// refUses are the places row writes backend, in the order KGWUI-013/014 list refs.
func refUses(row *explore.RouteRow, backend objects.Ref) []refUse {
	var rules []objects.Rule
	if row.View != nil {
		rules = row.View.Rules
	} else {
		rules = objects.DecodeRouteRules(row.Obj)
	}
	var out []refUse
	mirrors := func(fs []objects.Filter, rule, ref int) {
		for k, f := range fs {
			if f.Mirror != nil && f.Mirror.Backend == backend {
				out = append(out, refUse{rule, ref, health.RouteRefPath(rule, ref, k), f.Mirror.Port})
			}
		}
	}
	for i := range rules {
		for j, br := range rules[i].BackendRefs {
			if br.Ref == backend {
				out = append(out, refUse{i, j, health.RouteRefPath(i, j, -1), br.Port})
			}
			mirrors(br.Filters, i, j)
		}
		mirrors(rules[i].Filters, i, -1)
	}
	return out
}

// resolvedRefs reads the install's status parents of row: the first current
// ResolvedRefs=False reason other than RefNotPermitted (KGWUI-013's), and
// whether any ResolvedRefs=False describes an older generation.
func resolvedRefs(row *explore.RouteRow, controllers []string) (reason string, stale bool) {
	var parents []objects.RouteParentStatus
	if row.View != nil {
		parents = row.View.Parents
	} else {
		parents = objects.RouteParents(row.Obj)
	}
	for _, p := range parents {
		c := objects.FindCondition(p.Conditions, "ResolvedRefs")
		if c == nil || c.Status != metav1.ConditionFalse || !slices.Contains(controllers, p.ControllerName) {
			continue
		}
		switch {
		case objects.IsStale(row.Obj, *c):
			stale = true
		case reason == "" && c.Reason != reasonRefNotPermitted:
			reason = c.Reason
		}
	}
	return reason, stale
}

// reasonRefNotPermitted is KGWUI-014's reason.
const reasonRefNotPermitted = "RefNotPermitted"

// explainUse is u's explanation from fs, the route's 013 and 014 findings:
// attributed when one attributes u, else a candidate when one names it.
func explainUse(fs []*health.Finding, u refUse, reason013 string) (refExplanation, bool) {
	var out refExplanation
	found := false
	for _, f := range fs {
		ev, ok := f.Facts.(*health.RefsEvidence)
		if !ok || !slices.ContainsFunc(ev.Candidates, func(c health.RefLocation) bool { return c.Path == u.path }) {
			continue
		}
		reason := reason013
		if f.Code == health.CodeRefNotPermitted {
			reason = reasonRefNotPermitted
		}
		if ev.Attributed {
			return refExplanation{kind: RefAttributed, reason: reason, text: attributedText(f, ev, u, reason), f: f}, true
		}
		if !found {
			out, found = refExplanation{kind: RefCandidate, reason: reason, text: fmt.Sprintf(refExplainCandidate, reason), f: f}, true
		}
	}
	return out, found
}

// attributedText is an attributed finding's explanation in plain words:
// KGWUI-013's own explanation when it has one, else the route's reason.
func attributedText(f *health.Finding, ev *health.RefsEvidence, u refUse, reason string) string {
	c := ev.Candidates[0].Ref
	switch ev.Explanation {
	case health.RefExplainKindNotServed:
		return f.Message
	case health.RefExplainServiceMissing, health.RefExplainServicePort:
		return health.RefServiceSentence(ev.Explanation, c.Namespace+"/"+c.Name, u.port, ev.Ports)
	}
	switch reason {
	case reasonRefNotPermitted:
		return refExplainNotPermitted
	case "BackendNotFound":
		return refExplainNotFound
	case "InvalidKind":
		return refExplainInvalidKind
	}
	return f.Message
}

// explanationDTO is e for the caller of v, who may see its route. An
// attribution resting on evidence the caller may not see quotes only the
// route's status, as a candidate; a fix naming a hidden object is left out.
func explanationDTO(v *view, l linker, e refExplanation) RefExplanationDTO {
	out := RefExplanationDTO{Route: l.linked(e.route), Rule: e.use.rule, Ref: e.use.ref, Path: e.use.path, Kind: e.kind, Reason: e.reason, Explanation: e.text}
	if e.kind != RefAttributed {
		return out
	}
	if !v.allOf(e.f.Evidence) {
		out.Kind, out.Explanation = RefCandidate, fmt.Sprintf(refExplainCandidate, e.reason)
		return out
	}
	out.Fix = wireFix(v, l, diagnose.Fix(e.f))
	return out
}
