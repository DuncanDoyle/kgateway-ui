// Package explore builds the per-install index of routes, Gateways and
// ListenerSets that the explorer serves (design §3.2, §3.5). An Index is
// built once per recompute and is immutable once published: handlers filter
// and page it, and never decode objects themselves.
package explore

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Input is everything Build reads for one install.
type Input struct {
	Install    install.Resolved
	Owned      *install.Owned
	Assignment install.Assignment
	Snapshot   *cache.Snapshot
	Served     []kinds.Served
	Health     health.Report
	// Views decodes typed views; nil decodes directly.
	Views objects.Decoder
	// Computed is the install's view A; nil computes no members or computed flags.
	Computed *computed.Views
}

// RowID indexes Index.Routes, Index.Gateways or Index.ListenerSets, as the
// field holding it says.
type RowID int32

// Flag is a set of route row condition states (design §6.1).
type Flag uint16

// Flags owned by sub-phase 2.2.
const (
	FlagAccepted Flag = 1 << iota
	FlagNotAccepted
	FlagRefsUnresolved
	FlagNotProgrammed
	FlagPending
	FlagDeclared
	FlagNoParent
	// FlagDisagrees: the row's object has KGWUI-012.
	FlagDisagrees
	// FlagListenerUnknown: a status-accepted parentRef whose outcome is unknown or has no listener.
	FlagListenerUnknown
	// FlagDropped: a view B row of an owned parent selects the route and is
	// dropped, forbidden or rejected-hostnames (2.4).
	FlagDropped
)

// flagNames are the wire names of design §6.1, in bit order.
var flagNames = []struct {
	f    Flag
	name string
}{
	{FlagAccepted, "accepted"},
	{FlagNotAccepted, "not-accepted"},
	{FlagRefsUnresolved, "refs-unresolved"},
	{FlagNotProgrammed, "not-programmed"},
	{FlagPending, "pending"},
	{FlagDeclared, "declared"},
	{FlagNoParent, "no-parent"},
	{FlagDisagrees, "disagrees"},
	{FlagListenerUnknown, "listener-unknown"},
	{FlagDropped, "dropped"},
}

// ParseFlag returns the flag with wire name s.
func ParseFlag(s string) (Flag, bool) {
	for _, n := range flagNames {
		if n.name == s {
			return n.f, true
		}
	}
	return 0, false
}

// Names returns the wire names of the flags in f, in bit order.
func (f Flag) Names() []string {
	out := []string{}
	for _, n := range flagNames {
		if f&n.f != 0 {
			out = append(out, n.name)
		}
	}
	return out
}

// Attachment is how a route relates to one parent, from status only.
type Attachment string

// Attachments (design §7.3 ParentSummary).
const (
	AttachmentAccepted    Attachment = "accepted"
	AttachmentNotAccepted Attachment = "not-accepted"
	AttachmentPending     Attachment = "pending"
	AttachmentUnwatched   Attachment = "unwatched"
	AttachmentDeclared    Attachment = "declared"
)

// ParentLink is one parent of a route row.
type ParentLink struct {
	// Target is the object the parentRef names; Ref is the parentRef after defaults.
	Target objects.Ref
	Ref    objects.ParentRef
	// Via is the Gateway of a ListenerSet target, when the install owns it.
	Via        *objects.Ref
	Attachment Attachment
	// Cond is the deciding condition: the first current False among
	// Accepted, ResolvedRefs and kgateway.dev/Programmed (design §7.3).
	Cond *metav1.Condition
	// Pending is why the status entry is not current, for a pending link.
	Pending string
	// Spec is the index of Ref in the row's spec parentRefs, or -1.
	Spec int
	// Dups are the later spec indexes that resolve to Ref; one written
	// differently has its own report key and check (rules §8.2). Usually nil.
	Dups []int
}

// RouteRow is one route the install lists (design §3.5): an owned route,
// attached or not; an in-scope route naming a missing parent; an unwatched
// route that targets the install; a declared candidate; or a no-parent row.
type RouteRow struct {
	Ref  objects.Ref
	Kind kinds.Served
	Obj  *unstructured.Unstructured
	// View is the decoded HTTPRoute; nil for other kinds.
	View      *objects.HTTPRouteView
	Hostnames []string
	Parents   []ParentLink
	// ParentRoutes are the HTTPRoutes of the row's reported edges.
	ParentRoutes []objects.Ref
	// Children counts the routes with a reported edge to this one.
	Children int
	Flags    Flag
	State    health.State
	// Owned is true when the install owns the route.
	Owned bool
	// Computed is view A per parentRef to a Gateway or ListenerSet, in parentRef order.
	Computed []RefFacts
	// UnknownDeps are the dependencies of FlagListenerUnknown.
	UnknownDeps DepSet
	// DroppedDeps are the dependencies of FlagDropped: the union of the
	// flagging rows' Deps, which leave out the routes on their paths (D-024).
	DroppedDeps DepSet
	// search is the lower-cased "namespace/name" and hostnames.
	search string
}

