//go:build bench

package api

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/internal/testutil/benchgate"
	"github.com/DuncanDoyle/snellius/internal/testutil/scale"
	"github.com/DuncanDoyle/snellius/internal/testutil/scale/measure"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/ctrllogs"
	"github.com/DuncanDoyle/snellius/pkg/diagnose"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
	"github.com/DuncanDoyle/snellius/pkg/proxystats"
	"github.com/DuncanDoyle/snellius/pkg/scopeverify"
)

// The request and stream budgets of design §9.6 at Large (2.2–2.4).

// requestsPerMix is the sample count per filter mix (design §9.6 method).
const requestsPerMix = 10_000

// percentile is the q-th percentile (nearest rank) of sorted d.
func percentile(d []time.Duration, q int) time.Duration {
	i := (len(d)*q + 99) / 100
	return d[max(i, 1)-1]
}

// latencies sends n GETs of path through h from c goroutines and returns the
// sorted ServeHTTP durations. Any status but 200 fails tb.
func latencies(tb testing.TB, h http.Handler, path string, n, c int) []time.Duration {
	out := make([]time.Duration, n)
	var next atomic.Int64
	var bad atomic.Value
	var wg sync.WaitGroup
	for range c {
		wg.Go(func() {
			req := httptest.NewRequest(http.MethodGet, path, nil) // the mux writes to it: one per goroutine
			for i := next.Add(1) - 1; i < int64(n); i = next.Add(1) - 1 {
				w := httptest.NewRecorder()
				t0 := time.Now()
				h.ServeHTTP(w, req)
				out[i] = time.Since(t0)
				if w.Code != http.StatusOK {
					bad.Store(fmt.Sprintf("GET %s: %d %s", path, w.Code, w.Body))
				}
			}
		})
	}
	wg.Wait()
	if b := bad.Load(); b != nil {
		tb.Fatal(b)
	}
	slices.Sort(out)
	return out
}

// listRows is the item count and total of one list response from h.
func listRows(tb testing.TB, h http.Handler, path string) (items, total int) {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	var page struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	if w.Code != http.StatusOK {
		tb.Fatalf("GET %s: %d %s", path, w.Code, w.Body)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		tb.Fatal(err)
	}
	return len(page.Items), page.Total
}

// widest is the viewable key of m with the most rows.
func widest(m map[objects.Ref][]explore.RowID, keep func(objects.Ref) bool) objects.Ref {
	var best objects.Ref
	for r, ids := range m {
		if keep(r) && (len(ids) > len(m[best]) || len(ids) == len(m[best]) && r.String() < best.String()) {
			best = r
		}
	}
	return best
}

func refParam(r objects.Ref) string { return r.Group + "/" + r.Kind + "/" + r.Namespace + "/" + r.Name }

// budgetLabel is the label value labelWidest selects.
const budgetLabel = "budget-label"

// labelWidest rewrites the first child ref of the viewable HTTPRoute with the
// most child refs into a label ref under the wildcard namespace and labels
// that child. The selection is unchanged, but its scope is every in-scope
// HTTPRoute: the widest SelectionHidden scope (Task 64).
func labelWidest(can func(objects.Ref) bool) func(*cache.Snapshot) {
	return func(snap *cache.Snapshot) {
		var routes []*unstructured.Unstructured
		for gvr, objs := range snap.Objects {
			if gvr.Resource == "httproutes" {
				routes = objs
			}
		}
		childRefs := func(u *unstructured.Unstructured) []any {
			rules, _ := u.Object["spec"].(map[string]any)["rules"].([]any) // not NestedSlice, which copies
			if len(rules) == 0 {
				return nil
			}
			refs, _ := rules[0].(map[string]any)["backendRefs"].([]any)
			if len(refs) == 0 || refs[0].(map[string]any)["kind"] != "HTTPRoute" {
				return nil
			}
			return refs
		}
		var widest *unstructured.Unstructured
		for _, u := range routes {
			if n := len(childRefs(u)); n > 0 && can(objects.RefOf(u)) && (widest == nil || n > len(childRefs(widest))) {
				widest = u
			}
		}
		ref := childRefs(widest)[0].(map[string]any)
		for _, u := range routes {
			if u.GetNamespace() == ref["namespace"] && u.GetName() == ref["name"] {
				lbls := u.GetLabels()
				if lbls == nil {
					lbls = map[string]string{}
				}
				lbls[objects.DelegationLabelKey] = budgetLabel
				u.SetLabels(lbls)
			}
		}
		clear(ref)
		ref["group"], ref["kind"], ref["name"], ref["namespace"] = "delegation.kgateway.dev", "label", budgetLabel, install.WildcardAll
	}
}

