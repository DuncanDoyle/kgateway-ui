// Package api serves the read-only JSON API (design §7), the probes and the SPA.
package api

import (
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
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
	// ComputedAt is when the model behind Health was computed; clients apply the stale rule to it.
	ComputedAt time.Time `json:"computedAt"`
}

// HealthSummary is the state and per-severity counts.
type HealthSummary struct {
	State  health.State            `json:"state"`
	Counts map[health.Severity]int `json:"counts"`
}

// OverviewResponse is GET …/overview.
type OverviewResponse struct {
	overview.Overview
	CollectedAt time.Time `json:"collectedAt"`
}

// HealthResponse is GET …/health. It carries the install ID.
type HealthResponse = health.Report

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
}

// deploymentKind is listed in /api/v1/meta because finding links reach
// controller Deployments (D-013). It is read through typed informers, not counted.
var deploymentKind = kinds.Served{Descriptor: kinds.Descriptor{Group: "apps", Kind: "Deployment", Resource: "deployments",
	Versions: []string{"v1"}, Namespaced: true, Category: kinds.CategoryWorkload, DisplayName: "Deployments"}, Version: "v1"}

// ErrorResponse is the body of every non-2xx API response.
type ErrorResponse struct {
	Error string `json:"error"`
}
