package api

import (
	"time"

	"github.com/DuncanDoyle/snellius/pkg/health"
)

// Path row actions (design §4.1): what a row's rule does with a request.
// A delegation row is the parent's rule handing requests to child routes.
const (
	ActionBackend        = "backend"
	ActionRedirect       = "redirect"
	ActionDirectResponse = "direct-response"
	ActionNone           = "none"
	ActionDelegate       = "delegate"
)

// Path row notes: a view C state that leaves the row's outcome open (design §4.2).
const (
	NoteUnknown       = "unknown"
	NoteUnconfirmable = "unconfirmable"
	NoteNotComputed   = "not-computed"
)

// NotComputedNoAttachments says the install has no computed views, so no path is derived.
const NotComputedNoAttachments = "no-attachments"

// EndpointsSourceKubernetes is the endpoints source read from the cluster's EndpointSlices.
const EndpointsSourceKubernetes = "kubernetes"

// PathListenerDTO is a path row's listener. Protocol and port are absent
// when the spec has no listener of that name.
type PathListenerDTO struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol,omitempty"`
	Port     int32  `json:"port,omitempty"`
}

// GRPCMethodDTO is a GRPCRoute method match as written.
type GRPCMethodDTO struct {
	Type    string `json:"type,omitempty"`
	Service string `json:"service,omitempty"`
	Method  string `json:"method,omitempty"`
}

// GRPCMatchDTO is one GRPCRouteMatch as written.
type GRPCMatchDTO struct {
	Method  *GRPCMethodDTO `json:"method,omitempty"`
	Headers []KeyMatchDTO  `json:"headers,omitempty"`
}

// PathRowDTO is one path (design §4.1). Matches applies to HTTPRoute rows and
// GRPCMatches to GRPCRoute rows; TCPRoute and TLSRoute rows have neither.
// Matches is null on a delegated row whose view C match is unknown.
// Share is set for shown, failing, may-fail and no-traffic shares;
// MirrorPercent only on a mirror row. ID is the row's identity, unique among
// the rows of one Revision: its attachment, path context, rule, ref and mirror.
type PathRowDTO struct {
	ID          string          `json:"id,omitempty"`
	Gateway     LinkedRef       `json:"gateway"`
	ListenerSet *LinkedRef      `json:"listenerSet,omitempty"`
	Listener    PathListenerDTO `json:"listener"`
	Hostnames   []string        `json:"hostnames"`
	// Routes are the route chain, root first.
	Routes      []LinkedRef       `json:"routes"`
	Delegated   bool              `json:"delegated"`
	Rule        int               `json:"rule"`
	Matches     []MatchDTO        `json:"matches"`
	GRPCMatches []GRPCMatchDTO    `json:"grpcMatches,omitempty"`
	Action      string            `json:"action"`
	Delegation  *DelegationRefDTO `json:"delegation,omitempty"`
	Backend     *LinkedRef        `json:"backend,omitempty"`
	Port        int32             `json:"port,omitempty"`
	Weight      int32             `json:"weight"`
	Share       *float64          `json:"share,omitempty"`
	ShareState  string            `json:"shareState"`
	Mirror      bool              `json:"mirror"`
	// MirrorPercent is the mirror's written percent, not a share.
	MirrorPercent *float64      `json:"mirrorPercent,omitempty"`
	Problem       ProblemDTO    `json:"problem"`
	Endpoints     *EndpointsDTO `json:"endpoints,omitempty"`
	Note          string        `json:"note,omitempty"`
}

// ProblemDTO is a row's worst state and its signals that are not healthy (design §4.5).
type ProblemDTO struct {
	State   health.State `json:"state"`
	Signals []SignalDTO  `json:"signals"`
}

// SignalDTO is one existing signal on a row's chain: its kind, its code (a
// finding code, condition type, capability state or view C reason) and object.
// Title is a finding signal's message, as the findings API shows it.
// Attachment, on a route detail backendRef only, indexes the serves entry an
// attachment's Gateway, listener or parent status signal came from.
// Listener names the listener of Ref a listener's signal is about, waiting ones included.
type SignalDTO struct {
	Kind       string       `json:"kind"`
	Code       string       `json:"code,omitempty"`
	Title      string       `json:"title,omitempty"`
	Ref        *LinkedRef   `json:"ref,omitempty"`
	State      health.State `json:"state"`
	Attachment *int         `json:"attachment,omitempty"`
	Listener   string       `json:"listener,omitempty"`
}

