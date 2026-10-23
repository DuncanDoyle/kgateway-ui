package api

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
)

// Regenerate with: go test ./pkg/api -run Golden -update (never in CI).
// testdata/golden-v1 is the frozen Phase 1 copy and is never regenerated.
var update = flag.Bool("update", false, "rewrite the golden files under testdata/golden")

// TestGoldenWireFormat pins the v1 JSON (design §7, H11) without the Go DTO
// types: bodies are decoded into plain JSON values, so a renamed tag or a
// changed omitempty fails the compare. The fixture world uses the fixed clock
// and UIDs of internal/testutil, and the cache stamps snapshots on that clock,
// so only the build version is normalised. A change to an existing golden must
// be additive (TestGoldenChangesAreAdditive).
func TestGoldenWireFormat(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	// broken: error (KGWUI-002), restricted below; denied: error, unrestricted; blog/posts: warning
	// (KGWUI-004); young, created inside the grace period: info (KGWUI-003).
	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	// denied: an unrestricted KGWUI-002, so the goldens pin a finding's "since".
	denied := testutil.HTTPRoute("shop", "denied", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "RefNotPermitted")}})
	authz := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Namespace != "shop" || ref.Name != "broken" })
	_, kube, _ := startWith(t, setup{authz: authz, late: []*unstructured.Unstructured{testutil.HTTPRoute("shop", "young", []objects.ParentRef{gw})}}, world(broken, denied)...)
	// D-016 H2/H3: no namespace, name, scope or replica counts.
	_, bare, _ := startWith(t, setup{detectors: []install.Detector{idDetector{id: "acme~edge_1"}}, metrics: okMetrics("acme~edge_1"), platform: platform.Info{Name: "Test"}}, world()...)

	for _, tc := range []struct {
		golden string
		srv    *httptest.Server
		path   string
	}{
		{"meta", kube, "/api/v1/meta"},
		{"installs", kube, "/api/v1/installs"},
		{"overview", kube, "/api/v1/installs/kgw.kgateway/overview"},
		{"health", kube, "/api/v1/installs/kgw.kgateway/health"},
		{"resource", kube, "/api/v1/installs/kgw.kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/cart"},
		{"not-found", kube, "/api/v1/installs/kgw.kgateway/resources/-/Secret/shop/x"},
		{"bare-meta", bare, "/api/v1/meta"},
		{"bare-installs", bare, "/api/v1/installs"},
		{"bare-overview", bare, "/api/v1/installs/acme~edge_1/overview"},
	} {
		t.Run(tc.golden, func(t *testing.T) { checkGolden(t, tc.golden, tc.path, wire(t, tc.srv, tc.path)) })
	}
}

// TestGoldenExplorerLists pins the 2.2 list bodies (design §7.6). The
// restricted variant is the same backend query for a denied Backend and an
// absent one, so it also pins that they are identical.
func TestGoldenExplorerLists(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, explorerWorld()...)
	_, denied, _ := startWith(t, setup{authz: restrictedAuthz()}, explorerWorld(append(probeRoutes(), testutil.Backend("shop", "be-x", nil))...)...)
	_, absent, _ := startWith(t, setup{authz: restrictedAuthz()}, explorerWorld(probeRoutes()...)...)
	const base, edge = "/api/v1/installs/kgw.kgateway", "gateway.networking.k8s.io/Gateway/infra/edge"
	for _, tc := range []struct {
		golden string
		srv    *httptest.Server
		path   string
	}{
		{"routes", srv, base + "/routes"},
		{"routes-filtered", srv, base + "/routes?parent=" + edge + "&state=accepted&sort=name.desc&limit=2"},
		{"gateways", srv, base + "/gateways"},
		{"listenersets", srv, base + "/listenersets"},
		{"bad-request", srv, base + "/routes?limit=0"},
		{"routes-by-backend", srv, base + "/routes?backend=core/Service/shop/cart-svc&backend=gateway.kgateway.dev/Backend/shop/be1&backend=core/Service/shop/mirror-svc"},
		{"routes-restricted", denied, base + "/routes"},
		{"routes-by-backend-restricted", denied, base + "/routes?backend=gateway.kgateway.dev/Backend/shop/be-x"},
		{"routes-by-backend-restricted", absent, base + "/routes?backend=gateway.kgateway.dev/Backend/shop/be-x"},
	} {
		t.Run(tc.golden, func(t *testing.T) { checkGolden(t, tc.golden, tc.path, wire(t, tc.srv, tc.path)) })
	}
}

