package engine

import (
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
)

// Window is how long a computed disagreement must hold unchanged before it
// raises KGWUI-012 (computed-rules §9.3).
const Window = 30 * time.Second

// window is one candidate check's persistence state.
type window struct {
	print uint64
	since time.Time
	// run is the step that last saw the check as a candidate.
	run uint64
}

// persistence carries check windows across recomputes, as observe carries
// first-seen times (computed-rules §9.3). Only touched under computeMu.
type persistence struct {
	m   map[computed.Identity]window
	run uint64
}

func newPersistence() persistence { return persistence{m: map[computed.Identity]window{}} }

// step applies computed-rules §9.3 steps 1–8 to every install's checks of one
// recompute. It sets State, Mature and Since in place, deletes every entry
// not a candidate this run, and returns the earliest future maturation (zero
// when none). graceEnd gives a missing entry's grace end; nil means none.
// A KGWUI-011 trigger matures like any candidate but keeps state checked.
func (p *persistence) step(now time.Time, checks []computed.CheckResult, graceEnd func(computed.CheckResult) time.Time) (next time.Time) {
	p.run++
	next = p.visit(now, checks, graceEnd)
	p.prune()
	return next
}

// persist is step over each install's checks in place, so no slice is copied.
func (p *persistence) persist(now time.Time, views []*computed.Views, graceEnd func(computed.CheckResult) time.Time) (next time.Time) {
	p.run++
	for _, v := range views {
		if at := p.visit(now, v.Checks, graceEnd); !at.IsZero() && (next.IsZero() || at.Before(next)) {
			next = at
		}
	}
	p.prune()
	return next
}

// visit updates checks' windows and states for run p.run.
func (p *persistence) visit(now time.Time, checks []computed.CheckResult, graceEnd func(computed.CheckResult) time.Time) (next time.Time) {
	for i := range checks {
		c := &checks[i]
		c.Mature, c.Since = false, time.Time{}
		if !c.Candidate() {
			c.State = idleState(c.Class)
			continue
		}
		w, ok := p.m[c.ID]
		if !ok || w.print != c.Fingerprint {
			w = window{print: c.Fingerprint, since: now}
		}
		w.run = p.run
		p.m[c.ID] = w
		c.Since, c.State = w.since, computed.StatePending
		at := w.since.Add(Window)
		if c.Class == computed.ClassTrigger {
			// A trigger waits on no controller output: its window is tracked,
			// its state stays checked (rules §8.3, PF12).
			c.State = computed.StateChecked
		}
		if c.Class == computed.ClassMissing {
			// Without P's current entry it waits on P's status, not on time (step 3).
			if !c.ParentCurrent {
				continue
			}
			if graceEnd != nil {
				if g := graceEnd(*c); g.After(at) {
					at = g
				}
			}
		}
		if now.Before(at) {
			if next.IsZero() || at.Before(next) {
				next = at
			}
			continue
		}
		c.Mature = true
		if c.Class != computed.ClassTrigger {
			c.State = computed.StateDisagrees
		}
	}
	return next
}

// prune deletes the entries no check of this run kept (steps 6 and 7).
func (p *persistence) prune() {
	for id, w := range p.m {
		if w.run != p.run {
			delete(p.m, id)
		}
	}
}

// idleState is the state of a check that is not a candidate (rules §8.3).
func idleState(c computed.Class) computed.State {
	switch c {
	case computed.ClassStale:
		return computed.StatePending
	case computed.ClassUnknown:
		return computed.StateUnknown
	}
	// Compatible, not observable, inconclusive and unverified unexpected status.
	return computed.StateChecked
}
