package api

import (
	"net/http"
	"sync"

	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// view is what the caller of one request may see (design §7.4). Every
// visibility decision of a request goes through its view, which asks the
// Authorizer about each ref at most once. Under AllowAll it never asks and
// every count is the precomputed one (rule 10). A view is used by one
// goroutine.
type view struct {
	a    Authorizer
	r    *http.Request
	all  bool
	seen map[objects.Ref]bool
	// pooled is set when seen came from seenPool.
	pooled bool
	// deps memoises allowDeps per index of the request's dependency table: 1 visible, -1 hidden.
	deps []int8
}

// view returns the view of the caller of r.
func (h *handler) view(r *http.Request) *view {
	v := &view{a: h.authz, r: r, all: allowsAll(h.authz)}
	if !v.all {
		v.seen = map[objects.Ref]bool{}
	}
	return v
}

// seenPool recycles the memos of route list requests, which ask about
// every row: a memo grown afresh per request was most of their garbage.
// Tests swap it to control reuse.
var seenPool interface {
	Get() any
	Put(any)
} = &sync.Pool{New: func() any { return map[objects.Ref]bool{} }}

// borrow swaps v's memo for one from seenPool; release returns it.
func (v *view) borrow() {
	if v.all || v.pooled {
		return
	}
	v.seen, v.pooled = seenPool.Get().(map[objects.Ref]bool), true
}

// release clears a borrowed memo and returns it to seenPool. v asks the
// Authorizer afresh after it.
func (v *view) release() {
	if !v.pooled {
		return
	}
	clear(v.seen)
	seenPool.Put(v.seen)
	v.seen, v.pooled = map[objects.Ref]bool{}, false
}

// can reports whether the caller may view ref.
func (v *view) can(ref objects.Ref) bool {
	if v.all {
		return true
	}
	ok, asked := v.seen[ref]
	if !asked {
		ok = v.a.Authorize(v.r, ref)
		v.seen[ref] = ok
	}
	return ok
}

// visible is can in the form the explore queries take: nil under AllowAll,
// so they keep their precomputed values.
func (v *view) visible() func(objects.Ref) bool {
	if v.all {
		return nil
	}
	return v.can
}

// allowDeps reports whether the caller may view every dependency in d
// (design §7.4 rule 4). Answers are memoised per ref index; under AllowAll it
// is true without calls (rule 10). A view serves one index.
func (v *view) allowDeps(ix *explore.Index, d explore.DepSet) bool {
	if v.all {
		return true
	}
	if v.deps == nil {
		v.deps = make([]int8, len(ix.Refs))
	}
	for _, i := range d {
		switch v.deps[i] {
		case -1:
			return false
		case 0:
			if !v.can(ix.Ref(i)) {
				v.deps[i] = -1
				return false
			}
			v.deps[i] = 1
		}
	}
	return true
}

// anyOf reports whether the caller may view at least one of refs.
func (v *view) anyOf(refs []objects.Ref) bool {
	for _, ref := range refs {
		if v.can(ref) {
			return true
		}
	}
	return false
}

// allOf reports whether the caller may view every one of refs.
func (v *view) allOf(refs []objects.Ref) bool {
	for _, ref := range refs {
		if !v.can(ref) {
			return false
		}
	}
	return true
}

// recount is the number of ids whose ref the caller may view (rule 5).
func (v *view) recount(ids []explore.RowID, ref func(explore.RowID) objects.Ref) int {
	if v.all {
		return len(ids)
	}
	n := 0
	for _, id := range ids {
		if v.can(ref(id)) {
			n++
		}
	}
	return n
}

// deniedInInstall is the number of the n rows of a listed kind that the
// caller may not view. It takes no filter, so no query changes it, and it
// is the only hidden count the API sends (rule 2).
func (v *view) deniedInInstall(n int, ref func(explore.RowID) objects.Ref) int {
	if v.all {
		return 0
	}
	d := 0
	for i := range n {
		if !v.can(ref(explore.RowID(i))) { // #nosec G115 -- a row count fits in int32
			d++
		}
	}
	return d
}
