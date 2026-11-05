package health

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// withSpecListeners replaces u's spec listeners with ls; the protocol defaults
// to HTTP. Every status listener supports HTTPRoute.
func withSpecListeners(u *unstructured.Unstructured, ls ...objects.Listener) *unstructured.Unstructured {
	for _, sl := range u.Object["status"].(map[string]any)["listeners"].([]any) {
		sl.(map[string]any)["supportedKinds"] = []any{map[string]any{"group": objects.GatewayAPIGroup, "kind": "HTTPRoute"}}
	}
	specLs := make([]any, 0, len(ls))
	for _, l := range ls {
		proto := l.Protocol
		if proto == "" {
			proto = "HTTP"
		}
		m := map[string]any{"name": l.Name, "port": int64(l.Port), "protocol": proto}
		if l.Hostname != "" {
			m["hostname"] = l.Hostname
		}
		specLs = append(specLs, m)
	}
	u.Object["spec"].(map[string]any)["listeners"] = specLs
	return u
}

// conflictedWith is the status kgateway's rejectConflictedListener writes:
// Conflicted=True, Accepted=False and Programmed=False, all with reason.
func conflictedWith(name, reason string) testutil.Listener {
	return testutil.Listener{Name: name, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionFalse, reason),
		testutil.Cond("Programmed", metav1.ConditionFalse, reason),
		testutil.Cond("Conflicted", metav1.ConditionTrue, reason),
	}}
}

func conflictedListener(name string) testutil.Listener {
	return conflictedWith(name, "HostnameConflict")
}

func okListener(name string) testutil.Listener {
	return testutil.Listener{Name: name, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}}
}

// notValid is Accepted=True/ListenersNotValid, as kgateway writes it on a
// Gateway or ListenerSet with a rejected listener.
func notValid() []metav1.Condition {
	return []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "ListenersNotValid"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}
}

// notValidGateway is infra/edge with a rejected listener.
func notValidGateway(spec []objects.Listener, status ...testutil.Listener) *unstructured.Unstructured {
	return withSpecListeners(testutil.Gateway("infra", "edge", "kgateway", notValid(), status...), spec...)
}

// listenerSet is ns/name on infra/edge, created age before testutil.Old;
// kgateway ranks older ListenerSets first.
func listenerSet(ns, name string, age time.Duration, cs []metav1.Condition, spec []objects.Listener, status ...testutil.Listener) *unstructured.Unstructured {
	u := withSpecListeners(testutil.ListenerSet(ns, name, testutil.GwRef("infra", "edge"), cs, status...), spec...)
	u.SetCreationTimestamp(metav1.NewTime(testutil.Old.Add(-age)))
	return u
}

// conflictReport evaluates the healthy world with Gateway gw and extra objects.
func conflictReport(t *testing.T, gw *unstructured.Unstructured, extra ...*unstructured.Unstructured) Report {
	t.Helper()
	w := healthyWorld()
	w.objs[1] = gw
	w.objs = append(w.objs, extra...)
	return w.report(t)
}

