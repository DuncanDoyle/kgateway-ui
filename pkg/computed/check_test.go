package computed_test

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

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
			// A count is its listener's fact, so its finding quotes no other object (design §7.4 rule 5).
			if len(c.Deps) != 0 {
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

// Views B and C cross-checks (rules §8.1–§8.5, §11.4).

// both are the two rule sets' controller versions.
var both = []string{v23, v24}

// gwRef is the Gateway every delegation root is accepted on.
var gwRef = objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "gw"}

// Written parentRefs of status entries: a root's Gateway, and the ref a child
// reports under for parent infra/<name>, as kgateway writes it.
const gwEntry = "{name: gw}"

func synth(ns, name string) string {
	return "{group: gateway.networking.k8s.io, kind: HTTPRoute, namespace: " + ns + ", name: " + name + "}"
}

// hr decodes one HTTPRoute written as YAML (metadata and spec); its generation is gen.
func hr(t testing.TB, doc string) *unstructured.Unstructured {
	t.Helper()
	j, err := yaml.YAMLToJSON([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	u := &unstructured.Unstructured{}
	if err := u.UnmarshalJSON(append([]byte(`{"apiVersion":"gateway.networking.k8s.io/v1","kind":"HTTPRoute",`), j[1:]...)); err != nil {
		t.Fatal(err)
	}
	u.SetGeneration(gen)
	return u
}

// root is a top-level route infra/<name> on gwRef with one rule per match
// list, each delegating to refs; its own entry is current.
func root(t testing.TB, name, rules string) *unstructured.Unstructured {
	u := hr(t, "metadata: {name: "+name+", namespace: infra}\nspec:\n  parentRefs: ["+gwEntry+"]\n  rules: "+rules)
	entry(t, u, gwEntry, gen, "Accepted=True", "ResolvedRefs=True")
	return u
}

// to is a delegation backendRef to route ns/name.
func to(ns, name string) string {
	return "{group: gateway.networking.k8s.io, kind: HTTPRoute, namespace: " + ns + ", name: " + name + "}"
}

// rule is one rule with the given path prefixes, delegating to refs.
func rule(paths []string, refs ...string) string {
	ms := make([]string, len(paths))
	for i, p := range paths {
		ms[i] = "{path: {type: PathPrefix, value: " + p + "}}"
	}
	return "{matches: [" + strings.Join(ms, ", ") + "], backendRefs: [" + strings.Join(refs, ", ") + "]}"
}

// child is route ns/name with one rule of the given prefixes and extra spec lines.
func child(t testing.TB, ns, name string, paths []string, extra ...string) *unstructured.Unstructured {
	spec := "  rules: [" + rule(paths) + "]"
	for _, e := range extra {
		spec += "\n  " + e
	}
	return hr(t, "metadata: {name: "+name+", namespace: "+ns+"}\nspec:\n"+spec)
}

// entry adds this controller's status entry under parentRef ref (a YAML flow
// map) with conds, each "Type=Status[/Reason]", observed at obsGen.
func entry(t testing.TB, u *unstructured.Unstructured, ref string, obsGen int64, conds ...string) {
	t.Helper()
	j, err := yaml.YAMLToJSON([]byte(ref))
	if err != nil {
		t.Fatal(err)
	}
	pr := map[string]any{}
	if err := json.Unmarshal(j, &pr); err != nil {
		t.Fatal(err)
	}
	var cs []any
	for _, c := range conds {
		typ, rest, _ := strings.Cut(c, "=")
		st, reason, _ := strings.Cut(rest, "/")
		cs = append(cs, map[string]any{"type": typ, "status": st, "reason": cmp.Or(reason, typ), "observedGeneration": obsGen,
			"lastTransitionTime": "2026-10-26T00:00:00Z", "message": ""})
	}
	parents, _, _ := unstructured.NestedSlice(u.Object, "status", "parents")
	parents = append(parents, map[string]any{"parentRef": pr, "controllerName": testutil.Controller, "conditions": cs})
	if err := unstructured.SetNestedSlice(u.Object, parents, "status", "parents"); err != nil {
		t.Fatal(err)
	}
}

// dworld is views B and C's input: every route owned, the roots accepted on
// gwRef, and unknown the routes whose root eligibility is unknown.
type dworld struct {
	routes  []*unstructured.Unstructured
	roots   []string
	unknown []string
	mod     func(*computed.DelegationInput)
}

// input builds the world's DelegationInput as the engine does.
func (w dworld) input() computed.DelegationInput {
	in := computed.DelegationInput{Wildcard: install.Setting{Value: install.WildcardAll, Known: true},
		ControllerNames: []string{testutil.Controller}, Bounds: computed.DefaultBounds, Unwatched: map[objects.Ref]bool{}}
	for _, u := range w.routes {
		ref, name := objects.RefOf(u), u.GetNamespace()+"/"+u.GetName()
		v := objects.DecodeHTTPRoute(u)
		top := slices.Contains(w.roots, name) || slices.Contains(w.unknown, name)
		in.Routes = append(in.Routes, computed.DelegRoute{Ref: ref, Generation: u.GetGeneration(), View: v, Owned: true, TopLevel: top})
		switch {
		case slices.Contains(w.roots, name):
			in.Roots = append(in.Roots, computed.Root{Route: ref, Deps: []objects.Ref{gwRef, ref}, Keys: []computed.ReportKey{computed.KeyOf(v.WrittenRefs[0])}})
		case slices.Contains(w.unknown, name):
			in.Roots = append(in.Roots, computed.Root{Route: ref, Unknown: true, Deps: []objects.Ref{ref}})
		}
	}
	slices.SortFunc(in.Routes, func(x, y computed.DelegRoute) int { return x.Ref.Compare(y.Ref) })
	slices.SortFunc(in.Roots, func(x, y computed.Root) int { return x.Route.Compare(y.Route) })
	if w.mod != nil {
		w.mod(&in)
	}
	return in
}

// run computes views B and C with version's rule set and cross-checks them.
func (w dworld) run(version string) (*computed.DelegationResult, []computed.CheckResult) {
	in := w.input()
	sel := rulesets.Select(version)
	res := sel.Rules.Delegation(in)
	return &res, computed.CheckDelegation("kgw/kgateway", sel, in, &res)
}

func hRef(ns, name string) objects.Ref {
	return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: ns, Name: name}
}

// childEntries returns the child-entry checks on child ns/name under parent pns/pname.
func childEntries(cs []computed.CheckResult, ns, name, pns, pname string) []computed.CheckResult {
	var out []computed.CheckResult
	p := hRef(pns, pname)
	for _, c := range cs {
		parts := strings.Split(c.ID.Subject, "\x00")
		if c.ID.Kind == computed.KindChildEntry && c.On == hRef(ns, name) && len(parts) == 13 &&
			(objects.Ref{Group: parts[4], Kind: parts[5], Namespace: parts[6], Name: parts[7]}) == p {
			out = append(out, c)
		}
	}
	return out
}

func oneChildEntry(t *testing.T, cs []computed.CheckResult, ns, name, pns, pname string) computed.CheckResult {
	t.Helper()
	got := childEntries(cs, ns, name, pns, pname)
	if len(got) != 1 {
		t.Fatalf("want one child-entry check on %s/%s under %s/%s, got %+v", ns, name, pns, pname, got)
	}
	return got[0]
}

// parentChecks returns the parent-resolvedrefs checks on ns/name.
func parentChecks(cs []computed.CheckResult, ns, name string) []computed.CheckResult {
	var out []computed.CheckResult
	for _, c := range cs {
		if c.ID.Kind == computed.KindParentResolvedRefs && c.On == hRef(ns, name) {
			out = append(out, c)
		}
	}
	return out
}

func oneParentCheck(t *testing.T, cs []computed.CheckResult, ns, name string) computed.CheckResult {
	t.Helper()
	got := parentChecks(cs, ns, name)
	if len(got) != 1 {
		t.Fatalf("want one parent-resolvedrefs check on %s/%s, got %+v", ns, name, got)
	}
	return got[0]
}

// is fails unless c has class cl, and coverage cov when cov is set.
func is(t *testing.T, c computed.CheckResult, cl computed.Class, cov ...computed.Coverage) {
	t.Helper()
	if c.Class != cl || len(cov) > 0 && !slices.Equal(c.Coverage, cov) {
		t.Fatalf("got %s %v (expected %q, observed %q, note %q), want %s %v", c.Class, c.Coverage, c.Expected, c.Observed, c.Note, cl, cov)
	}
}

// basic is root infra/r delegating under prefixes to apps/c, which has the
// match /api/c and extra spec lines. c reports under synth(infra, r).
func basic(t *testing.T, prefixes []string, extra ...string) (r, c *unstructured.Unstructured) {
	r = root(t, "r", "["+rule(prefixes, to("apps", "c"))+"]")
	c = child(t, "apps", "c", []string{"/api/c"}, extra...)
	return r, c
}

func TestChildEntryAggregation(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			// One rule, two matches: /api attaches c, /v2 drops it. The row is partial; one entry is expected.
			r, c := basic(t, []string{"/api", "/v2"})
			entry(t, c, synth("infra", "r"), gen, "Accepted=True")
			res, cs := dworld{routes: []*unstructured.Unstructured{r, c}, roots: []string{"infra/r"}}.run(v)
			if res.Rows[0].Outcome != computed.OutcomePartial {
				t.Fatalf("row %s", res.Rows[0].Outcome)
			}
			ce := oneChildEntry(t, cs, "apps", "c", "infra", "r")
			is(t, ce, computed.ClassCompatible, computed.CoverageStatus)
			if ce.Expected != "entry" || ce.Observed != "True" {
				t.Fatalf("expected %q observed %q", ce.Expected, ce.Observed)
			}

			// Two rules: rule 0 (/x) drops c and rule 1 (/api) attaches it: one check, one entry.
			r = root(t, "r", "["+rule([]string{"/x"}, to("apps", "c"))+", "+rule([]string{"/api"}, to("apps", "c"))+"]")
			_, cs = dworld{routes: []*unstructured.Unstructured{r, c}, roots: []string{"infra/r"}}.run(v)
			is(t, oneChildEntry(t, cs, "apps", "c", "infra", "r"), computed.ClassCompatible, computed.CoverageStatus)

			// Diamond: r → b1, b2 → c. Two checks, one per parent key.
			r = root(t, "r", "["+rule([]string{"/api"}, to("apps", "b1"), to("apps", "b2"))+"]")
			b1 := hr(t, "metadata: {name: b1, namespace: apps}\nspec:\n  rules: ["+rule([]string{"/api/b"}, to("apps", "c"))+"]")
			b2 := hr(t, "metadata: {name: b2, namespace: apps}\nspec:\n  rules: ["+rule([]string{"/api/b"}, to("apps", "c"))+"]")
			c = child(t, "apps", "c", []string{"/api/b/c"})
			entry(t, c, synth("apps", "b1"), gen, "Accepted=True")
			_, cs = dworld{routes: []*unstructured.Unstructured{r, b1, b2, c}, roots: []string{"infra/r"}}.run(v)
			for _, p := range []string{"b1", "b2"} {
				if n := len(childEntries(cs, "apps", "c", "apps", p)); n != 1 {
					t.Fatalf("%d checks under %s", n, p)
				}
			}
			is(t, oneChildEntry(t, cs, "apps", "c", "apps", "b1"), computed.ClassCompatible)
			is(t, oneChildEntry(t, cs, "apps", "c", "apps", "b2"), computed.ClassMissing)
		})
	}
}

