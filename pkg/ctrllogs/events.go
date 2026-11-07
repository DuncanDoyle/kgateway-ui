package ctrllogs

import (
	"encoding/binary"
	"hash"
	"hash/fnv"
	"slices"
	"time"

	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// The collected key checks declare (design §8.9).
const (
	CollectorName = "controller-logs"
	ValueKey      = "events"
	Key           = CollectorName + "/" + ValueKey
)

// MaxEvents is how many derived events one install keeps; the oldest go first.
const MaxEvents = 1000

// Event is one derived signal from a controller log line. Every field is from
// a closed set or a known name; log text never is (D-037).
type Event struct {
	Time time.Time
	// Marker is the parser's marker the line matched.
	Marker  string
	Gateway objects.Ref
	TypeURL string
	// Names are Envoy resource names that equal one in the Gateway's proxy stats, sorted.
	Names []string
	Class string
	// Object is the known object a deployer error names, nil when none.
	Object *objects.Ref
	// Frame is a panic's top frame function, from the previous container only.
	Frame string
	// Pod is the ID of the replica whose previous container logged a panic. It is
	// empty on other events, so one line logged by two replicas stays one event.
	Pod string
}

// Logs is the value under Key, per install ID.
type Logs map[string]InstallLogs

// InstallLogs are one install's derived events, oldest first, and how its reading goes.
type InstallLogs struct {
	Events []Event
	State  State
}

// State is how an install's log reading goes.
type State string

// Reading states; Availability ranks on, not connected, failing, unrecognised, denied from best to worst.
const (
	StateOn State = "on"
	// StateNoParser: the install's rule set reads no logs, so nothing is read.
	StateNoParser State = "no-parser"
	// StateFailing: a stream cannot be read.
	StateFailing State = "failing"
	// StateUnrecognised: 10 marked lines in a row failed to parse.
	StateUnrecognised State = "unrecognised"
	// StateDenied: the UI may not read a controller Pod's log.
	StateDenied State = "denied"
	// StatePending: the first round has not connected a stream yet.
	StatePending State = "pending"
	// StateNotConnected: no stream has connected after the first round.
	StateNotConnected State = "not-connected"
)

var stateRank = map[State]int{StateOn: 0, StateNotConnected: 1, StateFailing: 2, StateUnrecognised: 3, StateDenied: 4}

// worse returns the worse of a and b.
func worse(a, b State) State {
	if stateRank[b] > stateRank[a] {
		return b
	}
	return a
}

// Availability is the controllerLogs capability's state from the collected
// value: the worst state of the installs that read logs, no-parser when none
// does, failing when the value is not current, and false before the first
// publication or while every reading install is pending.
func Availability(snap collect.Snapshot) (State, bool) {
	v, ok := snap.Get(Key)
	if !ok {
		return "", false
	}
	logs, _ := v.Data.(Logs)
	if v.State != collect.OK || logs == nil {
		return StateFailing, true
	}
	if len(logs) == 0 {
		// No install yet: nothing is read, and nothing fails.
		return StateOn, true
	}
	st, pending := StateNoParser, false
	for _, l := range logs {
		switch l.State {
		case StateNoParser:
			continue
		case StatePending:
			pending = true
			continue
		}
		if st == StateNoParser {
			st = StateOn
		}
		st = worse(st, l.State)
	}
	if st == StateNoParser && pending {
		return "", false // nothing settled yet
	}
	return st, true
}

// typeURLs are the xDS type URLs an event may carry.
var typeURLs = map[string]bool{
	"type.googleapis.com/envoy.config.listener.v3.Listener":                                                 true,
	"type.googleapis.com/envoy.config.cluster.v3.Cluster":                                                   true,
	"type.googleapis.com/envoy.config.route.v3.RouteConfiguration":                                          true,
	"type.googleapis.com/envoy.config.route.v3.ScopedRouteConfiguration":                                    true,
	"type.googleapis.com/envoy.config.route.v3.VirtualHost":                                                 true,
	"type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment":                                    true,
	"type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.Secret":                                  true,
	"type.googleapis.com/envoy.service.runtime.v3.Runtime":                                                  true,
	"type.googleapis.com/envoy.config.core.v3.TypedExtensionConfig":                                         true,
	"type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager": true,
}

// ring holds one install's events in time order, at most MaxEvents, and
// drops an event equal to one it holds, as a reconnect re-reads its overlap.
type ring struct {
	events []Event
	seen   map[uint64]int
	// stamped marks events the reader's clock timed that no re-read matched yet.
	stamped map[uint64]bool
}

// absorb reports whether a stamped event equal to e but for its time lies in
// [lo, hi], and marks it matched, so each absorbs one re-read.
func (r *ring) absorb(e Event, lo, hi time.Time) bool {
	i, _ := slices.BinarySearchFunc(r.events, lo, func(a Event, t time.Time) int { return a.Time.Compare(t) })
	for ; i < len(r.events) && !r.events[i].Time.After(hi); i++ {
		x := r.events[i]
		k := eventKey(x)
		y := e
		y.Time = x.Time
		if r.stamped[k] && eventKey(y) == k {
			r.stamped[k] = false
			return true
		}
	}
	return false
}

// add inserts e in time order and reports whether it changed the ring;
// stamped means the reader's clock timed it.
func (r *ring) add(e Event, stamped bool) bool {
	k := eventKey(e)
	if r.seen[k] > 0 {
		return false
	}
	i, _ := slices.BinarySearchFunc(r.events, e.Time, func(a Event, t time.Time) int { return a.Time.Compare(t) })
	// After the events with the same time, so arrival order breaks ties.
	for i < len(r.events) && r.events[i].Time.Equal(e.Time) {
		i++
	}
	if len(r.events) >= MaxEvents {
		// The newest panic keeps a reserved slot, so newer events cannot evict it (R102).
		j, reserved := 0, r.newestPanic()
		if newest := e.Class == computed.LogClassPanic && (reserved < 0 || !e.Time.Before(r.events[reserved].Time)); !newest && reserved == 0 {
			j = 1
		}
		if i <= j && e.Class != computed.LogClassPanic {
			return false // older than every event it could replace
		}
		r.forget(r.events[j])
		r.events = slices.Delete(r.events, j, j+1)
		if j < i {
			i--
		}
	}
	r.events = slices.Insert(r.events, i, e)
	if r.seen == nil {
		r.seen, r.stamped = map[uint64]int{}, map[uint64]bool{}
	}
	r.seen[k]++
	if stamped {
		r.stamped[k] = true
	}
	return true
}

// newestPanic is the index of the newest panic event, or -1.
func (r *ring) newestPanic() int {
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i].Class == computed.LogClassPanic {
			return i
		}
	}
	return -1
}

