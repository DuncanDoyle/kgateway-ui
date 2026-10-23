package cache

import (
	"maps"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// KindPrint summarises one kind's objects: their count and the wrapping sum
// of objHash over each object's (uid, resourceVersion), so order does not matter.
type KindPrint struct {
	Count int
	Hash  uint64
}

func (k *KindPrint) add(uid types.UID, rv string) {
	k.Count++
	k.Hash += objHash(uid, rv)
}

// Fingerprint identifies the source data of a snapshot (D-019 §2). Keys are
// GVR.String(), including NamespacesGVR, PodsGVR and DeploymentsGVR; every
// kind with an Access entry has a Kinds entry, empty kinds included.
type Fingerprint struct {
	Kinds  map[string]KindPrint
	Access map[string]Access
}

// Equal reports whether f and o describe the same objects and access states.
func (f Fingerprint) Equal(o Fingerprint) bool {
	return maps.Equal(f.Kinds, o.Kinds) && maps.Equal(f.Access, o.Access)
}

// Fingerprinter returns the fingerprint of the live source data.
type Fingerprinter interface{ Fingerprint() Fingerprint }

// Fingerprint walks the stores as Snapshot does, without copying or decoding
// anything, so it equals the Source of a snapshot taken from the same data.
func (c *Cache) Fingerprint() Fingerprint {
	var p printer
	access, _ := c.walk(func(gvr schema.GroupVersionResource, o any) { p.add(gvr, o.(metav1.Object)) })
	return p.finish(access)
}

// FNV-64a parameters and the splitmix64 increment.
const (
	fnvOffset = 14695981039346656037
	fnvPrime  = 1099511628211
	golden    = 0x9e3779b97f4a7c15
)

// objHash mixes FNV-64a of uid, a NUL, and resourceVersion through splitmix64.
// FNV is inlined so the walk allocates nothing per object.
func objHash(uid types.UID, rv string) uint64 {
	h := uint64(fnvOffset)
	for i := 0; i < len(uid); i++ {
		h = (h ^ uint64(uid[i])) * fnvPrime
	}
	h *= fnvPrime // XOR with the NUL separator is a no-op
	for i := 0; i < len(rv); i++ {
		h = (h ^ uint64(rv[i])) * fnvPrime
	}
	x := h + golden
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// printer accumulates KindPrints during a walk. The walk visits one kind's
// objects together, so the last kind's entry is cached.
type printer struct {
	kinds map[schema.GroupVersionResource]*KindPrint
	last  schema.GroupVersionResource
	cur   *KindPrint
}

func (p *printer) add(gvr schema.GroupVersionResource, o metav1.Object) {
	if p.cur == nil || gvr != p.last {
		if p.kinds == nil {
			p.kinds = map[schema.GroupVersionResource]*KindPrint{}
		}
		if p.cur = p.kinds[gvr]; p.cur == nil {
			p.cur = &KindPrint{}
			p.kinds[gvr] = p.cur
		}
		p.last = gvr
	}
	p.cur.add(o.GetUID(), o.GetResourceVersion())
}

// finish keys the result by GVR.String() and gives every kind in access an entry.
func (p *printer) finish(access map[schema.GroupVersionResource]Access) Fingerprint {
	f := Fingerprint{Kinds: make(map[string]KindPrint, len(access)), Access: make(map[string]Access, len(access))}
	for gvr, a := range access {
		f.Access[gvr.String()] = a
		f.Kinds[gvr.String()] = KindPrint{}
	}
	for gvr, k := range p.kinds {
		f.Kinds[gvr.String()] = *k
	}
	return f
}
