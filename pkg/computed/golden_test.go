package computed_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	// Checks, KGWUI012 and KGWUI011 are the cross-check's; pkg/api's matrix reads them.
	Checks   json.RawMessage `json:"checks"`
	KGWUI012 json.RawMessage `json:"kgwui012"`
	KGWUI011 json.RawMessage `json:"kgwui011"`
	delegationExpect
}

// delegationExpect is views B and C, compared only when Rows is written; then
// Entries, Signals, Effective and EffectiveTruncated are compared in full and
// absent means none. Routes are HTTPRoutes, written "<ns>/<name>"; report keys
// are written as kgateway's status parentRef (see keyString).
type delegationExpect struct {
	// Rows are keyed "<parent> <rule>.<ref> <child>", "-" for no child.
	Rows *map[string]rowExpect `json:"rows"`
	// Entries are the child status entries the rows expect, keyed
	// "<child> <key>": Accepted or UnsupportedValue (rules §8.2 child-entry).
	Entries map[string]string `json:"entries"`
	// Signals are the parents' ResolvedRefs=False reasons, keyed "<parent> <key>".
	Signals map[string][]string `json:"signals"`
	// Effective is view C per edge, keyed "<child> <- <parent>".
	Effective map[string]edgeExpect `json:"effective"`
	// EffectiveTruncated are the children with more parent edges than kept.
	EffectiveTruncated []string `json:"effectiveTruncated"`
	// Incomplete (routes in a reach closure), Uncertain (uncertain points,
	// "<route> <rule>.<ref> <reason>", rule "root" or "possible" without a ref)
	// and Cap are compared only when written.
	Incomplete *[]string `json:"incomplete"`
	Uncertain  *[]string `json:"uncertain"`
	Cap        *string   `json:"cap"`
}

// rowExpect is a row's aggregate (rules §5.5). Every field is compared except
// Contexts (the retained count), Paths and Deps, which are compared only when written.
type rowExpect struct {
	Outcome           string    `json:"outcome"`
	Unknown           string    `json:"unknown,omitempty"`
	Reason            string    `json:"reason,omitempty"`
	SomeNotComputed   bool      `json:"someNotComputed,omitempty"`
	ContextsTruncated bool      `json:"contextsTruncated,omitempty"`
	Contexts          *int      `json:"contexts,omitempty"`
	Paths             *[]string `json:"paths,omitempty"`
	Deps              *[]string `json:"deps,omitempty"`
}

// edgeExpect is one effective edge: each retained context's rules (see ctxRules).
type edgeExpect struct {
	Contexts          []string `json:"contexts"`
	ContextsTruncated bool     `json:"contextsTruncated,omitempty"`
}

func TestRuleSetGoldens(t *testing.T) {
	for dir, tag := range tags {
		t.Run(dir, func(t *testing.T) { goldens(t, dir, tag) })
	}
}

// delegationOutputs are kgateway's delegation translator tests every rule set
// pins (rules §11.2); the map adds those of one tag only.
var (
	delegationOutputs = []string{"basic", "basic_invalid_hostname", "basic_parentref_match", "basic_parentref_mismatch",
		"child_rule_matcher", "cyclic1", "cyclic2", "discard_invalid_child_matches", "inherit_parentref",
		"invalid_child_valid_standalone", "label_based", "multiple_children", "multiple_parents",
		"multi_level_multiple_parents", "nested_absolute_relative", "relative_paths", "unresolved_ref"}
	delegationOutputsOf = map[string][]string{"kgateway-2.4": {"basic_parentref_match_same_namespace"}}
)

