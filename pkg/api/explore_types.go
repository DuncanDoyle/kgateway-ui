package api

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/overview"
)

// Link is how the SPA may open a reference (design §7.1). Clients render it
// as given and never derive one from a kind.
type Link string

// Links.
const (
	// LinkDetail: admitted, and its kind has a detail view.
	LinkDetail Link = "detail"
	// LinkYAML: admitted without a detail view, or a no-parent row.
	LinkYAML Link = "yaml"
	// LinkNone: denied, absent, never served or not admitted (D-013).
	LinkNone Link = "none"
)

// LinkedRef is a reference with its link.
type LinkedRef struct {
	objects.Ref
	Link Link `json:"link"`
}

// restrictedEvidence replaces a finding's detail when it quotes an object the
// caller may not view (design §7.4 rule 7).
const restrictedEvidence = "Details depend on objects you cannot view"

// FindingDTO is a finding on the wire: its resource carries a link, and
// EvidenceRestricted marks a detail replaced because it quotes a hidden object.
type FindingDTO struct {
	health.Finding
	Resource           *LinkedRef `json:"resource,omitempty"`
	EvidenceRestricted bool       `json:"evidenceRestricted,omitempty"`
}

// HealthResponse is GET …/health. It carries the install ID. Findings
// shadows the report's, so the change from health.Report is additive.
type HealthResponse struct {
	health.Report
	Findings []FindingDTO `json:"findings"`
}

// OverviewGatewayRow is an overview Gateway row with its link.
type OverviewGatewayRow struct {
	overview.GatewayRow
	Link Link `json:"link,omitempty"`
}

// Page is one page of a list (design §7.3). Total counts the visible rows
// that match; DeniedInInstall is the unfiltered number of the install's rows
// of the listed kinds the caller may not view (design §7.4 rule 2).
type Page[T any] struct {
	Install         string    `json:"install"`
	Items           []T       `json:"items"`
	Total           int       `json:"total"`
	Next            string    `json:"next,omitempty"`
	Prev            string    `json:"prev,omitempty"`
	DeniedInInstall int       `json:"deniedInInstall,omitempty"`
	CollectedAt     time.Time `json:"collectedAt"`
}

// ParentSummary is one parent of a route row, from status. Via is the
// Gateway of a ListenerSet parent; ViaRestricted replaces it when the caller
// may not view that Gateway. ConditionType, Reason and Message are the
// deciding condition; a pending parent has only Reason, the pending reason.
type ParentSummary struct {
	Ref           LinkedRef          `json:"ref"`
	SectionName   string             `json:"sectionName,omitempty"`
	Port          int32              `json:"port,omitempty"`
	Via           *LinkedRef         `json:"via,omitempty"`
	ViaRestricted bool               `json:"viaRestricted,omitempty"`
	Attachment    explore.Attachment `json:"attachment"`
	ConditionType string             `json:"conditionType,omitempty"`
	Reason        string             `json:"reason,omitempty"`
	Message       string             `json:"message,omitempty"`
	// Computed is view A's outcome for this parentRef (2.3); nil when none was computed.
	Computed *Attachment `json:"computed,omitempty"`
}

// ComputedBy marks a fact the UI computed (D-020).
const ComputedBy = "ui"

