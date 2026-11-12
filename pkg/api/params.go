package api

import (
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
)

// listKind is the list a query is parsed for.
type listKind int

const (
	listRoutes listKind = iota
	listGateways
	listListenerSets
)

// coreGroup stands for the empty API group in a ref parameter.
const coreGroup = "core"

// Query parameters (design §7.2). single are allowed at most once.
var (
	commonParams = []string{"namespace", "health", "q", "sort", "limit", "cursor"}
	listParams   = map[listKind][]string{
		listRoutes:       {"kind", "parent", "attachment", "listener", "state", "backend"},
		listGateways:     nil,
		listListenerSets: {"parent"},
	}
	singleParams = []string{"parent", "attachment", "listener", "q", "sort", "limit", "cursor"}
)

// badParam is a 400 for a parameter value; it wraps explore.ErrBadQuery so
// every 400 has one cause.
func badParam(format string, args ...any) error {
	return fmt.Errorf("%w: %s", explore.ErrBadQuery, fmt.Sprintf(format, args...))
}

// parseQuery parses r's query for list (design §7.2). It rejects queries that
// do not decode, unknown parameters, repeated single-valued ones and malformed
// values; explore validates the rest (sort keys, health values, attachments, cursors).
func parseQuery(r *http.Request, list listKind) (explore.Query, error) {
	var q explore.Query
	// r.URL.Query drops pairs that fail to decode, which would drop a filter.
	vals, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, badParam("malformed query: %v", err)
	}
	for _, name := range slices.Sorted(maps.Keys(vals)) {
		vs := vals[name]
		if !slices.Contains(commonParams, name) && !slices.Contains(listParams[list], name) {
			return q, badParam("unknown parameter %q", name)
		}
		if len(vs) > 1 && slices.Contains(singleParams, name) {
			return q, badParam("parameter %q given more than once", name)
		}
	}
	q.Namespaces = vals["namespace"]
	for _, v := range vals["health"] {
		q.Health = append(q.Health, health.State(v))
	}
	q.Q, q.Cursor = vals.Get("q"), vals.Get("cursor")
	q.Attachment = explore.Attachment(vals.Get("attachment"))
	if vals.Has("listener") {
		if q.Listener = vals.Get("listener"); q.Listener == "" {
			return q, badParam("listener: want a listener name or %q", explore.ListenerUnknown)
		}
	}
	if v := vals.Get("limit"); vals.Has("limit") {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > explore.MaxLimit {
			return q, badParam("limit %q: want 1–%d", v, explore.MaxLimit)
		}
		q.Limit = n
	}
	if v := vals.Get("sort"); vals.Has("sort") {
		key, dir, _ := strings.Cut(v, ".")
		switch dir {
		case "", "asc":
		case "desc":
			q.Sort.Desc = true
		default:
			return q, badParam("sort %q", v)
		}
		if key == "" {
			return q, badParam("sort %q", v)
		}
		q.Sort.Key = key
	}
	for _, v := range vals["kind"] {
		s, err := segments(v, 2, "kind")
		if err != nil {
			return q, err
		}
		q.Kinds = append(q.Kinds, objects.GroupKind{Group: s[0], Kind: s[1]})
	}
	if vals.Has("parent") {
		ref, err := parseRef(vals.Get("parent"), "parent")
		if err != nil {
			return q, err
		}
		q.Parent = &ref
	}
	for _, v := range vals["backend"] {
		ref, err := parseRef(v, "backend")
		if err != nil {
			return q, err
		}
		q.Backends = append(q.Backends, ref)
	}
	for _, v := range vals["state"] {
		f, ok := explore.ParseFlag(v)
		if !ok {
			return q, badParam("state %q", v)
		}
		q.States |= f
	}
	return q, nil
}

