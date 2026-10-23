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
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

type staticSnap struct{ s *cache.Snapshot }

func (s staticSnap) Snapshot() *cache.Snapshot { return s.s }

// BenchmarkListsLarge measures one list request, query and DTOs included,
// at the Large tier under AllowAll. The snapshot is sorted as the cache sorts it.
func BenchmarkListsLarge(b *testing.B) {
	snap := scale.Snapshot(scale.Large, 1)
	for _, objs := range snap.Objects {
		slices.SortFunc(objs, func(x, y *unstructured.Unstructured) int {
			return strings.Compare(x.GetNamespace()+"/"+x.GetName(), y.GetNamespace()+"/"+y.GetName())
		})
	}
	e := engine.New(engine.Config{Snapshots: staticSnap{snap}, Served: testutil.ServedUpstream(), Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour), Metrics: okMetrics("kgw.kgateway"),
		Checks: health.Upstream(), MetricsMaxAge: 30 * time.Second, Now: func() time.Time { return testutil.Now }})
	m := e.Compute()
	id := m.Installs[0].Resolved.ID()
	ix := m.Installs[0].Index
	gw := ix.Gateways[0].Ref
	h := NewHandler(e, fstest.MapFS{}, nil)
	base := "/api/v1/installs/" + id
	for _, tc := range []struct{ name, path string }{
		{"routes", base + "/routes"},
		{"routes-health-desc", base + "/routes?sort=health.desc&limit=200"},
		{"routes-by-gateway", base + "/routes?parent=gateway.networking.k8s.io/Gateway/" + gw.Namespace + "/" + gw.Name},
		{"routes-q", base + "/routes?q=ns-01"},
		{"gateways-by-routes", base + "/gateways?sort=routes.desc"},
		{"listenersets", base + "/listenersets"},
	} {
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