// TestBudgetListRequests: per filter mix, 10,000 sequential requests give
// p95 < 50 ms and p99 < 100 ms, and 10,000 from 20 goroutines p95 < 100 ms;
// the details of the deepest delegated child and of the widest delegating
// parent p95 < 50 ms. Run under AllowAll, whose answers are precomputed, and
// under a restricting Authorizer, over Large with labelWidest's label ref.
func TestBudgetListRequests(t *testing.T) {
	for _, a := range []struct {
		name  string
		authz Authorizer
	}{{"allow-all", nil}, {"restricted", restricting}} {
		t.Run(a.name, func(t *testing.T) {
			can := func(r objects.Ref) bool { return a.authz == nil || a.authz.Authorize(nil, r) }
			h, m := tierHandler(t, scale.Large, a.authz, labelWidest(can))
			ix := m.Installs[0].Index
			if f, c := matured(m); f != 0 || c != 0 {
				t.Fatalf("the labelled Large world has %d KGWUI-012 of %d checks", f, c)
			}
			kindIs := func(k string) func(objects.Ref) bool {
				return func(r objects.Ref) bool { return r.Kind == k && can(r) }
			}
			gw, ls := widest(ix.ByParent, kindIs("Gateway")), widest(ix.ByParent, kindIs("ListenerSet"))
			parent := widest(ix.ByParentRoute, can)
			// The namespace with the most viewable not-accepted rows, so namespace+state returns rows.
			var ns string
			best := 0
			for n, ids := range ix.ByNamespace {
				if !can(objects.Ref{Namespace: n}) {
					continue
				}
				c := 0
				for _, id := range ids {
					if ix.Routes[id].Flags&explore.FlagNotAccepted != 0 {
						c++
					}
				}
				if c > best || c == best && c > 0 && n < ns {
					ns, best = n, c
				}
			}
			// The viewable Gateway listener with the most members, for the listener mix.
			var lk explore.ListenerKey
			for k, ms := range ix.ByListener {
				if k.Resource.Kind != "Gateway" || !can(k.Resource) {
					continue
				}
				if n := len(ix.ByListener[lk]); len(ms) > n || len(ms) == n && k.Resource.String()+k.Listener < lk.Resource.String()+lk.Listener {
					lk = k
				}
			}
			if lk.Listener == "" {
				t.Fatal("the Large tier has no listener members")
			}
			// The viewable parent with the most view B rows, for the delegation endpoint.
			var deleg objects.Ref
			for r, rows := range ix.Delegation {
				if n := len(ix.Delegation[deleg]); can(r) && (len(rows) > n || len(rows) == n && r.String() < deleg.String()) {
					deleg = r
				}
			}
			// It carries labelWidest's label ref. The restricted run hides part of its scope, so its
			// SelectionHidden check stops at the first hidden route; it is not a worst-case scan.
			label := slices.IndexFunc(ix.Delegation[deleg], func(i int) bool { return ix.DelegRows[i].RefAsWritten.Mode == objects.DelegationLabel })
			if label < 0 {
				t.Fatalf("the widest delegating parent %s has no label ref", deleg)
			}
			lr := ix.DelegRows[ix.Delegation[deleg][label]].Key
			labelKey := explore.RefKey{Parent: deleg, Rule: lr.Rule, Ref: lr.Ref}
			if a.authz != nil && !ix.SelectionHidden(labelKey, can) {
				t.Fatalf("the restricting Authorizer must hide the label ref's selection of %s", deleg)
			}
			t.Logf("widest delegating parent %s: %d view B rows; label ref rule %d ref %d, scope %d HTTPRoutes, selection hidden %v",
				deleg, len(ix.Delegation[deleg]), lr.Rule, lr.Ref, len(ix.RefScope[labelKey]), a.authz != nil && ix.SelectionHidden(labelKey, can))
			deepest, depth := deepestChild(ix, can)
			if depth == 0 || len(ix.ByParentRoute[parent]) == 0 {
				t.Fatal("the Large tier has no delegation")
			}
			t.Logf("parents: Gateway %s (%d rows), ListenerSet %s (%d rows), listener %s/%s (%d members), HTTPRoute %s (%d children); namespace %s (%d rows); deepest child %s at depth %d",
				gw, len(ix.ByParent[gw]), ls, len(ix.ByParent[ls]), lk.Resource, lk.Listener, len(ix.ByListener[lk]), parent, len(ix.ByParentRoute[parent]), ns, len(ix.ByNamespace[ns]), deepest, depth)
			base := "/api/v1/installs/" + m.Installs[0].Resolved.ID()
			mixes := []struct{ name, path string }{
				{"none", base + "/routes"},
				{"parent-gateway", base + "/routes?parent=" + refParam(gw)},
				{"parent-listenerset", base + "/routes?parent=" + refParam(ls)},
				{"parent+listener", base + "/routes?parent=" + refParam(lk.Resource) + "&listener=" + lk.Listener},
				{"namespace+state", base + "/routes?namespace=" + ns + "&state=not-accepted"},
				{"q", base + "/routes?q=r-01"},
				{"sort=health", base + "/routes?sort=health.desc"},
				{"parent-httproute", base + "/routes?parent=" + refParam(parent)},
				{"delegation-widest-parent", base + "/routes/gateway.networking.k8s.io/HTTPRoute/" + deleg.Namespace + "/" + deleg.Name + "/delegation"},
			}
			for _, mx := range mixes {
				items, total := listRows(t, h, mx.path)
				t.Logf("%-25s %d rows on the page, total %d", mx.name, items, total)
				if items == 0 {
					t.Fatalf("%s: GET %s returned no rows; it would measure nothing", mx.name, mx.path)
				}
				seq := latencies(t, h, mx.path, requestsPerMix, 1)
				conc := latencies(t, h, mx.path, requestsPerMix, 20)
				t.Logf("%-25s sequential p50 %v p95 %v p99 %v (budget p95 < 50 ms, p99 < 100 ms); 20 clients p50 %v p95 %v p99 %v (budget p95 < 100 ms)",
					mx.name, rd(percentile(seq, 50)), rd(percentile(seq, 95)), rd(percentile(seq, 99)),
					rd(percentile(conc, 50)), rd(percentile(conc, 95)), rd(percentile(conc, 99)))
				if p := percentile(seq, 95); p >= 50*time.Millisecond {
					benchgate.Timing(t, "%s: sequential p95 %v, budget < 50 ms", mx.name, p)
				}
				if p := percentile(seq, 99); p >= 100*time.Millisecond {
					benchgate.Timing(t, "%s: sequential p99 %v, budget < 100 ms", mx.name, p)
				}
				if p := percentile(conc, 95); p >= 100*time.Millisecond {
					benchgate.Timing(t, "%s: 20-client p95 %v, budget < 100 ms", mx.name, p)
				}
			}
			for _, dt := range []struct {
				name string
				ref  objects.Ref
			}{{"detail-deepest-child", deepest}, {"detail-widest-parent", deleg}} {
				d := latencies(t, h, base+"/routes/gateway.networking.k8s.io/HTTPRoute/"+dt.ref.Namespace+"/"+dt.ref.Name, requestsPerMix, 1)
				t.Logf("%-25s sequential p50 %v p95 %v p99 %v (budget p95 < 50 ms)", dt.name, rd(percentile(d, 50)), rd(percentile(d, 95)), rd(percentile(d, 99)))
				if p := percentile(d, 95); p >= 50*time.Millisecond {
					benchgate.Timing(t, "%s p95 %v, budget < 50 ms", dt.name, p)
				}
			}
			// A full breadcrumb trail: one GET …/refs per page view.
			trail := url.Values{"ref": {refParam(gw), refParam(ls), refParam(parent), refParam(deepest), refParam(deleg), refParam(lk.Resource)}}
			requestBudget(t, h, "refs-full-trail", base+"/refs?"+trail.Encode(), false)
		})
	}
}

// TestBudgetFindingsRequests: per findings mix and a finding detail, the list budgets
// of TestBudgetListRequests, in the matured worst case from a settled cache, allowed and restricted.
func TestBudgetFindingsRequests(t *testing.T) {
	for _, a := range []struct {
		name  string
		authz Authorizer
	}{{"allow-all", nil}, {"restricted", restricting}} {
		t.Run(a.name, func(t *testing.T) {
			tier := scale.Large.Contradicting().Worst()
			h, m := snapshotHandler(scale.LivePhase3(t, tier, nil).Cache.Snapshot(), tier, a.authz)
			in := m.Installs[0]
			if f, c := matured(m); f != c || c == 0 {
				t.Fatalf("worst case: %d KGWUI-012 of %d checks", f, c)
			}
			// The group with the most members, for the group mix and the detail.
			d := in.Diagnosis
			big := slices.MaxFunc(d.Groups, func(x, y diagnose.Group) int { return cmp.Compare(len(x.Members), len(y.Members)) })
			member := in.Health.Findings[big.Members[0]]
			if member.ID == "" {
				t.Fatal("the largest group's first finding has no id")
			}
			codes := map[string]int{}
			for _, f := range in.Health.Findings {
				codes[f.Code+"/"+string(f.Severity)]++
			}
			t.Logf("%d findings by code and severity %v; %d groups, largest %s (%s, %d members)", len(in.Health.Findings), codes, len(d.Groups), big.ID, big.Family, len(big.Members))
			base := "/api/v1/installs/" + in.Resolved.ID()
			mixes := []struct{ name, path string }{
				{"none", base + "/findings"},
				{"severity=error", base + "/findings?severity=error"},
				{"code=KGWUI-012", base + "/findings?code=" + health.CodeComputedDisagrees},
				{"q", base + "/findings?q=r-01"},
				{"group", base + "/findings?group=" + big.ID},
				{"sort=since", base + "/findings?sort=since.desc"},
				{"view=groups", base + "/findings?view=groups"},
				{"view=groups&severity=error", base + "/findings?view=groups&severity=error"},
				{"view=groups&q", base + "/findings?view=groups&q=r-01"},
			}
			for _, mx := range mixes {
				items, total := listRows(t, h, mx.path)
				t.Logf("%-25s %d rows on the page, total %d", mx.name, items, total)
				if items == 0 {
					t.Fatalf("%s: GET %s returned no rows; it would measure nothing", mx.name, mx.path)
				}
				requestBudget(t, h, mx.name, mx.path, true)
			}
			requestBudget(t, h, "detail-largest-group", base+"/findings/"+member.ID, false)
		})
	}
}

