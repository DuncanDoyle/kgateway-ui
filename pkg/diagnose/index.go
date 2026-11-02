package diagnose

import (
	"bytes"
	"cmp"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
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

// cursorAD binds a sealed cursor to the findings list of install.
func cursorAD(install string) []byte { return []byte("findings\x00" + install) }

// maxCursor is the longest cursor accepted, encoded.
const maxCursor = 1024

// Index holds an install's findings in each list order (design §13.1), as
// indexes into the report's findings. Build makes one per model; it is read-only
// but for the orders other than rank and the search text, each built on first use.
type Index struct {
	rank   []int32
	groups []Group
	// group is each finding's index in groups, built on first cursor use.
	groupOnce sync.Once
	group     []int32
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

// NewIndex returns the index of fs grouped into groups, whose rank order is
// rank, as Build makes them. Since and code orders, either direction, break
// ties by rank; a nil since sorts last.
func NewIndex(fs []health.Finding, groups []Group, rank []int) *Index {
	ix := &Index{rank: make([]int32, len(rank)), groups: groups}
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

// rankKey is a finding's rank order tuple: its group's keys (compareGroups),
// then its severity, since and ID. A nil since is HasSince false.
type rankKey struct {
	Unranked      bool   `json:"u,omitempty"`
	GroupSev      int    `json:"gs"`
	Class         Class  `json:"gc"`
	Affected      int    `json:"ga"`
	GroupSince    int64  `json:"gt"`
	HasGroupSince bool   `json:"gh,omitempty"`
	Group         string `json:"g"`
	Sev           int    `json:"fs"`
	Since         int64  `json:"ft"`
	HasSince      bool   `json:"fh,omitempty"`
	ID            string `json:"f"`
}

// keyOf returns finding i's rank key and code.
func (ix *Index) keyOf(fs []health.Finding, i int32) (rankKey, string) {
	ix.groupOnce.Do(func() {
		ix.group = make([]int32, len(fs))
		for g := range ix.groups {
			for _, m := range ix.groups[g].Members {
				ix.group[m] = int32(g) // #nosec G115 -- a group count fits in int32
			}
		}
	})
	f, g := &fs[i], &ix.groups[ix.group[i]]
	k := rankKey{Unranked: !g.Ranked, GroupSev: sevRank(g.Severity), Class: g.Class, Affected: len(g.Affected), Group: g.ID,
		Sev: sevRank(f.Severity), ID: f.ID}
	if g.Since != nil {
		k.GroupSince, k.HasGroupSince = g.Since.UnixNano(), true
	}
	if f.Since != nil {
		k.Since, k.HasSince = f.Since.UnixNano(), true
	}
	return k, f.Code
}

// compareNanos orders the oldest time first and a missing one last, as compareSince does.
func compareNanos(a int64, hasA bool, b int64, hasB bool) int {
	if hasA && hasB {
		return cmp.Compare(a, b)
	}
	return cmp.Compare(boolInt(!hasA), boolInt(!hasB))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// compareRank orders rank keys as rank does: compareGroups, then severity, since and ID.
func compareRank(a, b rankKey) int {
	return cmp.Or(
		cmp.Compare(boolInt(a.Unranked), boolInt(b.Unranked)),
		cmp.Compare(a.GroupSev, b.GroupSev),
		cmp.Compare(a.Class, b.Class),
		cmp.Compare(b.Affected, a.Affected),
		compareNanos(a.GroupSince, a.HasGroupSince, b.GroupSince, b.HasGroupSince),
		cmp.Compare(a.Group, b.Group),
		cmp.Compare(a.Sev, b.Sev),
		compareNanos(a.Since, a.HasSince, b.Since, b.HasSince),
		cmp.Compare(a.ID, b.ID),
	)
}

// compareIn orders (k, code) against (w, wcode) in s's order, as build sorts.
func compareIn(s Sort, k rankKey, code string, w rankKey, wcode string) int {
	var c int
	switch {
	case s.Key == SortSince && s.Desc && k.HasSince && w.HasSince:
		c = cmp.Compare(w.Since, k.Since)
	case s.Key == SortSince:
		c = compareNanos(k.Since, k.HasSince, w.Since, w.HasSince)
	case s.Key == SortCode && s.Desc:
		c = cmp.Compare(wcode, code)
	case s.Key == SortCode:
		c = cmp.Compare(code, wcode)
	}
	return cmp.Or(c, compareRank(k, w))
}

// wireCursor is a findings cursor: the sort, the direction and the anchor's
// ordering tuple. It is sealed, as the tuple can hold a hidden finding's since.
type wireCursor struct {
	S    string  `json:"s"`
	D    string  `json:"d"`
	B    bool    `json:"b"`
	Code string  `json:"c"`
	K    rankKey `json:"k"`
}

func direction(desc bool) string {
	if desc {
		return "desc"
	}
	return "asc"
}

// cursorAEAD seals cursors; cursorNonceKey derives each nonce from the plaintext, so an anchor has one cursor.
// Both are random per process: a restart invalidates every cursor.
var cursorAEAD, cursorNonceKey = func() (cipher.AEAD, []byte) {
	key := make([]byte, 64)
	_, _ = rand.Read(key) // never fails (crypto/rand)
	block, err := aes.NewCipher(key[:32])
	if err != nil {
		panic(err)
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return a, key[32:]
}()

func encodeCursor(install string, s Sort, before bool, k rankKey, code string) string {
	b, _ := json.Marshal(wireCursor{S: cursorPrefix + s.Key, D: direction(s.Desc), B: before, Code: code, K: k}) // plain fields cannot fail
	mac := hmac.New(sha256.New, cursorNonceKey)
	_, _ = mac.Write(cursorAD(install))
	_, _ = mac.Write(b)
	nonce := mac.Sum(make([]byte, 0, sha256.Size+len(b)+cursorAEAD.Overhead()))[:cursorAEAD.NonceSize()]
	return base64.RawURLEncoding.EncodeToString(cursorAEAD.Seal(nonce, nonce, b, cursorAD(install)))
}

func badCursor(why string) error { return fmt.Errorf("%w: %s", explore.ErrBadCursor, why) }

func decodeCursor(install, c string, s Sort) (wireCursor, error) {
	var w wireCursor
	if len(c) > maxCursor {
		return w, badCursor("too long")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return w, badCursor("not base64url")
	}
	ns := cursorAEAD.NonceSize()
	if len(sealed) < ns {
		return w, badCursor("not a cursor")
	}
	raw, err := cursorAEAD.Open(nil, sealed[:ns], sealed[ns:], cursorAD(install))
	if err != nil {
		return w, badCursor("not a cursor of this server")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil || dec.More() {
		return w, badCursor("not a cursor")
	}
	if w.S != cursorPrefix+s.Key || w.D != direction(s.Desc) {
		return w, badCursor("from another sort")
	}
	if w.K.ID == "" {
		return w, badCursor("bad anchor")
	}
	return w, nil
}

// Page returns up to limit findings of fs that match, in s's order, after or before cursor.
// A cursor opens only for the install it was minted for.
// A cursor resumes at its anchor's ordering tuple, so a vanished anchor skips nothing.
// Bad sorts are explore.ErrBadQuery, bad cursors explore.ErrBadCursor (design §14 rules 17, 18).
func (ix *Index) Page(fs []health.Finding, install string, s Sort, limit int, cursor string, match func(i int) bool) (Page, error) {
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
		w, err := decodeCursor(install, cursor, s)
		if err != nil {
			return Page{}, err
		}
		cur = &w
		// a is the anchor's insertion point: the first position not before it.
		a, found := sort.Find(n, func(k int) int {
			key, code := ix.keyOf(fs, at(k))
			return compareIn(s, w.K, w.Code, key, code)
		})
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
	anchor := func(before bool, k int) string {
		key, code := ix.keyOf(fs, at(k))
		return encodeCursor(install, s, before, key, code)
	}
	switch {
	case end > start:
		if end < len(ms) {
			p.Next = anchor(false, ms[end-1])
		}
		if start > 0 {
			p.Prev = anchor(true, ms[start])
		}
	// An empty page past rows that still exist turns back from the client's own anchor.
	case cur != nil && !cur.B && start > 0:
		p.Prev = encodeCursor(install, s, true, cur.K, cur.Code)
	case cur != nil && cur.B && end < len(ms):
		p.Next = encodeCursor(install, s, false, cur.K, cur.Code)
	}
	return p, nil
}
