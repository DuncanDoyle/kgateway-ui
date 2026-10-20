// Package platform names the platform a provider runs on and the capabilities
// it offers. Clients branch on capabilities, never on the platform name (D-016).
package platform

import "slices"

// Capabilities defined in Phase 1. Only capabilities a client reads are defined.
const (
	// Scope means installs have a namespace scope: the scope tile and filters.
	Scope = "scope"
	// Workloads means controllers and proxies run as Deployments the UI reads:
	// Deployment links and replica wording.
	Workloads = "workloads"
)

// Info describes a platform.
type Info struct {
	// Name is for display only, for example "Kubernetes".
	Name string
	// Capabilities is a set of capability names.
	Capabilities []string
}

// Has reports whether the platform offers capability c.
func (i Info) Has(c string) bool { return slices.Contains(i.Capabilities, c) }

// IsZero reports whether i is the zero Info: no name and no capabilities.
// Defaults treat only the zero Info as Kubernetes, never a name check.
func (i Info) IsZero() bool { return i.Name == "" && len(i.Capabilities) == 0 }

// Kubernetes is the platform of the Kubernetes provider.
func Kubernetes() Info { return Info{Name: "Kubernetes", Capabilities: []string{Scope, Workloads}} }
