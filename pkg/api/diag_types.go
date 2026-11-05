package api

import (
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/diagnose"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
)

// FindingRow is one finding of GET …/findings (design §13.3); restricted rows
// keep identity only (§14 rule 11), EvidenceRestricted hides the detail (rule 12).
// It has no docs link: the SPA builds the docs/findings.md anchor from Code.
type FindingRow struct {
	ID                 string          `json:"id"`
	Code               string          `json:"code"`
	Slug               string          `json:"slug"`
	Severity           health.Severity `json:"severity"`
	Resource           *LinkedRef      `json:"resource,omitempty"`
	Message            string          `json:"message,omitempty"`
	Detail             string          `json:"detail,omitempty"`
	Since              *time.Time      `json:"since,omitempty"`
	Group              string          `json:"group,omitempty"`
	Restricted         bool            `json:"restricted,omitempty"`
	EvidenceRestricted bool            `json:"evidenceRestricted,omitempty"`
}

// FindingDetail is GET …/findings/{findingId}; Affected pages through the visible objects of its
// group (§14 rule 13). A restricted or evidence-restricted finding has no Evidence (rule 12).
type FindingDetail struct {
	Install      string          `json:"install"`
	Finding      FindingRow      `json:"finding"`
	Evidence     health.Evidence `json:"evidence,omitempty"`
	SuggestedFix *SuggestedFix   `json:"suggestedFix,omitempty"`
	NextSteps    []NextStep      `json:"nextSteps,omitempty"`
	Affected     Page[LinkedRef] `json:"affected"`
	CollectedAt  time.Time       `json:"collectedAt"`
}

