package computed_test

import (
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/rulesets"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

const (
	v23 = "v2.3.11"
	v24 = "v2.4.7"
)

// Gen is every fixture object's generation; status at gen is current.
const gen = 3

func cGW(ns, name string, ls ...objects.Listener) computed.Resource {
	return computed.Resource{Ref: objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: ns, Name: name}, Generation: gen, Listeners: ls}
}

func cLis(name, proto string, port int32) objects.Listener {
	return objects.Listener{Name: name, Port: port, Protocol: proto}
}

// lisStatus adds a listener status entry with one Accepted condition at obsGen.
func lisStatus(r *computed.Resource, name string, attached int32, obsGen int64) {
	r.ListenerStatus = append(r.ListenerStatus, objects.ListenerStatus{Name: name, AttachedRoutes: attached,
		Conditions: []metav1.Condition{{Type: "Accepted", Status: "True", ObservedGeneration: obsGen}}})
}

// wref is a written parentRef naming a Gateway, with the namespace omitted.
func wref(name, section string) objects.WrittenRef {
	return objects.WrittenRef{Name: name, SectionName: section}
}

// resolve applies the parentRef defaults of rules §3.2 to w.
func resolve(w objects.WrittenRef, ns string) objects.ParentRef {
	p := objects.ParentRef{Group: w.Group, Kind: w.Kind, Namespace: w.Namespace, Name: w.Name, SectionName: w.SectionName, Port: w.Port}
	if w.Group == "" && w.Kind == "" {
		p.Group = objects.GatewayAPIGroup
	}
	if p.Kind == "" {
		p.Kind = "Gateway"
	}
	if p.Namespace == "" {
		p.Namespace = ns
	}
	return p
}

func cRoute(ns, name string, ws ...objects.WrittenRef) computed.Route {
	rt := computed.Route{Ref: objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: ns, Name: name}, Generation: gen, Written: ws}
	for _, w := range ws {
		rt.ParentRefs = append(rt.ParentRefs, resolve(w, ns))
	}
	return rt
}

// status adds this controller's entry for w with an Accepted condition; reason "" means True.
func status(rt *computed.Route, w objects.WrittenRef, reason string, obsGen int64) {
	c := metav1.Condition{Type: "Accepted", Status: "True", Reason: "Accepted", ObservedGeneration: obsGen}
	if reason != "" {
		c.Status, c.Reason = "False", reason
	}
	rt.Parents = append(rt.Parents, objects.RouteParentStatus{ParentRef: resolve(w, rt.Ref.Namespace), Written: w,
		ControllerName: testutil.Controller, Conditions: []metav1.Condition{c}})
}

func input(rs []computed.Resource, routes ...computed.Route) computed.AttachmentInput {
	return computed.AttachmentInput{Resources: rs, Routes: routes, NamespacesOK: true,
		NamespaceLabels: map[string]map[string]string{}, Experimental: install.Setting{Value: "true", Known: true},
		ControllerNames: []string{testutil.Controller}}
}

func check(version string, in computed.AttachmentInput) []computed.CheckResult {
	sel := rulesets.Select(version)
	return computed.CheckAttachment("kgw/kgateway", sel, in, sel.Rules.Attachment(in))
}

// countCheck finds the listener-count check of (r, listener).
func countCheck(t *testing.T, cs []computed.CheckResult, r computed.Resource, listener string) computed.CheckResult {
	t.Helper()
	for _, c := range cs {
		if c.ID.Kind == computed.KindListenerCount && c.On == r.Ref && c.Listener == listener {
			return c
		}
	}
	t.Fatalf("no listener-count check for %v %s in %+v", r.Ref, listener, cs)
	return computed.CheckResult{}
}

// routeChecks returns the route-accepted checks on rt.
func routeChecks(cs []computed.CheckResult, rt computed.Route) []computed.CheckResult {
	var out []computed.CheckResult
	for _, c := range cs {
		if c.ID.Kind == computed.KindRouteAccepted && c.On == rt.Ref {
			out = append(out, c)
		}
	}
	return out
}

func oneRouteCheck(t *testing.T, cs []computed.CheckResult, rt computed.Route) computed.CheckResult {
	t.Helper()
	got := routeChecks(cs, rt)
	if len(got) != 1 {
		t.Fatalf("want one route-accepted check on %v, got %+v", rt.Ref, got)
	}
	return got[0]
}

