package api

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/computedtest"
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
func TestGoldenWireFormat(t *testing.T) { runGolden(t, wireCases(t)) }

// goldenCase is one golden body and the server and path that produce it.
type goldenCase struct {
	golden string
	srv    *httptest.Server
	path   string
}

func runGolden(t *testing.T, cases []goldenCase) {
	for _, tc := range cases {
		t.Run(tc.golden, func(t *testing.T) { checkGolden(t, tc.golden, tc.path, wire(t, tc.srv, tc.path)) })
	}
}

// wireCases are the Phase 1 goldens.
func wireCases(t *testing.T) []goldenCase {
	gw := testutil.GwRef("infra", "edge")
	// broken: error (KGWUI-002), restricted below; denied: error, unrestricted; blog/posts: warning
	// (KGWUI-004); young, created inside the grace period: info (KGWUI-003).
	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	// denied: an unrestricted KGWUI-002, so the goldens pin a finding's "since".
	denied := testutil.HTTPRoute("shop", "denied", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "RefNotPermitted")}})
	authz := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Namespace != "shop" || ref.Name != "broken" })
	// golden-v1 is frozen at attachedRoutes 0, so these worlds skip view A rather than loosen additivity.
	_, kube, _ := startWith(t, setup{authz: authz, late: []*unstructured.Unstructured{testutil.HTTPRoute("shop", "young", []objects.ParentRef{gw})},
		rulesFor: computedtest.NoViews}, frozenWorld(broken, denied)...)
	// D-016 H2/H3: no namespace, name, scope or replica counts.
	_, bare, _ := startWith(t, setup{detectors: []install.Detector{idDetector{id: "acme~edge_1"}}, metrics: okMetrics("acme~edge_1"), platform: platform.Info{Name: "Test"},
		rulesFor: computedtest.NoViews}, frozenWorld()...)

	return []goldenCase{
		{"meta", kube, "/api/v1/meta"},
		{"installs", kube, "/api/v1/installs"},
		{"overview", kube, "/api/v1/installs/kgw.kgateway/overview"},
		{"health", kube, "/api/v1/installs/kgw.kgateway/health"},
		{"resource", kube, "/api/v1/installs/kgw.kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/cart"},
		{"not-found", kube, "/api/v1/installs/kgw.kgateway/resources/-/Secret/shop/x"},
		{"bare-meta", bare, "/api/v1/meta"},
		{"bare-installs", bare, "/api/v1/installs"},
		{"bare-overview", bare, "/api/v1/installs/acme~edge_1/overview"},
	}
}

// TestGoldenExplorerLists pins the 2.2 list bodies (design §7.6). The
// restricted variant is the same backend query for a denied Backend and an
// absent one, so it also pins that they are identical.
func TestGoldenExplorerLists(t *testing.T) { runGolden(t, listCases(t)) }

func listCases(t *testing.T) []goldenCase {
	_, srv, _ := startWith(t, setup{}, explorerWorld(partialLS())...)
	_, denied, _ := startWith(t, setup{authz: restrictedAuthz()}, explorerWorld(append(probeRoutes(), testutil.Backend("shop", "be-x", nil))...)...)
	_, absent, _ := startWith(t, setup{authz: restrictedAuthz()}, explorerWorld(probeRoutes()...)...)
	s, objs := viewAStates()
	_, states, _ := startWith(t, s, objs...)
	const base, edge = "/api/v1/installs/kgw.kgateway", "gateway.networking.k8s.io/Gateway/infra/edge"
	return []goldenCase{
		{"routes", srv, base + "/routes"},
		{"routes-filtered", srv, base + "/routes?parent=" + edge + "&state=accepted&sort=name.desc&limit=2"},
		{"gateways", srv, base + "/gateways"},
		{"listenersets", srv, base + "/listenersets"},
		{"bad-request", srv, base + "/routes?limit=0"},
		{"routes-by-backend", srv, base + "/routes?backend=core/Service/shop/cart-svc&backend=gateway.kgateway.dev/Backend/shop/be1&backend=core/Service/shop/mirror-svc"},
		{"routes-restricted", denied, base + "/routes"},
		{"routes-by-backend-restricted", denied, base + "/routes?backend=gateway.kgateway.dev/Backend/shop/be-x"},
		{"routes-by-backend-restricted", absent, base + "/routes?backend=gateway.kgateway.dev/Backend/shop/be-x"},
		{"routes-by-listener", states, base + "/routes?" + edgeParent + "&listener=drift"},
	}
}

// TestGoldenExplorerDetails pins the 2.2 detail bodies (design §7.6).
func TestGoldenExplorerDetails(t *testing.T) { runGolden(t, detailCases(t)) }