func (r *ring) forget(e Event) {
	k := eventKey(e)
	if r.seen[k]--; r.seen[k] <= 0 {
		delete(r.seen, k)
		delete(r.stamped, k)
	}
}

// eventKey identifies e by every field.
func eventKey(e Event) uint64 {
	w := writer{h: fnv.New64a()}
	w.event(e)
	return w.h.Sum64()
}

// fingerprint is FNV-64a over logs in install ID order. It is never zero.
func fingerprint(logs Logs) uint64 {
	w := writer{h: fnv.New64a()}
	ids := make([]string, 0, len(logs))
	for id := range logs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		w.str(id)
		w.str(string(logs[id].State))
		w.u64(uint64(len(logs[id].Events)))
		for _, e := range logs[id].Events {
			w.event(e)
		}
	}
	return w.h.Sum64() | 1
}

// writer feeds length-prefixed fields to h, so adjacent values stay apart.
type writer struct {
	h hash.Hash64
	b [8]byte
}

func (w *writer) u64(v uint64) {
	binary.LittleEndian.PutUint64(w.b[:], v)
	_, _ = w.h.Write(w.b[:])
}

func (w *writer) str(s string) {
	w.u64(uint64(len(s)))
	_, _ = w.h.Write([]byte(s))
}

func (w *writer) ref(r objects.Ref) {
	w.str(r.Group)
	w.str(r.Kind)
	w.str(r.Namespace)
	w.str(r.Name)
}

func (w *writer) event(e Event) {
	w.u64(uint64(e.Time.UnixNano())) //nolint:gosec // hashed bits
	w.str(e.Marker)
	w.ref(e.Gateway)
	w.str(e.TypeURL)
	w.u64(uint64(len(e.Names)))
	for _, n := range e.Names {
		w.str(n)
	}
	w.str(e.Class)
	if e.Object != nil {
		w.u64(1)
		w.ref(*e.Object)
	} else {
		w.u64(0)
	}
	w.str(e.Frame)
	w.str(e.Pod)
}
