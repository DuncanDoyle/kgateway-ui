package diagnose

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
)

// Finding list sort keys (design §13.1). Rank has no descending form.
const (
	SortRank  = "rank"
	SortSince = "since"
	SortCode  = "code"
)

// cursorPrefix keeps findings cursors apart from the explorer's sort keys.
const cursorPrefix = "findings."

// maxCursor is the longest cursor accepted, encoded.
const maxCursor = 1024

// Index holds an install's findings in each list order (design §13.1), as
// indexes into the report's findings. Build makes one per model; it is read-only.
type Index struct {
	rank, since, code []int32
	// byID is every finding index, sorted by finding ID.
	byID []int32
}

// NewIndex returns the index of fs, whose rank order is rank. Since and code
// orders break ties by rank.
func NewIndex(fs []health.Finding, rank []int) *Index {
	ix := &Index{rank: make([]int32, len(rank))}
	pos := make([]int32, len(fs))
	for k, i := range rank {
		ix.rank[k] = int32(i) // #nosec G115 -- a finding count fits in int32
		pos[i] = int32(k)     // #nosec G115 -- as above
	}
	byRank := func(a, b int32) int { return cmp.Compare(pos[a], pos[b]) }
	ix.since = slices.Clone(ix.rank)
	slices.SortStableFunc(ix.since, func(a, b int32) int { return compareSince(fs[a].Since, fs[b].Since) })
	ix.code = slices.Clone(ix.rank)
	slices.SortStableFunc(ix.code, func(a, b int32) int { return cmp.Or(strings.Compare(fs[a].Code, fs[b].Code), byRank(a, b)) })
	ix.byID = slices.Clone(ix.rank)
	slices.SortFunc(ix.byID, func(a, b int32) int { return strings.Compare(fs[a].ID, fs[b].ID) })
	return ix
}

// Find returns the index in fs of the finding with id.
func (ix *Index) Find(fs []health.Finding, id string) (int, bool) {
	if ix == nil {
		return 0, false
	}
	k, ok := slices.BinarySearchFunc(ix.byID, id, func(i int32, id string) int { return strings.Compare(fs[i].ID, id) })
	if !ok {
		return 0, false
	}
	return int(ix.byID[k]), true
}

// Sort is a list order: a sort key, descending when Desc.
type Sort struct {
	Key  string
	Desc bool
}

// Page is one page of findings: indexes into the report's findings. Total
// counts every finding that matches, not only this page.
type Page struct {
	Items      []int
	Total      int
	Next, Prev string
}

// order returns s's order, or false for an unknown key or a descending rank.
func (ix *Index) order(s Sort) ([]int32, bool) {
	switch s.Key {
	case SortRank:
		return ix.rank, !s.Desc
	case SortSince:
		return ix.since, true
	case SortCode:
		return ix.code, true
	}
	return nil, false
}

// wireCursor is the JSON inside a findings cursor, in the explorer's shape
// (design §3.6 of Phase 2): T is the anchor's finding ID and V its position
// in the order, used only when that finding is gone. Neither reveals data a
// restricted row hides.
type wireCursor struct {
	S string   `json:"s"`
	D string   `json:"d"`
	B bool     `json:"b"`
	V int      `json:"v"`
	T []string `json:"t"`
}

func direction(desc bool) string {
	if desc {
		return "desc"
	}
	return "asc"
}

func encodeCursor(s Sort, before bool, id string, at int) string {
	b, _ := json.Marshal(wireCursor{S: cursorPrefix + s.Key, D: direction(s.Desc), B: before, V: at, T: []string{id}}) // strings and ints cannot fail
	return base64.RawURLEncoding.EncodeToString(b)
}

func badCursor(why string) error { return fmt.Errorf("%w: %s", explore.ErrBadCursor, why) }

func decodeCursor(c string, s Sort) (wireCursor, error) {
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
	if w.S != cursorPrefix+s.Key || w.D != direction(s.Desc) {
		return w, badCursor("from another sort")
	}
	if len(w.T) != 1 || w.T[0] == "" || w.V < 0 {
		return w, badCursor("bad anchor")
	}
	return w, nil
}

// Page returns the findings of fs that match, in s's order, limit at most,
// after or before cursor. match sees every finding, restricted ones too, so
// ranks and totals cover them (design §14 rules 17, 18). An unknown sort is
// explore.ErrBadQuery; a cursor that does not decode for s is explore.ErrBadCursor.
func (ix *Index) Page(fs []health.Finding, s Sort, limit int, cursor string, match func(i int) bool) (Page, error) {
	order, ok := ix.order(s)
	if !ok {
		return Page{}, fmt.Errorf("%w: sort %q", explore.ErrBadQuery, s.Key)
	}
	if limit < 1 {
		return Page{}, fmt.Errorf("%w: limit %d", explore.ErrBadQuery, limit)
	}
	n := len(order)
	at := func(k int) int32 { // the finding at position k of s
		if s.Desc {
			return order[n-1-k]
		}
		return order[k]
	}
	// ms are the positions of the matching findings, ascending.
	ms := make([]int, 0, n)
	for k := range n {
		if match == nil || match(int(at(k))) {
			ms = append(ms, k)
		}
	}
	start, end := 0, min(len(ms), limit)
	var cur *wireCursor
	if cursor != "" {
		w, err := decodeCursor(cursor, s)
		if err != nil {
			return Page{}, err
		}
		cur = &w
		// The anchor's current position; a vanished anchor keeps its old one.
		a, found := w.V, false
		if i, ok := ix.Find(fs, w.T[0]); ok {
			for k := range n {
				if int(at(k)) == i {
					a, found = k, true
					break
				}
			}
		}
		if w.B {
			end = sort.SearchInts(ms, a)
			start = max(0, end-limit)
		} else {
			if found {
				a++
			}
			start = sort.SearchInts(ms, a)
			end = min(len(ms), start+limit)
		}
	}
	p := Page{Items: make([]int, 0, end-start), Total: len(ms)}
	for _, k := range ms[start:end] {
		p.Items = append(p.Items, int(at(k)))
	}
	switch {
	case end > start:
		if end < len(ms) {
			p.Next = encodeCursor(s, false, fs[at(ms[end-1])].ID, ms[end-1])
		}
		if start > 0 {
			p.Prev = encodeCursor(s, true, fs[at(ms[start])].ID, ms[start])
		}
	// An empty page past rows that still exist turns back from the client's own anchor.
	case cur != nil && !cur.B && start > 0:
		p.Prev = encodeCursor(s, true, cur.T[0], cur.V)
	case cur != nil && cur.B && end < len(ms):
		p.Next = encodeCursor(s, false, cur.T[0], cur.V)
	}
	return p, nil
}