func TestListenerCountAgreesAndDisagrees(t *testing.T) {
	for _, v := range []string{v23, v24} {
		t.Run(v, func(t *testing.T) {
			g := cGW("infra", "edge", cLis("a", "HTTP", 80))
			r1, r2 := cRoute("infra", "r1", wref("edge", "")), cRoute("infra", "r2", wref("edge", ""))
			agree := g
			lisStatus(&agree, "a", 2, gen)
			c := countCheck(t, check(v, input([]computed.Resource{agree}, r1, r2)), g, "a")
			if c.Class != computed.ClassCompatible || !slices.Equal(c.Coverage, []computed.Coverage{computed.CoverageCount}) || c.Candidate() {
				t.Errorf("agree: %+v", c)
			}
			off := g
			lisStatus(&off, "a", 1, gen)
			c = countCheck(t, check(v, input([]computed.Resource{off}, r1, r2)), g, "a")
			if c.Class != computed.ClassContradicting || !c.Candidate() || c.Expected != "2" || c.Observed != "1" || c.On != g.Ref {
				t.Errorf("off by one: %+v", c)
			}
			wantID := computed.Identity{Install: "kgw/kgateway", RuleSet: rulesets.Select(v).Rules.Version(), Kind: computed.KindListenerCount,
				Subject: computed.Subject(objects.GatewayAPIGroup, "Gateway", "infra", "edge", "a")}
			if c.ID != wantID {
				t.Errorf("identity %+v, want %+v", c.ID, wantID)
			}
			if !slices.Contains(c.Deps, r1.Ref) || !slices.Contains(c.Deps, g.Ref) {
				t.Errorf("deps %v", c.Deps)
			}
		})
	}
}

func TestListenerCountStaleIsPending(t *testing.T) {
	g := cGW("infra", "edge", cLis("a", "HTTP", 80))
	lisStatus(&g, "a", 0, gen-1)
	c := countCheck(t, check(v24, input([]computed.Resource{g}, cRoute("infra", "r1", wref("edge", "")))), g, "a")
	if c.Class != computed.ClassStale || c.Candidate() {
		t.Errorf("stale: %+v", c)
	}
	// No status entry for the listener: stale with "none" (Phase 1's KGWUI-003).
	g.ListenerStatus = nil
	c = countCheck(t, check(v24, input([]computed.Resource{g})), g, "a")
	if c.Class != computed.ClassStale || c.Observed != "none" || c.Candidate() {
		t.Errorf("missing: %+v", c)
	}
}

func TestRouteAcceptedContradicts(t *testing.T) {
	for _, v := range []string{v23, v24} {
		t.Run(v, func(t *testing.T) {
			g := cGW("infra", "edge", cLis("a", "HTTP", 80))
			w := wref("edge", "")
			rt := cRoute("infra", "r", w)
			status(&rt, w, "NoMatchingParent", gen)
			c := oneRouteCheck(t, check(v, input([]computed.Resource{g}, rt)), rt)
			if c.Class != computed.ClassContradicting || !c.Candidate() || c.On != rt.Ref ||
				c.Expected != "accepted" || c.Observed != "False/NoMatchingParent" || c.Key != computed.KeyOf(w) {
				t.Errorf("contradicting: %+v", c)
			}
			// A computed rejection against True contradicts, as does a different attachment reason.
			rej := cRoute("infra", "rej", wref("edge", "b"))
			status(&rej, rej.Written[0], "", gen)
			if c := oneRouteCheck(t, check(v, input([]computed.Resource{g}, rej)), rej); c.Class != computed.ClassContradicting || c.Expected != "NoMatchingParent" {
				t.Errorf("rejected vs True: %+v", c)
			}
			rej.Parents = nil
			status(&rej, rej.Written[0], "NotAllowedByListeners", gen)
			if c := oneRouteCheck(t, check(v, input([]computed.Resource{g}, rej)), rej); c.Class != computed.ClassContradicting {
				t.Errorf("other attachment reason: %+v", c)
			}
			rej.Parents = nil
			status(&rej, rej.Written[0], "NoMatchingParent", gen)
			if c := oneRouteCheck(t, check(v, input([]computed.Resource{g}, rej)), rej); c.Class != computed.ClassCompatible ||
				!slices.Equal(c.Coverage, []computed.Coverage{computed.CoverageStatus}) {
				t.Errorf("same reason: %+v", c)
			}
		})
	}
}

