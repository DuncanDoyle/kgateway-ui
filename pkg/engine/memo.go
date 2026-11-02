package engine

import (
	"encoding/binary"
	"fmt"
	"hash"
	"hash/fnv"
	"maps"
	"math"
	"slices"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

// kindKey names one kind in a snapshot: its GVR and its Source key.
type kindKey struct {
	gvr schema.GroupVersionResource
	key string
}

// kindKeys maps each served kind, Namespace, Pod, Deployment and the optional
// watches' kinds to its kindKey, and lists the kinds Owned covers: every
// served kind and Namespaces.
func kindKeys(served []kinds.Served) (map[objects.GroupKind]kindKey, []kindKey) {
	byGK := map[objects.GroupKind]kindKey{}
	owned := make([]kindKey, 0, len(served)+1)
	for _, s := range served {
		k := kindKey{s.GVR(), s.GVR().String()}
		byGK[objects.GroupKind{Group: s.Group, Kind: s.Kind}] = k
		owned = append(owned, k)
	}
	for gk, gvr := range map[objects.GroupKind]schema.GroupVersionResource{
		{Kind: "Namespace"}: cache.NamespacesGVR, {Kind: "Pod"}: cache.PodsGVR, {Group: "apps", Kind: "Deployment"}: cache.DeploymentsGVR,
		cache.ProxyPodsKind: cache.ProxyPodsGVR, cache.EventsKind: cache.EventsGVR, cache.CRDsKind: cache.CRDsGVR,
	} {
		byGK[gk] = kindKey{gvr, gvr.String()}
	}
	return byGK, append(owned, byGK[objects.GroupKind{Kind: "Namespace"}])
}

// printCtx is one install's evidence for its checks' input prints in one
// compute. Each component print is computed on first use and kept.
type printCtx struct {
	snap   *cache.Snapshot
	res    install.Resolved
	all    []install.Resolved
	read   MetricsRead
	now    time.Time
	maxAge time.Duration
	ver    *scopeverify.Result
	views  *computed.Views
	kc     kinds.Change
	col    collect.Snapshot

	owned, metrics, verify, computed, change *uint64
}

// lazy returns *p, computing it with f on first use.
func lazy(p **uint64, f func() uint64) uint64 {
	if *p == nil {
		v := f()
		*p = &v
	}
	return **p
}

// inputPrint keys a check's memo on what set declares (§16.2); earlier checks'
// outputs are mixed in by health.Evaluate. False means the check always runs.
func (e *Engine) inputPrint(set health.InputSet, ctx *printCtx) (uint64, bool) {
	if (set.Owned || len(set.Kinds) > 0) && ctx.snap.Source.Kinds == nil || len(set.Collected) > health.MaxStampedCollected {
		return 0, false
	}
	p := installPrint(ctx.res)
	if ctx.snap.Synced {
		p = mix(p, 1)
	}
	if set.Owned {
		p = mix(p, lazy(&ctx.owned, func() uint64 { return e.ownedPrint(ctx) }))
	}
	for _, gk := range set.Kinds {
		k, known := e.kindKeys[gk]
		if !known {
			return 0, false // not watched: no print can follow it
		}
		p = mix(p, kindPrint(ctx.snap, k))
	}
	if set.Metrics {
		p = mix(p, lazy(&ctx.metrics, func() uint64 { return encodeMetrics([]MetricsRead{ctx.read}, ctx.now, ctx.maxAge) }))
	}
	if set.Verify {
		p = mix(p, lazy(&ctx.verify, func() uint64 { return verifyPrint(ctx.ver) }))
	}
	if set.Computed {
		p = mix(p, lazy(&ctx.computed, func() uint64 {
			return mix(lazy(&ctx.owned, func() uint64 { return e.ownedPrint(ctx) }), viewsPrint(ctx.views))
		}))
	}
	if set.KindsChange {
		p = mix(p, lazy(&ctx.change, func() uint64 { return strPrint(ctx.kc.String()) }))
	}
	for _, k := range set.Collected {
		p = mix(p, collectedPrint(ctx.col, k))
	}
	return p, true
}

// ownedPrint covers Owned: every served kind and Namespaces, and every install.
func (e *Engine) ownedPrint(ctx *printCtx) uint64 {
	var p uint64
	for _, k := range e.ownedKeys {
		p = mix(p, kindPrint(ctx.snap, k))
	}
	for _, r := range ctx.all {
		p = mix(p, installPrint(r))
	}
	return p
}

// printer is FNV-64a with length-prefixed strings.
type printer struct {
	h hash.Hash64
	b [8]byte
}

func newPrinter() *printer { return &printer{h: fnv.New64a()} }

func (p *printer) u64(v uint64) {
	binary.LittleEndian.PutUint64(p.b[:], v)
	_, _ = p.h.Write(p.b[:])
}

func (p *printer) str(s string) {
	p.u64(uint64(len(s))) // length prefix keeps adjacent strings apart
	_, _ = p.h.Write([]byte(s))
}

func (p *printer) flag(v bool) {
	if v {
		p.u64(1)
	} else {
		p.u64(0)
	}
}

func (p *printer) time(t time.Time) {
	p.u64(uint64(t.UnixNano()))
	p.str(t.Location().String())
}

func strPrint(s string) uint64 {
	p := newPrinter()
	p.str(s)
	return p.h.Sum64()
}

// kindPrint is k's Source print and access state in snap; for Events also
// the deltas, the unknown histories and the cap, which the Source does not cover.
func kindPrint(snap *cache.Snapshot, k kindKey) uint64 {
	p := newPrinter()
	if k.gvr == cache.EventsGVR {
		eventsPrint(p, snap)
	}
	kp, ok := snap.Source.Kinds[k.key]
	p.flag(ok)
	p.u64(uint64(kp.Count)) //nolint:gosec // hashed bits; the sign does not matter
	p.u64(kp.Hash)
	a, ok := snap.Access[k.gvr]
	p.flag(ok)
	p.flag(a.OK)
	p.str(a.Err)
	return p.h.Sum64()
}

// eventsPrint writes snap's Event deltas, unknown histories and cap to p, Pods in UID order.
func eventsPrint(p *printer, snap *cache.Snapshot) {
	pods := slices.Sorted(maps.Keys(snap.EventDeltas))
	p.u64(uint64(len(pods)))
	for _, uid := range pods {
		p.str(string(uid))
		obs := snap.EventDeltas[uid]
		p.u64(uint64(len(obs)))
		for _, o := range obs {
			p.time(o.At)
			p.str(o.Reason)
			p.u64(uint64(o.Delta)) //nolint:gosec // hashed bits; the sign does not matter
		}
	}
	unknown := slices.Sorted(maps.Keys(snap.EventHistoryUnknown))
	p.u64(uint64(len(unknown)))
	for _, uid := range unknown {
		p.str(string(uid))
		p.flag(snap.EventHistoryUnknown[uid])
	}
	p.flag(snap.EventsCapped)
}

// installPrint covers what a check reads of r: its identity, controller
// Deployment and Pods by (uid, resourceVersion), scope and settings.
func installPrint(r install.Resolved) uint64 {
	p := newPrinter()
	for _, s := range []string{r.ID(), r.Namespace, r.Name, r.DisplayName, r.Version, r.BaseVersion, r.Edition, r.Container} {
		p.str(s)
	}
	p.u64(uint64(len(r.ControllerNames)))
	for _, n := range r.ControllerNames {
		p.str(n)
	}
	p.u64(uint64(r.MetricsPort)) //nolint:gosec // hashed bits; the sign does not matter
	if r.ProxySelector != nil {
		p.str(r.ProxySelector.String())
	}
	if p.flag(r.Deployment != nil); r.Deployment != nil {
		p.str(string(r.Deployment.UID))
		p.str(r.Deployment.ResourceVersion)
	}
	p.u64(uint64(len(r.Pods)))
	for _, pod := range r.Pods {
		p.str(string(pod.UID))
		p.str(pod.ResourceVersion)
	}
	p.str(string(r.Scope.Mode))
	p.str(r.Scope.Err)
	p.str(r.Scope.Source)
	p.str(fmt.Sprintf("%+v|%+v", r.Scope.Selectors, r.Settings))
	return p.h.Sum64()
}

// verifyPrint covers every field of v.
func verifyPrint(v *scopeverify.Result) uint64 {
	p := newPrinter()
	p.flag(v.Available)
	p.str(v.Reason)
	p.u64(uint64(len(v.Mismatches)))
	for _, m := range v.Mismatches {
		p.str(m.Namespace)
		p.str(m.Resource)
		p.u64(math.Float64bits(m.UI))
		p.u64(math.Float64bits(m.Controller))
		p.time(m.Since)
	}
	p.time(v.Matures)
	return p.h.Sum64()
}

// viewsPrint covers the views' rule set and each check's identity print and
// the state persistence set. The views themselves follow from Owned.
func viewsPrint(v *computed.Views) uint64 {
	p := newPrinter()
	if p.flag(v != nil); v == nil {
		return p.h.Sum64()
	}
	p.str(v.RuleSet)
	p.str(v.Note)
	p.u64(uint64(len(v.Checks)))
	for _, c := range v.Checks {
		p.str(string(c.Class))
		p.u64(c.Fingerprint)
		p.str(string(c.State))
		p.flag(c.Mature)
		p.time(c.Since)
	}
	return p.h.Sum64()
}

// collectedPrint covers key's presence, fingerprint, state and reason in col.
func collectedPrint(col collect.Snapshot, key string) uint64 {
	p := newPrinter()
	p.str(key)
	v, ok := col.Get(key)
	p.flag(ok)
	p.u64(v.Fingerprint)
	p.str(string(v.State))
	p.str(v.Reason)
	return p.h.Sum64()
}

// mix folds v into h: an FNV-1a step, then the splitmix64 finaliser.
func mix(h, v uint64) uint64 {
	x := (h^v)*1099511628211 + 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
