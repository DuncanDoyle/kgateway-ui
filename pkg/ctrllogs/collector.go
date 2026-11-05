// Package ctrllogs reads controller logs when the user opts in, and keeps only
// fixed fields (design §8.9, D-037). A Source finds the controller replicas
// and opens their logs; the Collector follows one stream per replica, drops
// every line without a parser marker unparsed and publishes derived events under Key.
package ctrllogs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"time"

	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

const (
	// FollowSince is how far back a replica's first stream starts.
	FollowSince = 900 * time.Second
	// PreviousLimit caps the one read of a restarted container's previous log.
	PreviousLimit = 256 << 10
	// PreviousTailLines asks for the previous log's tail, where a crash ends. The API cuts
	// the tail's end at PreviousLimit, so 500 lines fit it at up to 512 B a line.
	PreviousTailLines = 500
	// MaxFailures marked lines in a row that fail to parse make the format unrecognised.
	MaxFailures = 10
	// backoffMin and backoffMax bound the wait before a stream reconnects.
	backoffMin = time.Second
	backoffMax = 60 * time.Second
	// overlapWindow is how far before a stream's end a reconnect re-reads, rounded up to the second.
	overlapWindow = 2 * time.Second
	// maxLine is the line scanner's buffer; a longer line is dropped unparsed.
	maxLine = 64 << 10
	// firstRoundRetry is how soon a round without a model is retried.
	firstRoundRetry = time.Second
	// publishDelay batches the changes streams make into one publication.
	publishDelay = time.Second
)

// Errors a Source wraps, so the Collector can tell them apart.
var (
	// ErrDenied: the UI may not read the Pod's log.
	ErrDenied = errors.New("controller logs: access denied")
	// ErrNotReady: the container has no log to follow yet, or its Pod is gone.
	ErrNotReady = errors.New("controller logs: container not ready")
)

// Source finds an install's controller replicas and opens their logs. Kube is the default.
type Source interface {
	// Targets are the install's controller replicas.
	Targets(in install.Resolved) []Target
	// Follow streams t's current container log from since ago until ctx ends.
	Follow(ctx context.Context, t Target, since time.Duration) (io.ReadCloser, error)
	// Previous reads at most limit bytes of t's previous container log.
	Previous(ctx context.Context, t Target, limit int64) (io.ReadCloser, error)
}

// Target is one controller replica.
type Target struct {
	// ID is stable for the replica's life, such as a Pod UID.
	ID, Namespace, Name, Container string
	// Restarts is the container's restart count; each new one gets one previous read.
	Restarts int32
}

// Install is one install whose controller logs are read.
type Install struct {
	Resolved install.Resolved
	// Parser reads its kgateway version's lines; nil means no log is read.
	Parser computed.LogParser
	// Gateways are the install's Gateways; an event naming another is dropped.
	Gateways []objects.Ref
	// Known reports whether the UI sees an object a deployer error names; nil keeps none.
	Known func(objects.Ref) bool
}

// Config configures a Collector.
type Config struct {
	Source Source
	// Every is how often replicas are reconciled and the events published.
	Every time.Duration
	// Clock drives the ticker and back-off and stamps lines without a time; nil means the real clock.
	Clock clock.WithTicker
	// Installs returns the installs to read; false means no model yet.
	Installs func() ([]Install, bool)
	// Stats returns the latest proxy stats and their state; nil means unavailable.
	Stats func() (proxystats.Stats, StatsState)
}

// StatsState is how the latest proxy stats stand for the name check.
type StatsState int

// Proxy stats states. While pending, no stream starts, so the backfill meets the first scrape.
const (
	StatsUnavailable StatsState = iota
	StatsReady
	StatsPending
)

// Collector follows each controller replica's log and keeps derived events per install.
type Collector struct {
	cfg Config
	// after is the back-off wait; tests replace it.
	after func(time.Duration) <-chan time.Time
	// changed is signalled when an event or a stream state changes.
	changed chan struct{}
	// recMu serialises Reconcile; streams is only touched under it.
	recMu   sync.Mutex
	streams map[streamKey]*stream
	// mu guards installs, which the streams update.
	mu       sync.Mutex
	installs map[string]*installState
}

type streamKey struct{ install, target string }

// stream is one replica's follow stream and its previous reads.
type stream struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// prevRead is the restart count whose previous container was read.
	prevRead int32
}

