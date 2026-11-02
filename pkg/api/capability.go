package api

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
)

// CapabilityState is whether a capability works for this caller and install.
type CapabilityState string

// The capability states. Only CapOn also appears in MetaResponse.Capabilities.
const (
	CapOn      CapabilityState = "on"
	CapOff     CapabilityState = "off"
	CapDenied  CapabilityState = "denied"
	CapFailing CapabilityState = "failing"
)

// Capability is one named capability and its state; Reason says why it is not on.
type Capability struct {
	Name   string          `json:"name"`
	State  CapabilityState `json:"state"`
	Reason string          `json:"reason,omitempty"`
}

// coreCapabilities are the names the upstream UI reserves, besides the platform's.
var coreCapabilities = []string{"processes", "events", "crds", "endpoints", "proxyStats", "controllerLogs"}

// WithCoreCapabilities sets the source of the core names' states; it runs on
// every /api/v1/meta request, and any other name it reports is dropped.
func WithCoreCapabilities(f func() []Capability) HandlerOption {
	return func(c *handlerConfig) { c.coreSource = f }
}

// WithCapabilitySources adds sources of capability states for /api/v1/meta.
// Each source runs on every /api/v1/meta request, so it must be fast, safe for
// concurrent use and report a stable set of names.
func WithCapabilitySources(f ...func() []Capability) HandlerOption {
	return func(c *handlerConfig) { c.capSources = append(c.capSources, f...) }
}

// reserved reports whether name belongs to the core or the platform.
func reserved(name string, platformNames []string) bool {
	return slices.Contains(coreCapabilities, name) || slices.Contains(platformNames, name)
}

// CheckCapabilities calls each source once and reports a name that is empty,
// repeated, or reserved by the core or the platform, and an unknown state.
func CheckCapabilities(platformNames []string, sources ...func() []Capability) error {
	var errs []error
	seen := map[string]bool{}
	for _, f := range sources {
		for _, c := range f() {
			switch {
			case c.Name == "":
				errs = append(errs, errors.New("capability with an empty name"))
			case reserved(c.Name, platformNames):
				errs = append(errs, fmt.Errorf("capability %q collides with a core capability", c.Name))
			case seen[c.Name]:
				errs = append(errs, fmt.Errorf("capability %q is reported twice", c.Name))
			}
			if !validState(c.State) {
				errs = append(errs, fmt.Errorf("capability %q has unknown state %q", c.Name, c.State))
			}
			seen[c.Name] = true
		}
	}
	return errors.Join(errs...)
}

func validState(s CapabilityState) bool {
	return s == CapOn || s == CapOff || s == CapDenied || s == CapFailing
}

// warnedDrops holds the names already warned about, so the poll rate does not spam the log.
var warnedDrops sync.Map

// addCapabilities adds the core and extension sources' states to resp, sorted
// by name. The core source may report only core names; an extension name that
// is reserved or already seen is dropped, so a source cannot shadow a core one.
func addCapabilities(resp *MetaResponse, platformNames []string, core func() []Capability, sources []func() []Capability) {
	seen := map[string]bool{}
	var states []Capability
	if core != nil {
		for _, c := range core() {
			if slices.Contains(coreCapabilities, c.Name) && validState(c.State) && !seen[c.Name] {
				seen[c.Name] = true
				states = append(states, c)
			}
		}
	}
	for _, f := range sources {
		for _, c := range f() {
			if c.Name == "" || !validState(c.State) || reserved(c.Name, platformNames) || seen[c.Name] {
				if _, dup := warnedDrops.LoadOrStore(c.Name, true); !dup {
					slog.Warn("capability dropped", "name", c.Name, "state", c.State)
				}
				continue
			}
			seen[c.Name] = true
			states = append(states, c)
		}
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Name < states[j].Name })
	for _, c := range states {
		if c.State == CapOn {
			resp.Capabilities = append(resp.Capabilities, c.Name)
		}
	}
	resp.CapabilityStates = states
}
