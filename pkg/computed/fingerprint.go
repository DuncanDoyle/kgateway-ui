package computed

import (
	"encoding/binary"
	"hash"
	"hash/fnv"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// printer builds canonical encodings and hashes them with FNV-64a. Its
// buffers are reused across prints, so one printer serves one call.
type printer struct {
	h    hash.Hash64
	b    []byte
	keys []string
}

func newPrinter() *printer { return &printer{h: fnv.New64a()} }

// start resets the encoding buffer.
func (p *printer) start() { p.b = p.b[:0] }

// sum hashes the current encoding.
func (p *printer) sum() uint64 {
	p.h.Reset()
	_, _ = p.h.Write(p.b)
	return p.h.Sum64()
}

// str writes s with its length, so adjacent strings never run together.
func (p *printer) str(s string) {
	p.b = binary.AppendUvarint(p.b, uint64(len(s)))
	p.b = append(p.b, s...)
}

func (p *printer) num(n int64)  { p.b = binary.AppendVarint(p.b, n) }
func (p *printer) u64(n uint64) { p.b = binary.LittleEndian.AppendUint64(p.b, n) }

func (p *printer) flag(on bool) {
	if on {
		p.b = append(p.b, 1)
	} else {
		p.b = append(p.b, 0)
	}
}

func (p *printer) strs(s []string) {
	p.num(int64(len(s)))
	for _, v := range s {
		p.str(v)
	}
}

// ref writes an object reference.
func (p *printer) ref(r objects.Ref) {
	p.str(r.Group)
	p.str(r.Kind)
	p.str(r.Namespace)
	p.str(r.Name)
}

// reportKey writes a report key.
func (p *printer) reportKey(k ReportKey) {
	p.str(k.Group)
	p.str(k.Kind)
	p.str(k.Namespace)
	p.str(k.Name)
	p.str(k.SectionName)
}

// match writes an HTTPRouteMatch: path (nil or type and value), headers,
// query params and method.
func (p *printer) match(m objects.Match) {
	p.flag(m.Path != nil)
	if m.Path != nil {
		p.str(m.Path.Type)
		p.str(m.Path.Value)
	}
	for _, kms := range [][]objects.KeyMatch{m.Headers, m.QueryParams} {
		p.num(int64(len(kms)))
		for _, km := range kms {
			p.str(km.Type)
			p.str(km.Name)
			p.str(km.Value)
		}
	}
	p.str(m.Method)
}

// labels writes a label map in key order.
func (p *printer) labels(m map[string]string) {
	p.keys = p.keys[:0]
	for k := range m {
		p.keys = append(p.keys, k)
	}
	slices.Sort(p.keys)
	p.num(int64(len(p.keys)))
	for _, k := range p.keys {
		p.str(k)
		p.str(m[k])
	}
}

// listener writes a listener's spec as view A reads it: name, port, protocol,
// hostname, TLS mode (it decides the default kinds, rules §4.2) and allowedRoutes.
func (p *printer) listener(l *objects.Listener) {
	p.str(l.Name)
	p.num(int64(l.Port))
	p.str(l.Protocol)
	p.str(l.Hostname)
	p.flag(l.TLS != nil)
	if l.TLS != nil {
		p.str(l.TLS.Mode)
	}
	ns := l.AllowedRoutes.Namespaces
	p.str(ns.From)
	p.flag(ns.Selector != nil)
	if ns.Selector != nil {
		p.labels(ns.Selector.MatchLabels)
		p.num(int64(len(ns.Selector.MatchExpressions)))
		for _, e := range ns.Selector.MatchExpressions {
			p.str(e.Key)
			p.str(string(e.Operator))
			p.strs(e.Values)
		}
	}
	p.flag(l.AllowedRoutes.Kinds != nil)
	p.num(int64(len(l.AllowedRoutes.Kinds)))
	for _, k := range l.AllowedRoutes.Kinds {
		p.str(k.Group)
		p.str(k.Kind)
	}
}

// parentRef writes a resolved parentRef.
func (p *printer) parentRef(r objects.ParentRef) {
	p.str(r.Group)
	p.str(r.Kind)
	p.str(r.Namespace)
	p.str(r.Name)
	p.str(r.SectionName)
	p.num(int64(r.Port))
}

// route writes a route's kind, namespace, hostnames and its parentRefs to res, in spec order.
func (p *printer) route(rt *Route, res objects.Ref) {
	p.str(rt.Ref.Kind)
	p.str(rt.Ref.Namespace)
	p.strs(rt.Hostnames)
	for _, pr := range rt.ParentRefs {
		if pr.Ref() == res {
			p.parentRef(pr)
		}
	}
}

// experimental writes the parsed setting: unknown, false or true.
func (p *printer) experimental(s install.Setting) {
	v, known := s.Bool()
	p.flag(known)
	p.flag(v)
}

// listenerCountPrint is the listener-count fingerprint (rules §9.2). routes is
// the hash of the candidate route multiset, nsLabels that of the candidate
// namespaces' labels (used only when the listener uses Selector); st is the
// listener's status entry or nil.
func listenerCountPrint(p *printer, r *Resource, l *objects.Listener, routes, nsLabels uint64,
	exp install.Setting, st *objects.ListenerStatus,
) uint64 {
	p.start()
	p.listener(l)
	p.flag(r.Ref.Kind == "ListenerSet")
	// Defensive: a denied or status-less ListenerSet has no check or an unknown one (rules §9.2).
	p.num(int64(r.Acceptance))
	p.u64(routes)
	if l.AllowedRoutes.Namespaces.From == "Selector" {
		p.u64(nsLabels)
	}
	p.experimental(exp)
	p.flag(st != nil)
	if st != nil {
		p.num(int64(st.AttachedRoutes))
		p.num(minObserved(st.Conditions))
	}
	return p.sum()
}

// routeAcceptedPrint is the route-accepted fingerprint (rules §9.2). listeners
// is the hash of res's listener specs; labels is the route namespace's labels,
// read only when selector is set; acc is the key's Accepted condition or nil.
func routeAcceptedPrint(p *printer, rt *Route, res objects.Ref, listeners uint64, selector, labelsOK bool,
	labels map[string]string, acc *metav1.Condition,
) uint64 {
	p.start()
	p.route(rt, res)
	p.u64(listeners)
	p.flag(selector)
	if selector {
		p.flag(labelsOK)
		p.labels(labels)
	}
	p.flag(acc != nil)
	if acc != nil {
		p.str(string(acc.Status))
		// Another reason is compatible, so changing between them never resets the window.
		if attachmentReason(acc.Reason) {
			p.str(acc.Reason)
		} else {
			p.str("other")
		}
		p.num(acc.ObservedGeneration)
	}
	return p.sum()
}

// minObserved is the oldest observedGeneration of cs, -1 when there is none;
// a newer condition never resets the window.
func minObserved(cs []metav1.Condition) int64 {
	if len(cs) == 0 {
		return -1
	}
	m := cs[0].ObservedGeneration
	for _, c := range cs[1:] {
		m = min(m, c.ObservedGeneration)
	}
	return m
}
