package api

import (
	"time"

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
	ListenerSets   int          `json:"listenerSets"`
	Addresses      []string     `json:"addresses"`
}

// ListenerSetRow is one row of the ListenerSets list.
type ListenerSetRow struct {
	Ref            LinkedRef    `json:"ref"`
	Parent         LinkedRef    `json:"parent"`
	Health         health.State `json:"health"`
	Listeners      int          `json:"listeners"`
	AcceptedReason string       `json:"acceptedReason,omitempty"`
}