// onlyConflict returns the one 017 on res and its evidence. A conflicted
// listener's three conditions give that one finding and no 002 (D-030, R94).
func onlyConflict(t *testing.T, r Report, res string) (Finding, *ListenerConflictEvidence) {
	t.Helper()
	if fs := findingsFor(r, CodeConditionFalse, res); len(fs) != 0 {
		t.Fatalf("002 next to 017: %+v", fs)
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

// Design §7.5, R94: kgateway conflicts the later of two Gateway listeners
// with one hostname on a port. The Accepted=True/ListenersNotValid Gateway
// gets exactly one finding, the 017 keyed by the listener, naming the other side.
func TestListenerConflictBothSidesNamed(t *testing.T) {
	gw := notValidGateway([]objects.Listener{{Name: "http", Port: 80}, {Name: "first", Port: 443, Hostname: "a.example.com"},
		{Name: "https", Port: 443, Hostname: "a.example.com"}},
		okListener("http"), okListener("first"), conflictedListener("https"))
	r := conflictReport(t, gw)
	f, ev := onlyConflict(t, r, "infra/edge")
	if all := slices.DeleteFunc(slices.Clone(r.Findings), func(f Finding) bool { return f.Resource == nil || f.Resource.String() != "infra/edge" }); len(all) != 1 {
		t.Fatalf("one conflict, findings %+v", all)
	}
	if f.Severity != SeverityError || f.Slug != "listener-conflicted" || f.Message != "Listener hostname conflicts with another listener" {
		t.Fatalf("017 %+v", f)
	}
	want := ListenerConflictEvidence{Listener: "https", Port: 443, Protocol: "HTTP", Hostname: "a.example.com", Reason: "HostnameConflict",
		Rule: ListenerHostnameRule, Others: []ConflictingListener{{Owner: gwRef("infra", "edge"), Name: "first", Protocol: "HTTP", Hostname: "a.example.com"}}, OthersTotal: 1}
	if !reflect.DeepEqual(*ev, want) {
		t.Fatalf("evidence %+v, want %+v", *ev, want)
	}
	// The other side is on the finding's own Gateway, so it is no dependency.
	if len(f.Evidence) != 0 {
		t.Fatalf("dependencies %v", f.Evidence)
	}
	if !strings.Contains(f.Detail, "Same port and hostname as listener `first` of Gateway infra/edge. Computed by the UI's listener port and hostname rule.") {
		t.Fatalf("detail %q", f.Detail)
	}
}

// R77/R94: a later ListenerSet's listener names the older ListenerSet in
// another namespace with the same hostname. A ListenerSet the Gateway does not
// allow, and other hostnames, are not named.
func TestListenerConflictOnListenerSet(t *testing.T) {
	gw := withSpecListeners(testutil.Gateway("infra", "edge", "kgateway", nil, okListener("https")),
		objects.Listener{Name: "https", Port: 443, Hostname: "gw.example.com"})
	early := listenerSet("shop", "early", time.Hour, nil, []objects.Listener{{Name: "a", Port: 443, Hostname: "x.example.com"}}, okListener("a"))
	late := listenerSet("infra", "late", 0, notValid(),
		[]objects.Listener{{Name: "x", Port: 443, Hostname: "x.example.com"}, {Name: "y", Port: 443, Hostname: "*.example.com"}},
		conflictedListener("x"), okListener("y"))
	denied := listenerSet("blog", "denied", 2*time.Hour,
		[]metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowed"), testutil.Cond("Programmed", metav1.ConditionFalse, "NotAllowed")},
		[]objects.Listener{{Name: "d", Port: 443, Hostname: "x.example.com"}})
	w := healthyWorld()
	w.nss = append(w.nss, testutil.Namespace("blog", nil))
	w.objs[1] = gw
	w.objs = append(w.objs, early, late, denied)
	f, ev := onlyConflict(t, w.report(t), "infra/late")
	wantOthers := []ConflictingListener{{Owner: lsRef("shop", "early"), Name: "a", Protocol: "HTTP", Hostname: "x.example.com"}}
	if !reflect.DeepEqual(ev.Others, wantOthers) || ev.OthersTotal != 1 {
		t.Fatalf("others %+v total %d", ev.Others, ev.OthersTotal)
	}
	if !slices.Equal(f.Evidence, []objects.Ref{lsRef("shop", "early")}) {
		t.Fatalf("dependencies %v", f.Evidence)
	}
}

// R94: a ProtocolConflict names the listeners on the port with another
// protocol, whatever their hostnames, by the "listener port and protocol" rule.
func TestListenerConflictProtocol(t *testing.T) {
	gw := notValidGateway([]objects.Listener{{Name: "tcp", Port: 443, Protocol: "TCP"}, {Name: "same", Port: 443, Protocol: "HTTPS", Hostname: "b.example.com"},
		{Name: "https", Port: 443, Protocol: "HTTPS", Hostname: "a.example.com"}, {Name: "other-port", Port: 80, Protocol: "HTTP"}},
		okListener("tcp"), conflictedWith("same", "ProtocolConflict"), conflictedWith("https", "ProtocolConflict"), okListener("other-port"))
	r := conflictReport(t, gw)
	fs := findingsFor(r, CodeListenerConflicted, "infra/edge")
	if len(fs) != 2 || len(findingsFor(r, CodeConditionFalse, "infra/edge")) != 0 {
		t.Fatalf("findings %+v", r.Findings)
	}
	i := slices.IndexFunc(fs, func(f Finding) bool { return f.Facts.(*ListenerConflictEvidence).Listener == "https" })
	ev := fs[i].Facts.(*ListenerConflictEvidence)
	if ev.Rule != ListenerProtocolRule || !reflect.DeepEqual(ev.Others, []ConflictingListener{{Owner: gwRef("infra", "edge"), Name: "tcp", Protocol: "TCP"}}) {
		t.Fatalf("evidence %+v", ev)
	}
	if !strings.Contains(fs[i].Detail, "Port 443 keeps TCP: listener `tcp` of Gateway infra/edge. Computed by the UI's listener port and protocol rule.") {
		t.Fatalf("detail %q", fs[i].Detail)
	}
}

// Phase-3.3 brief: with no other side the evidence says so, and no KGWUI-012 is raised.
func TestListenerConflictNoOtherSide(t *testing.T) {
	for _, tc := range []struct{ reason, text string }{
		{"HostnameConflict", "The controller reports a conflict; no other accepted listener on port 443 has the same hostname."},
		{"ProtocolConflict", "The controller reports a conflict; no other accepted listener on port 443 uses another protocol."},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			gw := notValidGateway([]objects.Listener{{Name: "http", Port: 80, Hostname: "a.example.com"}, {Name: "https", Port: 443, Hostname: "b.example.com"}},
				okListener("http"), conflictedWith("https", tc.reason))
			ls := listenerSet("shop", "more", 0, nil, []objects.Listener{{Name: "a", Port: 443, Hostname: "c.example.com"}}, okListener("a"))
			r := conflictReport(t, gw, ls)
			f, ev := onlyConflict(t, r, "infra/edge")
			if len(ev.Others) != 0 || ev.OthersTotal != 0 || len(f.Evidence) != 0 {
				t.Fatalf("evidence %+v, dependencies %v", ev, f.Evidence)
			}
			if !strings.Contains(f.Detail, tc.text) {
				t.Fatalf("detail %q", f.Detail)
			}
			if has(r, CodeComputedDisagrees, "", "") {
				t.Fatalf("a 012 for a conflict with no other side: %+v", r.Findings)
			}
		})
	}
}