// Computed labels a computed fact with its rule set, rule and check state
// (design §7.3, rules §8.4). By is always ComputedBy. Reason is the unknown
// reason; a restricted fact carries no Rule, Reason or Note (design §7.4 rule 4).
type Computed struct {
	By       string   `json:"by"`
	Rules    string   `json:"rules"`
	Rule     string   `json:"rule,omitempty"`
	State    string   `json:"state"`
	Coverage []string `json:"coverage,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Note     string   `json:"note,omitempty"`
}

// ListenerAttachment is one listener a parentRef attaches to (view A).
// EffectiveHostnames is sent only in state checked or disagrees.
type ListenerAttachment struct {
	Resource           LinkedRef `json:"resource"`
	Listener           string    `json:"listener"`
	EffectiveHostnames []string  `json:"effectiveHostnames,omitempty"`
	Computed           Computed  `json:"computed"`
}

// Attachment is view A's outcome of one parentRef. Outcome is absent when
// the outcome is unknown or restricted; Explanation is set on a rejection.
type Attachment struct {
	Outcome     string               `json:"outcome,omitempty"`
	Listeners   []ListenerAttachment `json:"listeners"`
	Explanation string               `json:"explanation,omitempty"`
	Computed    Computed             `json:"computed"`
}

// RouteAttachment is view A for one Gateway or ListenerSet parentRef of a
// route, by its index in spec.parentRefs.
type RouteAttachment struct {
	ParentRef objects.ParentRef `json:"parentRef"`
	Index     int               `json:"index"`
	Target    LinkedRef         `json:"target"`
	Result    Attachment        `json:"result"`
}

// RouteRow is one row of the routes list. Mirror is set only under a
// backend filter, when every matched use of the row is a RequestMirror.
type RouteRow struct {
	Ref          LinkedRef       `json:"ref"`
	Hostnames    []string        `json:"hostnames"`
	Parents      []ParentSummary `json:"parents"`
	ParentRoutes []LinkedRef     `json:"parentRoutes"`
	Children     int             `json:"children"`
	Flags        []string        `json:"flags"`
	Health       health.State    `json:"health"`
	Mirror       bool            `json:"mirror,omitempty"`
}

// GatewayListRow is one row of the Gateways list (the design's GatewayRow2).
// AttachedRoutes is the status sum as written; NotAccepted and ListenerSets
// count visible rows only.
type GatewayListRow struct {
	Ref            LinkedRef    `json:"ref"`
	Class          string       `json:"class"`
	Health         health.State `json:"health"`
	Listeners      int          `json:"listeners"`
	AttachedRoutes int32        `json:"attachedRoutes"`
	NotAccepted    int          `json:"notAccepted"`
	// Routes is view A's distinct routes on the Gateway's own listeners,
	// hidden ones too; nil when a count is unknown (rule 5).
	Routes       *int `json:"routes,omitempty"`
	ListenerSets int  `json:"listenerSets"`
	// Disagreements counts the Gateway's checks in state disagrees. Like
	// health it is not filtered per caller (rule 9).
	Disagreements int      `json:"disagreements,omitempty"`
	Addresses     []string `json:"addresses"`
}

// ListenerSetRow is one row of the ListenerSets list.
type ListenerSetRow struct {
	Ref            LinkedRef    `json:"ref"`
	Parent         LinkedRef    `json:"parent"`
	Health         health.State `json:"health"`
	Listeners      int          `json:"listeners"`
	AcceptedReason string       `json:"acceptedReason,omitempty"`
	// Routes and Disagreements are as on GatewayListRow.
	Routes        *int `json:"routes,omitempty"`
	Disagreements int  `json:"disagreements,omitempty"`
}

// Condition is a status condition on the wire. Stale marks one whose
// observedGeneration is below the object's generation (Phase 1 freshness).
type Condition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason"`
	Message            string    `json:"message"`
	ObservedGeneration int64     `json:"observedGeneration"`
	LastTransitionTime time.Time `json:"lastTransitionTime"`
	Stale              bool      `json:"stale"`
}

// SpecParentRef is a spec parentRef with its target's link, so a parent
// without a status entry still links through admission (design §7.1).
type SpecParentRef struct {
	objects.ParentRef
	Target LinkedRef `json:"target"`
}

// ParentStatus is one status.parents entry of a route. Ours marks an entry
// written by one of the install's controllers.
type ParentStatus struct {
	ParentRef      objects.ParentRef `json:"parentRef"`
	Target         LinkedRef         `json:"target"`
	ControllerName string            `json:"controllerName"`
	Ours           bool              `json:"ours"`
	Conditions     []Condition       `json:"conditions"`
}

// ListenerTLSDTO is a listener's TLS block. Certificate refs always link
// none: Secrets are never read (design §11).
type ListenerTLSDTO struct {
	Mode            string      `json:"mode"`
	CertificateRefs []LinkedRef `json:"certificateRefs"`
}

