// Package metricstest builds controller metrics values for tests, as the
// kgateway pack's collector publishes them.
package metricstest

import (
	"time"

	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
)

// Healthy is a reject-free result with resources_managed present, collected at at.
func Healthy(at time.Time) ctrlmetrics.Result {
	return ctrlmetrics.Result{CollectedAt: at, Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{}, ManagedPresent: true}}
}

// Collected is a collected snapshot holding byInstall under ctrlmetrics.Key
// with maxAge, published at at.
func Collected(at time.Time, maxAge time.Duration, byInstall map[string]ctrlmetrics.Result) collect.Snapshot {
	s := collect.New(nil, func() time.Time { return at })
	ctrlmetrics.Publish(s, ctrlmetrics.Results{ByInstall: byInstall, MaxAge: maxAge})
	return s.Snapshot()
}