func TestRouteAcceptedStaleAndMissing(t *testing.T) {
	g := cGW("infra", "edge", cLis("a", "HTTP", 80))
	w := wref("edge", "")
	rt := cRoute("infra", "r", w)
	status(&rt, w, "NoMatchingParent", gen-1)
	if c := oneRouteCheck(t, check(v24, input([]computed.Resource{g}, rt)), rt); c.Class != computed.ClassStale || c.Candidate() {
		t.Errorf("stale: %+v", c)
	}
	// Another controller's entry is not ours: missing, stale with "none".
	rt.Parents = nil
	status(&rt, w, "NoMatchingParent", gen)
	rt.Parents[0].ControllerName = "example.com/other"
	if c := oneRouteCheck(t, check(v24, input([]computed.Resource{g}, rt)), rt); c.Class != computed.ClassStale || c.Observed != "none" || c.Candidate() {
		t.Errorf("missing: %+v", c)
	}
}

func TestRouteAcceptedLaterStagesCompatible(t *testing.T) {
	cases := []struct {
		version string
		conds   []metav1.Condition
	}{
		{v23, []metav1.Condition{{Type: "Accepted", Status: "False", Reason: "RouteRuleReplaced", ObservedGeneration: gen}}},
		{v23, []metav1.Condition{{Type: "Accepted", Status: "False", Reason: "Conflicted", ObservedGeneration: gen}}},
		{v24, []metav1.Condition{{Type: "Accepted", Status: "True", Reason: "Accepted", ObservedGeneration: gen},
			{Type: "kgateway.dev/Programmed", Status: "False", Reason: "RouteRuleReplaced", ObservedGeneration: gen}}},
		{v24, []metav1.Condition{{Type: "Accepted", Status: "True", Reason: "Accepted", ObservedGeneration: gen},
			{Type: "ResolvedRefs", Status: "False", Reason: "BackendNotFound", ObservedGeneration: gen}}},
	}
	for _, tc := range cases {
		g := cGW("infra", "edge", cLis("a", "HTTP", 80))
		w := wref("edge", "")
		rt := cRoute("infra", "r", w)
		rt.Parents = []objects.RouteParentStatus{{ParentRef: rt.ParentRefs[0], Written: w, ControllerName: testutil.Controller, Conditions: tc.conds}}
		c := oneRouteCheck(t, check(tc.version, input([]computed.Resource{g}, rt)), rt)
		if c.Class != computed.ClassCompatible || !slices.Equal(c.Coverage, []computed.Coverage{computed.CoverageStatus}) || c.Candidate() {
			t.Errorf("%s %v: %+v", tc.version, tc.conds, c)
		}
	}
}

func TestRefsDifferingByPortShareOneCheck(t *testing.T) {
	g := cGW("infra", "edge", cLis("a", "HTTP", 80))
	w1, w2 := wref("edge", ""), wref("edge", "")
	w1.Port, w1.PortSet = 81, true
	w2.Port, w2.PortSet = 82, true
	rt := cRoute("infra", "r", w1, w2)
	// The controller writes one entry per spec ref with the shared key's content.
	status(&rt, w1, "NoMatchingParent", gen)
	status(&rt, w2, "NoMatchingParent", gen)
	c := oneRouteCheck(t, check(v24, input([]computed.Resource{g}, rt)), rt)
	if c.Class != computed.ClassCompatible || c.Expected != "NoMatchingParent" || c.Key != computed.KeyOf(wref("edge", "")) {
		t.Errorf("shared key: %+v", c)
	}
	rt.Parents = nil
	status(&rt, w1, "", gen)
	status(&rt, w2, "", gen)
	if c := oneRouteCheck(t, check(v24, input([]computed.Resource{g}, rt)), rt); c.Class != computed.ClassContradicting {
		t.Errorf("both rejected, observed True: %+v", c)
	}
}

func TestKeyOfDropsPortKeepsWrittenNamespace(t *testing.T) {
	w := objects.WrittenRef{Group: "g", Kind: "k", Namespace: "", Name: "n", SectionName: "s", Port: 80, PortSet: true}
	if got := computed.KeyOf(w); got != (computed.ReportKey{Group: "g", Kind: "k", Name: "n", SectionName: "s"}) {
		t.Errorf("KeyOf %+v", got)
	}
	w.Namespace = "ns"
	if computed.KeyOf(w) == computed.KeyOf(objects.WrittenRef{Group: "g", Kind: "k", Name: "n", SectionName: "s"}) {
		t.Error("an explicit namespace must give another key")
	}
}

