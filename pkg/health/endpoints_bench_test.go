package health

import (
	"fmt"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// fanInWorld is the endpoints world with n more routes on Service shop/cart port 8080.
func fanInWorld(n int, ready bool) *endpointsWorld {
	gw := testutil.GwRef("infra", "edge")
	e := newEndpointsWorld()
	for i := range n {
		e.w.objs = append(e.w.objs, withRefs(testutil.HTTPRoute("shop", fmt.Sprintf("r%05d", i), []objects.ParentRef{gw},
			testutil.AcceptedBy(gw, testutil.Controller)), backendRef("cart", 8080, nil)))
	}
	if ready {
		e.slices[0].Ready = []int{1}
	}
	return e
}

// One route naming the same Service port twice, with another port between,
// is listed once in that port's evidence (phase review #6).
func TestBackendEndpointsRouteListedOnce(t *testing.T) {
	e := newEndpointsWorld()
	e.svcs = append(e.svcs, e.svcs[0])
	e.svcs[1].Name = "web"
	e.slices = append(e.slices, e.slices[0])
	e.slices[1].Service = "web"
	e.w.objs[2] = withRefs(e.w.objs[2], backendRef("cart", 8080, nil), backendRef("web", 8080, nil), backendRef("cart", 8080, nil))
	fs := endpointFindings(e.run(t, NewStates(), 0, 31*time.Second))
	if len(fs) != 2 {
		t.Fatalf("findings %+v", fs)
	}
	for _, f := range fs {
		if rs := f.Facts.(*EndpointsEvidence).Routes; len(rs) != 1 {
			t.Fatalf("%s: routes %+v, want the one route once", f.Resource, rs)
		}
	}
}

// BenchmarkEndpointsFanIn runs the endpoints check over many routes on one
// Service port, empty and ready; the cost per route must not grow with n.
func BenchmarkEndpointsFanIn(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		for _, ready := range []bool{false, true} {
			b.Run(fmt.Sprintf("routes=%d/ready=%v", n, ready), func(b *testing.B) {
				in := fanInWorld(n, ready).input(&testing.T{}, testutil.Now)
				c := endpointsCheck{}
				b.ResetTimer()
				for range b.N {
					c.Run(RunContext{Input: in, State: NewStore()})
				}
			})
		}
	}
}
