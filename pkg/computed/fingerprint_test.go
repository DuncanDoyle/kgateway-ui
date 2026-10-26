package computed_test

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// fpWorld is a Gateway with an All listener a and a Selector listener b, a
// ListenerSet with an All listener c, and a route on each; no Selector reads "plain".
func fpWorld() computed.AttachmentInput {
	a := cLis("a", "HTTP", 80)
	a.Hostname = "*.example.com"
	a.AllowedRoutes.Namespaces.From = "All"
	b := cLis("b", "HTTP", 81)
	b.AllowedRoutes.Namespaces = objects.RouteNamespaces{From: "Selector", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "web"}}}
	g := cGW("infra", "edge", a, b)
	lisStatus(&g, "a", 1, gen)
	lisStatus(&g, "b", 1, gen)
	c := cLis("c", "HTTP", 82)
	c.AllowedRoutes.Namespaces.From = "All"
	ls := cGW("infra", "ls", c)
	ls.Ref.Kind, ls.Parent = "ListenerSet", g.Ref
	lisStatus(&ls, "c", 1, gen)

	r1 := cRoute("infra", "r1", wref("edge", ""))
	r1.Hostnames = []string{"a.example.com"}
	status(&r1, r1.Written[0], "", gen)
	r2 := cRoute("web", "r2", objects.WrittenRef{Namespace: "infra", Name: "edge", SectionName: "b"})
	status(&r2, r2.Written[0], "", gen)
	r3 := cRoute("plain", "r3", objects.WrittenRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"})
	status(&r3, r3.Written[0], "", gen)

	in := input([]computed.Resource{g, ls}, r1, r2, r3)
	in.NamespaceLabels = map[string]map[string]string{"infra": {"team": "infra"}, "web": {"team": "web"}, "shop": {"team": "web"}, "plain": {"team": "plain"}}
	return in
}

// prints are the fingerprints of fpWorld's checks, by a readable name.
func prints(t *testing.T, in computed.AttachmentInput) map[string]uint64 {
	t.Helper()
	out := map[string]uint64{}
	for _, c := range check(v24, in) {
		name := c.On.Name + "/" + c.Listener
		if c.ID.Kind == computed.KindRouteAccepted {
			name = c.On.Name + "/" + c.Key.SectionName
		}
		if _, dup := out[name]; dup {
			t.Fatalf("duplicate check %s", name)
		}
		out[name] = c.Fingerprint
	}
	return out
}

// res returns resource i of in, for in-place edits.
func res(in *computed.AttachmentInput, i int) *computed.Resource { return &in.Resources[i] }

