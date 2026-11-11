package endpoints

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	testingclock "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// fakeReader counts reads per Service. With block set, a read starts by
// sending its Service on started, then waits for release or its context.
type fakeReader struct {
	mu      sync.Mutex
	reads   map[string]int
	total   atomic.Int32
	block   bool
	started chan string
	release chan struct{}
}

func newFake(block bool) *fakeReader {
	return &fakeReader{reads: map[string]int{}, block: block, started: make(chan string, 64), release: make(chan struct{})}
}

func (f *fakeReader) Endpoints(ctx context.Context, _ string, svc objects.Ref) (Result, error) {
	f.mu.Lock()
	f.reads[svc.Name]++
	f.mu.Unlock()
	f.total.Add(1)
	if f.block {
		f.started <- svc.Name
		select {
		case <-f.release:
		case <-ctx.Done():
			return Result{Complete: Failed}, ctx.Err()
		}
	}
	return Result{Complete: Complete, Items: []Endpoint{{Address: "10.0.0.1", Port: 8080, Ready: true, Serving: true}}}, nil
}

func (f *fakeReader) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads[name]
}

func svcRef(i int) objects.Ref {
	return objects.Ref{Kind: "Service", Namespace: "shop", Name: fmt.Sprintf("svc-%d", i)}
}

var t0 = time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)

// Ten concurrent requests for one Service share one read.
func TestBoundedCoalesces(t *testing.T) {
	f := newFake(true)
	r := Bounded(f, DefaultBounds, testingclock.NewFakeClock(t0))
	var wg sync.WaitGroup
	results := make([]Result, 10)
	for i := range 10 {
		wg.Go(func() { results[i], _ = r.Endpoints(context.Background(), "i1", svcRef(0)) })
	}
	<-f.started
	close(f.release)
	wg.Wait()
	if n := f.total.Load(); n != 1 {
		t.Fatalf("reads %d, want 1", n)
	}
	for i, res := range results {
		if res.Complete != Complete || len(res.Items) != 1 {
			t.Errorf("result %d: %+v", i, res)
		}
	}
}

// A fifth distinct read while four are in flight is busy, and reads nothing.
func TestBoundedBusy(t *testing.T) {
	f := newFake(true)
	clk := testingclock.NewFakeClock(t0)
	r := Bounded(f, DefaultBounds, clk)
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() { _, _ = r.Endpoints(context.Background(), "i1", svcRef(i)) })
	}
	for range 4 {
		<-f.started
	}
	res, err := r.Endpoints(context.Background(), "i1", svcRef(4))
	if err != nil || res.Complete != Busy || !res.ReadAt.Equal(t0) {
		t.Fatalf("fifth read: %+v, %v; want busy at %v", res, err, t0)
	}
	if f.count("svc-4") != 0 {
		t.Fatal("busy read reached the provider")
	}
	close(f.release)
	wg.Wait()
	if res, _ := r.Endpoints(context.Background(), "i1", svcRef(4)); res.Complete != Complete {
		t.Fatalf("after the four finish: %+v, want complete", res)
	}
}

// A read still running after the timeout is timed-out, its context is
// cancelled, and the result is not remembered.
func TestBoundedTimeout(t *testing.T) {
	f := newFake(true)
	clk := testingclock.NewFakeClock(t0)
	r := Bounded(f, DefaultBounds, clk)
	done := make(chan Result)
	go func() { res, _ := r.Endpoints(context.Background(), "i1", svcRef(0)); done <- res }()
	<-f.started // The timeout timer is set before the read starts.
	clk.Step(1999 * time.Millisecond)
	select {
	case res := <-done:
		t.Fatalf("returned before the timeout: %+v", res)
	default:
	}
	clk.Step(time.Millisecond)
	if res := <-done; res.Complete != TimedOut || !res.ReadAt.Equal(t0.Add(2*time.Second)) {
		t.Fatalf("result %+v, want timed-out at +2s", res)
	}
	close(f.release)
	if res, _ := r.Endpoints(context.Background(), "i1", svcRef(0)); res.Complete != Complete || f.count("svc-0") != 2 {
		t.Fatalf("after a timeout: %+v after %d reads, want a fresh complete read", res, f.count("svc-0"))
	}
}

