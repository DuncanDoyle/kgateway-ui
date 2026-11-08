package engine

import (
	"context"
	"time"

	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/snellius/pkg/cache"
)

// Source is the source fingerprint of a model (D-019 §2): the cache data it
// was computed from, the served-kinds change it saw and the collector values
// it read, controller metrics included.
type Source struct {
	Cache cache.Fingerprint
	// Kinds is the served-kinds change as kinds.Change.String().
	Kinds string
	// Collected is the collector store's print; zero without collectors.
	Collected uint64
	// Catalog is the served-kind catalog's kinds.Catalog.Key().
	Catalog string
}

// Equal reports whether s and o describe the same source data.
func (s Source) Equal(o Source) bool {
	return s.Kinds == o.Kinds && s.Catalog == o.Catalog && s.Collected == o.Collected && s.Cache.Equal(o.Cache)
}

// Published is one publication: a Model, the Source it was computed from and
// when that source was last confirmed live. It is never mutated after
// publication; a confirmation publishes a new value.
type Published struct {
	Model       *Model
	Source      Source
	ConfirmedAt time.Time
}

// LoopEventKind names a transition of Run's loop.
type LoopEventKind int

// Loop transitions reported to Config.OnLoop.
const (
	// EvTriggered: a trigger was recorded.
	EvTriggered LoopEventKind = iota
	// EvArmed: the loop armed its timer for the next due instant and is about to wait.
	EvArmed
	// EvRunStarted: a recompute started.
	EvRunStarted
	// EvPublished: a recompute published; Published is its value.
	EvPublished
	// EvConfirmed: a confirm tick renewed ConfirmedAt; Published is the new value.
	EvConfirmed
	// EvConfirmRefused: a confirm tick did not confirm, because a run was
	// pending or running, the live source differed, or something newer was published.
	EvConfirmRefused
)

// LoopEvent is one loop transition, at the loop clock's time.
type LoopEvent struct {
	Kind      LoopEventKind
	At        time.Time
	Published *Published
}

// publish makes next current unless a value with a higher Model.Seq already is.
func (e *Engine) publish(next *Published) {
	for {
		cur := e.cur.Load()
		if cur != nil && cur.Model.Seq > next.Model.Seq {
			return
		}
		if e.cur.CompareAndSwap(cur, next) {
			return
		}
	}
}

// confirm publishes prev's Model and Source with ConfirmedAt = now. It fails
// when anything was published after prev was read, so it never restores an
// older model.
func (e *Engine) confirm(prev *Published, now time.Time) bool {
	if e.beforeConfirmSwap != nil {
		e.beforeConfirmSwap()
	}
	return e.cur.CompareAndSwap(prev, &Published{Model: prev.Model, Source: prev.Source, ConfirmedAt: now})
}

// liveSource is the Source the stores, the kinds monitor and the collector
// store would give a computation now.
func (e *Engine) liveSource() Source {
	var src Source
	if f, ok := e.cfg.Snapshots.(cache.Fingerprinter); ok {
		src.Cache = f.Fingerprint()
	} else {
		src.Cache = e.cfg.Snapshots.Snapshot().Source
	}
	if e.cfg.KindsChange != nil {
		src.Kinds = e.cfg.KindsChange().String()
	}
	if e.cfg.Catalog != nil {
		src.Catalog = e.cfg.Catalog().Key()
	}
	if e.cfg.Collected != nil {
		src.Collected = e.cfg.Collected().Print
	}
	return src
}

// emit reports a loop transition to Config.OnLoop.
func (e *Engine) emit(k LoopEventKind, at time.Time, p *Published) {
	if e.cfg.OnLoop != nil {
		e.cfg.OnLoop(LoopEvent{Kind: k, At: at, Published: p})
	}
}

// Run is the scheduler goroutine (D-019 §3). It computes once at start, then
// whenever triggers, deadlines or the safety timer call for it, and runs the
// confirm tick every Config.ConfirmEvery until ctx is done. safety is the
// longest time between recomputes; zero or less disables it. Each recompute
// runs in its own goroutine, so triggers keep arriving during it; the loop
// publishes its result, and a result that arrives after ctx is done is dropped.
func (e *Engine) Run(ctx context.Context, safety time.Duration) {
	clk := e.cfg.Clock
	// Created before any run timer, so a fake clock step that fires both
	// delivers the tick first.
	tick := clk.NewTicker(e.cfg.ConfirmEvery)
	defer tick.Stop()
	var (
		s       sched
		timer   clock.Timer
		armedAt time.Time
		done    = make(chan *Published, 1) // buffered: a run never blocks after Run returns
	)
	stopTimer := func() {
		if timer != nil {
			timer.Stop()
			timer, armedAt = nil, time.Time{}
		}
	}
	defer stopTimer()
	s.setSafety(clk.Now()) // the start-up run
	for {
		now := clk.Now()
		if s.start(now) {
			e.emit(EvRunStarted, now, nil)
			go func() { done <- e.compute() }()
		}
		var timerC <-chan time.Time
		if at, ok := s.wake(); ok {
			d := at.Sub(clk.Now())
			if d <= 0 {
				continue // due already
			}
			if timer == nil || !at.Equal(armedAt) {
				// A new timer each time: a stale fire of the old one is never read.
				stopTimer()
				timer, armedAt = clk.NewTimer(d), at
			}
			timerC = timer.C()
			e.emit(EvArmed, now, nil)
		} else {
			stopTimer()
		}
		// PF14: a ready confirm tick goes before the timer.
		select {
		case <-tick.C():
			e.confirmTick(&s)
			continue
		default:
		}
		select {
		case <-ctx.Done():
			return
		case <-e.cfg.Triggers.C():
			now := clk.Now()
			s.trigger(now)
			e.emit(EvTriggered, now, nil)
		case <-tick.C():
			e.confirmTick(&s)
		case <-timerC:
			stopTimer()
			// A clock step that fires the timer has already delivered its tick.
			select {
			case <-tick.C():
				e.confirmTick(&s)
			default:
			}
		case p := <-done:
			// Published here, so nothing is published after Run returns.
			e.publish(p)
			now := clk.Now()
			s.finish()
			// A run consumes only due deadlines and safety instants; set both again.
			s.setDeadline(p.Model.Next)
			if safety > 0 {
				s.setSafety(now.Add(safety))
			}
			if e.cfg.OnCompute != nil {
				e.cfg.OnCompute(p.Model)
			}
			e.emit(EvPublished, now, p)
		}
	}
}

// confirmTick renews ConfirmedAt when the scheduler is idle and the live
// source equals the published one. A differing source schedules a recompute
// instead, so a change whose trigger has not arrived is still caught.
func (e *Engine) confirmTick(s *sched) {
	now := e.cfg.Clock.Now()
	p := e.cur.Load()
	if p == nil || !s.idle() {
		e.emit(EvConfirmRefused, now, p)
		return
	}
	if !e.liveSource().Equal(p.Source) {
		s.trigger(now)
		e.emit(EvConfirmRefused, now, p)
		return
	}
	if !e.confirm(p, now) {
		e.emit(EvConfirmRefused, now, e.cur.Load())
		return
	}
	e.emit(EvConfirmed, now, e.cur.Load())
}
