package endpoints

import (
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

// pack stores res; strings longer than 255 bytes, which valid addresses and Pod names never are, are cut.
func pack(res Result) packed {
	p := packed{complete: res.Complete, readAt: res.ReadAt, rows: make([]row, len(res.Items))}
	size := 0
	var prevAddr, prevPod []byte
	for _, e := range res.Items {
		_, a := addrBytes(e.Address)
		pod := cut(e.Pod)
		size += len(a) - shared(prevAddr, a) + len(pod) - shared(prevPod, pod)
		prevAddr, prevPod = a, pod
	}
	p.arena = make([]byte, 0, size)
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
			j = len(p.ports)
			p.ports = append(p.ports, k)
		}
		r.port = uint16(j) //nolint:gosec // a read holds at most MaxEndpoints, far fewer distinct ports
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
	return p
}

// addrBytes is an address as stored: an IP in binary with its flag, else its text.
func addrBytes(s string) (uint8, []byte) {
	if a, err := netip.ParseAddr(s); err == nil && a.Zone() == "" {
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
