package api

import (
	"net/http"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Authorizer decides, per request, which objects the caller may see (D-013).
// Findings are filtered by applying it to each finding's resource. Phase 1
// ships AllowAll; per-user authorisation plugs in here later.
type Authorizer interface {
	// Authorize reports whether the caller of r may view the object ref.
	Authorize(r *http.Request, ref objects.Ref) bool
}

// AllowAll lets every caller see everything the UI can read: the Phase 1
// trust model, in which whoever reaches the UI may view all it shows.
type AllowAll struct{}

// Authorize implements Authorizer.
func (AllowAll) Authorize(*http.Request, objects.Ref) bool { return true }

// AuthorizerFunc adapts a function to Authorizer.
type AuthorizerFunc func(r *http.Request, ref objects.Ref) bool

// Authorize implements Authorizer.
func (f AuthorizerFunc) Authorize(r *http.Request, ref objects.Ref) bool { return f(r, ref) }

// allowsAllHook lets tests mark a counting Authorizer as admitting every
// ref; it is nil in production.
var allowsAllHook func(Authorizer) bool

// allowsAll reports whether a is AllowAll, which the API never calls and
// answers from the precomputed model (design §7.4 rule 10). The check is on
// the exact type: a type embedding AllowAll may override Authorize.
func allowsAll(a Authorizer) bool {
	switch a.(type) {
	case AllowAll, *AllowAll:
		return true
	}
	return allowsAllHook != nil && allowsAllHook(a)
}
