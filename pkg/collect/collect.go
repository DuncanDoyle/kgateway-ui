// Package collect holds the values long-running collectors publish between
// recomputes (design §4.1 gap 2). A change requests a recompute through
// pkg/trigger; the engine reads a Snapshot per computation and its Print on
// every confirm tick (D-019).
package collect

import (
	"encoding/binary"
	"hash/fnv"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
)

// State is a value's availability.
type State string

// Value states. Collectors set OK by Publish and Failing or Off by Fail;
// Unavailable marks an expired value or one of a stopped collector.
const (
	OK          State = "ok"
	Failing     State = "failing"
	Off         State = "off"
	Unavailable State = "unavailable"
)

// Value is one published value. Data is set only while State is OK.
type Value struct {
	// Data is owned by the store once published: the collector never mutates
	// it afterwards and readers treat it as read-only.
	Data any
	// Fingerprint identifies Data; an equal fingerprint is no change.
	Fingerprint uint64
	// At is when the value was last published or failed.
	At time.Time
	// Expires is when the value turns Unavailable; zero means never.
	Expires time.Time
	State   State
	// Reason explains a state other than OK.
	Reason string
}

// expired reports whether v has expired at now.
func (v Value) expired(now time.Time) bool { return !v.Expires.IsZero() && !now.Before(v.Expires) }

// earlier reports whether expiry a (zero means never) comes before b.
func earlier(a, b time.Time) bool {
	return !a.IsZero() && (b.IsZero() || a.Before(b))
}

// Store holds the collected values by key, "<collector>/<key>". It is safe
// for concurrent use.
type Store struct {
	changes *trigger.Signal
	now     func() time.Time
	mu      sync.Mutex
	values  map[string]Value
	stopped map[string]bool
}

// New returns an empty Store that notifies changes on every change; a nil
// changes never notifies.
func New(changes *trigger.Signal, now func() time.Time) *Store {
	return &Store{changes: changes, now: now, values: map[string]Value{}, stopped: map[string]bool{}}
}

// collectorOf returns the collector part of key.
func collectorOf(key string) string {
	c, _, _ := strings.Cut(key, "/")
	return c
}

// Publish stores data under key with State OK, expiring after ttl (zero
// means never), and takes ownership of data. It notifies only when the
// fingerprint changes, the previous value was not current or the expiry moves
// earlier (or appears), so the engine schedules it. A stopped
// collector's publish is dropped.
func (s *Store) Publish(key string, data any, fingerprint uint64, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped[collectorOf(key)] {
		return
	}
	now := s.now()
	v := Value{Data: data, Fingerprint: fingerprint, At: now, State: OK}
	if ttl > 0 {
		v.Expires = now.Add(ttl)
	}
	old, had := s.values[key]
	s.values[key] = v
	if !had || old.Fingerprint != fingerprint || old.State != OK || old.expired(now) || earlier(v.Expires, old.Expires) {
		s.changes.Notify()
	}
}

// Fail marks key as Failing or Off with reason and drops its data; any other
// st is recorded as Failing. It notifies when the state or reason changes. A
// stopped collector's failure is dropped.
func (s *Store) Fail(key string, st State, reason string) {
	if st != Off {
		st = Failing
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped[collectorOf(key)] {
		return
	}
	old, had := s.values[key]
	s.values[key] = Value{Fingerprint: old.Fingerprint, At: s.now(), State: st, Reason: reason}
	if !had || old.State != st || old.Reason != reason {
		s.changes.Notify()
	}
}

// Stop expires every value of collector and drops its later publishes. It
// notifies when a value changed.
func (s *Store) Stop(collector string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped[collector] = true
	now, changed := s.now(), false
	for k, v := range s.values {
		if collectorOf(k) != collector || v.State == Unavailable {
			continue
		}
		s.values[k] = Value{Fingerprint: v.Fingerprint, At: v.At, Expires: now, State: Unavailable, Reason: "collector stopped"}
		changed = true
	}
	if changed {
		s.changes.Notify()
	}
}

// Snapshot is the store's values at one instant. An expired value is already
// Unavailable in it, without data.
type Snapshot struct {
	// Values is a copy of the map; the values' Data is shared and read-only.
	Values map[string]Value
	// Print covers each key's fingerprint, state, reason and expiry, so an
	// expiry is a change. It is zero for an empty store.
	Print uint64
}

// Snapshot returns the values at the store's now.
func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.values) == 0 {
		return Snapshot{}
	}
	now := s.now()
	out := Snapshot{Values: make(map[string]Value, len(s.values))}
	for k, v := range s.values {
		if v.State == OK && v.expired(now) {
			v = Value{Fingerprint: v.Fingerprint, At: v.At, Expires: v.Expires, State: Unavailable, Reason: "expired"}
		}
		out.Values[k] = v
	}
	out.Print = printOf(out.Values)
	return out
}

// printOf is FNV-64a over the values in key order.
func printOf(values map[string]Value) uint64 {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	h := fnv.New64a()
	var b [8]byte
	u64 := func(v uint64) {
		binary.LittleEndian.PutUint64(b[:], v)
		_, _ = h.Write(b[:])
	}
	str := func(s string) {
		u64(uint64(len(s))) // length prefix keeps adjacent strings apart
		_, _ = h.Write([]byte(s))
	}
	for _, k := range keys {
		v := values[k]
		str(k)
		u64(v.Fingerprint)
		str(string(v.State))
		str(v.Reason)
	}
	return h.Sum64()
}

// Get returns the value under key. A value that is not OK carries its state
// and reason, never data.
func (s Snapshot) Get(key string) (Value, bool) {
	v, ok := s.Values[key]
	return v, ok
}
