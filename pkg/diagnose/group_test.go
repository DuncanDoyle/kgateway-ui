package diagnose_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/diagnose"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

var in = diagnose.Input{Install: install.Resolved{Install: install.Install{Namespace: "kgw", Name: "kgateway"}}}

func ref(kind, name string) *objects.Ref {
	return &objects.Ref{Group: objects.GatewayAPIGroup, Kind: kind, Namespace: "shop", Name: name}
}

func ago(d time.Duration) *time.Time { t := testutil.Now.Add(-d); return &t }

// finding returns a finding named by its message, with an ID from code, resource and message.
func finding(name, code string, sev health.Severity, res *objects.Ref, since *time.Time) health.Finding {
	return health.Finding{ID: health.FindingID(in.Install.ID(), code, res, []string{name}), Code: code, Severity: sev,
		Install: in.Install.ID(), Resource: res, Message: name, Since: since}
}

// fake claims the findings named in claims: each maps to a cause name, and
// the cause's affected objects are routes a0..a(n-1), n from affected.
type fake struct {
	name     string
	claims   map[string]string
	affected map[string]int
}

func (f fake) Name() string { return f.name }

func (f fake) Claim(x health.Finding, _ diagnose.Input) (*objects.Ref, []string, bool) {
	c, ok := f.claims[x.Message]
	if !ok {
		return nil, nil, false
	}
	return ref("Gateway", c), nil, true
}

func (f fake) Affected(cause *objects.Ref, _ []string, _ []health.Finding, _ diagnose.Input) []objects.Ref {
	var out []objects.Ref
	for i := range f.affected[cause.Name] {
		out = append(out, *ref("HTTPRoute", "a"+string(rune('0'+i))))
	}
	return out
}

// names returns the messages of fs at idx.
func names(fs []health.Finding, idx []int) []string {
	out := make([]string, len(idx))
	for i, k := range idx {
		out[i] = fs[k].Message
	}
	return out
}

// groupNames returns, per group in order, its members' messages joined by "+".
func groupNames(rep *health.Report, d *diagnose.Diagnosis) []string {
	out := make([]string, len(d.Groups))
	for i, g := range d.Groups {
		for j, n := range names(rep.Findings, g.Members) {
			if j > 0 {
				out[i] += "+"
			}
			out[i] += n
		}
	}
	return out
}