func detailCases(t *testing.T) []goldenCase {
	notAllowed := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowed"), testutil.Cond("Programmed", metav1.ConditionFalse, "NotAllowed")}
	na := func() *unstructured.Unstructured {
		return lsWith("na", "ListenerSet", edgeRef, notAllowed, testutil.Listener{Name: "extra"})
	}
	_, gws, _ := startWith(t, setup{}, listenerEdge(explorerWorld(partialLS()))...)
	s, objs := viewAStates()
	_, states, _ := startWith(t, s, objs...)
	s.authz = hideNamespaces
	_, statesRestricted, _ := startWith(t, s, objs...)
	_, unset, _ := startWith(t, setup{}, world(na())...)
	_, allowed, _ := startWith(t, setup{}, edgeAllowing(map[string]any{"namespaces": map[string]any{"from": "Same"}}, world(na()))...)
	_, deleg, _ := startWith(t, setup{}, delegationWorld()...)
	_, restricted, _ := startWith(t, setup{authz: hideMidDeps()}, delegationWorld()...)
	mid := testutil.WithRules(testutil.HTTPRoute("shop", "mid", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		map[string]any{"backendRefs": []any{testutil.DelegateTo("blog", "far")}})
	_, cand, _ := startWith(t, setup{}, world(append(farWorld(), mid)...)...)
	return []goldenCase{
		{"gateway-detail", states, gwDetail},
		{"gateway-detail-restricted", statesRestricted, gwDetail},
		{"listenerset-detail", unset, lsDetail + "infra/na"},
		{"listenerset-detail-allowed", allowed, lsDetail + "infra/na"},
		{"listenerset-detail-invalid", gws, lsDetail + "infra/partial"},
		{"route-detail", deleg, routeDetail + "shop/mid"},
		{"route-detail-restricted", restricted, routeDetail + "shop/mid"},
		{"route-detail-candidate", cand, routeDetail + "blog/far"},
	}
}

// viewAStates is the 2.2 gateway-detail world with one listener per check
// state (design §7.6): http checked, https and new pending (no conditions, no
// entry), drift disagrees and tcp unknown. drift admits every namespace and
// reports one route more than view A counts, so the world disputes view A on
// purpose. tcp's candidate infra/stream is a pre-v1 TCPRoute under an unknown
// experimental setting. A caller who cannot view Namespaces sees http, a
// Selector listener, as an unrestricted one does: a count is the listener's.
func viewAStates() (setup, []runtime.Object) {
	stream := tcpRoute("v1alpha2", "infra", "stream", []objects.ParentRef{sectionRef("tcp")}, testutil.AcceptedBy(sectionRef("tcp"), testutil.Controller))
	objs := listenerEdge(explorerWorld(partialLS(), stream))
	exp := corev1.EnvVar{Name: install.ExperimentalEnv, ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{Key: "k"}}}
	d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"a"}}]`), exp)
	for i, o := range objs {
		switch v := o.(type) {
		case *appsv1.Deployment:
			if v.Name == "kgateway" {
				objs[i] = d
			}
		case *corev1.Pod:
			if v.Name == "kgateway-0" {
				objs[i] = testutil.ControllerPod(d, "kgateway-0", true)
			}
		case *unstructured.Unstructured:
			if v.GetKind() != "Gateway" || v.GetName() != "edge" {
				continue
			}
			spec, status := v.Object["spec"].(map[string]any), v.Object["status"].(map[string]any)
			spec["listeners"] = append(spec["listeners"].([]any),
				map[string]any{"name": "drift", "port": int64(8081), "protocol": "HTTP", "allowedRoutes": map[string]any{"namespaces": map[string]any{"from": "All"}}},
				map[string]any{"name": "tcp", "port": int64(9000), "protocol": "TCP"})
			http := status["listeners"].([]any)[0].(map[string]any)
			entry := func(name string, n int64) map[string]any {
				return map[string]any{"name": name, "attachedRoutes": n, "supportedKinds": []any{}, "conditions": http["conditions"]}
			}
			// drift admits what http admits: every plain parentRef from a watched namespace.
			status["listeners"] = append(status["listeners"].([]any), entry("drift", http["attachedRoutes"].(int64)+1), entry("tcp", 1))
		}
	}
	served := testutil.ServedUpstream()
	for i := range served {
		if served[i].Kind == "TCPRoute" {
			served[i].Version = "v1alpha2"
		}
	}
	return setup{served: served, disputes: true}, objs
}

// partialLS is an accepted ListenerSet whose one listener is not programmed.
func partialLS() *unstructured.Unstructured {
	cond := testutil.Cond
	return lsWith("partial", "ListenerSet", edgeRef, []metav1.Condition{cond("Accepted", metav1.ConditionTrue, "ListenersNotValid"), cond("Programmed", metav1.ConditionTrue, "Programmed")},
		testutil.Listener{Name: "extra", Conds: []metav1.Condition{cond("Accepted", metav1.ConditionTrue, "Accepted"), cond("Programmed", metav1.ConditionFalse, "PortUnavailable")}})
}

// TestGoldenLinksAgreeWithEndpoints serves every link in every golden body from
// the server that produced it (design §7.1): detail and yaml open, none is a 404.
func TestGoldenLinksAgreeWithEndpoints(t *testing.T) {
	var walked int
	for _, tc := range slices.Concat(wireCases(t), listCases(t), detailCases(t)) {
		install, _, ok := strings.Cut(strings.TrimPrefix(tc.path, "/api/v1/installs/"), "/")
		if !ok { // meta and installs: no install segment, and no links
			continue
		}
		installBase := "/api/v1/installs/" + install
		for _, l := range goldenLinks(readGolden(t, "golden", tc.golden).(map[string]any)["body"]) {
			walked++
			res := strings.Replace(resourcePath(l.ref), base, installBase, 1)
			dp, hasDetail := detailPath(l.ref)
			switch l.link {
			case LinkDetail:
				if !hasDetail {
					t.Errorf("%s: detail link on %v, a kind without a detail view", tc.golden, l.ref)
					continue
				}
				wantStatus(t, tc, installBase+dp, 200)
			case LinkYAML:
				wantStatus(t, tc, res, 200)
			case LinkNone:
				wantStatus(t, tc, res, 404)
				if hasDetail {
					wantStatus(t, tc, installBase+dp, 404)
				}
			default:
				t.Errorf("%s: link %q on %v", tc.golden, l.link, l.ref)
			}
		}
	}
	// Every link in every golden; update it when a golden gains or loses one.
	const want = 211
	if walked != want {
		t.Errorf("walked %d links, want %d", walked, want)
	}
}

// detailPath is ref's detail endpoint below the install, if its kind has one.
func detailPath(r objects.Ref) (string, bool) {
	switch r.Kind {
	case "Gateway":
		return "/gateways/" + r.Namespace + "/" + r.Name, true
	case "HTTPRoute":
		return "/routes/" + r.Group + "/" + r.Kind + "/" + r.Namespace + "/" + r.Name, true
	case "ListenerSet", "XListenerSet":
		return "/listenersets/" + r.Group + "/" + r.Kind + "/" + r.Namespace + "/" + r.Name, true
	}
	return "", false
}

// TestGoldenFilesHaveCases fails on a golden without a case: an orphan is never compared.
func TestGoldenFilesHaveCases(t *testing.T) {
	cased := map[string]bool{}
	for _, tc := range slices.Concat(wireCases(t), listCases(t), detailCases(t)) {
		cased[tc.golden+".json"] = true
	}
	files, err := os.ReadDir(filepath.Join("testdata", "golden"))
	if err != nil {
		t.Fatal(err)
	}
	onDisk := map[string]bool{}
	for _, f := range files {
		onDisk[f.Name()] = true
		if !cased[f.Name()] {
			t.Errorf("testdata/golden/%s has no case", f.Name())
		}
	}
	for name := range cased {
		if !onDisk[name] {
			t.Errorf("case %s has no file under testdata/golden", name)
		}
	}
}

// goldenLink is one link in a golden body and the ref it labels.
type goldenLink struct {
	ref  objects.Ref
	link Link
}

// goldenLinks finds every link in v: on a flattened LinkedRef, or beside a "ref" object.
func goldenLinks(v any) []goldenLink {
	var out []goldenLink
	switch x := v.(type) {
	case map[string]any:
		if l, ok := x["link"].(string); ok {
			r := x
			if _, flat := x["kind"]; !flat {
				r, _ = x["ref"].(map[string]any)
			}
			str := func(k string) string { s, _ := r[k].(string); return s }
			out = append(out, goldenLink{objects.Ref{Group: str("group"), Kind: str("kind"), Namespace: str("namespace"), Name: str("name")}, Link(l)})
		}
		for _, k := range slices.Sorted(maps.Keys(x)) {
			out = append(out, goldenLinks(x[k])...)
		}
	case []any:
		for _, e := range x {
			out = append(out, goldenLinks(e)...)
		}
	}
	return out
}

// wantStatus fails t unless GET path on tc's server answers status.
func wantStatus(t *testing.T, tc goldenCase, path string, status int) {
	t.Helper()
	resp, err := http.Get(tc.srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != status {
		t.Errorf("%s: GET %s = %d, want %d", tc.golden, path, resp.StatusCode, status)
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