func TestAncestorPrunedExpectsNoEntry(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			// R /api → P {/api, /other} → C /other/item: C is dropped under the only context.
			r := root(t, "r", "["+rule([]string{"/api"}, to("apps", "p"))+"]")
			p := hr(t, "metadata: {name: p, namespace: apps}\nspec:\n  rules: ["+rule([]string{"/api", "/other"}, to("apps", "c"))+"]")
			entry(t, p, synth("infra", "r"), gen, "Accepted=True")
			c := child(t, "apps", "c", []string{"/other/item"})
			w := dworld{routes: []*unstructured.Unstructured{r, p, c}, roots: []string{"infra/r"}}
			_, cs := w.run(v)
			ce := oneChildEntry(t, cs, "apps", "c", "apps", "p")
			is(t, ce, computed.ClassCompatible, computed.CoverageStatus)
			if ce.Expected != "none" || ce.Observed != "none" {
				t.Fatalf("expected %q observed %q", ce.Expected, ce.Observed)
			}

			// A current entry left after the parent edit: unexpected and verified.
			entry(t, c, synth("apps", "p"), gen, "Accepted=True")
			_, cs = w.run(v)
			ce = oneChildEntry(t, cs, "apps", "c", "apps", "p")
			is(t, ce, computed.ClassUnexpected)
			if !ce.Verified || !ce.Candidate() {
				t.Fatalf("verified %v candidate %v", ce.Verified, ce.Candidate())
			}
		})
	}
}

