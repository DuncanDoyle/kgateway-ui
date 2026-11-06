package health

import (
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/kgw23"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/kgw24"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/rulesets"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// t0 is when every object's current generation was first seen; grace ends at t0 + Grace.
var t0 = testutil.Now.Add(-time.Hour)

// tRef is a targetRefs entry; tSel a targetSelectors entry.
func tRef(kind, name, section string) map[string]any {
	m := map[string]any{"group": objects.GatewayAPIGroup, "kind": kind, "name": name}
	if section != "" {
		m["sectionName"] = section
	}
	return m
}

func tSel(kind string, labels map[string]any) map[string]any {
	return map[string]any{"group": objects.GatewayAPIGroup, "kind": kind, "matchLabels": labels}
}

// targetPolicy is TrafficPolicy ns/tp with refs, selectors and ancestors.
func targetPolicy(ns string, refs, sels []map[string]any, ancestors ...testutil.PolicyAncestor) *unstructured.Unstructured {
	u := testutil.TrafficPolicy(ns, "tp", ancestors...)
	spec := map[string]any{}
	if len(refs) > 0 {
		spec["targetRefs"] = toAny(refs)
	}
	if len(sels) > 0 {
		spec["targetSelectors"] = toAny(sels)
	}
	u.Object["spec"] = spec
	return u
}

func toAny(ms []map[string]any) []any {
	out := make([]any, len(ms))
	for i, m := range ms {
		out[i] = m
	}
	return out
}

// accepted is a current policy ancestor on gw by this install's controller.
func accepted(gw objects.ParentRef) testutil.PolicyAncestor {
	return testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Valid")}}
}

// targetsRun evaluates the healthy world plus objs on the 2.4 rule set at
// each offset after the grace end, sharing one States; it returns the last report.
func targetsRun(t *testing.T, sel computed.Selection, objs []*unstructured.Unstructured, after ...time.Duration) Report {
	t.Helper()
	return targetsRunWith(t, sel, nil, objs, after...)
}

// targetsRunWith is targetsRun with the controller's global policy namespace setting.
func targetsRunWith(t *testing.T, sel computed.Selection, global *install.Setting, objs []*unstructured.Unstructured, after ...time.Duration) Report {
	t.Helper()
	w := healthyWorld()
	w.objs = append(w.objs, objs...)
	w.firstSeen = func(*unstructured.Unstructured) time.Time { return t0 }
	in := w.input(t)
	in.Rules = sel
	if global != nil {
		in.Install.Settings.GlobalPolicyNamespace = *global
	}
	rt := Runtime{States: NewStates()}
	var r Report
	for _, d := range after {
		in.Now = t0.Add(Grace + d)
		r = Evaluate(in, Upstream(), rt)
	}
	return r
}

var rules24 = computed.Selection{Rules: kgw24.New()}

// matured runs one world in the grace (so the hold starts early), at grace + 29 s and at grace + 31 s.
func matured(t *testing.T, objs ...*unstructured.Unstructured) (before, after Report) {
	t.Helper()
	before = targetsRun(t, rules24, objs, -Grace+time.Second, 29*time.Second)
	after = targetsRun(t, rules24, objs, -Grace+time.Second, 29*time.Second, 31*time.Second)
	return before, after
}

func targetFindings(r Report, res string) []Finding {
	return findingsFor(r, CodePolicyTargetNotFound, res)
}

// Design §7.6: a lone targetRef to a missing Gateway is a computed 016 warning
// after grace + 30 s, labelled with the rule and rule set; it replaces 003.
func TestPolicyTargetNotFound(t *testing.T) {
	tp := targetPolicy("infra", []map[string]any{tRef("Gateway", "nope", "")}, nil)
	before, after := matured(t, tp)
	if fs := targetFindings(before, "infra/tp"); len(fs) != 0 {
		t.Fatalf("016 before grace + 30 s: %+v", fs)
	}
	if !has(before, CodeRouteUnattached, SeverityWarning, "infra/tp") {
		t.Fatalf("003 warning expected until 016 matures: %+v", before.Findings)
	}
	fs := targetFindings(after, "infra/tp")
	if len(fs) != 1 {
		t.Fatalf("want one 016, got %+v", after.Findings)
	}
	f := fs[0]
	if f.Severity != SeverityWarning || f.Slug != "policy-target-not-found" || f.Message != PolicyTargetNotFoundMessage {
		t.Fatalf("016 %+v", f)
	}
	for _, s := range []string{"targetRefs[0] Gateway `nope`", "not found in namespace infra", "policy targets rule", "Rule set kgateway-2.4."} {
		if !strings.Contains(f.Detail, s) {
			t.Errorf("detail %q lacks %q", f.Detail, s)
		}
	}
	if strings.Contains(f.Detail, "Not verified") {
		t.Errorf("a lone Gateway target is verifiable: %q", f.Detail)
	}
	if has(after, CodeRouteUnattached, "", "infra/tp") {
		t.Fatalf("016 replaces 003: %+v", after.Findings)
	}
	if has(after, CodeComputedDisagrees, "", "infra/tp") {
		t.Fatalf("no ancestor: nothing disagrees: %+v", after.Findings)
	}
}

// The same world on 2.3 raises the same 016; the rule set names itself.
func TestPolicyTargetNotFoundOn23(t *testing.T) {
	tp := targetPolicy("infra", []map[string]any{tRef("Gateway", "nope", "")}, nil)
	r := targetsRun(t, computed.Selection{Rules: kgw23.New()}, []*unstructured.Unstructured{tp}, 0, 31*time.Second)
	if fs := targetFindings(r, "infra/tp"); len(fs) != 1 || !strings.Contains(fs[0].Detail, "Rule set kgateway-2.3.") {
		t.Fatalf("016 on 2.3: %+v", r.Findings)
	}
}

func TestPolicyTargetSectionNotFound(t *testing.T) {
	tp := targetPolicy("infra", []map[string]any{tRef("Gateway", "edge", "nope")}, nil)
	_, after := matured(t, tp)
	fs := targetFindings(after, "infra/tp")
	if len(fs) != 1 || !strings.Contains(fs[0].Detail, "section `nope` not found") {
		t.Fatalf("section not found: %+v", after.Findings)
	}
}

// An empty matchLabels selects nothing; a selector is never verifiable.
func TestPolicyTargetEmptySelector(t *testing.T) {
	tp := targetPolicy("shop", nil, []map[string]any{tSel("HTTPRoute", map[string]any{})})
	_, after := matured(t, tp)
	fs := targetFindings(after, "shop/tp")
	if len(fs) != 1 || !strings.Contains(fs[0].Detail, "matches nothing") || !strings.Contains(fs[0].Detail, TargetNotVerified) {
		t.Fatalf("empty selector: %+v", after.Findings)
	}
	if has(after, CodeRouteUnattached, "", "shop/tp") {
		t.Fatalf("016 replaces 003: %+v", after.Findings)
	}
}

// The UI does not read the global policy namespace, so a labelled selector
// that matches nothing locally is unknown and stays 003 (R91).
func TestPolicyTargetSelectorUnknownWithoutGlobalNamespace(t *testing.T) {
	tp := targetPolicy("shop", nil, []map[string]any{tSel("HTTPRoute", map[string]any{"pick": "never"})})
	_, after := matured(t, tp)
	if fs := targetFindings(after, "shop/tp"); len(fs) != 0 {
		t.Fatalf("unknown selector raised 016: %+v", fs)
	}
	if !has(after, CodeRouteUnattached, SeverityWarning, "shop/tp") {
		t.Fatalf("003 stays: %+v", after.Findings)
	}
}

// Two targets, one missing: one 016 for it, not verified, and the policy's
// other findings unchanged.
func TestPolicyTargetPartial(t *testing.T) {
	edge := testutil.GwRef("infra", "edge")
	both := targetPolicy("infra", []map[string]any{tRef("Gateway", "edge", ""), tRef("Gateway", "nope", "")}, nil, accepted(edge))
	found := targetPolicy("infra", []map[string]any{tRef("Gateway", "edge", "")}, nil, accepted(edge))
	_, after := matured(t, both)
	fs := targetFindings(after, "infra/tp")
	if len(fs) != 1 || !strings.Contains(fs[0].Detail, "targetRefs[1] Gateway `nope`") || !strings.Contains(fs[0].Detail, TargetNotVerified) {
		t.Fatalf("partial: %+v", after.Findings)
	}
	_, base := matured(t, found)
	rest := slices.DeleteFunc(slices.Clone(after.Findings), func(f Finding) bool { return f.Code == CodePolicyTargetNotFound })
	if len(rest) != len(base.Findings) {
		t.Fatalf("other findings changed:\n%+v\n%+v", rest, base.Findings)
	}
	if has(after, CodeComputedDisagrees, "", "infra/tp") {
		t.Fatalf("a multi-target policy is never a 012: %+v", after.Findings)
	}
}

// Every target missing: one 016 each, no 003.
func TestPolicyTargetAllMissing(t *testing.T) {
	tp := targetPolicy("infra", []map[string]any{tRef("Gateway", "nope", ""), tRef("Gateway", "gone", "")}, nil)
	_, after := matured(t, tp)
	if fs := targetFindings(after, "infra/tp"); len(fs) != 2 {
		t.Fatalf("want two 016, got %+v", after.Findings)
	}
	if has(after, CodeRouteUnattached, "", "infra/tp") {
		t.Fatalf("016 replaces 003: %+v", after.Findings)
	}
}

// The verifier: a lone target the rule calls missing with a current ancestor
// for it is a disagreement, KGWUI-012 of kind policy-target.
func TestPolicyTargetVerifier(t *testing.T) {
	edge, other := testutil.GwRef("infra", "edge"), testutil.GwRef("infra", "other")
	ls := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	notFound := testutil.PolicyAncestor{Ref: edge, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "TargetNotFound")}}
	cases := []struct {
		name     string
		objs     []*unstructured.Unstructured
		res      string
		want012  bool
		verified bool
	}{
		{"Gateway section, ancestor on the Gateway",
			[]*unstructured.Unstructured{targetPolicy("infra", []map[string]any{tRef("Gateway", "edge", "nope")}, nil, accepted(edge))}, "infra/tp", true, true},
		{"route section, ancestor on the route's Gateway",
			[]*unstructured.Unstructured{targetPolicy("shop", []map[string]any{tRef("HTTPRoute", "cart", "nope")}, nil, accepted(edge))}, "shop/tp", true, true},
		{"route on Gateway A, ancestor on Gateway B",
			[]*unstructured.Unstructured{testutil.Gateway("infra", "other", "kgateway", nil),
				targetPolicy("shop", []map[string]any{tRef("HTTPRoute", "cart", "nope")}, nil, accepted(other))}, "shop/tp", false, true},
		{"the controller also reports TargetNotFound",
			[]*unstructured.Unstructured{targetPolicy("infra", []map[string]any{tRef("Gateway", "edge", "nope")}, nil, notFound)}, "infra/tp", false, true},
		{"route not found is not verified",
			[]*unstructured.Unstructured{targetPolicy("shop", []map[string]any{tRef("HTTPRoute", "nope", "")}, nil, accepted(edge))}, "shop/tp", false, false},
		{"route whose parent is a ListenerSet is not verified",
			[]*unstructured.Unstructured{testutil.HTTPRoute("shop", "lsr", []objects.ParentRef{edge, ls}),
				targetPolicy("shop", []map[string]any{tRef("HTTPRoute", "lsr", "nope")}, nil, accepted(edge))}, "shop/tp", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, after := matured(t, tc.objs...)
			if has(before, CodeComputedDisagrees, "", tc.res) {
				t.Fatalf("012 before the hold: %+v", before.Findings)
			}
			fs := targetFindings(after, tc.res)
			if len(fs) != 1 || strings.Contains(fs[0].Detail, TargetNotVerified) == tc.verified {
				t.Fatalf("016 (verified %v): %+v", tc.verified, after.Findings)
			}
			got := findingsFor(after, CodeComputedDisagrees, tc.res)
			if (len(got) == 1) != tc.want012 || len(got) > 1 {
				t.Fatalf("012 want %v: %+v", tc.want012, after.Findings)
			}
			if tc.want012 {
				f := got[0]
				for _, s := range []string{"section not found", "controller reports ancestor Gateway infra/edge", "Rule set kgateway-2.4."} {
					if !strings.Contains(f.Detail, s) {
						t.Errorf("012 detail %q lacks %q", f.Detail, s)
					}
				}
				if f.Severity != SeverityWarning || !slices.Contains(f.Evidence, edge.Ref()) {
					t.Errorf("012 %+v", f)
				}
			}
		})
	}
}

