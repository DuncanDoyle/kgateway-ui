package health

import (
	"reflect"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// policyCatalog serves the kgateway policy kinds and DirectResponse, with the unknown groups failed.
func policyCatalog(unknown ...string) kinds.Catalog {
	return kinds.NewCatalog([]schema.GroupKind{{Kind: "Service"}, {Group: objects.GatewayAPIGroup, Kind: "HTTPRoute"},
		{Group: kinds.KgatewayGroup, Kind: "TrafficPolicy"}, {Group: kinds.KgatewayGroup, Kind: "DirectResponse"}}, unknown)
}

// ruleRoute is shop/cart on infra/edge whose parent status carries cond, as
// kgateway 2.3 (Accepted) or 2.4 (kgateway.dev/Programmed) writes it.
func ruleRoute(cond metav1.Condition) *unstructured.Unstructured {
	gw := testutil.GwRef("infra", "edge")
	conds := []metav1.Condition{cond, testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}
	if cond.Type != "Accepted" {
		conds = append(conds, testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"))
	}
	return testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: conds})
}

func ruleCond(typ, reason, msg string, since time.Time) metav1.Condition {
	c := testutil.Cond(typ, metav1.ConditionFalse, reason)
	c.Message, c.LastTransitionTime = msg, metav1.NewTime(since)
	return c
}

// ruleReport evaluates the healthy world with route u and catalog c.
func ruleReport(t *testing.T, u *unstructured.Unstructured, c kinds.Catalog) Report {
	t.Helper()
	w := healthyWorld()
	w.objs[2] = u
	return refsReport(t, w, c)
}

// onlyRule returns the one 015 on shop/cart and its evidence; no 002 may remain (D-030).
func onlyRule(t *testing.T, r Report) (Finding, *RuleReplacedEvidence) {
	t.Helper()
	if fs := findingsFor(r, CodeConditionFalse, "shop/cart"); len(fs) != 0 {
		t.Fatalf("002 next to 015: %+v", fs)
	}
	fs := findingsFor(r, CodeRuleReplaced, "shop/cart")
	if len(fs) != 1 {
		t.Fatalf("want one 015 on shop/cart, got %+v", r.Findings)
	}
	ev, ok := fs[0].Facts.(*RuleReplacedEvidence)
	if !ok {
		t.Fatalf("evidence %#v, want rule-replaced", fs[0].Facts)
	}
	return fs[0], ev
}

func intp(n int) *int { return &n }

// Design §7.4: 2.3 reports through Accepted=False, 2.4 through
// kgateway.dev/Programmed=False; both are 015 with the match index, never 002.
func TestRuleReplacedOrDropped(t *testing.T) {
	tp := &objects.Ref{Group: kinds.KgatewayGroup, Kind: "TrafficPolicy", Namespace: "shop", Name: "auth"}
	since := testutil.Now.Add(-time.Minute)
	long := "Replaced Rule (1): " + strings.Repeat("é", 1024)
	cases := []struct {
		name    string
		cond    metav1.Condition
		catalog kinds.Catalog
		message string
		want    RuleReplacedEvidence
	}{
		{"2.3 Accepted=False, replaced by a served policy",
			ruleCond("Accepted", "RouteRuleReplaced", "Replaced Rule (2): gateway.kgateway.dev/TrafficPolicy/shop/auth: ext auth unavailable", since),
			policyCatalog(), "Rule replaced by a direct response",
			RuleReplacedEvidence{MatchIndex: intp(2), Reason: "RouteRuleReplaced", Message: "Replaced Rule (2): gateway.kgateway.dev/TrafficPolicy/shop/auth: ext auth unavailable", Policy: tp}},
		{"2.4 kgateway.dev/Programmed=False, dropped",
			ruleCond("kgateway.dev/Programmed", "RouteRuleDropped", "Dropped Rule (0): gateway.kgateway.dev/TrafficPolicy/shop/auth: invalid", since),
			policyCatalog(), "Rule dropped because of invalid configuration",
			RuleReplacedEvidence{MatchIndex: intp(0), Reason: "RouteRuleDropped", Message: "Dropped Rule (0): gateway.kgateway.dev/TrafficPolicy/shop/auth: invalid", Policy: tp}},
		{"unparseable message is quoted, not linked",
			ruleCond("Accepted", "RouteRuleReplaced", "rule 2 is gone: gateway.kgateway.dev/TrafficPolicy/shop/auth", since),
			policyCatalog(), "Rule replaced by a direct response",
			RuleReplacedEvidence{Reason: "RouteRuleReplaced", Message: "rule 2 is gone: gateway.kgateway.dev/TrafficPolicy/shop/auth"}},
		{"a served kind that is not a policy kind is not linked",
			ruleCond("Accepted", "RouteRuleReplaced", "Replaced Rule (0): gateway.kgateway.dev/DirectResponse/shop/dr: policy not found", since),
			policyCatalog(), "Rule replaced by a direct response",
			RuleReplacedEvidence{MatchIndex: intp(0), Reason: "RouteRuleReplaced", Message: "Replaced Rule (0): gateway.kgateway.dev/DirectResponse/shop/dr: policy not found"}},
		// R77: an unobserved served state is never treated as known.
		{"catalog not complete",
			ruleCond("Accepted", "RouteRuleReplaced", "Replaced Rule (2): gateway.kgateway.dev/TrafficPolicy/shop/auth: x", since),
			policyCatalog("metrics.example"), "Rule replaced by a direct response",
			RuleReplacedEvidence{MatchIndex: intp(2), Reason: "RouteRuleReplaced", Message: "Replaced Rule (2): gateway.kgateway.dev/TrafficPolicy/shop/auth: x"}},
		{"no catalog",
			ruleCond("Accepted", "RouteRuleReplaced", "Replaced Rule (2): gateway.kgateway.dev/TrafficPolicy/shop/auth: x", since),
			kinds.Catalog{}, "Rule replaced by a direct response",
			RuleReplacedEvidence{MatchIndex: intp(2), Reason: "RouteRuleReplaced", Message: "Replaced Rule (2): gateway.kgateway.dev/TrafficPolicy/shop/auth: x"}},
		{"two policies named: none is the cause",
			ruleCond("Accepted", "RouteRuleReplaced", "Replaced Rule (3): gateway.kgateway.dev/TrafficPolicy/shop/auth: x; gateway.kgateway.dev/TrafficPolicy/shop/rl: y", since),
			policyCatalog(), "Rule replaced by a direct response",
			RuleReplacedEvidence{MatchIndex: intp(3), Reason: "RouteRuleReplaced", Message: "Replaced Rule (3): gateway.kgateway.dev/TrafficPolicy/shop/auth: x; gateway.kgateway.dev/TrafficPolicy/shop/rl: y"}},
		// kgateway joins rule errors with "\n" (summarizeRuleErrors), so the parse spans lines.
		{"errors joined with a newline",
			ruleCond("kgateway.dev/Programmed", "RouteRuleReplaced", "Replaced Rule (1): gateway.kgateway.dev/TrafficPolicy/shop/auth: x\ngateway.kgateway.dev/TrafficPolicy/shop/auth: y", since),
			policyCatalog(), "Rule replaced by a direct response",
			RuleReplacedEvidence{MatchIndex: intp(1), Reason: "RouteRuleReplaced", Message: "Replaced Rule (1): gateway.kgateway.dev/TrafficPolicy/shop/auth: x\ngateway.kgateway.dev/TrafficPolicy/shop/auth: y", Policy: tp}},
		{"long message is clipped",
			ruleCond("Accepted", "RouteRuleReplaced", long, since),
			policyCatalog(), "Rule replaced by a direct response",
			RuleReplacedEvidence{MatchIndex: intp(1), Reason: "RouteRuleReplaced", Message: computed.ClipDisplay(long, computed.MaxStatusBytes)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, ev := onlyRule(t, ruleReport(t, ruleRoute(tc.cond), tc.catalog))
			if f.Slug != "rule-replaced" || f.Severity != SeverityError || f.Message != tc.message || f.Since == nil || !f.Since.Equal(since) {
				t.Fatalf("finding %+v", f)
			}
			if !reflect.DeepEqual(*ev, tc.want) {
				t.Fatalf("evidence %+v\nwant %+v", *ev, tc.want)
			}
			// §14 row 015: the linked policy is the evidence's only dependency.
			var deps []objects.Ref
			if tc.want.Policy != nil {
				deps = []objects.Ref{*tc.want.Policy}
			}
			if !reflect.DeepEqual(f.Evidence, deps) {
				t.Fatalf("evidence deps %v, want %v", f.Evidence, deps)
			}
		})
	}
}

