package proxymode

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
)

// snapshot holds objs under their served GVRs, with every served kind readable.
func snapshot(served []kinds.Served, objs ...*unstructured.Unstructured) *cache.Snapshot {
	s := &cache.Snapshot{Objects: map[schema.GroupVersionResource][]*unstructured.Unstructured{}, Access: map[schema.GroupVersionResource]cache.Access{}}
	for _, k := range served {
		s.Access[k.GVR()] = cache.Access{OK: true}
		for _, o := range objs {
			if o.GetKind() == k.Kind && o.GroupVersionKind().Group == k.Group {
				s.Objects[k.GVR()] = append(s.Objects[k.GVR()], o)
			}
		}
	}
	return s
}

// The unresolved rows the API test cannot reach: an owned Gateway always has its class.
func TestResolveUnresolved(t *testing.T) {
	served := testutil.ServedUpstream()
	sm := testutil.GatewayParameters("infra", "sm", true)
	gw := testutil.Gateway("infra", "edge", "kgateway", nil)
	unnamed := testutil.GatewayClass("kgateway", testutil.Controller, true)
	testutil.SetClassParameters(unnamed, kinds.KgatewayGroup, "GatewayParameters", "infra", "")
	named := testutil.GatewayClass("kgateway", testutil.Controller, true)
	testutil.SetClassParameters(named, kinds.KgatewayGroup, "GatewayParameters", "infra", "sm")

	cases := []struct {
		name string
		snap *cache.Snapshot
		want Mode
	}{
		{"GatewayClass missing", snapshot(served, gw, sm), Unresolved},
		{"class reference without a name", snapshot(served, gw, unnamed, sm), Unresolved},
		{"class selfManaged", snapshot(served, gw, named, sm), SelfManaged},
		{"no references", snapshot(served, gw, testutil.GatewayClass("kgateway", testutil.Controller, true)), Managed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (GatewayParameters{}).Resolve(gw, tc.snap, served); got != tc.want {
				t.Fatalf("want %s, got %s", tc.want, got)
			}
		})
	}
}
