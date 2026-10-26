package computed_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/rulesets"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// tags are the kgateway tags each rule set's kgateway provenance must cite.
var tags = map[string]string{"kgateway-2.3": "v2.3.11", "kgateway-2.4": "v2.4.7"}

// listenerExpect is a listener count. Every field is compared; absent means zero.
type listenerExpect struct {
	Count         int    `json:"count"`
	Unknown       string `json:"unknown,omitempty"`
	Error         string `json:"error,omitempty"`
	IsolationNote bool   `json:"isolationNote,omitempty"`
}

// refExpect is a parentRef outcome. Listeners is compared only when written,
// since kgateway's status does not name them.
type refExpect struct {
	Outcome   string    `json:"outcome,omitempty"`
	Unknown   string    `json:"unknown,omitempty"`
	Listeners *[]string `json:"listeners,omitempty"`
}

// viewAExpect is a case's expect block. Keys are "<Kind> <ns>/<name> <listener>"
// for listeners, "<Kind> <ns>/<name> #<i>" for parentRefs and
// "<route key> -> <listener key>" for members, whose value is the effective hostnames.
type viewAExpect struct {
	Listeners  map[string]listenerExpect `json:"listeners"`
	ParentRefs map[string]refExpect      `json:"parentRefs"`
	// Members is compared only when written.
	Members *map[string][]string `json:"members"`
	// Checks and KGWUI012 are the cross-check's; pkg/api's matrix reads them.
	Checks   json.RawMessage `json:"checks"`
	KGWUI012 json.RawMessage `json:"kgwui012"`
}

func TestRuleSetGoldens(t *testing.T) {
	for dir, tag := range tags {
		t.Run(dir, func(t *testing.T) { goldens(t, dir, tag) })
	}
}

// goldens checks every case of one rule set's directory.
func goldens(t *testing.T, dir, tag string) {
	files, err := filepath.Glob(filepath.Join("testdata", dir, "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures (%v)", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			c := testutil.LoadRuleCase(t, f)
			checkProvenance(t, c.Provenance, tag)
			if len(c.Bounds) > 0 {
				t.Fatal("bounds are not read yet")
			}
			rules := rulesets.Select(c.ControllerVersion).Rules
			if rules.Version() != dir {
				t.Fatalf("controller %q selects %s, not %s", c.ControllerVersion, rules.Version(), dir)
			}
			want := decodeExpect(t, c.Expect)
			got := viewA(rules.Attachment(attachmentInput(c)))
			if !reflect.DeepEqual(got.Listeners, want.Listeners) {
				t.Errorf("listeners:\n got %v\nwant %v", got.Listeners, want.Listeners)
			}
			for k, w := range want.ParentRefs {
				if w.Listeners == nil {
					w.Listeners = got.ParentRefs[k].Listeners
					want.ParentRefs[k] = w
				}
			}
			if !reflect.DeepEqual(got.ParentRefs, want.ParentRefs) {
				t.Errorf("parentRefs:\n got %s\nwant %s", show(got.ParentRefs), show(want.ParentRefs))
			}
			if want.Members != nil && !reflect.DeepEqual(*got.Members, *want.Members) {
				t.Errorf("members:\n got %v\nwant %v", *got.Members, *want.Members)
			}
		})
	}
}

// checkProvenance requires a provenance, and the rule set's tag on a kgateway output.
func checkProvenance(t *testing.T, p, tag string) {
	t.Helper()
	switch {
	case p == "":
		t.Fatal("case has no provenance")
	case strings.HasPrefix(p, "translator/") && !strings.HasSuffix(p, "@"+tag):
		t.Fatalf("provenance %q does not cite %s", p, tag)
	case !strings.HasPrefix(p, "translator/") && !strings.HasPrefix(p, "computed-rules §"):
		t.Fatalf("provenance %q names neither a kgateway output nor a rules section", p)
	}
}

// decodeExpect decodes a case's expect block strictly; listeners and parentRefs are required.
func decodeExpect(t *testing.T, m map[string]any) viewAExpect {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	var e viewAExpect
	if err := dec.Decode(&e); err != nil {
		t.Fatalf("expect: %v", err)
	}
	if e.Listeners == nil || e.ParentRefs == nil {
		t.Fatal("expect needs listeners and parentRefs, even when empty")
	}
	return e
}

