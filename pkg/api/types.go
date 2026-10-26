// Package api serves the read-only JSON API (design §7), the probes and the SPA.
package api

import (
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/overview"
)

// InstallSummary is one entry of GET /api/v1/installs. Namespace, Name and
// Scope are absent where the platform has no such notion (D-016).
type InstallSummary struct {
	ID              string                 `json:"id"`
	DisplayName     string                 `json:"displayName"`
	Namespace       string                 `json:"namespace,omitempty"`
	Name            string                 `json:"name,omitempty"`
	Version         string                 `json:"version"`
	Edition         string                 `json:"edition"`
	ControllerNames []string               `json:"controllerNames"`
	Readiness       overview.Readiness     `json:"readiness"`
	Scope           *overview.ScopeSummary `json:"scope,omitempty"`
	Health          HealthSummary          `json:"health"`
	// ComputedAt is when the model behind Health was computed. Clients fall back
	// to it when ConfirmedAt is absent.
	ComputedAt time.Time `json:"computedAt"`
	// ConfirmedAt is when the model was last confirmed to match the cluster
	// (D-019); clients apply the stale rule to it.
	ConfirmedAt *time.Time `json:"confirmedAt,omitempty"`
}

// HealthSummary is the state and per-severity counts.
type HealthSummary struct {
	State  health.State            `json:"state"`
	Counts map[health.Severity]int `json:"counts"`
}

// OverviewResponse is GET …/overview. Gateways shadows the overview's rows,
// so adding their link is additive on the wire.
type OverviewResponse struct {
	overview.Overview
	Gateways    []OverviewGatewayRow `json:"gateways"`
	CollectedAt time.Time            `json:"collectedAt"`
}

// MetaResponse is GET /api/v1/meta.
type MetaResponse struct {
	Version string `json:"version"`
	Pinned  string `json:"pinned,omitempty"`
	Synced  bool   `json:"synced"`
	// Platform names the platform for display, for example "Kubernetes".
	Platform string `json:"platform"`
	// Capabilities are what the platform offers (platform.Scope,
	// platform.Workloads). Clients branch on these, never on Platform.
	Capabilities []string `json:"capabilities"`
	// Kinds are the kinds the resource endpoint can serve: the served kinds,
	// plus apps/Deployment under the workloads capability. The frontend links only these.
	Kinds []kinds.Served `json:"kinds"`
	// Views are the group/kinds with a detail view; informational only.
	Views []objects.GroupKind `json:"views,omitempty"`
	// Filters lists, per list, the filters added after v1, so a client sends
	// one only when this backend applies it (design §7.2).
	Filters map[string][]string `json:"filters,omitempty"`
}

// addedFilters are the list filters added after API v1 (design §7.2).
var addedFilters = map[string][]string{"routes": {"listener"}}

// deploymentKind is listed in /api/v1/meta because finding links reach
// controller Deployments (D-013). It is read through typed informers, not counted.
var deploymentKind = kinds.Served{Descriptor: kinds.Descriptor{Group: "apps", Kind: "Deployment", Resource: "deployments",
	Versions: []string{"v1"}, Namespaced: true, Category: kinds.CategoryWorkload, DisplayName: "Deployments"}, Version: "v1"}

// addedAfterV1 lists the served kinds registered after API v1. Meta lists them
// after Deployment, so v1's kind list only gains elements at its end (D-016 H11).
var addedAfterV1 = map[string]bool{kinds.KgatewayGroup + "/Backend": true}

func isAddedAfterV1(k kinds.Served) bool { return addedAfterV1[k.Group+"/"+k.Kind] }

// ErrorResponse is the body of every non-2xx API response.
type ErrorResponse struct {
	Error string `json:"error"`
}