func TestOmittedVsExplicitNamespaceTwoChecks(t *testing.T) {
	g := cGW("infra", "edge", cLis("a", "HTTP", 80))
	w1, w2 := wref("edge", ""), wref("edge", "")
	w2.Namespace = "infra"
	rt := cRoute("infra", "r", w1, w2)
	status(&rt, w1, "", gen)
	status(&rt, w2, "", gen)
	got := routeChecks(check(v24, input([]computed.Resource{g}, rt)), rt)
	if len(got) != 2 || got[0].Key == got[1].Key || got[0].ID == got[1].ID {
		t.Fatalf("want two checks, got %+v", got)
	}
	for _, c := range got {
		if c.Class != computed.ClassCompatible {
			t.Errorf("%+v", c)
		}
	}
}

// inconclusive asserts rules §8.1: checked, not-verified, the note, never a candidate.
func inconclusive(t *testing.T, c computed.CheckResult) {
	t.Helper()
	if c.Class != computed.ClassInconclusive || c.Candidate() || !slices.Equal(c.Coverage, []computed.Coverage{computed.CoverageNone}) ||
		!strings.Contains(c.Note, "The controller's status cannot confirm this") {
		t.Errorf("inconclusive: %+v", c)
	}
}

func TestRejectionVsLaterStageReasonInconclusive(t *testing.T) {
	g := cGW("infra", "edge", cLis("a", "HTTP", 80))
	rt := cRoute("infra", "r", wref("edge", "b"))
	status(&rt, rt.Written[0], "RouteRuleReplaced", gen)
	inconclusive(t, oneRouteCheck(t, check(v23, input([]computed.Resource{g}, rt)), rt))
}

func TestSharedKeyDifferentOutcomesInconclusive(t *testing.T) {
	g := cGW("infra", "edge", cLis("a", "HTTP", 80))
	w1, w2 := wref("edge", ""), wref("edge", "")
	w2.Port, w2.PortSet = 81, true
	rt := cRoute("infra", "r", w1, w2)
	status(&rt, w1, "NoMatchingParent", gen)
	status(&rt, w2, "NoMatchingParent", gen)
	inconclusive(t, oneRouteCheck(t, check(v24, input([]computed.Resource{g}, rt)), rt))
}

func TestUnknownIsNeverACandidate(t *testing.T) {
	sel := cLis("sel", "HTTP", 80)
	sel.AllowedRoutes.Namespaces = objects.RouteNamespaces{From: "Selector", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "web"}}}
	g := cGW("infra", "edge", sel)
	lisStatus(&g, "sel", 7, gen)
	rt := cRoute("web", "r", objects.WrittenRef{Namespace: "infra", Name: "edge"})
	status(&rt, rt.Written[0], "NotAllowedByListeners", gen)
	in := input([]computed.Resource{g}, rt)
	in.NamespacesOK = false
	cs := check(v24, in)
	for _, c := range []computed.CheckResult{countCheck(t, cs, g, "sel"), oneRouteCheck(t, cs, rt)} {
		if c.Class != computed.ClassUnknown || c.Candidate() {
			t.Errorf("unknown: %+v", c)
		}
	}
}

func TestNoteCarriesSelection(t *testing.T) {
	g := cGW("infra", "edge", cLis("a", "HTTP", 80))
	lisStatus(&g, "a", 1, gen)
	w1, w2 := wref("edge", ""), wref("edge", "")
	w2.Port, w2.PortSet = 81, true
	ok, mixed := cRoute("infra", "ok", w1), cRoute("infra", "mixed", w1, w2)
	status(&ok, w1, "", gen)
	sel := rulesets.Select("")
	in := input([]computed.Resource{g}, ok, mixed)
	cs := computed.CheckAttachment("kgw/kgateway", sel, in, sel.Rules.Attachment(in))
	if len(cs) != 3 {
		t.Fatalf("want 3 checks, got %d", len(cs))
	}
	for _, c := range cs {
		if !strings.Contains(c.Note, "Controller version unknown; using the 2.4 rules") {
			t.Errorf("note %q on %+v", c.Note, c.ID)
		}
	}
	if c := countCheck(t, cs, g, "a"); c.Note != "Controller version unknown; using the 2.4 rules" {
		t.Errorf("note %q", c.Note)
	}
}

