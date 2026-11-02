package extension

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/kgw23"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/rulesets"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
)

type otherDetector struct{ install.UpstreamDetector }

func (otherDetector) Name() string { return "other" }

func TestNewAppliesOptionsOnTopOfUpstream(t *testing.T) {
	r := New(WithKinds(kinds.Descriptor{Kind: "Extra"}), WithDetectors(otherDetector{}))
	if r.Kinds[len(r.Kinds)-1].Kind != "Extra" || len(r.Kinds) != len(kinds.Upstream())+1 {
		t.Fatal("extra kind must be appended to the upstream kinds")
	}
	if r.Detectors[0].Name() != "other" || r.Detectors[1].Name() != "kgateway" {
		t.Fatal("downstream detectors must be tried before the upstream detector")
	}
	if r.Scope == nil || len(r.Checks) == 0 {
		t.Fatal("upstream defaults missing")
	}
	// The upstream verifier depends on the metrics interval, so the app builds it.
	if r.Verifier != nil {
		t.Fatalf("the default verifier must be left to the app, got %T", r.Verifier)
	}
	if _, ok := r.Authorizer.(api.AllowAll); !ok {
		t.Fatalf("the default authorizer must be AllowAll (D-013), got %T", r.Authorizer)
	}
	deny := api.AuthorizerFunc(func(*http.Request, objects.Ref) bool { return false })
	if _, ok := New(WithAuthorizer(deny)).Authorizer.(api.AuthorizerFunc); !ok {
		t.Fatal("WithAuthorizer must replace the default")
	}
	v := scopeverify.NewManagedCountVerifier(1)
	if New(WithScopeVerifier(v)).Verifier != v {
		t.Fatal("WithScopeVerifier must set the verifier")
	}
}

// Kinds are unique by group and kind; a later registration replaces the
// earlier one in its position.
func TestWithKindsDedupesByGroupKind(t *testing.T) {
	up := kinds.Upstream()
	hr := up[4]
	hr.Versions = []string{"v2", "v1"}
	extra := kinds.Descriptor{Group: "acme.example.com", Kind: "Extra"}
	r := New(WithKinds(extra, hr, extra))
	if len(r.Kinds) != len(up)+1 || r.Kinds[4].Kind != "HTTPRoute" || r.Kinds[4].Versions[0] != "v2" || r.Kinds[len(r.Kinds)-1].Kind != "Extra" {
		t.Fatalf("want upstream kinds with HTTPRoute replaced in place, plus one Extra: %+v", r.Kinds)
	}
}

// A downstream rule selection replaces rulesets.Select, which is the default (D-020).
func TestWithComputedRulesReachesTheEngine(t *testing.T) {
	if got := New().RulesFor("2.3.1"); got.Rules.Version() != rulesets.Select("2.3.1").Rules.Version() {
		t.Fatalf("default must be rulesets.Select, got %s", got.Rules.Version())
	}
	down := func(string) computed.Selection { return computed.Selection{Rules: kgw23.New(), Note: "downstream"} }
	if got := New(WithComputedRules(down)).RulesFor("acme-1.0"); got.Note != "downstream" || got.Rules.Version() != "kgateway-2.3" {
		t.Fatalf("WithComputedRules must set Registry.RulesFor, got %+v", got)
	}
}

type stubCheck struct{}

func (stubCheck) Name() string            { return "stub" }
func (stubCheck) Inputs() health.InputSet { return health.InputSet{} }
func (stubCheck) Run(health.RunContext) ([]health.Finding, health.CheckResult) {
	return nil, health.CheckResult{Name: "stub", OK: true}
}

func TestWithChecksRejectsReservedPrefix(t *testing.T) {
	err := New(WithChecks("KGWUI-", stubCheck{})).Err()
	if err == nil || !strings.Contains(err.Error(), "KGWUI-") {
		t.Fatalf("the upstream prefix must be rejected by name, got %v", err)
	}
}

func TestWithChecksRejectsDuplicatePrefix(t *testing.T) {
	if err := New(WithChecks("ACME-", stubCheck{})).Err(); err != nil {
		t.Fatalf("a first registration must succeed, got %v", err)
	}
	r := New(WithChecks("ACME-", stubCheck{}), WithChecks("ACME-", stubCheck{}))
	if err := r.Err(); err == nil || !strings.Contains(err.Error(), "ACME-") {
		t.Fatalf("a repeated prefix must be rejected, got %v", err)
	}
}

