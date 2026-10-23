package engine

import "time"

// Scheduler timing (D-019).
const (
	// Quiet is the trailing debounce window after the last trigger.
	Quiet = 250 * time.Millisecond
	// MaxDelay bounds the debounce from the first trigger of a burst.
	MaxDelay = time.Second
	// ConfirmEvery is the confirm tick interval.
	ConfirmEvery = time.Second
)

// Deadline is a time-driven instant at which the model must be recomputed,
// with no debounce. The zero Deadline is unset and never due.
type Deadline struct {
	// At is the boundary instant.
	At time.Time
	// Strict marks a predicate of the form "now > At": the deadline is due at
	// the first clock reading after At, not at At itself.
	Strict bool
}

// Due reports whether d is due at now: after At when Strict, else at or after At.
func (d Deadline) Due(now time.Time) bool {
	if d.IsZero() {
		return false
	}
	if d.Strict {
		return now.After(d.At)
	}
	return !now.Before(d.At)
}

// IsZero reports whether d is unset.
func (d Deadline) IsZero() bool { return d.At.IsZero() }

// wakeAt is the first instant at which d is due.
func (d Deadline) wakeAt() time.Time {
	if d.Strict {
		return d.At.Add(time.Nanosecond)
	}
	return d.At
}

// Earliest returns the set deadline that falls due first, or the zero
// Deadline when none is set. At equal At a non-strict deadline wins, since it
// is due at At itself; so a deadline due at now is never dropped.
func Earliest(ds ...Deadline) Deadline {
	var best Deadline
	for _, d := range ds {
		if d.IsZero() {
			continue
		}
		if best.IsZero() || d.wakeAt().Before(best.wakeAt()) {
			best = d
		}
	}
	return best
}

// sched is the recompute scheduler state machine (D-019). It is pure: the
// caller supplies every instant and owns the goroutine, clock and timers.
//
// States: idle, pending (triggers waiting out the debounce) and running.
// Pending and running can hold together: a trigger during a run stays pending
// and is due after finish.
type sched struct {
	running bool
	// pending marks a burst; first and last are its first and latest triggers.
	pending     bool
	first, last time.Time
	deadline    Deadline
	safety      time.Time
}

// trigger records an event at now. It starts a burst if none is pending.
func (s *sched) trigger(now time.Time) {
	if !s.pending {
		s.pending, s.first = true, now
	}
	s.last = now
}

// setDeadline replaces the previous deadline; the zero Deadline clears it.
// A run consumes only a due deadline, so the driver sets the next one after
// every run.
func (s *sched) setDeadline(d Deadline) { s.deadline = d }

// setSafety sets the instant by which a run must start; zero clears it.
// A run consumes only a due safety instant, so the driver must call setSafety
// after every run for "recompute at least this often" to hold.
func (s *sched) setSafety(at time.Time) { s.safety = at }

// pendingDue is min(last+Quiet, first+MaxDelay); ok is false when nothing is pending.
func (s *sched) pendingDue() (time.Time, bool) {
	if !s.pending {
		return time.Time{}, false
	}
	due := s.last.Add(Quiet)
	if capAt := s.first.Add(MaxDelay); capAt.Before(due) {
		due = capAt
	}
	return due, true
}

// wake returns the next instant at which start may return true: the earliest
// of the pending due time, the deadline (a strict one 1 ns after At) and the
// safety instant. ok is false when none is set, and while running, since
// nothing can start before finish; the driver asks again after finish.
func (s *sched) wake() (time.Time, bool) {
	if s.running {
		return time.Time{}, false
	}
	var at time.Time
	consider := func(t time.Time) {
		if !t.IsZero() && (at.IsZero() || t.Before(at)) {
			at = t
		}
	}
	if due, ok := s.pendingDue(); ok {
		consider(due)
	}
	if !s.deadline.IsZero() {
		consider(s.deadline.wakeAt())
	}
	consider(s.safety)
	return at, !at.IsZero()
}

// start returns true exactly when no run is in progress and a run is due at
// now: the pending burst's due time has passed (quiet elapsed or max delay
// reached), the deadline is due, or the safety instant has passed. On true it
// moves to running and consumes the pending burst and whichever of the
// deadline and safety instant were due; a not-yet-due one is kept.
//
// Contract: start consumes the whole pending burst, due or not, so the caller
// must take the snapshot after start returns true; the store already holds
// every consumed trigger's change (client-go updates it before handlers run).
func (s *sched) start(now time.Time) bool {
	if s.running {
		return false
	}
	due, pending := s.pendingDue()
	pendingDue := pending && !now.Before(due)
	deadlineDue := s.deadline.Due(now)
	safetyDue := !s.safety.IsZero() && !now.Before(s.safety)
	if !pendingDue && !deadlineDue && !safetyDue {
		return false
	}
	s.running = true
	s.pending, s.first, s.last = false, time.Time{}, time.Time{}
	if deadlineDue {
		s.deadline = Deadline{}
	}
	if safetyDue {
		s.safety = time.Time{}
	}
	return true
}

// finish ends the run. The state is pending if a trigger arrived during it,
// else idle.
func (s *sched) finish() { s.running = false }

// idle reports whether nothing is running and no trigger is pending.
// Deadlines and the safety instant do not count.
func (s *sched) idle() bool { return !s.running && !s.pending }
