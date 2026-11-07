package health

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"slices"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// sincePrefix keys a finding's first-seen time, by ID, in its check's Store.
const sincePrefix = "\x00since/"

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// FindingID returns "f1-" and 26 base32 characters of the SHA-256 of installID, code,
// the resource (four empty fields when nil) and key, NUL-joined (design §5.1).
func FindingID(installID, code string, resource *objects.Ref, key []string) string {
	var buf [256]byte
	return hashID(identity(buf[:0], installID, code, resource, false, "", key))
}

// GroupID returns the ID of the group of family whose cause is cause and
// causeKey: FindingID with code "G" and the family before the cause key (design §5.2).
func GroupID(installID, family string, cause *objects.Ref, causeKey []string) string {
	var buf [256]byte
	return hashID(identity(buf[:0], installID, "G", cause, true, family, causeKey))
}

// identity appends FindingID's NUL-joined input to b; for a group, family is a key element before key, even when empty.
func identity(b []byte, installID, code string, resource *objects.Ref, group bool, family string, key []string) []byte {
	var r objects.Ref
	if resource != nil {
		r = *resource
	}
	for i, s := range [...]string{installID, code, r.Group, r.Kind, r.Namespace, r.Name} {
		if i > 0 {
			b = append(b, 0)
		}
		b = append(b, s...)
	}
	if group {
		b = append(append(b, 0), family...)
	}
	for _, k := range key {
		b = append(append(b, 0), k...)
	}
	return b
}

// hashID is the ID of identity bytes b.
func hashID(b []byte) string {
	sum := sha256.Sum256(b)
	// 20 bytes encode to 32 characters, whose first 26 equal the full hash's.
	var out [3 + 32]byte
	copy(out[:], "f1-")
	idEncoding.Encode(out[3:], sum[:20])
	return string(out[:3+26])
}

// IDs memoises IDs by their identity bytes across recomputes, so an unchanged
// finding or group is not hashed again. A nil *IDs computes every ID.
// A key is the identity with its first four fields (install, code, group, kind)
// replaced by their index in prefixes, so the many keys sharing them stay short.
type IDs struct {
	m        map[string]*idEntry
	gen      uint32
	buf, key []byte
	prefixes map[string]uint64
	// last is the last prefix and lastIdx its index, which the next ID likely repeats; lastIdx 0 means none.
	last    []byte
	lastIdx uint64
}

type idEntry struct {
	id  string
	gen uint32
}

func newIDs() *IDs { return &IDs{m: map[string]*idEntry{}, prefixes: map[string]uint64{}} }

// FindingID is FindingID, memoised.
func (x *IDs) FindingID(installID, code string, resource *objects.Ref, key []string) string {
	if x == nil {
		return FindingID(installID, code, resource, key)
	}
	return x.get(identity(x.buf[:0], installID, code, resource, false, "", key))
}

// GroupID is GroupID, memoised.
func (x *IDs) GroupID(installID, family string, cause *objects.Ref, causeKey []string) string {
	if x == nil {
		return GroupID(installID, family, cause, causeKey)
	}
	return x.get(identity(x.buf[:0], installID, "G", cause, true, family, causeKey))
}

func (x *IDs) get(b []byte) string {
	x.buf = b
	x.key = x.keyOf(b)
	e := x.m[string(x.key)]
	if e == nil {
		e = &idEntry{id: hashID(b)}
		x.m[string(x.key)] = e
	}
	e.gen = x.gen
	return e.id
}

// keyOf returns b's key: the index of b up to its fourth NUL, then the rest of b.
func (x *IDs) keyOf(b []byte) []byte {
	p, nul := 0, 0
	for p < len(b) && nul < 4 {
		if b[p] == 0 {
			nul++
		}
		p++
	}
	if x.lastIdx == 0 || !bytes.Equal(b[:p], x.last) {
		i, ok := x.prefixes[string(b[:p])]
		if !ok {
			i = uint64(len(x.prefixes)) + 1
			x.prefixes[string(b[:p])] = i
		}
		x.last, x.lastIdx = append(x.last[:0], b[:p]...), i
	}
	return append(binary.AppendUvarint(x.key[:0], x.lastIdx), b[p:]...)
}

