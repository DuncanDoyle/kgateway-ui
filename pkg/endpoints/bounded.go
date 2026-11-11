package endpoints

import (
	"container/list"
	"context"
	"sync"
	"time"

	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// Bounds limit the reads behind a ScopedReader process-wide (design §9.3).
// InFlight is the most reads at once; MemoServices and TTL size the memo.
// MaxEndpoints, PageSize and MaxPages bound one read and are the provider's to apply.
type Bounds struct {
	InFlight, MemoServices, MaxEndpoints, PageSize, MaxPages int
	Timeout, TTL                                             time.Duration
}

// DefaultBounds are the design's bounds.
var DefaultBounds = Bounds{InFlight: 4, MemoServices: 32, MaxEndpoints: 1000, PageSize: 10, MaxPages: 10, Timeout: 2 * time.Second, TTL: 5 * time.Second}

// Bounded wraps r: duplicate requests share one read, at most b.InFlight
// reads run (a further one is Busy), a read is TimedOut after b.Timeout, and
// complete and partial results are remembered for b.TTL, least recently used first.
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
}

// call is one read; done closes once res and err are set.
type call struct {
	done chan struct{}
	res  Result
	err  error
}

type entry struct {
	key string
	at  time.Time
	res packed
}

func (b *bounded) Endpoints(ctx context.Context, installID string, svc objects.Ref) (Result, error) {
	key := installID + "\x00" + svc.Namespace + "\x00" + svc.Name
	b.mu.Lock()
	if el, ok := b.memo[key]; ok {
		e := el.Value.(*entry)
		if b.clock.Since(e.at) < b.b.TTL {
			b.lru.MoveToFront(el)
			b.mu.Unlock()
			return e.res.unpack(), nil
		}
		b.lru.Remove(el)
		delete(b.memo, key)
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
		// The read outlives the request that started it, since others may join it.
		go b.read(context.WithoutCancel(ctx), key, installID, svc, c)
	}
	b.mu.Unlock()
	select {
	case <-c.done:
		return c.res, c.err
	case <-ctx.Done():
		return Result{Complete: Failed, ReadAt: b.clock.Now()}, ctx.Err()
	}
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
		got <- out{res, err}
		b.mu.Lock()
		b.running--
		b.mu.Unlock()
	}()
	select {
	case o := <-got:
		c.res, c.err = o.res, o.err
	case <-timer.C():
		c.res = Result{Complete: TimedOut, ReadAt: b.clock.Now()}
	}
	b.mu.Lock()
	delete(b.calls, key)
	if c.err == nil && (c.res.Complete == Complete || c.res.Complete == Partial) {
		b.remember(key, c.res)
	}
	b.mu.Unlock()
	close(c.done)
}

// remember stores res for key, evicting the least recently used entry over the cap; b.mu is held.
func (b *bounded) remember(key string, res Result) {
	b.memo[key] = b.lru.PushFront(&entry{key: key, at: b.clock.Now(), res: pack(res)})
	for b.lru.Len() > b.b.MemoServices {
		el := b.lru.Back()
		b.lru.Remove(el)
		delete(b.memo, el.Value.(*entry).key)
	}
}
