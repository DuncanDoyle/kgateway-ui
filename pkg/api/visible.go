package api

import (
	"net/http"

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
}

// view returns the view of the caller of r.
func (h *handler) view(r *http.Request) *view {
	v := &view{a: h.authz, r: r, all: allowsAll(h.authz)}
	if !v.all {
		v.seen = map[objects.Ref]bool{}
	}
	return v
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
