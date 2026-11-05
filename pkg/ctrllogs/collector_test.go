package ctrllogs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

const sentinel = "AKIAFAKEKEY123456"

var (
	edge  = objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}
	other = objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "other"}
)

// fakeParser reads "xds error" lines of space-separated key=value fields:
// t (Unix seconds), gw (ns/name), type, names (comma-separated), class, obj
// (Kind/ns/name). A line with "bad" fails; one with "skip" is not its own.
type fakeParser struct {
	mu    sync.Mutex
	calls int
}

func (*fakeParser) Markers() []string { return []string{"xds error"} }

func (p *fakeParser) Parse(line []byte) (computed.LogFields, bool, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	s := string(line)
	switch {
	case strings.Contains(s, "bad"):
		return computed.LogFields{}, false, errors.New("unreadable")
	case strings.Contains(s, "skip"):
		return computed.LogFields{}, false, nil
	}
	var f computed.LogFields
	for _, kv := range strings.Fields(s) {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "t":
			n, _ := strconv.ParseInt(v, 10, 64)
			f.Time = testutil.Now.Add(time.Duration(n) * time.Second)
		case "gw":
			ns, name, _ := strings.Cut(v, "/")
			f.Gateway = objects.Ref{Namespace: ns, Name: name}
		case "type":
			f.TypeURL = v
		case "names":
			f.Names = strings.Split(v, ",")
		case "class":
			f.Class = v
		case "obj":
			parts := strings.SplitN(v, "/", 3)
			f.Object = &objects.Ref{Kind: parts[0], Namespace: parts[1], Name: parts[2]}
		}
	}
	return f, true, nil
}

func (p *fakeParser) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type followCall struct {
	t     Target
	since time.Duration
	ctx   context.Context
	reply chan followReply
}

type followReply struct {
	rc  io.ReadCloser
	err error
}

type prevCall struct {
	t     Target
	limit int64
	reply chan followReply
}

// fakeSource hands every Follow and Previous call to the test, which replies.
type fakeSource struct {
	mu      sync.Mutex
	targets []Target
	follows chan followCall
	prevs   chan prevCall
}

func newSource(ts ...Target) *fakeSource {
	return &fakeSource{targets: ts, follows: make(chan followCall, 8), prevs: make(chan prevCall, 8)}
}

func (s *fakeSource) set(ts ...Target) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targets = ts
}

func (s *fakeSource) Targets(install.Resolved) []Target {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.targets)
}

