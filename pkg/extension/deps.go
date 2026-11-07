package extension

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"time"

	"github.com/DuncanDoyle/snellius/pkg/api"
	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/engine"
)

// Deps is what the app gives an extension's handlers (design §4.1 gap 1).
type Deps struct {
	// Current returns the published model, nil before the first publication.
	// A handler calls it once per request and serves from that value.
	Current func() *engine.Published
	// Authorizer is the registry's; filter objects with api.Visible.
	Authorizer api.Authorizer
	// Capabilities returns the states every WithCapabilities source reports now,
	// in registration order; /api/v1/meta lists the same.
	Capabilities func() []api.Capability
	// Logger carries the extension's name.
	Logger *slog.Logger
	// Publish stores value under "<collector name>/<key>" and hands it over:
	// the collector never mutates it afterwards. ttl 0 means no expiry. Nil for handlers.
	Publish func(key string, value any, fingerprint uint64, ttl time.Duration)
	// Fail marks a collector key failing or off with a reason. Nil for handlers.
	Fail func(key string, st collect.State, reason string)
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

// Collector is a long-running source of values for checks (design §4.1 gap
// 2), such as a scraper or a prober. Name follows the WithHandlers rules.
type Collector interface {
	Name() string
	// Start runs until ctx is done, publishing through d.Publish and d.Fail.
	// An error return is logged once; the collector is not restarted and its
	// values turn unavailable.
	Start(ctx context.Context, d Deps) error
}

// WithCollectors adds collectors that the app starts after the cache. Names
// are unique among collectors; "proxy-stats" is the core collector's and
// app.Build rejects it. A rejected name is reported by Registry.Err.
func WithCollectors(c ...Collector) Option {
	return func(r *Registry) {
		for _, col := range c {
			name := col.Name()
			if err := api.CheckExtName(name); err != nil {
				r.fail("collector: %w", err)
				continue
			}
			if slices.ContainsFunc(r.collectors, func(o Collector) bool { return o.Name() == name }) {
				r.fail("collector name %q is already registered", name)
				continue
			}
			r.collectors = append(r.collectors, col)
		}
	}
}

// Collectors returns the registered collectors, in order.
func (r *Registry) Collectors() []Collector { return slices.Clone(r.collectors) }

// RunCollectors starts each collector in its own goroutine with d, its
// Publish and Fail writing to s under the collector's name. When Start
// returns, its values expire in s.
func (r *Registry) RunCollectors(ctx context.Context, d Deps, s *collect.Store) {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	for _, c := range r.collectors {
		name := c.Name()
		cd := d
		cd.Logger = d.Logger.With("collector", name)
		cd.Publish = func(key string, v any, fp uint64, ttl time.Duration) { s.Publish(name+"/"+key, v, fp, ttl) }
		cd.Fail = func(key string, st collect.State, reason string) { s.Fail(name+"/"+key, st, reason) }
		go func() {
			defer s.Stop(name)
			defer func() {
				if p := recover(); p != nil {
					cd.Logger.Error("collector panicked", "panic", p, "stack", string(debug.Stack()))
				}
			}()
			// A return caused by the cancelled ctx is a normal stop.
			if err := c.Start(ctx, cd); err != nil && (ctx.Err() == nil || !errors.Is(err, context.Canceled)) {
				cd.Logger.Error("collector stopped", "err", err)
			}
		}()
	}
}
