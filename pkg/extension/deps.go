package extension

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
)

// Deps is what the app gives an extension's handlers (design §4.1 gap 1).
type Deps struct {
	// Current returns the published model, nil before the first publication.
	// A handler calls it once per request and serves from that value.
	Current func() *engine.Published
	// Authorizer is the registry's; filter objects with api.Visible.
	Authorizer api.Authorizer
	// Logger carries the extension's name.
	Logger *slog.Logger
}

// Router registers an extension's endpoints. pattern is "[METHOD ]/path"
// relative to /api/ext/{name}, such as "GET /widgets/{id}".
type Router interface {
	Handle(pattern string, h http.Handler)
}

// extHandlers is one WithHandlers registration.
type extHandlers struct {
	name string
	f    func(Router, Deps)
}

// router collects one extension's mounts.
type router struct {
	name   string
	mounts []api.ExtMount
}

func (r *router) Handle(pattern string, h http.Handler) {
	r.mounts = append(r.mounts, api.ExtMount{Name: r.name, Pattern: pattern, Handler: h})
}

// WithHandlers adds endpoints under /api/ext/{name}/; f registers them when
// the app is built. name is a lowercase letter, then up to 62 lowercase
// letters, digits or dashes, and registers once. A rejected name, and a
// pattern that is malformed or leaves the prefix, is reported by Registry.Err
// or app.Build.
func WithHandlers(name string, f func(r Router, d Deps)) Option {
	return func(r *Registry) {
		if err := api.CheckExtName(name); err != nil {
			r.fail("%w", err)
			return
		}
		for _, h := range r.handlers {
			if h.name == name {
				r.fail("extension name %q is already registered", name)
				return
			}
		}
		r.handlers = append(r.handlers, extHandlers{name: name, f: f})
	}
}

// Mounts runs every WithHandlers function with d, its Logger tagged with the
// extension's name, and returns the endpoints for api.WithExtMounts. It
// returns an error if any endpoint fails api.CheckExtMounts.
func (r *Registry) Mounts(d Deps) ([]api.ExtMount, error) {
	if d.Authorizer == nil {
		d.Authorizer = api.AllowAll{}
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	var all []api.ExtMount
	for _, h := range r.handlers {
		rt := &router{name: h.name}
		hd := d
		hd.Logger = d.Logger.With("extension", h.name)
		h.f(rt, hd)
		all = append(all, rt.mounts...)
	}
	if err := api.CheckExtMounts(all...); err != nil {
		return nil, fmt.Errorf("extension endpoints: %w", err)
	}
	return all, nil
}