func TestAttachmentFingerprints(t *testing.T) {
	base := prints(t, fpWorld())
	cases := []struct {
		name    string
		mutate  func(in *computed.AttachmentInput)
		changed []string // checks whose fingerprint must change; every other must not
	}{
		// listener-count inputs (rules §9.2).
		{"listener port", func(in *computed.AttachmentInput) { res(in, 0).Listeners[0].Port = 8080 }, []string{"edge/a", "r1/", "r2/b"}},
		{"listener protocol", func(in *computed.AttachmentInput) { res(in, 0).Listeners[0].Protocol = "HTTPS" }, []string{"edge/a", "r1/", "r2/b"}},
		{"listener hostname", func(in *computed.AttachmentInput) { res(in, 0).Listeners[0].Hostname = "*.example.org" }, []string{"edge/a", "r1/", "r2/b"}},
		{"listener allowedRoutes", func(in *computed.AttachmentInput) { res(in, 0).Listeners[0].AllowedRoutes.Namespaces.From = "Same" },
			[]string{"edge/a", "r1/", "r2/b"}},
		{"route kind", func(in *computed.AttachmentInput) { in.Routes[0].Ref.Kind = "GRPCRoute" }, []string{"edge/a", "edge/b", "r1/"}},
		{"route namespace", func(in *computed.AttachmentInput) {
			in.Routes[1].Ref.Namespace = "shop"
			in.Routes[1].ParentRefs[0] = resolve(in.Routes[1].Written[0], "shop")
		}, []string{"edge/a", "edge/b", "r2/b"}},
		{"route hostnames", func(in *computed.AttachmentInput) { in.Routes[0].Hostnames = []string{"b.example.com"} }, []string{"edge/a", "edge/b", "r1/"}},
		{"route parentRefs", func(in *computed.AttachmentInput) {
			in.Routes[0].ParentRefs[0].Port = 80
			in.Routes[0].Written[0].Port, in.Routes[0].Written[0].PortSet = 80, true
		}, []string{"edge/a", "edge/b", "r1/"}},
		{"Selector namespace labels", func(in *computed.AttachmentInput) {
			in.NamespaceLabels["web"] = map[string]string{"team": "web", "x": "y"}
		},
			[]string{"edge/b", "r2/b"}},
		{"experimental setting", func(in *computed.AttachmentInput) { in.Experimental = install.Setting{Value: "false", Known: true} },
			[]string{"edge/a", "edge/b", "ls/c"}},
		{"attachedRoutes", func(in *computed.AttachmentInput) { res(in, 0).ListenerStatus[0].AttachedRoutes = 2 }, []string{"edge/a"}},
		{"listener observedGeneration", func(in *computed.AttachmentInput) {
			res(in, 0).ListenerStatus[0].Conditions[0].ObservedGeneration = gen - 1
		},
			[]string{"edge/a"}},

		// Unchanged by (rules §9.2).
		{"unrelated listener condition", func(in *computed.AttachmentInput) {
			ls := &res(in, 0).ListenerStatus[0]
			ls.Conditions = append(ls.Conditions, metav1.Condition{Type: "Programmed", Status: "False", ObservedGeneration: gen})
		}, nil},
		{"unrelated route condition", func(in *computed.AttachmentInput) {
			p := &in.Routes[0].Parents[0]
			p.Conditions = append(p.Conditions, metav1.Condition{Type: "ResolvedRefs", Status: "False", Reason: "BackendNotFound", ObservedGeneration: gen},
				metav1.Condition{Type: "kgateway.dev/Programmed", Status: "False", Reason: "RouteRuleReplaced", ObservedGeneration: gen})
		}, nil},
		{"lastTransitionTime", func(in *computed.AttachmentInput) {
			res(in, 0).ListenerStatus[0].Conditions[0].LastTransitionTime = metav1.Unix(1000, 0)
			in.Routes[0].Parents[0].Conditions[0].LastTransitionTime = metav1.Unix(1000, 0)
		}, nil},
		{"non-Selector namespace labels", func(in *computed.AttachmentInput) {
			in.NamespaceLabels["plain"] = map[string]string{"team": "web"}
		}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := fpWorld()
			tc.mutate(&in)
			got := prints(t, in)
			for name, fp := range base {
				if want := slices.Contains(tc.changed, name); (got[name] != fp) != want {
					t.Errorf("%s: changed=%v, want %v", name, got[name] != fp, want)
				}
			}
		})
	}
}

// ListenerSet acceptance is not a print input that matters: losing it makes
// the count unknown, which clears the window (rules §9.3 step 6).
func TestListenerSetAcceptanceClearsCount(t *testing.T) {
	in := fpWorld()
	res(&in, 1).Acceptance = computed.NoStatus
	if c := countCheck(t, check(v24, in), in.Resources[1], "c"); c.Class != computed.ClassUnknown || c.Candidate() {
		t.Errorf("status-less ListenerSet: %+v", c)
	}
}

// TLS mode decides a TLS listener's default kinds (rules §4.2), so it is hashed.
func TestTLSModeFingerprint(t *testing.T) {
	print := func(mode string) (count, route uint64) {
		l := cLis("t", "TLS", 443)
		l.TLS = &objects.ListenerTLS{Mode: mode}
		g := cGW("infra", "edge", l)
		lisStatus(&g, "t", 0, gen)
		rt := cRoute("infra", "r", wref("edge", ""))
		rt.Ref.Kind = "TLSRoute"
		status(&rt, rt.Written[0], "", gen)
		cs := check(v24, input([]computed.Resource{g}, rt))
		return countCheck(t, cs, g, "t").Fingerprint, oneRouteCheck(t, cs, rt).Fingerprint
	}
	c1, r1 := print("Terminate")
	c2, r2 := print("Passthrough")
	if c1 == c2 || r1 == r2 {
		t.Errorf("TLS mode must change the prints: count %v, route %v", c1 != c2, r1 != r2)
	}
}

// Currency needs every condition current; the print keeps the oldest, so a
// condition added at the current generation does not reset the window.
func TestListenerObservedGenerationIsOldest(t *testing.T) {
	print := func(gens ...int64) uint64 {
		in := fpWorld()
		st := &res(&in, 0).ListenerStatus[0]
		st.Conditions = nil
		for i, g := range gens {
			st.Conditions = append(st.Conditions, metav1.Condition{Type: fmt.Sprint("T", i), Status: "True", ObservedGeneration: g})
		}
		return prints(t, in)["edge/a"]
	}
	if print(gen-1) != print(gen-1, gen) {
		t.Error("a newer condition reset the print")
	}
	if print(gen-1) == print(gen) {
		t.Error("observedGeneration must be hashed")
	}
}