// R95: the other side is a listener kgateway kept. Of three listeners with one
// hostname, the third names only the first: the second was rejected too.
func TestListenerConflictThreeHostnames(t *testing.T) {
	gw := notValidGateway([]objects.Listener{{Name: "w", Port: 443, Hostname: "a.example.com"}, {Name: "a", Port: 443, Hostname: "a.example.com"},
		{Name: "b", Port: 443, Hostname: "a.example.com"}},
		okListener("w"), conflictedListener("a"), conflictedListener("b"))
	for _, f := range findingsFor(conflictReport(t, gw), CodeListenerConflicted, "infra/edge") {
		ev := f.Facts.(*ListenerConflictEvidence)
		if want := []ConflictingListener{{Owner: gwRef("infra", "edge"), Name: "w", Protocol: "HTTP", Hostname: "a.example.com"}}; !reflect.DeepEqual(ev.Others, want) {
			t.Fatalf("listener %s names %+v, want only w", ev.Listener, ev.Others)
		}
	}
}

// R95: with HTTP, then HTTPS, then TLS on a port, kgateway keeps HTTP and
// rejects both others; HTTPS names only HTTP, never the rejected TLS.
func TestListenerConflictProtocolKept(t *testing.T) {
	gw := notValidGateway([]objects.Listener{{Name: "http", Port: 443, Protocol: "HTTP"}, {Name: "https", Port: 443, Protocol: "HTTPS"},
		{Name: "tls", Port: 443, Protocol: "TLS"}},
		okListener("http"), conflictedWith("https", "ProtocolConflict"), conflictedWith("tls", "ProtocolConflict"))
	r := conflictReport(t, gw)
	fs := findingsFor(r, CodeListenerConflicted, "infra/edge")
	i := slices.IndexFunc(fs, func(f Finding) bool { return f.Facts.(*ListenerConflictEvidence).Listener == "https" })
	if len(fs) != 2 || i < 0 {
		t.Fatalf("findings %+v", r.Findings)
	}
	if ev := fs[i].Facts.(*ListenerConflictEvidence); !reflect.DeepEqual(ev.Others, []ConflictingListener{{Owner: gwRef("infra", "edge"), Name: "http", Protocol: "HTTP"}}) {
		t.Fatalf("https names %+v", ev.Others)
	}
	if !strings.Contains(fs[i].Detail, "Port 443 keeps HTTP: listener `http` of Gateway infra/edge.") {
		t.Fatalf("detail %q", fs[i].Detail)
	}
}