// parseRef parses group/Kind/namespace/name, with "core" for the empty group.
func parseRef(v, param string) (objects.Ref, error) {
	s, err := segments(v, 4, param)
	if err != nil {
		return objects.Ref{}, err
	}
	return objects.Ref{Group: s[0], Kind: s[1], Namespace: s[2], Name: s[3]}, nil
}

// segments splits v into n non-empty "/"-separated parts and maps the
// group "core" to the empty group.
func segments(v string, n int, param string) ([]string, error) {
	s := strings.Split(v, "/")
	if len(s) != n || slices.Contains(s, "") {
		return nil, badParam("%s %q: want %d non-empty segments, %q for the empty group", param, v, n, coreGroup)
	}
	if s[0] == coreGroup {
		s[0] = ""
	}
	return s, nil
}

// Delegation list parameters (design §7.2): no sort, the order is fixed.
var (
	delegationParams = []string{"rule", "ref", "outcome", "limit", "cursor"}
	delegationSingle = []string{"rule", "ref", "limit", "cursor"}
)

// parseDelegationQuery parses r's query for GET …/delegation. It rejects
// unknown and repeated single-valued parameters and non-integer rule, ref
// and limit values; explore validates the rest (ranges, outcomes, cursors).
func parseDelegationQuery(r *http.Request) (explore.DelegationQuery, error) {
	var q explore.DelegationQuery
	vals, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, badParam("malformed query: %v", err)
	}
	for _, name := range slices.Sorted(maps.Keys(vals)) {
		if !slices.Contains(delegationParams, name) {
			return q, badParam("unknown parameter %q", name)
		}
		if len(vals[name]) > 1 && slices.Contains(delegationSingle, name) {
			return q, badParam("parameter %q given more than once", name)
		}
	}
	index := func(name string) (*int, error) {
		if !vals.Has(name) {
			return nil, nil
		}
		n, err := strconv.Atoi(vals.Get(name))
		if err != nil {
			return nil, badParam("%s %q: want an integer", name, vals.Get(name))
		}
		return &n, nil
	}
	if q.Rule, err = index("rule"); err != nil {
		return q, err
	}
	if q.Ref, err = index("ref"); err != nil {
		return q, err
	}
	if v := vals.Get("limit"); vals.Has("limit") {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > explore.MaxLimit {
			return q, badParam("limit %q: want 1–%d", v, explore.MaxLimit)
		}
		q.Limit = n
	}
	for _, v := range vals["outcome"] {
		q.Outcomes = append(q.Outcomes, computed.RowOutcome(v))
	}
	q.Cursor = vals.Get("cursor")
	return q, nil
}

// Path list parameters (design §5). pathsSingle are allowed at most once.
var (
	pathsParams = []string{"gateway", "listener", "route", "backend", "namespace", "state", "kind", "mirror", "q", "sort", "cursor", "limit"}
	pathsSingle = []string{"listener", "route", "namespace", "kind", "mirror", "q", "sort", "cursor", "limit"}
)

