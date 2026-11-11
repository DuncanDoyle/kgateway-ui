package api

import "time"

// BackendRowDTO is one backend of the Backends list (design §8.1). Kind is
// "Service", "Backend · <spec.type>" or the ref's kind. Endpoints has one
// entry per referenced Service port, or one not-applicable entry for a
// Backend. Routes, Gateways and Consumers count what the caller may see;
// Confirmed counts its paths on an attachment the route's status accepts.
type BackendRowDTO struct {
	Ref       LinkedRef      `json:"ref"`
	Kind      string         `json:"kind"`
	Endpoints []EndpointsDTO `json:"endpoints"`
	Routes    int            `json:"routes"`
	Gateways  int            `json:"gateways"`
	Consumers int            `json:"consumers"`
	Confirmed int            `json:"confirmed"`
	Problem   ProblemDTO     `json:"problem"`
}

// BackendsResponse is GET …/backends. Hidden says a backend, path, route or
// consumer was left out for the caller (design §13).
type BackendsResponse struct {
	Install     string          `json:"install"`
	Rows        []BackendRowDTO `json:"rows"`
	Next        string          `json:"next,omitempty"`
	Prev        string          `json:"prev,omitempty"`
	Hidden      bool            `json:"hidden,omitempty"`
	CollectedAt time.Time       `json:"collectedAt"`
}

// BackendDetail is GET …/backends/{group}/{kind}/{ns}/{name} (design §8.2).
// Status is the backend's own signals, which the status line phrases; Problem
// also covers its paths. Exposure is the first page of its paths. Sources has
// one kubernetes entry per referenced Service port, with the scoped read's
// endpoints when it has them. Service holds a Service's cached facts and
// Conditions a Backend's.
type BackendDetail struct {
	Install      string              `json:"install"`
	Ref          LinkedRef           `json:"ref"`
	Kind         string              `json:"kind"`
	Type         string              `json:"type,omitempty"`
	Problem      ProblemDTO          `json:"problem"`
	Status       []SignalDTO         `json:"status"`
	Exposure     PathsResponse       `json:"exposure"`
	Consumers    []ConsumerDTO       `json:"consumers"`
	Sources      []EndpointsDTO      `json:"sources"`
	PortChain    []PortChainDTO      `json:"portChain"`
	Explanations []RefExplanationDTO `json:"explanations"`
	Service      *ServiceFactDTO     `json:"service,omitempty"`
	Conditions   []Condition         `json:"conditions,omitempty"`
	CollectedAt  time.Time           `json:"collectedAt"`
}

// ConsumerDTO is a non-route object that uses the backend (design §8.3):
// its role, such as ext-auth, the JSON path of its reference and the port written there.
type ConsumerDTO struct {
	Consumer LinkedRef `json:"consumer"`
	Role     string    `json:"role"`
	Field    string    `json:"field"`
	Port     int       `json:"port,omitempty"`
}

// PortChainDTO is one referenced port's chain as written and observed
// (design §9.4): the Service port it matches, its numeric target or target
// name, and the slice ports observed for that port's name. NoSuchPort is set
// when the cached Service has no such port, NoSlicePort only when a complete
// read found no slice port for the name.
type PortChainDTO struct {
	RefPort         int32   `json:"refPort"`
	ServicePort     *int32  `json:"servicePort,omitempty"`
	ServicePortName string  `json:"servicePortName,omitempty"`
	TargetPort      string  `json:"targetPort,omitempty"`
	TargetName      string  `json:"targetName,omitempty"`
	SlicePorts      []int32 `json:"slicePorts"`
	NoSuchPort      bool    `json:"noSuchPort,omitempty"`
	NoSlicePort     bool    `json:"noSlicePort,omitempty"`
}

// ServiceFactDTO is what the cache keeps of a Service: its type and ports.
type ServiceFactDTO struct {
	Type  string    `json:"type"`
	Ports []PortDTO `json:"ports"`
}

// RefExplanationDTO explains one route ref to the backend that does not
// resolve (design §10). Kind is attributed, candidate or waiting. Ref is the
// backendRefs index, which a backendRef's own mirror shares, and -1 for a rule
// filter's mirror; Path, where the route writes the ref, tells them apart.
type RefExplanationDTO struct {
	Route       LinkedRef     `json:"route"`
	Rule        int           `json:"rule"`
	Ref         int           `json:"ref"`
	Path        string        `json:"path"`
	Kind        string        `json:"kind"`
	Reason      string        `json:"reason,omitempty"`
	Explanation string        `json:"explanation,omitempty"`
	Fix         *SuggestedFix `json:"fix,omitempty"`
}
