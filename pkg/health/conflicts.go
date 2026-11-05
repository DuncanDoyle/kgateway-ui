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

// ListenerOverlapRule names the UI's rule that finds a conflict's other side.
const ListenerOverlapRule = "listener port and hostname overlap"

// maxConflictOthers caps ListenerConflictEvidence.Others; OthersTotal counts them all.
const maxConflictOthers = 10

// Wording for KGWUI-017 (design §7.5).
const (
	// conflictOthers ends the detail when other listeners overlap: the listeners, then the port.
	conflictOthers = "Overlaps %s on port %d. Computed by the UI's " + ListenerOverlapRule + " rule."
	// conflictNone ends the detail when none does; %d is the port.
	conflictNone = "The controller reports a conflict; no other listener on port %d overlaps."
)

// ListenerConflictEvidence is what KGWUI-017 quotes: the controller's
// condition on the listener, and the other listeners the UI computes overlap
// it by ListenerOverlapRule. Others is empty when none does.
type ListenerConflictEvidence struct {
	Listener string `json:"listener"`
	Port     int32  `json:"port"`
	Hostname string `json:"hostname,omitempty"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	Rule     string `json:"rule"`
	// Others holds the first maxConflictOthers; OthersTotal counts them all.
	Others      []ConflictingListener `json:"others,omitempty"`
	OthersTotal int                   `json:"othersTotal"`
}

// ConflictingListener is another listener on the same Gateway, port and an
// overlapping hostname. Owner is its Gateway or ListenerSet.
type ConflictingListener struct {
	Owner    objects.Ref `json:"owner"`
	Name     string      `json:"name"`
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

// listenerSide is a spec listener and its Gateway or ListenerSet.
type listenerSide struct {
	owner objects.Ref
	l     objects.Listener
}

// gatewayListeners returns the spec listeners of each owned Gateway, its own
// first, then its owned ListenerSets' by ref, each in spec order.
func (in Input) gatewayListeners() map[objects.Ref][]listenerSide {
	out := map[objects.Ref][]listenerSide{}
	add := func(gw, owner objects.Ref, ls []objects.Listener) {
		for _, l := range ls {
			if l.Name != "" {
				out[gw] = append(out[gw], listenerSide{owner, l})
			}
		}
	}
	for _, gw := range in.Owned.Gateways {
		r := objects.RefOf(gw)
		add(r, r, in.views().Gateway(gw).Listeners)
	}
	sets := slices.Clone(in.Owned.ListenerSets)
	slices.SortFunc(sets, func(a, b *unstructured.Unstructured) int { return objects.RefOf(a).Compare(objects.RefOf(b)) })
	for _, ls := range sets {
		v := in.views().ListenerSet(ls)
		add(v.Parent.Ref(), objects.RefOf(ls), v.Listeners)
	}
	return out
}

// conflictFinding is the 017 for Conflicted condition cond on listener l of
// u; sides are every listener on u's Gateway and owner is the listener's key.
func (in Input) conflictFinding(u *unstructured.Unstructured, l objects.Listener, cond metav1.Condition, sides []listenerSide, owner []string) Finding {
	self := objects.RefOf(u)
	ev := &ListenerConflictEvidence{Listener: l.Name, Port: l.Port, Hostname: l.Hostname, Reason: cond.Reason,
		Message: computed.ClipDisplay(cond.Message, computed.MaxStatusBytes), Rule: ListenerOverlapRule}
	var deps []objects.Ref
	var named []string
	for _, s := range sides {
		if (s.owner == self && s.l.Name == l.Name) || s.l.Port != l.Port || !hostnamesOverlap(s.l.Hostname, l.Hostname) {
			continue
		}
		ev.OthersTotal++
		if s.owner != self && !slices.Contains(deps, s.owner) {
			deps = append(deps, s.owner)
		}
		if len(ev.Others) < maxConflictOthers {
			ev.Others = append(ev.Others, ConflictingListener{Owner: s.owner, Name: s.l.Name, Hostname: s.l.Hostname})
			named = append(named, fmt.Sprintf("`%s` of %s %s", s.l.Name, s.owner.Kind, s.owner))
		}
	}
	end := fmt.Sprintf(conflictNone, l.Port)
	if len(named) > 0 {
		list := "listener " + named[0]
		if len(named) > 1 {
			list = "listeners " + strings.Join(named[:len(named)-1], ", ") + " and " + named[len(named)-1]
		}
		if more := ev.OthersTotal - len(named); more > 0 {
			list = "listeners " + strings.Join(named, ", ") + fmt.Sprintf(" and %d more", more)
		}
		end = fmt.Sprintf(conflictOthers, list, l.Port)
	}
	return Finding{Code: CodeListenerConflicted, Slug: "listener-conflicted", Severity: SeverityError, Install: in.Install.ID(), Resource: &self,
		Message: message(cond), Detail: detail(cond, "listener "+l.Name) + ". " + end, Since: since(cond),
		Key: slices.Clone(owner), Facts: ev, Evidence: deps}
}

// hostnamesOverlap reports whether one request host can match both listener
// hostnames. Per the Gateway API Listener.hostname spec, an empty hostname
// matches every host, and "*.example.com" matches hosts with one or more labels
// before "example.com", never "example.com" itself
// (https://gateway-api.sigs.k8s.io/reference/spec/#listener).
func hostnamesOverlap(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	a, b = strings.ToLower(a), strings.ToLower(b)
	wa, wb := strings.HasPrefix(a, "*."), strings.HasPrefix(b, "*.")
	switch {
	case wa && wb:
		return strings.HasSuffix(a[1:], b[1:]) || strings.HasSuffix(b[1:], a[1:])
	case wa:
		return strings.HasSuffix(b, a[1:])
	case wb:
		return strings.HasSuffix(a, b[1:])
	}
	return a == b
}
