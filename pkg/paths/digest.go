package paths

import (
	"math"
	"strconv"

	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// fnv1a is a 64-bit FNV-1a hash that hashes strings without allocating.
type fnv1a uint64

const (
	fnvOffset fnv1a = 14695981039346656037
	fnvPrime  fnv1a = 1099511628211
)

// bytes hashes b and a 0 terminator.
func (h *fnv1a) bytes(b []byte) {
	for _, c := range b {
		*h = (*h ^ fnv1a(c)) * fnvPrime
	}
	*h *= fnvPrime
}

// str hashes s and a 0 terminator.
func (h *fnv1a) str(s string) {
	for i := 0; i < len(s); i++ {
		*h = (*h ^ fnv1a(s[i])) * fnvPrime
	}
	*h *= fnvPrime
}

// num hashes n's decimal text, as str would.
func (h *fnv1a) num(n int64) {
	var b [20]byte
	h.bytes(strconv.AppendInt(b[:0], n, 10))
}

// word mixes n in whole: a multiply and a shift per word, not per byte.
func (h *fnv1a) word(n uint64) {
	*h = (*h ^ fnv1a(n)) * 0x9e3779b97f4a7c15
	*h ^= *h >> 29
}

// count hashes *n, or "-" when n is nil.
func (h *fnv1a) count(n *int) {
	if n == nil {
		h.str("-")
		return
	}
	h.num(int64(*n))
}

func (h *fnv1a) ref(r objects.Ref) { h.str(r.Group); h.str(r.Kind); h.str(r.Namespace); h.str(r.Name) }

func (h *fnv1a) keys(ks []objects.KeyMatch) {
	for _, k := range ks {
		h.str(k.Type)
		h.str(k.Name)
		h.str(k.Value)
	}
}

// matches hashes HTTP matches; RowID's identity hashes the same way.
func (h *fnv1a) matches(ms []objects.Match) {
	for _, m := range ms {
		h.str("m")
		if m.Path != nil {
			h.str(m.Path.Type)
			h.str(m.Path.Value)
		}
		h.keys(m.Headers)
		h.str("")
		h.str("")
		h.str("")
		h.keys(m.QueryParams)
		h.str(m.Method)
	}
}

// digest is row i's content as a client shows it: its identity id, listener,
// hostnames, rule, target, view C state, problem, share and endpoint counts.
// Equal content gives an equal digest, in any process; endpoint read times are left out.
func (p *projector) digest(r Row, id uint64, out *Projection, i int) uint64 {
	t := p.t
	h := fnvOffset
	h.word(id)
	h.num(int64(r.dup))
	ln := t.Listener(r)
	h.str(ln.Protocol)
	h.num(int64(ln.Port))
	hosts := t.Hostnames(r)
	h.num(int64(len(hosts)))
	for _, s := range hosts {
		h.str(s)
	}
	h.num(int64(t.Action(r)))
	rule := t.Rule(r)
	h.num(int64(rule.Index))
	h.matches(rule.Matches)
	for _, m := range rule.GRPCMatches {
		h.str("g")
		if m.Method != nil {
			h.str(m.Method.Type)
			h.str(m.Method.Service)
			h.str(m.Method.Method)
		}
		h.keys(m.Headers)
	}
	if mf, ok := t.Mirror(r); ok {
		h.ref(mf.Backend)
		h.num(int64(mf.Port))
	} else if b, ok := t.Backend(r); ok {
		h.ref(b.Ref)
		h.num(int64(b.Port))
		h.num(int64(b.Weight))
		if dl := b.Delegation; dl != nil {
			h.str(string(dl.Mode))
			h.str(dl.Namespace)
			h.str(dl.Name)
			h.str(dl.Label)
		}
	}
	c := &t.ctxs[r.ctx]
	h.num(int64(c.flags))
	if c.unknown > 0 {
		h.str(string(t.unknowns[c.unknown-1]))
	}
	pr := out.Problems[i]
	h.str(string(pr.State))
	h.word(p.setDigest(pr.Signals))
	sh := out.Shares[i]
	h.str(string(sh.State))
	h.word(math.Float64bits(sh.Percent))
	if e := out.Endpoints[i]; e != nil {
		h.str(e.Complete)
		h.count(e.Ready)
		h.count(e.Total)
		h.num(int64(e.Port))
		h.str(e.PortName)
	}
	return uint64(h)
}

// title is the message the API shows as a finding signal's title: the first
// finding on ref with code and a message, as pkg/api's signalTitles picks it.
func (p *projector) title(code string, ref objects.Ref) string {
	for _, f := range p.findings[ref] {
		if f.Code == code && f.Message != "" {
			return f.Message
		}
	}
	return ""
}

// setDigest hashes an interned signal set, finding titles included, once per projection.
func (p *projector) setDigest(sigs []Signal) uint64 {
	if len(sigs) == 0 {
		return 0
	}
	if d, ok := p.setDigests[&sigs[0]]; ok {
		return d
	}
	h := fnvOffset
	for _, s := range sigs {
		h.str(s.Kind)
		h.str(s.Code)
		h.ref(s.Ref)
		h.str(string(s.State))
		h.str(s.Listener)
		if s.Kind == SignalFinding {
			h.str(p.title(s.Code, s.Ref))
		}
	}
	p.setDigests[&sigs[0]] = uint64(h)
	return uint64(h)
}

// Digest is the ordered digest of rows: each row's content in that order.
// Two results digest alike only when a client would show them alike.
func (p *Projection) Digest(rows []int32) uint64 {
	h := fnvOffset
	for _, i := range rows {
		h.word(p.digests[i])
	}
	return uint64(h)
}
