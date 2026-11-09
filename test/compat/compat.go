// Package compat smoke-tests a running core-only build against any Gateway API
// controller (Phase 5 design §12.1). The `compat`-tagged test reads COMPAT_CONTROLLER,
// COMPAT_GATEWAY, COMPAT_ROUTE, COMPAT_UI_URL and COMPAT_DEFAULT_UI_URL (see Env).
package compat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/DuncanDoyle/snellius/pkg/api"
	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// Env is the smoke test's input. Gateway and Route are "namespace/name"; the route is an HTTPRoute.
type Env struct {
	Controller   string // COMPAT_CONTROLLER: the GatewayClass controllerName
	Gateway      string // COMPAT_GATEWAY
	Route        string // COMPAT_ROUTE
	UIURL        string // COMPAT_UI_URL: the core-only build
	DefaultUIURL string // COMPAT_DEFAULT_UI_URL: the default build, class-only installs off
}

// EnvFromOS reads Env from COMPAT_* variables; every one is required.
func EnvFromOS() (Env, error) {
	e := Env{Controller: os.Getenv("COMPAT_CONTROLLER"), Gateway: os.Getenv("COMPAT_GATEWAY"), Route: os.Getenv("COMPAT_ROUTE"),
		UIURL: strings.TrimSuffix(os.Getenv("COMPAT_UI_URL"), "/"), DefaultUIURL: strings.TrimSuffix(os.Getenv("COMPAT_DEFAULT_UI_URL"), "/")}
	var errs []error
	for name, v := range map[string]string{"COMPAT_CONTROLLER": e.Controller, "COMPAT_UI_URL": e.UIURL, "COMPAT_DEFAULT_UI_URL": e.DefaultUIURL} {
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", name))
		}
	}
	for name, v := range map[string]string{"COMPAT_GATEWAY": e.Gateway, "COMPAT_ROUTE": e.Route} {
		if ns, n, ok := strings.Cut(v, "/"); !ok || ns == "" || n == "" || strings.Contains(n, "/") {
			errs = append(errs, fmt.Errorf("%s must be namespace/name, got %q", name, v))
		}
	}
	return e, errors.Join(errs...)
}

// packCodes are the kgateway pack's finding codes (design §6); a class-only install never raises one.
var packCodes = []string{health.CodeXDSRejected, health.CodeScopeMismatch, health.CodeSelfManagedProxy, health.CodeDelegatedNotAttached,
	health.CodeComputedDisagrees, health.CodeRuleReplaced, health.CodePolicyTargetNotFound, health.CodeVersionSkew,
	health.CodeConfigWithheld, health.CodeProxyConfigWarming, health.CodeProxyDisconnected}

// Settle runs Check every `every` until it passes or timeout ends, and returns the last complete pass's errors.
func Settle(ctx context.Context, c *http.Client, e Env, timeout, every time.Duration) []error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var last []error
	for {
		errs := Check(ctx, c, e)
		if len(errs) == 0 {
			return nil
		}
		// A pass the deadline cut short says only "deadline exceeded"; the pass before it says why.
		if ctx.Err() != nil && last != nil {
			return last
		}
		last = errs
		select {
		case <-ctx.Done():
			return errs
		case <-time.After(every):
		}
	}
}