// D-024: an unknown discovery scope makes the rule unknown: no 016.
func TestPolicyTargetScopeUnknown(t *testing.T) {
	w := healthyWorld()
	w.objs = append(w.objs, targetPolicy("infra", []map[string]any{tRef("Gateway", "nope", "")}, nil))
	w.firstSeen = func(*unstructured.Unstructured) time.Time { return t0 }
	in := w.input(t)
	in.Rules, in.Install.Scope = rules24, install.Scope{Mode: install.ScopeUnknown}
	rt := Runtime{States: NewStates()}
	for _, d := range []time.Duration{0, time.Hour} {
		in.Now = t0.Add(Grace + d)
		if r := Evaluate(in, Upstream(), rt); has(r, CodePolicyTargetNotFound, "", "") {
			t.Fatalf("016 under unknown scope: %+v", r.Findings)
		}
	}
}

// A rule set without PolicyTargeter, and a target kind the rule does not judge, raise nothing.
func TestPolicyTargetNothingToJudge(t *testing.T) {
	missing := targetPolicy("infra", []map[string]any{tRef("Gateway", "nope", "")}, nil)
	if r := targetsRun(t, computed.Selection{}, []*unstructured.Unstructured{missing}, 0, time.Hour); has(r, CodePolicyTargetNotFound, "", "") {
		t.Fatalf("no rule set: %+v", r.Findings)
	}
	svc := targetPolicy("shop", []map[string]any{{"group": "", "kind": "Service", "name": "nope"}}, nil)
	if r := targetsRun(t, rules24, []*unstructured.Unstructured{svc}, 0, time.Hour); has(r, CodePolicyTargetNotFound, "", "") {
		t.Fatalf("Service target: %+v", r.Findings)
	}
}

