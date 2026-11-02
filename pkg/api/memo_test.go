package api

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/rulesets"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

// The route list borrows its Authorizer memo from seenPool. These tests
// swap the pool so reuse is certain, then check that callers stay apart.

// callerAuthz decides by the X-Caller header: "a" is denied hiddenSome,
// "b" is denied everything else, and "boom" answers as "b" three times,
// then panics. It counts calls per caller.
type callerAuthz struct {
	mu    sync.Mutex
	asked map[string]map[objects.Ref]int
	booms int
}

func (c *callerAuthz) Authorize(r *http.Request, ref objects.Ref) bool {
	who := r.Header.Get("X-Caller")
	c.mu.Lock()
	defer c.mu.Unlock()
	if who == "boom" {
		if c.booms++; c.booms > 3 {
			panic("authorizer failed")
		}
		return hiddenSome(ref)
	}
	if c.asked[who] == nil {
		c.asked[who] = map[objects.Ref]int{}
	}
	c.asked[who][ref]++
	return hiddenSome(ref) == (who == "b")
}

// take returns who's calls since the last take and resets them.
func (c *callerAuthz) take(who string) map[objects.Ref]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.asked[who]
	delete(c.asked, who)
	return out
}

// freshMemos hands out a new memo every time, as no pool would.
type freshMemos struct{}

func (freshMemos) Get() any { return map[objects.Ref]bool{} }
func (freshMemos) Put(any)  {}

// chanMemos hands out the same few memos over and over; Get waits for one.
// Put fails the test when a memo comes back with entries in it.
type chanMemos struct {
	t  *testing.T
	ch chan map[objects.Ref]bool
}

func newChanMemos(t *testing.T, n int) *chanMemos {
	p := &chanMemos{t: t, ch: make(chan map[objects.Ref]bool, n)}
	for range n {
		p.ch <- map[objects.Ref]bool{}
	}
	return p
}

func (p *chanMemos) Get() any { return <-p.ch }
func (p *chanMemos) Put(x any) {
	m := x.(map[objects.Ref]bool)
	if len(m) != 0 {
		p.t.Errorf("a memo went back to the pool with %d entries", len(m))
	}
	p.ch <- m
}

// usePool swaps seenPool for p for the rest of t.
func usePool(t *testing.T, p interface {
	Get() any
	Put(any)
}) {
	saved := seenPool
	seenPool = p
	t.Cleanup(func() { seenPool = saved })
}

// reply is one response as a caller sees it, with its Authorizer calls.
type reply struct {
	status int
	body   []byte
	calls  map[objects.Ref]int
}

// ask sends GET path as who; err is set when the server dropped the request.
func ask(srv *httptest.Server, who, path string) (status int, body []byte, err error) {
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-Caller", who)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err = io.ReadAll(resp.Body)
	return resp.StatusCode, body, err
}

// memoWorld starts visWorld under a callerAuthz and records each caller's
// replies with a fresh memo per request, the baseline the pooled runs must match.
func memoWorld(t *testing.T) (*httptest.Server, *callerAuthz, map[string]map[string]reply) {
	a := &callerAuthz{asked: map[string]map[objects.Ref]int{}}
	_, srv, _ := startWith(t, setup{authz: a}, visWorld()...)
	saved := seenPool
	seenPool = freshMemos{}
	defer func() { seenPool = saved }()
	want := map[string]map[string]reply{}
	for _, who := range []string{"a", "b"} {
		want[who] = map[string]reply{}
		for _, p := range endpoints {
			st, body, err := ask(srv, who, p)
			if err != nil {
				t.Fatalf("%s GET %s: %v", who, p, err)
			}
			want[who][p] = reply{st, body, a.take(who)}
		}
	}
	// The callers disagree, or the test proves nothing.
	if p := base + "/routes"; bytes.Equal(want["a"][p].body, want["b"][p].body) {
		t.Fatalf("callers a and b see the same %s", p)
	}
	return srv, a, want
}

// check compares one pooled reply with the baseline.
func check(t *testing.T, who, p string, got, want reply) {
	t.Helper()
	if got.status != want.status || !bytes.Equal(got.body, want.body) {
		t.Errorf("%s GET %s with a reused memo: %d %s\nwant %d %s", who, p, got.status, got.body, want.status, want.body)
	}
	if got.calls != nil && !maps.Equal(got.calls, want.calls) {
		t.Errorf("%s GET %s with a reused memo asked %v, want %v", who, p, got.calls, want.calls)
	}
}

