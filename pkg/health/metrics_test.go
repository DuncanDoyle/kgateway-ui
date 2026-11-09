package health

import (
	"maps"
	"time"

	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
)

// testMaxAge is the metrics max age the tests' worlds use (3 × a 10 s interval).
const testMaxAge = 30 * time.Second

// withMetrics returns rt with r as install id's controller metrics entry, stale
// after maxAge, next to rt's other collected values; nil r adds none.
func withMetrics(rt Runtime, id string, r *ctrlmetrics.Result, maxAge time.Duration) Runtime {
	if r == nil {
		return rt
	}
	vals := maps.Clone(rt.Collected.Values)
	if vals == nil {
		vals = map[string]collect.Value{}
	}
	res := ctrlmetrics.Results{ByInstall: map[string]ctrlmetrics.Result{id: *r}, MaxAge: maxAge}
	vals[ctrlmetrics.Key] = collect.Value{Data: res, Fingerprint: res.Fingerprint(), At: r.CollectedAt, State: collect.OK}
	rt.Collected = collect.Snapshot{Values: vals, Print: rt.Collected.Print ^ res.Fingerprint()}
	return rt
}

// freshMetrics is rt with healthyWorld's metrics for its install, never stale.
func freshMetrics(rt Runtime) Runtime {
	return withMetrics(rt, "kgw.kgateway", healthyWorld().metrics, 0)
}