// TestDelegationGoldensCiteKgateway requires a views B and C case for each of
// kgateway's delegation outputs at the rule set's tag.
func TestDelegationGoldensCiteKgateway(t *testing.T) {
	for dir, tag := range tags {
		files, _ := filepath.Glob(filepath.Join("testdata", dir, "delegation-*.yaml"))
		cited := map[string]bool{}
		for _, f := range files {
			c := testutil.LoadRuleCase(t, f)
			if c.Expect["rows"] == nil {
				t.Errorf("%s: a delegation case without rows", f)
			}
			cited[c.Provenance] = true
		}
		for _, o := range slices.Concat(delegationOutputs, delegationOutputsOf[dir]) {
			if p := "translator/gateway/testutils/outputs/delegation/" + o + ".yaml@" + tag; !cited[p] {
				t.Errorf("%s: no case cites %s", dir, p)
			}
		}
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
			rules := rulesets.Select(c.ControllerVersion).Rules
			if rules.Version() != dir {
				t.Fatalf("controller %q selects %s, not %s", c.ControllerVersion, rules.Version(), dir)
			}
			want := decodeExpect(t, c.Expect)
			ain := attachmentInput(c)
			att := rules.Attachment(ain)
			got := viewA(att)
			if want.Rows != nil {
				din := delegationInput(t, c, ain, att)
				compareDelegation(t, want.delegationExpect, din, rules.Delegation(din))
			} else if len(c.Bounds) > 0 {
				t.Fatal("bounds without rows")
			}
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
		ScopeUnknown:    scope(c).Mode == install.ScopeUnknown,
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

// scope is the case's discovery scope, read from the settings as
// install.NamespaceSelectorScope reads the controller env.
func scope(c testutil.RuleCase) install.Scope {
	v, ok := c.Settings[install.DiscoveryNamespaceSelectorsEnv]
	switch {
	case !ok:
		return install.Scope{Mode: install.ScopeAll}
	case v == testutil.SettingValueFrom:
		return install.Scope{Mode: install.ScopeUnknown}
	}
	return install.ParseSelectors(v)
}

// delegationInput builds views B and C's input from the case as the engine
// does (pkg/engine/views.go): in-scope HTTPRoutes, ownership from Gateway and
// ListenerSet parents then reported edges, and computed.Roots from view A.
func delegationInput(t *testing.T, c testutil.RuleCase, ain computed.AttachmentInput, att computed.AttachmentResult) computed.DelegationInput {
	t.Helper()
	sc := scope(c)
	// The wildcard setting as install.ReadSettings reads it: absent or empty means all.
	wc := install.Setting{Value: install.WildcardAll, Known: true}
	switch v, ok := c.Settings[install.WildcardEnv]; {
	case !ok || v == "":
	case v == testutil.SettingValueFrom:
		wc = install.Setting{}
	default:
		wc.Value = v
	}
	in := computed.DelegationInput{Wildcard: wc, ControllerNames: []string{testutil.Controller},
		Bounds: bounds(t, c.Bounds), ScopeUnknown: sc.Mode == install.ScopeUnknown, Unwatched: map[objects.Ref]bool{}}
	parents := map[objects.Ref]bool{} // the case's Gateways and ListenerSets
	for _, r := range ain.Resources {
		parents[r.Ref] = true
	}
	objs := map[objects.Ref]*unstructured.Unstructured{}
	for _, u := range c.Objects {
		if u.GetKind() != "HTTPRoute" {
			continue
		}
		ref := objects.RefOf(u)
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: u.GetNamespace(), Labels: c.Namespaces[u.GetNamespace()]}}
		if !sc.Contains(u.GetNamespace(), ns) {
			in.Unwatched[ref] = true
			continue
		}
		v := objects.DecodeHTTPRoute(u)
		top := slices.ContainsFunc(v.ParentRefs, func(p objects.ParentRef) bool { return parents[p.Ref()] })
		objs[ref] = u
		in.Routes = append(in.Routes, computed.DelegRoute{Ref: ref, Generation: u.GetGeneration(), View: v, Owned: top, TopLevel: top})
	}
	slices.SortFunc(in.Routes, func(x, y computed.DelegRoute) int { return x.Ref.Compare(y.Ref) })
	// Children are owned over reported edges from owned routes, as pkg/install's
	// delegate does (pkg/install/delegation.go); it works on an Assignment, so it is mirrored.
	for changed := true; changed; {
		changed = false
		for i := range in.Routes {
			r := &in.Routes[i]
			if r.Owned {
				continue
			}
			for _, p := range in.Routes {
				if _, ok := install.ReportedEdge(r.View, p.Ref, in.ControllerNames); ok && p.Owned {
					r.Owned, changed = true, true
					break
				}
			}
		}
	}
	in.Roots = computed.Roots(in.Routes, att, statusTarget(objs, parents))
	return in
}

// statusTarget is the case's computed.StatusTarget, as the engine's
// (pkg/engine/views.go statusTarget): the first current Accepted=True entry of
// testutil.Controller for one of the case's Gateways or ListenerSets.
func statusTarget(objs map[objects.Ref]*unstructured.Unstructured, parents map[objects.Ref]bool) computed.StatusTarget {
	return func(r computed.DelegRoute) (objects.Ref, computed.ReportKey, bool) {
		for _, e := range r.View.Parents {
			cond := objects.FindCondition(e.Conditions, "Accepted")
			if e.ControllerName != testutil.Controller || !parents[e.ParentRef.Ref()] ||
				cond == nil || cond.Status != "True" || objects.IsStale(objs[r.Ref], *cond) {
				continue
			}
			return e.ParentRef.Ref(), computed.KeyOf(e.Written), true
		}
		return objects.Ref{}, computed.ReportKey{}, false
	}
}

