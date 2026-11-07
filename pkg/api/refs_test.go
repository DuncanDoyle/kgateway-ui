package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DuncanDoyle/snellius/pkg/objects"
)

const refsBase = "/api/v1/installs/kgw.kgateway/refs?"

// refsQuery is the GET …/refs query for refs, each group/Kind/namespace/name.
func refsQuery(refs ...string) string {
	q := url.Values{"ref": refs}
	return refsBase + q.Encode()
}

// TestGoldenRefs pins GET …/refs for every caller and for one who may not
// view shop/secret or infra/edge: hidden and absent read the same (D-013).
func TestGoldenRefs(t *testing.T) { runGolden(t, refsCases(t)) }

func refsCases(t *testing.T) []goldenCase {
	_, srv, _ := startWith(t, setup{}, explorerWorld(partialLS())...)
	_, denied, _ := startWith(t, setup{authz: restrictedAuthz()}, explorerWorld(probeRoutes()...)...)
	const gw, hr = "gateway.networking.k8s.io/Gateway/", "gateway.networking.k8s.io/HTTPRoute/"
	return []goldenCase{
		{"refs", srv, refsQuery(gw+"infra/edge", hr+"shop/api", "gateway.networking.k8s.io/ListenerSet/infra/ls1",
			"gateway.networking.k8s.io/GatewayClass/-/kgateway", hr+"shop/ghost")},
		{"refs-restricted", denied, refsQuery(hr+"shop/uses-be", hr+"shop/secret", hr+"shop/ghost", gw+"infra/edge")},
	}
}

// A hidden ref and an absent one get the same answer, and that answer is the
// request echoed with link none: nothing a LinkedRef would not show.
func TestRefsHiddenAndAbsentAreIndistinguishable(t *testing.T) {
	_, srv, _ := startWith(t, setup{authz: restrictedAuthz()}, explorerWorld(probeRoutes()...)...)
	hidden := get[TrailRefs](t, srv, refsQuery("gateway.networking.k8s.io/HTTPRoute/shop/secret"), http.StatusOK)
	absent := get[TrailRefs](t, srv, refsQuery("gateway.networking.k8s.io/HTTPRoute/shop/zzzzzz"), http.StatusOK)
	want := func(name string) LinkedRef {
		return LinkedRef{Ref: objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: name}, Link: LinkNone}
	}
	if len(hidden.Refs) != 1 || hidden.Refs[0] != want("secret") {
		t.Errorf("hidden: %+v", hidden.Refs)
	}
	if len(absent.Refs) != 1 || absent.Refs[0] != want("zzzzzz") {
		t.Errorf("absent: %+v", absent.Refs)
	}
}

// Each ref's link is the one the route list gives the same caller.
func TestRefsAgreeWithListLinks(t *testing.T) {
	_, srv, _ := startWith(t, setup{authz: restrictedAuthz()}, explorerWorld(probeRoutes()...)...)
	rows := get[Page[RouteRow]](t, srv, "/api/v1/installs/kgw.kgateway/routes", http.StatusOK)
	if len(rows.Items) == 0 {
		t.Fatal("no route rows")
	}
	for _, row := range rows.Items {
		ns := row.Ref.Namespace
		got := get[TrailRefs](t, srv, refsQuery(objects.GatewayAPIGroup+"/HTTPRoute/"+ns+"/"+row.Ref.Name), http.StatusOK)
		if got.Refs[0].Link != row.Ref.Link {
			t.Errorf("%s/%s: refs link %q, list link %q", ns, row.Ref.Name, got.Refs[0].Link, row.Ref.Link)
		}
	}
}

func TestRefsRejectsBadInput(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, explorerWorld()...)
	ok := "gateway.networking.k8s.io/Gateway/infra/edge"
	for name, q := range map[string]string{
		"no refs":           refsBase,
		"seven refs":        refsQuery(ok, ok, ok, ok, ok, ok, ok),
		"unknown parameter": refsQuery(ok) + "&via=x",
		"three segments":    refsQuery("gateway.networking.k8s.io/Gateway/edge"),
		"empty group":       refsQuery("/Gateway/infra/edge"),
		"upper-case name":   refsQuery("gateway.networking.k8s.io/Gateway/infra/Edge"),
		"long namespace":    refsQuery("gateway.networking.k8s.io/Gateway/" + strings.Repeat("a", 64) + "/edge"),
		"empty name":        refsQuery("gateway.networking.k8s.io/Gateway/infra/"),
		"kind not served":   refsQuery("core/Secret/shop/x"),
		"bad kind":          refsQuery("gateway.networking.k8s.io/Gate-way/infra/edge"),
		"malformed query":   refsBase + "ref=%zz",
	} {
		t.Run(name, func(t *testing.T) { get[ErrorResponse](t, srv, q, http.StatusBadRequest) })
	}
	get[ErrorResponse](t, srv, "/api/v1/installs/nope/refs?ref="+ok, http.StatusNotFound)
	if got := get[TrailRefs](t, srv, refsQuery(ok, ok, ok, ok, ok, ok), http.StatusOK); len(got.Refs) != 6 {
		t.Errorf("six refs: got %d", len(got.Refs))
	}
	// A long query is refused before it is parsed.
	resp, err := http.Get(srv.URL + refsBase + strings.Repeat("ref=%zz&", maxRefsQuery/8+1))
	if err != nil {
		t.Fatal(err)
	}
	var e ErrorResponse
	_ = json.NewDecoder(resp.Body).Decode(&e)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(e.Error, "too long") {
		t.Errorf("long query: %d %q, want 400 too long", resp.StatusCode, e.Error)
	}
	// Six refs of the longest valid parts, percent-encoded, fit under the cap.
	long := "a." + strings.Repeat("b", 250) + "/K" + strings.Repeat("k", 62) + "/" + strings.Repeat("n", 63) + "/" + strings.Repeat("m", 253)
	q := url.Values{"ref": {long, long, long, long, long, long}}.Encode()
	if len(q) > maxRefsQuery {
		t.Errorf("six longest refs take %d bytes, cap %d", len(q), maxRefsQuery)
	}
}
