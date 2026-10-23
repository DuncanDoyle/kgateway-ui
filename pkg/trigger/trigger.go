// Package trigger carries coalescing "something changed" notifications from
// event sources to the recompute scheduler (D-019). A notification starts a
// recompute; it proves nothing about the data.
package trigger

// Signal coalesces notifications: any number of Notify calls between two
// receives on C deliver once. A nil *Signal is valid and never delivers.
type Signal struct{ ch chan struct{} }

// New returns a Signal with nothing pending.
func New() *Signal { return &Signal{ch: make(chan struct{}, 1)} }

// Notify marks a change pending. It never blocks and does nothing on a nil Signal.
func (s *Signal) Notify() {
	if s == nil {
		return
	}
	select {
	case s.ch <- struct{}{}:
	default: // one is already pending
	}
}

// C delivers one value per pending notification batch. It is nil for a nil
// Signal, so a select on it blocks forever.
func (s *Signal) C() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.ch
}