// BackendUse is one row naming a backend. Mirror is set when the row names
// it only through RequestMirror filters.
type BackendUse struct {
	Row    RowID
	Mirror bool
}

// GatewayRow is one Gateway of the install.
type GatewayRow struct {
	Ref   objects.Ref
	Obj   *unstructured.Unstructured
	View  *objects.GatewayView
	State health.State
	// AttachedRoutes is the sum of status.listeners[].attachedRoutes as written.
	AttachedRoutes int32
	// Routes and NotAccepted index Index.Routes; they include routes
	// through the Gateway's ListenerSets.
	Routes, NotAccepted []RowID
	// ListenerSets index Index.ListenerSets.
	ListenerSets []RowID
	// Members are the distinct member routes of the Gateway's own listeners whose count is known.
	Members []RowID
	// Disagreements counts the checks in state disagrees on the Gateway.
	Disagreements int
}

// ListenerSetRow is one ListenerSet of the install.
type ListenerSetRow struct {
	Ref         objects.Ref
	Obj         *unstructured.Unstructured
	View        *objects.ListenerSetView
	Parent      objects.Ref
	ParentFound bool
	State       health.State
	// AcceptedReason is the reason of an Accepted condition that is not True.
	AcceptedReason string
	// Routes index Index.Routes.
	Routes []RowID
	// Members and Disagreements are as on GatewayRow.
	Members       []RowID
	Disagreements int
}

// Worst is the first error and the first warning finding on one object.
type Worst struct {
	Error, Warning *health.Finding
}

// Index is the explorer's view of one install. It is immutable.
type Index struct {
	// Routes are sorted by (namespace, name, group, kind); a RowID into it is
	// stable within one Index only.
	Routes       []RouteRow
	Gateways     []GatewayRow
	ListenerSets []ListenerSetRow
	// ByParent keys Gateways and ListenerSets; a Gateway key also covers
	// routes through its ListenerSets. It also keys the target of an
	// unwatched row of any kind, an HTTPRoute included.
	ByParent map[objects.Ref][]RowID
	// ByParentRoute keys an HTTPRoute and lists the rows with a reported edge to it.
	ByParentRoute map[objects.Ref][]RowID
	// Declared keys an HTTPRoute and lists the rows that declare it with no reported edge.
	Declared map[objects.Ref][]RowID
	// ByBackend keys backend identity after defaults, Services and Backends
	// alike, from rule backendRefs and RequestMirror filters. Delegation refs
	// are never indexed.
	ByBackend   map[objects.Ref][]BackendUse
	ByNamespace map[string][]RowID
	// ParentRoutes maps a route to the HTTPRoutes of its reported edges.
	ParentRoutes map[objects.Ref][]objects.Ref
	NoParent     []RowID
	// Findings holds the worst error and warning finding per object.
	Findings map[objects.Ref]Worst
	// Refs is the install's dependency table; a DepSet indexes it (design §7.4 rule 10).
	// Refs[i] for i < len(Routes) is route row i's ref.
	Refs []objects.Ref
	// ByListener lists view A's members per listener whose count is known, by row and parentRef.
	ByListener map[ListenerKey][]Member
	// Listeners holds every computed listener count.
	Listeners map[ListenerKey]ListenerFacts
	// RuleSet and RuleNote are view A's rule set and its selection label.
	RuleSet, RuleNote string
	// DelegRows are view B's rows, sorted by (parent, rule, ref, child), each
	// ref by (namespace, name, group, kind).
	DelegRows []DelegRow
	// Delegation maps a parent route to its rows, as DelegRows indexes in order.
	Delegation map[objects.Ref][]int
	// RefRows maps one delegation ref of a parent to its rows, as Delegation does.
	RefRows map[RefKey][]int
	// Effective is view C per child route.
	Effective map[objects.Ref]EffectiveEntry
	// DelegationCap names the first compute cap views B and C hit, or "".
	DelegationCap string
	admitted      map[objects.Ref]Admission
	// parentKinds are the served Gateway and ListenerSet kinds.
	parentKinds map[objects.GroupKind]bool
}

// DepSet is a computed result's dependencies, as indexes into Index.Refs.
type DepSet []int32

// ListenerKey names one listener of a Gateway or ListenerSet.
type ListenerKey struct {
	Resource objects.Ref
	Listener string
}

// Member is a route row on a listener through one parentRef (spec index).
type Member struct {
	Row       RowID
	ParentRef int
	Hostnames []string
	Deps      DepSet
}

