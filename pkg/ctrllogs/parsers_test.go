package ctrllogs

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/kgw23"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/kgw24"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

var nack = objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "nack"}

const (
	cdsURL     = "type.googleapis.com/envoy.config.cluster.v3.Cluster"
	nackedName = "kube_shop_httpbin-nack_8000"
)

// ruleSets are the shipped parsers with their captures.
var ruleSets = []struct {
	name string
	p    computed.LogParser
	dir  string
}{
	{"kgateway-2.3", kgw23.New().(computed.LogParser), "../computed/testdata/kgateway-2.3/logs"},
	{"kgateway-2.4", kgw24.New().(computed.LogParser), "../computed/testdata/kgateway-2.4/logs"},
}

// capture reads a capture file, the way a stream serves it.
func capture(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(dir + "/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// parserHarness is a harness whose install reads with p and owns edge and nack.
func parserHarness(t *testing.T, src *fakeSource, p computed.LogParser) *harness {
	t.Helper()
	h := newHarness(t, src)
	h.c.cfg.Installs = func() ([]Install, bool) {
		return []Install{{Resolved: resolved(), Parser: p, Gateways: []objects.Ref{edge, nack},
			Known: func(r objects.Ref) bool { return r == nack }}}, true
	}
	return h
}

// statsNaming reports names for gw from one healthy Pod.
func statsNaming(gw objects.Ref, names ...string) func() (proxystats.Stats, StatsState) {
	s := proxystats.Stats{installID: {{Gateway: gw, Observed: true, Pods: []proxystats.Pod{{UID: "p1", Names: names}}}}}
	return func() (proxystats.Stats, StatsState) { return s, StatsReady }
}

func eventsFor(ev []Event, gw objects.Ref, typeURL string) []Event {
	var out []Event
	for _, e := range ev {
		if e.Gateway == gw && e.TypeURL == typeURL {
			out = append(out, e)
		}
	}
	return out
}

// A name equal to one in the Gateway's proxy stats is kept with its class,
// on the captured NACK lines of both versions.
func TestNameInProxyStatsKeepsNameAndClass(t *testing.T) {
	for _, rs := range ruleSets {
		t.Run(rs.name, func(t *testing.T) {
			h := parserHarness(t, newSource(pod("a", 0)), rs.p)
			h.stats = statsNaming(nack, "listener~8080", nackedName)
			h.c.Reconcile(h.ctx)
			h.serve(t, capture(t, rs.dir, "xds-error.txt"))
			ev := eventsFor(h.logs(t).Events, nack, cdsURL)
			if len(ev) != 1 || !slices.Equal(ev[0].Names, []string{nackedName}) || ev[0].Class != computed.LogClassTLS || ev[0].Marker != "xds error" {
				t.Fatalf("nack's cluster reject = %+v", ev)
			}
		})
	}
}

// A well-formed name the Gateway's proxy stats do not report is dropped, and
// the class stays: edge's proxies report no names here.
func TestUnknownNameKeepsOnlyClass(t *testing.T) {
	for _, rs := range ruleSets {
		t.Run(rs.name, func(t *testing.T) {
			h := parserHarness(t, newSource(pod("a", 0)), rs.p)
			h.stats = statsNaming(nack, nackedName)
			h.c.Reconcile(h.ctx)
			h.serve(t, capture(t, rs.dir, "xds-error.txt"))
			ev := eventsFor(h.logs(t).Events, edge, cdsURL)
			if len(ev) != 1 || ev[0].Names != nil || ev[0].Class != computed.LogClassTLS {
				t.Fatalf("edge's cluster reject = %+v", ev)
			}
		})
	}
}

// A reject text no class names reads other; a listener reject keeps no name
// even when one equals a name in proxy stats (R102).
func TestUnknownClassAndListenerRejects(t *testing.T) {
	for _, rs := range ruleSets {
		t.Run(rs.name, func(t *testing.T) {
			h := parserHarness(t, newSource(pod("a", 0)), rs.p)
			h.stats = statsNaming(edge, "listener~8080", "listener~8443", "kube_shop_d_80")
			h.c.Reconcile(h.ctx)
			h.serve(t, capture(t, rs.dir, "synthetic.txt"))
			ev := h.logs(t).Events
			lds := eventsFor(ev, edge, "type.googleapis.com/envoy.config.listener.v3.Listener")
			if len(lds) != 2 || lds[0].Names != nil || lds[1].Names != nil || lds[0].Class != computed.LogClassBind || lds[1].Class != computed.LogClassUnknownExtension {
				t.Fatalf("listener rejects = %+v", lds)
			}
			i := slices.IndexFunc(ev, func(e Event) bool { return slices.Contains(e.Names, "kube_shop_d_80") })
			if i < 0 || ev[i].Class != computed.LogClassOther {
				t.Fatalf("unclassed reject = %+v", ev)
			}
		})
	}
}

// The captured deferral and deployer lines keep the Gateway, a known object and the class.
func TestCapturedDeferralAndDeployer(t *testing.T) {
	for _, rs := range ruleSets {
		t.Run(rs.name, func(t *testing.T) {
			h := parserHarness(t, newSource(pod("a", 0)), rs.p)
			h.c.cfg.Installs = func() ([]Install, bool) {
				noparams := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "noparams"}
				badapply := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "badapply"}
				return []Install{{Resolved: resolved(), Parser: rs.p, Gateways: []objects.Ref{edge, noparams, badapply},
					Known: func(r objects.Ref) bool { return r == noparams }}}, true
			}
			h.c.Reconcile(h.ctx)
			h.serve(t, capture(t, rs.dir, "deferral.txt")+capture(t, rs.dir, "deployer.txt"))
			var got []string
			for _, e := range h.logs(t).Events {
				obj := ""
				if e.Object != nil {
					obj = e.Object.Kind + "/" + e.Object.Name
				}
				got = append(got, fmt.Sprintf("%s|%s|%s|%s", e.Marker, e.Gateway.Name, obj, e.Class))
			}
			want := []string{
				"per-client inputs not ready; deferring snapshot|edge||",
				"error handling |noparams|Gateway/noparams|parameters",
				"error handling |badapply||apply", // the Deployment is not one the UI sees
			}
			if !slices.Equal(got, want) {
				t.Fatalf("events:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

// A panic is read from the previous container only, with its top frame; the
// window's own marked lines are read too, and a window at the read's end is shorter.
func TestPanicFromThePreviousContainer(t *testing.T) {
	for _, rs := range ruleSets {
		t.Run(rs.name, func(t *testing.T) {
			src := newSource(pod("a", 1))
			h := parserHarness(t, src, rs.p)
			h.c.Reconcile(h.ctx)
			trace := capture(t, rs.dir, "panic.txt")
			xds := `{"time":"2026-11-05T09:10:00Z","level":"warn","msg":"xds error","component":"xds/envoy","gateway_name":"nack","gateway_ns":"infra","resource":"envoy.config.cluster.v3.Cluster","error":"x"}` + "\n"
			pc := testutil.Recv(t, src.prevs, "the previous read")
			ls := &liveStream{r: strings.NewReader(trace + "panic: second\n" + xds), closed: make(chan struct{})}
			pc.reply <- followReply{rc: ls}
			testutil.Recv(t, ls.closed, "the previous read to end")
			var panics []Event
			xdsSeen := false
			for _, e := range h.logs(t).Events {
				switch e.Class {
				case computed.LogClassPanic:
					panics = append(panics, e)
				case computed.LogClassOther:
					xdsSeen = e.Gateway == nack
				}
			}
			frame := "github.com/kgateway-dev/kgateway/v2/pkg/kgateway/proxy_syncer.snapshotPerClient.func3"
			nilDeref := "github.com/kgateway-dev/kgateway/v2/pkg/deployer.(*Deployer).DeployObjs"
			if len(panics) != 3 || panics[0].Frame != frame || panics[0].Marker != "panic: " || panics[1].Frame != nilDeref || panics[2].Frame != "" || !xdsSeen {
				t.Fatalf("panics = %+v, xds event in the window: %v", panics, xdsSeen)
			}
			// Each panic names the replica whose previous container logged it; other events name none.
			for _, e := range h.logs(t).Events {
				if want := map[bool]string{true: "a"}[e.Class == computed.LogClassPanic]; e.Pod != want {
					t.Fatalf("event %+v: Pod %q, want %q", e, e.Pod, want)
				}
			}
			// The same trace on the follow stream yields no panic.
			call := testutil.Recv(t, src.follows, "the follow")
			call.reply <- followReply{rc: io.NopCloser(strings.NewReader("panic: third\n" + trace))}
			testutil.Recv(t, h.waits, "the back-off")
			n := 0
			for _, e := range h.logs(t).Events {
				if e.Class == computed.LogClassPanic {
					n++
				}
			}
			if n != 3 {
				t.Fatalf("panics after the follow stream = %d, want the 3 previous ones", n)
			}
		})
	}
}

// windowParser asks for a window of want lines and records what it got.
type windowParser struct {
	fakeParser
	want int
	got  []int
	prev []bool
}

func (*windowParser) Markers() []string { return []string{"xds error", "open"} }

func (w *windowParser) Window(_ []byte, m string, previous bool) int {
	if m == "open" {
		return w.want
	}
	return 0
}

func (w *windowParser) ParseWindow(line []byte, next [][]byte, previous bool) (computed.LogFields, bool, error) {
	w.got = append(w.got, len(next))
	w.prev = append(w.prev, previous)
	return computed.LogFields{Class: computed.LogClassPanic, Frame: "main.main " + sentinel}, true, nil
}

// A window holds at most MaxLogWindow lines, the parser learns which
// container it reads, and the reader keeps a frame only from the previous one.
func TestWindowIsBounded(t *testing.T) {
	src := newSource(pod("a", 1))
	wp := &windowParser{want: 100}
	h := parserHarness(t, src, wp)
	h.c.Reconcile(h.ctx)
	pc := testutil.Recv(t, src.prevs, "the previous read")
	ls := &liveStream{r: strings.NewReader("open\n" + strings.Repeat("line\n", 20) + "open\nlast\n"), closed: make(chan struct{})}
	pc.reply <- followReply{rc: ls}
	testutil.Recv(t, ls.closed, "the previous read to end")
	h.serve(t, "open\nx\n")
	if !slices.Equal(wp.got, []int{computed.MaxLogWindow, 1, 1}) || !slices.Equal(wp.prev, []bool{true, true, false}) {
		t.Fatalf("windows = %v, previous = %v", wp.got, wp.prev)
	}
	for _, e := range h.logs(t).Events {
		if e.Frame != "" {
			t.Fatalf("a frame with a space survived: %+v", e)
		}
	}
	if n := len(h.logs(t).Events); n != 1 {
		t.Fatalf("events = %d, want the previous container's one panic", n)
	}
}

// A panic keeps a reserved slot: MaxEvents newer events do not evict it.
func TestPanicKeepsItsSlot(t *testing.T) {
	src := newSource(pod("a", 1))
	h := parserHarness(t, src, ruleSets[1].p)
	h.c.Reconcile(h.ctx)
	pc := testutil.Recv(t, src.prevs, "the previous read")
	ls := &liveStream{r: strings.NewReader(capture(t, ruleSets[1].dir, "panic.txt")), closed: make(chan struct{})}
	pc.reply <- followReply{rc: ls}
	testutil.Recv(t, ls.closed, "the previous read to end")
	var b strings.Builder
	for i := 1; i <= MaxEvents+50; i++ {
		fmt.Fprintf(&b, `{"time":"%s","msg":"xds error","component":"xds/envoy","gateway_name":"nack","gateway_ns":"infra","resource":"envoy.config.cluster.v3.Cluster","error":"x"}`+"\n",
			testutil.Now.Add(time.Hour+time.Duration(i)*time.Second).Format(time.RFC3339Nano))
	}
	h.serve(t, b.String())
	ev := h.logs(t).Events
	panics := slices.IndexFunc(ev, func(e Event) bool { return e.Class == computed.LogClassPanic })
	if len(ev) != MaxEvents || panics < 0 {
		t.Fatalf("kept %d events, panic at %d", len(ev), panics)
	}
	if last := ev[len(ev)-1].Time; !last.Equal(testutil.Now.Add(time.Hour + (MaxEvents+50)*time.Second)) {
		t.Fatalf("the newest event %v is missing", last)
	}
}

// A re-panic's indented "panic:" line is part of the open window, so the top frame below it is read.
func TestRepanicStaysInOneWindow(t *testing.T) {
	for _, rs := range ruleSets {
		t.Run(rs.name, func(t *testing.T) {
			src := newSource(pod("a", 1))
			h := parserHarness(t, src, rs.p)
			h.c.Reconcile(h.ctx)
			pc := testutil.Recv(t, src.prevs, "the previous read")
			trace := "panic: a [recovered]\n\tpanic: b\n\ngoroutine 1 [running]:\n" +
				"github.com/kgateway-dev/kgateway/v2/pkg/deployer.(*Deployer).DeployObjs(0x1, {0x2, 0x3})\n\tdeployer.go:336 +0x2a4\n"
			ls := &liveStream{r: strings.NewReader(trace), closed: make(chan struct{})}
			pc.reply <- followReply{rc: ls}
			testutil.Recv(t, ls.closed, "the previous read to end")
			ev := h.logs(t).Events
			if len(ev) != 1 || ev[0].Frame != "github.com/kgateway-dev/kgateway/v2/pkg/deployer.(*Deployer).DeployObjs" {
				t.Fatalf("events = %+v", ev)
			}
		})
	}
}

// A JSON line quoting a panic opens no window, so a real panic right after it keeps its frame.
func TestQuotedPanicOpensNoWindow(t *testing.T) {
	for _, rs := range ruleSets {
		t.Run(rs.name, func(t *testing.T) {
			src := newSource(pod("a", 1))
			h := parserHarness(t, src, rs.p)
			h.c.Reconcile(h.ctx)
			pc := testutil.Recv(t, src.prevs, "the previous read")
			body := `{"level":"error","time":"2026-11-05T09:10:00Z","msg":"Reconciler error","error":"panic: boom [recovered]"}` + "\n" +
				"panic: runtime error: invalid memory address or nil pointer dereference\n" +
				"[signal SIGSEGV: segmentation violation code=0x2 addr=0x0 pc=0x10441cbc4]\n\ngoroutine 1 [running]:\n" +
				"github.com/kgateway-dev/kgateway/v2/pkg/deployer.(*Deployer).DeployObjs(0x1?)\n\tdeployer.go:322 +0x4\n"
			ls := &liveStream{r: strings.NewReader(body), closed: make(chan struct{})}
			pc.reply <- followReply{rc: ls}
			testutil.Recv(t, ls.closed, "the previous read to end")
			ev := h.logs(t).Events
			if len(ev) != 1 || ev[0].Class != computed.LogClassPanic || ev[0].Frame != "github.com/kgateway-dev/kgateway/v2/pkg/deployer.(*Deployer).DeployObjs" {
				t.Fatalf("events = %+v", ev)
			}
		})
	}
}