// parsePathsQuery parses r's query for GET …/paths. It rejects unknown and
// repeated single-valued parameters and malformed values; paths.Run validates
// the rest (states, mirror, sort keys, cursors).
func parsePathsQuery(r *http.Request) (paths.Query, error) {
	var q paths.Query
	vals, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, badParam("malformed query: %v", err)
	}
	for _, name := range slices.Sorted(maps.Keys(vals)) {
		if !slices.Contains(pathsParams, name) {
			return q, badParam("unknown parameter %q", name)
		}
		if len(vals[name]) > 1 && slices.Contains(pathsSingle, name) {
			return q, badParam("parameter %q given more than once", name)
		}
	}
	for _, v := range vals["gateway"] {
		ns, name, ok := strings.Cut(v, "/")
		if !ok || ns == "" || name == "" || strings.Contains(name, "/") {
			return q, badParam("gateway %q: want namespace/name", v)
		}
		q.Gateways = append(q.Gateways, objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: ns, Name: name})
	}
	for _, name := range []string{"listener", "namespace", "kind"} {
		if vals.Has(name) && vals.Get(name) == "" {
			return q, badParam("%s: want a value", name)
		}
	}
	q.Listener, q.Namespace, q.Kind = vals.Get("listener"), vals.Get("namespace"), vals.Get("kind")
	if vals.Has("route") {
		ref, err := parseRef(vals.Get("route"), "route")
		if err != nil {
			return q, err
		}
		q.Route = &ref
	}
	for _, v := range vals["backend"] {
		ref, err := parseRef(v, "backend")
		if err != nil {
			return q, err
		}
		q.Backends = append(q.Backends, ref)
	}
	for _, v := range vals["state"] {
		q.States = append(q.States, health.State(v))
	}
	q.Mirror, q.Q, q.Cursor = vals.Get("mirror"), vals.Get("q"), vals.Get("cursor")
	if v := vals.Get("limit"); vals.Has("limit") {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > paths.MaxLimit {
			return q, badParam("limit %q: want 1–%d", v, paths.MaxLimit)
		}
		q.Limit = n
	}
	if v := vals.Get("sort"); vals.Has("sort") {
		key, dir, _ := strings.Cut(v, ".")
		switch dir {
		case "", "asc":
		case "desc":
			q.Desc = true
		default:
			return q, badParam("sort %q", v)
		}
		if key == "" {
			return q, badParam("sort %q", v)
		}
		q.Sort = key
	}
	return q, nil
}

// Backend list parameters (design §8.1). backendsSingle are allowed at most once.
var (
	backendsParams = []string{"kind", "namespace", "state", "used", "gateway", "q", "sort", "cursor", "limit"}
	backendsSingle = []string{"kind", "namespace", "used", "q", "sort", "cursor", "limit"}
)

// parseBackendsQuery parses r's query for GET …/backends. It rejects unknown
// and repeated single-valued parameters and malformed values;
// paths.RunBackends validates the rest (states, used, sort keys, cursors).
func parseBackendsQuery(r *http.Request) (paths.BackendQuery, error) {
	var q paths.BackendQuery
	vals, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, badParam("malformed query: %v", err)
	}
	for _, name := range slices.Sorted(maps.Keys(vals)) {
		if !slices.Contains(backendsParams, name) {
			return q, badParam("unknown parameter %q", name)
		}
		if len(vals[name]) > 1 && slices.Contains(backendsSingle, name) {
			return q, badParam("parameter %q given more than once", name)
		}
	}
	for _, name := range []string{"kind", "namespace", "used"} {
		if vals.Has(name) && vals.Get(name) == "" {
			return q, badParam("%s: want a value", name)
		}
	}
	q.Kind, q.Namespace, q.Used, q.Q, q.Cursor = vals.Get("kind"), vals.Get("namespace"), vals.Get("used"), vals.Get("q"), vals.Get("cursor")
	for _, v := range vals["gateway"] {
		ns, name, ok := strings.Cut(v, "/")
		if !ok || ns == "" || name == "" || strings.Contains(name, "/") {
			return q, badParam("gateway %q: want namespace/name", v)
		}
		q.Gateways = append(q.Gateways, objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: ns, Name: name})
	}
	for _, v := range vals["state"] {
		q.States = append(q.States, health.State(v))
	}
	if v := vals.Get("limit"); vals.Has("limit") {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > paths.MaxLimit {
			return q, badParam("limit %q: want 1–%d", v, paths.MaxLimit)
		}
		q.Limit = n
	}
	if v := vals.Get("sort"); vals.Has("sort") {
		key, dir, _ := strings.Cut(v, ".")
		switch dir {
		case "", "asc":
		case "desc":
			q.Desc = true
		default:
			return q, badParam("sort %q", v)
		}
		if key == "" {
			return q, badParam("sort %q", v)
		}
		q.Sort = key
	}
	return q, nil
}