// installState is one install's events and reading state.
type installState struct {
	in   Install
	ring ring
	// failures counts marked lines in a row that failed to parse.
	failures     int
	unrecognised bool
	// rounds counts the reconcile rounds that could start streams.
	rounds int
	// streams holds each running stream's access state by target ID.
	streams map[string]State
}

// New returns a Collector for cfg.
func New(cfg Config) *Collector {
	if cfg.Clock == nil {
		cfg.Clock = clock.RealClock{}
	}
	return &Collector{cfg: cfg, after: cfg.Clock.After, changed: make(chan struct{}, 1), streams: map[streamKey]*stream{}, installs: map[string]*installState{}}
}

// Run reconciles and publishes now and every interval until ctx is done,
// under ValueKey with a TTL of three intervals; a change a stream makes is
// published within publishDelay. Every stream has ended when it returns.
func (c *Collector) Run(ctx context.Context, publish func(key string, v any, fp uint64, ttl time.Duration), _ func(key string, st collect.State, reason string)) error {
	if c.cfg.Every <= 0 || c.cfg.Source == nil || c.cfg.Installs == nil {
		return errors.New("ctrllogs: the collector needs a positive interval, a Source and Installs")
	}
	defer c.stopAll()
	t := c.cfg.Clock.NewTicker(c.cfg.Every)
	defer t.Stop()
	publishNow := func() {
		v, fp := c.Value()
		publish(ValueKey, v, fp, 3*c.cfg.Every)
	}
	var pending <-chan time.Time
	for {
		var retry <-chan time.Time
		if c.Reconcile(ctx) {
			publishNow()
		} else {
			// No model or proxy stats yet: try again soon rather than a whole interval later.
			retry = c.cfg.Clock.After(min(c.cfg.Every, firstRoundRetry))
		}
	wait:
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-t.C():
				break wait
			case <-retry:
				break wait
			case <-c.changed:
				if pending == nil {
					pending = c.after(publishDelay)
				}
			case <-pending:
				pending = nil
				publishNow()
			}
		}
	}
}

// notify signals a change to Run without blocking.
func (c *Collector) notify() {
	select {
	case c.changed <- struct{}{}:
	default:
	}
}

// Reconcile starts a stream for each new replica and a previous read for
// each new restart, and stops, and waits for, the streams of replicas that
// went. It returns false when there is no model or no proxy stats value yet.
func (c *Collector) Reconcile(ctx context.Context) bool {
	ins, ok := c.cfg.Installs()
	if !ok {
		return false
	}
	c.recMu.Lock()
	defer c.recMu.Unlock()
	want := map[streamKey]Target{}
	parsers := map[string]computed.LogParser{}
	present := map[string]bool{}
	waiting := false
	if c.cfg.Stats != nil {
		_, ss := c.cfg.Stats()
		waiting = ss == StatsPending
	}
	c.mu.Lock()
	for _, in := range ins {
		id := in.Resolved.ID()
		present[id] = true
		st := c.installs[id]
		if st == nil {
			st = &installState{streams: map[string]State{}}
			c.installs[id] = st
		}
		st.in = in
		if in.Parser == nil || waiting {
			continue
		}
		st.rounds++
		parsers[id] = in.Parser
		for _, t := range c.cfg.Source.Targets(in.Resolved) {
			want[streamKey{install: id, target: t.ID}] = t
		}
	}
	c.mu.Unlock()
	if waiting {
		return false
	}
	for k, s := range c.streams {
		if _, ok := want[k]; ok {
			continue
		}
		s.cancel()
		s.wg.Wait()
		delete(c.streams, k)
		c.mu.Lock()
		if st := c.installs[k.install]; st != nil {
			delete(st.streams, k.target)
		}
		c.mu.Unlock()
	}
	c.mu.Lock()
	for id := range c.installs {
		if !present[id] {
			delete(c.installs, id)
		}
	}
	c.mu.Unlock()
	for k, t := range want {
		s := c.streams[k]
		if s == nil {
			sctx, cancel := context.WithCancel(ctx)
			s = &stream{ctx: sctx, cancel: cancel}
			c.streams[k] = s
			c.mu.Lock()
			if st := c.installs[k.install]; st != nil {
				st.streams[k.target] = StatePending
			}
			c.mu.Unlock()
			s.wg.Add(1)
			go c.follow(s, k, t, parsers[k.install])
		}
		if t.Restarts > s.prevRead {
			s.prevRead = t.Restarts
			s.wg.Add(1)
			go c.previous(s, k, t, parsers[k.install])
		}
	}
	return true
}