func TestWithChecksRejectsMalformedPrefix(t *testing.T) {
	for _, p := range []string{"acme", "ACME", "A-", "acme-", "ACME_", "ABCDEFGHIJKLMNOPQ-", ""} {
		r := New(WithChecks(p, stubCheck{}))
		if r.Err() == nil {
			t.Errorf("prefix %q must be rejected", p)
		}
		if len(r.Checks) != len(health.Upstream()) {
			t.Errorf("prefix %q: a rejected registration must add no checks", p)
		}
	}
	for _, p := range []string{"AB-", "ACME2-", "ABCDEFGHIJKLMNO-"} {
		if err := New(WithChecks(p, stubCheck{})).Err(); err != nil {
			t.Errorf("prefix %q must be accepted, got %v", p, err)
		}
	}
}

func TestErrJoinsEveryRegistrationError(t *testing.T) {
	err := New(WithChecks("acme", stubCheck{}), WithChecks("KGWUI-", stubCheck{})).Err()
	if err == nil || !strings.Contains(err.Error(), "acme") || !strings.Contains(err.Error(), "KGWUI-") {
		t.Fatalf("every error must be reported, got %v", err)
	}
}

func TestWithHandlersRejectsBadAndDuplicateNames(t *testing.T) {
	noop := func(Router, Deps) {}
	if err := New(WithHandlers("acme", noop), WithHandlers("acme-2", noop)).Err(); err != nil {
		t.Fatalf("valid names: %v", err)
	}
	err := New(WithHandlers("acme", noop), WithHandlers("acme", noop)).Err()
	if err == nil || !strings.Contains(err.Error(), `"acme" is already registered`) {
		t.Fatalf("duplicate name: %v", err)
	}
	for _, n := range []string{"", "Acme", "1acme", "acme/x", "..", "a_b", strings.Repeat("a", 64)} {
		if New(WithHandlers(n, noop)).Err() == nil {
			t.Errorf("name %q: want a registration error", n)
		}
	}
}

