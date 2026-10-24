package kgw24

import (
	"reflect"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/kgw23"
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

	r := withHosts(route("HTTPRoute", "infra", "r", pref(g, "")), "api.example.com")
	in := computed.AttachmentInput{
		Resources: []computed.Resource{g, ls, g2, denied, g3},
		Routes:    []computed.Route{r, route("HTTPRoute", "infra", "r2", pref(ls, "")), route("HTTPRoute", "infra", "r3", pref(g2, ""))},
	}
	res := run(in)
	want := map[string]bool{"edge/a": true, "edge/c": true, "extra/e": true}
	for _, lc := range res.Counts {
		if got := lc.IsolationNote; got != want[lc.Resource.Name+"/"+lc.Listener] {
			t.Errorf("%s/%s: note %v", lc.Resource.Name, lc.Listener, got)
		}
	}

	// The count never changes, and 2.3 has no note anywhere.
	in.NamespacesOK = true
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
