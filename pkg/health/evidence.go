package health

import (
	"encoding/json"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Evidence is a finding's typed evidence (design §13.3), tagged by its JSON
// "kind"; the objects it quotes are in Finding.Evidence (§14 rule 12).
type Evidence interface {
	EvidenceKind() string
}

// EvidenceProcess is the kind of ProcessEvidence.
const EvidenceProcess = "process"

// ProcessEvidence is one Pod's facts behind KGWUI-005, 006 and 019–021. Pod
// is empty on a 021 whose only facts come from the controller log.
type ProcessEvidence struct {
	Pod string `json:"pod,omitempty"`
	// Restarts are the restarts observed in the window; nil when none was observed.
	Restarts    *int32            `json:"restarts,omitempty"`
	Termination *TerminationFacts `json:"termination,omitempty"`
	Waiting     string            `json:"waiting,omitempty"`
	Probe       *ProbeFacts       `json:"probe,omitempty"`
	// ControllerLogs says whether the controller log was read, on the codes it informs (019, 021).
	ControllerLogs string `json:"controllerLogs,omitempty"`
	// Panic is the controller's last panic in the window, from the previous container's log.
	Panic *PanicFacts `json:"panic,omitempty"`
	// Deployer are the deployer errors for the Gateway in the window, newest first.
	Deployer []DeployerError `json:"deployer,omitempty"`
}

// Whether an install's controller log was read (design §8.9). Not read is
// never "nothing found": the log events are then absent, not empty.
const (
	LogsRead    = "read"
	LogsNotRead = "not-read"
)

// PanicFacts are a Go panic's class and top frame. A trace has no time, so
// ReadAt is when the UI read the previous container's log.
type PanicFacts struct {
	Class  string    `json:"class"`
	Frame  string    `json:"frame,omitempty"`
	ReadAt time.Time `json:"readAt"`
}

// DeployerError is one kind of deployer error on a Gateway: its class, the
// object it names when the UI knows it, and when it was last logged.
type DeployerError struct {
	Class string `json:"class"`
	// Object is filtered on its own for the caller (design §14).
	Object *objects.Ref `json:"object,omitempty"`
	Last   time.Time    `json:"last"`
}

// TerminationFacts are how a process last ended; never its message.
type TerminationFacts struct {
	Reason   string    `json:"reason,omitempty"`
	ExitCode int32     `json:"exitCode"`
	At       time.Time `json:"at"`
}

// ProbeFacts are a Pod's observed probe failures in the Events window. Type
// is empty when the Pod defines more than one probe type ("a probe").
type ProbeFacts struct {
	Type     string    `json:"type,omitempty"`
	Failures int       `json:"failures"`
	Last     time.Time `json:"last"`
	// HistoryUnknown marks failures before the UI saw the Event, which are not counted.
	HistoryUnknown bool `json:"historyUnknown,omitempty"`
}

// EvidenceKind implements Evidence.
func (*ProcessEvidence) EvidenceKind() string { return EvidenceProcess }

// MarshalJSON adds the kind tag.
func (e *ProcessEvidence) MarshalJSON() ([]byte, error) {
	type plain ProcessEvidence
	return json.Marshal(struct {
		Kind string `json:"kind"`
		*plain
	}{EvidenceProcess, (*plain)(e)})
}

// EvidenceVersionSkew is the kind of VersionSkewEvidence.
const EvidenceVersionSkew = "version-skew"

// VersionSkewEvidence is the versions behind KGWUI-022: the controller's,
// and the proxies', controller replicas' or CRDs' that differ.
type VersionSkewEvidence struct {
	Controller string        `json:"controller,omitempty"`
	Proxies    []VersionPods `json:"proxies,omitempty"`
	Replicas   []VersionPods `json:"replicas,omitempty"`
	CRDs       []CRDVersion  `json:"crds,omitempty"`
	// Expected is the Gateway API bundle minor the controller is built against.
	Expected string `json:"expected,omitempty"`
}

// VersionPods are the Pods running one version, empty when the tag is not one.
// Pods holds the first few names; Total counts them all.
type VersionPods struct {
	Version string   `json:"version"`
	Pods    []string `json:"pods"`
	Total   int      `json:"total"`
}

// CRDVersion is a Gateway API CRD's bundle annotations.
type CRDVersion struct {
	Name          string `json:"name"`
	BundleVersion string `json:"bundleVersion,omitempty"`
	Channel       string `json:"channel,omitempty"`
}

// EvidenceKind implements Evidence.
func (*VersionSkewEvidence) EvidenceKind() string { return EvidenceVersionSkew }

// MarshalJSON adds the kind tag.
func (e *VersionSkewEvidence) MarshalJSON() ([]byte, error) {
	type plain VersionSkewEvidence
	return json.Marshal(struct {
		Kind string `json:"kind"`
		*plain
	}{EvidenceVersionSkew, (*plain)(e)})
}

// EvidenceXDSRejects is the kind of XDSRejectsEvidence.
const EvidenceXDSRejects = "xds-rejects"

// XDSRejectsEvidence is what KGWUI-001 quotes: the rejected types, the
// Gateway's proxies, and whether its status contradicts the rejects (§8.3).
type XDSRejectsEvidence struct {
	Types []XDSTypeRejects `json:"types"`
	// Proxies is nil when the Gateway has no proxy Deployment.
	Proxies           *ProxyCount `json:"proxies,omitempty"`
	StatusSaysHealthy bool        `json:"statusSaysHealthy"`
	// ProxyStats are the Gateway's scraped proxy Pods: rejects observed since
	// their previous scrape, never "rejecting now" (design §8.8).
	ProxyStats []ProxyRejects `json:"proxyStats,omitempty"`
	// RecentlyChanged are objects on the Gateway changed shortly before the first
	// reject, newest first. Not in Finding.Evidence: each is authorized on its own (§14 rule 14).
	RecentlyChanged []objects.Ref `json:"recentlyChanged,omitempty"`
	// ControllerLogs says whether the controller log was read; LogEvents are absent when not.
	ControllerLogs string `json:"controllerLogs"`
	// LogEvents are the log's rejects per rejected type, one per type whichever replica logged it.
	LogEvents []XDSLogEvent `json:"logEvents,omitempty"`
}

// XDSLogEvent is one rejected type's "xds error" lines: the earliest, the
// error classes and the Envoy names known from proxy stats.
type XDSLogEvent struct {
	TypeURL        string    `json:"typeUrl"`
	Since          time.Time `json:"since"`
	Classes        []string  `json:"classes"`
	EnvoyResources []string  `json:"envoyResources,omitempty"`
	// Objects are the objects the rule set maps names to; each name is filtered as its object (§14). Never serialised.
	Objects map[string]objects.Ref `json:"-"`
}

// ProxyRejects is one proxy Pod's rejects observed per xDS type.
type ProxyRejects struct {
	Pod   string             `json:"pod"`
	Types []ProxyTypeRejects `json:"types"`
}

// ProxyTypeRejects says whether a type's update_rejected grew since the Pod's
// previous scrape, since when, and the Envoy names whose counter grew.
type ProxyTypeRejects struct {
	TypeURL        string     `json:"typeUrl"`
	Rejecting      bool       `json:"rejecting"`
	RejectingSince *time.Time `json:"rejectingSince,omitempty"`
	// EnvoyResources are Envoy names, never Gateway API objects.
	EnvoyResources          []string `json:"envoyResources,omitempty"`
	EnvoyResourcesTruncated bool     `json:"envoyResourcesTruncated,omitempty"`
}

// XDSTypeRejects is one rejected xDS type: rejecting streams summed over the
// controller Pods that report them, and rejects_total growth since FirstSeen.
type XDSTypeRejects struct {
	TypeURL             string    `json:"typeUrl"`
	Streams             int       `json:"streams"`
	ReportedBy          []string  `json:"reportedBy"`
	FirstSeen           time.Time `json:"firstSeen"`
	TotalSinceFirstSeen int       `json:"totalSinceFirstSeen"`
}

// ProxyCount is a Gateway's ready and desired proxy replicas.
type ProxyCount struct {
	Ready   int32 `json:"ready"`
	Desired int32 `json:"desired"`
}

// EvidenceKind implements Evidence.
func (*XDSRejectsEvidence) EvidenceKind() string { return EvidenceXDSRejects }

// MarshalJSON adds the kind tag.
func (e *XDSRejectsEvidence) MarshalJSON() ([]byte, error) {
	type plain XDSRejectsEvidence
	return json.Marshal(struct {
		Kind string `json:"kind"`
		*plain
	}{EvidenceXDSRejects, (*plain)(e)})
}

// EvidenceProxyStats is the kind of ProxyStatsEvidence.
const EvidenceProxyStats = "proxy-stats"

// ProxyStatsEvidence is the Ready proxy Pods behind KGWUI-025 or 026.
type ProxyStatsEvidence struct {
	Pods []ProxyPodStats `json:"pods"`
}

// ProxyPodStats is one proxy's Envoy stats; nil fields were not reported.
type ProxyPodStats struct {
	Pod              string `json:"pod"`
	Connected        *bool  `json:"connected,omitempty"`
	WarmingListeners *int64 `json:"warmingListeners,omitempty"`
	WarmingClusters  *int64 `json:"warmingClusters,omitempty"`
	ServerState      string `json:"serverState,omitempty"`
}

// EvidenceKind implements Evidence.
func (*ProxyStatsEvidence) EvidenceKind() string { return EvidenceProxyStats }

// MarshalJSON adds the kind tag.
func (e *ProxyStatsEvidence) MarshalJSON() ([]byte, error) {
	type plain ProxyStatsEvidence
	return json.Marshal(struct {
		Kind string `json:"kind"`
		*plain
	}{EvidenceProxyStats, (*plain)(e)})
}

// EvidenceFreshness is the kind of FreshnessEvidence.
const EvidenceFreshness = "freshness"

// FreshnessEvidence is why KGWUI-003 judges a status not current (design §10.1).
// Observed, Current and Since are set for a stale status only.
type FreshnessEvidence struct {
	Freshness string `json:"freshness"`
	Observed  *int64 `json:"observed,omitempty"`
	Current   *int64 `json:"current,omitempty"`
	// Since is when the UI first saw the current generation.
	Since *time.Time `json:"since,omitempty"`
}

// EvidenceKind implements Evidence.
func (*FreshnessEvidence) EvidenceKind() string { return EvidenceFreshness }

// MarshalJSON adds the kind tag.
func (e *FreshnessEvidence) MarshalJSON() ([]byte, error) {
	type plain FreshnessEvidence
	return json.Marshal(struct {
		Kind string `json:"kind"`
		*plain
	}{EvidenceFreshness, (*plain)(e)})
}

// EvidenceRefs is the kind of RefsEvidence.
const EvidenceRefs = "refs"

// Explanations of KGWUI-013 and 014, first match wins (design §7.3).
const (
	// RefExplainKindNotServed: discovery says the ref's group serves no such kind.
	RefExplainKindNotServed = "kind-not-served"
	// RefExplainServiceMissing: a core Service ref names a Service that does not exist.
	RefExplainServiceMissing = "service-missing"
	// RefExplainServicePort: the Service exists and has no port the ref names.
	RefExplainServicePort = "service-port"
	// RefExplainController: the controller's reason and message, as written.
	RefExplainController = "controller"
)

// RefLocation is one ref and where its referrer writes it, such as
// "spec.rules[2].backendRefs[0]". Ref has the spec defaults applied.
type RefLocation struct {
	Path string      `json:"path"`
	Ref  objects.Ref `json:"ref"`
}

// RefsEvidence is why KGWUI-013 or 014 names its refs. The condition names a
// parent or listener, not a ref, so Candidates are the refs it may be about;
// Attributed is true only with exactly one.
type RefsEvidence struct {
	Explanation string        `json:"explanation"`
	Candidates  []RefLocation `json:"candidates,omitempty"`
	Attributed  bool          `json:"attributed"`
	// Ports are the Service's ports, set for a service-port explanation.
	Ports []int32 `json:"ports,omitempty"`
	// Group is the one group discovery says serves the candidate's kind, set
	// only for an attributed kind-not-served explanation under complete
	// discovery. Never serialised.
	Group *string `json:"-"`
	// Unknown marks a candidate whose group's served state discovery does not
	// know; it may be a wrong group, so no grant is suggested. Never serialised.
	Unknown bool `json:"-"`
}

// EvidenceKind implements Evidence.
func (*RefsEvidence) EvidenceKind() string { return EvidenceRefs }

// MarshalJSON adds the kind tag.
func (e *RefsEvidence) MarshalJSON() ([]byte, error) {
	type plain RefsEvidence
	return json.Marshal(struct {
		Kind string `json:"kind"`
		*plain
	}{EvidenceRefs, (*plain)(e)})
}

// EvidenceEndpoints is the kind of EndpointsEvidence.
const EvidenceEndpoints = "endpoints"

// EndpointsEvidence is a Service port behind KGWUI-023: its ready and total
// endpoints summed over the Service's EndpointSlices, and how many slices.
type EndpointsEvidence struct {
	// Service is the Service's name; Port the port as the backendRef writes it, PortName its Service port name.
	Service  string `json:"service"`
	Port     int32  `json:"port"`
	PortName string `json:"portName,omitempty"`
	Ready    int    `json:"ready"`
	Total    int    `json:"total"`
	Slices   int    `json:"slices"`
	// Routes are the routes naming the port, the group's affected list. Never serialised.
	Routes []objects.Ref `json:"-"`
}

// EvidenceKind implements Evidence.
func (*EndpointsEvidence) EvidenceKind() string { return EvidenceEndpoints }

// MarshalJSON adds the kind tag.
func (e *EndpointsEvidence) MarshalJSON() ([]byte, error) {
	type plain EndpointsEvidence
	return json.Marshal(struct {
		Kind string `json:"kind"`
		*plain
	}{EvidenceEndpoints, (*plain)(e)})
}

// RawEvidence is evidence of a kind this build does not know, kept as sent.
type RawEvidence struct {
	Kind string
	JSON json.RawMessage
}

// EvidenceKind implements Evidence.
func (e *RawEvidence) EvidenceKind() string { return e.Kind }

// MarshalJSON returns the evidence as sent.
func (e *RawEvidence) MarshalJSON() ([]byte, error) { return e.JSON, nil }

// DecodeEvidence decodes the JSON of an Evidence by its kind; nil for null or empty input.
func DecodeEvidence(b []byte) (Evidence, error) {
	if len(b) == 0 || string(b) == "null" {
		return nil, nil
	}
	var tag struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(b, &tag); err != nil {
		return nil, err
	}
	switch tag.Kind {
	case EvidenceProcess:
		e := &ProcessEvidence{}
		return e, json.Unmarshal(b, e)
	case EvidenceVersionSkew:
		e := &VersionSkewEvidence{}
		return e, json.Unmarshal(b, e)
	case EvidenceXDSRejects:
		e := &XDSRejectsEvidence{}
		return e, json.Unmarshal(b, e)
	case EvidenceProxyStats:
		e := &ProxyStatsEvidence{}
		return e, json.Unmarshal(b, e)
	case EvidenceFreshness:
		e := &FreshnessEvidence{}
		return e, json.Unmarshal(b, e)
	case EvidenceRefs:
		e := &RefsEvidence{}
		return e, json.Unmarshal(b, e)
	case EvidenceRuleReplaced:
		e := &RuleReplacedEvidence{}
		return e, json.Unmarshal(b, e)
	case EvidenceListenerConflict:
		e := &ListenerConflictEvidence{}
		return e, json.Unmarshal(b, e)
	case EvidenceStatusProgress:
		e := &StatusProgressEvidence{}
		return e, json.Unmarshal(b, e)
	case EvidenceEndpoints:
		e := &EndpointsEvidence{}
		return e, json.Unmarshal(b, e)
	case EvidenceWithheld:
		e := &WithheldEvidence{}
		return e, json.Unmarshal(b, e)
	}
	return &RawEvidence{Kind: tag.Kind, JSON: append(json.RawMessage(nil), b...)}, nil
}