// Review Focus 5: a target that arrives during the hold raises nothing, and
// one that disappears again restarts the hold.
func TestPolicyTargetHoldRestarts(t *testing.T) {
	tp := targetPolicy("infra", []map[string]any{tRef("Gateway", "late", "")}, nil)
	late := testutil.Gateway("infra", "late", "kgateway", nil)
	w := healthyWorld()
	w.firstSeen = func(*unstructured.Unstructured) time.Time { return t0 }
	in := func(present bool) Input {
		w.objs = append(healthyWorld().objs, tp)
		if present {
			w.objs = append(w.objs, late)
		}
		in := w.input(t)
		in.Rules = rules24
		return in
	}
	rt := Runtime{States: NewStates()}
	steps := []struct {
		at      time.Duration
		present bool
		want    bool
	}{
		{10 * time.Second, false, false},
		{20 * time.Second, true, false},  // arrives in the hold
		{25 * time.Second, false, false}, // gone again: the hold starts over
		{41 * time.Second, false, false}, // 31 s after the first sight, 16 s into the new hold
		{54 * time.Second, false, false},
		{56 * time.Second, false, true},
	}
	for _, s := range steps {
		x := in(s.present)
		x.Now = t0.Add(Grace + s.at)
		r := Evaluate(x, Upstream(), rt)
		if got := has(r, CodePolicyTargetNotFound, "", "infra/tp"); got != s.want {
			t.Fatalf("grace + %s (target present %v): 016 %v, want %v: %+v", s.at, s.present, got, s.want, r.Findings)
		}
	}
}

