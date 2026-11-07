// Package ctrllogstest serves controller logs for benchmarks and scale tests:
// a ctrllogs.Source whose follow streams write marked lines at a fixed rate.
package ctrllogstest

import (
	"context"
	"io"
	"sync"
	"time"

	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/snellius/pkg/ctrllogs"
	"github.com/DuncanDoyle/snellius/pkg/install"
)

// chunk bounds the bytes a stream prepares per read.
const chunk = 64 << 10

// Source is a ctrllogs.Source over an install's running controller Pods. A
// follow stream writes at once the lines its since covers, Rate per second of
// Clock, then one line every 1/Rate until the stream ends. Line makes a
// stream's nth line, timed at; it has no newline. A previous log is never ready.
type Source struct {
	Rate  int
	Clock clock.Clock
	Line  func(n int, at time.Time) []byte

	mu      sync.Mutex
	open    map[*stream]bool
	changed chan struct{}
}

// Targets implements ctrllogs.Source.
func (*Source) Targets(in install.Resolved) []ctrllogs.Target { return ctrllogs.Kube{}.Targets(in) }

// Previous implements ctrllogs.Source.
func (*Source) Previous(context.Context, ctrllogs.Target) (io.ReadCloser, error) {
	return nil, ctrllogs.ErrNotReady
}

// Follow implements ctrllogs.Source.
func (s *Source) Follow(ctx context.Context, _ ctrllogs.Target, since time.Duration) (io.ReadCloser, error) {
	step := time.Second / time.Duration(s.Rate)
	st := &stream{src: s, ctx: ctx, step: step, next: s.Clock.Now().Add(-since).Truncate(step)}
	s.mu.Lock()
	if s.open == nil {
		s.open = map[*stream]bool{}
	}
	s.open[st] = false
	s.mu.Unlock()
	s.notify()
	return st, nil
}

// WaitIdle blocks until n streams are open and each has been read up to the
// clock's now and waits for its next line, so the reader has handled every
// line before now. It returns ctx's error if ctx ends first.
func (s *Source) WaitIdle(ctx context.Context, n int) error {
	for {
		s.mu.Lock()
		if s.changed == nil {
			s.changed = make(chan struct{})
		}
		ch, idle := s.changed, len(s.open) == n
		now := s.Clock.Now()
		for st, waiting := range s.open {
			idle = idle && waiting && st.next.After(now)
		}
		s.mu.Unlock()
		if idle {
			return nil
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// notify wakes every WaitIdle.
func (s *Source) notify() {
	s.mu.Lock()
	if s.changed != nil {
		close(s.changed)
	}
	s.changed = make(chan struct{})
	s.mu.Unlock()
}

// setWaiting records whether st waits for its next line; a closed stream stays gone.
func (s *Source) setWaiting(st *stream, waiting bool) {
	s.mu.Lock()
	if _, ok := s.open[st]; ok {
		s.open[st] = waiting
	}
	s.mu.Unlock()
	s.notify()
}

// stream is one follow stream; buf[off:] is what the next read returns.
type stream struct {
	src  *Source
	ctx  context.Context
	step time.Duration
	// next is the time of the next line; only Read writes it, WaitIdle reads it under src.mu.
	next time.Time
	n    int
	buf  []byte
	off  int
}

// Read implements io.Reader: the lines due by now, else it waits for the next one.
func (r *stream) Read(p []byte) (int, error) {
	for {
		if r.off < len(r.buf) {
			n := copy(p, r.buf[r.off:])
			r.off += n
			return n, nil
		}
		r.buf, r.off = r.buf[:0], 0
		now := r.src.Clock.Now()
		r.src.mu.Lock()
		for !r.next.After(now) && len(r.buf) < chunk {
			r.buf = append(append(r.buf, r.src.Line(r.n, r.next)...), '\n')
			r.n++
			r.next = r.next.Add(r.step)
		}
		r.src.mu.Unlock()
		if len(r.buf) > 0 {
			continue
		}
		r.src.setWaiting(r, true)
		select {
		case <-r.ctx.Done():
			return 0, io.EOF
		case <-r.src.Clock.After(r.next.Sub(now)):
		}
		r.src.setWaiting(r, false)
	}
}

// Close implements io.Closer.
func (r *stream) Close() error {
	r.src.mu.Lock()
	delete(r.src.open, r)
	r.src.mu.Unlock()
	r.src.notify()
	return nil
}