// R95: a comparator kgateway did not keep is no other side; the finding shows
// the none-text. Not kept: Accepted=False, stale status, or no supported kinds.
func TestListenerConflictRejectedComparator(t *testing.T) {
	rejected := testutil.Listener{Name: "first", Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionFalse, "UnsupportedProtocol"), testutil.Cond("Programmed", metav1.ConditionFalse, "UnsupportedProtocol")}}
	stale := okListener("first")
	stale.Conds[0].ObservedGeneration = 0
	for _, tc := range []struct {
		name  string
		first testutil.Listener
		kinds bool
	}{{"accepted false", rejected, true}, {"stale status", stale, true}, {"no supported kinds", okListener("first"), false}} {
		t.Run(tc.name, func(t *testing.T) {
			gw := notValidGateway([]objects.Listener{{Name: "first", Port: 443, Hostname: "a.example.com"}, {Name: "https", Port: 443, Hostname: "a.example.com"}},
				tc.first, conflictedListener("https"))
			if !tc.kinds {
				gw.Object["status"].(map[string]any)["listeners"].([]any)[0].(map[string]any)["supportedKinds"] = []any{}
			}
			var f Finding
			for _, g := range findingsFor(conflictReport(t, gw), CodeListenerConflicted, "infra/edge") {
				f = g
			}
			ev, _ := f.Facts.(*ListenerConflictEvidence)
			if ev == nil || len(ev.Others) != 0 || ev.OthersTotal != 0 || len(f.Evidence) != 0 ||
				!strings.Contains(f.Detail, "The controller reports a conflict; no other accepted listener on port 443 has the same hostname.") {
				t.Fatalf("017 %+v, evidence %+v", f, ev)
			}
		})
	}
}

// R94: any other reason, such as kgateway's reserved-port Invalid, computes no
// other side and adds no other-side sentence; 017 still owns all three conditions.
func TestListenerConflictOtherReason(t *testing.T) {
	gw := notValidGateway([]objects.Listener{{Name: "first", Port: 9091}, {Name: "admin", Port: 9091}},
		okListener("first"), conflictedWith("admin", "Invalid"))
	f, ev := onlyConflict(t, conflictReport(t, gw), "infra/edge")
	if ev.Rule != "" || len(ev.Others) != 0 || len(f.Evidence) != 0 || strings.Contains(f.Detail, "port 9091") || strings.Contains(f.Detail, "Computed") {
		t.Fatalf("evidence %+v, detail %q", ev, f.Detail)
	}
}