// The hold asks for a recompute when it matures.
func TestPolicyTargetWakesAtMaturity(t *testing.T) {
	tp := targetPolicy("infra", []map[string]any{tRef("Gateway", "nope", "")}, nil)
	r := targetsRun(t, rules24, []*unstructured.Unstructured{tp}, 5*time.Second)
	if want := t0.Add(Grace + 5*time.Second + TargetHold); !r.Next.Equal(want) {
		t.Fatalf("next %v, want %v", r.Next, want)
	}
}

// A policy with no namespace is never searched for: its targets are unknown.
func TestPolicyTargetEmptyNamespaceUnknown(t *testing.T) {
	tp := targetPolicy("", []map[string]any{tRef("Gateway", "nope", "")}, nil)
	w := healthyWorld()
	in := w.input(t)
	in.Rules = rules24
	refs, sels := computed.DecodeTargets(tp)
	if rs := in.targetResults(rules24.PolicyTargeter(), tp, refs, sels); len(rs) != 0 {
		t.Fatalf("results %+v", rs)
	}
}

// R93: with the global policy namespace setting read, a labelled selector that
// matches nothing is a 016; one in the global namespace may match elsewhere.
func TestPolicyTargetLabelledSelector(t *testing.T) {
	none := &install.Setting{Known: true}
	global := &install.Setting{Known: true, Value: "infra"}
	unread := &install.Setting{Reason: "valueFrom"}
	never := targetPolicy("shop", nil, []map[string]any{tSel("HTTPRoute", map[string]any{"pick": "never"})})
	r := targetsRunWith(t, rules24, none, []*unstructured.Unstructured{never}, 0, 31*time.Second)
	fs := targetFindings(r, "shop/tp")
	if len(fs) != 1 || !strings.Contains(fs[0].Detail, "targetSelectors[0] HTTPRoute `pick=never`: matches nothing in namespace shop") ||
		!strings.Contains(fs[0].Detail, TargetNotVerified) {
		t.Fatalf("labelled selector: %+v", r.Findings)
	}
	if r := targetsRunWith(t, rules24, unread, []*unstructured.Unstructured{never}, 0, 31*time.Second); has(r, CodePolicyTargetNotFound, "", "") {
		t.Fatalf("unread setting: %+v", r.Findings)
	}
	// shop/cart carries the label; a policy in the global namespace infra selects it.
	cart := testutil.HTTPRoute("shop", "cart", []objects.ParentRef{testutil.GwRef("infra", "edge")}, testutil.AcceptedBy(testutil.GwRef("infra", "edge"), testutil.Controller))
	cart.SetLabels(map[string]string{"pick": "yes"})
	inGlobal := targetPolicy("infra", nil, []map[string]any{tSel("HTTPRoute", map[string]any{"pick": "yes"})})
	if r := targetsRunWith(t, rules24, global, []*unstructured.Unstructured{cart, inGlobal}, 0, 31*time.Second); has(r, CodePolicyTargetNotFound, "", "infra/tp") {
		t.Fatalf("global namespace selector: %+v", r.Findings)
	}
	if r := targetsRunWith(t, rules24, none, []*unstructured.Unstructured{cart, inGlobal}, 0, 31*time.Second); !has(r, CodePolicyTargetNotFound, "", "infra/tp") {
		t.Fatalf("without a global namespace the selector is local: %+v", r.Findings)
	}
}

