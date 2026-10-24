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
	gatewayOf := func(r *computed.Resource) objects.Ref {
		if r.Ref.Kind == "Gateway" {
			return r.Ref
		}
		return r.Parent
	}
	// Denied ListenerSets are not merged into their Gateway.
	ports := map[portKey]*portHosts{}
	for i := range in.Resources {
		r := &in.Resources[i]
		if r.Acceptance == computed.Denied {
			continue
		}
		for _, l := range r.Listeners {
			if l.Protocol != "HTTP" || l.Hostname == "" {
				continue
			}
			k := portKey{gatewayOf(r), l.Port}
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
		}
	}
	if len(ports) == 0 {
		return
	}

	type listenerKey struct {
		resource objects.Ref
		name     string
	}
	noted := map[listenerKey]bool{}
	for i := range in.Resources {
		r := &in.Resources[i]
		if r.Acceptance == computed.Denied {
			continue
		}
		for _, l := range r.Listeners {
			if l.Protocol != "HTTP" {
				continue
			}
			p := ports[portKey{gatewayOf(r), l.Port}]
			switch {
			case p == nil:
			case l.Hostname == "" && p.named > 0,
				strings.HasPrefix(l.Hostname, "*.") && p.within[l.Hostname[1:]]:
				noted[listenerKey{r.Ref, l.Name}] = true
			}
		}
	}
	for i := range res.Counts {
		c := &res.Counts[i]
		c.IsolationNote = noted[listenerKey{c.Resource, c.Listener}]
	}
}

// nextDot returns the index of the next '.' in h after i, or -1.
func nextDot(h string, i int) int {
	j := strings.IndexByte(h[i+1:], '.')
	if j < 0 {
		return -1
	}
	return i + 1 + j
}