// Streams is how many replica streams run.
func (c *Collector) Streams() int {
	c.recMu.Lock()
	defer c.recMu.Unlock()
	return len(c.streams)
}

// stopAll stops every stream and waits for it.
func (c *Collector) stopAll() {
	c.recMu.Lock()
	defer c.recMu.Unlock()
	for k, s := range c.streams {
		s.cancel()
		s.wg.Wait()
		delete(c.streams, k)
	}
}

// Value is a copy of every install's events and state, and its fingerprint.
func (c *Collector) Value() (Logs, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(Logs, len(c.installs))
	for id, st := range c.installs {
		l := InstallLogs{State: st.state()}
		if len(st.ring.events) > 0 {
			l.Events = slices.Clone(st.ring.events)
		}
		out[id] = l
	}
	return out, fingerprint(out)
}

func (st *installState) state() State {
	if st.in.Parser == nil {
		return StateNoParser
	}
	s, connected := StateOn, false
	for _, x := range st.streams {
		switch x {
		case StateOn:
			connected = true
		case StatePending:
		default:
			s = worse(s, x)
		}
	}
	if st.unrecognised {
		s = worse(s, StateUnrecognised)
	}
	switch {
	case s != StateOn || connected:
		return s
	case st.rounds <= 1:
		return StatePending
	}
	return StateNotConnected
}

// follow streams t's log until s ends, reconnecting with back-off from 1 s
// to 60 s. A reconnect starts where the last stream ended, plus 1 s of overlap.
func (c *Collector) follow(s *stream, k streamKey, t Target, p computed.LogParser) {
	defer s.wg.Done()
	from := c.cfg.Clock.Now().Add(-FollowSince)
	// ended is when the previous connection ended; zero before the first.
	var ended time.Time
	wait := backoffMin
	for {
		since := min(c.cfg.Clock.Since(from)+time.Second, FollowSince)
		rc, err := c.cfg.Source.Follow(s.ctx, t, since)
		if err == nil {
			c.setStream(k, StateOn)
			// A read error ends this stream like EOF; the reconnect covers the gap.
			n, _ := c.read(k, p, rc, ended)
			_ = rc.Close()
			from = c.cfg.Clock.Now()
			ended = from
			if n > 0 {
				wait = backoffMin
			}
		}
		if s.ctx.Err() != nil {
			return
		}
		switch {
		case errors.Is(err, ErrDenied):
			c.setStream(k, StateDenied)
		case err != nil && !errors.Is(err, ErrNotReady):
			c.setStream(k, StateFailing)
		}
		select {
		case <-s.ctx.Done():
			return
		case <-c.after(wait):
		}
		wait = min(2*wait, backoffMax)
	}
}

// previous reads t's previous container log once, at most PreviousLimit bytes.
func (c *Collector) previous(s *stream, k streamKey, t Target, p computed.LogParser) {
	defer s.wg.Done()
	rc, err := c.cfg.Source.Previous(s.ctx, t, PreviousLimit)
	if err != nil {
		return // the follow stream reports access
	}
	defer func() { _ = rc.Close() }()
	_, _ = c.read(k, p, io.LimitReader(rc, PreviousLimit), time.Time{})
}

// read hands each line of r with one of p's markers to handle. A marked line
// over maxLine counts as a parse failure, unread past the scanner's buffer; a
// last line without a newline, as a capped read ends, is dropped. ended is
// when the stream's previous connection ended, zero for none. It returns the bytes read.
func (c *Collector) read(k streamKey, p computed.LogParser, r io.Reader, ended time.Time) (int64, error) {
	markers := p.Markers()
	br := bufio.NewReaderSize(r, maxLine)
	var n int64
	for {
		line, err := br.ReadSlice('\n')
		n += int64(len(line))
		if errors.Is(err, bufio.ErrBufferFull) {
			// tail keeps a chunk's last bytes, so a marker across two fills is found.
			span := maxMarker(markers)
			marked := marker(line, markers) != ""
			tail := append([]byte(nil), last(line, span)...)
			for errors.Is(err, bufio.ErrBufferFull) {
				line, err = br.ReadSlice('\n')
				n += int64(len(line))
				if !marked {
					marked = marker(line, markers) != "" || marker(append(tail, head(line, span)...), markers) != ""
					tail = append(tail[:0], last(line, span)...)
				}
			}
			if err != nil {
				return n, eof(err)
			}
			if marked {
				c.failure(k)
			}
			continue
		}
		if err != nil {
			return n, eof(err)
		}
		if m := marker(line, markers); m != "" {
			c.handle(k, p, m, bytes.TrimRight(line, "\r\n"), ended)
		}
	}
}

