package paths_test

import (
	"testing"

	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
)

func wref(w int32, defaulted bool) objects.BackendRef {
	return objects.BackendRef{Weight: w, WeightDefaulted: defaulted}
}

func TestSharesWeights(t *testing.T) {
	got := paths.Shares(&objects.Rule{BackendRefs: []objects.BackendRef{wref(1, false), wref(3, false)}})
	want := []paths.Share{{25, paths.ShareShown}, {75, paths.ShareShown}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSharesDefaultedWeightsAreEqual(t *testing.T) {
	got := paths.Shares(&objects.Rule{BackendRefs: []objects.BackendRef{wref(1, true), wref(1, true)}})
	for i, s := range got {
		if s.Percent != 50 || s.State != paths.ShareShown {
			t.Fatalf("ref %d: %v", i, s)
		}
	}
}

func TestSharesAllZeroIsNoTraffic(t *testing.T) {
	got := paths.Shares(&objects.Rule{BackendRefs: []objects.BackendRef{wref(0, false), wref(0, false)}})
	for i, s := range got {
		if s.Percent != 0 || s.State != paths.ShareNoTraffic {
			t.Fatalf("ref %d: %v", i, s)
		}
	}
}

func TestSharesZeroWeightAmongOthers(t *testing.T) {
	got := paths.Shares(&objects.Rule{BackendRefs: []objects.BackendRef{wref(0, false), wref(2, false)}})
	if got[0].Percent != 0 || got[1].Percent != 100 {
		t.Fatalf("got %v", got)
	}
}

func TestSharesDelegationRuleIsNotShown(t *testing.T) {
	deleg := objects.BackendRef{Weight: 1, Delegation: &objects.DelegationRef{}}
	got := paths.Shares(&objects.Rule{BackendRefs: []objects.BackendRef{deleg, wref(1, false)}})
	for i, s := range got {
		if s.State != paths.ShareNotShown || s.Percent != 0 {
			t.Fatalf("ref %d: %v", i, s)
		}
	}
}

func TestShareWithAttribution(t *testing.T) {
	s := paths.Share{Percent: 25, State: paths.ShareShown}
	if got := s.WithAttribution(true, true); got.State != paths.ShareFailing || got.Percent != 25 {
		t.Fatalf("attributed: %v", got)
	}
	if got := s.WithAttribution(false, true); got.State != paths.ShareMayFail || got.Percent != 25 {
		t.Fatalf("candidate: %v", got)
	}
	if got := s.WithAttribution(false, false); got != s {
		t.Fatalf("neither: %v", got)
	}
	nt := paths.Share{State: paths.ShareNoTraffic}
	if got := nt.WithAttribution(true, true); got != nt {
		t.Fatalf("no-traffic must stay: %v", got)
	}
	ns := paths.Share{State: paths.ShareNotShown}
	if got := ns.WithAttribution(true, true); got != ns {
		t.Fatalf("not-shown must stay: %v", got)
	}
}

func TestMirrorShare(t *testing.T) {
	p := int32(30)
	if got := paths.MirrorShare(&objects.MirrorFilter{Percent: &p}); got != (paths.Share{Percent: 30, State: paths.ShareMirror}) {
		t.Fatalf("percent: %v", got)
	}
	f := &objects.MirrorFilter{Fraction: &objects.Fraction{Numerator: 1, Denominator: 4}}
	if got := paths.MirrorShare(f); got != (paths.Share{Percent: 25, State: paths.ShareMirror}) {
		t.Fatalf("fraction: %v", got)
	}
	if got := paths.MirrorShare(&objects.MirrorFilter{}); got.Percent != 100 {
		t.Fatalf("unset: %v", got)
	}
}