// R93 (phase-2 rules §9.3): the verifier's 012 has its own 30 s window from
// the ancestor's first sight, with that as since; it restarts when the ancestor goes.
func TestPolicyTargetVerifierWindow(t *testing.T) {
	edge := testutil.GwRef("infra", "edge")
	bare := targetPolicy("infra", []map[string]any{tRef("Gateway", "edge", "nope")}, nil)
	withAncestor := targetPolicy("infra", []map[string]any{tRef("Gateway", "edge", "nope")}, nil, accepted(edge))
	w := healthyWorld()
	w.firstSeen = func(*unstructured.Unstructured) time.Time { return t0 }
	rt := Runtime{States: NewStates()}
	steps := []struct {
		at       time.Duration
		ancestor bool
		want     bool
	}{
		{0, false, false},
		{40 * time.Second, true, false},  // 016 matured; the ancestor appears: the window starts
		{69 * time.Second, true, false},  // 29 s into the window
		{71 * time.Second, true, true},   // 31 s
		{80 * time.Second, false, false}, // gone: the window ends
		{90 * time.Second, true, false},  // back: a new window
		{119 * time.Second, true, false},
		{121 * time.Second, true, true},
	}
	for _, s := range steps {
		p := bare
		if s.ancestor {
			p = withAncestor
		}
		w.objs = append(healthyWorld().objs, p)
		in := w.input(t)
		in.Rules, in.Now = rules24, t0.Add(Grace+s.at)
		r := Evaluate(in, Upstream(), rt)
		got := findingsFor(r, CodeComputedDisagrees, "infra/tp")
		if (len(got) == 1) != s.want || len(got) > 1 {
			t.Fatalf("grace + %s (ancestor %v): 012 %+v, want %v", s.at, s.ancestor, got, s.want)
		}
		if !has(r, CodePolicyTargetNotFound, "", "infra/tp") && s.at > 30*time.Second {
			t.Fatalf("grace + %s: the 016 stays: %+v", s.at, r.Findings)
		}
		if s.want {
			start := t0.Add(Grace + 40*time.Second)
			if s.at > 100*time.Second {
				start = t0.Add(Grace + 90*time.Second)
			}
			if got[0].Since == nil || !got[0].Since.Equal(start) {
				t.Fatalf("grace + %s: since %v, want %v", s.at, got[0].Since, start)
			}
		}
	}
}

