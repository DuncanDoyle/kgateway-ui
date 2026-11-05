package kgw23

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// The controller log lines this rule set reads, pinned at v2.3.11 (design §8.9). Lines are slog
// JSON (pkg/logging/options.go:66 defaults to JSON); the Gateway queue logs through Istio's scope.
const (
	// markerXDS: logger.Warn("xds error", ...) at pkg/kgateway/setup/envoy_error.go:125.
	markerXDS = "xds error"
	// markerDeferral: logger.Info at pkg/kgateway/proxy_syncer/perclient.go:149.
	markerDeferral = "per-client inputs not ready; deferring snapshot"
	// markerDeployer: Istio's queue logs "error handling <ns/name>, retrying …" for
	// GatewayController (pkg/kgateway/controller/gw_controller.go:101).
	markerDeployer = "error handling "
	// markerPanic: a Go runtime panic on the container's stderr.
	markerPanic = "panic: "
)

var logMarkers = []string{markerXDS, markerDeferral, markerDeployer, markerPanic}

// maxNames bounds the candidate names one line yields; the reader keeps only known ones.
const maxNames = 16

// Markers implements computed.LogParser.
func (rules) Markers() []string { return logMarkers }

// Parse implements computed.LogParser. Only a JSON line whose own msg is a
// marker's is read, so a marker inside a value or a key is not. A panic is
// read through ParseWindow only.
func (rules) Parse(line []byte) (computed.LogFields, bool, error) {
	if bytes.HasPrefix(line, []byte(markerPanic)) || bytes.HasPrefix(line, []byte("\t")) {
		return computed.LogFields{}, false, nil // Go runtime output
	}
	l, err := decodeLine(line)
	if err != nil || l.dup {
		return computed.LogFields{}, false, err
	}
	switch {
	case l.msg == markerXDS && l.component == "xds/envoy":
		return parseXDS(l)
	case l.msg == markerDeferral && l.component == "proxy_syncer":
		return parseDeferral(l)
	case l.scope == "controllers" && l.controller == "GatewayController" && strings.HasPrefix(l.msg, markerDeployer):
		return parseDeployer(l)
	}
	return computed.LogFields{}, false, nil
}

// Window implements computed.LogWindow: a raw panic line reads the lines after
// it, in the previous container only (R102); a JSON line quoting one does not.
func (rules) Window(line []byte, m string, previous bool) int {
	if m == markerPanic && previous && bytes.HasPrefix(line, []byte(markerPanic)) {
		return computed.MaxLogWindow
	}
	return 0
}

// ParseWindow implements computed.LogWindow.
func (r rules) ParseWindow(line []byte, next [][]byte, previous bool) (computed.LogFields, bool, error) {
	if !bytes.HasPrefix(line, []byte(markerPanic)) {
		return r.Parse(line)
	}
	if !previous {
		return computed.LogFields{}, false, nil
	}
	return computed.LogFields{Marker: markerPanic, Class: computed.LogClassPanic, Frame: topFrame(next)}, true, nil
}

// Parse errors are fixed texts: a time or JSON error would quote the line.
var (
	errNotJSON = errors.New("not a JSON log line")
	errBadJSON = errors.New("unreadable JSON log line")
	errBadTime = errors.New("unreadable log time")
)

// logLine holds the fields a line is read by; the rest of the line is never decoded.
type logLine struct {
	time, msg, component, scope, controller           string
	gatewayName, gatewayNS, resource, errText, client string
	// dup is set when a field the parser dispatches on repeats.
	dup bool
}

// decodeLine reads a one-object JSON line. The first "time" wins, as kgateway's
// own lines can repeat it with a duration.
func decodeLine(line []byte) (logLine, error) {
	var l logLine
	if !bytes.HasPrefix(line, []byte("{")) {
		return l, errNotJSON
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	if _, err := dec.Token(); err != nil {
		return l, errBadJSON
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return l, errBadJSON
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return l, errBadJSON
		}
		dst := l.field(key)
		if dst == nil {
			continue
		}
		if seen[key] {
			if key != "time" {
				l.dup = true
			}
			continue
		}
		seen[key] = true
		if err := json.Unmarshal(raw, dst); err != nil {
			return l, errBadJSON
		}
	}
	if _, err := dec.Token(); err != nil {
		return l, errBadJSON
	}
	if _, err := dec.Token(); err != io.EOF {
		return l, errBadJSON
	}
	return l, nil
}

func (l *logLine) field(key string) *string {
	switch key {
	case "time":
		return &l.time
	case "msg":
		return &l.msg
	case "component":
		return &l.component
	case "scope":
		return &l.scope
	case "controller":
		return &l.controller
	case "gateway_name":
		return &l.gatewayName
	case "gateway_ns":
		return &l.gatewayNS
	case "resource":
		return &l.resource
	case "error":
		return &l.errText
	case "client":
		return &l.client
	}
	return nil
}

