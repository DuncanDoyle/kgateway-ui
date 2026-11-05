package computed

import (
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// LogFields are the fixed fields one controller log line yields (design §8.9).
// The reader keeps a value only when it is in a closed set, so a parser that
// returns log text loses it there.
type LogFields struct {
	// Time is when the controller logged the line; zero means the reader's clock.
	Time time.Time
	// Gateway is the Gateway the line names, by namespace and name; zero when none.
	Gateway objects.Ref
	// TypeURL is the xDS type URL of a reject.
	TypeURL string
	// Names are candidate Envoy resource names from Envoy's fixed NACK shapes.
	Names []string
	// Class is an error class; one KnownLogClass does not report becomes LogClassOther.
	Class string
	// Object is the object a deployer error names, nil when none.
	Object *objects.Ref
}

// LogParser is an optional interface of a Rules: it reads the controller log
// lines of its kgateway version. A rule set without it reads no logs.
type LogParser interface {
	// Markers are the fixed substrings of the lines Parse reads; a line without one is never parsed.
	Markers() []string
	// Parse reads one line, valid only during the call. ok false with a nil
	// error means the line is not one it reads; an error means it failed.
	Parse(line []byte) (fields LogFields, ok bool, err error)
}

// LogParser returns the selected rule set's log parser, or nil when it has none.
func (s Selection) LogParser() LogParser {
	p, _ := s.Rules.(LogParser)
	return p
}

// Error classes of derived log events (design §8.9).
const (
	LogClassPanic = "panic"
	LogClassOther = "other"
)

// KnownLogClass reports whether c is on the fixed list of error classes.
func KnownLogClass(c string) bool {
	switch c {
	case LogClassPanic, LogClassOther:
		return true
	}
	return false
}