// NextStep is one command a user can run to learn more. Needs names the
// permissions it requires, for example "pods/log".
type NextStep struct {
	Title   string   `json:"title"`
	Command string   `json:"command"`
	Needs   []string `json:"needs,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// SuggestedFix is the one change the evidence leaves (design §7.7). Path is
// the fragment's location when Create is false.
type SuggestedFix struct {
	Title  string    `json:"title"`
	Target LinkedRef `json:"target"`
	Create bool      `json:"create"`
	Path   string    `json:"path,omitempty"`
	YAML   string    `json:"yaml"`
	Basis  string    `json:"basis"`
}

// GroupRow is one ranked group with its rank keys (design §13.2); Code and Title are its top
// member's, and a restricted row has no title, affected list, since or steps (§14 rule 13).
type GroupRow struct {
	ID            string          `json:"id"`
	Title         string          `json:"title"`
	Code          string          `json:"code"`
	Severity      health.Severity `json:"severity"`
	Class         diagnose.Class  `json:"class"`
	Cause         *LinkedRef      `json:"cause,omitempty"`
	Members       int             `json:"members"`
	Affected      []LinkedRef     `json:"affected"`
	AffectedTotal int             `json:"affectedTotal"`
	Since         *time.Time      `json:"since,omitempty"`
	NextSteps     []NextStep      `json:"nextSteps,omitempty"`
	Restricted    bool            `json:"restricted,omitempty"`
	// Grant is the shape of a "Not permitted" group, which has no cause object (design §5.2).
	Grant *GrantShape `json:"grant,omitempty"`
}

// GrantShape is the (from namespace, to namespace, to group and kind) one
// ReferenceGrant allows; ToGroup is "" for the core group.
type GrantShape struct {
	FromNamespace string `json:"fromNamespace"`
	ToNamespace   string `json:"toNamespace"`
	ToGroup       string `json:"toGroup"`
	ToKind        string `json:"toKind"`
}

// Diagnostics is GET …/diagnostics (design §13.3): the verdict, the top
// groups of "Needs attention" and the process and data-plane summaries.
type Diagnostics struct {
	Install string  `json:"install"`
	Verdict Verdict `json:"verdict"`
	// Groups are the first maxGroups of the GroupTotal ranked groups with a problem.
	Groups      []GroupRow           `json:"groups"`
	GroupTotal  int                  `json:"groupTotal"`
	Processes   ProcessSummaries     `json:"processes"`
	DataPlane   DataPlaneSummary     `json:"dataPlane"`
	Checks      []health.CheckResult `json:"checks"`
	CollectedAt time.Time            `json:"collectedAt"`
}

// Verdict is the install's state over every finding (§14 rule 18); Reason names the required check
// that made it unknown, and OldestSince is the oldest since the caller may see.
type Verdict struct {
	State       health.State            `json:"state"`
	Problems    int                     `json:"problems"`
	Counts      map[health.Severity]int `json:"counts"`
	OldestSince *time.Time              `json:"oldestSince,omitempty"`
	Reason      string                  `json:"reason,omitempty"`
}

// ProcessSummaries are the Processes lines of the overview (design §6).
type ProcessSummaries struct {
	Controller ProcessSummary `json:"controller"`
	Proxies    ProxySummary   `json:"proxies"`
}

// ProcessSummary is the controller line. State is the worst controller finding, unknown when not
// observed and none fired; Restricted keeps the counts and state only (§14 rule 16).
type ProcessSummary struct {
	State    health.State `json:"state"`
	Observed bool         `json:"observed"`
	Ready    *int32       `json:"ready,omitempty"`
	Desired  *int32       `json:"desired,omitempty"`
	// Version is the version every visible replica runs; empty when they differ.
	Version    string `json:"version,omitempty"`
	Restricted bool   `json:"restricted,omitempty"`
}

// ProxySummary counts the visible Gateways: those with every proxy ready, with
// a reject, with version skew, with a restart, and whose proxies are not observed.
// State is the worst known health of those Gateways; unknown when none is known.
type ProxySummary struct {
	State       health.State `json:"state"`
	Gateways    int          `json:"gateways"`
	Ready       int          `json:"ready"`
	Rejecting   int          `json:"rejecting"`
	Skew        int          `json:"skew"`
	Restarting  int          `json:"restarting"`
	NotObserved int          `json:"notObserved"`
}

// DataPlaneSummary counts Gateways with a KGWUI-001 over every finding, beside the visible Gateways
// and those a rejecting row names; Unavailable is set while the xDS rejects check is not OK.
type DataPlaneSummary struct {
	Gateways    int  `json:"gateways"`
	Rejecting   int  `json:"rejecting"`
	Unavailable bool `json:"unavailable,omitempty"`
}

// ProcessesResponse is GET …/processes: the controller's processes and a page
// of the Gateways' proxy processes (design §13.1).
type ProcessesResponse struct {
	Install     string                 `json:"install"`
	Controller  ProcessSetDTO          `json:"controller"`
	Proxies     Page[GatewayProcesses] `json:"proxies"`
	CollectedAt time.Time              `json:"collectedAt"`
}

// ProcessSetDTO is the processes behind one role; Observed false is "not observed", never failing.
// Processes lists the Pods the caller may view; Restricted keeps the counts only (§14 rule 16).
type ProcessSetDTO struct {
	Observed   bool         `json:"observed"`
	Desired    *int32       `json:"desired,omitempty"`
	Ready      *int32       `json:"ready,omitempty"`
	Processes  []ProcessDTO `json:"processes"`
	Rollout    *RolloutDTO  `json:"rollout,omitempty"`
	Restricted bool         `json:"restricted,omitempty"`
}

// RolloutDTO is a workload's last rollout; Reason is a stuck one's.
type RolloutDTO struct {
	Progressing bool   `json:"progressing"`
	Reason      string `json:"reason,omitempty"`
}

// ProcessDTO is one replica or proxy; an absent field was not observed.
// RestartsRecent are the restarts its KGWUI-019 counts in the window.
type ProcessDTO struct {
	Name           string                   `json:"name"`
	Ready          *bool                    `json:"ready,omitempty"`
	Restarts       *int32                   `json:"restarts,omitempty"`
	RestartsRecent *int32                   `json:"restartsRecent,omitempty"`
	LastStart      *time.Time               `json:"lastStart,omitempty"`
	Termination    *health.TerminationFacts `json:"termination,omitempty"`
	Waiting        string                   `json:"waiting,omitempty"`
	Version        string                   `json:"version,omitempty"`
	Probe          *health.ProbeFacts       `json:"probe,omitempty"`
}

// GatewayProcesses is one Gateway's proxies: Health is its worst process or data-plane finding, Rejects
// its rejecting streams, Skew a proxy on another controller version, patches included (never a finding).
type GatewayProcesses struct {
	Gateway LinkedRef     `json:"gateway"`
	Health  health.State  `json:"health"`
	Set     ProcessSetDTO `json:"set"`
	Rejects *int          `json:"rejects,omitempty"`
	Skew    bool          `json:"skew,omitempty"`
}

// DataPlaneDTO is a Gateway's data plane (design §13.3). Rejecting says a KGWUI-001 is active;
// its Rejects and RecentlyChanged need every evidence dependency (§14 rule 12), else EvidenceRestricted.
type DataPlaneDTO struct {
	Rejecting          bool             `json:"rejecting"`
	Rejects            []DataPlaneTypes `json:"rejects"`
	StatusSaysHealthy  bool             `json:"statusSaysHealthy"`
	Snapshot           *SnapshotDTO     `json:"snapshot,omitempty"`
	NextSteps          []NextStep       `json:"nextSteps"`
	RecentlyChanged    []LinkedRef      `json:"recentlyChanged"`
	EvidenceRestricted bool             `json:"evidenceRestricted,omitempty"`
	// Unavailable is set while the xDS rejects check is not OK: Rejecting false then means unknown.
	Unavailable bool          `json:"unavailable,omitempty"`
	ProxyStats  ProxyStatsDTO `json:"proxyStats"`
	// Withheld is set when the controller log shows the Gateway's proxies may get no config updates (R105).
	Withheld *WithheldDTO `json:"withheld,omitempty"`
	// ControllerLogs says whether the install's controller log was read; without it, no withheld means unobserved (D-006).
	ControllerLogs string `json:"controllerLogs"`
}

// WithheldDTO is the snapshot deferral that recurred after the proxies
// connected: the first and last such line (kgateway 2.3 and 2.4).
type WithheldDTO struct {
	Message string    `json:"message"`
	First   time.Time `json:"first"`
	Last    time.Time `json:"last"`
}

// DataPlaneTypes is one rejected xDS type: the rejecting streams and when the UI first saw them.
type DataPlaneTypes struct {
	TypeURL   string    `json:"typeUrl"`
	Streams   int       `json:"streams"`
	FirstSeen time.Time `json:"firstSeen"`
}

// SnapshotDTO is kgateway_xds_snapshot_resources for one Gateway. By is always
// "controller": what it built, not what proxies accepted; never a finding.
type SnapshotDTO struct {
	By        string `json:"by"`
	Listeners int    `json:"listeners"`
	Routes    int    `json:"routes"`
	Clusters  int    `json:"clusters"`
	Endpoints int    `json:"endpoints"`
	Secrets   int    `json:"secrets"`
}

// ProxyStatsDTO says whether proxy stats feed a Gateway; Unavailable and Reason say why not (D-034).
type ProxyStatsDTO struct {
	Available   bool                `json:"available"`
	Unavailable health.Availability `json:"unavailable,omitempty"`
	Reason      string              `json:"reason,omitempty"`
}
