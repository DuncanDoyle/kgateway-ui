package api

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
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
		listRoutes:       {"kind", "parent", "attachment", "state", "backend"},
		listGateways:     nil,
		listListenerSets: {"parent"},
	}
	singleParams = []string{"parent", "attachment", "q", "sort", "limit", "cursor"}
)

// badParam is a 400 for a parameter value; it wraps explore.ErrBadQuery so
// every 400 has one cause.
func badParam(format string, args ...any) error {
	return fmt.Errorf("%w: %s", explore.ErrBadQuery, fmt.Sprintf(format, args...))
}

// parseQuery parses r's query for list (design §7.2). It rejects unknown
// parameters, repeated single-valued ones and malformed values; explore
// validates the rest (sort keys, health values, attachments, cursors).
func parseQuery(r *http.Request, list listKind) (explore.Query, error) {
	vals := r.URL.Query()
	var q explore.Query
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