func TestRankOrderEveryClass(t *testing.T) {
	// Equal since and affected count tie on the group id; order that pair by it.
	tieA, tieB := "tie-a", "tie-b"
	if health.GroupID(in.Install.ID(), "Other", ref("HTTPRoute", tieA), nil) > health.GroupID(in.Install.ID(), "Other", ref("HTTPRoute", tieB), nil) {
		tieA, tieB = tieB, tieA
	}
	rep := &health.Report{Findings: []health.Finding{
		finding("hygiene-error", health.CodeComputedDisagrees, health.SeverityError, ref("HTTPRoute", "h"), ago(time.Hour)),
		finding("process-error", health.CodeControllerNotReady, health.SeverityError, ref("Deployment", "p"), nil),
		finding("dataplane-error", health.CodeXDSRejected, health.SeverityError, ref("Gateway", "d"), nil),
		// Class 3 warnings: blast radius, then age, then group id.
		finding("narrow", "KGWUI-015", health.SeverityWarning, ref("HTTPRoute", "n"), ago(time.Hour)),
		finding("wide", "KGWUI-013", health.SeverityWarning, ref("HTTPRoute", "w"), nil),
		finding("pending-old", health.CodeRouteUnattached, health.SeverityWarning, ref("HTTPRoute", "po"), ago(2*time.Hour)),
		finding("no-since", "KGWUI-017", health.SeverityWarning, ref("HTTPRoute", "ns"), nil),
		finding(tieB, "KGWUI-023", health.SeverityWarning, ref("HTTPRoute", tieB), ago(time.Minute)),
		finding(tieA, "KGWUI-023", health.SeverityWarning, ref("HTTPRoute", tieA), ago(time.Minute)),
		// A 003 inside grace is info and class 4; a 002 warning is class 4.
		finding("hygiene-warning", health.CodeConditionFalse, health.SeverityWarning, ref("HTTPRoute", "hw"), ago(3*time.Hour)),
		finding("in-grace", health.CodeRouteUnattached, health.SeverityInfo, ref("HTTPRoute", "ig"), nil),
		finding("process-info", health.CodeProxyNotReady, health.SeverityInfo, ref("Gateway", "pi"), nil),
		// A shared cause: an error and a warning member make an error group.
		finding("shared-warning", "KGWUI-024", health.SeverityWarning, ref("Gateway", "s1"), ago(time.Minute)),
		finding("shared-error", "KGWUI-014", health.SeverityError, ref("HTTPRoute", "s2"), ago(time.Second)),
	}}
	fam := fake{name: "Fake", claims: map[string]string{"narrow": "c-narrow", "wide": "c-wide", "shared-warning": "c-shared", "shared-error": "c-shared"},
		affected: map[string]int{"c-narrow": 1, "c-wide": 3}}
	d := diagnose.Build(rep, in, []diagnose.Family{fam})

	want := []string{"shared-warning+shared-error", "dataplane-error", "process-error", "hygiene-error",
		"wide", "narrow", "pending-old", tieA, tieB, "no-since", "hygiene-warning", "process-info", "in-grace"}
	if got := groupNames(rep, d); !slices.Equal(got, want) {
		t.Fatalf("group order\n got %v\nwant %v", got, want)
	}
	g := d.Groups[0]
	if g.Severity != health.SeverityError || g.Class != diagnose.ClassDataPlane || g.Since == nil || !g.Since.Equal(*ago(time.Minute)) ||
		g.Family != "Fake" || g.Cause == nil || g.Cause.Name != "c-shared" || !g.Ranked {
		t.Errorf("shared group %+v", g)
	}
	// A finding ranks by its group, then severity, since and id.
	wantRank := []string{"shared-error", "shared-warning", "dataplane-error", "process-error", "hygiene-error",
		"wide", "narrow", "pending-old", tieA, tieB, "no-since", "hygiene-warning", "process-info", "in-grace"}
	if got := names(rep.Findings, d.Rank); !slices.Equal(got, wantRank) {
		t.Errorf("finding rank\n got %v\nwant %v", got, wantRank)
	}
	for k, c := range map[string]diagnose.Class{"dataplane-error": 1, "process-info": 2, "pending-old": 3, "no-since": 3, "in-grace": 4, "hygiene-warning": 4,
		"hygiene-error": 4} {
		f := rep.Findings[slices.IndexFunc(rep.Findings, func(f health.Finding) bool { return f.Message == k })]
		if got := diagnose.ClassOf(f); got != c {
			t.Errorf("ClassOf(%s) = %d, want %d", k, got, c)
		}
	}
	if n := len(d.Groups[4].Affected); n != 3 {
		t.Errorf("wide affected %d, want 3", n)
	}
}

func TestGroupMembershipExclusive(t *testing.T) {
	fs := []health.Finding{
		finding("both", "KGWUI-013", health.SeverityWarning, ref("HTTPRoute", "a"), nil),
		finding("second", "KGWUI-014", health.SeverityWarning, ref("HTTPRoute", "b"), nil),
		finding("neither", health.CodeConditionFalse, health.SeverityWarning, ref("HTTPRoute", "c"), nil),
	}
	first := fake{name: "First", claims: map[string]string{"both": "x"}}
	second := fake{name: "Second", claims: map[string]string{"both": "y", "second": "y"}}
	build := func() (*health.Report, *diagnose.Diagnosis) {
		rep := &health.Report{Findings: slices.Clone(fs)}
		return rep, diagnose.Build(rep, in, []diagnose.Family{first, second})
	}
	rep, d := build()

	seen := map[int]string{}
	for _, g := range d.Groups {
		for _, m := range g.Members {
			if prev, dup := seen[m]; dup {
				t.Errorf("finding %d in %s and %s", m, prev, g.ID)
			}
			seen[m] = g.ID
			if rep.Findings[m].Group != g.ID {
				t.Errorf("finding %d has group %q, member of %q", m, rep.Findings[m].Group, g.ID)
			}
		}
	}
	if len(seen) != len(fs) {
		t.Errorf("%d of %d findings grouped", len(seen), len(fs))
	}
	if r := slices.Sorted(slices.Values(d.Rank)); !slices.Equal(r, []int{0, 1, 2}) {
		t.Errorf("rank %v is not a permutation of the findings", d.Rank)
	}
	fam := map[string]string{}
	for _, g := range d.Groups {
		fam[names(rep.Findings, g.Members)[0]] = g.Family
	}
	if want := map[string]string{"both": "First", "second": "Second", "neither": "Other"}; !reflect.DeepEqual(fam, want) {
		t.Errorf("families %v, want %v", fam, want)
	}
	// Recomputing gives the same ids, membership and order.
	rep2, d2 := build()
	if !reflect.DeepEqual(d, d2) || !reflect.DeepEqual(rep, rep2) {
		t.Errorf("a recompute differs:\n%+v\n%+v", d, d2)
	}
}

