package api

import (
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/overview"
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

// WithPacks names the packs this server runs, for MetaResponse.Packs.
func WithPacks(names ...string) HandlerOption {
	return func(cfg *handlerConfig) { cfg.packs = names }
}

// WithInstallKinds reports whether a served kind applies to an install, such
// as a pack's kind to that pack's installs; overview counts keep only those.
func WithInstallKinds(f func(install.Install, objects.GroupKind) bool) HandlerOption {
	return func(cfg *handlerConfig) { cfg.kindFor = f }
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

// applicableCounts is counts without the kinds that do not apply to i; a new
// slice, since counts may be the model's.
func (h *handler) applicableCounts(i *engine.InstallModel, counts []overview.Count) []overview.Count {
	if h.kindFor == nil {
		return counts
	}
	out := make([]overview.Count, 0, len(counts))
	for _, c := range counts {
		if h.kindFor(i.Resolved.Install, objects.GroupKind{Group: c.Group, Kind: c.Kind}) {
			out = append(out, c)
		}
	}
	return out
}
