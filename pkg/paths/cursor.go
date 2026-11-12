package paths

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"slices"

	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// maxCursor is the longest cursor accepted, encoded; a chain makes it longer than explore's.
const maxCursor = 4096

// wireCursor is the JSON inside a cursor, as pkg/explore's: the sort, the
// direction, B for a prev cursor, V the anchor's sort value, then its row
// identity: problem P, Gateway G, listener L, chain C, R its rule, ref and
// mirror indexes and RowID's place among alike rows, and H RowID's hash. Refs
// are (namespace, name, group, kind).
type wireCursor struct {
	S string       `json:"s"`
	D string       `json:"d"`
	B bool         `json:"b"`
	V []string     `json:"v,omitempty"`
	P health.State `json:"p"`
	G []string     `json:"g"`
	L string       `json:"l"`
	C [][]string   `json:"c"`
	R []int        `json:"r"`
	H uint64       `json:"h"`
}

// anchor is a row's sort key: its sort value and its identity in the
// problem-first order. It need not exist in the table a cursor is used on.
// The last tie-break is the row's content identity, not its index, so a
// cursor keeps its place across topologies that add or drop other rows.
type anchor struct {
	val      sortVal
	state    health.State
	gateway  objects.Ref
	listener string
	chain    []objects.Ref
	rule     int
	// ref and mirror are as in Row; -1 for none.
	ref, mirror int16
	hash        uint64
	dup         uint16
}

// sortVal is a backend or hostname sort value; has is false for none.
type sortVal struct {
	has  bool
	ref  objects.Ref
	host string
}

func direction(desc bool) string {
	if desc {
		return "desc"
	}
	return "asc"
}

func wireRef(r objects.Ref) []string { return []string{r.Namespace, r.Name, r.Group, r.Kind} }

// refOf decodes a wire ref; false unless it has a name and a kind.
func refOf(s []string) (objects.Ref, bool) {
	if len(s) != 4 || s[1] == "" || s[3] == "" {
		return objects.Ref{}, false
	}
	return objects.Ref{Namespace: s[0], Name: s[1], Group: s[2], Kind: s[3]}, true
}

// encodeCursor returns the cursor anchored at a.
func encodeCursor(sort string, desc, before bool, a *anchor) string {
	w := wireCursor{S: sort, D: direction(desc), B: before, P: a.state, G: wireRef(a.gateway), L: a.listener,
		R: []int{a.rule, int(a.ref), int(a.mirror), int(a.dup)}, H: a.hash}
	for _, r := range a.chain {
		w.C = append(w.C, wireRef(r))
	}
	switch {
	case !a.val.has:
	case sort == SortBackend:
		w.V = wireRef(a.val.ref)
	case sort == SortHostname:
		w.V = []string{a.val.host}
	}
	b, _ := json.Marshal(w) // marshalling strings and ints cannot fail
	return base64.RawURLEncoding.EncodeToString(b)
}

// badCursor is explore.ErrBadCursor with why.
func badCursor(why string) error { return fmt.Errorf("%w: %s", explore.ErrBadCursor, why) }

// decodeCursor parses c for sort and desc; before is set for a prev cursor.
// Every failure is explore.ErrBadCursor.
func decodeCursor(c, sort string, desc bool) (a anchor, before bool, err error) {
	if len(c) > maxCursor {
		return a, false, badCursor("too long")
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return a, false, badCursor("not base64url")
	}
	var w wireCursor
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil || dec.More() {
		return a, false, badCursor("not a cursor")
	}
	if w.S != sort || w.D != direction(desc) {
		return a, false, badCursor("from another sort")
	}
	var ok bool
	if a.gateway, ok = refOf(w.G); !ok || !slices.Contains(states, w.P) || len(w.C) == 0 || len(w.R) != 4 ||
		w.R[0] < 0 || w.R[1] < -1 || w.R[2] < -1 || w.R[3] < 0 {
		return a, false, badCursor("bad anchor")
	}
	a.state, a.listener, a.rule, a.hash = w.P, w.L, w.R[0], w.H
	a.ref, a.mirror, a.dup = int16(min(w.R[1], math.MaxInt16)), int16(min(w.R[2], math.MaxInt16)), uint16(min(w.R[3], math.MaxUint16)) // #nosec G115 -- clamped to each type
	for _, s := range w.C {
		r, ok := refOf(s)
		if !ok {
			return a, false, badCursor("bad anchor")
		}
		a.chain = append(a.chain, r)
	}
	switch {
	case len(w.V) == 0:
	case sort == SortBackend:
		a.val.ref, a.val.has = refOf(w.V)
	case sort == SortHostname && len(w.V) == 1:
		a.val.host, a.val.has = w.V[0], true
	}
	if len(w.V) > 0 && !a.val.has {
		return a, false, badCursor("bad sort value")
	}
	return a, w.B, nil
}
