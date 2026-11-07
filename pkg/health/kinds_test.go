package health

import (
	"slices"
	"strings"
	"testing"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
)

// A served set that differs from the watched one only in list-only kinds is no
// KGWUI-008: the check stays OK and says a restart lists them (phase review #3).
// A judged kind's change is 008, with or without list-only ones beside it.
func TestServedKindsListOnlyChange(t *testing.T) {
	watched := testutil.ServedUpstream()
	without := func(names ...string) []kinds.Served {
		return slices.DeleteFunc(slices.Clone(watched), func(s kinds.Served) bool { return slices.Contains(names, s.Kind) })
	}
	run := func(now []kinds.Served) Report {
		in := healthyWorld().input(t)
		in.KindsChange = kinds.Diff(watched, now)
		return Evaluate(in, Upstream(), Runtime{})
	}
	r := run(without("ReferenceGrant", "DirectResponse"))
	if has(r, CodeSignalUnavailable, "", "") {
		t.Fatalf("list-only change raised 008: %+v", r.Findings)
	}
	i := slices.IndexFunc(r.Checks, func(c CheckResult) bool { return c.Name == "served kinds" })
	if i < 0 || !r.Checks[i].OK || !strings.Contains(r.Checks[i].Detail, "referencegrants") || !strings.Contains(r.Checks[i].Detail, "directresponses") {
		t.Fatalf("served kinds check %+v", r.Checks)
	}
	r = run(without("ReferenceGrant", "TCPRoute"))
	if !has(r, CodeSignalUnavailable, SeverityInfo, "") {
		t.Fatalf("judged change: want 008: %+v", r.Findings)
	}
}
