package rulesets

import "testing"

// Selection follows the controller minor (rules §2).
func TestSelect(t *testing.T) {
	for v, want := range map[string]struct{ set, note string }{
		"2.3.11": {"kgateway-2.3", ""}, "v2.3.0": {"kgateway-2.3", ""}, "2.4.7": {"kgateway-2.4", ""},
		"":       {"kgateway-2.4", "Controller version unknown; using the 2.4 rules"},
		"banana": {"kgateway-2.4", "Controller version unknown; using the 2.4 rules"},
		"2.5.0":  {"kgateway-2.4", "Rules not yet verified for 2.5"},
		"3.0.1":  {"kgateway-2.4", "Rules not yet verified for 3.0"},
		"2.2.9":  {"kgateway-2.3", "Rules not yet verified for 2.2"}, // older than the oldest set (our call)
	} {
		s := Select(v)
		if s.Rules.Version() != want.set || s.Note != want.note {
			t.Errorf("%q: %s %q", v, s.Rules.Version(), s.Note)
		}
	}
}
