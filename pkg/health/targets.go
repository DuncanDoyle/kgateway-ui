package health

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// CodePolicyTargetNotFound is KGWUI-016: a policy target resolves to nothing (design §7.6).
const CodePolicyTargetNotFound = "KGWUI-016"

// TargetHold is how long a computed missing target must hold, after the
// policy's grace, before it is a KGWUI-016 (design §7.6).
const TargetHold = 30 * time.Second

// Wording for the computed KGWUI-016 and its verifier (design §7.6).
const (
	// PolicyTargetNotFoundMessage is KGWUI-016's message, as for the controller's TargetNotFound.
	PolicyTargetNotFoundMessage = "Policy target not found"
	// TargetNotVerified labels a 016 whose ancestors cannot be tied to the missing target.
	TargetNotVerified = "Not verified: the controller's status cannot confirm this."
	// targetComputed labels every computed 016.
	targetComputed = " Computed by the UI's policy targets rule."
	// Details per outcome: the target, then the policy's namespace or the section.
	targetNotFound       = "%s: not found in namespace %s."
	targetSectionMissing = "%s: section `%s` not found."
	targetMatchesNothing = "%s: matches nothing in namespace %s."
	targetEmptySelector  = "%s: an empty selector matches nothing."
	// targetDisagrees is the verifier's 012 detail: target, computed outcome, the ancestor.
	targetDisagrees = "%s: computed %s, controller reports ancestor %s."
)

// targetsKey is the Store key of the holds: hold key → when the target was first seen missing.
const targetsKey = "policy targets"

// policyTargets is one run's computed 016 state for the resource status check.
type policyTargets struct {
	in         *Input
	t          computed.PolicyTargeter
	prev, next map[string]time.Time
	wake       func(time.Time)
}

// newPolicyTargets returns the run's targets state, or nil when the rule set has no PolicyTargeter.
func (in *Input) newPolicyTargets(st *Store, wake func(time.Time)) *policyTargets {
	t := in.Rules.PolicyTargeter()
	if t == nil {
		return nil
	}
	prev, _ := Load[map[string]time.Time](st, targetsKey)
	return &policyTargets{in: in, t: t, prev: prev, next: map[string]time.Time{}, wake: wake}
}

// save keeps the holds of the targets missing in this run; the rest start over.
func (p *policyTargets) save(st *Store) {
	if p == nil || st == nil {
		return
	}
	if len(p.next) == 0 {
		st.Delete(targetsKey)
		return
	}
	if !maps.Equal(p.prev, p.next) {
		st.Set(targetsKey, p.next)
	}
}

// missing reports whether a target outcome is a 016.
func missing(outcome string) bool {
	switch outcome {
	case computed.TargetNotFound, computed.TargetSectionNotFound, computed.TargetMatchesNothing:
		return true
	}
	return false
}