// rules25 is the selection a 2.5 controller gets.
var rules25 = rulesets.Select("v2.5.0")

// summary is the StatusSummary ancestor a 2.5 controller writes for a missing targetRef
// (policy_target_status.go:294-307 at v2.5.0), with the conditions' observedGeneration.
func summary(gen int64) testutil.PolicyAncestor {
	acc := testutil.Cond("Accepted", metav1.ConditionFalse, "TargetNotFound")
	acc.Message, acc.ObservedGeneration = "Gateway infra/nope not found", gen
	att := testutil.Cond("Attached", metav1.ConditionFalse, "TargetNotFound")
	att.Message, att.ObservedGeneration = "policy target not found", gen
	return testutil.PolicyAncestor{Ref: objects.ParentRef{Group: "gateway.kgateway.dev", Kind: "StatusSummary", Name: "StatusSummary"},
		Controller: testutil.Controller, Conds: []metav1.Condition{acc, att}}
}

// D-020 rule 5: on kgateway-2.5 a current TargetNotFound ancestor is one 016 error, at once.
// No 002 or 003 beside it.
func TestPolicyTargetNotFoundFromStatus(t *testing.T) {
	tp := targetPolicy("infra", []map[string]any{tRef("Gateway", "nope", "")}, nil, summary(1))
	r := targetsRun(t, rules25, []*unstructured.Unstructured{tp}, 0)
	fs := targetFindings(r, "infra/tp")
	if len(fs) != 1 {
		t.Fatalf("want one 016, got %+v", r.Findings)
	}
	f := fs[0]
	if f.Severity != SeverityError || f.Slug != "policy-target-not-found" || f.Message != PolicyTargetNotFoundMessage ||
		f.Since == nil || !f.Since.Equal(testutil.Old.Time) {
		t.Fatalf("016 %+v", f)
	}
	for _, s := range []string{"reason TargetNotFound", "Gateway infra/nope not found", "ancestor StatusSummary"} {
		if !strings.Contains(f.Detail, s) {
			t.Errorf("detail %q lacks %q", f.Detail, s)
		}
	}
	if strings.Contains(f.Detail, "Computed by the UI") {
		t.Errorf("a status 016 is not computed: %q", f.Detail)
	}
	if has(r, CodeConditionFalse, "", "infra/tp") || has(r, CodeRouteUnattached, "", "infra/tp") {
		t.Fatalf("016 replaces 002 and 003: %+v", r.Findings)
	}
}

// kgateway-2.5 computes no 016; a stale or foreign TargetNotFound raises none either.
func TestPolicyTargetComputedOffOn25(t *testing.T) {
	if rules25.PolicyTargeter() != nil {
		t.Fatalf("%s has a PolicyTargeter", rules25.Rules.Version())
	}
	bare := targetPolicy("infra", []map[string]any{tRef("Gateway", "nope", "")}, nil)
	r := targetsRun(t, rules25, []*unstructured.Unstructured{bare}, -Grace+time.Second, time.Hour)
	if has(r, CodePolicyTargetNotFound, "", "") || !has(r, CodeRouteUnattached, SeverityWarning, "infra/tp") {
		t.Fatalf("no status: want 003 only: %+v", r.Findings)
	}
	stale := targetPolicy("infra", []map[string]any{tRef("Gateway", "nope", "")}, nil, summary(0))
	stale.SetGeneration(2)
	r = targetsRun(t, rules25, []*unstructured.Unstructured{stale}, time.Hour)
	if has(r, CodePolicyTargetNotFound, "", "") || !has(r, CodeRouteUnattached, "", "infra/tp") {
		t.Fatalf("stale: want 003 only: %+v", r.Findings)
	}
	foreign := summary(1)
	foreign.Controller = "example.com/other"
	r = targetsRun(t, rules25, []*unstructured.Unstructured{targetPolicy("infra", []map[string]any{tRef("Gateway", "nope", "")}, nil, foreign)}, time.Hour)
	if has(r, CodePolicyTargetNotFound, "", "") {
		t.Fatalf("foreign controller: %+v", r.Findings)
	}
}

