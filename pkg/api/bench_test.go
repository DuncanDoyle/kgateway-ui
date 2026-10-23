//go:build bench

package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

type staticSnap struct{ s *cache.Snapshot }

func (s staticSnap) Snapshot() *cache.Snapshot { return s.s }

// largeHandler is the handler over a Large-tier model under AllowAll. The
// snapshot is sorted as the cache sorts it.
func largeHandler(b *testing.B) (http.Handler, *engine.Model) { return largeHandlerWith(b, nil) }

// restricting denies every object in a namespace whose name ends in 1,
// about a tenth of them, so every request pays the per-ref checks.
var restricting = AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return !strings.HasSuffix(ref.Namespace, "1") })

// largeHandlerWith is largeHandler under authz.
func largeHandlerWith(b *testing.B, authz Authorizer) (http.Handler, *engine.Model) {
	b.Helper()
	snap := scale.Snapshot(scale.Large, 1)
	for _, objs := range snap.Objects {
		slices.SortFunc(objs, func(x, y *unstructured.Unstructured) int {
			return strings.Compare(x.GetNamespace()+"/"+x.GetName(), y.GetNamespace()+"/"+y.GetName())
		})
	}
	e := engine.New(engine.Config{Snapshots: staticSnap{snap}, Served: testutil.ServedUpstream(), Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour), Metrics: okMetrics("kgw.kgateway"),
		Checks: health.Upstream(), MetricsMaxAge: 30 * time.Second, Now: func() time.Time { return testutil.Now }})
	return NewHandler(e, fstest.MapFS{}, authz), e.Compute()
}

// serve benchmarks one GET per case.
func serve(b *testing.B, h http.Handler, cases []struct{ name, path string }) {
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			b.ReportAllocs()
			for b.Loop() {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					b.Fatalf("GET %s: %d %s", tc.path, w.Code, w.Body)
				}
			}
		})
	}
}

// BenchmarkListsLarge measures one list request, query and DTOs included,
// at the Large tier under AllowAll.
func BenchmarkListsLarge(b *testing.B) { benchLists(b, nil) }

// BenchmarkListsLargeRestricted is BenchmarkListsLarge under a restricting Authorizer.
func BenchmarkListsLargeRestricted(b *testing.B) { benchLists(b, restricting) }

func benchLists(b *testing.B, authz Authorizer) {
	h, m := largeHandlerWith(b, authz)
	ix := m.Installs[0].Index
	gw := ix.Gateways[0].Ref
	base := "/api/v1/installs/" + m.Installs[0].Resolved.ID()
	serve(b, h, []struct{ name, path string }{
		{"routes", base + "/routes"},
		{"routes-health-desc", base + "/routes?sort=health.desc&limit=200"},
		{"routes-by-gateway", base + "/routes?parent=gateway.networking.k8s.io/Gateway/" + gw.Namespace + "/" + gw.Name},
		{"routes-q", base + "/routes?q=ns-01"},
		{"gateways-by-routes", base + "/gateways?sort=routes.desc"},
		{"listenersets", base + "/listenersets"},
	})
}

// BenchmarkDetailsLarge measures detail requests at the Large tier; the
// route is the deepest delegated child, so its up-chain walk is the longest.
func BenchmarkDetailsLarge(b *testing.B) { benchDetails(b, nil) }

// BenchmarkDetailsLargeRestricted is BenchmarkDetailsLarge under a restricting Authorizer.
func BenchmarkDetailsLargeRestricted(b *testing.B) { benchDetails(b, restricting) }

func benchDetails(b *testing.B, authz Authorizer) {
	h, m := largeHandlerWith(b, authz)
	ix := m.Installs[0].Index
	depth := map[objects.Ref]int{}
	var depthOf func(objects.Ref) int
	depthOf = func(r objects.Ref) int {
		if d, ok := depth[r]; ok {
			return d
		}
		depth[r] = 0 // cycle guard
		d := 0
		for _, p := range ix.ParentRoutes[r] {
			d = max(d, depthOf(p)+1)
		}
		depth[r] = d
		return d
	}
	// Under authz only an object the caller may view opens.
	can := func(r objects.Ref) bool { return authz == nil || authz.Authorize(nil, r) }
	var deepest objects.Ref
	for _, r := range ix.Routes {
		if can(r.Ref) && (deepest.Name == "" || depthOf(r.Ref) > depthOf(deepest)) {
			deepest = r.Ref
		}
	}
	if depthOf(deepest) == 0 {
		b.Fatal("the Large tier has no delegated child")
	}
	b.Logf("deepest delegated child %s at depth %d", deepest, depthOf(deepest))
	gw := ix.Gateways[slices.IndexFunc(ix.Gateways, func(g explore.GatewayRow) bool { return can(g.Ref) })].Ref
	base := "/api/v1/installs/" + m.Installs[0].Resolved.ID()
	serve(b, h, []struct{ name, path string }{
		{"gateway", base + "/gateways/" + gw.Namespace + "/" + gw.Name},
		{"route-deepest-child", base + "/routes/gateway.networking.k8s.io/HTTPRoute/" + deepest.Namespace + "/" + deepest.Name},
	})
}
