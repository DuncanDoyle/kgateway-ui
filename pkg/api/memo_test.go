package api

import (
	"bytes"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// The route list borrows its Authorizer memo from seenPool. These tests
// swap the pool so reuse is certain, then check that callers stay apart.

// callerAuthz decides by the X-Caller header: "a" is denied hiddenSome,
// "b" is denied everything else, and "boom" answers as "b" three times,
// then panics. It counts calls per caller.
type callerAuthz struct {
	mu    sync.Mutex
	asked map[string]map[objects.Ref]int
	booms int
}

func (c *callerAuthz) Authorize(r *http.Request, ref objects.Ref) bool {
	who := r.Header.Get("X-Caller")
	c.mu.Lock()
	defer c.mu.Unlock()
	if who == "boom" {
		if c.booms++; c.booms > 3 {
			panic("authorizer failed")
		}
		return hiddenSome(ref)
	}
	if c.asked[who] == nil {
		c.asked[who] = map[objects.Ref]int{}
	}
	c.asked[who][ref]++
	return hiddenSome(ref) == (who == "b")
}

// take returns who's calls since the last take and resets them.
func (c *callerAuthz) take(who string) map[objects.Ref]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.asked[who]
	delete(c.asked, who)
	return out
}

// freshMemos hands out a new memo every time, as no pool would.
type freshMemos struct{}

func (freshMemos) Get() any { return map[objects.Ref]bool{} }
func (freshMemos) Put(any)  {}

// chanMemos hands out the same few memos over and over; Get waits for one.
// Put fails the test when a memo comes back with entries in it.
type chanMemos struct {
	t  *testing.T
	ch chan map[objects.Ref]bool
}

func newChanMemos(t *testing.T, n int) *chanMemos {
	p := &chanMemos{t: t, ch: make(chan map[objects.Ref]bool, n)}
	for range n {
		p.ch <- map[objects.Ref]bool{}
	}
	return p
}

func (p *chanMemos) Get() any { return <-p.ch }
func (p *chanMemos) Put(x any) {
	m := x.(map[objects.Ref]bool)
	if len(m) != 0 {
		p.t.Errorf("a memo went back to the pool with %d entries", len(m))
	}
	p.ch <- m
}

// usePool swaps seenPool for p for the rest of t.
func usePool(t *testing.T, p interface {
	Get() any
	Put(any)
}) {
	saved := seenPool
	seenPool = p
	t.Cleanup(func() { seenPool = saved })
}

// reply is one response as a caller sees it, with its Authorizer calls.
type reply struct {
	status int
	body   []byte
	calls  map[objects.Ref]int
}

// ask sends GET path as who; err is set when the server dropped the request.
func ask(srv *httptest.Server, who, path string) (status int, body []byte, err error) {
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-Caller", who)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err = io.ReadAll(resp.Body)
	return resp.StatusCode, body, err
}

// memoWorld starts visWorld under a callerAuthz and records each caller's
// replies with a fresh memo per request, the baseline the pooled runs must match.
func memoWorld(t *testing.T) (*httptest.Server, *callerAuthz, map[string]map[string]reply) {
	a := &callerAuthz{asked: map[string]map[objects.Ref]int{}}
	_, srv, _ := startWith(t, setup{authz: a}, visWorld()...)
	saved := seenPool
	seenPool = freshMemos{}
	defer func() { seenPool = saved }()
	want := map[string]map[string]reply{}
	for _, who := range []string{"a", "b"} {
		want[who] = map[string]reply{}
		for _, p := range endpoints {
			st, body, err := ask(srv, who, p)
			if err != nil {
				t.Fatalf("%s GET %s: %v", who, p, err)
			}
			want[who][p] = reply{st, body, a.take(who)}
		}
	}
	// The callers disagree, or the test proves nothing.
	if p := base + "/routes"; bytes.Equal(want["a"][p].body, want["b"][p].body) {
		t.Fatalf("callers a and b see the same %s", p)
	}
	return srv, a, want
}

// check compares one pooled reply with the baseline.
func check(t *testing.T, who, p string, got, want reply) {
	t.Helper()
	if got.status != want.status || !bytes.Equal(got.body, want.body) {
		t.Errorf("%s GET %s with a reused memo: %d %s\nwant %d %s", who, p, got.status, got.body, want.status, want.body)
	}
	if got.calls != nil && !maps.Equal(got.calls, want.calls) {
		t.Errorf("%s GET %s with a reused memo asked %v, want %v", who, p, got.calls, want.calls)
	}
}

// Callers with opposite decisions, one after the other on one memo, each
// get the baseline rows, totals and restricted facts, and each asks the
// Authorizer itself.
func TestPooledMemoSequentialCallers(t *testing.T) {
	srv, a, want := memoWorld(t)
	usePool(t, newChanMemos(t, 1))
	for range 20 {
		for _, who := range []string{"a", "b"} {
			for _, p := range endpoints {
				st, body, err := ask(srv, who, p)
				if err != nil {
					t.Fatalf("%s GET %s: %v", who, p, err)
				}
				check(t, who, p, reply{st, body, a.take(who)}, want[who][p])
			}
		}
	}
}

// The same callers at once, sharing two memos, under -race.
func TestPooledMemoConcurrentCallers(t *testing.T) {
	srv, _, want := memoWorld(t)
	usePool(t, newChanMemos(t, 2))
	var wg sync.WaitGroup
	for i := range 8 {
		who := []string{"a", "b"}[i%2]
		wg.Go(func() {
			for range 10 {
				for _, p := range endpoints {
					st, body, err := ask(srv, who, p)
					if err != nil {
						t.Errorf("%s GET %s: %v", who, p, err)
						return
					}
					// Calls interleave across a caller's goroutines, so only the replies are compared.
					check(t, who, p, reply{status: st, body: body}, want[who][p])
				}
			}
		})
	}
	wg.Wait()
}

// An Authorizer that panics mid-list leaves the memo clean: the next
// caller on the same memo gets its baseline.
func TestPooledMemoAfterAuthorizerPanic(t *testing.T) {
	srv, a, want := memoWorld(t)
	pool := newChanMemos(t, 1)
	usePool(t, pool)
	p := base + "/routes"
	// Fill the memo with b's answers first, so a leak would show in a's reply.
	if _, _, err := ask(srv, "b", p); err != nil {
		t.Fatal(err)
	}
	a.take("b")
	if _, _, err := ask(srv, "boom", p); err == nil {
		t.Fatal("the panicking request returned a response")
	}
	// The server recovered the panic; the memo is back in the pool, empty.
	if len(pool.ch) != 1 {
		t.Fatalf("%d memos in the pool after the panic, want 1", len(pool.ch))
	}
	for _, who := range []string{"a", "b"} {
		st, body, err := ask(srv, who, p)
		if err != nil {
			t.Fatal(err)
		}
		check(t, who, p, reply{st, body, a.take(who)}, want[who][p])
	}
}
