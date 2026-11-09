package ctrlmetrics

import (
	"testing"
	"time"

	"github.com/DuncanDoyle/snellius/internal/testutil"
)

// The fingerprint covers every field a check reads, and not CollectedAt.
func TestFingerprintCoversContent(t *testing.T) {
	at := testutil.Now
	res := func(rejects float64) Result {
		return Result{CollectedAt: at, Metrics: Metrics{Rejects: map[GatewayKey]float64{{Namespace: "infra", Name: "edge"}: rejects},
			Managed: map[ManagedKey]float64{{Namespace: "shop", Resource: "httproutes"}: 3, {Namespace: "infra", Resource: "gateways"}: 1}, ManagedPresent: true}}
	}
	enc := func(r Result, present bool) uint64 {
		rs := Results{ByInstall: map[string]Result{}}
		if present {
			rs.ByInstall["kgw.kgateway"] = r
		}
		return rs.Fingerprint()
	}
	if enc(res(0), true) == enc(res(1), true) {
		t.Fatal("same CollectedAt, different Rejects must encode differently")
	}
	if enc(Result{}, false) == enc(Result{}, true) {
		t.Fatal("absent and present empty results must encode differently")
	}
	a, b := res(1), res(1) // separate maps: iteration order must not matter
	if enc(a, true) != enc(b, true) {
		t.Fatal("equal results must encode equally")
	}
	later := res(1)
	later.CollectedAt = at.Add(10 * time.Second)
	if enc(later, true) != enc(res(1), true) {
		t.Fatal("CollectedAt is not part of the identity")
	}
	pod := func(n float64, present bool) Result {
		r := res(1)
		r.Metrics.PerPod = []PodMetrics{{UID: "u", Name: "p", Dropped: n, DroppedPresent: present}}
		return r
	}
	if enc(pod(2, true), true) == enc(pod(3, true), true) || enc(pod(0, true), true) == enc(pod(0, false), true) {
		t.Fatal("the dropped counter and its presence must be part of the identity")
	}
	perClient := func(mutate func(p *PodMetrics)) uint64 {
		r := res(1)
		r.Metrics.PerPod = []PodMetrics{{UID: "u", Name: "p"}}
		mutate(&r.Metrics.PerPod[0])
		return enc(r, true)
	}
	nack := GatewayKey{Namespace: "infra", Name: "nack"}
	plain := perClient(func(*PodMetrics) {})
	for name, mutate := range map[string]func(p *PodMetrics){
		"build version": func(p *PodMetrics) { p.BuildVersion = "v2.5.0" },
		"presence":      func(p *PodMetrics) { p.PerClientPresent = true },
		"withheld":      func(p *PodMetrics) { p.Withheld = map[GatewayKey]float64{nack: 1} },
		"bounded": func(p *PodMetrics) {
			p.Bounded = map[ModeKey]float64{{Gateway: nack, Mode: "warm_release"}: 1}
		},
	} {
		if perClient(mutate) == plain {
			t.Errorf("%s must be part of the identity", name)
		}
	}
	errRes := res(0)
	errRes.Err = "no ready controller pod to scrape"
	if enc(errRes, true) == enc(res(0), true) {
		t.Fatal("Err must be part of the identity")
	}
	snap := res(0)
	snap.Metrics.Snapshot = map[GatewayKey]SnapshotResources{{Namespace: "infra", Name: "edge"}: {Routes: 1}}
	if enc(snap, true) == enc(res(0), true) {
		t.Fatal("the snapshot must be part of the identity")
	}
}
