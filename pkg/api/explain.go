package api

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Explanation drafts (design §7.5), one per fact code; the wording pass is Task 48.
const (
	explainHostname      = "listener %s has %s; route has %s"
	explainSameNamespace = "listener %s allows routes from namespace %s; route is in %s"
	explainSelector      = "listener %s allows namespaces with %s; route's namespace has %s"
	explainNoLabels      = "listener %s allows namespaces with %s; route's namespace has no labels"
	explainKind          = "listener %s allows %s; route is %s"
	explainSelectorError = "listener %s has an invalid namespace selector: %s"
	explainPort          = "parentRef port %s; listener %s is on port %s"
	explainSection       = "parentRef section %s; listener is %s"
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
		return fmt.Sprintf(explainKind, f.Listener, f.Want, f.Got)
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

// restricted is a fact with a hidden dependency: no reason and no note (rule 4).
func (f facts) restricted(rule string) Computed {
	return Computed{By: ComputedBy, Rules: f.ix.RuleSet, Rule: rule, State: string(computed.StateRestricted)}
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
		return Attachment{Listeners: []ListenerAttachment{}, Computed: f.restricted(rule)}
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
// is unknown presents no members, so the membership is unknown too. The
// count's check counts only when its dependencies are visible (rule 5). The
// note is the ref check's.
func (f facts) membership(res LinkedRef, rf *explore.RefFacts, h *explore.ListenerHit) ListenerAttachment {
	rule := rf.Out.Rule
	out := ListenerAttachment{Resource: res, Listener: h.Listener}
	// Every listener of a computed resource has a count; without one only the ref's check applies.
	lf, ok := f.ix.Listeners[explore.ListenerKey{Resource: rf.Out.Resource, Listener: h.Listener}]
	switch {
	case !f.v.allowDeps(f.ix, h.Deps):
		out.Computed = f.restricted(rule)
		return out
	case ok && !lf.Known:
		out.Computed = f.unknown(rule, lf.Unknown)
		return out
	}
	lc := lf.Check
	if !ok || !f.v.allowDeps(f.ix, lf.Deps) {
		lc = nil
	}
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

// listener adds view A's count of listener dto of resource: omitted when
// unknown or restricted (rule 5), with no isolation note then.
func (f facts) listener(resource objects.Ref, dto *ListenerDTO) {
	lf, ok := f.ix.Listeners[explore.ListenerKey{Resource: resource, Listener: dto.Name}]
	if !ok {
		return
	}
	var c Computed
	switch {
	case !f.v.allowDeps(f.ix, lf.Deps):
		c = f.restricted(ruleListenerCount)
	case !lf.Known:
		c = f.unknown(ruleListenerCount, lf.Unknown)
	default:
		n := lf.Count
		dto.ComputedRoutes, dto.IsolationNote = &n, lf.IsolationNote
		c = f.checked(ruleListenerCount, lf.Check)
	}
	dto.Computed = &c
}