func TestAttachedChildMissingBackend(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			r, c := basic(t, []string{"/api"})
			entry(t, c, synth("infra", "r"), gen, "Accepted=True", "ResolvedRefs=False/BackendNotFound")
			_, cs := dworld{routes: []*unstructured.Unstructured{r, c}, roots: []string{"infra/r"}}.run(v)
			ce := oneChildEntry(t, cs, "apps", "c", "infra", "r")
			is(t, ce, computed.ClassCompatible, computed.CoverageStatus)
			if ce.Candidate() {
				t.Fatal("candidate")
			}
		})
	}
}

func TestAttachedChildReplacedRule(t *testing.T) {
	conds := map[string][]string{v23: {"Accepted=False/RouteRuleReplaced"}, v24: {"Accepted=True", "kgateway.dev/Programmed=False/RouteRuleReplaced"}}
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			r, c := basic(t, []string{"/api"})
			entry(t, c, synth("infra", "r"), gen, conds[v]...)
			_, cs := dworld{routes: []*unstructured.Unstructured{r, c}, roots: []string{"infra/r"}}.run(v)
			is(t, oneChildEntry(t, cs, "apps", "c", "infra", "r"), computed.ClassCompatible, computed.CoverageStatus)
		})
	}
}

func TestRejectedHostnamesExpectsUnsupportedValue(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			r, c := basic(t, []string{"/api"}, "hostnames: [c.example.com]")
			entry(t, c, synth("infra", "r"), gen, "Accepted=False/UnsupportedValue")
			w := dworld{routes: []*unstructured.Unstructured{r, c}, roots: []string{"infra/r"}}
			_, cs := w.run(v)
			ce := oneChildEntry(t, cs, "apps", "c", "infra", "r")
			is(t, ce, computed.ClassCompatible, computed.CoverageStatus)
			if ce.Expected != "False/UnsupportedValue" {
				t.Fatalf("expected %q", ce.Expected)
			}

			// Accepted=True contradicts a complete rejection.
			_, c2 := basic(t, []string{"/api"}, "hostnames: [c.example.com]")
			entry(t, c2, synth("infra", "r"), gen, "Accepted=True")
			_, cs = dworld{routes: []*unstructured.Unstructured{r, c2}, roots: []string{"infra/r"}}.run(v)
			is(t, oneChildEntry(t, cs, "apps", "c", "infra", "r"), computed.ClassContradicting)

			// Incomplete (an unknown root u also selects c): the row stays rejected
			// with SomeNotComputed, an entry is still expected, but its Accepted value is uncertain.
			u := hr(t, "metadata: {name: u, namespace: infra}\nspec:\n  parentRefs: ["+gwEntry+"]\n  rules: ["+rule([]string{"/api"}, to("apps", "c"))+"]")
			res, cs := dworld{routes: []*unstructured.Unstructured{r, u, c2}, roots: []string{"infra/r"}, unknown: []string{"infra/u"}}.run(v)
			for _, rw := range res.Rows {
				if rw.Key.Parent.Name == "r" && (rw.Outcome != computed.OutcomeRejectedHostnames || !rw.SomeNotComputed) {
					t.Fatalf("row %s someNotComputed %v", rw.Outcome, rw.SomeNotComputed)
				}
			}
			is(t, oneChildEntry(t, cs, "apps", "c", "infra", "r"), computed.ClassInconclusive, computed.CoverageNone)

			// With no entry at all, the rejection still expects one: missing.
			_, c3 := basic(t, []string{"/api"}, "hostnames: [c.example.com]")
			_, cs = dworld{routes: []*unstructured.Unstructured{r, u, c3}, roots: []string{"infra/r"}, unknown: []string{"infra/u"}}.run(v)
			is(t, oneChildEntry(t, cs, "apps", "c", "infra", "r"), computed.ClassMissing)
		})
	}
}