// ListenerFacts is a listener's computed count and its cross-check.
type ListenerFacts struct {
	Count         int
	Known         bool
	Unknown       computed.UnknownReason
	Error         string
	IsolationNote bool
	// Check is the listener-count cross-check, shared with the views; nil when none.
	Check *computed.CheckResult
}

// DelegRow is one view B row (rules §5.5) with its dependencies interned.
// Deps and ContextDeps, index-aligned with Contexts, leave out the routes on
// each context's path, which never restrict (design §7.4 rule 4b, D-024).
// Check is the row's child-entry check, joined by (child, parent, ChildKey);
// nil when none.
type DelegRow struct {
	computed.Row
	Deps        DepSet
	ContextDeps []DepSet
	Check       *computed.CheckResult
}

// EffectiveEntry is view C of one child (rules §6). EdgeDeps are index-aligned
// with Edges and ContextDeps with each edge's Contexts; both leave out the
// routes on the paths, as DelegRow's do.
type EffectiveEntry struct {
	computed.ChildEffective
	EdgeDeps    []DepSet
	ContextDeps [][]DepSet
}

// RefKey names one delegation ref of a parent route by rule and ref index.
type RefKey struct {
	Parent    objects.Ref
	Rule, Ref int
}

// ListenerHit is one listener an accepted parentRef attaches to.
type ListenerHit struct {
	Listener  string
	Hostnames []string
	Deps      DepSet
}

// RefFacts is view A's outcome of one parentRef of a route row. Out is the
// source outcome, shared with the views: its ParentRef is the spec index and
// its Resource the target. Listeners and Deps replace Out's.
type RefFacts struct {
	Out       *computed.RefOutcome
	Listeners []ListenerHit
	// Check is the route-accepted cross-check of the ref's report key, shared with the views; nil when none.
	Check *computed.CheckResult
	Deps  DepSet
}

// Facts returns view A's facts for parent link l of row id, or nil.
func (ix *Index) Facts(id RowID, l ParentLink) *RefFacts {
	if l.Spec < 0 {
		return nil
	}
	return ix.Routes[id].facts(l.Spec, l.Target)
}

// DupFacts returns view A's facts for l's spec index l.Dups[k], or nil.
func (ix *Index) DupFacts(id RowID, l ParentLink, k int) *RefFacts {
	return ix.Routes[id].facts(l.Dups[k], l.Target)
}

// Ref returns the ref at index i of the dependency table.
func (ix *Index) Ref(i int32) objects.Ref { return ix.Refs[i] }

// Visible reports whether visible admits every ref of d; nil admits all.
func (ix *Index) Visible(d DepSet, visible func(objects.Ref) bool) bool {
	if visible == nil {
		return true
	}
	for _, i := range d {
		if !visible(ix.Refs[i]) {
			return false
		}
	}
	return true
}

// RowState is the state of ref's row from rep (design §6.3): any error
// finding on ref is error; else any required check not OK is unknown, for
// every row of the install; else any warning finding is warning; else
// healthy. Info findings never set a state.
func RowState(ref objects.Ref, rep health.Report) health.State {
	var w Worst
	for i := range rep.Findings {
		if f := &rep.Findings[i]; f.Resource != nil && *f.Resource == ref {
			w = w.add(f)
		}
	}
	return w.state(requiredNotOK(rep))
}

// add records f if it is the first of its severity.
func (w Worst) add(f *health.Finding) Worst {
	switch {
	case f.Severity == health.SeverityError && w.Error == nil:
		w.Error = f
	case f.Severity == health.SeverityWarning && w.Warning == nil:
		w.Warning = f
	}
	return w
}

// state applies the row rule; unknown is set when a required check is not OK.
func (w Worst) state(unknown bool) health.State {
	switch {
	case w.Error != nil:
		return health.StateError
	case unknown:
		return health.StateUnknown
	case w.Warning != nil:
		return health.StateWarning
	}
	return health.StateHealthy
}

// requiredNotOK reports whether any required check of rep is not OK.
func requiredNotOK(rep health.Report) bool {
	for _, c := range rep.Checks {
		if c.Required && !c.OK {
			return true
		}
	}
	return false
}

// DetailKinds returns the served group/kinds with a detail view: Gateway,
// every ListenerSet kind and HTTPRoute.
func DetailKinds(served []kinds.Served) []objects.GroupKind {
	var out []objects.GroupKind
	for _, k := range served {
		gw := k.Category == kinds.CategoryGateway && k.Group == objects.GatewayAPIGroup && k.Kind == "Gateway"
		if gw || k.Category == kinds.CategoryListenerSet || isHTTPRoute(k.Group, k.Kind) {
			out = append(out, objects.GroupKind{Group: k.Group, Kind: k.Kind})
		}
	}
	return out
}

// isHTTPRoute reports whether group/kind is HTTPRoute.
func isHTTPRoute(group, kind string) bool {
	return group == objects.GatewayAPIGroup && kind == "HTTPRoute"
}
