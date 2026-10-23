package api

import (
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