func TestRouteAcceptedReasonFingerprint(t *testing.T) {
	g := cGW("infra", "edge", cLis("a", "HTTP", 80))
	print := func(reason string) uint64 {
		rt := cRoute("infra", "r", wref("edge", "b"))
		status(&rt, rt.Written[0], reason, gen)
		return oneRouteCheck(t, check(v23, input([]computed.Resource{g}, rt)), rt).Fingerprint
	}
	if print("RouteRuleReplaced") != print("Conflicted") {
		t.Error("non-attachment reasons must hash as other")
	}
	if print("NoMatchingParent") == print("NotAllowedByListeners") {
		t.Error("attachment reasons must hash as written")
	}
	if print("NoMatchingParent") == print("") {
		t.Error("status must be hashed")
	}
}

// Fingerprints are independent of input and map order.
func TestFingerprintsCanonical(t *testing.T) {
	base := prints(t, fpWorld())
	rng := rand.New(rand.NewPCG(3, 4)) //nolint:gosec // test shuffle
	for range 20 {
		in := fpWorld()
		rng.Shuffle(len(in.Resources), func(i, j int) { in.Resources[i], in.Resources[j] = in.Resources[j], in.Resources[i] })
		rng.Shuffle(len(in.Routes), func(i, j int) { in.Routes[i], in.Routes[j] = in.Routes[j], in.Routes[i] })
		for i := range in.Resources {
			st := in.Resources[i].ListenerStatus
			rng.Shuffle(len(st), func(i, j int) { st[i], st[j] = st[j], st[i] })
		}
		in.NamespaceLabels["web"] = map[string]string{"x": "1", "team": "web", "a": "2"}
		in2 := fpWorld()
		in2.NamespaceLabels["web"] = map[string]string{"a": "2", "team": "web", "x": "1"}
		if got, want := prints(t, in), prints(t, in2); !mapsEqual(got, want) {
			t.Fatalf("order changed fingerprints:\n got %v\nwant %v", got, want)
		}
	}
	if len(base) != 6 {
		t.Fatalf("want 6 checks, got %v", base)
	}
}

func mapsEqual(a, b map[string]uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// Metadata outside the rules' inputs never reaches the fingerprint: decode a
// route before and after an annotation, resourceVersion and managedFields edit.
func TestFingerprintIgnoresMetadata(t *testing.T) {
	g := cGW("infra", "edge", cLis("a", "HTTP", 80))
	g.Generation = 0
	ref := testutil.GwRef("infra", "edge")
	u := testutil.HTTPRoute("infra", "r", []objects.ParentRef{ref}, testutil.AcceptedBy(ref, testutil.Controller))
	print := func(u *unstructured.Unstructured) uint64 {
		v := objects.DecodeRoute(u)
		rt := computed.Route{Ref: objects.RefOf(u), Generation: u.GetGeneration(), Hostnames: v.Hostnames, ParentRefs: v.ParentRefs,
			Written: objects.WrittenParentRefs(u), Parents: v.Parents}
		return oneRouteCheck(t, check(v24, input([]computed.Resource{g}, rt)), rt).Fingerprint
	}
	before := print(u)
	u2 := u.DeepCopy()
	u2.SetAnnotations(map[string]string{"example.com/note": "x"})
	u2.SetResourceVersion("999")
	u2.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "kubectl", Operation: "Update"}})
	if print(u2) != before {
		t.Error("metadata changed the fingerprint")
	}
}

