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

// PathRowDTO is one path (design §4.1). Matches is null on a delegated row
// whose view C match is unknown; it never falls back to the child's rule.
// Share is set for shown, failing, may-fail and no-traffic shares;
// MirrorPercent only on a mirror row.
type PathRowDTO struct {
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
type SignalDTO struct {
	Kind   string       `json:"kind"`
	Code   string       `json:"code,omitempty"`
	Ref    *LinkedRef   `json:"ref,omitempty"`
	State  health.State `json:"state"`
	Reason string       `json:"reason,omitempty"`
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
type PathSummaryDTO struct {
	PathCountsDTO
	ByState          map[string]PathCountsDTO `json:"byState"`
	EndpointsNotRead bool                     `json:"endpointsNotRead,omitempty"`
	Hidden           bool                     `json:"hidden,omitempty"`
	Unmemoised       bool                     `json:"unmemoised,omitempty"`
}

// PathsResponse is GET …/paths.
type PathsResponse struct {
	Install     string         `json:"install"`
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
	ReadAt  time.Time   `json:"readAt"`
	// Complete is complete, partial, failed, unavailable, stale or not-applicable.
	Complete string `json:"complete"`
	// Ready and Total are absent unless the read establishes a count.
	Ready *int          `json:"ready,omitempty"`
	Total *int          `json:"total,omitempty"`
	Items []EndpointDTO `json:"items,omitempty"`
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
