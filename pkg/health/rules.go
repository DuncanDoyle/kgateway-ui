package health

import (
	"encoding/json"
	"regexp"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// CodeRuleReplaced is KGWUI-015: the controller replaced or dropped a route rule (design §7.4).
const CodeRuleReplaced = "KGWUI-015"

// RuleReplacedHint ends a 015's detail once the rule has been replaced for
// longer than RuleReplacedHintAge (kgateway #14047).
const RuleReplacedHint = "If the cause was temporary, such as an identity provider outage, some versions keep the rule replaced until the controller restarts."

// RuleReplacedHintAge is how long a rule stays replaced before the hint shows.
const RuleReplacedHintAge = 10 * time.Minute

// EvidenceRuleReplaced is the kind of RuleReplacedEvidence.
const EvidenceRuleReplaced = "rule-replaced"

// RuleReplacedEvidence is what KGWUI-015 quotes. MatchIndex is the controller's
// match index, not a rule index, and nil when the message does not parse.
// Policy is the served policy kind the message names, when exactly one is named.
type RuleReplacedEvidence struct {
	MatchIndex *int         `json:"matchIndex,omitempty"`
	Reason     string       `json:"reason"`
	Message    string       `json:"message"`
	Policy     *objects.Ref `json:"policy,omitempty"`
}

// EvidenceKind implements Evidence.
func (*RuleReplacedEvidence) EvidenceKind() string { return EvidenceRuleReplaced }

// MarshalJSON adds the kind tag.
func (e *RuleReplacedEvidence) MarshalJSON() ([]byte, error) {
	type plain RuleReplacedEvidence
	return json.Marshal(struct {
		Kind string `json:"kind"`
		*plain
	}{EvidenceRuleReplaced, (*plain)(e)})
}

var (
	// ruleMessage is the controller's "Replaced Rule (%d): …" or "Dropped Rule (%d): …".
	ruleMessage = regexp.MustCompile(`(?s)^(Replaced|Dropped) Rule \((\d+)\): (.*)$`)
	// policyToken is a group/Kind/namespace/name token in the text after the colon.
	policyToken = regexp.MustCompile(`([a-z0-9][-a-z0-9.]*)/([A-Z][A-Za-z0-9]*)/([a-z0-9][-a-z0-9]*)/([a-z0-9][-a-z0-9.]*[a-z0-9]|[a-z0-9])`)
)

// ruleReplacedReason reports whether cond is a current RouteRuleReplaced or
// RouteRuleDropped: on Accepted on 2.3, on kgateway.dev/Programmed on 2.4.
func ruleReplacedReason(cond metav1.Condition) bool {
	return cond.Status == metav1.ConditionFalse && (cond.Type == "Accepted" || cond.Type == "kgateway.dev/Programmed") &&
		(cond.Reason == "RouteRuleReplaced" || cond.Reason == "RouteRuleDropped")
}

// ruleFinding returns the 015 for cond on a parent of route u, or false for a
// condition another code keeps. owner is the parent's key. wake asks for a
// recompute when the hint becomes due.
func (in Input) ruleFinding(u *unstructured.Unstructured, cond metav1.Condition, extra string, owner []string, wake func(time.Time)) (Finding, bool) {
	if !ruleReplacedReason(cond) {
		return Finding{}, false
	}
	ev := &RuleReplacedEvidence{Reason: cond.Reason, Message: computed.ClipDisplay(cond.Message, computed.MaxStatusBytes)}
	match := ""
	if m := ruleMessage.FindStringSubmatch(cond.Message); m != nil {
		if n, err := strconv.Atoi(m[2]); err == nil {
			ev.MatchIndex, match = &n, m[2]
			ev.Policy = in.namedPolicy(m[3])
		}
	}
	f := Finding{Code: CodeRuleReplaced, Slug: "rule-replaced", Severity: SeverityError, Install: in.Install.ID(), Resource: ref(u),
		Message: message(cond), Detail: detail(cond, extra), Since: since(cond), Facts: ev,
		Key: append(append([]string(nil), owner...), match, cond.Type)}
	if ev.Policy != nil {
		f.Evidence = []objects.Ref{*ev.Policy}
	}
	// The hint counts from since, the condition's lastTransitionTime (R77).
	if f.Since != nil && cond.Reason == "RouteRuleReplaced" {
		due := f.Since.Add(RuleReplacedHintAge)
		if in.Now.After(due) {
			f.Detail += ". " + RuleReplacedHint
		} else {
			wake(due.Add(time.Nanosecond))
		}
	}
	return f, true
}

// namedPolicy returns the one served policy kind text names, or nil when it
// names none or several. Without complete discovery no kind is known served (R77).
func (in Input) namedPolicy(text string) *objects.Ref {
	if !in.Catalog.Complete() {
		return nil
	}
	var found *objects.Ref
	for _, m := range policyToken.FindAllStringSubmatch(text, -1) {
		r := objects.Ref{Group: m[1], Kind: m[2], Namespace: m[3], Name: m[4]}
		if !in.policyKind(r.Group, r.Kind) || !in.Catalog.Served(r.Group, r.Kind) {
			continue
		}
		if found != nil && *found != r {
			return nil
		}
		found = &r
	}
	return found
}

// policyKind reports whether group and kind are a policy kind this UI serves.
func (in Input) policyKind(group, kind string) bool {
	for _, s := range kinds.ByCategory(in.Served, kinds.CategoryPolicy) {
		if s.Group == group && s.Kind == kind && !s.ListOnly {
			return true
		}
	}
	return false
}