// R94: a hostname conflict's other side has the identical hostname. A wildcard
// and its subdomains are distinct listeners, and empty matches only empty.
func TestListenerConflictHostnameRule(t *testing.T) {
	cases := []struct {
		name, mine, other string
		otherPort         int32
		want              bool
	}{
		{"same hostname", "a.example.com", "a.example.com", 443, true},
		{"empty and empty", "", "", 443, true},
		{"same wildcard", "*.example.com", "*.example.com", 443, true},
		{"different hostnames", "a.example.com", "b.example.com", 443, false},
		{"wildcard and a subdomain", "*.example.com", "a.example.com", 443, false},
		{"subdomain and a wildcard", "a.example.com", "*.example.com", 443, false},
		{"empty and a hostname", "", "a.example.com", 443, false},
		{"a hostname and empty", "a.example.com", "", 443, false},
		{"same hostname, other port", "a.example.com", "a.example.com", 8443, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := notValidGateway([]objects.Listener{{Name: "other", Port: tc.otherPort, Hostname: tc.other}, {Name: "https", Port: 443, Hostname: tc.mine}},
				okListener("other"), conflictedListener("https"))
			_, ev := onlyConflict(t, conflictReport(t, gw), "infra/edge")
			if got := len(ev.Others) == 1; got != tc.want {
				t.Fatalf("other side %v, want %v: %+v", got, tc.want, ev.Others)
			}
		})
	}
}

// Design §5.1: 017 is keyed by listener name, so two conflicted listeners on
// one Gateway get two stable ids.
func TestListenerConflictPerListener(t *testing.T) {
	gw := notValidGateway([]objects.Listener{{Name: "w", Port: 443}, {Name: "a", Port: 443}, {Name: "b", Port: 443}},
		okListener("w"), conflictedListener("a"), conflictedListener("b"))
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
// dependencies cover every one. A TCP listener after many kept HTTP ones is a
// ProtocolConflict kgateway can write.
func TestListenerConflictOthersCapped(t *testing.T) {
	gw := withSpecListeners(testutil.Gateway("infra", "edge", "kgateway", nil, okListener("http")), objects.Listener{Name: "http", Port: 80})
	extra := []*unstructured.Unstructured{
		listenerSet("shop", "zz", 0, notValid(), []objects.Listener{{Name: "l", Port: 443, Protocol: "TCP"}}, conflictedWith("l", "ProtocolConflict")),
	}
	for i := range maxConflictOthers + 2 {
		name := "ls-" + string(rune('a'+i))
		extra = append(extra, listenerSet("shop", name, time.Hour, nil, []objects.Listener{{Name: "l", Port: 443}}, okListener("l")))
	}
	f, ev := onlyConflict(t, conflictReport(t, gw, extra...), "shop/zz")
	if len(ev.Others) != maxConflictOthers || ev.OthersTotal != maxConflictOthers+2 || len(f.Evidence) != maxConflictOthers+2 {
		t.Fatalf("others %d, total %d, dependencies %d", len(ev.Others), ev.OthersTotal, len(f.Evidence))
	}
	if !strings.Contains(f.Detail, "Port 443 keeps HTTP: listeners `l` of ListenerSet shop/ls-a, ") {
		t.Fatalf("detail %q", f.Detail)
	}
	if !strings.Contains(f.Detail, " and 2 more. Computed by the UI's listener port and protocol rule.") {
		t.Fatalf("detail %q", f.Detail)
	}
}

// A Conflicted condition that may describe an older generation raises no 017.
func TestListenerConflictStale(t *testing.T) {
	l := conflictedListener("https")
	l.Conds[2].ObservedGeneration = 0
	gw := notValidGateway([]objects.Listener{{Name: "https", Port: 443}}, l)
	r := conflictReport(t, gw)
	if fs := findingsFor(r, CodeListenerConflicted, "infra/edge"); len(fs) != 0 {
		t.Fatalf("017 from a stale condition: %+v", fs)
	}
}