// findings returns policy u's matured 016s and verifier 012s; all is true
// when u has targets and every one is a matured 016, so 016 replaces 003.
func (p *policyTargets) findings(u *unstructured.Unstructured) (fs []Finding, all bool) {
	if p == nil {
		return nil, false
	}
	in := p.in
	refs, sels := computed.DecodeTargets(u)
	rs := in.targetResults(p.t, u, refs, sels)
	graceEnd := in.firstSeen(u).Add(Grace)
	all = len(rs) > 0
	for _, r := range rs {
		if !missing(r.Outcome) {
			all = false
			continue
		}
		key := holdKey(u, r)
		seen := p.hold(key)
		// The hold runs from the later of the first sight and the grace end.
		due := graceEnd
		if seen.After(due) {
			due = seen
		}
		due = due.Add(TargetHold)
		selector := r.Index >= len(refs)
		// An absent route has no Gateways to tie an ancestor to.
		verifiable := r.Verifiable && !selector && !in.listenerSetParent(u, r.Ref) &&
			(r.Outcome != computed.TargetNotFound || r.Ref.Kind == "Gateway")
		// The verifier's 012 has its own window from the ancestor's first sight (phase-2 rules §9.3).
		var ancestor objects.Ref
		var ancestorDue, ancestorSeen time.Time
		if verifiable {
			if a, ok := in.ancestorFor(u, r.Ref); ok {
				ancestor, ancestorSeen = a, p.hold(key+"\x00ancestor\x00"+a.String())
				ancestorDue = ancestorSeen.Add(TargetHold)
				if ancestorDue.Before(due) {
					ancestorDue = due
				}
			}
		}
		if !ancestorDue.IsZero() && in.Now.Before(ancestorDue) {
			p.wake(ancestorDue)
		}
		if in.Now.Before(due) {
			p.wake(due)
			all = false
			continue
		}
		since := seen
		f := Finding{Code: CodePolicyTargetNotFound, Slug: "policy-target-not-found", Severity: SeverityWarning, Install: in.Install.ID(),
			Resource: ref(u), Message: PolicyTargetNotFoundMessage, Detail: in.targetDetail(u, r, refs, sels, verifiable), Since: &since,
			Key: []string{strconv.Itoa(r.Index), r.Ref.Group, r.Ref.Kind, r.Ref.Name, r.Ref.SectionName}}
		fs = append(fs, f)
		if !ancestorDue.IsZero() && !in.Now.Before(ancestorDue) {
			fs = append(fs, in.targetDisagrees(u, r, ancestor, ancestorSeen, len(refs)))
		}
	}
	return fs, all
}

// hold returns when key was first seen, now on its first sight, and keeps it for the next run.
func (p *policyTargets) hold(key string) time.Time {
	seen, ok := p.prev[key]
	if !ok {
		seen = p.in.Now
	}
	p.next[key] = seen
	return seen
}

// holdKey names one target of u, by uid and ref, index and ref as written.
func holdKey(u *unstructured.Unstructured, r computed.TargetResult) string {
	ref := objects.RefOf(u)
	return strings.Join([]string{string(u.GetUID()), ref.Namespace, ref.Kind, ref.Name, strconv.Itoa(r.Index),
		r.Ref.Group, r.Ref.Kind, r.Ref.Name, r.Ref.SectionName}, "\x00")
}

// targetResults runs the rule over policy u's targets as written. A policy
// with no namespace is never searched for, so it has no results.
func (in Input) targetResults(t computed.PolicyTargeter, u *unstructured.Unstructured, refs []computed.TargetRef, sels []computed.TargetSelector) []computed.TargetResult {
	if t == nil || u.GetNamespace() == "" || len(refs)+len(sels) == 0 {
		return nil
	}
	return t.PolicyTargets(computed.TargetInput{Policy: objects.RefOf(u), TargetRefs: refs, Selectors: sels,
		Lookup: in.lookup, Labelled: in.labelled, Served: in.servedWatched,
		ScopeKnown:            in.Install.Scope.Mode != install.ScopeUnknown,
		GlobalPolicyNamespace: in.Install.Settings.GlobalPolicyNamespace.Value, GlobalPolicyNamespaceKnown: in.Install.Settings.GlobalPolicyNamespace.Known})
}

// lookup returns the cached object of gk named ns/name.
func (in Input) lookup(gk objects.GroupKind, ns, name string) (*unstructured.Unstructured, bool) {
	u := in.stored(objects.Ref{Group: gk.Group, Kind: gk.Kind, Namespace: ns, Name: name})
	return u, u != nil
}

// labelled reports whether a cached object of gk in ns, or in any namespace for "", has every label in sel.
func (in Input) labelled(gk objects.GroupKind, ns string, sel map[string]string) bool {
	if in.Snapshot == nil {
		return false
	}
	for _, k := range in.Served {
		if k.Group != gk.Group || k.Kind != gk.Kind {
			continue
		}
		objs := in.Snapshot.Objects[k.GVR()]
		i := 0
		if ns != "" {
			// The cache sorts each kind by "namespace/name".
			i, _ = slices.BinarySearchFunc(objs, ns+"/", func(u *unstructured.Unstructured, p string) int {
				return strings.Compare(u.GetNamespace()+"/"+u.GetName(), p)
			})
		}
		for ; i < len(objs) && (ns == "" || objs[i].GetNamespace() == ns); i++ {
			if hasLabels(objs[i].GetLabels(), sel) {
				return true
			}
		}
		return false
	}
	return false
}

