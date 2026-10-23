// Package explore builds the per-install index of routes, Gateways and
// ListenerSets that the explorer serves (design §3.2, §3.5). An Index is
// built once per recompute and is immutable once published: handlers filter
// and page it, and never decode objects themselves.
package explore

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
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
	admitted map[objects.Ref]Admission
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