// Design §7.4 and R77: the #14047 hint appears once a rule has been replaced
// for more than 10 minutes, counted from the finding's since; the engine
// recomputes at that moment.
func TestRuleReplacedHint(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason string
		age    time.Duration
		hint   bool
	}{
		{"replaced 9:59", "RouteRuleReplaced", 9*time.Minute + 59*time.Second, false},
		{"replaced 10:01", "RouteRuleReplaced", 10*time.Minute + time.Second, true},
		{"dropped 10:01", "RouteRuleDropped", 10*time.Minute + time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			since := testutil.Now.Add(-tc.age)
			w := healthyWorld()
			w.objs[2] = ruleRoute(ruleCond("Accepted", tc.reason, strings.TrimPrefix(tc.reason, "RouteRule")+" Rule (0): x", since))
			in := w.input(t)
			in.Catalog = policyCatalog()
			r := Evaluate(in, Upstream(), Runtime{})
			f, _ := onlyRule(t, r)
			if got := strings.Contains(f.Detail, RuleReplacedHint); got != tc.hint {
				t.Fatalf("hint %v, want %v: %q", got, tc.hint, f.Detail)
			}
			wake := since.Add(RuleReplacedHintAge + time.Nanosecond)
			if !tc.hint && tc.reason == "RouteRuleReplaced" && (r.Next.IsZero() || r.Next.After(wake)) {
				t.Fatalf("next recompute %v, want by %v", r.Next, wake)
			}
		})
	}
}

// Design §5.1: the key is the parentRef as written and the match index, so two
// parents each get their own 015 and ids stay stable across recomputes.
func TestRuleReplacedPerParent(t *testing.T) {
	gw, other := testutil.GwRef("infra", "edge"), testutil.GwRef("infra", "other")
	cond := ruleCond("Accepted", "RouteRuleReplaced", "Replaced Rule (1): x", testutil.Now.Add(-time.Minute))
	resolved := testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")
	w := healthyWorld()
	w.objs = append(w.objs, testutil.Gateway("infra", "other", "kgateway", nil))
	w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw, other},
		testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{cond, resolved}},
		testutil.RouteParent{Ref: other, Controller: testutil.Controller, Conds: []metav1.Condition{cond, resolved}})
	in := w.input(t)
	r1, r2 := Evaluate(in, Upstream(), Runtime{}), Evaluate(in, Upstream(), Runtime{})
	a, b := findingsFor(r1, CodeRuleReplaced, "shop/cart"), findingsFor(r2, CodeRuleReplaced, "shop/cart")
	if len(a) != 2 || a[0].ID == a[1].ID || len(b) != 2 || a[0].ID != b[0].ID || a[1].ID != b[1].ID {
		t.Fatalf("ids %+v then %+v", a, b)
	}
}
