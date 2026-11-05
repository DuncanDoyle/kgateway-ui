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
	// Marker is which of the parser's Markers the line is; empty means the one the reader found first.
	Marker string
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
	// Frame is a panic's top frame function, from the previous container only.
	Frame string
}

// LogParser is an optional interface of a Rules: it reads the controller log
// lines of its kgateway version. A rule set without it reads no logs.
type LogParser interface {
	// Markers are the fixed substrings of the lines Parse reads; a line without one is never parsed.
	Markers() []string
	// Parse reads one line, valid only during the call. ok false with a nil
	// error means the line is not one it reads; an error means it failed, in a fixed text without log content.
	Parse(line []byte) (fields LogFields, ok bool, err error)
}

// MaxLogWindow is how many following lines a LogWindow marker may ask for.
const MaxLogWindow = 8

// LogWindow is an optional interface of a LogParser whose marker needs the
// lines after it, such as a Go panic's top frame (R102).
type LogWindow interface {
	// Window is how many lines after line, which holds marker m, ParseWindow reads,
	// at most MaxLogWindow; previous reports a read of the previous container. 0 means none.
	Window(line []byte, m string, previous bool) int
	// ParseWindow reads a marked line and the lines after it, fewer at the end
	// of the read; all are valid only during the call. Results are as for Parse.
	ParseWindow(line []byte, next [][]byte, previous bool) (fields LogFields, ok bool, err error)
}

// LogParser returns the selected rule set's log parser, or nil when it has none.
func (s Selection) LogParser() LogParser {
	p, _ := s.Rules.(LogParser)
	return p
}

// Error classes of derived log events (design §8.9): Envoy's reject reasons,
// the deployer's failures, a Go panic and other.
const (
	LogClassRegex            = "regex"
	LogClassTLS              = "tls"
	LogClassDuplicate        = "duplicate"
	LogClassBind             = "bind"
	LogClassValidation       = "validation"
	LogClassUnknownExtension = "unknown-extension"
	LogClassApply            = "apply"
	LogClassParameters       = "parameters"
	LogClassRender           = "render"
	LogClassNoValidPorts     = "no-valid-ports"
	LogClassStatusUpdate     = "status-update"
	LogClassPrune            = "prune"
	LogClassPanic            = "panic"
	LogClassOther            = "other"
)

// DeployerLogClass reports whether c is one of the deployer's error classes,
// which only deployer lines carry.
func DeployerLogClass(c string) bool {
	switch c {
	case LogClassApply, LogClassParameters, LogClassRender, LogClassNoValidPorts, LogClassStatusUpdate, LogClassPrune:
		return true
	}
	return false
}

// KnownLogClass reports whether c is on the fixed list of error classes.
func KnownLogClass(c string) bool {
	switch c {
	case LogClassRegex, LogClassTLS, LogClassDuplicate, LogClassBind, LogClassValidation, LogClassUnknownExtension,
		LogClassPanic, LogClassOther:
		return true
	}
	return DeployerLogClass(c)
}
