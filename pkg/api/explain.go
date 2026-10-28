package api

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
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
	return Computed{By: ComputedBy, Rules: f.ix.RuleSet, Rule: rule, State: string(st), Note: f.ix.RuleNote}
}

// restricted is a fact with a hidden dependency (rule 4). It names no rule,
// reason or note: the deciding rule can vary with the hidden data. Only the
// rule set, which is the install's, stays.
func (f facts) restricted() Computed {
	return Computed{By: ComputedBy, Rules: f.ix.RuleSet, State: string(computed.StateRestricted)}
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
	key := func(what string) string {
		if f.Child == "" {
			return fmt.Sprintf(fitKeyNone, what, f.Parent)
		}
		return fmt.Sprintf(fitKey, what, f.Parent, f.Child)
	}
	switch f.Clause {
	case computed.ClausePathType:
		if f.Parent == "" {
			return fitNoPath
		}
		return fmt.Sprintf(fitPathType, f.Parent)
	case computed.ClausePathPrefix:
		return fmt.Sprintf(fitPathPrefix, f.Child, f.Parent)
	case computed.ClauseDefaultSlash:
		return fmt.Sprintf(fitDefaultSlash, f.Parent)
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