// PathCountsDTO counts filtered visible paths and the distinct routes,
// Gateways and backends they reach.
type PathCountsDTO struct {
	Paths    int `json:"paths"`
	Routes   int `json:"routes"`
	Gateways int `json:"gateways"`
	Backends int `json:"backends"`
}

// PathSummaryDTO summarises the whole filtered result the caller may see
// (design §5, §13). Hidden says some paths were left out for the caller;
// Unmemoised says the topology was over the memo cap and derived per request.
// NotComputed says why there are no paths to compute, such as no computed views.
type PathSummaryDTO struct {
	PathCountsDTO
	ByState          map[string]PathCountsDTO `json:"byState"`
	NotComputed      string                   `json:"notComputed,omitempty"`
	EndpointsNotRead bool                     `json:"endpointsNotRead,omitempty"`
	Hidden           bool                     `json:"hidden,omitempty"`
	Unmemoised       bool                     `json:"unmemoised,omitempty"`
	// Facets are the Gateways and route namespaces of the visible rows,
	// each counted with every filter but its own.
	Facets PathFacetsDTO `json:"facets"`
}

// PathFacetsDTO lists the gateway and namespace filter values, sorted by name.
type PathFacetsDTO struct {
	Gateways   []GatewayFacetDTO   `json:"gateways"`
	Namespaces []NamespaceFacetDTO `json:"namespaces"`
}

// GatewayFacetDTO is a Gateway and the paths through it.
type GatewayFacetDTO struct {
	Ref   LinkedRef `json:"ref"`
	Paths int       `json:"paths"`
}

// NamespaceFacetDTO is a route namespace and the paths with a route in it.
type NamespaceFacetDTO struct {
	Name  string `json:"name"`
	Paths int    `json:"paths"`
}

// PathsResponse is GET …/paths. Revision is a digest of the whole ordered
// result the page was cut from, visible rows only: pages of one walk agree on
// it while the result holds, and a walk whose pages differ must restart.
type PathsResponse struct {
	Install     string         `json:"install"`
	Revision    string         `json:"revision,omitempty"`
	Rows        []PathRowDTO   `json:"rows"`
	Next        string         `json:"next,omitempty"`
	Prev        string         `json:"prev,omitempty"`
	Summary     PathSummaryDTO `json:"summary"`
	CollectedAt time.Time      `json:"collectedAt"`
}

// EndpointsDTO is one backend port's endpoints from one source (design §9.5).
type EndpointsDTO struct {
	Source  string      `json:"source"`
	Port    *PortDTO    `json:"port,omitempty"`
	Runtime *RuntimeDTO `json:"runtime,omitempty"`
	// ReadAt is meaningless when Complete is not-applicable: nothing was read.
	ReadAt time.Time `json:"readAt"`
	// Complete is complete, partial, failed, unavailable, stale or not-applicable;
	// a backend detail's scoped read may also be timed-out or busy.
	Complete string `json:"complete"`
	// Ready and Total are absent unless the read establishes a count.
	Ready *int          `json:"ready,omitempty"`
	Total *int          `json:"total,omitempty"`
	Items []EndpointDTO `json:"items,omitempty"`
	// CachedReady is the cache's ready count, set only when a complete read
	// finds none ready while the cache counts some: the two sources disagree.
	CachedReady *int `json:"cachedReady,omitempty"`
}

// PortDTO is a backend port and its Service port name, when known.
type PortDTO struct {
	Port int32  `json:"port"`
	Name string `json:"name,omitempty"`
}

// RuntimeDTO names the proxy process a per-proxy endpoints source reads
// (#35). Instance changes when the proxy is replaced.
type RuntimeDTO struct {
	Install  string    `json:"install"`
	Gateway  LinkedRef `json:"gateway"`
	Proxy    string    `json:"proxy"`
	Instance string    `json:"instance"`
}

// EndpointDTO is one endpoint of a backend detail read (design §9.3).
type EndpointDTO struct {
	Address     string `json:"address"`
	Port        int32  `json:"port,omitempty"`
	Ready       bool   `json:"ready"`
	Serving     bool   `json:"serving"`
	Terminating bool   `json:"terminating"`
	Pod         string `json:"pod,omitempty"`
}
