package computed_test

import (
	"math/rand/v2"
	"slices"
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
		{"ListenerSet acceptance", func(in *computed.AttachmentInput) { res(in, 1).Acceptance = computed.NoStatus }, []string{"ls/c"}},
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