func hasLabels(have, want map[string]string) bool {
	for k, v := range want {
		if got, ok := have[k]; !ok || got != v {
			return false
		}
	}
	return true
}

// servedWatched reports whether gk is served and the cache can read it.
func (in Input) servedWatched(gk objects.GroupKind) bool {
	for _, k := range in.Served {
		if k.Group == gk.Group && k.Kind == gk.Kind {
			return in.Snapshot != nil && in.Snapshot.Access[k.GVR()].OK
		}
	}
	return false
}

// listenerSetParent reports whether target is a cached route with a
// ListenerSet parent: that ancestor was never observed, so it is not verifiable.
func (in Input) listenerSetParent(u *unstructured.Unstructured, target computed.TargetRef) bool {
	for _, p := range in.routeTargetParents(u, target) {
		if p.Kind == "ListenerSet" || p.Kind == "XListenerSet" {
			return true
		}
	}
	return false
}

// routeTargetParents returns the parentRefs of route target in u's namespace, or nil.
func (in Input) routeTargetParents(u *unstructured.Unstructured, target computed.TargetRef) []objects.ParentRef {
	if target.Kind != "HTTPRoute" && target.Kind != "GRPCRoute" {
		return nil
	}
	r, ok := in.lookup(objects.GroupKind{Group: target.Group, Kind: target.Kind}, u.GetNamespace(), target.Name)
	if !ok {
		return nil
	}
	if target.Kind == "HTTPRoute" {
		return in.views().HTTPRoute(r).ParentRefs
	}
	return in.views().Route(r).ParentRefs
}

// ancestorFor returns a current ancestor of u, written by this install's
// controller, that belongs to target: the Gateway itself, or one of the
// route's own parent Gateways. An ancestor reporting TargetNotFound agrees.
func (in Input) ancestorFor(u *unstructured.Unstructured, target computed.TargetRef) (objects.Ref, bool) {
	var gws []objects.Ref
	switch target.Kind {
	case "Gateway":
		gws = []objects.Ref{{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: u.GetNamespace(), Name: target.Name}}
	default:
		for _, p := range in.routeTargetParents(u, target) {
			if p.Group == objects.GatewayAPIGroup && p.Kind == "Gateway" {
				gws = append(gws, p.Ref())
			}
		}
	}
	for _, a := range objects.PolicyAncestors(u) {
		r := a.AncestorRef.Ref()
		if !slices.Contains(in.Install.ControllerNames, a.ControllerName) || !slices.Contains(gws, r) ||
			PolicyPending(u, a.Conditions) != "" || targetNotFoundReason(a.Conditions) {
			continue
		}
		return r, true
	}
	return objects.Ref{}, false
}

func targetNotFoundReason(cs []metav1.Condition) bool {
	return slices.ContainsFunc(cs, func(c metav1.Condition) bool { return c.Reason == "TargetNotFound" })
}

// targetStatusRules is a rule set whose controller reports TargetNotFound itself, as kgw25 does.
type targetStatusRules interface{ ReportsTargetNotFound() bool }

// statusTargetFinding is the KGWUI-016 error for a current policy ancestor whose
// condition is False with reason TargetNotFound, on a rule set that reads it from
// status (design §7.6, D-020 rule 5). It replaces that ancestor's 002s (D-030).
func (in Input) statusTargetFinding(u *unstructured.Unstructured, cs []metav1.Condition, extra string, owner []string) (Finding, bool) {
	if r, ok := in.Rules.Rules.(targetStatusRules); !ok || !r.ReportsTargetNotFound() {
		return Finding{}, false
	}
	// Accepted comes first: it carries the controller's list of missing targets.
	for _, t := range append(slices.Clone(policyRequired), policyOptional...) {
		if c := objects.FindCondition(cs, t); c != nil && c.Status == metav1.ConditionFalse && c.Reason == "TargetNotFound" {
			return Finding{Code: CodePolicyTargetNotFound, Slug: "policy-target-not-found", Severity: SeverityError, Install: in.Install.ID(),
				Resource: ref(u), Message: PolicyTargetNotFoundMessage, Detail: detail(*c, extra), Since: since(*c),
				Key: append(slices.Clip(owner), c.Type)}, true
		}
	}
	return Finding{}, false
}

