package trigger

import (
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
)

func TestSignalCoalesces(t *testing.T) {
	s := New()
	s.Notify()
	s.Notify()
	s.Notify()
	testutil.Recv(t, s.C(), "first")
	select {
	case <-s.C():
		t.Fatal("three notifies must deliver once")
	default:
	}
	s.Notify()
	testutil.Recv(t, s.C(), "a notify after the receive")
}

// A nil Signal is a valid "no notifications" value: Notify does nothing and
// C is nil, so a select on it never fires.
func TestNilSignal(t *testing.T) {
	var s *Signal
	s.Notify()
	if s.C() != nil {
		t.Fatal("a nil Signal must return a nil channel")
	}
	select {
	case <-s.C():
		t.Fatal("a nil Signal's channel must never deliver")
	default:
	}
}
