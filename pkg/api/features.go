package api

import (
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/install"
)

// Install features, as InstallSummary.Features lists them (Phase 5 design §10).
const (
	FeatureDelegation = "delegation"
	FeatureComputed   = "computed"
	FeatureDataPlane  = "dataplane"
	FeatureProcesses  = "processes"
)

// WithInstallFeatures sets each install's pack features. The API drops
// FeatureComputed without a rule set and FeatureDelegation without a Delegator.
func WithInstallFeatures(f func(install.Install) []string) HandlerOption {
	return func(cfg *handlerConfig) { cfg.featuresFor = f }
}

// features is i's pack features that its rule set supports; never nil.
func (h *handler) features(i *engine.InstallModel) []string {
	out := []string{}
	if h.featuresFor == nil {
		return out
	}
	for _, f := range h.featuresFor(i.Resolved.Install) {
		if f == FeatureComputed && i.Rules.Rules == nil || f == FeatureDelegation && i.Rules.Delegator() == nil {
			continue
		}
		out = append(out, f)
	}
	return out
}