func TestSubjectIsNULJoined(t *testing.T) {
	if computed.Subject("a", "", "b") != "a\x00\x00b" {
		t.Error("subject")
	}
}

func TestCandidate(t *testing.T) {
	for cl, want := range map[computed.Class]bool{
		computed.ClassContradicting: true, computed.ClassMissing: true, computed.ClassStale: false, computed.ClassUnexpected: false,
		computed.ClassCompatible: false, computed.ClassNotObservable: false, computed.ClassInconclusive: false, computed.ClassUnknown: false,
	} {
		if got := (computed.CheckResult{Class: cl}).Candidate(); got != want {
			t.Errorf("%s: %v", cl, got)
		}
	}
	if !(computed.CheckResult{Class: computed.ClassUnexpected, Verified: true}).Candidate() {
		t.Error("verified unexpected")
	}
}

// The input's Deps slices are shared; the check must not reorder or extend them.
func TestCheckLeavesResultUnchanged(t *testing.T) {
	g := cGW("infra", "edge", cLis("a", "HTTP", 80), cLis("b", "HTTP", 81))
	lisStatus(&g, "a", 1, gen)
	w1, w2 := wref("edge", "a"), wref("edge", "b")
	rt := cRoute("infra", "r", w1, w2)
	in := input([]computed.Resource{g}, rt)
	sel := rulesets.Select(v24)
	res := sel.Rules.Attachment(in)
	want := sel.Rules.Attachment(in)
	computed.CheckAttachment("i", sel, in, res)
	for i := range res.Outcomes {
		if !slices.Equal(res.Outcomes[i].Deps, want.Outcomes[i].Deps) {
			t.Errorf("outcome deps changed")
		}
	}
	for i := range res.Counts {
		if !slices.Equal(res.Counts[i].Deps, want.Counts[i].Deps) {
			t.Errorf("count deps changed")
		}
	}
}

// largeInput builds the Large tier's view A input, with status, from the scale world.
func largeInput(b *testing.B) computed.AttachmentInput {
	in := computed.AttachmentInput{NamespaceLabels: map[string]map[string]string{}, NamespacesOK: true,
		Experimental: install.Setting{Value: "true", Known: true}, ControllerNames: []string{testutil.Controller}}
	for _, ns := range testutil.ScaleNamespaces(scale.Large.Namespaces) {
		in.NamespaceLabels[ns.Name] = ns.Labels
	}
	for _, u := range scale.World(scale.Large, 1) {
		ref := objects.RefOf(u)
		switch u.GetKind() {
		case "Gateway":
			v := objects.DecodeGateway(u)
			in.Resources = append(in.Resources, computed.Resource{Ref: ref, Generation: u.GetGeneration(), Listeners: v.Listeners, ListenerStatus: v.ListenerStatus})
		case "ListenerSet":
			v := objects.DecodeListenerSet(u)
			in.Resources = append(in.Resources, computed.Resource{Ref: ref, Generation: u.GetGeneration(), Listeners: v.Listeners, ListenerStatus: v.ListenerStatus})
		case "HTTPRoute", "GRPCRoute", "TCPRoute", "TLSRoute":
			v := objects.DecodeRoute(u)
			in.Routes = append(in.Routes, computed.Route{Ref: ref, Generation: u.GetGeneration(), Hostnames: v.Hostnames,
				ParentRefs: v.ParentRefs, Written: objects.WrittenParentRefs(u), Parents: v.Parents,
				ExperimentalOnly: u.GetKind() != "HTTPRoute" && u.GetKind() != "GRPCRoute"})
		}
	}
	if len(in.Resources) == 0 || len(in.Routes) == 0 {
		b.Fatal("empty scale world")
	}
	return in
}

func BenchmarkCheckAttachmentLarge(b *testing.B) {
	in := largeInput(b)
	sel := rulesets.Select(v24)
	res := sel.Rules.Attachment(in)
	b.ReportAllocs()
	for b.Loop() {
		computed.CheckAttachment("kgw/kgateway", sel, in, res)
	}
}