func (s *fakeSource) Follow(ctx context.Context, t Target, since time.Duration) (io.ReadCloser, error) {
	c := followCall{t: t, since: since, ctx: ctx, reply: make(chan followReply, 1)}
	s.follows <- c
	select {
	case r := <-c.reply:
		return r.rc, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *fakeSource) Previous(ctx context.Context, t Target, limit int64) (io.ReadCloser, error) {
	c := prevCall{t: t, limit: limit, reply: make(chan followReply, 1)}
	s.prevs <- c
	select {
	case r := <-c.reply:
		return r.rc, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// liveStream serves its lines, then, with a context, blocks like a follow
// stream until it ends; closed is closed by Close.
type liveStream struct {
	r   io.Reader
	ctx context.Context
	// blocked is closed once every line was read and the stream waits.
	blocked, closed chan struct{}
	bonce, once     sync.Once
	read            int64
}

func live(ctx context.Context, body string) *liveStream {
	return &liveStream{r: strings.NewReader(body), ctx: ctx, blocked: make(chan struct{}), closed: make(chan struct{})}
}

func (l *liveStream) Read(b []byte) (int, error) {
	n, err := l.r.Read(b)
	l.read += int64(n)
	if errors.Is(err, io.EOF) && l.ctx != nil {
		l.bonce.Do(func() { close(l.blocked) })
		<-l.ctx.Done()
		return n, l.ctx.Err()
	}
	return n, err
}

func (l *liveStream) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

type wait struct {
	d  time.Duration
	ch chan time.Time
}

type harness struct {
	c      *Collector
	src    *fakeSource
	parser *fakeParser
	waits  chan wait
	ctx    context.Context
	stats  func() (proxystats.Stats, bool)
}

const installID = "kgateway-system.kgateway"

func resolved() install.Resolved {
	return install.Resolved{Install: install.Install{Namespace: "kgateway-system", Name: "kgateway", Container: "kgateway"}}
}

func newHarness(t *testing.T, src *fakeSource) *harness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{src: src, parser: &fakeParser{}, waits: make(chan wait, 8), ctx: ctx}
	h.stats = func() (proxystats.Stats, bool) { return nil, false }
	h.c = New(Config{Source: src, Every: 10 * time.Second, Clock: testutil.FakeClock(),
		Installs: func() ([]Install, bool) {
			return []Install{{Resolved: resolved(), Parser: h.parser, Gateways: []objects.Ref{edge},
				Known: func(r objects.Ref) bool {
					return r == objects.Ref{Kind: "Deployment", Namespace: "infra", Name: "edge"}
				}}}, true
		},
		Stats: func() (proxystats.Stats, bool) { return h.stats() }})
	h.c.after = func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		h.waits <- wait{d: d, ch: ch}
		return ch
	}
	t.Cleanup(func() {
		cancel()
		h.c.stopAll()
	})
	return h
}

func (h *harness) logs(t *testing.T) InstallLogs {
	t.Helper()
	v, _ := h.c.Value()
	return v[installID]
}

// serve answers the next Follow with body, which then ends, and waits for the back-off.
func (h *harness) serve(t *testing.T, body string) (followCall, wait) {
	t.Helper()
	call := testutil.Recv(t, h.src.follows, "a follow")
	call.reply <- followReply{rc: io.NopCloser(strings.NewReader(body))}
	return call, testutil.Recv(t, h.waits, "the back-off")
}

func pod(id string, restarts int32) Target {
	return Target{ID: id, Namespace: "kgateway-system", Name: id, Container: "kgateway", Restarts: restarts}
}

// A line without a marker is never parsed, short or 1 MiB, and a line over
// the scanner's buffer is dropped even with a marker; the next line still reads.
func TestPrefilterDropsUnmarkedLinesUnparsed(t *testing.T) {
	h := newHarness(t, newSource(pod("a", 0)))
	h.c.Reconcile(h.ctx)
	huge := strings.Repeat("x", 1<<20)
	h.serve(t, huge+"\n"+huge+" xds error gw=infra/edge t=1\ninfo gw=infra/edge t=3\nxds error gw=infra/edge t=2\n")
	if n := h.parser.Calls(); n != 1 {
		t.Fatalf("parsed %d lines, want only the short marked one", n)
	}
	if ev := h.logs(t).Events; len(ev) != 1 || !ev[0].Time.Equal(testutil.Now.Add(2*time.Second)) || ev[0].Marker != "xds error" {
		t.Fatalf("events = %+v", ev)
	}
}

// The first stream starts 15 min back; failures back off from 1 s to 60 s,
// and a stream that delivered lines resets it. A reconnect starts where the
// last stream ended, with 1 s of overlap whose repeated lines are dropped.
func TestReconnectBacksOff(t *testing.T) {
	h := newHarness(t, newSource(pod("a", 0)))
	h.c.Reconcile(h.ctx)
	var waits []time.Duration
	for i := range 8 {
		call := testutil.Recv(t, h.src.follows, "a follow")
		if call.since != FollowSince {
			t.Fatalf("follow %d since %v, want %v before any stream", i, call.since, FollowSince)
		}
		call.reply <- followReply{err: errors.New("connection refused")}
		w := testutil.Recv(t, h.waits, "the back-off")
		if i == 0 {
			if st := h.logs(t).State; st != StateFailing {
				t.Fatalf("state after a failed follow = %q", st)
			}
		}
		waits = append(waits, w.d)
		w.ch <- time.Time{}
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, 60 * time.Second, 60 * time.Second}
	if !slices.Equal(waits, want) {
		t.Fatalf("back-off = %v, want %v", waits, want)
	}
	line := "xds error gw=infra/edge t=1\n"
	_, w := h.serve(t, line)
	if w.d != time.Second {
		t.Fatalf("back-off after a stream with lines = %v, want 1s", w.d)
	}
	if st := h.logs(t).State; st != StateOn {
		t.Fatalf("state after a stream = %q", st)
	}
	w.ch <- time.Time{}
	call, _ := h.serve(t, line)
	if call.since != time.Second {
		t.Fatalf("reconnect since %v, want 1s of overlap", call.since)
	}
	if n := len(h.logs(t).Events); n != 1 {
		t.Fatalf("a re-read line counted %d times", n)
	}
}

// Two replicas each follow their own stream; the same NACK from both is one
// event. A Pod that goes ends its stream; a replacement starts its own.
func TestStreamEndsWhenPodGoes(t *testing.T) {
	src := newSource(pod("a", 0), pod("b", 0))
	h := newHarness(t, src)
	h.c.Reconcile(h.ctx)
	streams := map[string]*liveStream{}
	for range 2 {
		call := testutil.Recv(t, src.follows, "a follow")
		if call.since != FollowSince {
			t.Fatalf("since = %v", call.since)
		}
		ls := live(call.ctx, "xds error gw=infra/edge t=5 type=type.googleapis.com/envoy.config.listener.v3.Listener\n")
		streams[call.t.ID] = ls
		call.reply <- followReply{rc: ls}
	}
	for _, ls := range streams {
		testutil.Recv(t, ls.blocked, "a stream to read its line")
	}
	if len(streams) != 2 || h.c.Streams() != 2 {
		t.Fatalf("want one stream per replica, got %d", h.c.Streams())
	}
	src.set(pod("b", 0), pod("c", 0))
	h.c.Reconcile(h.ctx)
	select {
	case <-streams["a"].closed:
	default:
		t.Fatal("the deleted Pod's stream is still open")
	}
	select {
	case <-streams["b"].closed:
		t.Fatal("the remaining Pod's stream was closed")
	default:
	}
	if call := testutil.Recv(t, src.follows, "the replacement's follow"); call.t.ID != "c" {
		t.Fatalf("follow for %q, want c", call.t.ID)
	} else {
		call.reply <- followReply{rc: live(call.ctx, "")}
	}
	if n := h.c.Streams(); n != 2 {
		t.Fatalf("streams = %d, want 2", n)
	}
	src.set()
	h.c.Reconcile(h.ctx)
	if n := h.c.Streams(); n != 0 {
		t.Fatalf("streams after every Pod went = %d", n)
	}
	if ev := h.logs(t).Events; len(ev) != 1 {
		t.Fatalf("the same NACK from two replicas gave %d events", len(ev))
	}
}

// A restart gets one read of the previous container, at most 256 KiB, and
// never a second read for the same restart.
func TestPreviousReadIsCappedAndOnce(t *testing.T) {
	src := newSource(pod("a", 1))
	h := newHarness(t, src)
	h.c.Reconcile(h.ctx)
	var b strings.Builder
	for i := 0; b.Len() < 1<<20; i++ {
		fmt.Fprintf(&b, "xds error gw=infra/edge t=%d %s\n", i, strings.Repeat("p", 1200))
	}
	pc := testutil.Recv(t, src.prevs, "the previous read")
	if pc.limit != PreviousLimit || PreviousLimit != 256<<10 {
		t.Fatalf("previous limit = %d", pc.limit)
	}
	ls := &liveStream{r: strings.NewReader(b.String()), closed: make(chan struct{})} // ends at EOF
	pc.reply <- followReply{rc: ls}
	testutil.Recv(t, ls.closed, "the previous read to end")
	if ls.read > PreviousLimit {
		t.Fatalf("read %d bytes of the previous log, cap %d", ls.read, PreviousLimit)
	}
	lineLen := len(fmt.Sprintf("xds error gw=infra/edge t=%d %s\n", 0, strings.Repeat("p", 1200)))
	if n, max := len(h.logs(t).Events), PreviousLimit/lineLen+1; n == 0 || n > max {
		t.Fatalf("events from the previous log = %d, want 1..%d", n, max)
	}
	call := testutil.Recv(t, src.follows, "the follow")
	call.reply <- followReply{rc: live(call.ctx, "")}
	h.c.Reconcile(h.ctx)
	select {
	case <-src.prevs:
		t.Fatal("a second previous read for the same restart")
	default:
	}
	src.set(pod("a", 2))
	h.c.Reconcile(h.ctx)
	pc = testutil.Recv(t, src.prevs, "the next restart's previous read")
	pc.reply <- followReply{err: ErrNotReady}
}

// An install keeps at most MaxEvents, in time order; the oldest go, and an
// event older than every kept one is not added.
func TestEventCap(t *testing.T) {
	h := newHarness(t, newSource(pod("a", 0)))
	h.c.Reconcile(h.ctx)
	var b strings.Builder
	for i := 1; i <= MaxEvents+200; i++ {
		fmt.Fprintf(&b, "xds error gw=infra/edge t=%d\n", i)
	}
	b.WriteString("xds error gw=infra/edge t=0\n")
	h.serve(t, b.String())
	ev := h.logs(t).Events
	if len(ev) != MaxEvents {
		t.Fatalf("kept %d events, cap %d", len(ev), MaxEvents)
	}
	if first, last := ev[0].Time, ev[len(ev)-1].Time; !first.Equal(testutil.Now.Add(201*time.Second)) || !last.Equal(testutil.Now.Add(1200*time.Second)) {
		t.Fatalf("kept %v..%v, want the newest %d", first, last, MaxEvents)
	}
}

// 10 marked lines in a row that fail to parse make the format unrecognised;
// lines the parser does not read do not count, and a parsed line clears it.
func TestTenParseFailuresMarkFormatUnrecognised(t *testing.T) {
	h := newHarness(t, newSource(pod("a", 0)))
	h.c.Reconcile(h.ctx)
	nine := strings.Repeat("xds error bad\nxds error skip\n", MaxFailures-1)
	_, w := h.serve(t, nine)
	if st := h.logs(t).State; st != StateOn {
		t.Fatalf("after 9 failures: %q", st)
	}
	w.ch <- time.Time{}
	_, w = h.serve(t, "xds error bad\n")
	if st := h.logs(t).State; st != StateUnrecognised {
		t.Fatalf("after 10 failures: %q", st)
	}
	w.ch <- time.Time{}
	h.serve(t, "xds error gw=infra/edge t=1\n")
	if st := h.logs(t).State; st != StateOn {
		t.Fatalf("after a parsed line: %q", st)
	}
}

// A follow the API forbids, as when the Role is missing, reads denied.
func TestRoleMissingReadsDenied(t *testing.T) {
	h := newHarness(t, newSource(pod("a", 0)))
	h.c.Reconcile(h.ctx)
	call := testutil.Recv(t, h.src.follows, "a follow")
	call.reply <- followReply{err: classify(apierrors.NewForbidden(schema.GroupResource{Resource: "pods/log"}, "a", errors.New(sentinel)))}
	testutil.Recv(t, h.waits, "the back-off")
	v, fp := h.c.Value()
	if v[installID].State != StateDenied {
		t.Fatalf("state = %q", v[installID].State)
	}
	store := collect.New(nil, func() time.Time { return testutil.Now })
	store.Publish(Key, v, fp, 0)
	if st, ok := Availability(store.Snapshot()); !ok || st != StateDenied {
		t.Fatalf("availability = %q, %v", st, ok)
	}
}

// Only closed-set values survive: a Gateway of the install, a known type URL,
// class and object, and names in that Gateway's proxy stats; a line naming
// another Gateway is dropped, and without proxy stats no name is kept.
func TestEventKeepsOnlyClosedSetFields(t *testing.T) {
	h := newHarness(t, newSource(pod("a", 0)))
	h.c.Reconcile(h.ctx)
	stats := proxystats.Stats{installID: {
		{Gateway: edge, Observed: true, Pods: []proxystats.Pod{{UID: "p1", RDS: []string{"listener~80"}}, {UID: "p2", Err: "timed out", Clusters: []string{"kube_x"}}}},
		{Gateway: other, Observed: true, Pods: []proxystats.Pod{{UID: "p3", Clusters: []string{"kube_other"}}}},
	}}
	h.stats = func() (proxystats.Stats, bool) { return stats, true }
	body := "xds error gw=infra/edge t=1 type=type.googleapis.com/envoy.config.route.v3.RouteConfiguration names=" + sentinel + ",listener~80,kube_x,kube_other class=" + sentinel + " obj=Secret/infra/" + sentinel + "\n" +
		"xds error gw=infra/" + sentinel + " t=2 class=panic\n" +
		"xds error t=3 type=" + sentinel + " class=panic obj=Deployment/infra/edge\n"
	_, w := h.serve(t, body)
	ev := h.logs(t).Events
	if len(ev) != 2 {
		t.Fatalf("events = %+v", ev)
	}
	want := Event{Time: testutil.Now.Add(time.Second), Marker: "xds error", Gateway: edge, TypeURL: "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
		Names: []string{"listener~80"}, Class: computed.LogClassOther}
	if got := ev[0]; got.Object != nil || got.Time != want.Time || got.Gateway != want.Gateway || got.TypeURL != want.TypeURL || !slices.Equal(got.Names, want.Names) || got.Class != want.Class {
		t.Fatalf("event = %+v, want %+v", got, want)
	}
	if got := ev[1]; got.TypeURL != "" || got.Class != computed.LogClassPanic || got.Object == nil || *got.Object != (objects.Ref{Kind: "Deployment", Namespace: "infra", Name: "edge"}) {
		t.Fatalf("event = %+v", got)
	}
	if s := fmt.Sprintf("%+v", ev); strings.Contains(s, sentinel) {
		t.Fatalf("the sentinel survived: %s", s)
	}
	h.stats = func() (proxystats.Stats, bool) { return nil, false }
	w.ch <- time.Time{}
	h.serve(t, "xds error gw=infra/edge t=9 names=listener~80\n")
	if ev := h.logs(t).Events; ev[len(ev)-1].Names != nil {
		t.Fatalf("names kept without proxy stats: %+v", ev[len(ev)-1])
	}
}

// The capability is the worst state of the installs that read logs.
func TestAvailability(t *testing.T) {
	now := testutil.Now
	store := collect.New(nil, func() time.Time { return now })
	if _, ok := Availability(store.Snapshot()); ok {
		t.Fatal("a state before the first publication")
	}
	for _, tc := range []struct {
		logs Logs
		want State
	}{
		{Logs{}, StateOn},
		{Logs{"a": {State: StateNoParser}}, StateNoParser},
		{Logs{"a": {State: StateNoParser}, "b": {State: StateOn}}, StateOn},
		{Logs{"a": {State: StateFailing}, "b": {State: StateUnrecognised}}, StateUnrecognised},
		{Logs{"a": {State: StateDenied}, "b": {State: StateUnrecognised}}, StateDenied},
	} {
		store.Publish(Key, tc.logs, fingerprint(tc.logs), time.Second)
		if st, _ := Availability(store.Snapshot()); st != tc.want {
			t.Errorf("%v: %q, want %q", tc.logs, st, tc.want)
		}
	}
	now = now.Add(time.Second)
	if st, _ := Availability(store.Snapshot()); st != StateFailing {
		t.Errorf("an expired value: %q", st)
	}
}

// An install whose rule set has no parser starts no stream.
func TestNoParserNoReader(t *testing.T) {
	src := newSource(pod("a", 0))
	c := New(Config{Source: src, Every: time.Second, Clock: testutil.FakeClock(),
		Installs: func() ([]Install, bool) { return []Install{{Resolved: resolved()}}, true }})
	c.Reconcile(context.Background())
	if n := c.Streams(); n != 0 {
		t.Fatalf("streams = %d", n)
	}
	if v, _ := c.Value(); v[installID].State != StateNoParser {
		t.Fatalf("state = %q", v[installID].State)
	}
}

// Run waits for a model, publishes under ValueKey, publishes a stream's
// change after publishDelay, and leaves no stream when it returns.
func TestRunPublishesAndStopsStreams(t *testing.T) {
	src := newSource(pod("a", 0))
	ready := make(chan bool, 1)
	h := newHarness(t, src)
	c := New(Config{Source: src, Every: time.Hour, Clock: testutil.FakeClock(),
		Installs: func() ([]Install, bool) {
			select {
			case <-ready:
				ready <- true
				return []Install{{Resolved: resolved(), Parser: &fakeParser{}}}, true
			default:
				return nil, false
			}
		}})
	c.after = h.c.after
	type pub struct {
		key  string
		logs Logs
		ttl  time.Duration
	}
	pubs := make(chan pub, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	ready <- true
	go func() {
		done <- c.Run(ctx, func(k string, v any, _ uint64, ttl time.Duration) { pubs <- pub{k, v.(Logs), ttl} }, nil)
	}()
	if p := testutil.Recv(t, pubs, "the first publication"); p.key != ValueKey || p.ttl != 3*time.Hour || p.logs[installID].State != StateOn {
		t.Fatalf("published %+v", p)
	}
	call := testutil.Recv(t, src.follows, "a follow")
	call.reply <- followReply{err: ErrDenied}
	for range 2 { // the stream's back-off and the publication delay, both 1 s
		w := testutil.Recv(t, h.waits, "a wait")
		if w.d != publishDelay {
			t.Fatalf("wait %v", w.d)
		}
		w.ch <- time.Time{}
	}
	if p := testutil.Recv(t, pubs, "the change's publication"); p.logs[installID].State != StateDenied {
		t.Fatalf("published %+v", p)
	}
	call = testutil.Recv(t, src.follows, "the reconnect")
	ls := live(call.ctx, "")
	call.reply <- followReply{rc: ls}
	cancel()
	if err := testutil.Recv(t, done, "Run to return"); err != nil {
		t.Fatal(err)
	}
	if n := c.Streams(); n != 0 {
		t.Fatalf("streams after Run = %d", n)
	}
}

// Kube follows running controller Pods' container from since ago, reads the
// previous container with a byte limit, and tells denied from not ready.
func TestKubeSource(t *testing.T) {
	running := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "kgateway-system", Name: "a", UID: "uid-a"},
		Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "kgateway"}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "kgateway", RestartCount: 3}}}}
	pending := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "kgateway-system", Name: "b", UID: "uid-b"}, Status: corev1.PodStatus{Phase: corev1.PodPending}}
	in := resolved()
	in.Pods = []*corev1.Pod{running, pending}
	kube := kubefake.NewClientset()
	src := Kube{Client: kube}
	ts := src.Targets(in)
	if want := []Target{{ID: "uid-a", Namespace: "kgateway-system", Name: "a", Container: "kgateway", Restarts: 3}}; !slices.Equal(ts, want) {
		t.Fatalf("targets = %+v", ts)
	}
	rc, err := src.Follow(context.Background(), ts[0], 900*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = rc.Close()
	rc, err = src.Previous(context.Background(), ts[0], PreviousLimit)
	if err != nil {
		t.Fatal(err)
	}
	_ = rc.Close()
	var opts []*corev1.PodLogOptions
	for _, a := range kube.Actions() {
		if g, ok := a.(k8stesting.GenericAction); ok && a.GetSubresource() == "log" {
			opts = append(opts, g.GetValue().(*corev1.PodLogOptions))
		}
	}
	if len(opts) != 2 {
		t.Fatalf("log requests = %d", len(opts))
	}
	if o := opts[0]; !o.Follow || o.Container != "kgateway" || o.SinceSeconds == nil || *o.SinceSeconds != 900 || o.Previous {
		t.Errorf("follow options = %+v", o)
	}
	if o := opts[1]; !o.Previous || o.Follow || o.LimitBytes == nil || *o.LimitBytes != PreviousLimit {
		t.Errorf("previous options = %+v", o)
	}
	kube.PrependReactor("get", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods/log"}, "a", errors.New(sentinel))
	})
	if _, err := src.Follow(context.Background(), ts[0], time.Second); !errors.Is(err, ErrDenied) || strings.Contains(err.Error(), sentinel) {
		t.Errorf("a forbidden follow: %v", err)
	}
	gr := schema.GroupResource{Resource: "pods/log"}
	if err := classify(apierrors.NewForbidden(gr, "a", errors.New(sentinel))); !errors.Is(err, ErrDenied) || strings.Contains(err.Error(), sentinel) {
		t.Errorf("forbidden: %v", err)
	}
	if err := classify(apierrors.NewBadRequest(sentinel)); !errors.Is(err, ErrNotReady) || strings.Contains(err.Error(), sentinel) {
		t.Errorf("bad request: %v", err)
	}
	if err := classify(apierrors.NewInternalError(errors.New(sentinel))); errors.Is(err, ErrDenied) || errors.Is(err, ErrNotReady) || strings.Contains(err.Error(), sentinel) {
		t.Errorf("internal: %v", err)
	}
}