// TestBudgetDiagnosticsRequests: …/diagnostics and …/processes (with filters)
// hold the list budgets in the matured 3.2 measurement world, allowed and restricted.
func TestBudgetDiagnosticsRequests(t *testing.T) {
	for _, a := range []struct {
		name  string
		authz Authorizer
	}{{"allow-all", nil}, {"restricted", restricting}} {
		t.Run(a.name, func(t *testing.T) {
			h, m := measuredHandler(t, a.authz)
			in := m.Installs[0]
			base := "/api/v1/installs/" + in.Resolved.ID()
			proxies := 0
			for _, ps := range in.Process.Proxies {
				proxies += len(ps.Processes)
			}
			t.Logf("%d findings, %d groups, %d Gateways, %d proxy Pods", len(in.Health.Findings), len(in.Diagnosis.Groups), len(in.Owned.Gateways), proxies)
			for _, mx := range []struct{ name, path string }{
				{"diagnostics", base + "/diagnostics"},
				{"processes", base + "/processes"},
				{"processes?q", base + "/processes?q=g-01&health=healthy&health=unknown"},
				{"processes?restarts", base + "/processes?health=error&health=warning&restarts=true"},
				{"findings?view=groups", base + "/findings?view=groups"},
				{"findings?view=groups&code", base + "/findings?view=groups&code=" + health.CodeRefNotPermitted},
			} {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, mx.path, nil))
				t.Logf("%-25s %d B", mx.name, w.Body.Len())
				requestBudget(t, h, mx.name, mx.path, true)
			}
		})
	}
}

// measuredHandler is the handler over the matured 3.2 measurement world under
// authz: rejects on every Gateway, process data and stub proxy stats, as the engine's gates use it.
func measuredHandler(t *testing.T, authz Authorizer) (http.Handler, *engine.Model) {
	fc := testutil.FakeClock()
	w := measure.New(t, scale.Large.Contradicting().Worst(), fc)
	e, m := measuredEngine(t, w, fc, w.Live.Cache.Snapshot())
	return NewHandler(e, fstest.MapFS{}, authz), m
}

// measuredEngine is the engine over snap of the measurement world w, matured
// as measuredHandler's, and its model.
func measuredEngine(t *testing.T, w *measure.World, fc *clocktesting.FakeClock, snap *cache.Snapshot) (*engine.Engine, *engine.Model) {
	e := engine.New(engine.Config{Snapshots: staticSnap{snap}, Served: testutil.ServedUpstream(),
		Detectors: []install.Detector{install.UpstreamDetector{}}, Scope: install.NamespaceSelectorScope{},
		Verifier: scopeverify.NewManagedCountVerifier(time.Hour), Collected: w.Store.Snapshot,
		Checks: health.Upstream(), Now: fc.Now})
	w.SetInstalls(func() ([]proxystats.Install, *cache.Snapshot) { return statsInstalls(current(e)) })
	// Scrapes around testutil.Now, past every window and 5 min later, so windows, holds,
	// proxy conditions, 016 and 018 mature.
	e.Compute()
	w.Scrape(t)
	e.Compute()
	mature := testutil.Now.Add(max(engine.Window, health.Grace) + time.Second)
	for _, at := range []time.Time{mature, mature.Add(5 * time.Minute)} {
		fc.SetTime(at)
		w.Scrape(t)
		e.Compute()
	}
	m := current(e)
	if f, c := matured(m); f != c || c == 0 || !hasCodes(m, measuredCodes) || !hasCodes(m, analyzerCodes) {
		t.Fatalf("measurement world did not mature: %d KGWUI-012 of %d checks", f, c)
	}
	return e, m
}

// current is e's published model, nil before the first.
func current(e *engine.Engine) *engine.Model {
	if p := e.Current(); p != nil {
		return p.Model
	}
	return nil
}

// requestBudget gates path's sequential p95 < 50 ms and, for a list, its
// p99 < 100 ms and 20-client p95 < 100 ms.
func requestBudget(t *testing.T, h http.Handler, name, path string, list bool) {
	t.Helper()
	seq := latencies(t, h, path, requestsPerMix, 1)
	if !list {
		t.Logf("%-25s sequential p50 %v p95 %v p99 %v (budget p95 < 50 ms)", name, rd(percentile(seq, 50)), rd(percentile(seq, 95)), rd(percentile(seq, 99)))
		if p := percentile(seq, 95); p >= 50*time.Millisecond {
			benchgate.Timing(t, "%s p95 %v, budget < 50 ms", name, p)
		}
		return
	}
	conc := latencies(t, h, path, requestsPerMix, 20)
	t.Logf("%-25s sequential p50 %v p95 %v p99 %v (budget p95 < 50 ms, p99 < 100 ms); 20 clients p50 %v p95 %v p99 %v (budget p95 < 100 ms)",
		name, rd(percentile(seq, 50)), rd(percentile(seq, 95)), rd(percentile(seq, 99)),
		rd(percentile(conc, 50)), rd(percentile(conc, 95)), rd(percentile(conc, 99)))
	if p := percentile(seq, 95); p >= 50*time.Millisecond {
		benchgate.Timing(t, "%s: sequential p95 %v, budget < 50 ms", name, p)
	}
	if p := percentile(seq, 99); p >= 100*time.Millisecond {
		benchgate.Timing(t, "%s: sequential p99 %v, budget < 100 ms", name, p)
	}
	if p := percentile(conc, 95); p >= 100*time.Millisecond {
		benchgate.Timing(t, "%s: 20-client p95 %v, budget < 100 ms", name, p)
	}
}

// TestBudgetHealth reports /health's response size and latency, 1,000
// sequential requests and 1,000 from 20 goroutines (a worst-case response
// is about 2 MiB), for the consistent
// world and the worst case (every check matured), under AllowAll and a
// restricting Authorizer. §9.6 sets no /health budget, so nothing is asserted.
func TestBudgetHealth(t *testing.T) {
	for _, tier := range []scale.Tier{scale.Large, scale.Large.Contradicting()} {
		for _, a := range []struct {
			name  string
			authz Authorizer
		}{{"allow-all", nil}, {"restricted", restricting}} {
			t.Run(tier.Name+"/"+a.name, func(t *testing.T) {
				h, m := tierHandler(t, tier, a.authz)
				f, c := matured(m)
				path := "/api/v1/installs/" + m.Installs[0].Resolved.ID() + "/health"
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
				if rec.Code != http.StatusOK {
					t.Fatalf("GET %s: %d", path, rec.Code)
				}
				seq := latencies(t, h, path, 1000, 1)
				conc := latencies(t, h, path, 1000, 20)
				t.Logf("%s %s: %d findings (%d KGWUI-012 of %d checks), %d B (%.1f KiB); sequential p50 %v p95 %v p99 %v; 20 clients p50 %v p95 %v p99 %v",
					tier.Name, a.name, len(m.Installs[0].Health.Findings), f, c, rec.Body.Len(), float64(rec.Body.Len())/1024,
					rd(percentile(seq, 50)), rd(percentile(seq, 95)), rd(percentile(seq, 99)),
					rd(percentile(conc, 50)), rd(percentile(conc, 95)), rd(percentile(conc, 99)))
				if tier.Contradict && (f != c || c == 0) {
					t.Fatalf("worst case: %d KGWUI-012 of %d checks", f, c)
				}
			})
		}
	}
}

// rd rounds d for logs.
func rd(d time.Duration) time.Duration {
	if d < time.Millisecond {
		return d.Round(time.Microsecond)
	}
	return d.Round(10 * time.Microsecond)
}