// RouteNamespacesDTO is allowedRoutes.namespaces.
type RouteNamespacesDTO struct {
	From     string                `json:"from"`
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// AllowedRoutesDTO is a listener's allowedRoutes.
type AllowedRoutesDTO struct {
	Namespaces RouteNamespacesDTO  `json:"namespaces"`
	Kinds      []objects.GroupKind `json:"kinds"`
}

// ListenerStatusDTO is a listener's status block as written.
type ListenerStatusDTO struct {
	AttachedRoutes int32               `json:"attachedRoutes"`
	SupportedKinds []objects.GroupKind `json:"supportedKinds"`
	Conditions     []Condition         `json:"conditions"`
}

// ListenerDTO is one spec listener with its status block, when there is one.
type ListenerDTO struct {
	Name          string             `json:"name"`
	Port          int32              `json:"port,omitempty"`
	Protocol      string             `json:"protocol"`
	Hostname      string             `json:"hostname,omitempty"`
	TLS           *ListenerTLSDTO    `json:"tls,omitempty"`
	AllowedRoutes AllowedRoutesDTO   `json:"allowedRoutes"`
	Status        *ListenerStatusDTO `json:"status,omitempty"`
	// ComputedRoutes is view A's count, hidden members too; nil when unknown (rule 5).
	ComputedRoutes *int      `json:"computedRoutes,omitempty"`
	Computed       *Computed `json:"computed,omitempty"`
	// IsolationNote: Envoy may serve fewer hostnames on this listener (rules §4.5).
	IsolationNote bool `json:"isolationNote,omitempty"`
}

// RouteCounts counts the visible routes under a parent per attachment, as
// the routes list with that parent and attachment totals them.
type RouteCounts struct {
	Accepted    int `json:"accepted"`
	NotAccepted int `json:"notAccepted"`
	Pending     int `json:"pending"`
	Unwatched   int `json:"unwatched"`
}

// Candidate says why an object the install does not own has a detail page.
// By is the visible owned object that names a referenced candidate.
type Candidate struct {
	Reason explore.CandidateReason `json:"reason"`
	By     *LinkedRef              `json:"by,omitempty"`
}

// GatewayDetail is GET …/gateways/{ns}/{name}. Its ListenerSets are paged
// through the ListenerSets list, never embedded; ListenerSets is that list's
// total for the caller (design §7.4 rule 5).
type GatewayDetail struct {
	Install              string        `json:"install"`
	Ref                  LinkedRef     `json:"ref"`
	Generation           int64         `json:"generation"`
	Class                string        `json:"class"`
	Health               health.State  `json:"health"`
	Candidate            *Candidate    `json:"candidate,omitempty"`
	RuleSet              string        `json:"ruleSet,omitempty"`
	Conditions           []Condition   `json:"conditions"`
	Addresses            []string      `json:"addresses"`
	Listeners            []ListenerDTO `json:"listeners"`
	AttachedListenerSets *int32        `json:"attachedListenerSets,omitempty"`
	ListenerSets         int           `json:"listenerSets"`
	RouteCounts          RouteCounts   `json:"routeCounts"`
	CollectedAt          time.Time     `json:"collectedAt"`
}

// AllowedListenersDTO is the parent Gateway's spec.allowedListeners as
// written. CRDHint is set whenever the field is absent: the UI reads no
// CRDs, so it cannot tell an unset field from one the CRD lacks (design §8.5).
type AllowedListenersDTO struct {
	Set     bool           `json:"set"`
	Value   map[string]any `json:"value,omitempty"`
	CRDHint bool           `json:"crdHint"`
}

// ListenerSetDetail is GET …/listenersets/{group}/{kind}/{ns}/{name}.
// ParentFound is false when the parent does not open for the caller
// (absent, hidden or unadmitted). ParentAllowedListeners is sent only while
// the ListenerSet's current Accepted condition is False with reason
// NotAllowed and its parent is found.
type ListenerSetDetail struct {
	Install                string               `json:"install"`
	Ref                    LinkedRef            `json:"ref"`
	Generation             int64                `json:"generation"`
	Health                 health.State         `json:"health"`
	Candidate              *Candidate           `json:"candidate,omitempty"`
	RuleSet                string               `json:"ruleSet,omitempty"`
	Parent                 LinkedRef            `json:"parent"`
	ParentFound            bool                 `json:"parentFound"`
	Conditions             []Condition          `json:"conditions"`
	Listeners              []ListenerDTO        `json:"listeners"`
	ExperimentalHint       string               `json:"experimentalHint,omitempty"`
	RouteCounts            RouteCounts          `json:"routeCounts"`
	ParentAllowedListeners *AllowedListenersDTO `json:"parentAllowedListeners,omitempty"`
	CollectedAt            time.Time            `json:"collectedAt"`
}

// RouteAnnotations are the route annotations the UI reads. A nil field is
// an absent annotation.
type RouteAnnotations struct {
	InheritParentMatcher *bool `json:"inheritParentMatcher,omitempty"`
}

// RouteDetail is GET …/routes/{group}/{kind}/{ns}/{name}, HTTPRoute only.
// ChildCount and DeclaredCount count visible routes only.
// Effective is view C per parent edge (2.4); EffectiveTruncated says the
// route had more parent edges than were kept.
type RouteDetail struct {
	Install            string            `json:"install"`
	Ref                LinkedRef         `json:"ref"`
	Generation         int64             `json:"generation"`
	Health             health.State      `json:"health"`
	Candidate          *Candidate        `json:"candidate,omitempty"`
	RuleSet            string            `json:"ruleSet,omitempty"`
	Hostnames          []string          `json:"hostnames"`
	Annotations        RouteAnnotations  `json:"annotations"`
	ParentRefs         []SpecParentRef   `json:"parentRefs"`
	Parents            []ParentStatus    `json:"parents"`
	Attachments        []RouteAttachment `json:"attachments,omitempty"`
	Rules              []RuleDTO         `json:"rules"`
	Effective          []EffectiveDTO    `json:"effective,omitempty"`
	EffectiveTruncated bool              `json:"effectiveTruncated,omitempty"`
	Ancestors          []AncestorPathDTO `json:"ancestors"`
	AncestorsTruncated bool              `json:"ancestorsTruncated"`
	ChildCount         int               `json:"childCount"`
	DeclaredCount      int               `json:"declaredCount"`
	CollectedAt        time.Time         `json:"collectedAt"`
}

// PathMatchDTO is a path match after defaults.
type PathMatchDTO struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// KeyMatchDTO is a header or query parameter match after defaults.
type KeyMatchDTO struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// MatchDTO is one HTTPRouteMatch after defaults.
type MatchDTO struct {
	Path        *PathMatchDTO `json:"path,omitempty"`
	Headers     []KeyMatchDTO `json:"headers,omitempty"`
	QueryParams []KeyMatchDTO `json:"queryParams,omitempty"`
	Method      string        `json:"method,omitempty"`
}

// RuleDTO is one spec rule. Omitted matches carry the CRD default, one
// PathPrefix "/" match. An explicit empty list stays [] as written; kgateway
// treats it as match-all (or, for an inheriting child, the parent's match),
// so clients must not render it as "matches nothing".
type RuleDTO struct {
	Index       int             `json:"index"`
	Name        string          `json:"name,omitempty"`
	Matches     []MatchDTO      `json:"matches"`
	Filters     []FilterDTO     `json:"filters"`
	BackendRefs []BackendRefDTO `json:"backendRefs"`
}

// FractionDTO is a mirror fraction; the denominator defaults to 100.
type FractionDTO struct {
	Numerator   int32 `json:"numerator"`
	Denominator int32 `json:"denominator"`
}

// MirrorDTO is a RequestMirror filter's target. Port is omitted when absent.
type MirrorDTO struct {
	BackendRef LinkedRef    `json:"backendRef"`
	Port       int32        `json:"port,omitempty"`
	Percent    *int32       `json:"percent,omitempty"`
	Fraction   *FractionDTO `json:"fraction,omitempty"`
}

// FilterDTO is one filter in written order; Config is the filter as written.
type FilterDTO struct {
	Type         string         `json:"type"`
	ExtensionRef *LinkedRef     `json:"extensionRef,omitempty"`
	Mirror       *MirrorDTO     `json:"mirror,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
}

// Delegation ref states (design §7.3), status-based, for name refs only.
const (
	DelegationReported    = "reported"
	DelegationNotReported = "not-reported"
	DelegationNotFound    = "not-found"
	DelegationRestricted  = "restricted"
)

// DelegationRefDTO is a delegation backendRef as classified. Selected and
// Attached count the visible children of view B's rows (2.4, design §7.4
// rule 5); each is nil when the count is not known to the caller. Computed
// merges the rows' checks worst first (D-023 decision 2, PF14).
type DelegationRefDTO struct {
	Mode      objects.DelegationMode `json:"mode"`
	Namespace string                 `json:"namespace"`
	Name      string                 `json:"name,omitempty"`
	Label     string                 `json:"label,omitempty"`
	State     string                 `json:"state,omitempty"`
	Selected  *int                   `json:"selected,omitempty"`
	Attached  *int                   `json:"attached,omitempty"`
	Computed  *Computed              `json:"computed,omitempty"`
}

// BackendStateDTO is a Backend's state and Accepted condition, or only
// Restricted when the caller may not view the Backend (design §7.4 rule 3).
type BackendStateDTO struct {
	State      health.State `json:"state,omitempty"`
	Accepted   *Condition   `json:"accepted,omitempty"`
	Restricted bool         `json:"restricted,omitempty"`
}

// BackendRefDTO is one rule backendRef. An absent weight is 1 with
// WeightDefaulted; an absent port is omitted.
type BackendRefDTO struct {
	Ref             LinkedRef         `json:"ref"`
	Port            int32             `json:"port,omitempty"`
	Weight          int32             `json:"weight"`
	WeightDefaulted bool              `json:"weightDefaulted,omitempty"`
	Filters         []FilterDTO       `json:"filters"`
	Delegation      *DelegationRefDTO `json:"delegation,omitempty"`
	Backend         *BackendStateDTO  `json:"backend,omitempty"`
}

// AncestorStepDTO is one up-chain step: a ref, or Restricted with no
// identity. A hidden ancestor route has link none and Matches, the matches
// it delegated under in view B, and nothing else (design §7.4 rule 3, D-024).
type AncestorStepDTO struct {
	Ref        *LinkedRef `json:"ref,omitempty"`
	Matches    []MatchDTO `json:"matches,omitempty"`
	Restricted bool       `json:"restricted,omitempty"`
}

// AncestorPathDTO is one up-chain path, root first.
type AncestorPathDTO struct {
	Steps []AncestorStepDTO `json:"steps"`
	Cycle bool              `json:"cycle"`
}

// DelegationRow is one view B row of GET …/delegation (design §7.3). Child
// is absent on a row whose ref selected no route. A restricted row has no
// Outcome and no Reason (design §7.4 rule 4).
type DelegationRow struct {
	Rule              int                 `json:"rule"`
	Ref               int                 `json:"ref"`
	RefAsWritten      DelegationRefDTO    `json:"refAsWritten"`
	Child             *LinkedRef          `json:"child,omitempty"`
	Allows            string              `json:"allows,omitempty"`
	Outcome           string              `json:"outcome,omitempty"`
	Reason            string              `json:"reason,omitempty"`
	Contexts          []DelegationContext `json:"contexts"`
	ContextsTruncated bool                `json:"contextsTruncated,omitempty"`
	Computed          Computed            `json:"computed"`
	Observed          DelegationObserved  `json:"observed"`
}

// DelegationContext is one retained context of a row, or only Restricted
// when a dependency other than the routes on its path is hidden (design
// §7.4 rules 4, 4b).
type DelegationContext struct {
	Restricted  bool          `json:"restricted,omitempty"`
	ParentMatch *MatchDTO     `json:"parentMatch,omitempty"`
	Path        []PathStepDTO `json:"path,omitempty"`
	Outcome     string        `json:"outcome,omitempty"`
	Reason      string        `json:"reason,omitempty"`
}

// PathStepDTO is a route on a context's path and the match it delegated
// under. A hidden ancestor has link none and nothing else (D-024).
type PathStepDTO struct {
	Ref   LinkedRef `json:"ref"`
	Match MatchDTO  `json:"match"`
}

// DelegationObserved is what the controller wrote for a row: the child's
// status entry under the row's report key, and the parent's ResolvedRefs
// condition under one of its reporter keys.
type DelegationObserved struct {
	Entry           *ParentStatus `json:"entry,omitempty"`
	ParentCondition *Condition    `json:"parentCondition,omitempty"`
}

// EffectiveDTO is view C for one parent edge of a route (computed-rules
// §5.7, §6). Contexts is sent as [] when none was kept; ContextsTruncated
// says contexts were cut.
type EffectiveDTO struct {
	Parent            LinkedRef          `json:"parent"`
	Mode              string             `json:"mode"`
	ContextsTruncated bool               `json:"contextsTruncated,omitempty"`
	Contexts          []EffectiveContext `json:"contexts"`
	Computed          Computed           `json:"computed"`
}

// EffectiveContext is the route's rules under one parent match, or only
// Restricted (design §7.4 rule 4).
type EffectiveContext struct {
	Restricted  bool               `json:"restricted,omitempty"`
	ParentMatch *MatchDTO          `json:"parentMatch,omitempty"`
	Rules       []EffectiveRuleDTO `json:"rules,omitempty"`
}

// EffectiveRuleDTO is one rule under a parent match: kept with its
// effective matches, or dropped with Reason.
type EffectiveRuleDTO struct {
	Index   int        `json:"index"`
	Kept    bool       `json:"kept"`
	Matches []MatchDTO `json:"matches"`
	Reason  string     `json:"reason,omitempty"`
}
