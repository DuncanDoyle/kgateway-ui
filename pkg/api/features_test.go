package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kgateway"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// allFeatures is what a kgateway install with a delegating rule set lists.
var allFeatures = []string{FeatureDelegation, FeatureComputed, FeatureDataPlane, FeatureProcesses}

// packFeatures gives kgateway installs the kgateway pack's features and the rest none, as the app does.
func packFeatures(i install.Install) []string {
	if i.Impl != kgateway.Name {
		return nil
	}
	return kgateway.Features()
}

// Each install lists its UI features (Phase 5 design §10): a kgateway install
// all four, a class-only install none, sent as an empty list.
func TestInstallFeatures(t *testing.T) {
	h := newHarness(t, setup{detectors: []install.Detector{kgwDetector{}}, hooks: packHooks, classOnly: true,
		opts: []HandlerOption{WithInstallFeatures(packFeatures)}}, world()...)
	got := get[[]InstallSummary](t, h.srv, "/api/v1/installs", 200)
	if len(got) != 2 {
		t.Fatalf("installs %+v", got)
	}
	if co := got[0]; co.Features == nil || len(co.Features) != 0 {
		t.Errorf("class-only features %#v, want an empty list", co.Features)
	}
	if k := got[1]; !slices.Equal(k.Features, allFeatures) {
		t.Errorf("kgateway features %v, want %v", k.Features, allFeatures)
	}
	if raw := get[json.RawMessage](t, h.srv, "/api/v1/installs", 200); !bytes.Contains(raw, []byte(`"features":[]`)) {
		t.Errorf("class-only install must send an empty list: %s", raw)
	}
}

// delegation needs a Delegator: an attachment-only rule set keeps computed and drops it.
func TestFeaturesFollowTheRuleSet(t *testing.T) {
	kgw := func(install.Install) []string { return kgateway.Features() }
	for _, tc := range []struct {
		name  string
		rules func(string) computed.Selection
		want  []string
	}{
		{"kgateway", nil, allFeatures},
		{"attachment only", attachOnlyRules, []string{FeatureComputed, FeatureDataPlane, FeatureProcesses}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, setup{rulesFor: tc.rules, opts: []HandlerOption{WithInstallFeatures(kgw)}}, world()...)
			if got := get[[]InstallSummary](t, srv, "/api/v1/installs", 200); len(got) != 1 || !slices.Equal(got[0].Features, tc.want) {
				t.Fatalf("installs %+v, want features %v", got, tc.want)
			}
		})
	}
}

// Without WithInstallFeatures no install lists a pack feature.
func TestNoInstallFeaturesWithoutThePack(t *testing.T) {
	h := newHarness(t, setup{}, world()...)
	srv := httptest.NewServer(NewHandler(h.e, fstest.MapFS{}, nil))
	t.Cleanup(srv.Close)
	if got := get[[]InstallSummary](t, srv, "/api/v1/installs", 200); len(got) != 1 || got[0].Features == nil || len(got[0].Features) != 0 {
		t.Fatalf("installs %+v, want no features", got)
	}
}

// Computed facts and details carry the rule set's display label beside its ID
// (design §10), so the SPA parses no rule-set ID.
func TestComputedFactsCarryTheRuleSetLabel(t *testing.T) {
	r := testutil.HTTPRoute("shop", "r", []objects.ParentRef{testutil.GwRef("infra", "edge")})
	h := newHarness(t, setup{disputes: true}, world(r)...)
	gw := get[GatewayDetail](t, h.srv, base+"/gateways/infra/edge", 200)
	if gw.RuleSet != "kgateway-2.4" || gw.RuleSetLabel != "kgateway 2.4" {
		t.Fatalf("gateway rule set %q label %q", gw.RuleSet, gw.RuleSetLabel)
	}
	if c := gw.Listeners[0].Computed; c == nil || c.Label != "kgateway 2.4" {
		t.Errorf("listener computed %+v", c)
	}
	rt := get[RouteDetail](t, h.srv, base+"/routes/gateway.networking.k8s.io/HTTPRoute/shop/r", 200)
	if rt.RuleSetLabel != "kgateway 2.4" || len(rt.Attachments) == 0 {
		t.Fatalf("route label %q attachments %+v", rt.RuleSetLabel, rt.Attachments)
	}
	raw := get[json.RawMessage](t, h.srv, base+"/routes/gateway.networking.k8s.io/HTTPRoute/shop/r", 200)
	if n, m := bytes.Count(raw, []byte(`"rules":"kgateway-2.4"`)), bytes.Count(raw, []byte(`"label":"kgateway 2.4"`)); n == 0 || n != m {
		t.Errorf("%d computed facts, %d labelled: %s", n, m, raw)
	}
}