// TestGoldenExplorerDetails pins the 2.2 detail bodies (design §7.6).
func TestGoldenExplorerDetails(t *testing.T) {
	notAllowed := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowed"), testutil.Cond("Programmed", metav1.ConditionFalse, "NotAllowed")}
	na := func() *unstructured.Unstructured {
		return lsWith("na", "ListenerSet", edgeRef, notAllowed, testutil.Listener{Name: "extra"})
	}
	_, gws, _ := startWith(t, setup{}, listenerEdge(explorerWorld())...)
	_, unset, _ := startWith(t, setup{}, world(na())...)
	_, allowed, _ := startWith(t, setup{}, edgeAllowing(map[string]any{"namespaces": map[string]any{"from": "Same"}}, world(na()))...)
	_, deleg, _ := startWith(t, setup{}, delegationWorld()...)
	_, restricted, _ := startWith(t, setup{authz: hideMidDeps()}, delegationWorld()...)
	mid := testutil.WithRules(testutil.HTTPRoute("shop", "mid", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		map[string]any{"backendRefs": []any{testutil.DelegateTo("blog", "far")}})
	_, cand, _ := startWith(t, setup{}, world(append(farWorld(), mid)...)...)
	for _, tc := range []struct {
		golden string
		srv    *httptest.Server
		path   string
	}{
		{"gateway-detail", gws, gwDetail},
		{"listenerset-detail", unset, lsDetail + "infra/na"},
		{"listenerset-detail-allowed", allowed, lsDetail + "infra/na"},
		{"route-detail", deleg, routeDetail + "shop/mid"},
		{"route-detail-restricted", restricted, routeDetail + "shop/mid"},
		{"route-detail-candidate", cand, routeDetail + "blog/far"},
	} {
		t.Run(tc.golden, func(t *testing.T) { checkGolden(t, tc.golden, tc.path, wire(t, tc.srv, tc.path)) })
	}
}

// checkGolden compares got with testdata/golden/<name>.json, or rewrites it under -update.
func checkGolden(t *testing.T, name, path string, got []byte) {
	t.Helper()
	file := filepath.Join("testdata", "golden", name+".json")
	if *update {
		if err := os.WriteFile(file, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("GET %s differs from %s (run with -update if the change is intended):\n--- got\n%s", path, file, got)
	}
}

// normalise replaces the one volatile value, keeping its key and type: the
// build version (set by ldflags).
func normalise(body any, path string) {
	m, ok := body.(map[string]any)
	if !ok {
		return
	}
	if _, isStr := m["version"].(string); isStr && path == "/api/v1/meta" {
		m["version"] = "<build version>"
	}
}

// wire is the canonical JSON of a response: status, the headers clients read
// (an absent header is absent here too) and the body as plain JSON values.
func wire(t *testing.T, srv *httptest.Server, path string) []byte {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var body any
	if err := dec.Decode(&body); err != nil {
		t.Fatalf("GET %s: body is not JSON: %v\n%s", path, err, raw)
	}
	normalise(body, path)
	headers := map[string]string{}
	for _, h := range []string{"Content-Type", "Cache-Control", "Collected-At"} {
		if v := resp.Header.Get(h); v != "" {
			headers[h] = v
		}
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"status": resp.StatusCode, "headers": headers, "body": body}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
