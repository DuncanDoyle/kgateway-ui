package collect

import (
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
)

// notified reports whether sig has a pending notification, consuming it.
func notified(sig *trigger.Signal) bool {
	select {
	case <-sig.C():
		return true
	default:
		return false
	}
}

func TestPublishNotifiesOnlyOnNewFingerprint(t *testing.T) {
	sig := trigger.New()
	c := testutil.FakeClock()
	s := New(sig, c.Now)
	s.Publish("acme/k", "a", 1, 0)
	if !notified(sig) {
		t.Fatal("a first publish must notify")
	}
	s.Publish("acme/k", "a again", 1, 0)
	if notified(sig) {
		t.Fatal("an equal fingerprint must not notify")
	}
	s.Publish("acme/k", "b", 2, 0)
	if !notified(sig) {
		t.Fatal("a new fingerprint must notify")
	}
	v, ok := s.Snapshot().Get("acme/k")
	if !ok || v.Data != "b" || v.State != OK || v.Fingerprint != 2 || !v.At.Equal(testutil.Now) {
		t.Fatalf("got %+v, %v", v, ok)
	}
}

func TestSnapshotPrintIsOrderIndependent(t *testing.T) {
	c := testutil.FakeClock()
	a, b := New(nil, c.Now), New(nil, c.Now)
	a.Publish("acme/x", 1, 10, 0)
	a.Publish("acme/y", 2, 20, 0)
	b.Publish("acme/y", 2, 20, 0)
	b.Publish("acme/x", 1, 10, 0)
	if a.Snapshot().Print != b.Snapshot().Print {
		t.Fatal("the print must not depend on publish order")
	}
	b.Publish("acme/x", 1, 11, 0)
	if a.Snapshot().Print == b.Snapshot().Print {
		t.Fatal("a fingerprint change must change the print")
	}
}

func TestExpiredValueIsUnavailable(t *testing.T) {
	sig := trigger.New()
	c := testutil.FakeClock()
	s := New(sig, c.Now)
	s.Publish("acme/k", "v", 1, 30*time.Second)
	notified(sig)
	fresh := s.Snapshot()
	if v, _ := fresh.Get("acme/k"); v.State != OK || !v.Expires.Equal(testutil.Now.Add(30*time.Second)) {
		t.Fatalf("before expiry: %+v", v)
	}
	c.Step(30*time.Second - time.Second)
	if s.Snapshot().Print != fresh.Print {
		t.Fatal("the print must hold until the expiry")
	}
	c.Step(2 * time.Second) // Expires + 1 s
	late := s.Snapshot()
	v, ok := late.Get("acme/k")
	if !ok || v.State != Unavailable || v.Data != nil || v.Reason == "" {
		t.Fatalf("an expired value must read unavailable without data: %+v", v)
	}
	if late.Print == fresh.Print {
		t.Fatal("an expiry must change the print")
	}
	s.Publish("acme/k", "v", 1, 30*time.Second)
	if !notified(sig) {
		t.Fatal("renewing an expired value must notify")
	}
}

func TestStoppedCollectorValuesExpire(t *testing.T) {
	sig := trigger.New()
	c := testutil.FakeClock()
	s := New(sig, c.Now)
	s.Publish("acme/a", "a", 1, 0)
	s.Publish("acme/b", "b", 2, time.Hour)
	s.Publish("other/a", "o", 3, 0)
	before := s.Snapshot().Print
	notified(sig)
	s.Stop("acme")
	if !notified(sig) {
		t.Fatal("stopping a collector with values must notify")
	}
	snap := s.Snapshot()
	for _, k := range []string{"acme/a", "acme/b"} {
		if v, ok := snap.Get(k); !ok || v.State != Unavailable || v.Data != nil {
			t.Fatalf("%s: %+v", k, v)
		}
	}
	if v, _ := snap.Get("other/a"); v.State != OK || v.Data != "o" {
		t.Fatalf("another collector's value must stay: %+v", v)
	}
	if snap.Print == before {
		t.Fatal("a stop must change the print")
	}
	s.Publish("acme/a", "late", 9, 0)
	if v, _ := s.Snapshot().Get("acme/a"); v.State != Unavailable || notified(sig) {
		t.Fatalf("a stopped collector's publish must be dropped: %+v", v)
	}
}

func TestFailedValueKeepsReason(t *testing.T) {
	sig := trigger.New()
	c := testutil.FakeClock()
	s := New(sig, c.Now)
	s.Publish("acme/k", "v", 1, 0)
	notified(sig)
	s.Fail("acme/k", Failing, "scrape timed out")
	if !notified(sig) {
		t.Fatal("a failure must notify")
	}
	v, ok := s.Snapshot().Get("acme/k")
	if !ok || v.State != Failing || v.Reason != "scrape timed out" || v.Data != nil {
		t.Fatalf("got %+v", v)
	}
	s.Fail("acme/k", Failing, "scrape timed out")
	if notified(sig) {
		t.Fatal("an equal failure must not notify")
	}
	s.Fail("acme/off", Off, "disabled")
	if v, _ := s.Snapshot().Get("acme/off"); v.State != Off || v.Reason != "disabled" {
		t.Fatalf("a never-published key takes its state: %+v", v)
	}
	s.Publish("acme/k", "v", 1, 0)
	if v, _ := s.Snapshot().Get("acme/k"); v.State != OK || v.Data != "v" || v.Reason != "" || !notified(sig) {
		t.Fatalf("a publish recovers a failed key: %+v", v)
	}
}