func TestParentWithOneUnresolvedRef(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			for _, tc := range []struct {
				observed string
				class    computed.Class
			}{
				{"ResolvedRefs=False/BackendNotFound", computed.ClassCompatible},
				{"ResolvedRefs=False/RefNotPermitted", computed.ClassContradicting},
				{"ResolvedRefs=True", computed.ClassContradicting},
			} {
				// Rule 0 attaches c; rule 1's ref selects nothing.
				r := hr(t, "metadata: {name: r, namespace: infra}\nspec:\n  parentRefs: ["+gwEntry+"]\n  rules: ["+
					rule([]string{"/api"}, to("apps", "c"))+", "+rule([]string{"/b"}, to("apps", "missing"))+"]")
				entry(t, r, gwEntry, gen, "Accepted=True", tc.observed)
				c := child(t, "apps", "c", []string{"/api/c"})
				_, cs := dworld{routes: []*unstructured.Unstructured{r, c}, roots: []string{"infra/r"}}.run(v)
				pc := oneParentCheck(t, cs, "infra", "r")
				is(t, pc, tc.class)
				if pc.Expected != "False/BackendNotFound" || pc.Key != (computed.ReportKey{Name: "gw"}) {
					t.Fatalf("expected %q key %+v", pc.Expected, pc.Key)
				}
			}
		})
	}
}

