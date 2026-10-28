package explore

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// maxCursor is the longest cursor accepted, encoded.
const maxCursor = 1024

// wireCursor is the JSON inside a cursor (design §3.6). B is true for a
// prev cursor; T is the anchor's (namespace, name, group, kind). R is a
// delegation anchor's (rule, ref); T is then its child, all empty for none.
type wireCursor struct {
	S string          `json:"s"`
	D string          `json:"d"`
	B bool            `json:"b"`
	V json.RawMessage `json:"v,omitempty"`
	R []int           `json:"r,omitempty"`
	T []string        `json:"t"`
}

// sortDelegation is the delegation rows' only order (design §3.6).
const sortDelegation = "delegation"

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

// badCursor is ErrBadCursor with why.
func badCursor(why string) error { return fmt.Errorf("%w: %s", ErrBadCursor, why) }

// parseCursor decodes c's JSON without checking its fields.
func parseCursor(c string) (wireCursor, error) {
	var w wireCursor
	if len(c) > maxCursor {
		return w, badCursor("too long")
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return w, badCursor("not base64url")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil || dec.More() {
		return w, badCursor("not a cursor")
	}
	return w, nil
}

// decodeCursor parses c for sort s. Every failure is ErrBadCursor.
func decodeCursor(c string, s Sort) (cursor, error) {
	bad := func(why string) (cursor, error) { return cursor{}, badCursor(why) }
	w, err := parseCursor(c)
	if err != nil {
		return cursor{}, err
	}
	if w.S != s.Key || w.D != direction(s.Desc) || w.R != nil {
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

// delegCursor is a decoded delegation cursor: the anchor row's key and the direction.
type delegCursor struct {
	before    bool
	rule, ref int
	child     objects.Ref
}

// compare orders row key k against the anchor, as compareRowKeys does within one parent.
func (c *delegCursor) compare(k *computed.RowKey) int {
	return cmp.Or(cmp.Compare(k.Rule, c.rule), cmp.Compare(k.Ref, c.ref), compareRefs(k.Child, c.child))
}

// key is the anchor as a row key, with no parent.
func (c *delegCursor) key() *computed.RowKey {
	return &computed.RowKey{Rule: c.rule, Ref: c.ref, Child: c.child}
}

// encodeDelegCursor returns the delegation cursor anchored at k.
func encodeDelegCursor(before bool, k *computed.RowKey) string {
	c := k.Child
	w := wireCursor{S: sortDelegation, D: direction(false), B: before, R: []int{k.Rule, k.Ref}, T: []string{c.Namespace, c.Name, c.Group, c.Kind}}
	b, _ := json.Marshal(w) // marshalling strings and ints cannot fail
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeDelegCursor parses a delegation cursor. Every failure is ErrBadCursor.
func decodeDelegCursor(c string) (delegCursor, error) {
	w, err := parseCursor(c)
	if err != nil {
		return delegCursor{}, err
	}
	if w.S != sortDelegation || w.D != direction(false) || len(w.V) > 0 {
		return delegCursor{}, badCursor("from another sort")
	}
	if len(w.R) != 2 || w.R[0] < 0 || w.R[1] < 0 || len(w.T) != 4 {
		return delegCursor{}, badCursor("bad anchor")
	}
	child := objects.Ref{Namespace: w.T[0], Name: w.T[1], Group: w.T[2], Kind: w.T[3]}
	// A row with no child has an all-empty child; any other needs a name and kind.
	if child != (objects.Ref{}) && (child.Name == "" || child.Kind == "") {
		return delegCursor{}, badCursor("bad anchor")
	}
	return delegCursor{before: w.B, rule: w.R[0], ref: w.R[1], child: child}, nil
}
