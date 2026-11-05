package health

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// withSpecListeners replaces u's spec listeners with ls, written as HTTP listeners.
func withSpecListeners(u *unstructured.Unstructured, ls ...objects.Listener) *unstructured.Unstructured {
	specLs := make([]any, 0, len(ls))
	for _, l := range ls {
		m := map[string]any{"name": l.Name, "port": int64(l.Port), "protocol": "HTTP"}
		if l.Hostname != "" {
			m["hostname"] = l.Hostname
		}
		specLs = append(specLs, m)
	}
	u.Object["spec"].(map[string]any)["listeners"] = specLs
	return u
}

// conflictedListener is the status kgateway writes for a listener that loses a
// hostname conflict: Conflicted=True, Accepted and Programmed False.
func conflictedListener(name string) testutil.Listener {
	return testutil.Listener{Name: name, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionFalse, "HostnameConflict"),
		testutil.Cond("Programmed", metav1.ConditionFalse, "HostnameConflict"),
		testutil.Cond("Conflicted", metav1.ConditionTrue, "HostnameConflict"),
	}}
}

func okListener(name string) testutil.Listener {
	return testutil.Listener{Name: name, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}}
}

// notValidGateway is infra/edge with Accepted=True/ListenersNotValid, as
// kgateway writes it when one listener is rejected.
func notValidGateway(spec []objects.Listener, status ...testutil.Listener) *unstructured.Unstructured {
	cs := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "ListenersNotValid"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}
	return withSpecListeners(testutil.Gateway("infra", "edge", "kgateway", cs, status...), spec...)
}

// conflictReport evaluates the healthy world with Gateway gw and extra objects.
func conflictReport(t *testing.T, gw *unstructured.Unstructured, extra ...*unstructured.Unstructured) Report {
	t.Helper()
	w := healthyWorld()
	w.objs[1] = gw
	w.objs = append(w.objs, extra...)
	return w.report(t)
}

// onlyConflict returns the one 017 on res and its evidence; the Conflicted
// condition gives no 002 (D-030).
func onlyConflict(t *testing.T, r Report, res string) (Finding, *ListenerConflictEvidence) {
	t.Helper()
	for _, f := range findingsFor(r, CodeConditionFalse, res) {
		if strings.HasPrefix(f.Detail, "Conflicted=") {
			t.Fatalf("002 for the Conflicted condition next to 017: %+v", f)
		}
	}
	fs := findingsFor(r, CodeListenerConflicted, res)
	if len(fs) != 1 {
		t.Fatalf("want one 017 on %s, got %+v", res, r.Findings)
	}
	ev, ok := fs[0].Facts.(*ListenerConflictEvidence)
	if !ok {
		t.Fatalf("evidence %#v, want listener-conflict", fs[0].Facts)
	}
	return fs[0], ev
}

func lsRef(ns, name string) objects.Ref {
	return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: ns, Name: name}
}

func gwRef(ns, name string) objects.Ref {
	return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: ns, Name: name}
}

// Design §7.5 and phase-3.3 brief: an Accepted=True/ListenersNotValid Gateway
// whose listener https is conflicted raises 017, keyed by the listener, with
// the ListenerSet in another namespace named as the other side.
func TestListenerConflictBothSidesNamed(t *testing.T) {
	gw := notValidGateway([]objects.Listener{{Name: "http", Port: 80}, {Name: "https", Port: 443, Hostname: "*.example.com"}},
		okListener("http"), conflictedListener("https"))
	ls := withSpecListeners(testutil.ListenerSet("shop", "more", testutil.GwRef("infra", "edge"), nil, okListener("a")),
		objects.Listener{Name: "a", Port: 443, Hostname: "a.example.com"})
	r := conflictReport(t, gw, ls)
	f, ev := onlyConflict(t, r, "infra/edge")
	if f.Severity != SeverityError || f.Slug != "listener-conflicted" || f.Message != "Listener hostname conflicts with another listener" {
		t.Fatalf("017 %+v", f)
	}
	want := ListenerConflictEvidence{Listener: "https", Port: 443, Hostname: "*.example.com", Reason: "HostnameConflict",
		Rule: ListenerOverlapRule, Others: []ConflictingListener{{Owner: lsRef("shop", "more"), Name: "a", Hostname: "a.example.com"}}, OthersTotal: 1}
	if !reflect.DeepEqual(*ev, want) {
		t.Fatalf("evidence %+v, want %+v", *ev, want)
	}
	if !slices.Equal(f.Evidence, []objects.Ref{lsRef("shop", "more")}) {
		t.Fatalf("dependencies %v", f.Evidence)
	}
	if !strings.Contains(f.Detail, "Overlaps listener `a` of ListenerSet shop/more on port 443. Computed by the UI's listener port and hostname overlap rule.") {
		t.Fatalf("detail %q", f.Detail)
	}
}