// Sweep ends a recompute: once unused entries outnumber used ones, it drops them.
func (x *IDs) Sweep() {
	if x == nil {
		return
	}
	used := 0
	for _, e := range x.m {
		if e.gen == x.gen {
			used++
		}
	}
	if len(x.m)-used > used+64 {
		for k, e := range x.m {
			if e.gen != x.gen {
				delete(x.m, k)
			}
		}
	}
	x.gen++
}

// Discriminator owners of KGWUI-002, KGWUI-003 and KGWUI-011 (design §5.1).
const (
	ownerSelf     = "self"
	ownerParent   = "parent"
	ownerListener = "listener"
	ownerAncestor = "ancestor"
	// ownerStatusParent tags a parent found only in status; rankOwners replaces it.
	ownerStatusParent = "status-parent"
	// computedOwner leads a computed KGWUI-011's key, apart from declared and orphan ones.
	computedOwner = "computed"
)

// refKey is r's group, kind, namespace and name.
func refKey(r objects.Ref) []string { return []string{r.Group, r.Kind, r.Namespace, r.Name} }

// appendWrittenKey appends a parentRef as written to k, keyed as computed-rules §7 keys
// a report, plus the written port, so refs differing only by port stay apart.
func appendWrittenKey(k []string, w objects.WrittenRef) []string {
	port := ""
	if w.PortSet {
		port = strconv.Itoa(int(w.Port))
	}
	return append(k, w.Group, w.Kind, w.Namespace, w.Name, w.SectionName, port)
}

// parentKey is the discriminator owner of route u's parentRef p: p as written
// in u's spec, else a status-only owner from u's status entry for p, else p itself.
func (in Input) parentKey(u *unstructured.Unstructured, p objects.ParentRef) []string {
	var spec []objects.ParentRef
	var written []objects.WrittenRef
	var status []objects.RouteParentStatus
	if gvk := u.GroupVersionKind(); gvk.Group == objects.GatewayAPIGroup && gvk.Kind == "HTTPRoute" {
		v := in.views().HTTPRoute(u)
		spec, written, status = v.ParentRefs, v.WrittenRefs, v.Parents
	} else {
		v := in.views().Route(u)
		spec, written, status = v.ParentRefs, v.WrittenRefs, v.Parents
	}
	if i := slices.Index(spec, p); i >= 0 && i < len(written) {
		return appendWrittenKey([]string{ownerParent}, written[i])
	}
	w := objects.WrittenRef{Group: p.Group, Kind: p.Kind, Namespace: p.Namespace, Name: p.Name, SectionName: p.SectionName, Port: p.Port, PortSet: p.Port != 0}
	if j := slices.IndexFunc(status, func(s objects.RouteParentStatus) bool { return s.ParentRef == p }); j >= 0 {
		w = status[j].Written
	}
	return appendWrittenKey([]string{ownerStatusParent}, w)
}

// ownerLen is the length of an ancestor or status-only parent owner: its tag and six ref fields.
const ownerLen = 7

// rankOwners replaces each status-only owner of fs, a key's first ownerLen elements
// led by tag, with label and "#k": its rank by ref among the distinct such owners of
// one code on one resource. Status is hidden from a restricted row (design §14 rule 11).
func rankOwners(fs []Finding, tag, label string) {
	var is []int
	for i := range fs {
		if k := fs[i].Key; len(k) >= ownerLen && k[0] == tag {
			is = append(is, i)
		}
	}
	if len(is) == 0 {
		return
	}
	same := func(a, b *Finding) int {
		return cmp.Or(cmp.Compare(a.Code, b.Code), compareOptRefs(a.Resource, b.Resource))
	}
	slices.SortFunc(is, func(a, b int) int {
		return cmp.Or(same(&fs[a], &fs[b]), slices.Compare(fs[a].Key[:ownerLen], fs[b].Key[:ownerLen]))
	})
	rank := make([]int, len(is))
	for j := range is {
		rank[j] = 1
		if j > 0 {
			p, f := &fs[is[j-1]], &fs[is[j]]
			if same(p, f) == 0 {
				rank[j] = rank[j-1]
				if !slices.Equal(p.Key[:ownerLen], f.Key[:ownerLen]) {
					rank[j]++
				}
			}
		}
	}
	// Keys are rewritten only after every comparison.
	for j, i := range is {
		f := &fs[i]
		f.Key = append([]string{label + ordinal(rank[j])}, f.Key[ownerLen:]...)
	}
}