// Callers with opposite decisions, one after the other on one memo, each
// get the baseline rows, totals and restricted facts, and each asks the
// Authorizer itself.
func TestPooledMemoSequentialCallers(t *testing.T) {
	srv, a, want := memoWorld(t)
	usePool(t, newChanMemos(t, 1))
	for range 20 {
		for _, who := range []string{"a", "b"} {
			for _, p := range endpoints {
				st, body, err := ask(srv, who, p)
				if err != nil {
					t.Fatalf("%s GET %s: %v", who, p, err)
				}
				check(t, who, p, reply{st, body, a.take(who)}, want[who][p])
			}
		}
	}
}

// The same callers at once, sharing two memos, under -race.
func TestPooledMemoConcurrentCallers(t *testing.T) {
	srv, _, want := memoWorld(t)
	usePool(t, newChanMemos(t, 2))
	var wg sync.WaitGroup
	for i := range 8 {
		who := []string{"a", "b"}[i%2]
		wg.Go(func() {
			for range 10 {
				for _, p := range endpoints {
					st, body, err := ask(srv, who, p)
					if err != nil {
						t.Errorf("%s GET %s: %v", who, p, err)
						return
					}
					// Calls interleave across a caller's goroutines, so only the replies are compared.
					check(t, who, p, reply{status: st, body: body}, want[who][p])
				}
			}
		})
	}
	wg.Wait()
}

// An Authorizer that panics mid-list leaves the memo clean: the next
// caller on the same memo gets its baseline.
func TestPooledMemoAfterAuthorizerPanic(t *testing.T) {
	srv, a, want := memoWorld(t)
	pool := newChanMemos(t, 1)
	usePool(t, pool)
	p := base + "/routes"
	// Fill the memo with b's answers first, so a leak would show in a's reply.
	if _, _, err := ask(srv, "b", p); err != nil {
		t.Fatal(err)
	}
	a.take("b")
	if _, _, err := ask(srv, "boom", p); err == nil {
		t.Fatal("the panicking request returned a response")
	}
	// The server recovered the panic; the memo is back in the pool, empty.
	if len(pool.ch) != 1 {
		t.Fatalf("%d memos in the pool after the panic, want 1", len(pool.ch))
	}
	for _, who := range []string{"a", "b"} {
		st, body, err := ask(srv, who, p)
		if err != nil {
			t.Fatal(err)
		}
		check(t, who, p, reply{st, body, a.take(who)}, want[who][p])
	}
}

// scriptSnaps serves s stamped at now; both engines of a script read it.
type scriptSnaps struct {
	s   *cache.Snapshot
	now *time.Time
}

func (m *scriptSnaps) Snapshot() *cache.Snapshot {
	s := *m.s
	s.TakenAt = *m.now
	return &s
}

// scriptVerifier returns *r; both engines of a script read it.
type scriptVerifier struct{ r *scopeverify.Result }

func (scriptVerifier) Name() string                                  { return "script" }
func (v scriptVerifier) Verify(scopeverify.Input) scopeverify.Result { return *v.r }