// Check makes one pass over the assertions of design §12.1 and returns every failure.
func Check(ctx context.Context, c *http.Client, e Env) []error {
	var errs []error
	fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	id := install.ClassOnlyID(e.Controller)

	// 1. One install, the controller's class-only install, with an assumed all-namespaces scope.
	var insts []api.InstallSummary
	if err := get(ctx, c, e.UIURL+"/api/v1/installs", &insts); err != nil {
		return append(errs, err)
	}
	if len(insts) != 1 {
		fail("want one install, got %d: %v", len(insts), ids(insts))
	}
	i := slices.IndexFunc(insts, func(s api.InstallSummary) bool { return s.ID == id })
	if i < 0 {
		return append(errs, fmt.Errorf("no install with ID %q (controller %s): %v", id, e.Controller, ids(insts)))
	}
	inst := insts[i]
	if s := inst.Scope; s == nil || s.Mode != string(install.ScopeAll) || s.Source != install.SourceAssumed {
		fail("install scope %+v, want mode all, source assumed", s)
	}
	// 2. Install verdict, then the Gateway and route with their status.
	if inst.Health.State == health.StateError {
		fail("install verdict is error: %+v", inst.Health.Counts)
	}
	base := e.UIURL + "/api/v1/installs/" + id
	var gws api.Page[api.GatewayListRow]
	if err := get(ctx, c, base+"/gateways?limit=200", &gws); err != nil {
		fail("%v", err)
	} else if j := slices.IndexFunc(gws.Items, func(r api.GatewayListRow) bool { return r.Ref.String() == e.Gateway }); j < 0 {
		fail("Gateway %s not listed", e.Gateway)
	} else if h := gws.Items[j].Health; h == health.StateError || h == health.StateUnknown || h == "" {
		fail("Gateway %s health %q, want its status read and not error", e.Gateway, h)
	}
	var routes api.Page[api.RouteRow]
	if err := get(ctx, c, base+"/routes?limit=200", &routes); err != nil {
		fail("%v", err)
	} else if j := slices.IndexFunc(routes.Items, func(r api.RouteRow) bool {
		return r.Ref.Kind == "HTTPRoute" && r.Ref.String() == e.Route
	}); j < 0 {
		fail("route %s not listed", e.Route)
	} else if !slices.ContainsFunc(routes.Items[j].Parents, func(p api.ParentSummary) bool {
		return p.Ref.Kind == "Gateway" && p.Ref.String() == e.Gateway && p.Attachment == explore.AttachmentAccepted
	}) {
		fail("route %s is not accepted by Gateway %s: %+v", e.Route, e.Gateway, routes.Items[j].Parents)
	}
	// 3. No kgateway kind, no pack, and no feature. Both lists are sent empty: an absent one means an older server.
	var meta api.MetaResponse
	if err := get(ctx, c, e.UIURL+"/api/v1/meta", &meta); err != nil {
		fail("%v", err)
	} else {
		if k := slices.IndexFunc(meta.Kinds, func(k kinds.Served) bool { return k.Group == kinds.KgatewayGroup }); k >= 0 {
			fail("/meta lists %s/%s", meta.Kinds[k].Group, meta.Kinds[k].Kind)
		}
		if meta.Packs == nil || len(meta.Packs) > 0 {
			fail("/meta packs %v, want present and empty", meta.Packs)
		}
	}
	if inst.Features == nil || len(inst.Features) > 0 {
		fail("install features %v, want present and empty (no delegation, computed, dataplane or processes)", inst.Features)
	}
	// 4. No finding with a kgateway pack code.
	var h api.HealthResponse
	if err := get(ctx, c, base+"/health", &h); err != nil {
		fail("%v", err)
	}
	for _, f := range h.Findings {
		if slices.Contains(packCodes, f.Code) {
			fail("pack finding %s on %v: %s", f.Code, ref(f.Resource), f.Message)
		}
	}
	// 5. The default build, class-only installs off, does not claim the controller once it has synced.
	var defMeta api.MetaResponse
	var def []api.InstallSummary
	if err := get(ctx, c, e.DefaultUIURL+"/api/v1/meta", &defMeta); err != nil {
		fail("default build: %v", err)
	} else if !defMeta.Synced {
		fail("default build has not synced yet")
	} else if err := get(ctx, c, e.DefaultUIURL+"/api/v1/installs", &def); err != nil {
		fail("default build: %v", err)
	}
	for _, s := range def {
		if slices.Contains(s.ControllerNames, e.Controller) {
			fail("the default build claims %s as install %s", e.Controller, s.ID)
		}
	}
	return errs
}

// get decodes url's JSON into v; anything but 200 is an error.
func get(ctx context.Context, c *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	return nil
}

func ids(insts []api.InstallSummary) []string {
	out := make([]string, 0, len(insts))
	for _, s := range insts {
		out = append(out, s.ID)
	}
	return out
}

func ref(r *api.LinkedRef) objects.Ref {
	if r == nil {
		return objects.Ref{}
	}
	return r.Ref
}
