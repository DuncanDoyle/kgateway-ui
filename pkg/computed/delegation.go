package computed

import (
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// DelegRoute is an in-scope HTTPRoute as views B and C read it.
type DelegRoute struct {
	Ref        objects.Ref
	Generation int64
	View       *objects.HTTPRouteView
	// Owned is true for a route of the install; TopLevel for an owned route
	// that has a Gateway or ListenerSet parent.
	Owned, TopLevel bool
}

// Root is a route delegation is evaluated from (rules §5.1). FromStatus marks
// a root taken from the controller's status because view A is unknown for it;
// Unknown marks a route that may be a root but cannot be told.
type Root struct {
	Route               objects.Ref
	FromStatus, Unknown bool
	// Deps are the root's eligibility dependencies (rules §5.6).
	Deps []objects.Ref
}

// Bounds are the compute and retention caps of views B and C (rules §5.7).
type Bounds struct {
	// Compute caps per install.
	Edges, Contexts, Matchers int
	// Retention caps: contexts per row and per install.
	ContextsPerRow, ContextsPerInstall int
	// Projection caps: parent edges per route, contexts per edge.
	EffectiveEdges, ContextsPerEdge int
}

// DefaultBounds are the caps of rules §5.7.
var DefaultBounds = Bounds{Edges: 50_000, Contexts: 20_000, Matchers: 200_000,
	ContextsPerRow: 20, ContextsPerInstall: 100_000, EffectiveEdges: 20, ContextsPerEdge: 20}

// DelegationInput is everything views B and C read for one install.
type DelegationInput struct {
	// Routes are every in-scope HTTPRoute, sorted by (namespace, name).
	Routes []DelegRoute
	// Unwatched are the HTTPRoutes outside the scope.
	Unwatched map[objects.Ref]bool
	Roots     []Root
	// Wildcard is the delegation wildcard namespace setting (rules §5.2).
	Wildcard        install.Setting
	ControllerNames []string
	Bounds          Bounds
	// ScopeUnknown makes views B and C scope-unknown: the candidates are a guess (D-024).
	ScopeUnknown bool
}

// DelegationResult is views B and C for one install; fields arrive with those views.
type DelegationResult struct{}

// FitClause names the clause of the default-mode fit that dropped a child
// rule (rules §6).
type FitClause string

// Fit clauses (rules §6).
const (
	// ClausePathType: the parent path type is not PathPrefix.
	ClausePathType FitClause = "path-type"
	// ClausePathPrefix: the child path does not start with the parent's.
	ClausePathPrefix FitClause = "path-prefix"
	// ClauseHeader and ClauseQuery: a parent header or query param is missing from the child.
	ClauseHeader FitClause = "header"
	ClauseQuery  FitClause = "query"
	// ClauseMethod: the child does not have the parent's method.
	ClauseMethod FitClause = "method"
	// ClauseDefaultSlash: omitted matches default to PathPrefix /, outside a parent prefix other than /.
	ClauseDefaultSlash FitClause = "default-slash"
	// ClauseNoMatches: an explicit empty match list in default mode (our call).
	ClauseNoMatches FitClause = "no-matches"
)

// FitFailure is why a child rule was dropped: the clause its first match
// failed. Match is -1 for an explicit empty list. Parent and Child are the
// values the clause compared; Child is empty when the child has none.
type FitFailure struct {
	Rule, Match   int
	Clause        FitClause
	Parent, Child string
}

// EffectiveRule is one child rule under one parent match (view C, rules §6).
// A kept rule has its effective matches; a dropped one has Reason.
type EffectiveRule struct {
	Index   int
	Kept    bool
	Matches []objects.Match
	Reason  *FitFailure
}