func TestTruncatedParentAggregate(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			// p's rule 0 is unresolved (BackendNotFound); the contexts cap cuts its
			// rule 1, a cycle. The observed RefNotPermitted is compatible: the reason is not compared.
			r := root(t, "r", "["+rule([]string{"/api"}, to("apps", "p"))+"]")
			p := hr(t, "metadata: {name: p, namespace: apps}\nspec:\n  rules: ["+
				rule([]string{"/api/a"}, to("apps", "missing"))+", "+rule([]string{"/api/b"}, to("apps", "p"))+"]")
			entry(t, p, synth("infra", "r"), gen, "Accepted=True", "ResolvedRefs=False/RefNotPermitted")
			w := dworld{routes: []*unstructured.Unstructured{r, p}, roots: []string{"infra/r"},
				mod: func(in *computed.DelegationInput) { in.Bounds.Contexts = 2 }}
			res, cs := w.run(v)
			if !res.Complete.Incomplete(hRef("apps", "p")) {
				t.Fatal("p complete")
			}
			pc := oneParentCheck(t, cs, "apps", "p")
			is(t, pc, computed.ClassCompatible, computed.CoverageStatus)
			if pc.Expected != "False" || pc.Candidate() {
				t.Fatalf("expected %q candidate %v", pc.Expected, pc.Candidate())
			}
		})
	}
}

func TestParentMissingServiceBackend(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			svc := "{name: svc, port: 80}"
			for _, tc := range []struct {
				name  string
				rules string
				class computed.Class
			}{
				// A Service backend may be what failed: an observed False is compatible.
				{"service", "[" + rule([]string{"/api"}, to("apps", "c")) + ", " + rule([]string{"/s"}, svc) + "]", computed.ClassCompatible},
				// Only delegation refs, none failed: an observed False contradicts.
				{"delegation only", "[" + rule([]string{"/api"}, to("apps", "c")) + "]", computed.ClassContradicting},
			} {
				r := hr(t, "metadata: {name: r, namespace: infra}\nspec:\n  parentRefs: ["+gwEntry+"]\n  rules: "+tc.rules)
				entry(t, r, gwEntry, gen, "Accepted=True", "ResolvedRefs=False/BackendNotFound")
				c := child(t, "apps", "c", []string{"/api/c"})
				_, cs := dworld{routes: []*unstructured.Unstructured{r, c}, roots: []string{"infra/r"}}.run(v)
				pc := oneParentCheck(t, cs, "infra", "r")
				if pc.Class != tc.class || pc.Expected != "True" {
					t.Fatalf("%s: %s expected %q", tc.name, pc.Class, pc.Expected)
				}
			}
		})
	}
}

func TestComputedAttachedNoEntry(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			for _, pGen := range []int64{gen, gen - 1} {
				// r → p → c; c is attached under p and has no entry.
				r := root(t, "r", "["+rule([]string{"/api"}, to("apps", "p"))+"]")
				p := hr(t, "metadata: {name: p, namespace: apps}\nspec:\n  rules: ["+rule([]string{"/api/p"}, to("apps", "c"))+"]")
				entry(t, p, synth("infra", "r"), pGen, "Accepted=True")
				c := child(t, "apps", "c", []string{"/api/p/c"})
				_, cs := dworld{routes: []*unstructured.Unstructured{r, p, c}, roots: []string{"infra/r"}}.run(v)
				ce := oneChildEntry(t, cs, "apps", "c", "apps", "p")
				is(t, ce, computed.ClassMissing)
				want := []objects.Ref{hRef("apps", "c"), hRef("apps", "p"), hRef("infra", "r")}
				if !ce.Candidate() || !slices.Equal(ce.Contributors, want) || ce.ParentCurrent != (pGen == gen) {
					t.Fatalf("p at %d: candidate %v contributors %v parentCurrent %v", pGen, ce.Candidate(), ce.Contributors, ce.ParentCurrent)
				}
				if !slices.Contains(ce.Deps, gwRef) || !slices.Contains(ce.Deps, hRef("apps", "c")) {
					t.Fatalf("deps %v", ce.Deps)
				}
			}
		})
	}
}