// stamp parses the line's time; a line without one gets the reader's clock.
func (l logLine) stamp() (time.Time, error) {
	if l.time == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, l.time)
	if err != nil {
		return time.Time{}, errBadTime
	}
	return t, nil
}

func gateway(ns, name string) objects.Ref {
	return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: ns, Name: name}
}

// parseXDS reads a NACK. kgateway trims the type URL's prefix
// (envoy_error.go:103); names come only from Envoy's CDS and RDS shapes.
func parseXDS(l logLine) (computed.LogFields, bool, error) {
	t, err := l.stamp()
	if err != nil {
		return computed.LogFields{}, false, err
	}
	if l.gatewayName == "" || l.gatewayNS == "" || l.resource == "" || l.errText == "" {
		return computed.LogFields{}, false, errors.New("xds error: a field is missing")
	}
	f := computed.LogFields{Time: t, Marker: markerXDS, Gateway: gateway(l.gatewayNS, l.gatewayName),
		TypeURL: "type.googleapis.com/" + l.resource, Class: classOf(l.errText, xdsClasses)}
	switch l.resource {
	case "envoy.config.cluster.v3.Cluster":
		f.Names = clusterNames(l.errText)
	case "envoy.config.route.v3.RouteConfiguration":
		f.Names = routeNames(l.errText)
	}
	// A listener reject keeps only its class: Envoy has no per-listener reject stat to match (R102).
	return f, true, nil
}

// clusterNames reads "Error adding/updating cluster(s) <name>: <msg>, <name>: <msg>"
// (Envoy cds_api_impl.cc:110, cds_api_helper.cc:55 at v1.37.9).
func clusterNames(msg string) []string {
	rest, ok := strings.CutPrefix(msg, "Error adding/updating cluster(s) ")
	if !ok {
		return nil
	}
	var out []string
	for _, part := range strings.Split(rest, ", ") {
		if name, _, ok := strings.Cut(part, ": "); ok && plainName(name) && len(out) < maxNames {
			out = append(out, name)
		}
	}
	return out
}

// routeNames reads "Unexpected RDS configuration (expecting <name>): <name>"
// (Envoy source/common/rds/rds_route_config_subscription.cc:103 at v1.37.9).
func routeNames(msg string) []string {
	rest, ok := strings.CutPrefix(msg, "Unexpected RDS configuration (expecting ")
	if !ok {
		return nil
	}
	want, got, ok := strings.Cut(rest, "): ")
	if !ok || !plainName(want) || !plainName(got) {
		return nil
	}
	return []string{want, got}
}

// plainName accepts 1 to 200 printable ASCII characters without spaces, like Envoy's stat names.
func plainName(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] <= ' ' || s[i] > '~' {
			return false
		}
	}
	return true
}

// phrase is a fixed text that names an error class.
type phrase struct{ text, class string }

// xdsClasses are fixed texts of Envoy's reject messages at v1.37.9 (source/common/...).
var xdsClasses = []phrase{
	{"RE2 program size", computed.LogClassRegex},                                            // common/regex.cc:71
	{"Failed to initialize cipher suites", computed.LogClassTLS},                            // tls/context_impl.cc:144
	{"Failed to initialize ECDH curves", computed.LogClassTLS},                              // tls/context_impl.cc:153
	{"Failed to load certificate chain", computed.LogClassTLS},                              // tls/context_impl.cc:245
	{"Failed to load private key", computed.LogClassTLS},                                    // tls/context_impl.cc:747
	{"Failed to load trusted CA certificates", computed.LogClassTLS},                        // tls/cert_validator/default_validator.cc:86
	{"duplicate listener ", computed.LogClassDuplicate},                                     // listener_manager/lds_api.cc:96
	{"duplicate cluster ", computed.LogClassDuplicate},                                      // upstream/cds_api_helper.cc:49
	{"cannot bind '", computed.LogClassBind},                                                // network/listen_socket_impl.cc:28
	{"Proto constraint validation failed", computed.LogClassValidation},                     // protobuf/utility.cc:161
	{"Didn't find a registered implementation for name", computed.LogClassUnknownExtension}, // config/utility.h:220
}

// deployerClasses are the Gateway reconcile errors at v2.3.11.
var deployerClasses = []phrase{
	{"failed to apply object ", computed.LogClassApply},                    // pkg/deployer/deployer.go:336
	{"could not retrieve GatewayParameters", computed.LogClassParameters},  // pkg/deployer/errors.go:9
	{"failed to get helm values", computed.LogClassRender},                 // deployer.go:194
	{"failed to render helm chart", computed.LogClassRender},               // deployer.go:174
	{"failed to convert helm manifest", computed.LogClassRender},           // deployer.go:151
	{"failed to get objects to deploy", computed.LogClassRender},           // deployer.go:209
	{"failed to post-process objects", computed.LogClassRender},            // deployer.go:217
	{"no valid ports", computed.LogClassNoValidPorts},                      // pkg/kgateway/deployer/gateway_parameters.go:28
	{"error pruning removed resources", computed.LogClassPrune},            // gw_controller.go:351
	{"failed to update status for Gateway", computed.LogClassStatusUpdate}, // gw_controller.go:324
	{"error updating status for Gateway", computed.LogClassStatusUpdate},   // gw_controller.go:366
}