// targetDesc names a target as written: "targetRefs[0] Gateway `edge`" or
// "targetSelectors[0] HTTPRoute `app=web`".
func targetDesc(r computed.TargetResult, refs int, selector bool, labels map[string]string) string {
	if !selector {
		return fmt.Sprintf("targetRefs[%d] %s `%s`", r.Index, r.Ref.Kind, r.Ref.Name)
	}
	ls := make([]string, 0, len(labels))
	for _, k := range slices.Sorted(maps.Keys(labels)) {
		ls = append(ls, k+"="+labels[k])
	}
	return fmt.Sprintf("targetSelectors[%d] %s `%s`", r.Index-refs, r.Ref.Kind, strings.Join(ls, ","))
}

// targetDetail says what is missing, that the UI computed it, and under which rule set.
func (in Input) targetDetail(u *unstructured.Unstructured, r computed.TargetResult, refs []computed.TargetRef, sels []computed.TargetSelector, verifiable bool) string {
	selector := r.Index >= len(refs)
	var labels map[string]string
	if selector {
		labels = sels[r.Index-len(refs)].MatchLabels
	}
	desc := targetDesc(r, len(refs), selector, labels)
	var d string
	switch {
	case r.Outcome == computed.TargetSectionNotFound:
		d = fmt.Sprintf(targetSectionMissing, desc, r.Ref.SectionName)
	case r.Outcome == computed.TargetMatchesNothing && len(labels) == 0:
		d = fmt.Sprintf(targetEmptySelector, desc)
	case r.Outcome == computed.TargetMatchesNothing:
		d = fmt.Sprintf(targetMatchesNothing, desc, u.GetNamespace())
	default:
		d = fmt.Sprintf(targetNotFound, desc, u.GetNamespace())
	}
	d += targetComputed
	if !verifiable {
		d += " " + TargetNotVerified
	}
	return d + in.ruleSetText()
}

// ruleSetText ends a detail with the rule set and its selection note.
func (in Input) ruleSetText() string {
	d := fmt.Sprintf(ruleSetDetail, in.Rules.Rules.Version())
	if in.Rules.Note != "" {
		d += " " + in.Rules.Note + "."
	}
	return d
}

// targetDisagrees is the verifier's KGWUI-012: the controller wrote a current
// ancestor for a lone target the rule calls missing.
func (in Input) targetDisagrees(u *unstructured.Unstructured, r computed.TargetResult, ancestor objects.Ref, since time.Time, refs int) Finding {
	outcome := "not found"
	if r.Outcome == computed.TargetSectionNotFound {
		outcome = "section not found"
	}
	own := in.firstSeen(u)
	return Finding{Code: CodeComputedDisagrees, Slug: "computed-view-disagrees", Severity: SeverityWarning, Install: in.Install.ID(),
		Resource: ref(u), Message: ComputedDisagreesMessage, Since: &since, ResourceSince: &own, Evidence: []objects.Ref{ancestor},
		Detail: fmt.Sprintf(targetDisagrees, targetDesc(r, refs, false, nil), outcome, ancestor.Kind+" "+ancestor.Namespace+"/"+ancestor.Name) + in.ruleSetText(),
		// The check kind and key become "#n" (design §5.1).
		Key: []string{string(computed.KindPolicyTarget), strconv.Itoa(r.Index), r.Ref.Group, r.Ref.Kind, r.Ref.Name, r.Ref.SectionName}}
}