// The memo holds 32 Services: the 33rd evicts the least recently used, and
// an entry 5 s old is read again.
func TestBoundedLRU(t *testing.T) {
	f := newFake(false)
	clk := testingclock.NewFakeClock(t0)
	r := Bounded(f, DefaultBounds, clk)
	read := func(i int) {
		t.Helper()
		if res, err := r.Endpoints(context.Background(), "i1", svcRef(i)); err != nil || res.Complete != Complete {
			t.Fatalf("svc-%d: %+v, %v", i, res, err)
		}
	}
	for i := range 32 {
		read(i)
	}
	read(0) // svc-0 becomes most recent; svc-1 is now the least.
	read(32)
	read(0)
	if f.count("svc-0") != 1 {
		t.Errorf("svc-0 read %d times, want 1 (memo hits)", f.count("svc-0"))
	}
	read(1)
	if f.count("svc-1") != 2 {
		t.Errorf("svc-1 read %d times, want 2 (evicted by svc-32)", f.count("svc-1"))
	}
	// The same Service in another install is its own entry.
	if _, _ = r.Endpoints(context.Background(), "i2", svcRef(0)); f.count("svc-0") != 2 {
		t.Errorf("svc-0 of i2 read %d times in all, want 2", f.count("svc-0"))
	}
	clk.Step(4999 * time.Millisecond)
	read(32)
	if f.count("svc-32") != 1 {
		t.Errorf("svc-32 read %d times before its TTL, want 1", f.count("svc-32"))
	}
	clk.Step(time.Millisecond)
	read(32)
	if f.count("svc-32") != 2 {
		t.Errorf("svc-32 read %d times after 5 s, want 2", f.count("svc-32"))
	}
}

// A provider error is returned and not remembered.
func TestBoundedErrorNotRemembered(t *testing.T) {
	calls := 0
	r := Bounded(readerFunc(func(context.Context, string, objects.Ref) (Result, error) {
		calls++
		return Result{Complete: Failed}, fmt.Errorf("forbidden")
	}), DefaultBounds, testingclock.NewFakeClock(t0))
	for range 2 {
		if res, err := r.Endpoints(context.Background(), "i1", svcRef(0)); err == nil || res.Complete != Failed {
			t.Fatalf("result %+v, %v; want failed with the error", res, err)
		}
	}
	if calls != 2 {
		t.Fatalf("reads %d, want 2", calls)
	}
}

type readerFunc func(context.Context, string, objects.Ref) (Result, error)

func (f readerFunc) Endpoints(ctx context.Context, id string, svc objects.Ref) (Result, error) {
	return f(ctx, id, svc)
}

// TestMemoRetained fills the memo with MemoServices Services of MaxEndpoints
// endpoints each, decoded fresh as a provider would, and holds its retained
// heap to MemoBytes. At the IPv4 and IPv6 density every Service fits; with
// FQDNs and Pod names sharing no prefix the byte budget evicts.
func TestMemoRetained(t *testing.T) {
	b := DefaultBounds
	rs := func(i, n int) string { // n pseudo-random lowercase letters
		out := make([]byte, n)
		h := uint64(i)*0x9E3779B97F4A7C15 + 1
		for j := range out {
			h ^= h >> 31
			h *= 0xBF58476D1CE4E5B9
			out[j] = 'a' + byte(h>>59)%26
		}
		return string(out)
	}
	for _, tc := range []struct {
		name    string
		addr    func(svc string, i int) string
		pod     func(svc string, i int) string
		fitsAll bool
	}{
		{"IPv4", func(_ string, i int) string { return fmt.Sprintf("10.244.%d.%d", node(i), i%250+2) },
			func(svc string, i int) string { return svc + "-7d9f8b6c5d-" + podSuffix(i) }, true},
		{"IPv6", func(_ string, i int) string { return fmt.Sprintf("fd00:10:244:%x::%x", node(i), i%250+2) },
			func(svc string, i int) string { return svc + "-7d9f8b6c5d-" + podSuffix(i) }, true},
		{"FQDN, no shared prefixes", func(svc string, i int) string { return rs(i, 40) + ".example.com" },
			func(svc string, i int) string { return rs(i+1<<20, 50) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fill := readerFunc(func(_ context.Context, _ string, svc objects.Ref) (Result, error) {
				items := make([]Endpoint, 0, b.MaxEndpoints)
				for i := range b.MaxEndpoints {
					items = append(items, Endpoint{Address: tc.addr(svc.Name, i), Port: 8080, PortName: "http",
						Ready: true, Serving: true, Pod: tc.pod(svc.Name, i)})
				}
				return Result{Items: items, Complete: Partial}, nil
			})
			r := Bounded(fill, b, testingclock.NewFakeClock(t0))
			before := heapAlloc()
			for i := range b.MemoServices {
				svc := objects.Ref{Kind: "Service", Namespace: "shop", Name: fmt.Sprintf("checkout-service-%02d", i)}
				if res, err := r.Endpoints(context.Background(), "i1", svc); err != nil || res.Complete != Partial {
					t.Fatalf("%s: %s, %v; want partial", svc.Name, res.Complete, err)
				}
			}
			retained := int64(heapAlloc()) - int64(before)
			bd := r.(*bounded)
			bd.mu.Lock()
			kept, counted := bd.lru.Len(), bd.bytes
			bd.mu.Unlock()
			t.Logf("memo keeps %d of %d Services × %d endpoints; counts %d B, retains %d B (%.2f MiB)",
				kept, b.MemoServices, b.MaxEndpoints, counted, retained, float64(retained)/(1<<20))
			if retained > int64(b.MemoBytes) || counted > b.MemoBytes {
				t.Errorf("memo retains %d B (counted %d), budget %d", retained, counted, b.MemoBytes)
			}
			if tc.fitsAll && kept != b.MemoServices {
				t.Errorf("memo keeps %d Services, want all %d", kept, b.MemoServices)
			}
		})
	}
}

