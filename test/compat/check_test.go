package compat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/app"
	"github.com/DuncanDoyle/snellius/pkg/extension"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// controller is a made-up controller name; the smoke test names no implementation (D-065).
const controller = "example.com/gateway"

// serve starts reg's backend over a cluster with one controller's class, Gateway and route.
func serve(t *testing.T, reg *extension.Registry, classOnly bool) string {
	t.Helper()
	gw := testutil.GwRef("infra", "edge")
	ok := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}
	dyn, kube := testutil.ClientsWith(t, nil, testutil.Namespace("infra", nil), testutil.Namespace("shop", nil),
		testutil.GatewayClass("other", controller, true), testutil.Gateway("infra", "edge", "other", nil),
		testutil.HTTPRoute("shop", "web", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: controller, Conds: ok}))
	a, err := app.Build(app.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery()},
		app.Options{PollInterval: 50 * time.Millisecond, MetricsInterval: time.Hour, ClassOnlyInstalls: classOnly,
			UI: fstest.MapFS{"index.html": {Data: []byte("x")}}}, reg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.Start(ctx)
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

// mutating proxies upstream, rewriting the JSON body of path with f.
func mutating(t *testing.T, upstream, path string, f func(any) any) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(upstream + r.URL.RequestURI())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		var v any
		if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if r.URL.Path == path {
			v = f(v)
		}
		w.WriteHeader(resp.StatusCode)
		_ = json.NewEncoder(w).Encode(v)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func env(core, def string) Env {
	return Env{Controller: controller, Gateway: "infra/edge", Route: "shop/web", UIURL: core, DefaultUIURL: def}
}

// The smoke test passes against the core-only build over a class-only
// install, and the default build does not claim the controller (design §12.1).
func TestCheckPassesOnCoreBuild(t *testing.T) {
	e := env(serve(t, extension.NewCore(), true), serve(t, extension.New(), false))
	if errs := Settle(context.Background(), http.DefaultClient, e, 10*time.Second, 50*time.Millisecond); len(errs) > 0 {
		t.Fatalf("smoke test failed on the core build: %v", errs)
	}
}

// Each assertion of design §12.1 catches its own break.
func TestCheckCatchesEachBreak(t *testing.T) {
	core, def := serve(t, extension.NewCore(), true), serve(t, extension.New(), false)
	if errs := Settle(context.Background(), http.DefaultClient, env(core, def), 10*time.Second, 50*time.Millisecond); len(errs) > 0 {
		t.Fatalf("fixture never settled: %v", errs)
	}
	const id = "/api/v1/installs/example.com~2Fgateway"
	first := func(v any) map[string]any { return v.([]any)[0].(map[string]any) }
	items := func(v any) []any { return v.(map[string]any)["items"].([]any) }
	cases := []struct {
		name, path string
		f          func(any) any
		want       string
	}{
		{"a second install", "/api/v1/installs", func(v any) any { return append(v.([]any), first(v)) }, "want one install"},
		{"another ID", "/api/v1/installs", func(v any) any { first(v)["id"] = "infra.other"; return v }, "no install with ID"},
		{"a read scope", "/api/v1/installs", func(v any) any {
			first(v)["scope"] = map[string]any{"mode": "all", "source": "deployment template"}
			return v
		}, "scope"},
		{"an error verdict", "/api/v1/installs", func(v any) any {
			first(v)["health"].(map[string]any)["state"] = "error"
			return v
		}, "verdict"},
		{"a pack feature", "/api/v1/installs", func(v any) any { first(v)["features"] = []any{"delegation"}; return v }, "delegation"},
		{"the processes feature", "/api/v1/installs", func(v any) any { first(v)["features"] = []any{"processes"}; return v }, "processes"},
		{"no features list", "/api/v1/installs", func(v any) any { delete(first(v), "features"); return v }, "features"},
		{"a pack listed", "/api/v1/meta", func(v any) any { v.(map[string]any)["packs"] = []any{"kgateway"}; return v }, "packs"},
		{"no packs list", "/api/v1/meta", func(v any) any { delete(v.(map[string]any), "packs"); return v }, "packs"},
		{"a kgateway kind", "/api/v1/meta", func(v any) any {
			m := v.(map[string]any)
			m["kinds"] = append(m["kinds"].([]any), map[string]any{"group": "gateway.kgateway.dev", "kind": "Backend"})
			return v
		}, "gateway.kgateway.dev"},
		{"a pack finding", id + "/health", func(v any) any {
			m := v.(map[string]any)
			m["findings"] = append(m["findings"].([]any), map[string]any{"code": "KGWUI-001", "severity": "error"})
			return v
		}, "KGWUI-001"},
		{"no Gateway", id + "/gateways", func(v any) any { v.(map[string]any)["items"] = []any{}; return v }, "Gateway infra/edge"},
		{"a Gateway without status", id + "/gateways", func(v any) any {
			items(v)[0].(map[string]any)["health"] = "unknown"
			return v
		}, "Gateway infra/edge"},
		{"no route", id + "/routes", func(v any) any { v.(map[string]any)["items"] = []any{}; return v }, "route shop/web"},
		{"a route not accepted", id + "/routes", func(v any) any {
			p := items(v)[0].(map[string]any)["parents"].([]any)[0].(map[string]any)
			p["attachment"] = "not-accepted"
			return v
		}, "route shop/web"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := Check(context.Background(), http.DefaultClient, env(mutating(t, core, tc.path, tc.f), def))
			if !mentions(errs, tc.want) {
				t.Fatalf("errors %v, want one naming %q", errs, tc.want)
			}
		})
	}
	t.Run("the default build claims the controller", func(t *testing.T) {
		errs := Check(context.Background(), http.DefaultClient, env(core, serve(t, extension.New(), true)))
		if !mentions(errs, "default build") {
			t.Fatalf("errors %v, want one naming the default build", errs)
		}
	})
}

