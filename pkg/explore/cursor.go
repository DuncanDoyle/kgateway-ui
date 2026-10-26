package explore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// maxCursor is the longest cursor accepted, encoded.
const maxCursor = 1024

// wireCursor is the JSON inside a cursor (design §3.6). B is true for a
// prev cursor; T is the anchor's (namespace, name, group, kind).
type wireCursor struct {
	S string          `json:"s"`
	D string          `json:"d"`
	B bool            `json:"b"`
	V json.RawMessage `json:"v,omitempty"`
	T []string        `json:"t"`
}

// cursor is a decoded cursor: the anchor row's key and the direction.
type cursor struct {
	before bool
	anchor entry
}

func direction(desc bool) string {
	if desc {
		return "desc"
	}
	return "asc"
}

// encodeCursor returns the cursor anchored at e.
func encodeCursor(s Sort, before bool, e entry) string {
	w := wireCursor{S: s.Key, D: direction(s.Desc), B: before, T: []string{e.ref.Namespace, e.ref.Name, e.ref.Group, e.ref.Kind}}
	switch s.Key {
	case sortKind:
		w.V, _ = json.Marshal(e.v.group + "/" + e.v.kind)
	case sortHealth:
		w.V, _ = json.Marshal(healthStates[e.v.n])
	case sortRoutes:
		w.V = strconv.AppendInt(nil, int64(e.v.n), 10)
	}
	b, _ := json.Marshal(w) // marshalling strings and a raw value cannot fail
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeCursor parses c for sort s. Every failure is ErrBadCursor.
func decodeCursor(c string, s Sort) (cursor, error) {
	bad := func(why string) (cursor, error) { return cursor{}, fmt.Errorf("%w: %s", ErrBadCursor, why) }
	if len(c) > maxCursor {
		return bad("too long")
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return bad("not base64url")
	}
	var w wireCursor
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil || dec.More() {
		return bad("not a cursor")
	}
	if w.S != s.Key || w.D != direction(s.Desc) {
		return bad("from another sort")
	}
	if len(w.T) != 4 || w.T[1] == "" || w.T[3] == "" {
		return bad("bad anchor")
	}
	e := entry{ref: objects.Ref{Namespace: w.T[0], Name: w.T[1], Group: w.T[2], Kind: w.T[3]}}
	ok := s.Key == sortName || len(w.V) > 0 && string(w.V) != "null"
	switch s.Key {
	case sortName:
		ok = len(w.V) == 0
	case sortKind:
		var v string
		ok = ok && json.Unmarshal(w.V, &v) == nil
		var found bool
		e.v.group, e.v.kind, found = strings.Cut(v, "/")
		ok = ok && found && e.v.kind != ""
	case sortHealth:
		var v health.State
		ok = ok && json.Unmarshal(w.V, &v) == nil
		ok = ok && validHealth(v)
		e.v.n = healthRank(v)
	case sortRoutes:
		ok = ok && json.Unmarshal(w.V, &e.v.n) == nil && e.v.n >= -1 // -1: no count
	}
	if !ok {
		return bad("bad sort value")
	}
	return cursor{before: w.B, anchor: e}, nil
}
