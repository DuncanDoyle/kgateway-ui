package health

import (
	"crypto/sha256"
	"encoding/base32"
	"slices"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
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
type IDs struct {
	m   map[string]*idEntry
	gen uint32
	buf []byte
}

type idEntry struct {
	id  string
	gen uint32
}

func newIDs() *IDs { return &IDs{m: map[string]*idEntry{}} }

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
	e := x.m[string(b)]
	if e == nil {
		e = &idEntry{id: hashID(b)}
		x.m[string(b)] = e
	}
	e.gen = x.gen
	return e.id
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

// Discriminator owners of KGWUI-002 and KGWUI-003 (design §5.1).
const (
	ownerSelf     = "self"
	ownerParent   = "parent"
	ownerListener = "listener"
	ownerAncestor = "ancestor"
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

// parentKey is the discriminator owner of route u's parentRef p: p as
// written in u's spec, else in u's status entry for p, else p itself.
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
	w := objects.WrittenRef{Group: p.Group, Kind: p.Kind, Namespace: p.Namespace, Name: p.Name, SectionName: p.SectionName, Port: p.Port, PortSet: p.Port != 0}
	if i := slices.Index(spec, p); i >= 0 && i < len(written) {
		w = written[i]
	} else if j := slices.IndexFunc(status, func(s objects.RouteParentStatus) bool { return s.ParentRef == p }); j >= 0 {
		w = status[j].Written
	}
	return appendWrittenKey([]string{ownerParent}, w)
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