// A conflicted ListenerSet listener names the Gateway's listener and another
// ListenerSet's; its own other listeners on the port are named too.
func TestListenerConflictOnListenerSet(t *testing.T) {
	gw := withSpecListeners(testutil.Gateway("infra", "edge", "kgateway", nil, okListener("https")),
		objects.Listener{Name: "https", Port: 443})
	mine := withSpecListeners(testutil.ListenerSet("shop", "mine", testutil.GwRef("infra", "edge"), nil, conflictedListener("x"), okListener("y")),
		objects.Listener{Name: "x", Port: 443, Hostname: "x.example.com"}, objects.Listener{Name: "y", Port: 443, Hostname: "*.example.com"})
	other := withSpecListeners(testutil.ListenerSet("infra", "other", testutil.GwRef("infra", "edge"), nil, okListener("z")),
		objects.Listener{Name: "z", Port: 443, Hostname: "x.example.com"})
	r := conflictReport(t, gw, mine, other)
	f, ev := onlyConflict(t, r, "shop/mine")
	wantOthers := []ConflictingListener{
		{Owner: gwRef("infra", "edge"), Name: "https"},
		{Owner: lsRef("infra", "other"), Name: "z", Hostname: "x.example.com"},
		{Owner: lsRef("shop", "mine"), Name: "y", Hostname: "*.example.com"},
	}
	if !reflect.DeepEqual(ev.Others, wantOthers) || ev.OthersTotal != 3 {
		t.Fatalf("others %+v total %d", ev.Others, ev.OthersTotal)
	}
	// The finding's own ListenerSet is its resource, not a dependency.
	if !slices.Equal(f.Evidence, []objects.Ref{gwRef("infra", "edge"), lsRef("infra", "other")}) {
		t.Fatalf("dependencies %v", f.Evidence)
	}
}

// Phase-3.3 brief: with no other side the evidence says so, and no KGWUI-012 is raised.
func TestListenerConflictNoOtherSide(t *testing.T) {
	gw := notValidGateway([]objects.Listener{{Name: "http", Port: 80, Hostname: "a.example.com"}, {Name: "https", Port: 443, Hostname: "b.example.com"}},
		okListener("http"), conflictedListener("https"))
	ls := withSpecListeners(testutil.ListenerSet("shop", "more", testutil.GwRef("infra", "edge"), nil, okListener("a")),
		objects.Listener{Name: "a", Port: 443, Hostname: "c.example.com"})
	r := conflictReport(t, gw, ls)
	f, ev := onlyConflict(t, r, "infra/edge")
	if len(ev.Others) != 0 || ev.OthersTotal != 0 || len(f.Evidence) != 0 {
		t.Fatalf("evidence %+v, dependencies %v", ev, f.Evidence)
	}
	if !strings.Contains(f.Detail, "The controller reports a conflict; no other listener on port 443 overlaps.") {
		t.Fatalf("detail %q", f.Detail)
	}
	if has(r, CodeComputedDisagrees, "", "") {
		t.Fatalf("a 012 for a conflict with no other side: %+v", r.Findings)
	}
}

