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
	"sync"
	"time"

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
// indexes into the report's findings. Build makes one per model; it is read-only
// but for the orders other than rank and the search text, each built on first use.
type Index struct {
	rank []int32
	// orders builds since, code and byID; a recompute need not pay for them.
	orders                           sync.Once
	since, sinceDesc, code, codeDesc []int32
	// byID is every finding index, sorted by finding ID.
	byID []int32
	// text is each finding's search text, lowered on the first search.
	textOnce sync.Once
	text     []Text
}

// Text is a finding's search fields, lower-cased. Identity is the resource's
// namespace/name, empty without one; callers decide which fields a caller may match.
type Text struct {
	Identity, Message, Detail string
}

// Text returns the search text of fs, the findings ix was built from, by finding index.
func (ix *Index) Text(fs []health.Finding) []Text {
	ix.textOnce.Do(func() {
		ix.text = make([]Text, len(fs))
		for i := range fs {
			f := &fs[i]
			t := &ix.text[i]
			if f.Resource != nil {
				t.Identity = strings.ToLower(f.Resource.Namespace + "/" + f.Resource.Name)
			}
			t.Message, t.Detail = strings.ToLower(f.Message), strings.ToLower(f.Detail)
		}
	})
	return ix.text
}

// NewIndex returns the index of fs, whose rank order is rank. Since and code
// orders, either direction, break ties by rank; a nil since sorts last.
func NewIndex(fs []health.Finding, rank []int) *Index {
	ix := &Index{rank: make([]int32, len(rank))}
	for k, i := range rank {
		ix.rank[k] = int32(i) // #nosec G115 -- a finding count fits in int32
	}
	return ix
}

// build makes the orders other than rank, over fs, the findings ix was built from.
func (ix *Index) build(fs []health.Finding) {
	ix.orders.Do(func() {
		pos := make([]int32, len(fs))
		for k, i := range ix.rank {
			pos[i] = int32(k) // #nosec G115 -- a finding count fits in int32
		}
		// Dense keys: the comparators below would otherwise chase each finding.
		since := make([]*time.Time, len(fs))
		ids := make([]string, len(fs))
		for i := range fs {
			since[i], ids[i] = fs[i].Since, fs[i].ID
		}
		byRank := func(a, b int32) int { return cmp.Compare(pos[a], pos[b]) }
		sorted := func(c func(a, b int32) int) []int32 {
			o := slices.Clone(ix.rank)
			slices.SortFunc(o, func(a, b int32) int { return cmp.Or(c(a, b), byRank(a, b)) })
			return o
		}
		ix.since = sorted(func(a, b int32) int { return compareSince(since[a], since[b]) })
		ix.sinceDesc = sorted(func(a, b int32) int {
			if sa, sb := since[a], since[b]; sa != nil && sb != nil {
				return sb.Compare(*sa)
			}
			return compareSince(since[a], since[b]) // nil last
		})
		ix.code, ix.codeDesc = byCode(fs, ix.rank)
		ix.byID = slices.Clone(ix.rank)
		slices.SortFunc(ix.byID, func(a, b int32) int { return strings.Compare(ids[a], ids[b]) })
	})
}

// byCode returns rank ordered by code, ascending and descending, ties in rank order.
func byCode(fs []health.Finding, rank []int32) (asc, desc []int32) {
	at := map[string]int{}
	var codes []string
	var buckets [][]int32
	for _, i := range rank {
		k, ok := at[fs[i].Code]
		if !ok {
			k = len(codes)
			at[fs[i].Code] = k
			codes, buckets = append(codes, fs[i].Code), append(buckets, nil)
		}
		buckets[k] = append(buckets[k], i)
	}
	order := make([]int, len(codes))
	for k := range order {
		order[k] = k
	}
	slices.SortFunc(order, func(a, b int) int { return strings.Compare(codes[a], codes[b]) })
	asc, desc = make([]int32, 0, len(rank)), make([]int32, 0, len(rank))
	for _, k := range order {
		asc = append(asc, buckets[k]...)
	}
	for _, k := range slices.Backward(order) {
		desc = append(desc, buckets[k]...)
	}
	return asc, desc
}

// Find returns the index in fs of the finding with id.
func (ix *Index) Find(fs []health.Finding, id string) (int, bool) {
	if ix == nil {
		return 0, false
	}
	ix.build(fs)
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
func (ix *Index) order(fs []health.Finding, s Sort) ([]int32, bool) {
	if s.Key != SortRank {
		ix.build(fs)
	}
	switch s.Key {
	case SortRank:
		return ix.rank, !s.Desc
	case SortSince:
		if s.Desc {
			return ix.sinceDesc, true
		}
		return ix.since, true
	case SortCode:
		if s.Desc {
			return ix.codeDesc, true
		}
		return ix.code, true
	}
	return nil, false
}

// wireCursor is a findings cursor in the explorer's shape: T is the anchor's
// ID, V its position, used when it is gone; neither reveals hidden data.
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

// Page returns up to limit findings of fs that match, in s's order, after or before cursor.
// Bad sorts are explore.ErrBadQuery, bad cursors explore.ErrBadCursor (design §14 rules 17, 18).
func (ix *Index) Page(fs []health.Finding, s Sort, limit int, cursor string, match func(i int) bool) (Page, error) {
	order, ok := ix.order(fs, s)
	if !ok {
		return Page{}, fmt.Errorf("%w: sort %q", explore.ErrBadQuery, s.Key)
	}
	if limit < 1 {
		return Page{}, fmt.Errorf("%w: limit %d", explore.ErrBadQuery, limit)
	}
	n := len(order)
	at := func(k int) int32 { return order[k] } // the finding at position k of s
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