// Mounts runs each extension's function with its own Deps and returns its
// endpoints; an endpoint that leaves its prefix fails the whole set.
func TestRegistryMounts(t *testing.T) {
	got := map[string]Deps{}
	reg := func(name string, patterns ...string) Option {
		return WithHandlers(name, func(r Router, d Deps) {
			got[name] = d
			for _, p := range patterns {
				r.Handle(p, http.NotFoundHandler())
			}
		})
	}
	m, err := New(reg("acme", "GET /a", "POST /b/{id}"), reg("other", "/")).Mounts(Deps{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, x := range m {
		names = append(names, x.Name+" "+x.Pattern)
	}
	if want := []string{"acme GET /a", "acme POST /b/{id}", "other /"}; strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("mounts %v, want %v", names, want)
	}
	// Unset Deps fields get the defaults: AllowAll and a logger.
	if _, ok := got["acme"].Authorizer.(api.AllowAll); !ok || got["acme"].Logger == nil || got["other"].Logger == nil {
		t.Fatalf("deps defaults: %+v", got["acme"])
	}
	deny := api.AuthorizerFunc(func(*http.Request, objects.Ref) bool { return false })
	if _, err := New(reg("acme", "GET /a")).Mounts(Deps{Authorizer: deny}); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["acme"].Authorizer.(api.AuthorizerFunc); !ok {
		t.Fatalf("the given Authorizer must reach the handlers, got %T", got["acme"].Authorizer)
	}
	for _, p := range []string{"GET /../v1/meta", "hello", "GET /a//b"} {
		if _, err := New(reg("acme", p)).Mounts(Deps{}); err == nil {
			t.Errorf("pattern %q: want an error", p)
		}
	}
}

func TestCapabilitiesRegistry(t *testing.T) {
	one := func() []api.Capability { return []api.Capability{{Name: "acme-widgets", State: api.CapOn}} }
	two := func() []api.Capability { return []api.Capability{{Name: "acme-audit", State: api.CapOff}} }
	r := New(WithCapabilities(one), WithCapabilities(two))
	if got := r.CapabilityStates(); len(got) != 2 || got[0].Name != "acme-widgets" || got[1].Name != "acme-audit" {
		t.Fatalf("CapabilityStates = %+v, want both sources in order", got)
	}
	if err := r.CheckCapabilities([]string{"scope"}); err != nil {
		t.Fatal(err)
	}
	clash := func() []api.Capability { return []api.Capability{{Name: "events", State: api.CapOn}} }
	if New(WithCapabilities(clash)).CheckCapabilities([]string{"scope"}) == nil {
		t.Error("a core name: want an error from CheckCapabilities")
	}
	if New().CapabilityStates() != nil {
		t.Error("no source: want nil")
	}
}

type namedCollector string

func (c namedCollector) Name() string                    { return string(c) }
func (namedCollector) Start(context.Context, Deps) error { return nil }

func TestWithCollectorsRejectsBadAndDuplicateNames(t *testing.T) {
	r := New(WithCollectors(namedCollector("acme"), namedCollector("Bad_Name")), WithCollectors(namedCollector("acme")))
	err := r.Err()
	if err == nil || !strings.Contains(err.Error(), "Bad_Name") || !strings.Contains(err.Error(), `"acme" is already registered`) {
		t.Fatalf("Err = %v", err)
	}
	if got := r.Collectors(); len(got) != 1 || got[0].Name() != "acme" {
		t.Fatalf("Collectors = %v", got)
	}
}

// syncBuf is a log sink safe for the collector goroutine and the test.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// funcCollector runs f as its Start.
type funcCollector struct {
	name string
	f    func(context.Context, Deps) error
}

func (c funcCollector) Name() string                            { return c.name }
func (c funcCollector) Start(ctx context.Context, d Deps) error { return c.f(ctx, d) }

// runOne runs c once, waits for its deferred Stop to notify, and returns the
// log and the store. c publishes a value first so Stop has something to expire.
func runOne(t *testing.T, c funcCollector, cancel bool) (string, collect.Snapshot) {
	t.Helper()
	sig := trigger.New()
	store := collect.New(sig, testutil.FakeClock().Now)
	var log syncBuf
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	published := c.f
	c.f = func(ctx context.Context, d Deps) error {
		d.Publish("k", "v", 1, 0)
		return published(ctx, d)
	}
	New(WithCollectors(c)).RunCollectors(ctx, Deps{Logger: slog.New(slog.NewTextHandler(&log, nil))}, store)
	// Signals coalesce, so wait on the store's state rather than counting them.
	waitFor := func(what string, ok func(collect.Value, bool) bool) {
		for {
			if v, found := store.Snapshot().Get("acme/k"); ok(v, found) {
				return
			}
			testutil.Recv(t, sig.C(), what)
		}
	}
	waitFor("the publish", func(_ collect.Value, found bool) bool { return found })
	if cancel {
		stop()
	}
	waitFor("Stop", func(v collect.Value, _ bool) bool { return v.State == collect.Unavailable })
	return log.String(), store.Snapshot()
}

func TestCollectorCancelledReturnIsNotLogged(t *testing.T) {
	for name, wrap := range map[string]func(error) error{
		"bare":    func(err error) error { return err },
		"wrapped": func(err error) error { return fmt.Errorf("scrape: %w", err) },
	} {
		t.Run(name, func(t *testing.T) {
			log, snap := runOne(t, funcCollector{"acme", func(ctx context.Context, _ Deps) error {
				<-ctx.Done()
				return wrap(ctx.Err())
			}}, true)
			if strings.Contains(log, "collector stopped") {
				t.Errorf("a stop after cancellation must not log an error: %s", log)
			}
			if v, _ := snap.Get("acme/k"); v.State != collect.Unavailable {
				t.Errorf("value after Stop: %+v", v)
			}
		})
	}
	// An error without a cancellation is still logged once.
	log, _ := runOne(t, funcCollector{"acme", func(context.Context, Deps) error { return errors.New("boom") }}, false)
	if strings.Count(log, "collector stopped") != 1 || !strings.Contains(log, "boom") {
		t.Errorf("a real error must log once: %s", log)
	}
}

func TestCollectorPanicIsRecovered(t *testing.T) {
	log, snap := runOne(t, funcCollector{"acme", func(context.Context, Deps) error { panic("kaput") }}, false)
	if strings.Count(log, "collector panicked") != 1 || !strings.Contains(log, "kaput") {
		t.Errorf("a panic must log once: %s", log)
	}
	if v, _ := snap.Get("acme/k"); v.State != collect.Unavailable {
		t.Errorf("the deferred Stop must still run: %+v", v)
	}
}