// fpDelegDocs are the delegation fingerprint world's routes as YAML: roots r1
// (/a, /b) and r2 (/a) delegate to p, so p reports under two keys. p's rule 0
// selects c, rule 1 the label team (d), rule 2 e, which every context drops.
func fpDelegDocs() map[string]string {
	label := func(v string) string {
		return "{group: delegation.kgateway.dev, kind: label, namespace: apps, name: " + v + "}"
	}
	return map[string]string{
		"r1": "metadata: {name: r1, namespace: infra}\nspec:\n  parentRefs: [" + gwEntry + "]\n  rules: [" + rule([]string{"/a", "/b"}, to("apps", "p")) + "]",
		"r2": "metadata: {name: r2, namespace: infra}\nspec:\n  parentRefs: [" + gwEntry + "]\n  rules: [" + rule([]string{"/a"}, to("apps", "p")) + "]",
		"p": "metadata: {name: p, namespace: apps}\nspec:\n  parentRefs: [" + synth("infra", "r1") + ", " + synth("infra", "r2") + "]\n  rules: [" +
			rule([]string{"/a/x", "/b/x"}, to("apps", "c")) + ", " + rule([]string{"/a/x"}, label("team")) + ", " + rule([]string{"/a/x"}, to("apps", "e")) + "]",
		"c": "metadata: {name: c, namespace: apps, labels: {delegation.kgateway.dev/label: cee}}\nspec:\n  rules: [" + rule([]string{"/a/x/c"}) + "]",
		"d": "metadata: {name: d, namespace: apps, labels: {delegation.kgateway.dev/label: team}}\nspec:\n  rules: [" + rule([]string{"/a/x/d"}) + "]",
		"e": "metadata: {name: e, namespace: apps}\nspec:\n  parentRefs: [{group: gateway.networking.k8s.io, kind: HTTPRoute, namespace: apps, name: p}]\n  rules: [" +
			rule([]string{"/zzz"}) + "]",
	}
}

// fpDelegCase changes the world: docs before decoding, status after, the input last.
type fpDelegCase struct {
	name    string
	docs    func(map[string]string)
	status  func(t *testing.T, objs map[string]*unstructured.Unstructured)
	in      func(*computed.DelegationInput)
	unknown string // add an unknown root u that selects this route, so it is incomplete
}

// replace returns a docs change replacing old with new in doc name, once.
func replace(name, old, new string) func(map[string]string) {
	return func(d map[string]string) {
		if !strings.Contains(d[name], old) {
			panic(name + " has no " + old)
		}
		d[name] = strings.Replace(d[name], old, new, 1)
	}
}

// restatus replaces route name's status with one current entry under ref.
func restatus(name, ref string, conds ...string) func(*testing.T, map[string]*unstructured.Unstructured) {
	return restatusAt(name, ref, gen, conds...)
}

// restatusAt is restatus with the entry observed at obsGen.
func restatusAt(name, ref string, obsGen int64, conds ...string) func(*testing.T, map[string]*unstructured.Unstructured) {
	return func(t *testing.T, objs map[string]*unstructured.Unstructured) {
		unstructured.RemoveNestedField(objs[name].Object, "status")
		if ref != "" {
			entry(t, objs[name], ref, obsGen, conds...)
		}
	}
}

// delegPrints are the child-entry ("ce <child><-<parent>") and parent-resolvedrefs
// ("pr <parent>@<key name>") fingerprints of the world under fc.
func delegPrints(t *testing.T, v string, fc fpDelegCase) map[string]uint64 {
	t.Helper()
	docs := fpDelegDocs()
	if fc.docs != nil {
		fc.docs(docs)
	}
	objs := map[string]*unstructured.Unstructured{}
	w := dworld{roots: []string{"infra/r1", "infra/r2"}}
	for _, name := range slices.Sorted(maps.Keys(docs)) {
		objs[name] = hr(t, docs[name])
		w.routes = append(w.routes, objs[name])
	}
	if fc.unknown != "" {
		u := hr(t, "metadata: {name: u, namespace: infra}\nspec:\n  parentRefs: ["+gwEntry+"]\n  rules: ["+rule([]string{"/a/x"}, to("apps", fc.unknown))+"]")
		w.routes, w.unknown = append(w.routes, u), []string{"infra/u"}
	}
	entry(t, objs["r1"], gwEntry, gen, "Accepted=True", "ResolvedRefs=True")
	entry(t, objs["r2"], gwEntry, gen, "Accepted=True", "ResolvedRefs=True")
	entry(t, objs["p"], synth("infra", "r1"), gen, "Accepted=True", "ResolvedRefs=True")
	entry(t, objs["p"], synth("infra", "r2"), gen, "Accepted=True", "ResolvedRefs=True")
	entry(t, objs["c"], synth("apps", "p"), gen, "Accepted=True", "ResolvedRefs=True")
	if fc.status != nil {
		fc.status(t, objs)
	}
	w.mod = func(in *computed.DelegationInput) {
		in.Bounds.ContextsPerRow = 1 // so most contexts are not retained
		if fc.in != nil {
			fc.in(in)
		}
	}
	_, cs := w.run(v)
	out := map[string]uint64{}
	for _, c := range cs {
		var name string
		switch c.ID.Kind {
		case computed.KindChildEntry:
			name = "ce " + c.On.Name + "<-" + strings.Split(c.ID.Subject, "\x00")[7]
		case computed.KindParentResolvedRefs:
			name = "pr " + c.On.Name + "@" + c.Key.Name
		default:
			continue
		}
		if _, dup := out[name]; dup {
			t.Fatalf("duplicate check %s", name)
		}
		out[name] = c.Fingerprint
	}
	return out
}

