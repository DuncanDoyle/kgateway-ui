package endpoints

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"

	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// Bounds limit the reads behind a ScopedReader process-wide (design §9.3).
// InFlight is the most reads at once; MemoServices, MemoBytes and TTL bound
// the memo. MaxEndpoints, PageSize and MaxPages bound one read; Bounded also cuts a longer result to MaxEndpoints.
type Bounds struct {
	InFlight, MemoServices, MemoBytes, MaxEndpoints, PageSize, MaxPages int
	Timeout, TTL                                                        time.Duration
}

// DefaultBounds are the design's bounds.
var DefaultBounds = Bounds{InFlight: 4, MemoServices: 32, MemoBytes: 1 << 20, MaxEndpoints: 1000, PageSize: 10, MaxPages: 10,
	Timeout: 2 * time.Second, TTL: 5 * time.Second}

// Bounded wraps r: duplicate requests share one read, at most b.InFlight
// reads run (a further one is Busy), a read is TimedOut after b.Timeout, and
// complete and partial results are remembered for b.TTL, least recently used
// first, within b.MemoServices and b.MemoBytes. Only Service refs are read; others are Unavailable.
func Bounded(r ScopedReader, b Bounds, clk clock.Clock) ScopedReader {
	return &bounded{r: r, b: b, clock: clk, calls: map[string]*call{}, memo: map[string]*list.Element{}, lru: list.New()}
}

type bounded struct {
	r     ScopedReader
	b     Bounds
	clock clock.Clock

	mu sync.Mutex
	// calls are the reads requests can join; running counts provider reads, timed-out ones included.
	calls   map[string]*call
	running int
	memo    map[string]*list.Element
	lru     *list.List // of *entry, most recent first
	bytes   int        // the memo's entries' sizes
}

// call is one read; done closes once res and err are set.
type call struct {
	done chan struct{}
	res  Result
	err  error
}

type entry struct {
	key  string
	at   time.Time
	res  packed
	size int
}

// Endpoints serves svc from the memo or a shared read. A read joined by
// several requests runs with the first caller's context values, without its cancellation.
func (b *bounded) Endpoints(ctx context.Context, installID string, svc objects.Ref) (Result, error) {
	if svc.Group != "" || svc.Kind != "Service" {
		return Result{Complete: Unavailable, ReadAt: b.clock.Now()}, nil
	}
	if err := ctx.Err(); err != nil {
		return b.ctxResult(err), err
	}
	key := installID + "\x00" + svc.Namespace + "\x00" + svc.Name
	b.mu.Lock()
	if el, ok := b.memo[key]; ok {
		e := el.Value.(*entry)
		if b.clock.Since(e.at) < b.b.TTL {
			b.lru.MoveToFront(el)
			b.mu.Unlock()
			return e.res.unpack(), nil
		}
		b.forget(el)
	}
	c, ok := b.calls[key]
	if !ok {
		if b.running >= b.b.InFlight {
			b.mu.Unlock()
			return Result{Complete: Busy, ReadAt: b.clock.Now()}, nil
		}
		c = &call{done: make(chan struct{})}
		b.calls[key] = c
		b.running++
		go b.read(context.WithoutCancel(ctx), key, installID, svc, c)
	}
	b.mu.Unlock()
	select {
	case <-c.done:
		return c.res, c.err
	case <-ctx.Done():
		return b.ctxResult(ctx.Err()), ctx.Err()
	}
}

// ctxResult is the result of a request whose context ended with err.
func (b *bounded) ctxResult(err error) Result {
	if errors.Is(err, context.DeadlineExceeded) {
		return Result{Complete: TimedOut, ReadAt: b.clock.Now()}
	}
	return Result{Complete: Failed, ReadAt: b.clock.Now()}
}

// read runs one provider read for key and finishes c, at the latest at the timeout.
func (b *bounded) read(ctx context.Context, key, installID string, svc objects.Ref, c *call) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	timer := b.clock.NewTimer(b.b.Timeout)
	defer timer.Stop()
	type out struct {
		res Result
		err error
	}
	got := make(chan out, 1)
	go func() {
		res, err := b.r.Endpoints(ctx, installID, svc)
		// The slot frees before the result is seen, so a later request never finds it taken.
		b.mu.Lock()
		b.running--
		b.mu.Unlock()
		got <- out{res, err}
	}()
	select {
	case o := <-got:
		c.res, c.err = o.res, o.err
		if c.err != nil && errors.Is(c.err, context.DeadlineExceeded) {
			c.res = b.ctxResult(c.err)
		}
	case <-timer.C():
		c.res = Result{Complete: TimedOut, ReadAt: b.clock.Now()}
	}
	if len(c.res.Items) > b.b.MaxEndpoints {
		c.res.Items, c.res.Complete = c.res.Items[:b.b.MaxEndpoints:b.b.MaxEndpoints], Partial
	}
	var p packed
	keep := c.err == nil && (c.res.Complete == Complete || c.res.Complete == Partial)
	if keep {
		p, keep = pack(c.res)
	}
	b.mu.Lock()
	delete(b.calls, key)
	if keep {
		b.remember(key, p)
	}
	b.mu.Unlock()
	close(c.done)
}

// remember stores p for key unless it alone passes MemoBytes, then evicts
// least recently used entries while over either cap; b.mu is held.
func (b *bounded) remember(key string, p packed) {
	size := p.size() + len(key)
	if size > b.b.MemoBytes {
		return
	}
	b.memo[key] = b.lru.PushFront(&entry{key: key, at: b.clock.Now(), res: p, size: size})
	b.bytes += size
	for b.lru.Len() > b.b.MemoServices || b.bytes > b.b.MemoBytes {
		b.forget(b.lru.Back())
	}
}

// forget drops one memo entry; b.mu is held.
func (b *bounded) forget(el *list.Element) {
	e := el.Value.(*entry)
	b.lru.Remove(el)
	delete(b.memo, e.key)
	b.bytes -= e.size
}
