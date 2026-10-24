package kgw24

import (
	"strings"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// portKey is one port of one Gateway, with its allowed ListenerSets merged in.
type portKey struct {
	gateway objects.Ref
	port    int32
}

// portHosts summarises the plain-HTTP listener hostnames on one portKey.
type portHosts struct {
	// named counts hostnames; an empty-hostname listener yields to any of them.
	named int
	// within holds each ".x" for which a hostname is strictly inside "*.x", so it precedes that wildcard.
	within map[string]bool
}

// isolationNotes notes each plain-HTTP listener that a more specific one on the same Gateway port
// may narrow (rules §4.5; kgateway V4 gateway_listener_translator.go:689-747). Counts are unchanged.
func isolationNotes(in computed.AttachmentInput, res *computed.AttachmentResult) {
	ports := map[portKey]*portHosts{}
	eachMerged(in, func(k portKey, _ *computed.Resource, l *objects.Listener) {
		if l.Hostname == "" {
			return
		}
		p := ports[k]
		if p == nil {
			p = &portHosts{within: map[string]bool{}}
			ports[k] = p
		}
		p.named++
		// A wildcard's own suffix is not strictly within it; start after it.
		h := strings.TrimPrefix(l.Hostname, "*.")
		for i := strings.IndexByte(h, '.'); i >= 0; i = nextDot(h, i) {
			p.within[h[i:]] = true
		}
	})
	if len(ports) == 0 {
		return
	}

	type listenerKey struct {
		resource objects.Ref
		name     string
	}
	noted := map[listenerKey]bool{}
	eachMerged(in, func(k portKey, r *computed.Resource, l *objects.Listener) {
		p := ports[k]
		switch {
		case p == nil:
		case l.Hostname == "" && p.named > 0,
			strings.HasPrefix(l.Hostname, "*.") && p.within[l.Hostname[1:]]:
			noted[listenerKey{r.Ref, l.Name}] = true
		}
	})
	for i := range res.Counts {
		c := &res.Counts[i]
		// An unknown count gets no note, whatever the reason.
		c.IsolationNote = c.Unknown == "" && noted[listenerKey{c.Resource, c.Listener}]
	}
}

// eachMerged calls f for each plain-HTTP listener kgateway merges into a Gateway port: not on a
// denied, statusless or parentless ListenerSet, and not rejected by its current status (V4 :62-63).
func eachMerged(in computed.AttachmentInput, f func(portKey, *computed.Resource, *objects.Listener)) {
	for i := range in.Resources {
		r := &in.Resources[i]
		gateway := r.Ref
		if r.Ref.Kind != "Gateway" {
			gateway = r.Parent
		}
		if r.Acceptance != computed.Accepted || gateway == (objects.Ref{}) {
			continue
		}
		for j := range r.Listeners {
			l := &r.Listeners[j]
			if l.Protocol == "HTTP" && !rejected(r, l.Name) {
				f(portKey{gateway, l.Port}, r, l)
			}
		}
	}
}

// rejected reports a current Accepted=False on the listener, which kgateway drops before merging.
func rejected(r *computed.Resource, name string) bool {
	for _, st := range r.ListenerStatus {
		if st.Name != name {
			continue
		}
		c := objects.FindCondition(st.Conditions, "Accepted")
		return c != nil && c.Status == "False" && c.ObservedGeneration >= r.Generation
	}
	return false
}

// nextDot returns the index of the next '.' in h after i, or -1.
func nextDot(h string, i int) int {
	j := strings.IndexByte(h[i+1:], '.')
	if j < 0 {
		return -1
	}
	return i + 1 + j
}