// maxMarker is one less than the longest marker's length: the bytes a marker can span across two chunks.
func maxMarker(markers []string) int {
	n := 0
	for _, m := range markers {
		n = max(n, len(m)-1)
	}
	return n
}

func head(b []byte, n int) []byte { return b[:min(n, len(b))] }
func last(b []byte, n int) []byte { return b[len(b)-min(n, len(b)):] }

func eof(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// marker returns the first of markers in line, or "".
func marker(line []byte, markers []string) string {
	for _, m := range markers {
		if m != "" && bytes.Contains(line, []byte(m)) {
			return m
		}
	}
	return ""
}

// handle parses one marked line and keeps its event.
func (c *Collector) handle(k streamKey, p computed.LogParser, m string, line []byte, ended time.Time) {
	f, ok, err := p.Parse(line)
	var stats proxystats.Stats
	statsOK := false
	if err == nil && ok && len(f.Names) > 0 && c.cfg.Stats != nil {
		var ss StatsState
		stats, ss = c.cfg.Stats()
		statsOK = ss == StatsReady
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.installs[k.install]
	switch {
	case st == nil:
		return
	case err != nil:
		c.failLocked(st)
		return
	case !ok:
		return
	}
	if st.unrecognised {
		c.notify()
	}
	st.failures, st.unrecognised = 0, false
	e, keep := c.event(k.install, st, f, m, stats, statsOK)
	if !keep {
		return
	}
	stamped := f.Time.IsZero()
	// A line without a time re-read in the reconnect overlap matches the one stamped before the end.
	if stamped && !ended.IsZero() && st.ring.absorb(e, ended.Add(-overlapWindow), ended) {
		return
	}
	if st.ring.add(e, stamped) {
		c.notify()
	}
}

// failure counts an unreadable marked line.
func (c *Collector) failure(k streamKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st := c.installs[k.install]; st != nil {
		c.failLocked(st)
	}
}

func (c *Collector) failLocked(st *installState) {
	if st.failures++; st.failures >= MaxFailures && !st.unrecognised {
		st.unrecognised = true
		c.notify()
	}
}

// event keeps only f's fields that are in a closed set: a Gateway of the
// install, a known type URL, class and object, and names in that Gateway's
// proxy stats. A line naming another Gateway is dropped.
func (c *Collector) event(id string, st *installState, f computed.LogFields, m string, stats proxystats.Stats, statsOK bool) (Event, bool) {
	e := Event{Time: f.Time.UTC(), Marker: m, Class: f.Class}
	if f.Time.IsZero() {
		e.Time = c.cfg.Clock.Now().UTC()
	}
	if e.Class != "" && !computed.KnownLogClass(e.Class) {
		e.Class = computed.LogClassOther
	}
	if typeURLs[f.TypeURL] {
		e.TypeURL = f.TypeURL
	}
	if f.Object != nil && st.in.Known != nil && st.in.Known(*f.Object) {
		o := *f.Object
		e.Object = &o
	}
	if f.Gateway == (objects.Ref{}) {
		return e, true
	}
	i := slices.IndexFunc(st.in.Gateways, func(g objects.Ref) bool { return g.Namespace == f.Gateway.Namespace && g.Name == f.Gateway.Name })
	if i < 0 {
		return Event{}, false
	}
	e.Gateway = st.in.Gateways[i]
	if statsOK {
		e.Names = knownNames(stats[id], e.Gateway, f.Names)
	}
	return e, true
}

// knownNames are the names of cand that one of gw's Pods reports in proxy stats, sorted.
func knownNames(gws []proxystats.Gateway, gw objects.Ref, cand []string) []string {
	var out []string
	for _, g := range gws {
		if g.Gateway != gw {
			continue
		}
		for _, p := range g.Pods {
			if p.Err != "" {
				continue
			}
			for _, n := range cand {
				if slices.Contains(p.Names, n) {
					out = append(out, n)
				}
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// setStream records a stream's access state.
func (c *Collector) setStream(k streamKey, s State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st := c.installs[k.install]; st != nil && st.streams[k.target] != s {
		st.streams[k.target] = s
		c.notify()
	}
}