// cpuTime is the process's user + system CPU time.
func cpuTime(tb testing.TB) time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		tb.Fatal(err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// gcCPU reads the runtime's estimates of GC and used (not idle) CPU time, in seconds.
func gcCPU() (gc, used float64) {
	s := []metrics.Sample{{Name: "/cpu/classes/gc/total:cpu-seconds"}, {Name: "/cpu/classes/total:cpu-seconds"}, {Name: "/cpu/classes/idle:cpu-seconds"}}
	metrics.Read(s)
	return s[0].Value.Float64(), s[1].Value.Float64() - s[2].Value.Float64()
}

// TestBudgetSustainedStream: over a real cache and Run loop at Large, one
// HTTPRoute update per 100 ms for 10 min (-short: 60 s) while 20 clients poll
// /routes every 5 s gives ≤ 1 recompute per s and process CPU ≤ 15 % of one
// core (rusage delta; it bounds the engine's share) in every run, and the median
// memory obtained from the OS over five runs stays ≤ 288 MiB, inside the chart's 512 MiB
// pod limit (D-043). GC share is logged. Both the consistent world and the worst case
// run, each in fresh processes so earlier tests' heap and CPU do not count; the
// worst case starts once every check has matured.
func TestBudgetSustainedStream(t *testing.T) {
	inChild(t, "TestBudgetSustainedStream", []scale.Tier{scale.Large, scale.Large.Contradicting()}, func(t *testing.T, tier scale.Tier) {
		sustainedStream(t, tier, annotationStream)
	})
}

// TestBudgetSpecEditStream is the sustained stream at Large and in the
// measurement world with route spec edits instead of annotations, the latter with
// EndpointSlice churn (churnSlice): process CPU
// ≤ 25 % of one core in every run, edit-driven recomputes ≤ 1/s (D-047) and the
// median memory from the OS ≤ 288 MiB (design §16.3, D-043).
func TestBudgetSpecEditStream(t *testing.T) {
	inChild(t, "TestBudgetSpecEditStream", []scale.Tier{scale.Large, scale.Large.Contradicting().Worst()}, func(t *testing.T, tier scale.Tier) {
		sustainedStream(t, tier, specEditStream)
	})
}

// streamRuns is the child count per world; the OS-memory gate reads their median (D-043).
const streamRuns = 5

// streamOSBudget is the median memory from the OS a stream may take: the worst 3.2 median
// (256.1 MiB, spec-edit in the measurement world) plus 10 %, rounded up to 8 MiB (D-043).
const streamOSBudget = 288 << 20

// streamResult is the line a stream child prints for its parent.
type streamResult struct{ Sys uint64 }

// inChild runs run streamRuns times per tier, each in a fresh process that runs
// only test, logs the children's report lines and gates their median memory from the OS.
func inChild(t *testing.T, test string, tiers []scale.Tier, run func(*testing.T, scale.Tier)) {
	if name := os.Getenv(streamChildEnv); name != "" {
		run(t, tiers[slices.IndexFunc(tiers, func(x scale.Tier) bool { return x.Name == name })])
		return
	}
	const mib = 1 << 20
	for _, tier := range tiers {
		t.Run(tier.Name, func(t *testing.T) {
			var sys []uint64
			for n := range streamRuns {
				args := []string{"-test.run=^" + test + "$", "-test.count=1", "-test.v"}
				if testing.Short() {
					args = append(args, "-test.short")
				}
				cmd := exec.Command(os.Args[0], args...)
				cmd.Env = append(os.Environ(), streamChildEnv+"="+tier.Name)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Errorf("stream child %d: %v\n%s", n+1, err, out)
					return
				}
				for line := range strings.Lines(string(out)) {
					if strings.Contains(line, "budget_test.go") {
						t.Logf("run %d: %s", n+1, strings.TrimSpace(line))
					}
					if j, ok := strings.CutPrefix(line, "stream: "); ok {
						var r streamResult
						if err := json.Unmarshal([]byte(j), &r); err != nil {
							t.Fatal(err)
						}
						sys = append(sys, r.Sys)
					}
				}
			}
			if len(sys) != streamRuns {
				t.Fatalf("%d of %d stream children reported", len(sys), streamRuns)
			}
			slices.Sort(sys)
			med := sys[len(sys)/2]
			t.Logf("%s: memory from the OS over %d runs: median %.1f MiB, min %.1f, max %.1f (budget median ≤ %d MiB)",
				tier.Name, streamRuns, float64(med)/mib, float64(sys[0])/mib, float64(sys[len(sys)-1])/mib, streamOSBudget/mib)
			if med > streamOSBudget {
				benchgate.Timing(t, "median memory from the OS %.1f MiB, budget ≤ %d MiB", float64(med)/mib, streamOSBudget/mib)
			}
		})
	}
}

// streamChildEnv makes a stream test run one world in this process.
const streamChildEnv = "SNELLIUS_STREAM_CHILD"

// streamVariant is what each stream update changes, and whether the budgets gate it.
type streamVariant struct {
	name  string // log prefix
	gated bool
	// burstGated gates the edit-driven runs alone ≤ 1/s; grace deadlines are logged (D-047).
	burstGated bool
	cpu        float64 // process CPU budget per run, a share of one core
	// edit changes u, a copy of a seeded route, for update n in pass over the routes.
	edit func(u *unstructured.Unstructured, n, pass int) error
}

// budget is note when v is gated, else empty.
func (v streamVariant) budget(note string) string {
	if v.gated {
		return note
	}
	return ""
}

// annotationStream changes one annotation per update; the views memo absorbs it.
var annotationStream = streamVariant{gated: true, cpu: 0.15, edit: func(u *unstructured.Unstructured, n, _ int) error {
	u.SetAnnotations(map[string]string{"stream": strconv.Itoa(n)})
	return nil
}}

// specEditStream edits spec and generation, as the API server does; status stays stale.
var specEditStream = streamVariant{name: "spec-edit ", burstGated: true, cpu: 0.25, edit: togglePath}

// togglePath appends "-x" to u's first path value on even passes and removes
// it on odd ones; a pass edits each route once, so generation rises by one.
func togglePath(u *unstructured.Unstructured, _, pass int) error {
	u.SetGeneration(u.GetGeneration() + 1)
	rules, _, _ := unstructured.NestedSlice(u.Object, "spec", "rules")
	for _, r := range rules {
		ms, _ := r.(map[string]any)["matches"].([]any)
		for _, m := range ms {
			p, ok := m.(map[string]any)["path"].(map[string]any)
			v, isStr := p["value"].(string)
			if !ok || !isStr {
				continue
			}
			if pass%2 == 0 {
				p["value"] = v + "-x"
			} else {
				p["value"] = strings.TrimSuffix(v, "-x")
			}
			return unstructured.SetNestedSlice(u.Object, rules, "spec", "rules")
		}
	}
	return fmt.Errorf("HTTPRoute %s/%s has no path match", u.GetNamespace(), u.GetName())
}

// measuredCodes are the Phase 3 codes the measurement world raises once matured.
var measuredCodes = []string{health.CodeXDSRejected, health.CodeProcessRestarting, health.CodeProbeFailing, health.CodeRolloutStuck,
	health.CodeVersionSkew, health.CodeProxyConfigWarming, health.CodeProxyDisconnected}

// analyzerCodes are the 3.3 and 3.4 codes it raises 5 min after maturing; streamCodes
// those a stream waits for, as 018 holds for 5 min first.
var (
	analyzerCodes = append(slices.Clone(streamCodes), health.CodeStatusNotProgressing)
	streamCodes   = []string{health.CodeRefUnresolved, health.CodeRefNotPermitted, health.CodeRuleReplaced, health.CodePolicyTargetNotFound,
		health.CodeListenerConflicted, health.CodeBackendNoReadyEndpoints}
)