// bounds is DefaultBounds with the case's overrides, by field name.
func bounds(t *testing.T, over map[string]int) computed.Bounds {
	t.Helper()
	b := computed.DefaultBounds
	v := reflect.ValueOf(&b).Elem()
	for name, n := range over {
		f := v.FieldByName(name)
		if !f.IsValid() {
			t.Fatalf("bounds: no field %q", name)
		}
		f.SetInt(int64(n))
	}
	return b
}

// compareDelegation compares views B and C with the case's expectation.
func compareDelegation(t *testing.T, want delegationExpect, in computed.DelegationInput, res computed.DelegationResult) {
	t.Helper()
	rows := map[string]rowExpect{}
	entries := map[string]string{}
	for _, r := range res.Rows {
		child := "-"
		if r.Key.Child != (objects.Ref{}) {
			child = nsName(r.Key.Child)
		}
		k := fmt.Sprintf("%s %d.%d %s", nsName(r.Key.Parent), r.Key.Rule, r.Key.Ref, child)
		g := rowExpect{Outcome: string(r.Outcome), Unknown: string(r.Unknown), SomeNotComputed: r.SomeNotComputed,
			ContextsTruncated: r.ContextsTruncated}
		if r.Reason != nil {
			g.Reason = string(r.Reason.Clause)
		}
		if w, ok := (*want.Rows)[k]; ok {
			if w.Contexts != nil {
				g.Contexts = ptr(len(r.Contexts))
			}
			if w.Paths != nil {
				ps := []string{}
				for _, c := range r.Contexts {
					ps = append(ps, pathString(c.Path.Steps()))
				}
				g.Paths = &ps
			}
			if w.Deps != nil {
				ds := []string{}
				for _, d := range r.Deps {
					ds = append(ds, key(d))
				}
				g.Deps = &ds
			}
		}
		rows[k] = g
		if r.WritesEntry {
			switch r.Outcome {
			case computed.OutcomeAttached, computed.OutcomePartial:
				entries[nsName(r.Key.Child)+" "+keyString(r.ChildKey)] = "Accepted"
			case computed.OutcomeRejectedHostnames:
				entries[nsName(r.Key.Child)+" "+keyString(r.ChildKey)] = "UnsupportedValue"
			}
		}
	}
	if !reflect.DeepEqual(rows, *want.Rows) {
		t.Errorf("rows:\n got %s\nwant %s", js(rows), js(*want.Rows))
	}
	if !reflect.DeepEqual(entries, orEmpty(want.Entries)) {
		t.Errorf("entries:\n got %v\nwant %v", entries, want.Entries)
	}
	signals := map[string][]string{}
	for _, s := range res.Signals {
		signals[nsName(s.Parent)+" "+keyString(s.Key)] = s.Reasons
	}
	if !reflect.DeepEqual(signals, orEmpty(want.Signals)) {
		t.Errorf("signals:\n got %v\nwant %v", signals, want.Signals)
	}
	effective, truncated := map[string]edgeExpect{}, []string{}
	for child, ce := range res.Effective {
		if ce.Truncated {
			truncated = append(truncated, nsName(child))
		}
		for _, e := range ce.Edges {
			cs := []string{}
			for _, c := range e.Contexts {
				cs = append(cs, ctxRules(c))
			}
			effective[nsName(child)+" <- "+nsName(e.Parent)] = edgeExpect{Contexts: cs, ContextsTruncated: e.ContextsTruncated}
			checkSameModel(t, res, child, e)
		}
	}
	if !reflect.DeepEqual(effective, orEmpty(want.Effective)) {
		t.Errorf("effective:\n got %s\nwant %s", js(effective), js(want.Effective))
	}
	slices.Sort(truncated)
	if !slices.Equal(truncated, want.EffectiveTruncated) && len(truncated)+len(want.EffectiveTruncated) > 0 {
		t.Errorf("effectiveTruncated %v, want %v", truncated, want.EffectiveTruncated)
	}
	if want.Incomplete != nil {
		inc := []string{}
		for _, r := range in.Routes {
			if res.Complete.Incomplete(r.Ref) {
				inc = append(inc, nsName(r.Ref))
			}
		}
		if !slices.Equal(inc, *want.Incomplete) {
			t.Errorf("incomplete %v, want %v", inc, *want.Incomplete)
		}
	}
	if want.Uncertain != nil {
		var got []string
		for _, u := range res.Uncertain {
			at := fmt.Sprintf("%d.%d", u.Rule, u.Ref)
			switch u.Rule {
			case computed.UncertainRoot:
				at = "root"
			case computed.UncertainPossible:
				at = "possible"
			}
			got = append(got, nsName(u.Route)+" "+at+" "+string(u.Reason))
		}
		if !slices.Equal(got, *want.Uncertain) && len(got)+len(*want.Uncertain) > 0 {
			t.Errorf("uncertain %v, want %v", got, *want.Uncertain)
		}
	}
	if want.Cap != nil && res.Cap != *want.Cap {
		t.Errorf("cap %q, want %q", res.Cap, *want.Cap)
	}
}

