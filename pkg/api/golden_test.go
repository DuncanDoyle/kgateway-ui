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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
)

// Regenerate with: go test ./pkg/api -run Golden -update (never in CI).
var update = flag.Bool("update", false, "rewrite the golden files under testdata/golden")

// TestGoldenWireFormat pins the v1 JSON (design §7, H11) without the Go DTO
// types: bodies are decoded into plain JSON values, so a renamed tag or a
// changed omitempty fails the compare. The fixture world uses the fixed clock
// and UIDs of internal/testutil, so only the build version is normalised.
func TestGoldenWireFormat(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	// broken: error (KGWUI-002), restricted below; blog/posts: warning
	// (KGWUI-004); young, created inside the grace period: info (KGWUI-003).
	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	authz := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Namespace != "shop" || ref.Name != "broken" })
	_, kube, _ := startWith(t, setup{authz: authz, late: []*unstructured.Unstructured{testutil.HTTPRoute("shop", "young", []objects.ParentRef{gw})}}, world(broken)...)
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
		t.Run(tc.golden, func(t *testing.T) {
			got := wire(t, tc.srv, tc.path)
			file := filepath.Join("testdata", "golden", tc.golden+".json")
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
				t.Errorf("GET %s differs from %s (run with -update if the change is intended):\n--- got\n%s", tc.path, file, got)
			}
		})
	}
}

// normalise replaces the two volatile values, keeping their key and type: the
// build version (set by ldflags) and a check's collectedAt taken from the cache
// snapshot, which runs on the wall clock (cache.Config takes no clock). Only a
// UTC RFC 3339 value as time.Time encodes it is replaced, so a zone or format
// regression still fails the compare.
func normalise(body any, path string) {
	m, ok := body.(map[string]any)
	if !ok {
		return
	}
	if _, isStr := m["version"].(string); isStr && path == "/api/v1/meta" {
		m["version"] = "<build version>"
	}
	checks, _ := m["checks"].([]any)
	for _, c := range checks {
		c, _ := c.(map[string]any)
		s, _ := c["collectedAt"].(string)
		if at, err := time.Parse(time.RFC3339Nano, s); err == nil && !at.Equal(testutil.Now) && s == at.UTC().Format(time.RFC3339Nano) {
			c["collectedAt"] = "<snapshot time>"
		}
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
