package proxystats_test

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

// memberSource serves one body per target name and records each filtered fetch.
type memberSource struct {
	mu      sync.Mutex
	targets map[objects.Ref][]proxystats.Target
	bodies  map[string]string
	fetches []string // "<pod> <filter>"
	plain   bool     // without FetchFiltered
}

func (m *memberSource) Targets(_ install.Resolved, gw objects.Ref, _ *cache.Snapshot) ([]proxystats.Target, bool) {
	ts, ok := m.targets[gw]
	return ts, ok
}

func (m *memberSource) Fetch(context.Context, proxystats.Target) ([]byte, error) {
	return nil, errors.New("unused")
}

func (m *memberSource) Interpret(proxystats.Target, []byte) (proxystats.PodStats, error) {
	return proxystats.PodStats{}, nil
}

// filtered is memberSource with FetchFiltered.
type filtered struct{ *memberSource }

func (f filtered) FetchFiltered(_ context.Context, t proxystats.Target, filter string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetches = append(f.fetches, t.Name+" "+filter)
	b, ok := f.bodies[t.Name]
	if !ok {
		return nil, errors.New("connection refused")
	}
	return []byte(b), nil
}

func newMemberSource() *memberSource {
	edge, east := testutil.GwRef("infra", "edge").Ref(), testutil.GwRef("infra", "east").Ref()
	return &memberSource{
		targets: map[objects.Ref][]proxystats.Target{
			edge: {{ID: "u0", Name: "edge-0", Gateway: edge}, {ID: "u1", Name: "edge-1", Gateway: edge}},
			east: {{ID: "u2", Name: "east-0", Gateway: east}},
		},
		bodies: map[string]string{
			"edge-1": "cluster.kube_shop_cart_8080.membership_healthy: 0\ncluster.kube_shop_cart_8080.membership_total: 2\n" +
				"cluster.kube_shop_other_80.membership_total: 9\n",
		},
	}
}

func (m *memberSource) collector() *proxystats.Collector {
	var src proxystats.Source = filtered{m}
	if m.plain {
		src = m
	}
	return proxystats.New(proxystats.Config{Source: src, Every: 10 * time.Second, Clock: testutil.FakeClock(),
		Installs: func() ([]proxystats.Install, *cache.Snapshot) { return nil, nil }})
}

// The filter names only the asked clusters' membership stats, quoted.
func TestMembershipFilterNamesOnlyTheClusters(t *testing.T) {
	f := proxystats.MembershipFilter([]string{"kube_shop_cart_8080", "a.b+c"})
	re := regexp.MustCompile(f)
	for name, want := range map[string]bool{
		"cluster.kube_shop_cart_8080.membership_healthy": true, "cluster.kube_shop_cart_8080.membership_total": true,
		"cluster.a.b+c.membership_total": true, "cluster.aXb+c.membership_total": false, "cluster.abbc.membership_total": false,
		"cluster.kube_shop_other_80.membership_total": false, "cluster.kube_shop_cart_8080.update_rejected": false,
	} {
		if re.MatchString(name) != want {
			t.Errorf("%s: match %v, want %v", name, !want, want)
		}
	}
}

// One Ready proxy per Gateway is asked; its counts are kept for the asked
// clusters only; a Gateway without a Ready proxy or not observed says why.
func TestMembershipOneReadyProxyPerGateway(t *testing.T) {
	m := newMemberSource()
	c := m.collector()
	edge, east, west := testutil.GwRef("infra", "edge").Ref(), testutil.GwRef("infra", "east").Ref(), testutil.GwRef("infra", "west").Ref()
	ready := func(t proxystats.Target) bool { return t.Name != "edge-0" && t.Name != "east-0" }
	got := c.Membership(context.Background(), install.Resolved{}, nil, []objects.Ref{edge, east, west}, []string{"kube_shop_cart_8080"}, ready)
	want := []proxystats.GatewayMembership{
		{Gateway: edge, Pod: "edge-1", Clusters: map[string]proxystats.Membership{"kube_shop_cart_8080": {Healthy: ptr(int64(0)), Total: ptr(int64(2))}}},
		{Gateway: east, Err: proxystats.ErrNoReadyProxy},
		{Gateway: west, Err: proxystats.ErrNotObserved},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if len(m.fetches) != 1 || m.fetches[0] != "edge-1 "+proxystats.MembershipFilter([]string{"kube_shop_cart_8080"}) {
		t.Fatalf("fetches %q", m.fetches)
	}
}

// An answer is reused for 10 s per (install, Gateway, clusters), then asked again.
func TestMembershipCachedTenSeconds(t *testing.T) {
	m := newMemberSource()
	fc := testutil.FakeClock()
	c := proxystats.New(proxystats.Config{Source: filtered{m}, Every: 10 * time.Second, Clock: fc,
		Installs: func() ([]proxystats.Install, *cache.Snapshot) { return nil, nil }})
	edge := []objects.Ref{testutil.GwRef("infra", "edge").Ref()}
	ask := func(clusters ...string) {
		c.Membership(context.Background(), install.Resolved{}, nil, edge, clusters, nil)
	}
	ask("kube_shop_cart_8080")
	fc.Step(9 * time.Second)
	ask("kube_shop_cart_8080")
	if len(m.fetches) != 1 {
		t.Fatalf("within 10 s: %d fetches", len(m.fetches))
	}
	ask("kube_shop_other_80")
	if len(m.fetches) != 2 {
		t.Fatalf("other clusters: %d fetches", len(m.fetches))
	}
	fc.Step(2 * time.Second)
	ask("kube_shop_cart_8080")
	if len(m.fetches) != 3 {
		t.Fatalf("after 10 s: %d fetches", len(m.fetches))
	}
}

// A failed fetch and a source that cannot filter give fixed-text errors.
func TestMembershipErrors(t *testing.T) {
	m := newMemberSource()
	edge := testutil.GwRef("infra", "edge").Ref()
	only0 := func(t proxystats.Target) bool { return t.Name == "edge-0" }
	got := m.collector().Membership(context.Background(), install.Resolved{}, nil, []objects.Ref{edge}, []string{"x"}, only0)
	if len(got) != 1 || got[0].Err != "connection refused" || got[0].Pod != "edge-0" {
		t.Fatalf("failed fetch: %+v", got)
	}
	m.plain = true
	got = m.collector().Membership(context.Background(), install.Resolved{}, nil, []objects.Ref{edge}, []string{"x"}, nil)
	if len(got) != 1 || got[0].Err != proxystats.ErrNoMembership {
		t.Fatalf("plain source: %+v", got)
	}
}