func TestStaleChildEntryIsStale(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			// Attached, but the stale entry says UnsupportedValue; dropped, with a stale entry left.
			for _, prefix := range []string{"/api", "/other"} {
				r, c := basic(t, []string{prefix})
				entry(t, c, synth("infra", "r"), gen-1, "Accepted=False/UnsupportedValue")
				_, cs := dworld{routes: []*unstructured.Unstructured{r, c}, roots: []string{"infra/r"}}.run(v)
				ce := oneChildEntry(t, cs, "apps", "c", "infra", "r")
				is(t, ce, computed.ClassStale)
				if ce.Candidate() {
					t.Fatal("candidate")
				}
			}
		})
	}
}

func TestForbiddenEntryLeftNotVerified(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			// c's parentRefs name another parent only, so r may not select it.
			r, c := basic(t, []string{"/api"}, "parentRefs: [{group: gateway.networking.k8s.io, kind: HTTPRoute, namespace: infra, name: other}]")
			entry(t, c, synth("infra", "r"), gen, "Accepted=True")
			_, cs := dworld{routes: []*unstructured.Unstructured{r, c}, roots: []string{"infra/r"}}.run(v)
			ce := oneChildEntry(t, cs, "apps", "c", "infra", "r")
			is(t, ce, computed.ClassUnexpected)
			if ce.Verified || ce.Candidate() || !strings.Contains(ce.Note, "The controller may keep old status") {
				t.Fatalf("verified %v note %q", ce.Verified, ce.Note)
			}
		})
	}
}

func TestLeftEntryAnyCause(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			// A parent edit (r's match becomes /store) and a child edit (c's match
			// becomes /other/six) both drop c. No first-seen time is needed.
			parentEdit := func() []*unstructured.Unstructured { return pair(basic(t, []string{"/store"})) }
			childEdit := func() []*unstructured.Unstructured {
				r := root(t, "r", "["+rule([]string{"/api"}, to("apps", "c"))+"]")
				return []*unstructured.Unstructured{r, child(t, "apps", "c", []string{"/other/six"})}
			}
			for name, world := range map[string]func() []*unstructured.Unstructured{"parent edit": parentEdit, "child edit": childEdit} {
				routes := world()
				entry(t, routes[1], synth("infra", "r"), gen, "Accepted=True")
				_, cs := dworld{routes: routes, roots: []string{"infra/r"}}.run(v)
				ce := oneChildEntry(t, cs, "apps", "c", "infra", "r")
				if ce.Class != computed.ClassUnexpected || !ce.Verified || !ce.Candidate() {
					t.Fatalf("%s: %s verified %v", name, ce.Class, ce.Verified)
				}
			}
		})
	}
}

func pair(a, b *unstructured.Unstructured) []*unstructured.Unstructured {
	return []*unstructured.Unstructured{a, b}
}

func TestDeselectedEntryLeft(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			// r now selects c2, not c; c keeps a current entry under r's key.
			r := root(t, "r", "["+rule([]string{"/api"}, to("apps", "c2"))+"]")
			c := child(t, "apps", "c", []string{"/api/c"})
			entry(t, c, synth("infra", "r"), gen, "Accepted=True")
			c2 := child(t, "apps", "c2", []string{"/api/c2"})
			_, cs := dworld{routes: []*unstructured.Unstructured{r, c, c2}, roots: []string{"infra/r"}}.run(v)
			ce := oneChildEntry(t, cs, "apps", "c", "infra", "r")
			if ce.Class != computed.ClassUnexpected || !ce.Verified || !ce.Candidate() || ce.Expected != "none" {
				t.Fatalf("%s verified %v expected %q", ce.Class, ce.Verified, ce.Expected)
			}

			// An unknown root u that selects c makes it incomplete: no negative expectation.
			u := hr(t, "metadata: {name: u, namespace: infra}\nspec:\n  parentRefs: ["+gwEntry+"]\n  rules: ["+rule([]string{"/api"}, to("apps", "c"))+"]")
			_, cs = dworld{routes: []*unstructured.Unstructured{r, u, c, c2}, roots: []string{"infra/r"}, unknown: []string{"infra/u"}}.run(v)
			ce = oneChildEntry(t, cs, "apps", "c", "infra", "r")
			if ce.Class != computed.ClassUnknown || ce.Note != "Not all paths computed" {
				t.Fatalf("%s note %q", ce.Class, ce.Note)
			}
		})
	}
}