// Review Focus 3: a wildcard overlaps its subdomains but not its apex, an
// empty hostname overlaps every hostname, and only the same port counts.
func TestListenerConflictOverlap(t *testing.T) {
	cases := []struct {
		name, mine, other string
		otherPort         int32
		want              bool
	}{
		{"wildcard and a subdomain", "*.example.com", "a.example.com", 443, true},
		{"wildcard and a deeper subdomain", "*.example.com", "a.b.example.com", 443, true},
		{"subdomain and a wildcard", "a.example.com", "*.example.com", 443, true},
		{"wildcard and its apex", "*.example.com", "example.com", 443, false},
		{"wildcard and a lookalike", "*.example.com", "badexample.com", 443, false},
		{"nested wildcards", "*.example.com", "*.a.example.com", 443, true},
		{"unrelated wildcards", "*.example.com", "*.example.org", 443, false},
		{"empty and any hostname", "", "a.example.com", 443, true},
		{"any hostname and empty", "a.example.com", "", 443, true},
		{"same hostname", "a.example.com", "a.example.com", 443, true},
		{"different hostnames", "a.example.com", "b.example.com", 443, false},
		{"same hostname, other port", "a.example.com", "a.example.com", 8443, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := notValidGateway([]objects.Listener{{Name: "https", Port: 443, Hostname: tc.mine}, {Name: "other", Port: tc.otherPort, Hostname: tc.other}},
				conflictedListener("https"), okListener("other"))
			_, ev := onlyConflict(t, conflictReport(t, gw), "infra/edge")
			if got := len(ev.Others) == 1; got != tc.want {
				t.Fatalf("overlap %v, want %v: %+v", got, tc.want, ev.Others)
			}
		})
	}
}

// Design §5.1: 017 is keyed by listener name, so two conflicted listeners on
// one Gateway get two stable ids.
func TestListenerConflictPerListener(t *testing.T) {
	gw := notValidGateway([]objects.Listener{{Name: "a", Port: 443}, {Name: "b", Port: 443}}, conflictedListener("a"), conflictedListener("b"))
	ids := func() []string {
		var out []string
		for _, f := range findingsFor(conflictReport(t, gw), CodeListenerConflicted, "infra/edge") {
			out = append(out, f.ID)
		}
		slices.Sort(out)
		return out
	}
	first := ids()
	if len(first) != 2 || first[0] == first[1] || first[0] == "" || !slices.Equal(first, ids()) {
		t.Fatalf("ids %v then %v", first, ids())
	}
}

// The evidence names at most maxConflictOthers listeners; OthersTotal and the
// dependencies cover every one.
func TestListenerConflictOthersCapped(t *testing.T) {
	spec := []objects.Listener{{Name: "https", Port: 443}}
	status := []testutil.Listener{conflictedListener("https")}
	var extra []*unstructured.Unstructured
	for i := range maxConflictOthers + 2 {
		name := string(rune('a' + i))
		extra = append(extra, withSpecListeners(testutil.ListenerSet("shop", "ls-"+name, testutil.GwRef("infra", "edge"), nil, okListener("l")),
			objects.Listener{Name: "l", Port: 443, Hostname: name + ".example.com"}))
	}
	r := conflictReport(t, notValidGateway(spec, status...), extra...)
	f, ev := onlyConflict(t, r, "infra/edge")
	if len(ev.Others) != maxConflictOthers || ev.OthersTotal != maxConflictOthers+2 || len(f.Evidence) != maxConflictOthers+2 {
		t.Fatalf("others %d, total %d, dependencies %d", len(ev.Others), ev.OthersTotal, len(f.Evidence))
	}
	if !strings.Contains(f.Detail, " and 2 more on port 443.") {
		t.Fatalf("detail %q", f.Detail)
	}
}

// A Conflicted condition that may describe an older generation raises nothing.
func TestListenerConflictStale(t *testing.T) {
	l := conflictedListener("https")
	l.Conds[2].ObservedGeneration = 0
	gw := notValidGateway([]objects.Listener{{Name: "https", Port: 443}}, l)
	gw.SetGeneration(1)
	r := conflictReport(t, gw)
	if fs := findingsFor(r, CodeListenerConflicted, "infra/edge"); len(fs) != 0 {
		t.Fatalf("017 from a stale condition: %+v", fs)
	}
}

func TestHostnamesOverlap(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"", "", true}, {"*.example.com", "A.Example.com", true}, {"*.example.com", "example.com", false},
		{"*.b.example.com", "*.example.com", true}, {"*.example.com", "*.xexample.com", false},
	} {
		if got := hostnamesOverlap(tc.a, tc.b); got != tc.want {
			t.Errorf("hostnamesOverlap(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