// wrapped25 wraps kgw25 as a downstream rule set does and forwards the status hook.
type wrapped25 struct{ computed.Rules }

func (w wrapped25) ReportsTargetNotFound() bool {
	r := computed.Selection{Rules: w.Rules}.TargetStatusReporter()
	return r != nil && r.ReportsTargetNotFound()
}

// A wrapped kgw25 that forwards computed.TargetStatusReporter keeps the 016 from
// status; one that only embeds computed.Rules loses it.
func TestPolicyTargetFromStatusThroughWrapper(t *testing.T) {
	tp := targetPolicy("infra", []map[string]any{tRef("Gateway", "nope", "")}, nil, summary(1))
	fwd := rules25
	fwd.Rules = wrapped25{rules25.Rules}
	if fwd.TargetStatusReporter() == nil {
		t.Fatal("the forwarding wrapper is no TargetStatusReporter")
	}
	r := targetsRun(t, fwd, []*unstructured.Unstructured{tp}, 0)
	if fs := targetFindings(r, "infra/tp"); len(fs) != 1 || has(r, CodeConditionFalse, "", "infra/tp") {
		t.Fatalf("forwarding wrapper: want one 016 and no 002: %+v", r.Findings)
	}
	bare := rules25
	bare.Rules = struct{ computed.Rules }{rules25.Rules}
	if bare.TargetStatusReporter() != nil {
		t.Fatal("a bare embedding offers the hook")
	}
	r = targetsRun(t, bare, []*unstructured.Unstructured{tp}, 0)
	if has(r, CodePolicyTargetNotFound, "", "infra/tp") {
		t.Fatalf("bare wrapper: a status 016: %+v", r.Findings)
	}
}

// One target found, one missing: the found one's ancestor stays healthy, the
// summary is one 016, and nothing else is raised.
func TestPolicyTargetPartlyMissingFromStatus(t *testing.T) {
	tp := targetPolicy("infra", []map[string]any{tRef("Gateway", "edge", ""), tRef("Gateway", "nope", "")}, nil,
		accepted(testutil.GwRef("infra", "edge")), summary(1))
	r := targetsRun(t, rules25, []*unstructured.Unstructured{tp}, 0)
	var other []Finding
	for _, f := range r.Findings {
		if f.Resource != nil && f.Resource.String() == "infra/tp" && f.Code != CodePolicyTargetNotFound {
			other = append(other, f)
		}
	}
	if len(targetFindings(r, "infra/tp")) != 1 || len(other) != 0 {
		t.Fatalf("want one 016 only: %+v", r.Findings)
	}
}

// kgw24 is unchanged: the same TargetNotFound ancestor stays a 002 per condition.
func TestPolicyTargetStatusOn24Unchanged(t *testing.T) {
	tp := targetPolicy("infra", []map[string]any{tRef("Gateway", "edge", "")}, nil, summary(1))
	r := targetsRun(t, rules24, []*unstructured.Unstructured{tp}, time.Hour)
	if fs := findingsFor(r, CodeConditionFalse, "infra/tp"); len(fs) != 2 || has(r, CodePolicyTargetNotFound, "", "") {
		t.Fatalf("2.4: want two 002, no 016: %+v", r.Findings)
	}
}

// D-040: the status 016 id hashes the ancestor only, so Accepted vs Attached cannot change it.
func TestPolicyTargetStatusIDFromAncestor(t *testing.T) {
	attachedOnly := summary(1)
	attachedOnly.Conds[0] = testutil.Cond("Accepted", metav1.ConditionTrue, "Valid")
	var ids []string
	for _, a := range []testutil.PolicyAncestor{summary(1), attachedOnly} {
		tp := targetPolicy("infra", []map[string]any{tRef("Gateway", "nope", "")}, nil, a)
		fs := targetFindings(targetsRun(t, rules25, []*unstructured.Unstructured{tp}, 0), "infra/tp")
		if len(fs) != 1 {
			t.Fatalf("want one 016: %+v", fs)
		}
		ids = append(ids, fs[0].ID)
	}
	if ids[0] != ids[1] {
		t.Fatalf("id depends on the condition type: %v", ids)
	}
}