// hasCodes reports whether m's install raises every code of codes.
func hasCodes(m *engine.Model, codes []string) bool {
	seen := map[string]bool{}
	for _, f := range m.Installs[0].Health.Findings {
		seen[f.Code] = true
	}
	return !slices.ContainsFunc(codes, func(c string) bool { return !seen[c] })
}

// logsOn reports whether mw's log reader is on and has published m's install's full ring.
func logsOn(m *engine.Model, mw *measure.World) bool {
	v, ok := mw.Store.Snapshot().Get(ctrllogs.Key)
	logs, _ := v.Data.(ctrllogs.Logs)
	l := logs[m.Installs[0].Resolved.ID()]
	return ok && v.State == collect.OK && l.State == ctrllogs.StateOn && len(l.Events) == ctrllogs.MaxEvents
}

// statsInstalls are m's installs and Gateways, as the app feeds the proxy stats collector.
func statsInstalls(m *engine.Model) ([]proxystats.Install, *cache.Snapshot) {
	if m == nil || !m.Snapshot.Synced {
		return nil, nil
	}
	out := make([]proxystats.Install, 0, len(m.Installs))
	for _, im := range m.Installs {
		in := proxystats.Install{Resolved: im.Resolved}
		if im.Owned != nil {
			for _, g := range im.Owned.Gateways {
				in.Gateways = append(in.Gateways, objects.RefOf(g))
			}
		}
		out = append(out, in)
	}
	return out, m.Snapshot
}

// liveHeap is HeapAlloc after a full GC.
func liveHeap() uint64 {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// matured counts m's KGWUI-012 findings and the checks that can raise one: a
// candidate that is no trigger, and no missing child entry whose parent's own
// entry is not current, which waits on that status, not on time (rules §9.3).
func matured(m *engine.Model) (findings, checks int) {
	in := m.Installs[0]
	for _, f := range in.Health.Findings {
		if f.Code == health.CodeComputedDisagrees {
			findings++
		}
	}
	if in.Computed != nil {
		for _, c := range in.Computed.Checks {
			if c.Candidate() && c.Class != computed.ClassTrigger && (c.Class != computed.ClassMissing || c.ParentCurrent) {
				checks++
			}
		}
	}
	return findings, checks
}

func sustainedStream(t *testing.T, tier scale.Tier, v streamVariant) {
	const mib = 1 << 20
	// The binary's soft limit under the chart's 512Mi pod limit: 90 % of it (D-025); GOMEMLIMIT still wins.
	if os.Getenv("GOMEMLIMIT") == "" {
		defer debug.SetMemoryLimit(debug.SetMemoryLimit(512 << 20 / 10 * 9))
	}
	dur := 10 * time.Minute
	if testing.Short() {
		dur = time.Minute
	}
	ev := make(chan engine.LoopEvent, 1<<16)
	cfg := engine.Config{Served: testutil.ServedUpstream(), Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour),
		// Static metrics never go stale here, as a scraped cluster's do not.
		Collected: served(okMetrics("kgw.kgateway"), 0, nil), Checks: health.Upstream(),
		OnLoop: func(e engine.LoopEvent) {
			select {
			case ev <- e:
			default:
				t.Errorf("loop event buffer full")
			}
		}}
	var w *scale.LiveWorld
	var mw *measure.World
	if tier.IsWorst {
		// The 3.2 measurement world on the real clock moved to its fixtures' time, its stats scraped every 10 s.
		clk := measure.ShiftedTo(testutil.Now)
		mw = measure.New(t, tier, clk)
		w, cfg.Collected, cfg.Clock = mw.Live, mw.Store.Snapshot, clk
	} else {
		w = scale.Live(t, tier, nil)
	}
	cfg.Snapshots, cfg.Triggers = w.Cache, w.Signal
	e := engine.New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		e.Run(ctx, 30*time.Second)
	}()
	t.Cleanup(func() {
		cancel()
		testutil.Recv(t, stopped, "Run to return")
	})
	for testutil.Recv(t, ev, "loop event").Kind != engine.EvPublished { // the start-up publication
	}
	if mw != nil {
		mw.SetInstalls(func() ([]proxystats.Install, *cache.Snapshot) { return statsInstalls(e.Current().Model) })
		go mw.Run(ctx)
	}
	if tier.Contradict {
		// Windows mature on the real clock 30 s after start-up, missing child entries and proxy conditions at 60 s.
		for deadline := time.Now().Add(3 * time.Minute); ; time.Sleep(500 * time.Millisecond) {
			// Loop events hold their models; drained here, so waiting keeps no more of them than the UI does.
			testutil.Drain(ev)
			m := e.Current().Model
			if f, c := matured(m); f == c && c > 0 && (mw == nil || hasCodes(m, measuredCodes) && hasCodes(m, streamCodes) && logsOn(m, mw)) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the worst case did not mature")
			}
		}
		testutil.Drain(ev)
	}
	// Each update edits the fake's current copy, so the seeded routes are not kept alive.
	routes := make([]types.NamespacedName, len(w.Routes))
	for i, r := range w.Routes {
		routes[i] = types.NamespacedName{Namespace: r.GetNamespace(), Name: r.GetName()}
	}
	w.Routes = nil
	h := NewHandler(e, fstest.MapFS{}, nil)
	path := "/api/v1/installs/" + e.Current().Model.Installs[0].Resolved.ID() + "/routes"

	// Count runs and their wall time from loop events while the stream runs.
	var runs, pubs, deadlineRuns, safetyRuns int
	var busy time.Duration
	var runStart time.Time
	// A run whose start finds the last published deadline due ran for it; one 30 s after the last publication, for the safety timer.
	lastNext, lastPub := e.Current().Model.Next, time.Now()
	counted := make(chan struct{})
	collect, stopCollect := context.WithCancel(context.Background())
	go func() {
		defer close(counted)
		for {
			select {
			case x := <-ev:
				switch x.Kind {
				case engine.EvRunStarted:
					runs++
					runStart = x.At
					switch {
					case lastNext.Due(x.At):
						deadlineRuns++
					case x.At.Sub(lastPub) >= 30*time.Second:
						safetyRuns++
					}
				case engine.EvPublished:
					pubs++
					if !runStart.IsZero() { // a run started before counting began has no start here
						busy += x.At.Sub(runStart)
					}
					lastNext, lastPub = x.Published.Model.Next, x.At
				}
			case <-collect.Done():
				return
			}
		}
	}()

	cpu0 := cpuTime(t)
	gc0, tot0 := gcCPU()
	start := time.Now()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var polls []time.Duration
	var pollMu sync.Mutex
	for c := range 20 {
		wg.Go(func() {
			// Staggered so the clients do not poll in lockstep.
			select {
			case <-time.After(time.Duration(c) * 250 * time.Millisecond):
			case <-stop:
				return
			}
			tick := time.NewTicker(5 * time.Second)
			defer tick.Stop()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			for {
				rec := httptest.NewRecorder()
				t0 := time.Now()
				h.ServeHTTP(rec, req)
				d := time.Since(t0)
				if rec.Code != http.StatusOK {
					t.Errorf("poll: %d %s", rec.Code, rec.Body)
				}
				pollMu.Lock()
				polls = append(polls, d)
				pollMu.Unlock()
				select {
				case <-tick.C:
				case <-stop:
					return
				}
			}
		})
	}
	updates, sliceUpdates := 0, 0
	// The live heap after a full GC, at the first minute mark and at the end: growth would be a leak.
	var heap60 uint64
	// Routes change every 100 ms; a world with EndpointSlices churns one 50 ms after each (D-047).
	tick := time.NewTicker(50 * time.Millisecond)
	for deadline, half := start.Add(dur), false; time.Now().Before(deadline); <-tick.C {
		if heap60 == 0 && dur > time.Minute && time.Since(start) >= time.Minute {
			heap60 = liveHeap()
		}
		if half = !half; !half {
			if len(w.Slices) > 0 {
				sliceUpdates++
				churnSlice(t, w, sliceUpdates)
			}
			continue
		}
		ref := routes[updates%len(routes)]
		u, err := w.GetRoute(context.Background(), ref.Namespace, ref.Name)
		if err != nil {
			t.Fatal(err)
		}
		updates++
		u.SetResourceVersion(strconv.Itoa(1_000_000 + updates))
		if err := v.edit(u, updates, (updates-1)/len(routes)); err != nil {
			t.Fatal(err)
		}
		if err := w.Update(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}
	tick.Stop()
	close(stop)
	wg.Wait()
	wall := time.Since(start)
	cpu := cpuTime(t) - cpu0
	gc1, tot1 := gcCPU()
	stopCollect()
	<-counted

	if len(polls) == 0 {
		t.Fatal("no poll completed")
	}
	slices.Sort(polls)
	rate := float64(runs) / wall.Seconds()
	bursts := runs - deadlineRuns - safetyRuns
	burstRate := float64(bursts) / wall.Seconds()
	share := cpu.Seconds() / wall.Seconds()
	t.Logf("%sstream %v: %d updates and %d EndpointSlice updates, %d recomputes (%.2f/s%s), %d publications; process CPU %v = %.1f %% of one core%s; recompute wall %v = %.1f %%; GC share %.1f %% of used CPU (%.1f %% of one core)",
		v.name, wall.Round(time.Millisecond), updates, sliceUpdates, runs, rate, v.budget(", budget ≤ 1/s"), pubs, cpu.Round(time.Millisecond), 100*share, fmt.Sprintf(" (budget ≤ %.0f %%)", 100*v.cpu), busy.Round(time.Millisecond), 100*busy.Seconds()/wall.Seconds(), 100*(gc1-gc0)/(tot1-tot0), 100*(gc1-gc0)/wall.Seconds())
	if v.burstGated {
		t.Logf("%srecomputes by cause: %d edit-driven (%.2f/s, budget ≤ 1/s), %d deadline (%.2f/s), %d safety; total %.2f/s (deadlines and total logged)",
			v.name, bursts, burstRate, deadlineRuns, float64(deadlineRuns)/wall.Seconds(), safetyRuns, rate)
	}
	t.Logf("polls: %d, p50 %v p95 %v max %v", len(polls), rd(percentile(polls, 50)), rd(percentile(polls, 95)), rd(polls[len(polls)-1]))
	heapEnd := liveHeap()
	if heap60 == 0 {
		heap60 = heapEnd // the -short run ends at the minute mark
	}
	t.Logf("%s: heap after GC %.1f MiB at 1 min, %.1f MiB at the end (%+.1f MiB)",
		tier.Name, float64(heap60)/mib, float64(heapEnd)/mib, (float64(heapEnd)-float64(heap60))/mib)
	f, c := matured(e.Current().Model)
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	t.Logf("%s: %d KGWUI-012 of %d checks at the end; memory from the OS %.1f MiB, heap in use %.1f MiB",
		tier.Name, f, c, float64(ms.Sys)/mib, float64(ms.HeapInuse)/mib)
	j, _ := json.Marshal(streamResult{Sys: ms.Sys})
	fmt.Println("stream: " + string(j))
	if share > v.cpu {
		benchgate.Timing(t, "process CPU %.1f %% of one core, budget ≤ %.0f %%", 100*share, 100*v.cpu)
	}
	if v.burstGated && burstRate > 1 {
		t.Errorf("%.2f edit-driven recomputes per s, budget ≤ 1", burstRate)
	}
	if !v.gated {
		codes := map[string]int{}
		for _, x := range e.Current().Model.Installs[0].Health.Findings {
			codes[x.Code]++
		}
		t.Logf("%s: findings by code at the end %v", tier.Name, codes)
		return
	}
	if tier.Contradict && f != c {
		t.Errorf("worst case: %d KGWUI-012 of %d checks at the end", f, c)
	}
	if !tier.Contradict && (f != 0 || c != 0) {
		t.Errorf("consistent world: %d KGWUI-012 of %d checks at the end", f, c)
	}
	if rate > 1 {
		t.Errorf("%.2f recomputes per s, budget ≤ 1", rate)
	}
}