func TestScopeUnknownChecksUnknown(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			r, c := basic(t, []string{"/api"})
			entry(t, c, synth("infra", "r"), gen, "Accepted=True")
			left := child(t, "apps", "left", []string{"/api/l"})
			entry(t, left, synth("infra", "r"), gen, "Accepted=True")
			w := dworld{routes: []*unstructured.Unstructured{r, c, left}, roots: []string{"infra/r"},
				mod: func(in *computed.DelegationInput) { in.ScopeUnknown = true }}
			_, cs := w.run(v)
			n := 0
			for _, ck := range cs {
				if ck.ID.Kind != computed.KindChildEntry && ck.ID.Kind != computed.KindParentResolvedRefs {
					continue
				}
				n++
				if ck.Class != computed.ClassUnknown || ck.Unknown != computed.UnknownScope || ck.Candidate() {
					t.Fatalf("%s %s: %s %s", ck.ID.Kind, ck.On, ck.Class, ck.Unknown)
				}
			}
			if n < 2 {
				t.Fatalf("%d checks, want the attached child's and the leftover's", n)
			}
		})
	}
}

func TestNoEntryIncompleteIsUnknown(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			// §11.2 cut row: /x drops c, then the contexts cap cuts /api, which would attach it.
			r, c := basic(t, []string{"/x", "/api"})
			entry(t, c, synth("infra", "r"), gen, "Accepted=True")
			w := dworld{routes: []*unstructured.Unstructured{r, c}, roots: []string{"infra/r"},
				mod: func(in *computed.DelegationInput) { in.Bounds.Contexts = 1 }}
			res, cs := w.run(v)
			if res.Rows[0].Outcome != computed.OutcomeUnknown {
				t.Fatalf("row %s", res.Rows[0].Outcome)
			}
			ce := oneChildEntry(t, cs, "apps", "c", "infra", "r")
			if ce.Class != computed.ClassUnknown || ce.Note != "Not all paths computed" || ce.Candidate() {
				t.Fatalf("%s note %q", ce.Class, ce.Note)
			}
			// /x records a failure, so False is expected; the cut leaves the reason uncompared.
			if pc := oneParentCheck(t, cs, "infra", "r"); pc.Expected != "False" {
				t.Fatalf("parent expected %q", pc.Expected)
			}

			// An unknown wildcard setting with labelled routes elsewhere: possible
			// route other/d keeps an entry under r; its negative check is unknown.
			lr := root(t, "r", "["+rule([]string{"/api"}, "{group: delegation.kgateway.dev, kind: label, name: team, namespace: apps}")+"]")
			kid := child(t, "apps", "kid", []string{"/api/k"})
			kid.SetLabels(map[string]string{"delegation.kgateway.dev/label": "team"})
			d := child(t, "other", "d", []string{"/zzz"})
			d.SetLabels(map[string]string{"delegation.kgateway.dev/label": "team"})
			entry(t, d, synth("infra", "r"), gen, "Accepted=True")
			w = dworld{routes: []*unstructured.Unstructured{lr, kid, d}, roots: []string{"infra/r"},
				mod: func(in *computed.DelegationInput) { in.Wildcard = install.Setting{} }}
			_, cs = w.run(v)
			ce = oneChildEntry(t, cs, "other", "d", "infra", "r")
			if ce.Class != computed.ClassUnknown || ce.Note != "Not all paths computed" {
				t.Fatalf("possible child: %s note %q", ce.Class, ce.Note)
			}
			if pc := oneParentCheck(t, cs, "infra", "r"); pc.Class != computed.ClassUnknown {
				t.Fatalf("parent %s", pc.Class)
			}
		})
	}
}

func Test23ChildWithoutEntryNotVerified(t *testing.T) {
	// §11.3: on 2.3 a permitting ref that omits the namespace or sets
	// sectionName gets no entry: not observable. 2.4 writes one under the ref as written.
	for _, ref := range []string{
		"{group: gateway.networking.k8s.io, kind: HTTPRoute, name: r}",
		"{group: gateway.networking.k8s.io, kind: HTTPRoute, namespace: infra, name: r, sectionName: s}",
	} {
		for _, hostnames := range []string{"hostnames: []", "hostnames: [c.example.com]"} {
			r := root(t, "r", "["+rule([]string{"/api"}, to("infra", "c"))+"]")
			c := child(t, "infra", "c", []string{"/api/c"}, "parentRefs: ["+ref+"]", hostnames)
			w := dworld{routes: []*unstructured.Unstructured{r, c}, roots: []string{"infra/r"}}
			_, cs := w.run(v23)
			ce := oneChildEntry(t, cs, "infra", "c", "infra", "r")
			is(t, ce, computed.ClassNotObservable, computed.CoverageNone)
			_, cs = w.run(v24)
			is(t, oneChildEntry(t, cs, "infra", "c", "infra", "r"), computed.ClassMissing)
		}
	}
}