// memoProbes are memoizable checks, one per kind of input; each run counts in runs by name.
func memoProbes(runs map[string]int) []health.Check {
	routes := objects.GroupKind{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute"}
	probe := func(name string, in health.InputSet, detail func(ctx health.RunContext) string) health.Check {
		return scriptCheck{name, in, func(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
			runs[name]++
			return []health.Finding{{Code: "ACME-001", Slug: "probe", Severity: health.SeverityInfo, Install: ctx.Install.ID(), Message: name, Detail: detail(ctx)}},
				health.CheckResult{Name: name, OK: true, CollectedAt: ctx.Now}
		}}
	}
	at := scriptCheck{"probe stamped", health.InputSet{Collected: []string{"probe/at"}}, func(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
		runs["probe stamped"]++
		v, _ := ctx.Collected("probe/at")
		return nil, health.CheckResult{Name: "probe stamped", OK: true, CollectedAt: v.At}
	}}
	return []health.Check{
		probe("probe collected", health.InputSet{Collected: []string{"probe/value"}}, func(ctx health.RunContext) string {
			v, _ := ctx.Collected("probe/value")
			return fmt.Sprintf("%s %v", v.State, v.Data)
		}),
		at,
		probe("probe waker", health.InputSet{}, func(ctx health.RunContext) string {
			start, ok := health.Load[time.Time](ctx.State, "start")
			if !ok {
				start = ctx.Now
				ctx.State.Set("start", start)
			}
			ctx.WakeAt(start.Add(45 * time.Second))
			return fmt.Sprint("due ", !ctx.Now.Before(start.Add(45*time.Second)))
		}),
		probe("probe access", health.InputSet{Checks: []string{"cluster access"}}, func(ctx health.RunContext) string {
			o, _ := ctx.Output("cluster access")
			return fmt.Sprintf("%d findings, ok %v", len(o.Findings), o.Result.OK)
		}),
		probe("probe routes", health.InputSet{Kinds: []objects.GroupKind{routes}}, func(ctx health.RunContext) string {
			var rvs []string
			for _, s := range ctx.Served {
				if s.Group == routes.Group && s.Kind == routes.Kind {
					for _, u := range ctx.Snapshot.Objects[s.GVR()] {
						rvs = append(rvs, u.GetName()+"@"+u.GetResourceVersion())
					}
				}
			}
			return fmt.Sprint(rvs)
		}),
		probe("probe metrics", health.InputSet{Metrics: true}, func(ctx health.RunContext) string {
			return fmt.Sprint(ctx.HasMetrics, ctx.Metrics.Metrics.Rejects, ctx.Metrics.Metrics.PerPod)
		}),
		probe("probe verify", health.InputSet{Verify: true}, func(ctx health.RunContext) string {
			return fmt.Sprint(ctx.Verify.Available, len(ctx.Verify.Mismatches))
		}),
		probe("probe computed", health.InputSet{Computed: true}, func(ctx health.RunContext) string {
			if ctx.Computed == nil {
				return "none"
			}
			return ctx.Computed.RuleSet
		}),
		probe("probe kinds", health.InputSet{KindsChange: true}, func(ctx health.RunContext) string { return ctx.KindsChange.String() }),
	}
}

// scriptCheck is a check whose Run is a func.
type scriptCheck struct {
	name string
	in   health.InputSet
	run  func(ctx health.RunContext) ([]health.Finding, health.CheckResult)
}

func (c scriptCheck) Name() string            { return c.name }
func (c scriptCheck) Inputs() health.InputSet { return c.in }
func (c scriptCheck) Run(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
	return c.run(ctx)
}

// Review finding 13: a memoizing and a non-memoizing engine over the same
// scripted sequence serve byte-equal bodies after every step.
func TestMemoizedResponsesEqualUncached(t *testing.T) {
	h := newHarness(t, setup{}, world()...)
	now := testutil.Now
	src := &scriptSnaps{s: h.cache.Snapshot(), now: &now}
	col := collect.New(nil, func() time.Time { return now })
	col.Publish("probe/value", "v1", 1, 20*time.Second)
	now = testutil.Now.Add(-time.Second) // apart from Now, so the stamp is the value's time alone
	col.Publish("probe/at", "same", 7, 0)
	now = testutil.Now
	metrics := okMetrics("kgw.kgateway")
	verify := &scopeverify.Result{Available: true}
	ruleVersion, change := "v2.4.7", kinds.Change{}
	type side struct {
		e    *engine.Engine
		srv  *httptest.Server
		runs map[string]int
	}
	newSide := func(noMemo bool) *side {
		s := &side{runs: map[string]int{}}
		s.e = engine.New(engine.Config{Snapshots: src, Served: testutil.ServedUpstream(), Detectors: []install.Detector{install.UpstreamDetector{}},
			Scope: install.NamespaceSelectorScope{}, Verifier: scriptVerifier{verify}, Metrics: metrics,
			Checks: append(health.Upstream(), memoProbes(s.runs)...), MetricsMaxAge: 30 * time.Second, Now: func() time.Time { return now },
			Collected: col.Snapshot, NoMemo: noMemo, RulesFor: func(string) computed.Selection { return rulesets.Select(ruleVersion) },
			KindsChange: func() kinds.Change { return change }})
		s.srv = httptest.NewServer(NewHandler(s.e, fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}}, nil))
		t.Cleanup(s.srv.Close)
		return s
	}
	memo, plain := newSide(false), newSide(true)

	hr := testutil.ServedUpstream()[slices.IndexFunc(testutil.ServedUpstream(), func(s kinds.Served) bool { return s.Kind == "HTTPRoute" })].GVR()
	withAccess := func(a cache.Access) {
		s := *src.s
		s.Access, s.Source.Access = maps.Clone(s.Access), maps.Clone(s.Source.Access)
		s.Access[hr], s.Source.Access[hr.String()] = a, a
		src.s = &s
	}
	steps := []struct {
		name string
		do   func()
	}{
		{"first compute", func() {}},
		{"unchanged", func() {}},
		{"annotation-only edit", func() {
			s := *src.s
			s.Objects, s.Source.Kinds = maps.Clone(s.Objects), maps.Clone(s.Source.Kinds)
			us := slices.Clone(s.Objects[hr])
			u := us[0].DeepCopy()
			u.SetAnnotations(map[string]string{"note": "edited"})
			u.SetResourceVersion(u.GetResourceVersion() + "-edited")
			us[0] = u
			s.Objects[hr] = us
			k := s.Source.Kinds[hr.String()]
			k.Hash++
			s.Source.Kinds[hr.String()] = k
			src.s = &s
		}},
		{"access failure", func() { withAccess(cache.Access{Err: "watch failed"}) }},
		{"access recovery", func() { withAccess(cache.Access{OK: true}) }},
		{"metrics reject", func() {
			r := metrics["kgw.kgateway"]
			r.Metrics.Rejects = map[ctrlmetrics.GatewayKey]float64{{Namespace: "infra", Name: "edge"}: 1}
			metrics["kgw.kgateway"] = r
		}},
		{"per-Pod dropped updates", func() {
			r := metrics["kgw.kgateway"]
			r.Metrics.PerPod = []ctrlmetrics.PodMetrics{{UID: "u1", Name: "kgateway-0", Dropped: 1, DroppedPresent: true}}
			metrics["kgw.kgateway"] = r
		}},
		{"per-Pod rejects by type", func() {
			r := metrics["kgw.kgateway"]
			k := ctrlmetrics.TypeKey{Gateway: ctrlmetrics.GatewayKey{Namespace: "infra", Name: "edge"}, TypeURL: "envoy.config.cluster.v3.Cluster"}
			r.Metrics.PerPod = []ctrlmetrics.PodMetrics{{UID: "u1", Name: "kgateway-0", Dropped: 1, DroppedPresent: true, Total: map[ctrlmetrics.TypeKey]float64{k: 4}}}
			metrics["kgw.kgateway"] = r
		}},
		{"verifier mismatch", func() {
			verify.Mismatches = []scopeverify.Mismatch{{Namespace: "shop", Resource: "HTTPRoute", UI: 2, Controller: 1}}
		}},
		{"rule-set change", func() { ruleVersion = "v2.3.0" }},
		{"served-kinds diff", func() { change = kinds.Change{Removed: []string{"TCPRoute"}} }},
		{"collector republished with the same fingerprint", func() {
			now = testutil.Now.Add(2 * time.Second)
			col.Publish("probe/at", "same", 7, 0)
			now = testutil.Now.Add(3 * time.Second)
		}},
		{"collector value expires", func() { now = now.Add(21 * time.Second) }},
		{"deadline passes", func() { now = testutil.Now.Add(health.Grace + time.Second) }},
	}
	paths := []string{"/api/v1/installs/kgw.kgateway/health", "/api/v1/installs/kgw.kgateway/overview", "/api/v1/installs/kgw.kgateway/findings",
		"/api/v1/installs/kgw.kgateway/findings?sort=since.desc&limit=1"}
	for _, st := range steps {
		st.do()
		memo.e.Compute()
		plain.e.Compute()
		// Group ids, membership and rank order match too.
		if got, want := memo.e.Current().Model.Installs[0].Diagnosis, plain.e.Current().Model.Installs[0].Diagnosis; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: diagnosis differs with memoization:\n got %+v\nwant %+v", st.name, got, want)
		}
		for _, p := range paths {
			_, want, err := ask(plain.srv, "", p)
			if err != nil {
				t.Fatal(err)
			}
			_, got, err := ask(memo.srv, "", p)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s: GET %s differs with memoization:\n got %s\nwant %s", st.name, p, got, want)
			}
			// The probes' findings carry an id, a first-seen since and a group, so all three are compared.
			if p == paths[0] && (!bytes.Contains(got, []byte(`"id":"f1-`)) || !bytes.Contains(got, []byte(`"since":`)) ||
				!bytes.Contains(got, []byte(`"group":"f1-`))) {
				t.Errorf("%s: GET %s has no finding id, since or group:\n%s", st.name, p, got)
			}
		}
	}
	// Each probe was skipped at least once, and the republish was served from the memo.
	for name, n := range plain.runs {
		if memo.runs[name] >= n {
			t.Errorf("%s ran %d times memoized, %d plain: never skipped", name, memo.runs[name], n)
		}
	}
	if n := memo.runs["probe stamped"]; n != 1 {
		t.Errorf("probe stamped ran %d times memoized, want once", n)
	}
}
