package rulesets

import "testing"

// Selection follows the controller minor (rules §2).
func TestSelect(t *testing.T) {
	for v, want := range map[string]struct{ set, note string }{
		"2.3.11": {"kgateway-2.3", ""}, "v2.3.0": {"kgateway-2.3", ""}, "2.4.7": {"kgateway-2.4", ""},
		"":       {"kgateway-2.4", "Controller version unknown; using the 2.4 rules"},
		"banana": {"kgateway-2.4", "Controller version unknown; using the 2.4 rules"},
		"2.5.0":  {"kgateway-2.5", ""}, "v2.5.0-rc.1": {"kgateway-2.5", ""},
		"2.6.0": {"kgateway-2.5", "Rules not yet verified for 2.6"},
		"3.0.1": {"kgateway-2.5", "Rules not yet verified for 3.0"},
		"2.2.9": {"kgateway-2.3", "Rules not yet verified for 2.2"}, // older than the oldest set (our call)
	} {
		s := Select(v)
		if s.Rules.Version() != want.set || s.Note != want.note {
			t.Errorf("%q: %s %q", v, s.Rules.Version(), s.Note)
		}
	}
}

// The Gateway API table follows each kgateway minor's go.mod; other minors have no row.
func TestExpectedGatewayAPI(t *testing.T) {
	for minor, want := range map[string]string{"2.3": "v1.5", "2.4": "v1.6", "2.5": "v1.7", "2.2": "", "2.6": "", "": "", "banana": ""} {
		got, ok := ExpectedGatewayAPI(minor)
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q %v, want %q", minor, got, ok, want)
		}
	}
}

// A pre-release of a newer minor keeps the "not yet verified" note on the
// newest set; 2.5 has its own set and the v1.7 row of the v2.5.0 go.mod.
func TestPrereleaseKeepsNoteAndGatewayAPI(t *testing.T) {
	if s := Select("v2.6.0-rc.1"); s.Rules.Version() != "kgateway-2.5" || s.Note != "Rules not yet verified for 2.6" {
		t.Fatalf("selection %s %q", s.Rules.Version(), s.Note)
	}
	if b, ok := ExpectedGatewayAPI("2.5"); !ok || b != "v1.7" {
		t.Fatalf("2.5: %q %v", b, ok)
	}
}

// allowedListeners is in the experimental Gateway CRD from v1.3 and the
// standard one from v1.5 (each bundle's config/crd).
func TestGatewayCRDHasAllowedListeners(t *testing.T) {
	for _, tc := range []struct {
		channel, bundle string
		has, known      bool
	}{
		{"standard", "v1.4.1", false, true}, {"standard", "v1.5.1", true, true}, {"standard", "v1.6.1", true, true},
		{"experimental", "v1.2.1", false, true}, {"experimental", "v1.3.0", true, true}, {"experimental", "v1.4.1", true, true},
		{"standard", "", false, false}, {"", "v1.4.1", false, false}, {"standard", "main", false, false}, {"standard", "v2.0.0", false, false},
	} {
		has, known := GatewayCRDHasAllowedListeners(tc.channel, tc.bundle)
		if has != tc.has || known != tc.known {
			t.Errorf("%s %s: %v %v, want %v %v", tc.channel, tc.bundle, has, known, tc.has, tc.known)
		}
	}
}