// churnSlice is EndpointSlice update n of w's: on its first pass over the slices
// it adds a not-ready endpoint, on the next removes it, so no Service's readiness changes.
func churnSlice(t *testing.T, w *scale.LiveWorld, n int) {
	ref := w.Slices[(n-1)%len(w.Slices)]
	ctx := context.Background()
	c := w.Kube.DiscoveryV1().EndpointSlices(ref.Namespace)
	es, err := c.Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ((n-1)/len(w.Slices))%2 == 0 {
		no := false
		es.Endpoints = append(es.Endpoints, discoveryv1.Endpoint{Addresses: []string{"10.4.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &no}})
	} else {
		es.Endpoints = es.Endpoints[:len(es.Endpoints)-1]
	}
	es.ResourceVersion = strconv.Itoa(2_000_000 + n)
	if _, err := c.Update(ctx, es, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	w.Kube.ClearActions()
}

// chainShape is an inherit chain world: edge → shop/api, then depth-1
// inheriting chain routes, the last one delegating to leaves leaves. Each
// match adds keys headers and keys query parameters with values of size
// bytes; the root has fan path matches, so each leaf row gets fan contexts.
type chainShape struct{ depth, leaves, keys, size, fan int }

// inheritChain builds s; {100, 200, 16, 1, 1} is the re-review's construction.
func inheritChain(s chainShape) []kruntime.Object {
	v := strings.Repeat("v", s.size)
	match := func(level int, path string) map[string]any {
		var hs, qs []any
		for k := range s.keys {
			hs = append(hs, map[string]any{"type": "Exact", "name": fmt.Sprintf("x-l%03d-h%02d", level, k), "value": v})
			qs = append(qs, map[string]any{"type": "Exact", "name": fmt.Sprintf("l%03d-q%02d", level, k), "value": v})
		}
		m := matchPath(path)
		if s.keys > 0 {
			m["headers"], m["queryParams"] = hs, qs
		}
		return m
	}
	name := func(i int) string { return fmt.Sprintf("c%03d", i) }
	team := map[string]string{"team": "a"}
	var ms []map[string]any
	for k := range s.fan {
		ms = append(ms, match(0, fmt.Sprintf("/a%02d", k)))
	}
	root := testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		testutil.Rule(ms...))
	root.Object["spec"].(map[string]any)["rules"].([]any)[0].(map[string]any)["backendRefs"] = []any{testutil.DelegateTo("chain", name(1))}
	objs := []kruntime.Object{testutil.Namespace("chain", team), testutil.Namespace("leaves", team), root}
	for i := 1; i < s.depth; i++ {
		to := testutil.DelegateTo("chain", name(i+1))
		if i+1 == s.depth {
			to = testutil.DelegateWildcard("leaves")
		}
		r := testutil.Rule(match(i, "/"))
		r["backendRefs"] = []any{to}
		parent := testutil.HTTPRouteRef("chain", name(i-1))
		if i == 1 {
			parent = shopAPI
		}
		u := testutil.WithRules(testutil.HTTPRoute("chain", name(i), nil, testutil.AcceptedBy(parent, testutil.Controller)), r)
		u.SetAnnotations(map[string]string{objects.InheritParentMatcherAnnotation: "true"})
		objs = append(objs, u)
	}
	last := testutil.HTTPRouteRef("chain", name(s.depth-1))
	for i := range s.leaves {
		objs = append(objs, leaf("leaves", fmt.Sprintf("l%03d", i), nil, "/", testutil.AcceptedBy(last, testutil.Controller)))
	}
	return isolated(objs...)
}

// countingWriter is a ResponseWriter that only counts the body, as a socket would take it.
type countingWriter struct {
	h    http.Header
	n    int
	code int
}

func (w *countingWriter) Header() http.Header         { return w.h }
func (w *countingWriter) WriteHeader(code int)        { w.code = code }
func (w *countingWriter) Write(b []byte) (int, error) { w.n += len(b); return len(b), nil }

// pageCost is the bytes one delegation page of the chain's last route
// allocates and sends.
func pageCost(t *testing.T, s chainShape) (alloc, body uint64) {
	e, srv, _ := startWith(t, setup{disputes: true}, inheritChain(s)...)
	url := delegURL("chain", fmt.Sprintf("c%03d", s.depth-1), "?limit=200")
	p := get[Page[DelegationRow]](t, srv, url, 200)
	steps, keys, truncated := 0, 0, 0
	for _, r := range p.Items {
		for _, c := range r.Contexts {
			steps += len(c.Path)
			for _, s := range c.Path {
				keys += len(s.Match.Headers) + len(s.Match.QueryParams)
			}
		}
		if r.ContextsTruncated {
			truncated++
		}
	}
	t.Logf("%+v: %d rows, %d path steps, %d key matches, %d rows truncated", s, len(p.Items), steps, keys, truncated)
	// Two GCs empty the encoder's buffer pool, so the request grows its own.
	h := NewHandler(e, fstest.MapFS{}, nil)
	w := &countingWriter{h: http.Header{}}
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
	runtime.ReadMemStats(&after)
	if w.code != 0 && w.code != http.StatusOK {
		t.Fatalf("delegation page: %d", w.code)
	}
	return after.TotalAlloc - before.TotalAlloc, uint64(w.n)
}

// TestBudgetMatcherMaterialization: inherited matches grow with depth, so a
// page is bounded by the units it materializes, not by path steps alone. The
// worst shape measured 24.7 MiB, mostly the encoder's buffer (about 6× the body).
func TestBudgetMatcherMaterialization(t *testing.T) {
	const mib = 1 << 20
	for _, s := range []chainShape{{100, 200, 16, 1, 1}, {25, 200, 16, 1, 1}, {10, 200, 16, 1, 1}, {25, 200, 16, 200, 1},
		{100, 200, 16, 4096, 1}, {26, 200, 0, 0, 20}} {
		alloc, body := pageCost(t, s)
		t.Logf("%+v: one page allocates %.1f MiB, body %.1f MiB", s, float64(alloc)/mib, float64(body)/mib)
		if alloc > 32*mib {
			t.Errorf("a page of %+v allocates %.1f MiB, want at most 32", s, float64(alloc)/mib)
		}
	}
}

// The path list's request and heap budgets of design §14 (6.2).

// pathsTiers are the worlds the paths budgets run on: the most path rows, and
// the measurement world, whose Services make the endpoints projection work.
var pathsTiers = []scale.Tier{scale.Large.Paths(), scale.Large.Contradicting().Worst()}

// pathsEngine is tier's engine and model, and the measurement world it keeps
// using (nil outside it); that world matures as measuredHandler's.
func pathsEngine(t *testing.T, tier scale.Tier) (*engine.Engine, *engine.Model, *measure.World) {
	if !tier.IsWorst {
		e, m := snapshotEngine(scale.Snapshot(tier, 1), tier)
		return e, m, nil
	}
	fc := testutil.FakeClock()
	w, snap := stoppedWorld(t, tier, fc)
	e, m := measuredEngine(t, w, fc, snap)
	return e, m, w
}

// stoppedWorld is tier's measurement world and its cache's snapshot, once the
// cache and every goroutine it started have stopped. No GC runs while it builds:
// when the informers' collections run decides which spans the snapshot shares
// with garbage, so HeapInuse would vary by up to 9 MB per process.
func stoppedWorld(t *testing.T, tier scale.Tier, fc *clocktesting.FakeClock) (*measure.World, *cache.Snapshot) {
	gc := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(gc)
	g := runtime.NumGoroutine()
	w := measure.New(t, tier, fc)
	snap := w.Live.Cache.Snapshot()
	w.Live.Stop()
	w.Live = nil
	for deadline := time.Now().Add(time.Minute); runtime.NumGoroutine() > g; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines after the world stopped, %d before it", runtime.NumGoroutine(), g)
		}
	}
	return w, snap
}

// pathsHandler is a handler over e under authz with an empty path memo whose
// projections expire by m's clock, as the app's do by the real one.
func pathsHandler(e *engine.Engine, m *engine.Model, authz Authorizer) http.Handler {
	return NewHandler(e, fstest.MapFS{}, authz, withPathMemo(paths.NewMemo(clocktesting.NewFakeClock(m.ComputedAt))))
}

// allocated is the process's total heap allocation so far, in bytes.
func allocated() uint64 {
	s := []metrics.Sample{{Name: "/gc/heap/allocs:bytes"}}
	metrics.Read(s)
	return s[0].Value.Uint64()
}

// pathsGet serves one GET of path through h and returns its duration, bytes
// allocated and decoded body. Any status but 200 fails tb.
func pathsGet(tb testing.TB, h http.Handler, path string) (time.Duration, uint64, PathsResponse) {
	tb.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	a, start := allocated(), time.Now()
	h.ServeHTTP(w, req)
	d, n := time.Since(start), allocated()-a
	if w.Code != http.StatusOK {
		tb.Fatalf("GET %s: %d %s", path, w.Code, w.Body)
	}
	var out PathsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		tb.Fatal(err)
	}
	return d, n, out
}