// node scatters endpoint i over 50 nodes, so neighbours rarely share a node's address range.
func node(i int) int { return int(uint32(i)*2246822519>>7) % 50 }

// podSuffix is a ReplicaSet Pod's random-looking five-character suffix.
func podSuffix(i int) string {
	const alphabet = "bcdfghjklmnpqrstvwxz2456789"
	h := uint32(i)*2654435761 + 12345
	b := make([]byte, 5)
	for j := range b {
		b[j] = alphabet[h%uint32(len(alphabet))]
		h /= uint32(len(alphabet))
	}
	return string(b)
}

func heapAlloc() uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// A memo hit returns the endpoints the read did: both IP families, IPs not
// written canonically, an FQDN,
// several ports per address, every condition, and no Pod.
func TestBoundedMemoHitKeepsEndpoints(t *testing.T) {
	want := []Endpoint{
		{Address: "10.244.1.7", Port: 8080, PortName: "http", Ready: true, Serving: true, Pod: "cart-7d9f8b6c5d-x2k9p"},
		{Address: "10.244.1.7", Port: 9000, PortName: "grpc", Ready: true, Serving: true, Pod: "cart-7d9f8b6c5d-x2k9p"},
		{Address: "10.244.3.12", Port: 8080, PortName: "http", Serving: true, Terminating: true, Pod: "cart-7d9f8b6c5d-q7m4z"},
		{Address: "fd00:10:244:3::c", Port: 8080, PortName: "http", Pod: "cart-5c6b7a8d9e-b2c3d"},
		{Address: "::ffff:10.1.2.3", Port: 8080, PortName: "http", Ready: true},
		{Address: "fd00:0:0::5", Port: 8080, PortName: "http", Ready: true},
		{Address: "FD00::6", Port: 8080, PortName: "http", Ready: true},
		{Address: "db.example.com", Port: 5432, Ready: true, Serving: true},
		{Address: "10.244.1.8", Port: 5432, Ready: true},
	}
	calls := 0
	r := Bounded(readerFunc(func(context.Context, string, objects.Ref) (Result, error) {
		calls++
		return Result{Items: slices.Clone(want), Complete: Complete, ReadAt: t0}, nil
	}), DefaultBounds, testingclock.NewFakeClock(t0))
	for range 2 {
		res, err := r.Endpoints(context.Background(), "i1", svcRef(0))
		if err != nil || res.Complete != Complete || !res.ReadAt.Equal(t0) || !slices.Equal(res.Items, want) {
			t.Fatalf("result %+v, %v\nwant items %+v", res, err, want)
		}
	}
	if calls != 1 {
		t.Fatalf("reads %d, want 1", calls)
	}
}

