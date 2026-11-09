package kgateway

import (
	"slices"

	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/ctrllogs"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/proxystats"
)

// Reported is the version the install's controller reports in
// kgateway_build_info, read from the controller metrics value in col.
func Reported(i install.Install, col collect.Snapshot) (string, bool) {
	return ctrlmetrics.ReportedVersion(col, i.ID())
}

// Claimed keeps the installs whose Impl is pack.
func Claimed(insts []install.Install, pack string) []install.Install {
	return slices.DeleteFunc(slices.Clone(insts), func(i install.Install) bool { return i.Impl != pack })
}

// LatestStats is the proxy stats value in store for the log collector: pending
// until the first value lands, so the backfill meets its names.
func LatestStats(store *collect.Store) (proxystats.Stats, ctrllogs.StatsState) {
	v, ok := store.Snapshot().Get(proxystats.Key)
	if !ok {
		return nil, ctrllogs.StatsPending
	}
	if s, isStats := v.Data.(proxystats.Stats); v.State == collect.OK && isStats {
		return s, ctrllogs.StatsReady
	}
	return nil, ctrllogs.StatsUnavailable
}
