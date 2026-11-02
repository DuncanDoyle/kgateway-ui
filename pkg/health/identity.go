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

var idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// FindingID returns a finding's stable ID (design §5.1): "f1-" and the first
// 26 lower-case base32 characters of the SHA-256 of installID, code, the
// resource's group, kind, namespace and name, and each key field, joined by
// NUL bytes. A nil resource gives four empty fields. Message, detail,
// severity and since are not part of it. Clients compare IDs, never parse them.
func FindingID(installID, code string, resource *objects.Ref, key []string) string {
	var r objects.Ref
	if resource != nil {
		r = *resource
	}
	n := len(installID) + len(code) + len(r.Group) + len(r.Kind) + len(r.Namespace) + len(r.Name) + 6
	for _, k := range key {
		n += len(k) + 1
	}
	b := make([]byte, 0, n)
	for i, s := range [...]string{installID, code, r.Group, r.Kind, r.Namespace, r.Name} {
		if i > 0 {
			b = append(b, 0)
		}
		b = append(b, s...)
	}
	for _, k := range key {
		b = append(append(b, 0), k...)
	}
	sum := sha256.Sum256(b)
	// 20 bytes encode to 32 characters, whose first 26 equal the full hash's.
	return "f1-" + strings.ToLower(idEncoding.EncodeToString(sum[:20]))[:26]
}

// GroupID returns the ID of the group of family whose cause is cause and
// causeKey: FindingID with code "G" and the family before the cause key (design §5.2).
func GroupID(installID, family string, cause *objects.Ref, causeKey []string) string {
	return FindingID(installID, "G", cause, append([]string{family}, causeKey...))
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

// writtenKey is a parentRef as written, keyed as computed-rules §7 keys a
// report, plus the written port, so refs differing only by port stay apart.
func writtenKey(w objects.WrittenRef) []string {
	port := ""
	if w.PortSet {
		port = strconv.Itoa(int(w.Port))
	}
	return []string{w.Group, w.Kind, w.Namespace, w.Name, w.SectionName, port}
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
	return append([]string{ownerParent}, writtenKey(w)...)
}

// identify sets each finding's ID and gives one returned without Since its
// first-seen time from st. It returns the IDs to keep: those returned and retained.
func identify(install string, now time.Time, fs []Finding, st *Store, retained map[string]bool) map[string]bool {
	keep := make(map[string]bool, len(fs)+len(retained))
	for id := range retained {
		keep[id] = true
	}
	for i := range fs {
		f := &fs[i]
		f.ID = FindingID(install, f.Code, f.Resource, f.Key)
		keep[f.ID] = true
		if f.Since != nil || st == nil {
			continue
		}
		at, ok := Load[time.Time](st, sincePrefix+f.ID)
		if !ok {
			at = now
			st.Set(sincePrefix+f.ID, at)
		}
		f.Since = &at
	}
	return keep
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