// A provider result over MaxEndpoints is cut and partial, read and remembered alike.
func TestBoundedCutsToMaxEndpoints(t *testing.T) {
	b := DefaultBounds
	b.MaxEndpoints = 3
	r := Bounded(readerFunc(func(context.Context, string, objects.Ref) (Result, error) {
		items := make([]Endpoint, 5)
		for i := range items {
			items[i] = Endpoint{Address: fmt.Sprintf("10.0.0.%d", i+1), Port: int32(8000 + i)}
		}
		return Result{Items: items, Complete: Complete}, nil
	}), b, testingclock.NewFakeClock(t0))
	for range 2 {
		if res, _ := r.Endpoints(context.Background(), "i1", svcRef(0)); res.Complete != Partial || len(res.Items) != 3 || res.Items[2].Address != "10.0.0.3" {
			t.Fatalf("result %+v, want the first 3, partial", res)
		}
	}
}

// A result larger than MemoBytes is served but not remembered.
func TestBoundedSkipsOversized(t *testing.T) {
	b := DefaultBounds
	b.MemoBytes = 1024
	calls := 0
	r := Bounded(readerFunc(func(context.Context, string, objects.Ref) (Result, error) {
		calls++
		items := make([]Endpoint, 100)
		for i := range items {
			items[i] = Endpoint{Address: fmt.Sprintf("10.0.%d.%d", i, i), Pod: fmt.Sprintf("pod-%03d", i)}
		}
		return Result{Items: items, Complete: Complete}, nil
	}), b, testingclock.NewFakeClock(t0))
	for range 2 {
		if res, _ := r.Endpoints(context.Background(), "i1", svcRef(0)); res.Complete != Complete || len(res.Items) != 100 {
			t.Fatalf("result %+v", res)
		}
	}
	if calls != 2 || r.(*bounded).bytes != 0 {
		t.Fatalf("reads %d, memo %d B; want 2 reads and an empty memo", calls, r.(*bounded).bytes)
	}
}

// A request whose context has ended takes no slot: a deadline is timed-out,
// a cancel failed. A provider's deadline error is timed-out too.
func TestBoundedContext(t *testing.T) {
	f := newFake(false)
	r := Bounded(f, DefaultBounds, testingclock.NewFakeClock(t0))
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	past, cancel2 := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancel2()
	if res, err := r.Endpoints(cancelled, "i1", svcRef(0)); err == nil || res.Complete != Failed {
		t.Errorf("cancelled: %+v, %v; want failed", res, err)
	}
	if res, err := r.Endpoints(past, "i1", svcRef(0)); err == nil || res.Complete != TimedOut {
		t.Errorf("past deadline: %+v, %v; want timed-out", res, err)
	}
	if n := f.total.Load(); n != 0 {
		t.Errorf("reads %d, want 0", n)
	}
	late := Bounded(readerFunc(func(context.Context, string, objects.Ref) (Result, error) {
		return Result{Complete: Failed}, fmt.Errorf("list: %w", context.DeadlineExceeded)
	}), DefaultBounds, testingclock.NewFakeClock(t0))
	if res, err := late.Endpoints(context.Background(), "i1", svcRef(0)); err == nil || res.Complete != TimedOut {
		t.Errorf("provider deadline: %+v, %v; want timed-out", res, err)
	}
}

// Only core Service refs are read; any other kind is unavailable.
func TestBoundedServicesOnly(t *testing.T) {
	f := newFake(false)
	r := Bounded(f, DefaultBounds, testingclock.NewFakeClock(t0))
	for _, ref := range []objects.Ref{{Group: "gateway.kgateway.dev", Kind: "Backend", Namespace: "shop", Name: "svc-0"}, {Kind: "Pod", Namespace: "shop", Name: "svc-0"}} {
		if res, err := r.Endpoints(context.Background(), "i1", ref); err != nil || res.Complete != Unavailable {
			t.Errorf("%+v: %+v, %v; want unavailable", ref, res, err)
		}
	}
	if n := f.total.Load(); n != 0 {
		t.Errorf("reads %d, want 0", n)
	}
}

// size counts a row as 12 bytes and a portKey as 24.
func TestPackedSizes(t *testing.T) {
	if r, k := unsafe.Sizeof(row{}), unsafe.Sizeof(portKey{}); r != 12 || k != 24 {
		t.Fatalf("row %d B, portKey %d B; size assumes 12 and 24", r, k)
	}
}