// The paths request gates (design §14, tuned in 6.2 from 31 ms, 28.4 MiB cold
// and 1.2 ms, 1.7 MiB warm). Times gate on the reference machine; allocation everywhere.
const (
	pathsColdTime  = 100 * time.Millisecond
	pathsColdAlloc = 32 << 20
	pathsWarmTime  = 5 * time.Millisecond
	pathsWarmAlloc = 2 << 20
)

// TestBudgetPathsRequests: at Large, the first page of …/paths cold (a fresh
// path memo) and warm (memo hit), under AllowAll and a restricting Authorizer:
// median time and bytes allocated per request within the §14 gates.
func TestBudgetPathsRequests(t *testing.T) {
	const (
		mib      = 1 << 20
		coldRuns = 11
		warmRuns = 201
	)
	for _, tier := range pathsTiers {
		t.Run(tier.Name, func(t *testing.T) {
			e, m, w := pathsEngine(t, tier)
			defer runtime.KeepAlive(w)
			url := "/api/v1/installs/" + m.Installs[0].Resolved.ID() + "/paths"
			for _, a := range []struct {
				name  string
				authz Authorizer
			}{{"allow-all", nil}, {"restricted", restricting}} {
				var coldT, warmT []time.Duration
				var coldB, warmB []uint64
				var sum PathSummaryDTO
				for range coldRuns {
					d, n, out := pathsGet(t, pathsHandler(e, m, a.authz), url)
					coldT, coldB, sum = append(coldT, d), append(coldB, n), out.Summary
				}
				h := pathsHandler(e, m, a.authz)
				pathsGet(t, h, url)
				for range warmRuns {
					d, n, _ := pathsGet(t, h, url)
					warmT, warmB = append(warmT, d), append(warmB, n)
				}
				slices.Sort(coldT)
				slices.Sort(warmT)
				slices.Sort(coldB)
				slices.Sort(warmB)
				ct, cb := coldT[len(coldT)/2], coldB[len(coldB)/2]
				wt, wb := warmT[len(warmT)/2], warmB[len(warmB)/2]
				t.Logf("%s %s: %d paths (hidden %v, unmemoised %v); cold median %v max %v, %.2f MiB allocated (budget < %v, < %d MiB); warm median %v p95 %v, %.3f MiB allocated (budget < %v, < %d MiB)",
					tier.Name, a.name, sum.Paths, sum.Hidden, sum.Unmemoised, rd(ct), rd(coldT[len(coldT)-1]), float64(cb)/mib, pathsColdTime, pathsColdAlloc/mib,
					rd(wt), rd(percentile(warmT, 95)), float64(wb)/mib, pathsWarmTime, pathsWarmAlloc/mib)
				if sum.Unmemoised {
					t.Errorf("%s %s: the topology is over the memo cap, so warm requests derive it", tier.Name, a.name)
				}
				if ct >= pathsColdTime {
					benchgate.Timing(t, "%s %s: cold median %v, budget < %v", tier.Name, a.name, ct, pathsColdTime)
				}
				if wt >= pathsWarmTime {
					benchgate.Timing(t, "%s %s: warm median %v, budget < %v", tier.Name, a.name, wt, pathsWarmTime)
				}
				if cb >= pathsColdAlloc {
					t.Errorf("%s %s: cold allocates %d B, budget < %d B", tier.Name, a.name, cb, pathsColdAlloc)
				}
				if wb >= pathsWarmAlloc {
					t.Errorf("%s %s: warm allocates %d B, budget < %d B", tier.Name, a.name, wb, pathsWarmAlloc)
				}
			}
		})
	}
}

