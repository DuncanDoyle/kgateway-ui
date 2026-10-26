package computed

import (
	"cmp"
	"strings"

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

// Unresolved says why a delegation ref selects no routes (rules §5.2). It is a
// code; pkg/api renders the text.
type Unresolved string

// Unresolved codes.
const (
	// UnresolvedMissing: no in-scope route matches the ref.
	UnresolvedMissing Unresolved = "missing"
	// UnresolvedUnwatched: a name ref's target exists outside the controller's scope.
	UnresolvedUnwatched Unresolved = "unwatched"
)

// Allows is which parents a child's parentRefs permit (rules §5.3).
type Allows string

// Allows values.
const (
	// AllowsAny: the child has no parentRefs, so any parent may select it.
	AllowsAny Allows = "any"
	// AllowsThis: a parentRef names the selecting parent.
	AllowsThis Allows = "this"
	// AllowsOther: the parentRefs name other parents only.
	AllowsOther Allows = "other"
)

// DelegationResult is views B and C for one install (rules §5, §6, §8.5).
type DelegationResult struct {
	// Rows are in RowKey order.
	Rows []Row
	// Signals are the parents' ResolvedRefs failures, by (Parent, Key).
	Signals []ParentSignal
	// Reach is (route → permitted selected child) for every in-scope route
	// with delegation refs, from selection and permission alone (rules §8.5).
	// It is sorted by (Parent, Child).
	Reach []Edge
	// Uncertain are the uncertain points of rules §8.5, sorted by (Route, Rule, Ref).
	Uncertain []Uncertain
	// Cap names the first compute cap hit: "", "edges", "contexts" or "matchers".
	Cap string
}

// Compute caps, as DelegationResult.Cap names them (rules §5.7).
const (
	CapEdges    = "edges"
	CapContexts = "contexts"
	CapMatchers = "matchers"
)

// Parent signal reasons: ResolvedRefs=False reasons a delegation failure
// writes on the parent (ProcessBackendError, pkg/kgateway/query/utils.go V3:37-50).
const (
	ReasonBackendNotFound = "BackendNotFound"
	ReasonRefNotPermitted = "RefNotPermitted"
)

// ParentSignal is the ResolvedRefs=False a delegating parent's reporter
// receives (rules §5.4 parentSignal). Reasons are in first-seen order; Deps
// cover every context that recorded a failure.
type ParentSignal struct {
	Parent  objects.Ref
	Key     ReportKey
	Reasons []string
	Deps    []objects.Ref
}

// Edge is a reach graph edge: Parent selects Child and Child permits it.
type Edge struct {
	Parent, Child objects.Ref
}

// Uncertain point rule indexes besides a real rule index (rules §8.5).
const (
	// UncertainRoot: an owned top-level route whose root eligibility is unknown.
	UncertainRoot = -1
	// UncertainPossible: a route an uncertain selection might select.
	UncertainPossible = -2
)

// Uncertain is an uncertain point (rules §8.5): a (route, rule, ref) that was
// cut by a bound or whose selection or context is unknown, or a route with
// Rule UncertainRoot or UncertainPossible, where Ref is -1.
type Uncertain struct {
	Route     objects.Ref
	Rule, Ref int
	Reason    UnknownReason
}

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

// RowOutcome is a delegation row's outcome, per context or aggregated (rules §5.5).
type RowOutcome string

// Row outcomes. Partial is only ever an aggregate.
const (
	OutcomeAttached          RowOutcome = "attached"
	OutcomePartial           RowOutcome = "partial"
	OutcomeDropped           RowOutcome = "dropped"
	OutcomeForbidden         RowOutcome = "forbidden"
	OutcomeRejectedHostnames RowOutcome = "rejected-hostnames"
	OutcomeCycle             RowOutcome = "cycle"
	OutcomeUnresolved        RowOutcome = "unresolved"
	OutcomeUnknown           RowOutcome = "unknown"
)

// RowKey identifies a delegation row: a parent, its rule and ref indexes and
// the selected child. A ref that selects nothing has one row with a zero
// Child (rules §5.5).
type RowKey struct {
	Parent    objects.Ref
	Rule, Ref int
	Child     objects.Ref
}

// Compare orders keys by parent, rule, ref and child.
func (k RowKey) Compare(o RowKey) int {
	return cmp.Or(k.Parent.Compare(o.Parent), cmp.Compare(k.Rule, o.Rule), cmp.Compare(k.Ref, o.Ref), k.Child.Compare(o.Child))
}

// PathStep is a route on a context's path and the match it delegated under (D-024).
type PathStep struct {
	Route objects.Ref
	Match objects.Match
}

// Context is one evaluation of a row under one incoming parent match (rules
// §5.1). Its slices are read-only once added to a row, so they may be shared.
type Context struct {
	ParentMatch objects.Match
	Path        []PathStep
	// Reporter is the report key of the parent's reporter (rules §7).
	Reporter ReportKey
	// Outcome must be set by the caller; an empty or unknown value counts as unknown.
	Outcome RowOutcome
	Unknown UnknownReason
	Reason  *FitFailure
	Rules   []EffectiveRule
	Deps    []objects.Ref
}

// RowMeta is what a row knows before its contexts: the ref as written, the
// child's permission and, for a zero-child row, why the ref selected nothing.
// ScopeUnknown makes the row unknown/scope-unknown even with no contexts (D-024).
type RowMeta struct {
	RefAsWritten objects.DelegationRef
	Allows       Allows
	Unresolved   Unresolved
	ScopeUnknown bool
}

// Row is one delegation row aggregated over its contexts (rules §5.5). Deps,
// Reporters and the digests cover every context, retained or not (rules §5.7).
type Row struct {
	Key          RowKey
	RefAsWritten objects.DelegationRef
	Allows       Allows
	Outcome      RowOutcome
	Unknown      UnknownReason
	// SomeNotComputed: a positive outcome (attached, partial or
	// rejected-hostnames) while some paths were not computed.
	// Unconfirmable: status cannot confirm this row's own outcome.
	SomeNotComputed, Unconfirmable bool
	Reason                         *FitFailure
	Unresolved                     Unresolved
	Contexts                       []Context
	ContextsTruncated              bool
	Deps                           []objects.Ref
	Reporters                      []ReportKey
	ChildKey                       ReportKey
	WritesEntry                    bool
	// ContextDigest is an order-independent digest of every contributing
	// context's incoming match, path, reporter and outcome; KeyDigests is the
	// same digest per reporter. Fingerprints read them (rules §9.2).
	ContextDigest uint64
	KeyDigests    map[ReportKey]uint64
}

// ContextHasher hashes contexts for the row digests with the fingerprint
// encoding (rules §9.2). It reuses its buffers, so one hasher serves one caller.
type ContextHasher struct{ p *printer }

// NewContextHasher returns a ContextHasher.
func NewContextHasher() *ContextHasher { return &ContextHasher{p: newPrinter()} }

// Sum hashes c's incoming match, path, reporter and outcome.
func (h *ContextHasher) Sum(c *Context) uint64 {
	p := h.p
	p.start()
	p.match(c.ParentMatch)
	p.num(int64(len(c.Path)))
	for _, s := range c.Path {
		p.ref(s.Route)
		p.match(s.Match)
	}
	p.reportKey(c.Reporter)
	p.str(string(c.Outcome))
	return p.sum()
}

// Compare orders report keys field by field.
func (k ReportKey) Compare(o ReportKey) int {
	return cmp.Or(strings.Compare(k.Group, o.Group), strings.Compare(k.Kind, o.Kind),
		strings.Compare(k.Namespace, o.Namespace), strings.Compare(k.Name, o.Name), strings.Compare(k.SectionName, o.SectionName))
}