// checkSameModel requires each effective context to be a retained context of
// a row of that (parent, child): the effective matcher shown is the one the
// traversal used (rules §5.7).
func checkSameModel(t *testing.T, res computed.DelegationResult, child objects.Ref, e computed.EffectiveEdge) {
	t.Helper()
	for _, c := range e.Contexts {
		if !slices.ContainsFunc(res.Rows, func(r computed.Row) bool {
			return r.Key.Parent == e.Parent && r.Key.Child == child &&
				slices.ContainsFunc(r.Contexts, func(rc computed.Context) bool { return reflect.DeepEqual(rc, c) })
		}) {
			t.Errorf("effective %s <- %s: context %s is no row's", nsName(child), nsName(e.Parent), ctxRules(c))
		}
	}
}

func nsName(r objects.Ref) string { return r.Namespace + "/" + r.Name }

func ptr[T any](v T) *T { return &v }

func orEmpty[M ~map[K]V, K comparable, V any](m M) M {
	if m == nil {
		return M{}
	}
	return m
}

func js(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// keyString writes a report key as kgateway's status parentRef: the fields
// written, in its order, e.g. "{group: gateway.networking.k8s.io, kind: HTTPRoute, name: p, namespace: ns}".
func keyString(k computed.ReportKey) string {
	var f []string
	for _, kv := range [][2]string{{"group", k.Group}, {"kind", k.Kind}, {"name", k.Name}, {"namespace", k.Namespace}, {"sectionName", k.SectionName}} {
		if kv[1] != "" {
			f = append(f, kv[0]+": "+kv[1])
		}
	}
	return "{" + strings.Join(f, ", ") + "}"
}

// matchString writes a match as "<type> <path>", then " h:<name>=<value>",
// " q:<name>=<value>" (~ for a regex) and " m:<method>".
func matchString(m objects.Match) string {
	s := "PathPrefix /"
	if m.Path != nil {
		s = m.Path.Type + " " + m.Path.Value
	}
	kv := func(p string, ks []objects.KeyMatch) {
		for _, k := range ks {
			op := "="
			if k.Type == "RegularExpression" {
				op = "~"
			}
			s += " " + p + ":" + k.Name + op + k.Value
		}
	}
	kv("h", m.Headers)
	kv("q", m.QueryParams)
	if m.Method != "" {
		s += " m:" + m.Method
	}
	return s
}

// pathString writes a context's path as "<route> <match> > ...".
func pathString(p []computed.PathStep) string {
	var s []string
	for _, st := range p {
		s = append(s, nsName(st.Route)+" "+matchString(st.Match))
	}
	return strings.Join(s, " > ")
}

// ctxRules writes a context's child rules: "r<i> <match> | <match>" when kept,
// "r<i> dropped <clause>" when not, joined by "; ".
func ctxRules(c computed.Context) string {
	var s []string
	for _, r := range c.Rules {
		if !r.Kept {
			clause := ""
			if r.Reason != nil {
				clause = string(r.Reason.Clause)
			}
			s = append(s, fmt.Sprintf("r%d dropped %s", r.Index, clause))
			continue
		}
		var ms []string
		for _, m := range r.Matches {
			ms = append(ms, matchString(m))
		}
		s = append(s, fmt.Sprintf("r%d %s", r.Index, strings.Join(ms, " | ")))
	}
	return strings.Join(s, "; ")
}
