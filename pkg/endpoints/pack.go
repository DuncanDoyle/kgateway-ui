package endpoints

import (
	"math"
	"net/netip"
	"slices"
	"time"
)

// packed is a remembered Result in 12 bytes per item plus one arena. IP
// addresses are binary, and each address and Pod name keeps only its suffix
// after the prefix it shares with the previous item's (design §9.3, §14).
type packed struct {
	arena    []byte
	rows     []row
	ports    []portKey
	complete string
	readAt   time.Time
}

// row is one Endpoint. At off are its address suffix (addrLen bytes) and
// its Pod name suffix (podLen bytes); the shared fields count the prefix bytes taken from the previous row.
type row struct {
	off                                           uint32
	port                                          uint16
	addrShared, addrLen, podShared, podLen, flags uint8
}

type portKey struct {
	name string
	port int32
}

// The bits of row.flags; an address without ip4 or ip6 is text, such as an FQDN.
const (
	ready uint8 = 1 << iota
	serving
	terminating
	ip4
	ip6
)

// packedOverhead is a memo entry's fixed size beyond its slices: the entry,
// its list element and map slot, rounded up.
const packedOverhead = 512

// size is what p retains, for the memo's byte budget: a row is 12 bytes and a portKey 24.
func (p *packed) size() int {
	n := cap(p.arena) + 12*cap(p.rows) + 24*cap(p.ports) + packedOverhead
	for _, k := range p.ports {
		n += len(k.name)
	}
	return n
}

// pack stores res, and reports false when res has more distinct ports than
// a row can index. Strings longer than 255 bytes, which valid addresses and Pod names never are, are cut.
func pack(res Result) (packed, bool) {
	// slices.Grow rounds a capacity up to the allocation's real size, which size counts.
	p := packed{complete: res.Complete, readAt: res.ReadAt, rows: slices.Grow([]row(nil), len(res.Items))[:len(res.Items)]}
	size := 0
	var prevAddr, prevPod []byte
	for _, e := range res.Items {
		_, a := addrBytes(e.Address)
		pod := cut(e.Pod)
		size += len(a) - shared(prevAddr, a) + len(pod) - shared(prevPod, pod)
		prevAddr, prevPod = a, pod
	}
	p.arena = slices.Grow([]byte(nil), size)
	prevAddr, prevPod = nil, nil
	for i, e := range res.Items {
		flag, a := addrBytes(e.Address)
		pod := cut(e.Pod)
		r := row{off: uint32(len(p.arena)), flags: flag}                                    //nolint:gosec // the arena holds at most 510 bytes per item
		r.addrShared, r.podShared = uint8(shared(prevAddr, a)), uint8(shared(prevPod, pod)) //nolint:gosec // both are cut to 255 bytes
		p.arena = append(p.arena, a[r.addrShared:]...)
		p.arena = append(p.arena, pod[r.podShared:]...)
		r.addrLen, r.podLen = uint8(len(a)-int(r.addrShared)), uint8(len(pod)-int(r.podShared)) //nolint:gosec // both are cut to 255 bytes
		prevAddr, prevPod = a, pod
		k := portKey{e.PortName, e.Port}
		j := slices.Index(p.ports, k)
		if j < 0 {
			if len(p.ports) > math.MaxUint16 {
				return packed{}, false
			}
			j = len(p.ports)
			p.ports = append(p.ports, k)
		}
		r.port = uint16(j) //nolint:gosec // j ≤ MaxUint16, checked above
		for _, c := range [...]struct {
			on  bool
			bit uint8
		}{{e.Ready, ready}, {e.Serving, serving}, {e.Terminating, terminating}} {
			if c.on {
				r.flags |= c.bit
			}
		}
		p.rows[i] = r
	}
	return p, true
}

// addrBytes is an address as stored: an IP in binary with its flag when that
// prints back as s, else its text.
func addrBytes(s string) (uint8, []byte) {
	if a, err := netip.ParseAddr(s); err == nil && a.Zone() == "" && a.String() == s {
		if a.Is4() {
			return ip4, a.AsSlice()
		}
		return ip6, a.AsSlice()
	}
	return 0, cut(s)
}

func cut(s string) []byte { return []byte(s[:min(len(s), 255)]) }

// shared is the length of a and b's common prefix.
func shared(a, b []byte) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// unpack rebuilds the Result.
func (p *packed) unpack() Result {
	res := Result{Complete: p.complete, ReadAt: p.readAt, Items: make([]Endpoint, len(p.rows))}
	var prevAddr, prevPod []byte
	for i, r := range p.rows {
		at := p.arena[r.off:]
		a := append(prevAddr[:r.addrShared:r.addrShared], at[:r.addrLen]...)
		pod := append(prevPod[:r.podShared:r.podShared], at[r.addrLen:int(r.addrLen)+int(r.podLen)]...)
		addr := string(a)
		if r.flags&(ip4|ip6) != 0 {
			ip, _ := netip.AddrFromSlice(a)
			addr = ip.String()
		}
		k := p.ports[r.port]
		res.Items[i] = Endpoint{Address: addr, Port: k.port, PortName: k.name, Ready: r.flags&ready != 0,
			Serving: r.flags&serving != 0, Terminating: r.flags&terminating != 0, Pod: string(pod)}
		prevAddr, prevPod = a, pod
	}
	return res
}