func TestUnconfirmableRow(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			// Rule 0 (/x) drops c, rule 1 (/api) attaches it: the entry rule 1
			// expects cannot confirm rule 0's drop.
			r := root(t, "r", "["+rule([]string{"/x"}, to("apps", "c"))+", "+rule([]string{"/api"}, to("apps", "c"))+"]")
			c := child(t, "apps", "c", []string{"/api/c"})
			entry(t, c, synth("infra", "r"), gen, "Accepted=True")
			res, _ := dworld{routes: []*unstructured.Unstructured{r, c}, roots: []string{"infra/r"}}.run(v)
			if len(res.Rows) != 2 || !res.Rows[0].Unconfirmable || res.Rows[1].Unconfirmable {
				t.Fatalf("rows %+v", res.Rows)
			}
			// Dropped everywhere: the absent entry confirms the drop.
			r = root(t, "r", "["+rule([]string{"/x"}, to("apps", "c"))+"]")
			res, _ = dworld{routes: []*unstructured.Unstructured{r, child(t, "apps", "c", []string{"/api/c"})}, roots: []string{"infra/r"}}.run(v)
			if res.Rows[0].Unconfirmable {
				t.Fatal("dropped row unconfirmable")
			}
		})
	}
}

func TestUncertainRootKeyMakesNoNegativeExpectation(t *testing.T) {
	for _, v := range both {
		t.Run(v, func(t *testing.T) {
			// View A is unknown for r's parentRef gw2: no context reports under it,
			// so "no failure" there is not expected, whatever the entry says.
			r := hr(t, "metadata: {name: r, namespace: infra}\nspec:\n  parentRefs: [{name: gw}, {name: gw2}]\n  rules: ["+rule([]string{"/api"}, to("apps", "c"))+"]")
			entry(t, r, gwEntry, gen, "Accepted=True", "ResolvedRefs=True")
			entry(t, r, "{name: gw2}", gen, "Accepted=True", "ResolvedRefs=False/BackendNotFound")
			c := child(t, "apps", "c", []string{"/api/c"})
			w := dworld{routes: []*unstructured.Unstructured{r, c}, roots: []string{"infra/r"},
				mod: func(in *computed.DelegationInput) { in.Roots[0].UncertainKeys = []computed.ReportKey{{Name: "gw2"}} }}
			_, cs := w.run(v)
			pcs := parentChecks(cs, "infra", "r")
			if len(pcs) != 2 {
				t.Fatalf("checks %+v", pcs)
			}
			for _, pc := range pcs {
				switch pc.Key.Name {
				case "gw":
					is(t, pc, computed.ClassCompatible, computed.CoverageStatus)
				case "gw2":
					if pc.Class != computed.ClassUnknown || pc.Note != "Not all paths computed" {
						t.Fatalf("gw2: %s note %q", pc.Class, pc.Note)
					}
				}
			}
		})
	}
}

func BenchmarkCheckDelegationLarge(b *testing.B) {
	var objs []*unstructured.Unstructured
	for _, u := range scale.World(scale.Large, 1) {
		if u.GetKind() == "HTTPRoute" {
			objs = append(objs, u)
		}
	}
	w := dworld{routes: objs}
	for _, u := range objs {
		for _, p := range objects.DecodeHTTPRoute(u).ParentRefs {
			if p.Kind != "HTTPRoute" {
				w.roots = append(w.roots, u.GetNamespace()+"/"+u.GetName())
				break
			}
		}
	}
	in := w.input()
	sel := rulesets.Select(v24)
	res := sel.Rules.Delegation(in)
	cs := computed.CheckDelegation("kgw/kgateway", sel, in, &res)
	b.ReportMetric(float64(len(cs)), "checks")
	b.ReportAllocs()
	for b.Loop() {
		computed.CheckDelegation("kgw/kgateway", sel, in, &res)
	}
}
