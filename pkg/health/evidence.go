package health

import (
	"encoding/json"
	"time"
)

// Evidence is a finding's typed evidence (design §13.3), tagged by its JSON
// "kind"; the objects it quotes are in Finding.Evidence (§14 rule 12).
type Evidence interface {
	EvidenceKind() string
}

// EvidenceProcess is the kind of ProcessEvidence.
const EvidenceProcess = "process"

// ProcessEvidence is one Pod's facts behind KGWUI-005, 006 and 019–021.
type ProcessEvidence struct {
	Pod string `json:"pod"`
	// Restarts are the restarts observed in the window; nil when none was observed.
	Restarts    *int32            `json:"restarts,omitempty"`
	Termination *TerminationFacts `json:"termination,omitempty"`
	Waiting     string            `json:"waiting,omitempty"`
	Probe       *ProbeFacts       `json:"probe,omitempty"`
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
	}
	return &RawEvidence{Kind: tag.Kind, JSON: append(json.RawMessage(nil), b...)}, nil
}
