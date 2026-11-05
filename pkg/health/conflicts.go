package health

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// CodeListenerConflicted is KGWUI-017: the controller reports a listener conflict (design §7.5).
const CodeListenerConflicted = "KGWUI-017"

// EvidenceListenerConflict is the kind of ListenerConflictEvidence.
const EvidenceListenerConflict = "listener-conflict"

// Rules that find a conflict's other side, one per Conflicted reason (design §7.5, R94).
const (
	// ListenerHostnameRule is HostnameConflict's: same port and the same hostname.
	ListenerHostnameRule = "listener port and hostname"
	// ListenerProtocolRule is ProtocolConflict's: same port and a different protocol.
	ListenerProtocolRule = "listener port and protocol"
)

// maxConflictOthers caps ListenerConflictEvidence.Others; OthersTotal counts them all.
const maxConflictOthers = 10

// Wording for KGWUI-017 (design §7.5): a lead-in per rule, then the rule.
const (
	// conflictSameHostname leads the hostname rule's other side; %s is the listeners.
	conflictSameHostname = "Same port and hostname as %s."
	// conflictPortKeeps leads the protocol rule's: the port, the kept protocols, the listeners.
	conflictPortKeeps = "Port %d keeps %s: %s."
	// conflictComputed follows either lead-in; %s is the rule.
	conflictComputed = " Computed by the UI's %s rule."
	// conflictNoHostname and conflictNoProtocol end the detail when the rule finds none.
	conflictNoHostname = "The controller reports a conflict; no other accepted listener on port %d has the same hostname."
	conflictNoProtocol = "The controller reports a conflict; no other accepted listener on port %d uses another protocol."
)