func TestDelegationFingerprints(t *testing.T) {
	inherit := `annotations: {delegation.kgateway.dev/inherit-parent-matcher: "true"}`
	cases := []struct {
		fpDelegCase
		check   string
		changes bool
	}{
		// child-entry (rules §9.2).
		{fpDelegCase{name: "child ResolvedRefs", status: restatus("c", synth("apps", "p"), "Accepted=True", "ResolvedRefs=False/BackendNotFound")}, "ce c<-p", false},
		{fpDelegCase{name: "child Programmed", status: restatus("c", synth("apps", "p"), "Accepted=True", "ResolvedRefs=True", "kgateway.dev/Programmed=False/X")}, "ce c<-p", false},
		{fpDelegCase{name: "child other annotation", docs: replace("c", "namespace: apps,", "namespace: apps, annotations: {foo: bar},")}, "ce c<-p", false},
		{fpDelegCase{name: "unretained incoming match", docs: replace("p", "/b/x", "/b/z")}, "ce c<-p", true},
		{fpDelegCase{name: "path", docs: replace("r1", "value: /b}", "value: /b/}")}, "ce c<-p", true},
		{fpDelegCase{name: "child matches", docs: replace("c", "/a/x/c", "/a/x/cc")}, "ce c<-p", true},
		{fpDelegCase{name: "child hostnames", docs: replace("e", "spec:\n", "spec:\n  hostnames: [e.example.com]\n")}, "ce e<-p", true},
		{fpDelegCase{name: "child parentRefs", docs: replace("e", "name: p}", "name: p, port: 80}")}, "ce e<-p", true},
		{fpDelegCase{name: "child inherit", docs: replace("e", "namespace: apps}", "namespace: apps, "+inherit+"}")}, "ce e<-p", true},
		{fpDelegCase{name: "ref", docs: replace("p", to("apps", "c"), "{group: delegation.kgateway.dev, kind: label, namespace: apps, name: cee}")}, "ce c<-p", true},
		{fpDelegCase{name: "wildcard", in: func(in *computed.DelegationInput) { in.Wildcard.Value = "elsewhere" }}, "ce c<-p", true},
		{fpDelegCase{name: "completeness", unknown: "c"}, "ce c<-p", true},
		{fpDelegCase{name: "Accepted observedGeneration", status: restatusAt("c", synth("apps", "p"), gen-1, "Accepted=True", "ResolvedRefs=True")}, "ce c<-p", true},
		{fpDelegCase{name: "entry presence", status: restatus("c", "")}, "ce c<-p", true},
		{fpDelegCase{name: "UnsupportedValue", status: restatus("c", synth("apps", "p"), "Accepted=False/UnsupportedValue", "ResolvedRefs=True")}, "ce c<-p", true},
		// parent-resolvedrefs (rules §9.2); r1's /b context is under p's key r1 only.
		{fpDelegCase{name: "unretained context under K1", docs: replace("r1", "value: /b}", "value: /b/}")}, "pr p@r1", true},
		{fpDelegCase{name: "unretained context under K1", docs: replace("r1", "value: /b}", "value: /b/}")}, "pr p@r2", false},
		{fpDelegCase{name: "parent Accepted and Programmed", status: func(t *testing.T, objs map[string]*unstructured.Unstructured) {
			restatus("p", synth("infra", "r1"), "Accepted=False/Other", "ResolvedRefs=True", "kgateway.dev/Programmed=False/X")(t, objs)
			entry(t, objs["p"], synth("infra", "r2"), gen, "Accepted=True", "ResolvedRefs=True")
		}}, "pr p@r1", false},
		{fpDelegCase{name: "unused child label", docs: replace("d", "labels: {", "labels: {foo: bar, ")}, "pr p@r1", false},
		{fpDelegCase{name: "child inherit", docs: replace("c", "namespace: apps,", "namespace: apps, "+inherit+",")}, "pr p@r1", true},
		{fpDelegCase{name: "child matches", docs: replace("c", "/a/x/c", "/a/x/cc")}, "pr p@r1", true},
		{fpDelegCase{name: "label used by a label ref", docs: replace("d", "label: team", "label: team2")}, "pr p@r1", true},
		{fpDelegCase{name: "backendRef kinds", docs: replace("p", to("apps", "e"), to("apps", "e")+", {name: svc, port: 80}")}, "pr p@r1", true},
		{fpDelegCase{name: "ExtensionRef filter", docs: replace("p", "{matches:", "{filters: [{type: ExtensionRef, extensionRef: {group: x.io, kind: Thing, name: t}}], matches:")}, "pr p@r1", true},
		{fpDelegCase{name: "child hostnames", docs: replace("e", "spec:\n", "spec:\n  hostnames: [e.example.com]\n")}, "pr p@r1", true},
		{fpDelegCase{name: "child parentRefs", docs: replace("e", "name: p}", "name: p, port: 80}")}, "pr p@r1", true},
		{fpDelegCase{name: "ref", docs: replace("p", to("apps", "c"), "{group: delegation.kgateway.dev, kind: label, namespace: apps, name: cee}")}, "pr p@r1", true},
		{fpDelegCase{name: "wildcard", in: func(in *computed.DelegationInput) { in.Wildcard.Value = "elsewhere" }}, "pr p@r1", true},
		// p also permits u, so the unknown root reaches it; that ref changes nothing else of (p, r1).
		{fpDelegCase{name: "completeness", unknown: "p", docs: replace("p", synth("infra", "r2")+"]", synth("infra", "r2")+", "+synth("infra", "u")+"]")}, "pr p@r1", true},
		{fpDelegCase{name: "parent permits u, no unknown root", docs: replace("p", synth("infra", "r2")+"]", synth("infra", "r2")+", "+synth("infra", "u")+"]")}, "pr p@r1", false},
		{fpDelegCase{name: "ResolvedRefs observedGeneration", status: func(t *testing.T, objs map[string]*unstructured.Unstructured) {
			restatusAt("p", synth("infra", "r1"), gen-1, "Accepted=True", "ResolvedRefs=True")(t, objs)
			entry(t, objs["p"], synth("infra", "r2"), gen, "Accepted=True", "ResolvedRefs=True")
		}}, "pr p@r1", true},
		{fpDelegCase{name: "ResolvedRefs", status: func(t *testing.T, objs map[string]*unstructured.Unstructured) {
			restatus("p", synth("infra", "r1"), "Accepted=True", "ResolvedRefs=False/BackendNotFound")(t, objs)
			entry(t, objs["p"], synth("infra", "r2"), gen, "Accepted=True", "ResolvedRefs=True")
		}}, "pr p@r1", true},
	}
	for _, v := range both {
		base := delegPrints(t, v, fpDelegCase{})
		for _, k := range []string{"ce c<-p", "ce e<-p", "pr p@r1", "pr p@r2"} {
			if base[k] == 0 {
				t.Fatalf("%s: no %s in %v", v, k, base)
			}
		}
		for _, tc := range cases {
			got := delegPrints(t, v, tc.fpDelegCase)
			if changed := got[tc.check] != base[tc.check]; changed != tc.changes {
				t.Errorf("%s %s: %s changed %v, want %v", v, tc.name, tc.check, changed, tc.changes)
			}
		}
		// An ExtensionRef filter's target is not read: only that one exists (rules §9.2).
		withExt := func(name string) func(map[string]string) {
			return replace("p", "{matches:", "{filters: [{type: ExtensionRef, extensionRef: {group: x.io, kind: Thing, name: "+name+"}}], matches:")
		}
		if delegPrints(t, v, fpDelegCase{docs: withExt("t")})["pr p@r1"] != delegPrints(t, v, fpDelegCase{docs: withExt("u")})["pr p@r1"] {
			t.Errorf("%s: an ExtensionRef target changed the parent-resolvedrefs print", v)
		}
		// Reasons other than UnsupportedValue are hashed alike.
		a := delegPrints(t, v, fpDelegCase{status: restatus("c", synth("apps", "p"), "Accepted=False/RouteRuleReplaced")})
		b := delegPrints(t, v, fpDelegCase{status: restatus("c", synth("apps", "p"), "Accepted=False/Other")})
		if a["ce c<-p"] != b["ce c<-p"] {
			t.Errorf("%s: a non-UnsupportedValue reason changed the child-entry print", v)
		}
	}
}
