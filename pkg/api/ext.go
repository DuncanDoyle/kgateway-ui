package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/DuncanDoyle/snellius/pkg/diagnose"
	"github.com/DuncanDoyle/snellius/pkg/endpoints"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
)

// ExtMount is one extension endpoint (design §4.1 gap 1). Pattern is
// "[METHOD ]/path" relative to /api/ext/{Name}, so the endpoint never
// collides with API v1, another extension or the SPA.
type ExtMount struct {
	Name, Pattern string
	Handler       http.Handler
}

// HandlerOption configures NewHandler.
type HandlerOption func(*handlerConfig)

type handlerConfig struct {
	ext        []ExtMount
	capSources []func() []Capability
	coreSource func() []Capability
	commands   diagnose.Commands
	// commandsFor, if set, gives an install's own commands.
	commandsFor func(install.Install) diagnose.Commands
	// featuresFor, if set, gives an install's pack features.
	featuresFor func(install.Install) []string
	// packs are the server's pack names; kindFor, if set, filters overview counts per install.
	packs   []string
	kindFor func(install.Install, objects.GroupKind) bool
	// pathMemo replaces the handler's path memo; nil is a fresh one.
	pathMemo *paths.Memo
	// scoped is the scoped endpoints reader; nil is endpoints.NoReader.
	scoped endpoints.ScopedReader
	// consumersFor, if set, gives an install's non-route backend consumers.
	consumersFor func(install.Install, *install.Owned) []objects.Consumer
	// boot names this process in path revisions; "" is the start time.
	boot string
}

// WithBoot names the process in the paths revision instead of its start
// time, so tests get stable bodies.
func WithBoot(boot string) HandlerOption { return func(c *handlerConfig) { c.boot = boot } }

// WithExtMounts serves m under /api/ext/{name}/. Callers check them with
// CheckExtMounts first; NewHandler logs and skips a mount that fails it.
func WithExtMounts(m ...ExtMount) HandlerOption {
	return func(c *handlerConfig) { c.ext = append(c.ext, m...) }
}

// extMethodPattern is an HTTP method token without lower-case letters; the mux
// matches methods case-sensitively.
var extMethodPattern = regexp.MustCompile("^[A-Z0-9!#$%&'*+.^_`|~-]+$")

var extNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// CheckExtName reports whether name can name an extension: a lowercase
// letter, then up to 62 lowercase letters, digits or dashes.
func CheckExtName(name string) error {
	if !extNamePattern.MatchString(name) {
		return fmt.Errorf("extension name %q must match %s", name, extNamePattern)
	}
	return nil
}

// CheckExtMounts reports every mount whose name or pattern is invalid, whose
// path leaves /api/ext/{name}/, or that the mux rejects, such as two
// patterns it cannot tell apart.
func CheckExtMounts(m ...ExtMount) error {
	var errs []error
	mux := http.NewServeMux()
	for _, x := range m {
		if err := mountExt(mux, x); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// mountExt registers x on mux under its prefix. It turns the mux's
// registration panics into errors.
func mountExt(mux *http.ServeMux, x ExtMount) (err error) {
	route, err := x.route()
	if err != nil {
		return err
	}
	if x.Handler == nil {
		return fmt.Errorf("extension %q pattern %q: nil handler", x.Name, x.Pattern)
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("extension %q pattern %q: %v", x.Name, x.Pattern, p)
		}
	}()
	mux.Handle(route, x.Handler)
	return nil
}

// route is x's mux pattern under /api/ext/{name}. The path starts with "/"
// and has no ".", ".." or empty segments, escaped or not, so it cannot leave
// the prefix; a trailing "/" still marks a subtree.
func (x ExtMount) route() (string, error) {
	if err := CheckExtName(x.Name); err != nil {
		return "", err
	}
	method, path, found := strings.Cut(x.Pattern, " ")
	if !found {
		method, path = "", x.Pattern
	}
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, " \t") || (found && !extMethodPattern.MatchString(method)) {
		return "", fmt.Errorf("extension %q pattern %q must be \"[METHOD ]/path\"", x.Name, x.Pattern)
	}
	segs := strings.Split(path[1:], "/")
	for i, s := range segs {
		u, err := url.PathUnescape(s)
		if err != nil || u == "." || u == ".." || (u == "" && i < len(segs)-1) {
			return "", fmt.Errorf("extension %q pattern %q: path must stay under /api/ext/%s/", x.Name, x.Pattern, x.Name)
		}
	}
	route := "/api/ext/" + x.Name + path
	if method != "" {
		route = method + " " + route
	}
	return route, nil
}

// WriteJSON writes v as an uncached JSON response, as core handlers do.
func WriteJSON(w http.ResponseWriter, v any) { writeJSON(w, v) }

// WriteError writes the API's JSON error body with status code.
func WriteError(w http.ResponseWriter, code int, msg string) { writeErr(w, code, msg) }

// Visible returns the items whose ref the caller of r may view, in order,
// and the number hidden, asking a about each ref once (D-013). Under
// AllowAll, or a nil a, it makes no calls and returns items, clipped so an append cannot alias it.
func Visible[T any](r *http.Request, a Authorizer, items []T, ref func(T) objects.Ref) (visible []T, hidden int) {
	if a == nil || allowsAll(a) {
		return slices.Clip(items), 0
	}
	v := &view{a: a, r: r, seen: map[objects.Ref]bool{}}
	visible = make([]T, 0, len(items))
	for _, it := range items {
		if v.can(ref(it)) {
			visible = append(visible, it)
		} else {
			hidden++
		}
	}
	return visible, hidden
}

// mountExts registers m on mux, logging and skipping a mount CheckExtMounts
// would reject.
func mountExts(mux *http.ServeMux, m []ExtMount) {
	for _, x := range m {
		if err := mountExt(mux, x); err != nil {
			slog.Error("extension endpoint skipped", "err", err)
		}
	}
}
