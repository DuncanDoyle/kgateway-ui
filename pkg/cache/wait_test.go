package cache

import (
	"context"
	"regexp"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
)

var goroutineHeader = regexp.MustCompile(`(?m)^goroutine (\d+) \[`)

// goroutines returns the stack of every live goroutine by id.
func goroutines(t *testing.T) map[int]string {
	t.Helper()
	var sb strings.Builder
	if err := pprof.Lookup("goroutine").WriteTo(&sb, 2); err != nil {
		t.Fatal(err)
	}
	out := map[int]string{}
	for _, g := range strings.Split(sb.String(), "\n\n") {
		if m := goroutineHeader.FindStringSubmatch(g); m != nil {
			id, _ := strconv.Atoi(m[1])
			out[id] = g
		}
	}
	return out
}

// started returns the stacks of goroutines that are not in before.
func started(t *testing.T, before map[int]string) []string {
	t.Helper()
	var out []string
	for id, g := range goroutines(t) {
		if _, ok := before[id]; !ok {
			out = append(out, g)
		}
	}
	return out
}

// waitReturns calls c.Wait and fails t when it does not return within 10 s.
func waitReturns(t *testing.T, c *Cache, what string) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); c.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("Wait did not return: %s", what)
	}
}

// fullCache is a cache with every watch on, over fake clients.
func fullCache(t *testing.T) *Cache {
	t.Helper()
	dyn, kube := testutil.Clients(t, testutil.Namespace("shop", nil), ctrlPod(), proxyPod(), testutil.Gateway("shop", "edge", "kgateway", nil))
	return New(Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream(), Metadata: newMeta(t),
		PodSelectors: []labels.Selector{podSel}, DeploymentSelectors: []labels.Selector{deploySel},
		ProxyPodSelectors: []labels.Selector{proxySel}, Events: true, CRDs: true, Endpoints: true})
}

// Wait returns once every goroutine Start launched has exited, and none is left.
func TestWaitJoinsEveryGoroutineStartLaunched(t *testing.T) {
	c := fullCache(t)
	before := goroutines(t)
	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx)
	waitSynced(t, c)
	if n := len(started(t, before)); n == 0 {
		t.Fatal("Start launched no goroutines")
	}
	cancel()
	waitReturns(t, c, "after cancel")
	if left := started(t, before); len(left) > 0 {
		t.Fatalf("%d goroutines outlive Wait:\n%s", len(left), strings.Join(left, "\n\n"))
	}
}

// Wait before Start, and a second Wait, return at once.
func TestWaitBeforeStartAndTwiceReturnAtOnce(t *testing.T) {
	c := fullCache(t)
	waitReturns(t, c, "before Start")
	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx)
	waitSynced(t, c)
	cancel()
	waitReturns(t, c, "first")
	waitReturns(t, c, "second")
}

// Wait blocks while the context is live.
func TestWaitBlocksUntilCancelled(t *testing.T) {
	c := fullCache(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Start(ctx)
	waitSynced(t, c)
	done := make(chan struct{})
	go func() { defer close(done); c.Wait() }()
	select {
	case <-done:
		t.Fatal("Wait returned before the context was cancelled")
	case <-time.After(300 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Wait did not return after cancel")
	}
}

// The watch wrappers' loops are joined: the group stays busy until the watch stops.
func TestWatchWrapperLoopsAreJoined(t *testing.T) {
	for name, wrap := range map[string]func(watch.Interface, *sync.WaitGroup) watch.Interface{
		"observe": func(w watch.Interface, wg *sync.WaitGroup) watch.Interface {
			return observe(w, wg, func(watch.Event) {})
		},
		"filter": func(w watch.Interface, wg *sync.WaitGroup) watch.Interface {
			return newFilterWatch(w, wg, func(e watch.Event) []watch.Event { return []watch.Event{e} })
		},
	} {
		t.Run(name, func(t *testing.T) {
			var wg sync.WaitGroup
			in := watch.NewFake()
			w := wrap(in, &wg)
			done := make(chan struct{})
			go func() { defer close(done); wg.Wait() }()
			select {
			case <-done:
				t.Fatal("joined before the watch stopped")
			case <-time.After(100 * time.Millisecond):
			}
			w.Stop()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("the loop outlived Stop")
			}
		})
	}
}