// ordinals are the "#n" key elements; ordinal builds the rare ones past them.
var ordinals = func() (o [32]string) {
	for i := range o {
		o[i] = "#" + strconv.Itoa(i+1)
	}
	return o
}()

func ordinal(n int) string {
	if n <= len(ordinals) {
		return ordinals[n-1]
	}
	return "#" + strconv.Itoa(n)
}

// ordinalKeys rekeys the findings of fs for which visible returns ok: the first n
// key elements stay and the rest become "#k", the finding's rank by them among
// those sharing resource and kept prefix. A restricted row's id then hashes only
// what it shows (design §14 rule 11); it can shift when a sibling appears or clears.
func ordinalKeys(fs []Finding, visible func(f *Finding) (n int, ok bool)) {
	type entry struct{ i, n int }
	var es []entry
	for i := range fs {
		if n, ok := visible(&fs[i]); ok {
			es = append(es, entry{i, n})
		}
	}
	if len(es) == 0 {
		return
	}
	// same compares resource and kept prefix; the sort adds the rest of the key.
	same := func(a, b entry) int {
		fa, fb := &fs[a.i], &fs[b.i]
		return cmp.Or(compareOptRefs(fa.Resource, fb.Resource), slices.Compare(fa.Key[:a.n], fb.Key[:b.n]))
	}
	slices.SortFunc(es, func(a, b entry) int {
		return cmp.Or(same(a, b), slices.Compare(fs[a.i].Key[a.n:], fs[b.i].Key[b.n:]))
	})
	rank := make([]int, len(es))
	for j := range es {
		rank[j] = 1
		if j > 0 && same(es[j-1], es[j]) == 0 {
			rank[j] = rank[j-1] + 1
		}
	}
	// Keys are rewritten only after every comparison; each key's array is its finding's own.
	for j, e := range es {
		f := &fs[e.i]
		f.Key = append(f.Key[:e.n], ordinal(rank[j]))
	}
}

// identify sets each finding's ID, then drops its Key, and gives one returned without
// Since its first-seen time from st. It returns how many since entries it read or set.
func identify(install string, now time.Time, fs []Finding, st *Store, ids *IDs) int {
	var key []byte     // sincePrefix + ID, reused so a lookup does not allocate
	var at []time.Time // one backing array for the Since this run gives
	for i := range fs {
		f := &fs[i]
		f.ID, f.Key = ids.FindingID(install, f.Code, f.Resource, f.Key), nil
		if f.Since != nil || st == nil {
			continue
		}
		if at == nil {
			at = make([]time.Time, 0, len(fs)-i)
		}
		key = append(append(key[:0], sincePrefix...), f.ID...)
		t, ok := st.m[string(key)].(time.Time)
		if !ok {
			t = now
			st.Set(string(key), t)
		}
		at = append(at, t)
		f.Since = &at[len(at)-1]
	}
	ids.Sweep()
	return len(at)
}

// keepOf returns the IDs of fs and retained.
func keepOf(fs []Finding, retained map[string]bool) map[string]bool {
	keep := make(map[string]bool, len(fs)+len(retained))
	for id := range retained {
		keep[id] = true
	}
	for i := range fs {
		keep[fs[i].ID] = true
	}
	return keep
}

// SeedSince sets finding id's first-seen time in check's Store, as if it were first seen at at.
func (s *States) SeedSince(check, id string, at time.Time) {
	s.store(check).Set(sincePrefix+id, at)
}

// sinceEntries counts the since entries in st.
func sinceEntries(st *Store) int {
	n := 0
	if st != nil {
		for k := range st.m {
			if strings.HasPrefix(k, sincePrefix) {
				n++
			}
		}
	}
	return n
}

// pruneSince deletes the since entries in st whose ID is not in keep.
func pruneSince(st *Store, keep map[string]bool) {
	if st == nil {
		return
	}
	for k := range st.m {
		if id, ok := strings.CutPrefix(k, sincePrefix); ok && !keep[id] {
			delete(st.m, k)
		}
	}
}