// heapBaseline is the median "retained" HeapInuse of the 2.1 engine at Large
// (docs/phases/phase-2/bench/2.2-baseline-heap.txt), as pkg/engine's TestBudgetHeap holds it.
const heapBaseline = 25411584

// apiHeapChildEnv makes TestBudgetHeapAPIWarm measure in this process and print one line.
const apiHeapChildEnv = "SNELLIUS_API_HEAP_CHILD"

// apiHeapRun is one child's measurement: HeapInuse once the engine has computed
// (the engine figure) and once paths are warm for every install, and HeapAlloc
// freed by dropping the path memos.
type apiHeapRun struct {
	Engine, Warm uint64
	// Alloc is the HeapAlloc warming added; Freed what dropping the handler and its memo freed.
	Alloc, Freed int64
	Paths        int
	Unmemoised   bool
}

// TestBudgetHeapAPIWarm: as TestBudgetHeap, one fresh process per run and five
// runs, the median heap retained once paths are warm for every install stays
// ≤ 48 MiB over the 2.1 baseline (D-057). Extreme is reported: D-057 is a Large budget.
func TestBudgetHeapAPIWarm(t *testing.T) {
	cases := append(slices.Clone(pathsTiers), scale.Extreme)
	if name := os.Getenv(apiHeapChildEnv); name != "" {
		i := slices.IndexFunc(cases, func(x scale.Tier) bool { return x.Name == name })
		apiHeapChild(t, cases[i])
		return
	}
	for _, tier := range cases {
		t.Run(tier.Name, func(t *testing.T) { apiHeapBudget(t, tier, tier.Name != scale.Extreme.Name) })
	}
}

// apiHeapBudget runs five heap children for tier and checks their medians if gated.
func apiHeapBudget(t *testing.T, tier scale.Tier, gated bool) {
	const mib = 1 << 20
	var runs []apiHeapRun
	for range 5 {
		cmd := exec.Command(os.Args[0], "-test.run=^TestBudgetHeapAPIWarm$", "-test.count=1", "-test.timeout=0")
		cmd.Env = append(os.Environ(), apiHeapChildEnv+"="+tier.Name)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("heap child: %v\n%s", err, out)
		}
		var r apiHeapRun
		for line := range strings.Lines(string(out)) {
			if j, ok := strings.CutPrefix(line, "apiheap: "); ok {
				if err := json.Unmarshal([]byte(j), &r); err != nil {
					t.Fatal(err)
				}
			}
		}
		if r.Engine == 0 {
			t.Fatalf("heap child printed no result:\n%s", out)
		}
		runs = append(runs, r)
	}
	t.Logf("run  engine (B)  warm (B)  API memos (B)  HeapAlloc added (B)  freed by dropping them (B)")
	eng, api, freed, warm := make([]int64, len(runs)), make([]int64, len(runs)), make([]int64, len(runs)), make([]int64, len(runs))
	for i, r := range runs {
		eng[i], api[i], freed[i] = int64(r.Engine)-heapBaseline, int64(r.Warm)-int64(r.Engine), r.Freed //nolint:gosec // heap sizes fit
		warm[i] = int64(r.Warm) - heapBaseline                                                          //nolint:gosec // heap sizes fit
		t.Logf("%d    %d    %d    %d    %d    %d", i+1, r.Engine, r.Warm, api[i], r.Alloc, r.Freed)
	}
	med := func(v []int64) int64 { v = slices.Clone(v); slices.Sort(v); return v[len(v)/2] }
	// The gate is one child's whole figure; the engine and API medians are its breakdown.
	em, am, fm, total := med(eng), med(api), med(freed), med(warm)
	note := "budget ≤ 48 MiB"
	if !gated {
		note = "reported: D-057 is a Large budget"
	}
	t.Logf("%s: %d paths (unmemoised %v); warm retained median %+.1f MiB over the 2.1 baseline %d B (%s); breakdown: engine figure median %+.1f MiB, API memos median %+.2f MiB (freed by dropping them %.2f MiB)",
		tier.Name, runs[0].Paths, runs[0].Unmemoised, float64(total)/mib, heapBaseline, note, float64(em)/mib, float64(am)/mib, float64(fm)/mib)
	if gated && total > 48*mib {
		t.Errorf("%s: warm retained heap %.1f MiB above the baseline, budget ≤ 48 MiB", tier.Name, float64(total)/mib)
	}
}

// apiHeapChild measures one apiHeapRun of tier and prints it.
func apiHeapChild(t *testing.T, tier scale.Tier) {
	stats := func() runtime.MemStats {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m
	}
	e, m, w := pathsEngine(t, tier)
	var r apiHeapRun
	st := stats()
	r.Engine = st.HeapInuse
	h := pathsHandler(e, m, nil)
	for _, i := range m.Installs {
		_, _, out := pathsGet(t, h, "/api/v1/installs/"+i.Resolved.ID()+"/paths")
		r.Paths += out.Summary.Paths
		r.Unmemoised = r.Unmemoised || out.Summary.Unmemoised
	}
	warm := stats()
	r.Warm, r.Alloc = warm.HeapInuse, int64(warm.HeapAlloc)-int64(st.HeapAlloc) //nolint:gosec // heap sizes fit
	runtime.KeepAlive(h)                                                        // the handler and its memo go here
	r.Freed = int64(warm.HeapAlloc) - int64(stats().HeapAlloc)                  //nolint:gosec // heap sizes fit
	runtime.KeepAlive(e)
	runtime.KeepAlive(m)
	runtime.KeepAlive(w)
	j, _ := json.Marshal(r)
	fmt.Println("apiheap: " + string(j))
}
