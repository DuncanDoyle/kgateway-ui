package engine

import (
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
)

func TestDebounceBurst(t *testing.T) {
	var s sched
	t0 := testutil.Now
	for i := range 500 { // 500 events in 100 ms
		s.trigger(t0.Add(time.Duration(i) * 200 * time.Microsecond))
	}
	last := t0.Add(499 * 200 * time.Microsecond)
	if at, _ := s.wake(); !at.Equal(last.Add(Quiet)) {
		t.Fatalf("due %v, want 250 ms after the last event", at)
	}
	if s.start(last.Add(Quiet - time.Nanosecond)) {
		t.Fatal("started early")
	}
	if !s.start(last.Add(Quiet)) {
		t.Fatal("not started when due")
	}
}

func TestDebounceStreamCapsAtOneSecond(t *testing.T) {
	var s sched
	runs := 0
	t0 := testutil.Now
	for i := 0; i <= 50; i++ { // one event per 100 ms for 5 s
		now := t0.Add(time.Duration(i) * 100 * time.Millisecond)
		if s.start(now) {
			runs++
			s.finish()
		}
		s.trigger(now)
	}
	if runs != 5 {
		t.Fatalf("runs %d, want one per 1 s window", runs)
	}
}

func TestSingleFlightKeepsTriggerDuringRun(t *testing.T) {
	var s sched
	s.trigger(testutil.Now)
	s.start(testutil.Now.Add(Quiet))
	s.trigger(testutil.Now.Add(Quiet + time.Millisecond)) // during the run
	if s.start(testutil.Now.Add(10 * time.Second)) {
		t.Fatal("second run while running")
	}
	s.finish()
	if s.idle() {
		t.Fatal("a trigger during the run must leave the state pending")
	}
}

func TestDeadlinesHaveNoDebounce(t *testing.T) {
	var s sched
	at := testutil.Now.Add(60 * time.Second)
	s.setDeadline(Deadline{At: at})
	if !s.start(at) {
		t.Fatal("non-strict deadline is due at its instant")
	}
	s.finish()
	s.setDeadline(Deadline{At: at.Add(time.Minute), Strict: true})
	if s.start(at.Add(time.Minute)) {
		t.Fatal("strict deadline must wait for the first reading after it")
	}
	if w, _ := s.wake(); !w.Equal(at.Add(time.Minute + time.Nanosecond)) {
		t.Fatal("strict wake")
	}
}

func TestSafetyTimer(t *testing.T) {
	var s sched
	s.setSafety(testutil.Now.Add(30 * time.Second))
	if s.start(testutil.Now.Add(29 * time.Second)) {
		t.Fatal("early")
	}
	if !s.start(testutil.Now.Add(30 * time.Second)) {
		t.Fatal("safety timer did not fire")
	}
}