// ListenerConflictEvidence is what KGWUI-017 quotes: the controller's
// condition on the listener, and the other listeners the UI computes by Rule.
// Rule is empty for a reason no rule covers, such as a reserved port.
type ListenerConflictEvidence struct {
	Listener string `json:"listener"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	Rule     string `json:"rule,omitempty"`
	// Others holds the first maxConflictOthers; OthersTotal counts them all.
	Others      []ConflictingListener `json:"others,omitempty"`
	OthersTotal int                   `json:"othersTotal"`
}

// ConflictingListener is another listener on the same Gateway that the rule
// finds. Owner is its Gateway or ListenerSet.
type ConflictingListener struct {
	Owner    objects.Ref `json:"owner"`
	Name     string      `json:"name"`
	Protocol string      `json:"protocol,omitempty"`
	Hostname string      `json:"hostname,omitempty"`
}

// EvidenceKind implements Evidence.
func (*ListenerConflictEvidence) EvidenceKind() string { return EvidenceListenerConflict }

// MarshalJSON adds the kind tag.
func (e *ListenerConflictEvidence) MarshalJSON() ([]byte, error) {
	type plain ListenerConflictEvidence
	return json.Marshal(struct {
		Kind string `json:"kind"`
		*plain
	}{EvidenceListenerConflict, (*plain)(e)})
}

// listenerSide is a spec listener and its Gateway or ListenerSet; kept is
// whether its status says the controller kept it.
type listenerSide struct {
	owner objects.Ref
	l     objects.Listener
	kept  bool
}

// keptListener reports whether status block st of u says the controller kept
// the listener (R95): current, Accepted=True, not Conflicted, with supported kinds.
func keptListener(u *unstructured.Unstructured, st objects.ListenerStatus) bool {
	if len(st.SupportedKinds) == 0 {
		return false
	}
	for _, c := range st.Conditions {
		if objects.IsStale(u, c) {
			return false
		}
	}
	a, cf := objects.FindCondition(st.Conditions, "Accepted"), objects.FindCondition(st.Conditions, "Conflicted")
	return a != nil && a.Status == metav1.ConditionTrue && (cf == nil || cf.Status != metav1.ConditionTrue)
}

// gatewayListeners returns the spec listeners of each owned Gateway, its own
// first, then its owned ListenerSets' by ref, each in spec order. A ListenerSet
// the Gateway does not allow (current Accepted=False/NotAllowed) is left out.
func (in Input) gatewayListeners() map[objects.Ref][]listenerSide {
	out := map[objects.Ref][]listenerSide{}
	add := func(u *unstructured.Unstructured, gw objects.Ref, ls []objects.Listener, status []objects.ListenerStatus) {
		owner := objects.RefOf(u)
		for _, l := range ls {
			if l.Name == "" {
				continue
			}
			i := slices.IndexFunc(status, func(st objects.ListenerStatus) bool { return st.Name == l.Name })
			out[gw] = append(out[gw], listenerSide{owner, l, i >= 0 && keptListener(u, status[i])})
		}
	}
	for _, gw := range in.Owned.Gateways {
		v := in.views().Gateway(gw)
		add(gw, objects.RefOf(gw), v.Listeners, v.ListenerStatus)
	}
	sets := slices.Clone(in.Owned.ListenerSets)
	slices.SortFunc(sets, func(a, b *unstructured.Unstructured) int { return objects.RefOf(a).Compare(objects.RefOf(b)) })
	for _, ls := range sets {
		v := in.views().ListenerSet(ls)
		if a := objects.FindCondition(v.Conditions, "Accepted"); a != nil && a.Status == metav1.ConditionFalse && a.Reason == "NotAllowed" && !objects.IsStale(ls, *a) {
			continue
		}
		add(ls, v.Parent.Ref(), v.Listeners, v.ListenerStatus)
	}
	return out
}

// conflictRule returns the rule for a Conflicted reason, whether a kept
// listener s is its other side for l, and its no-other-side text; ok is false
// for a reason no rule covers. Per the Gateway API Listener distinctness rules
// (gateway_types.go:113-166), listeners are distinct by port and protocol, then hostname.
func conflictRule(reason string) (rule string, match func(l, s objects.Listener) bool, none string, ok bool) {
	switch reason {
	case "HostnameConflict":
		// Exact equality: a wildcard and its subdomains are distinct, and empty matches only empty.
		return ListenerHostnameRule, func(l, s objects.Listener) bool { return s.Hostname == l.Hostname }, conflictNoHostname, true
	case "ProtocolConflict":
		return ListenerProtocolRule, func(l, s objects.Listener) bool { return s.Protocol != l.Protocol }, conflictNoProtocol, true
	}
	return "", nil, "", false
}

// conflictFinding is the 017 for Conflicted condition cond on listener l of
// u; sides are every listener on u's Gateway and owner is the listener's key.
func (in Input) conflictFinding(u *unstructured.Unstructured, l objects.Listener, cond metav1.Condition, sides []listenerSide, owner []string) Finding {
	self := objects.RefOf(u)
	ev := &ListenerConflictEvidence{Listener: l.Name, Port: l.Port, Protocol: l.Protocol, Hostname: l.Hostname, Reason: cond.Reason,
		Message: computed.ClipDisplay(cond.Message, computed.MaxStatusBytes)}
	f := Finding{Code: CodeListenerConflicted, Slug: "listener-conflicted", Severity: SeverityError, Install: in.Install.ID(), Resource: &self,
		Message: message(cond), Detail: detail(cond, "listener "+l.Name), Since: since(cond), Key: slices.Clone(owner), Facts: ev}
	rule, match, none, ok := conflictRule(cond.Reason)
	if !ok {
		return f
	}
	ev.Rule = rule
	var named, protocols []string
	for _, s := range sides {
		if !s.kept || (s.owner == self && s.l.Name == l.Name) || s.l.Port != l.Port || !match(l, s.l) {
			continue
		}
		ev.OthersTotal++
		if s.owner != self && !slices.Contains(f.Evidence, s.owner) {
			f.Evidence = append(f.Evidence, s.owner)
		}
		if len(ev.Others) < maxConflictOthers {
			ev.Others = append(ev.Others, ConflictingListener{Owner: s.owner, Name: s.l.Name, Protocol: s.l.Protocol, Hostname: s.l.Hostname})
			named = append(named, fmt.Sprintf("`%s` of %s %s", s.l.Name, s.owner.Kind, s.owner))
			if !slices.Contains(protocols, s.l.Protocol) {
				protocols = append(protocols, s.l.Protocol)
			}
		}
	}
	end := fmt.Sprintf(none, l.Port)
	if len(named) > 0 {
		list := "listener " + named[0]
		if len(named) > 1 {
			list = "listeners " + strings.Join(named[:len(named)-1], ", ") + " and " + named[len(named)-1]
		}
		if more := ev.OthersTotal - len(named); more > 0 {
			list = "listeners " + strings.Join(named, ", ") + fmt.Sprintf(" and %d more", more)
		}
		end = fmt.Sprintf(conflictSameHostname, list)
		if rule == ListenerProtocolRule {
			end = fmt.Sprintf(conflictPortKeeps, l.Port, strings.Join(protocols, ", "), list)
		}
		end += fmt.Sprintf(conflictComputed, rule)
	}
	f.Detail += ". " + end
	return f
}
