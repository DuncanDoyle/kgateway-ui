package kgw24

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/kgw23"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// childOf returns a ListenerSet whose parent is g.
func childOf(g computed.Resource, ns, name string, acc computed.Acceptance) computed.Resource {
	r := lset(ns, name, acc)
	r.Parent = g.Ref
	return r
}

func TestIsolationNote(t *testing.T) {
	b, c, e := lis("b", "HTTP", 8080), lis("c", "HTTP", 8080), lis("e", "HTTP", 8080)
	b.Hostname, c.Hostname, e.Hostname = "api.example.com", "*.example.com", "*.com"
	g := gw("infra", "edge", lis("a", "HTTP", 8080), b, c, lis("d", "HTTPS", 8443))
	// e on a ListenerSet shares g's port, so it covers b and c across resources.
	ls := childOf(g, "infra", "extra", computed.Accepted)
	ls.Listeners = append(ls.Listeners, e)
	// f is alone on its Gateway's port: a denied ListenerSet's listener does not count.
	g2 := gw("infra", "other", lis("f", "HTTP", 8080))
	denied := childOf(g2, "infra", "denied", computed.Denied)
	z := lis("z", "HTTP", 8080)
	z.Hostname = "z.example.org"
	denied.Listeners = append(denied.Listeners, z)
	// Equal wildcards do not precede each other.
	w1, w2 := lis("w1", "HTTP", 80), lis("w2", "HTTP", 80)
	w1.Hostname, w2.Hostname = "*.example.org", "*.example.org"
	g3 := gw("infra", "third", w1, w2)
	// Two empty hostnames do not precede each other either.
	g6 := gw("infra", "empties", lis("e1", "HTTP", 80), lis("e2", "HTTP", 80))
	// A ListenerSet with a zero Parent neither feeds nor receives notes, even on a shared port.
	orphan := lset("infra", "orphan", computed.Accepted, lis("o1", "HTTP", 7070), withHostname(lis("o2", "HTTP", 7070), "o.example.com"))
	orphan2 := lset("infra", "orphan2", computed.Accepted, lis("o3", "HTTP", 7070))
	// A ListenerSet without status neither feeds p nor receives a note on q1.
	g4 := gw("infra", "fourth", lis("p", "HTTP", 8080))
	nostat := childOf(g4, "infra", "nostat", computed.NoStatus)
	nostat.Listeners = append(nostat.Listeners, lis("q1", "HTTP", 8081),
		withHostname(lis("q2", "HTTP", 8080), "q.example.net"), withHostname(lis("q3", "HTTP", 8081), "r.example.net"))
	// A listener kgateway rejected (current Accepted=False) does not feed m; a stale rejection still feeds m2.
	g5 := gw("infra", "fifth", lis("m", "HTTP", 8080), withHostname(lis("n", "HTTP", 8080), "n.example.net"),
		lis("m2", "HTTP", 8081), withHostname(lis("n2", "HTTP", 8081), "n2.example.net"))
	g5.Generation = 2
	g5.ListenerStatus = []objects.ListenerStatus{
		{Name: "n", Conditions: []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionFalse, Reason: "HostnameConflict", ObservedGeneration: 2}}},
		{Name: "n2", Conditions: []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionFalse, Reason: "HostnameConflict", ObservedGeneration: 1}}},
	}

	r := withHosts(route("HTTPRoute", "infra", "r", pref(g, "")), "api.example.com")
	in := computed.AttachmentInput{
		Resources: []computed.Resource{g, ls, g2, denied, g3, g6, orphan, orphan2, g4, nostat, g5},
		Routes:    []computed.Route{r, route("HTTPRoute", "infra", "r2", pref(ls, "")), route("HTTPRoute", "infra", "r3", pref(g2, ""))},
	}
	// One NamespacesOK for both rule sets, so only the note may differ.
	in.NamespacesOK = true
	res := New().Attachment(in)
	want := map[string]bool{"edge/a": true, "edge/c": true, "extra/e": true, "fifth/m2": true}
	for _, lc := range res.Counts {
		if got := lc.IsolationNote; got != want[lc.Resource.Name+"/"+lc.Listener] {
			t.Errorf("%s/%s: note %v", lc.Resource.Name, lc.Listener, got)
		}
	}

	// The count never changes, and 2.3 has no note anywhere.
	old := kgw23.New().Attachment(in)
	for _, lc := range old.Counts {
		if lc.IsolationNote {
			t.Errorf("kgw23 %s/%s: note set", lc.Resource.Name, lc.Listener)
		}
	}
	for i := range res.Counts {
		res.Counts[i].IsolationNote = false
	}
	if !reflect.DeepEqual(res, old) {
		t.Errorf("kgw24 result differs from kgw23 beyond the note:\n%+v\n%+v", res, old)
	}
}

// withHostname sets l's hostname.
func withHostname(l objects.Listener, h string) objects.Listener {
	l.Hostname = h
	return l
}

// A listener whose count is unknown gets no note (any reason): under unknown
// scope no listener is noted; with unavailable labels only the Selector
// listener loses its note, and it still narrows the others.
func TestIsolationNoteSkipsUnknownCounts(t *testing.T) {
	sel := withFrom(lis("a", "HTTP", 8080), "Selector", map[string]string{"team": "web"})
	g := gw("infra", "edge", sel, lis("b", "HTTP", 8080), withHostname(lis("c", "HTTP", 8080), "c.example.com"))
	rt := route("HTTPRoute", "infra", "r", pref(g, ""))
	notes := func(in computed.AttachmentInput) map[string]bool {
		out := map[string]bool{}
		for _, c := range New().Attachment(in).Counts {
			out[c.Listener] = c.IsolationNote
		}
		return out
	}
	in := computed.AttachmentInput{Resources: []computed.Resource{g}, Routes: []computed.Route{rt}}
	if got := notes(in); !reflect.DeepEqual(got, map[string]bool{"a": false, "b": true, "c": false}) {
		t.Fatalf("labels unavailable: %v", got)
	}
	in.NamespacesOK, in.NamespaceLabels = true, map[string]map[string]string{"infra": {"team": "web"}}
	if got := notes(in); !reflect.DeepEqual(got, map[string]bool{"a": true, "b": true, "c": false}) {
		t.Fatalf("labels known: %v", got)
	}
	in.ScopeUnknown = true
	if got := notes(in); !reflect.DeepEqual(got, map[string]bool{"a": false, "b": false, "c": false}) {
		t.Fatalf("scope unknown: %v", got)
	}
}