// show renders parentRef outcomes as JSON, so pointer fields print as values.
func show(m map[string]refExpect) string {
	b, _ := json.Marshal(m)
	return string(b)
}

func key(r objects.Ref) string { return r.Kind + " " + r.Namespace + "/" + r.Name }

// viewA renders a result in the expect block's shape.
func viewA(res computed.AttachmentResult) viewAExpect {
	out := viewAExpect{Listeners: map[string]listenerExpect{}, ParentRefs: map[string]refExpect{}, Members: &map[string][]string{}}
	for _, c := range res.Counts {
		out.Listeners[key(c.Resource)+" "+c.Listener] = listenerExpect{
			Count: c.Count, Unknown: string(c.Unknown), Error: c.Error, IsolationNote: c.IsolationNote,
		}
	}
	for _, o := range res.Outcomes {
		ls := append([]string{}, o.Listeners...)
		out.ParentRefs[fmt.Sprintf("%s #%d", key(o.Route), o.ParentRef)] = refExpect{
			Outcome: string(o.Outcome), Unknown: string(o.Unknown), Listeners: &ls,
		}
	}
	for _, m := range res.Members {
		k := fmt.Sprintf("%s #%d -> %s %s", key(m.Route), m.ParentRef, key(m.Resource), m.Listener)
		(*out.Members)[k] = append([]string{}, m.Hostnames...)
	}
	return out
}

// attachmentInput builds view A's input from the case's objects with the pkg/objects decoders.
func attachmentInput(c testutil.RuleCase) computed.AttachmentInput {
	in := computed.AttachmentInput{
		NamespaceLabels: c.Namespaces,
		NamespacesOK:    c.NamespacesOK,
		Experimental:    experimental(c.Settings),
	}
	for _, u := range c.Objects {
		ref := objects.RefOf(u)
		switch ref.Kind {
		case "Gateway":
			v := objects.DecodeGateway(u)
			in.Resources = append(in.Resources, computed.Resource{
				Ref: ref, Generation: u.GetGeneration(), Listeners: v.Listeners, ListenerStatus: v.ListenerStatus,
			})
		case "ListenerSet", "XListenerSet":
			v := objects.DecodeListenerSet(u)
			in.Resources = append(in.Resources, computed.Resource{
				Ref: ref, Parent: v.Parent.Ref(), Generation: u.GetGeneration(), Listeners: v.Listeners,
				ListenerStatus: v.ListenerStatus, Acceptance: acceptance(u, v),
			})
		case "HTTPRoute", "GRPCRoute", "TCPRoute", "TLSRoute":
			v := objects.DecodeRoute(u)
			gv, _ := schema.ParseGroupVersion(u.GetAPIVersion())
			in.Routes = append(in.Routes, computed.Route{
				Ref: ref, Generation: u.GetGeneration(), Hostnames: v.Hostnames, ParentRefs: v.ParentRefs,
				Written: objects.WrittenParentRefs(u), Parents: v.Parents,
				ExperimentalOnly: kinds.ExperimentalOnly(gv.Group, ref.Kind, gv.Version, c.ControllerVersion),
			})
		}
	}
	return in
}

// acceptance reads a ListenerSet's current Accepted condition (rules §3.1).
func acceptance(u *unstructured.Unstructured, v *objects.ListenerSetView) computed.Acceptance {
	cond := objects.FindCondition(v.Conditions, "Accepted")
	switch {
	case cond == nil || objects.IsStale(u, *cond):
		return computed.NoStatus
	case cond.Status == "False":
		return computed.Denied
	}
	return computed.Accepted
}

// experimental reads the experimental setting as install.ReadSettings would.
func experimental(settings map[string]string) install.Setting {
	v, ok := settings[install.ExperimentalEnv]
	switch {
	case !ok:
		return install.Setting{Value: "true", Known: true}
	case v == testutil.SettingValueFrom:
		return install.Setting{}
	}
	return install.Setting{Value: v, Known: true}
}