// classOf is the class of the phrase that comes first in msg, the outermost
// error; a missing GatewayParameters names the render failure it causes.
// A text no phrase names is other.
func classOf(msg string, list []phrase) string {
	class, at := computed.LogClassOther, len(msg)
	for _, p := range list {
		if i := strings.Index(msg, p.text); i >= 0 && i < at {
			class, at = p.class, i
		}
	}
	if class == computed.LogClassRender && strings.Contains(msg, "could not retrieve GatewayParameters") {
		return computed.LogClassParameters
	}
	return class
}

// parseDeferral reads the client a snapshot is withheld from: Envoy's role
// "<prefix>~<ns>~<name>" plus kgateway's suffixes (envoy_error.go:87-97, pkg/pluginsdk/ir/model.go:56).
func parseDeferral(l logLine) (computed.LogFields, bool, error) {
	t, err := l.stamp()
	if err != nil {
		return computed.LogFields{}, false, err
	}
	parts := strings.Split(l.client, "~")
	if len(parts) < 3 || parts[1] == "" || parts[2] == "" {
		return computed.LogFields{}, false, errors.New("deferral: unknown client format")
	}
	return computed.LogFields{Time: t, Marker: markerDeferral, Gateway: gateway(parts[1], parts[2])}, true, nil
}

// applyObject reads "failed to apply object <group>/<version>, Kind=<kind> <ns>/<name>: …" (deployer.go:336).
var applyObject = regexp.MustCompile(`failed to apply object ([a-z0-9.-]*)/[a-z0-9]+, Kind=([A-Za-z]+) ([a-z0-9.-]+)/([a-z0-9.-]+): `)

// parseDeployer reads a Gateway reconcile error. Istio's queue key is "<ns>/<name>";
// the object is the one an apply names, else the Gateway.
func parseDeployer(l logLine) (computed.LogFields, bool, error) {
	t, err := l.stamp()
	if err != nil {
		return computed.LogFields{}, false, err
	}
	key, rest, ok := strings.Cut(strings.TrimPrefix(l.msg, markerDeployer), ", ")
	ns, name, nsOK := strings.Cut(key, "/")
	if !ok || !nsOK || ns == "" || name == "" || strings.Contains(name, "/") {
		return computed.LogFields{}, false, errors.New("deployer: unknown message format")
	}
	gw := gateway(ns, name)
	f := computed.LogFields{Time: t, Marker: markerDeployer, Gateway: gw, Object: &gw, Class: classOf(rest, deployerClasses)}
	if m := applyObject.FindStringSubmatch(rest); m != nil && f.Class == computed.LogClassApply {
		f.Object = &objects.Ref{Group: m[1], Kind: m[2], Namespace: m[3], Name: m[4]}
	}
	return f, true, nil
}

// signalLine is the runtime's line after a signal panic's message (runtime/panic.go:1570 at go1.26.10).
var signalLine = regexp.MustCompile(`^\[signal [^\]]*\]$`)

// goroutineHeader is the line the Go runtime prints before the panicking
// goroutine's frames, with GOTRACEBACK=system's extra fields.
var goroutineHeader = regexp.MustCompile(`^goroutine [0-9]+ (gp=0x[0-9a-f]+ m=[0-9]+ (mp=0x[0-9a-f]+ )?)?\[running[^\]]*\]:$`)

// topFrame is the panicking goroutine's first frame function outside the
// runtime, or "". Go indents a panic message's lines with a tab (runtime
// printindented, Go 1.24+; kgateway builds with 1.26), so a column-0 header
// and frame come from the runtime, not from the message. Under
// GOTRACEBACK=all the panic line can sit above the read tail: then no event.
func topFrame(next [][]byte) string {
	i := 0
	for i < len(next) && bytes.HasPrefix(next[i], []byte("\t")) {
		i++ // the panic message's further lines
	}
	if i < len(next) && signalLine.Match(next[i]) {
		i++ // a signal panic's "[signal SIGSEGV: …]" line
	}
	if i+1 >= len(next) || len(next[i]) != 0 || !goroutineHeader.Match(next[i+1]) {
		return ""
	}
	for i += 2; i+1 < len(next); i += 2 {
		fn, file := string(next[i]), next[i+1]
		if !bytes.HasPrefix(file, []byte("\t")) {
			return ""
		}
		open := strings.LastIndexByte(fn, '(')
		if open <= 0 || !strings.HasSuffix(fn, ")") {
			return ""
		}
		fn = fn[:open]
		if fn == "panic" || strings.HasPrefix(fn, "runtime.") {
			continue
		}
		return fn
	}
	return ""
}
