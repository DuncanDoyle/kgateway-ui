package proxystats

import (
	"encoding/binary"
	"hash"
	"hash/fnv"
	"slices"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// The collected key checks declare (design §8.8).
const (
	CollectorName = "proxy-stats"
	ValueKey      = "stats"
	Key           = CollectorName + "/" + ValueKey
)

// Stats is the value under Key: per install ID, its Gateways in Ref order.
// It holds observed rejects, not counters, so steady rejects keep one value.
type Stats map[string][]Gateway

// Gateway is one Gateway's proxies.
type Gateway struct {
	Gateway objects.Ref
	// Observed is false when the source knows no proxies for the Gateway, as
	// for a self-managed one; that is "not observed", never failing.
	Observed bool
	// Pods are in name order. A Pod with Err is failing; the others keep their stats.
	Pods []Pod
}

// Pod is one proxy's latest scrape. Nil fields were not reported.
type Pod struct {
	UID, Name string
	// Err is a fixed-text reason when the scrape failed; the other fields are then empty.
	Err                               string
	Connected                         *bool
	WarmingListeners, WarmingClusters *int64
	ServerState                       string
	// LDS and CDS say whether update_rejected grew since this UID's previous scrape.
	LDS, CDS Rejects
	// RDS and Clusters are the Envoy names whose update_rejected grew since
	// the previous scrape, sorted. Names are evidence, never Gateway API objects.
	RDS, Clusters []string
	// RDSTruncated and ClustersTruncated are set when names were dropped from that list.
	RDSTruncated, ClustersTruncated bool
}

// Rejects are a type's rejects observed since the previous scrape; never
// "rejecting now" (review finding 4). Since starts the current run of growth.
type Rejects struct {
	Seen  bool
	Since time.Time
}

// fingerprint is FNV-64a over st in install ID order; Gateways and Pods are
// already sorted. It is never zero.
func fingerprint(st Stats) uint64 {
	h := fnv.New64a()
	w := writer{h: h}
	ids := make([]string, 0, len(st))
	for id := range st {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		w.str(id)
		w.u64(uint64(len(st[id])))
		for _, g := range st[id] {
			w.str(g.Gateway.Group)
			w.str(g.Gateway.Kind)
			w.str(g.Gateway.Namespace)
			w.str(g.Gateway.Name)
			w.bool(g.Observed)
			w.u64(uint64(len(g.Pods)))
			for _, p := range g.Pods {
				w.pod(p)
			}
		}
	}
	return h.Sum64() | 1
}

// writer feeds length-prefixed fields to h, so adjacent values stay apart.
type writer struct {
	h hash.Hash64
	b [8]byte
}

func (w *writer) u64(v uint64) {
	binary.LittleEndian.PutUint64(w.b[:], v)
	_, _ = w.h.Write(w.b[:])
}

func (w *writer) str(s string) {
	w.u64(uint64(len(s)))
	_, _ = w.h.Write([]byte(s))
}

func (w *writer) bool(b bool) {
	if b {
		w.u64(1)
	} else {
		w.u64(0)
	}
}

func (w *writer) opt(v *int64) {
	w.bool(v != nil)
	if v != nil {
		w.u64(uint64(*v)) //nolint:gosec // hashed bits; the sign does not matter
	}
}

func (w *writer) strs(ss []string) {
	w.u64(uint64(len(ss)))
	for _, s := range ss {
		w.str(s)
	}
}

func (w *writer) rejects(r Rejects) {
	w.bool(r.Seen)
	if !r.Since.IsZero() {
		w.u64(uint64(r.Since.UnixNano())) //nolint:gosec // hashed bits
	}
}

func (w *writer) pod(p Pod) {
	w.str(p.UID)
	w.str(p.Name)
	w.str(p.Err)
	w.bool(p.Connected != nil)
	if p.Connected != nil {
		w.bool(*p.Connected)
	}
	w.opt(p.WarmingListeners)
	w.opt(p.WarmingClusters)
	w.str(p.ServerState)
	w.rejects(p.LDS)
	w.rejects(p.CDS)
	w.strs(p.RDS)
	w.strs(p.Clusters)
	w.bool(p.RDSTruncated)
	w.bool(p.ClustersTruncated)
}
