package api

import (
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/diagnose"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
)

// FindingRow is one finding of GET …/findings (design §13.3); restricted rows
// keep identity only (§14 rule 11), EvidenceRestricted hides the detail (rule 12).
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

// FindingDetail is GET …/findings/{findingId}. Affected pages through the
// visible objects of the finding's group (design §14 rule 13).
type FindingDetail struct {
	Install      string          `json:"install"`
	Finding      FindingRow      `json:"finding"`
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

// GroupRow is one ranked group with its rank keys, so a client can explain
// the order (design §13.2).
type GroupRow struct {
	ID            string          `json:"id"`
	Title         string          `json:"title"`
	Severity      health.Severity `json:"severity"`
	Class         diagnose.Class  `json:"class"`
	Cause         *LinkedRef      `json:"cause,omitempty"`
	Members       int             `json:"members"`
	Affected      []LinkedRef     `json:"affected"`
	AffectedTotal int             `json:"affectedTotal"`
	Since         *time.Time      `json:"since,omitempty"`
	NextSteps     []NextStep      `json:"nextSteps,omitempty"`
	Restricted    bool            `json:"restricted,omitempty"`
}