func TestDeadlineDue(t *testing.T) {
	at := testutil.Now
	for _, tc := range []struct {
		name string
		d    Deadline
		now  time.Time
		want bool
	}{
		{"zero is never due", Deadline{}, at, false},
		{"non-strict before", Deadline{At: at}, at.Add(-time.Nanosecond), false},
		{"non-strict at instant", Deadline{At: at}, at, true},
		{"non-strict after", Deadline{At: at}, at.Add(time.Nanosecond), true},
		{"strict at instant", Deadline{At: at, Strict: true}, at, false},
		{"strict after", Deadline{At: at, Strict: true}, at.Add(time.Nanosecond), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.d.Due(tc.now); got != tc.want {
				t.Fatalf("Due = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEarliest(t *testing.T) {
	at := testutil.Now
	for _, tc := range []struct {
		name string
		in   []Deadline
		want Deadline
	}{
		{"none", nil, Deadline{}},
		{"zeros are skipped", []Deadline{{}, {At: at}, {}}, Deadline{At: at}},
		{"earlier instant wins", []Deadline{{At: at.Add(time.Second)}, {At: at}}, Deadline{At: at}},
		{"equal instant keeps non-strict", []Deadline{{At: at, Strict: true}, {At: at}}, Deadline{At: at}},
		{"equal instant keeps non-strict, reversed", []Deadline{{At: at}, {At: at, Strict: true}}, Deadline{At: at}},
		{"strict earlier still wins", []Deadline{{At: at.Add(time.Second)}, {At: at, Strict: true}}, Deadline{At: at, Strict: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Earliest(tc.in...)
			if !got.At.Equal(tc.want.At) || got.Strict != tc.want.Strict {
				t.Fatalf("Earliest = %+v, want %+v", got, tc.want)
			}
			// A deadline at now must stay due at now after Earliest.
			if !tc.want.IsZero() && got.Due(at) != tc.want.Due(at) {
				t.Fatal("Earliest changed whether the result is due at now")
			}
		})
	}
}

// op mutates a scheduler as one step of a transition case.
type op func(*sched)

func trig(d time.Duration) op { return func(s *sched) { s.trigger(testutil.Now.Add(d)) } }
func dl(d time.Duration, st bool) op {
	return func(s *sched) { s.setDeadline(Deadline{At: testutil.Now.Add(d), Strict: st}) }
}
func safety(d time.Duration) op { return func(s *sched) { s.setSafety(testutil.Now.Add(d)) } }
func fin() op                   { return func(s *sched) { s.finish() } }

// run starts the scheduler at d and requires it to start.
func run(t *testing.T, d time.Duration) op {
	return func(s *sched) {
		if !s.start(testutil.Now.Add(d)) {
			t.Fatalf("setup: start at %v refused", d)
		}
	}
}

func TestStartTransitions(t *testing.T) {
	const ms = time.Millisecond
	for _, tc := range []struct {
		name  string
		ops   func(t *testing.T) []op
		at    time.Duration // start(now) is called at testutil.Now+at
		want  bool
		after bool // idle() after the start call
	}{
		{"idle, nothing due", func(*testing.T) []op { return nil }, time.Hour, false, true},
		{"idle, deadline not due", func(*testing.T) []op { return []op{dl(time.Second, false)} }, 999 * ms, false, true},
		{"idle, deadline due", func(*testing.T) []op { return []op{dl(time.Second, false)} }, time.Second, true, false},
		{"idle, strict deadline at instant", func(*testing.T) []op { return []op{dl(time.Second, true)} }, time.Second, false, true},
		{"idle, strict deadline after instant", func(*testing.T) []op { return []op{dl(time.Second, true)} }, time.Second + 1, true, false},
		{"idle, safety not due", func(*testing.T) []op { return []op{safety(time.Second)} }, 999 * ms, false, true},
		{"idle, safety due", func(*testing.T) []op { return []op{safety(time.Second)} }, time.Second, true, false},
		{"pending, quiet not elapsed", func(*testing.T) []op { return []op{trig(0)} }, Quiet - 1, false, false},
		{"pending, quiet elapsed", func(*testing.T) []op { return []op{trig(0)} }, Quiet, true, false},
		{"pending, burst extends quiet", func(*testing.T) []op { return []op{trig(0), trig(200 * ms)} }, 449 * ms, false, false},
		{"pending, quiet after last", func(*testing.T) []op { return []op{trig(0), trig(200 * ms)} }, 450 * ms, true, false},
		{"pending, max delay not reached", func(*testing.T) []op {
			return []op{trig(0), trig(200 * ms), trig(400 * ms), trig(600 * ms), trig(800 * ms)}
		}, 999 * ms, false, false},
		{"pending, max delay reached", func(*testing.T) []op {
			return []op{trig(0), trig(200 * ms), trig(400 * ms), trig(600 * ms), trig(800 * ms)}
		}, time.Second, true, false},
		{"pending, deadline due before quiet", func(*testing.T) []op { return []op{trig(0), dl(10*ms, false)} }, 10 * ms, true, false},
		{"pending, safety due before quiet", func(*testing.T) []op { return []op{trig(0), safety(10 * ms)} }, 10 * ms, true, false},
		{"running, everything due", func(t *testing.T) []op {
			return []op{trig(0), run(t, Quiet), trig(Quiet + 1), dl(0, false), safety(0)}
		}, time.Hour, false, false},
		{"finish, trigger during run is pending", func(t *testing.T) []op {
			return []op{trig(0), run(t, Quiet), trig(Quiet + ms), fin()}
		}, Quiet + ms + Quiet - 1, false, false},
		{"finish, trigger during run runs after quiet", func(t *testing.T) []op {
			return []op{trig(0), run(t, Quiet), trig(Quiet + ms), fin()}
		}, Quiet + ms + Quiet, true, false},
		{"finish, no trigger during run is idle", func(t *testing.T) []op {
			return []op{trig(0), run(t, Quiet), fin()}
		}, time.Hour, false, true},
		{"deadline consumed by its run", func(t *testing.T) []op {
			return []op{dl(time.Second, false), run(t, time.Second), fin()}
		}, time.Hour, false, true},
		{"safety consumed by its run", func(t *testing.T) []op {
			return []op{safety(time.Second), run(t, time.Second), fin()}
		}, time.Hour, false, true},
		{"future deadline survives a trigger run", func(t *testing.T) []op {
			return []op{trig(0), dl(time.Minute, false), run(t, Quiet), fin()}
		}, time.Minute, true, false},
		{"setDeadline replaces the previous one", func(*testing.T) []op {
			return []op{dl(time.Second, false), dl(time.Minute, false)}
		}, time.Second, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s sched
			for _, o := range tc.ops(t) {
				o(&s)
			}
			if got := s.start(testutil.Now.Add(tc.at)); got != tc.want {
				t.Fatalf("start = %v, want %v", got, tc.want)
			}
			if got := s.idle(); got != tc.after {
				t.Fatalf("idle = %v, want %v", got, tc.after)
			}
		})
	}
}

func TestWake(t *testing.T) {
	const ms = time.Millisecond
	for _, tc := range []struct {
		name string
		ops  []op
		want time.Duration
		ok   bool
	}{
		{"nothing scheduled", nil, 0, false},
		{"quiet expiry", []op{trig(0)}, Quiet, true},
		{"quiet after last trigger", []op{trig(0), trig(100 * ms)}, 100*ms + Quiet, true},
		{"max delay caps a burst", []op{trig(0), trig(400 * ms), trig(800 * ms)}, MaxDelay, true},
		{"non-strict deadline", []op{dl(time.Second, false)}, time.Second, true},
		{"strict deadline wakes 1 ns after", []op{dl(time.Second, true)}, time.Second + 1, true},
		{"safety", []op{safety(30 * time.Second)}, 30 * time.Second, true},
		{"deadline before quiet", []op{trig(0), dl(10*ms, false), safety(time.Minute)}, 10 * ms, true},
		{"safety before deadline", []op{dl(time.Minute, false), safety(time.Second)}, time.Second, true},
		{"quiet before deadline and safety", []op{trig(0), dl(time.Second, true), safety(time.Minute)}, Quiet, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s sched
			for _, o := range tc.ops {
				o(&s)
			}
			at, ok := s.wake()
			if ok != tc.ok {
				t.Fatalf("wake ok = %v, want %v", ok, tc.ok)
			}
			if ok && !at.Equal(testutil.Now.Add(tc.want)) {
				t.Fatalf("wake = %v, want Now+%v", at.Sub(testutil.Now), tc.want)
			}
		})
	}
}
