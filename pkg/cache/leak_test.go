package cache

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	clienttesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/trigger"
)

// queuedOnStop is a watch that queues one event as it stops, like a stream
// that decoded an event just as its reader went away.
type queuedOnStop struct {
	ch   chan watch.Event
	once sync.Once
}

func (w *queuedOnStop) ResultChan() <-chan watch.Event { return w.ch }

func (w *queuedOnStop) Stop() {
	w.once.Do(func() {
		w.ch <- watch.Event{Type: watch.Added, Object: testutil.Namespace("late", nil)}
		close(w.ch)
	})
}

// settleGoroutines waits until no more than base goroutines run: the core
// informers stop on their own after cancel, so Start gives nothing to wait on.
func settleGoroutines(t *testing.T, base int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for runtime.NumGoroutine() > base {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			t.Fatalf("%d goroutines, want at most %d:\n%s", runtime.NumGoroutine(), base, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A stopped cache leaves no goroutines behind, also when a watch ends with an
// event still in flight: on cancel, and on a watch restart while it runs.
func TestStoppedCacheLeavesNoGoroutines(t *testing.T) {
	tooMany := metav1.Status{Status: metav1.StatusFailure, Code: 429, Reason: metav1.StatusReasonTooManyRequests, Message: "slow down"}
	cases := []struct {
		name  string
		runs  int
		watch func() watch.Interface
		// whileRunning acts on the cache's first Namespaces watch after sync.
		whileRunning func(t *testing.T, first watch.Interface, watchers <-chan watch.Interface)
	}{
		{"cancel with an event in flight", 10,
			func() watch.Interface { return &queuedOnStop{ch: make(chan watch.Event, 1)} }, nil},
		{"watch restart with an event in flight", 3,
			func() watch.Interface { return watch.NewRaceFreeFake() },
			func(t *testing.T, first watch.Interface, watchers <-chan watch.Interface) {
				w := first.(*watch.RaceFreeFakeWatcher)
				status := tooMany
				w.Error(&status) // the reflector stops reading here and restarts the watch
				w.Add(testutil.Namespace("late", nil))
				testutil.Recv(t, watchers, "the restarted watch")
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := runtime.NumGoroutine()
			for range tc.runs {
				dyn, kube := testutil.Clients(t, testutil.Namespace("shop", nil))
				watchers := make(chan watch.Interface, 8)
				kube.PrependWatchReactor("namespaces", func(clienttesting.Action) (bool, watch.Interface, error) {
					w := tc.watch()
					watchers <- w
					return true, w, nil
				})
				c := New(Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream(), Changes: trigger.New(),
					PodSelectors: []labels.Selector{podSel}, DeploymentSelectors: []labels.Selector{deploySel}})
				ctx, cancel := context.WithCancel(context.Background())
				c.Start(ctx)
				waitSynced(t, c)
				first := testutil.Recv(t, watchers, "the Namespaces watch")
				if tc.whileRunning != nil {
					tc.whileRunning(t, first, watchers)
				}
				cancel()
			}
			settleGoroutines(t, base)
		})
	}
}
