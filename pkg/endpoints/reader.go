// Package endpoints reads one Service's endpoints on request (design §9.3).
// The interface is platform neutral (D-016); a provider implements it.
package endpoints

import (
	"context"
	"time"

	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// The completeness of a Result.
const (
	Complete    = "complete"
	Partial     = "partial"
	Failed      = "failed"
	Unavailable = "unavailable"
	TimedOut    = "timed-out"
	Busy        = "busy"
)

// Endpoint is one address and port of a Service. Pod is the target Pod's
// name when the provider names one.
type Endpoint struct {
	Address                     string
	Port                        int32
	PortName                    string
	Ready, Serving, Terminating bool
	Pod                         string
}

// Result is one read of a Service's endpoints. Only a Complete result
// establishes that Items is every endpoint. Callers must not modify Items.
type Result struct {
	Items    []Endpoint
	Complete string
	ReadAt   time.Time
}

// ScopedReader reads the endpoints of Service svc in the install with ID
// installID. It takes the ID, not an install, so providers need not import pkg/install.
type ScopedReader interface {
	Endpoints(ctx context.Context, installID string, svc objects.Ref) (Result, error)
}

// NoReader is the reader of a provider without one: every result is Unavailable.
var NoReader ScopedReader = noReader{}

type noReader struct{}

func (noReader) Endpoints(context.Context, string, objects.Ref) (Result, error) {
	return Result{Complete: Unavailable, ReadAt: time.Now()}, nil
}