// The default build must be synced before its empty install list counts: one
// that answers empty during startup and claims the controller later fails (review #4).
func TestCheckWaitsForASyncedDefaultBuild(t *testing.T) {
	core, def := serve(t, extension.NewCore(), true), serve(t, extension.New(), true)
	var calls atomic.Int32
	// The default build's first answers: not synced, no installs; then the real, claiming ones.
	starting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 4 {
			switch r.URL.Path {
			case "/api/v1/meta":
				_, _ = w.Write([]byte(`{"synced":false,"kinds":[],"packs":["kgateway"]}`))
				return
			case "/api/v1/installs":
				_, _ = w.Write([]byte(`[]`))
				return
			}
		}
		http.Redirect(w, r, def+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	}))
	t.Cleanup(starting.Close)
	errs := Settle(context.Background(), http.DefaultClient, env(core, starting.URL), 3*time.Second, 50*time.Millisecond)
	if !mentions(errs, "default build claims") {
		t.Fatalf("errors %v, want the default build's later claim", errs)
	}
	if calls.Load() <= 4 {
		t.Fatal("Settle stopped while the default build was still starting")
	}
}

// Env reads every input from the environment and rejects a missing one.
func TestEnvFromOS(t *testing.T) {
	for k, v := range map[string]string{"COMPAT_CONTROLLER": controller, "COMPAT_GATEWAY": "infra/edge", "COMPAT_ROUTE": "shop/web",
		"COMPAT_UI_URL": "http://a", "COMPAT_DEFAULT_UI_URL": "http://b"} {
		t.Setenv(k, v)
	}
	if e, err := EnvFromOS(); err != nil || e != env("http://a", "http://b") {
		t.Fatalf("EnvFromOS() = %+v, %v", e, err)
	}
	t.Setenv("COMPAT_ROUTE", "web")
	if _, err := EnvFromOS(); err == nil || !strings.Contains(err.Error(), "COMPAT_ROUTE") {
		t.Fatalf("a route without namespace: err %v", err)
	}
}

func mentions(errs []error, s string) bool {
	for _, err := range errs {
		if strings.Contains(err.Error(), s) {
			return true
		}
	}
	return false
}