func TestOtherGroupPerResource(t *testing.T) {
	a, b := ref("HTTPRoute", "a"), ref("HTTPRoute", "b")
	rep := &health.Report{Findings: []health.Finding{
		finding("a-1", health.CodeConditionFalse, health.SeverityWarning, a, nil),
		finding("b-1", health.CodeConditionFalse, health.SeverityWarning, b, nil),
		finding("a-2", health.CodeRouteUnattached, health.SeverityError, a, nil),
		finding("install", health.CodeScopeMismatch, health.SeverityWarning, nil, nil),
	}}
	d := diagnose.Build(rep, in, diagnose.Families())
	got := map[string]string{}
	for _, g := range d.Groups {
		got[groupNames(rep, &diagnose.Diagnosis{Groups: []diagnose.Group{g}})[0]] = g.ID
		if g.Family != "Other" || len(g.Affected) != 0 {
			t.Errorf("group %+v", g)
		}
	}
	id := in.Install.ID()
	want := map[string]string{"a-1+a-2": health.GroupID(id, "Other", a, nil), "b-1": health.GroupID(id, "Other", b, nil),
		"install": health.GroupID(id, "Other", nil, nil)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups %v, want %v", got, want)
	}
	if d.Groups[0].Cause == nil || *d.Groups[0].Cause != *a || d.Groups[0].Severity != health.SeverityError {
		t.Errorf("first group %+v, want the error group on a", d.Groups[0])
	}
}

func TestSignalUnavailableNotRanked(t *testing.T) {
	rep := &health.Report{Findings: []health.Finding{
		finding("signal", health.CodeSignalUnavailable, health.SeverityInfo, nil, ago(time.Hour)),
		finding("scope", health.CodeScopeMismatch, health.SeverityInfo, nil, nil),
		finding("info", health.CodeConditionFalse, health.SeverityInfo, ref("HTTPRoute", "i"), nil),
	}}
	d := diagnose.Build(rep, in, diagnose.Families())
	if got, want := groupNames(rep, d), []string{"scope", "info", "signal"}; !slices.Equal(got, want) &&
		!slices.Equal(got, []string{"info", "scope", "signal"}) {
		t.Fatalf("groups %v, want KGWUI-008 last and on its own", got)
	}
	last := d.Groups[len(d.Groups)-1]
	if last.Ranked || !d.Groups[0].Ranked || !d.Groups[1].Ranked {
		t.Errorf("ranked flags %v %v %v", d.Groups[0].Ranked, d.Groups[1].Ranked, last.Ranked)
	}
	if rep.Findings[0].Group != last.ID || rep.Findings[0].Group == rep.Findings[1].Group {
		t.Errorf("KGWUI-008 group %q, scope group %q", rep.Findings[0].Group, rep.Findings[1].Group)
	}
	if got := names(rep.Findings, d.Rank); got[len(got)-1] != "signal" {
		t.Errorf("finding rank %v, want KGWUI-008 last", got)
	}
}
