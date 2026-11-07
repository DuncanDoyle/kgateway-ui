package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// extServer serves NewHandler over src with mounts and a one-file SPA.
func extServer(t *testing.T, src ModelSource, m ...ExtMount) *httptest.Server {
	t.Helper()
	ui := fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}}
	srv := httptest.NewServer(NewHandler(src, ui, nil, WithExtMounts(m...)))
	t.Cleanup(srv.Close)
	return srv
}

// fetch returns the status, content type and body of GET path.
func fetch(t *testing.T, srv *httptest.Server, path string) (int, string, string) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(b)
}

func TestExtMountServesUnderPrefix(t *testing.T) {
	e, _, _ := start(t, nil, "", nil, world()...)
	hello := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, map[string]string{"hello": "acme"}) })
	srv := extServer(t, e, ExtMount{Name: "acme", Pattern: "GET /hello", Handler: hello})

	if got := get[map[string]string](t, srv, "/api/ext/acme/hello", http.StatusOK); got["hello"] != "acme" {
		t.Errorf("GET /api/ext/acme/hello: %v", got)
	}
	// Unknown paths under and around the prefix get the API's JSON 404, never the SPA.
	for _, p := range []string{"/api/ext/acme/missing", "/api/ext/acme/hello/x", "/api/ext/acme", "/api/ext/other/hello", "/hello"} {
		code, ct, body := fetch(t, srv, p)
		if p == "/hello" {
			if code != http.StatusOK || !strings.Contains(body, "spa") {
				t.Errorf("GET %s: want the SPA outside /api/, got %d %q", p, code, body)
			}
			continue
		}
		var e ErrorResponse
		if code != http.StatusNotFound || ct != "application/json" || json.Unmarshal([]byte(body), &e) != nil || e.Error != "not found" {
			t.Errorf("GET %s: want the JSON 404, got %d %s %q", p, code, ct, body)
		}
	}
	// A wrong method falls through to the same JSON 404.
	resp, err := http.Post(srv.URL+"/api/ext/acme/hello", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("POST /api/ext/acme/hello: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	// WriteError writes the API's error body.
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusTeapot, "nope")
	if rec.Code != http.StatusTeapot || strings.TrimSpace(rec.Body.String()) != `{"error":"nope"}` {
		t.Errorf("WriteError: %d %q", rec.Code, rec.Body.String())
	}
}

func TestExtCannotShadowCoreOrSPA(t *testing.T) {
	ok := http.NotFoundHandler()
	bad := []ExtMount{
		{Name: "acme", Pattern: "hello", Handler: ok},
		{Name: "acme", Pattern: "GET hello", Handler: ok},
		{Name: "acme", Pattern: "", Handler: ok},
		{Name: "acme", Pattern: "GET", Handler: ok},
		{Name: "acme", Pattern: "GET example.com/x", Handler: ok},
		{Name: "acme", Pattern: "/../v1/meta", Handler: ok},
		{Name: "acme", Pattern: "GET /a/../../../v1/meta", Handler: ok},
		{Name: "acme", Pattern: "GET /%2e%2e/v1/meta", Handler: ok},
		{Name: "acme", Pattern: "GET /./x", Handler: ok},
		{Name: "acme", Pattern: "GET /a//b", Handler: ok},
		{Name: "acme", Pattern: "GET  /x", Handler: ok},
		{Name: "acme", Pattern: "GET /{x", Handler: ok},
		{Name: "acme", Pattern: "get /x", Handler: ok},
		{Name: "acme", Pattern: "Get /x", Handler: ok},
		{Name: "acme", Pattern: "G(T /x", Handler: ok},
		{Name: "acme", Pattern: "GET /x", Handler: nil},
		{Name: "", Pattern: "GET /x", Handler: ok},
		{Name: "..", Pattern: "GET /v1/meta", Handler: ok},
		{Name: "Acme", Pattern: "GET /x", Handler: ok},
		{Name: "acme/x", Pattern: "GET /x", Handler: ok},
	}
	for _, m := range bad {
		if err := CheckExtMounts(m); err == nil {
			t.Errorf("mount %q %q: want a build error", m.Name, m.Pattern)
		}
	}
	// Two patterns the mux cannot tell apart are rejected, not left to panic.
	if err := CheckExtMounts(ExtMount{Name: "acme", Pattern: "GET /a/{x}", Handler: ok}, ExtMount{Name: "acme", Pattern: "GET /a/{y}", Handler: ok}); err == nil {
		t.Error("conflicting patterns: want a build error")
	}

	// A catch-all stays inside its own prefix: core routes, other extensions and the SPA are untouched.
	all := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, "acme") })
	other := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, "other") })
	mounts := []ExtMount{{Name: "acme", Pattern: "GET /{x...}", Handler: all}, {Name: "other", Pattern: "GET /x", Handler: other}}
	if err := CheckExtMounts(mounts...); err != nil {
		t.Fatal(err)
	}
	e, _, _ := start(t, nil, "", nil, world()...)
	srv := extServer(t, e, mounts...)
	if got := get[string](t, srv, "/api/ext/acme/a/b", http.StatusOK); got != "acme" {
		t.Errorf("acme catch-all: %q", got)
	}
	if got := get[string](t, srv, "/api/ext/other/x", http.StatusOK); got != "other" {
		t.Errorf("other extension: %q", got)
	}
	if m := get[MetaResponse](t, srv, "/api/v1/meta", http.StatusOK); m.Version == "" {
		t.Error("core /api/v1/meta must still serve")
	}
	if code, _, body := fetch(t, srv, "/routes"); code != http.StatusOK || !strings.Contains(body, "spa") {
		t.Errorf("SPA: %d %q", code, body)
	}
	if code, _, body := fetch(t, srv, "/api/ext/other/y"); code != http.StatusNotFound || !strings.Contains(body, "not found") {
		t.Errorf("other's unknown path: %d %q", code, body)
	}
}

func TestVisibleAppliesAuthorizer(t *testing.T) {
	type row struct{ ref objects.Ref }
	refOf := func(r row) objects.Ref { return r.ref }
	a, b := objects.Ref{Kind: "Gateway", Namespace: "infra", Name: "a"}, objects.Ref{Kind: "Gateway", Namespace: "infra", Name: "b"}
	items := []row{{a}, {b}, {a}}
	req := httptest.NewRequest(http.MethodGet, "/api/ext/acme/x", nil)

	var asked []objects.Ref
	denyB := AuthorizerFunc(func(r *http.Request, ref objects.Ref) bool {
		if r != req {
			t.Error("the Authorizer must receive the request")
		}
		asked = append(asked, ref)
		return ref != b
	})
	vis, hidden := Visible(req, denyB, items, refOf)
	if len(vis) != 2 || vis[0].ref != a || vis[1].ref != a || hidden != 1 {
		t.Errorf("deny b: visible %v, hidden %d", vis, hidden)
	}
	// Each ref is asked once per call, as core handlers do.
	if !slices.Equal(asked, []objects.Ref{a, b}) {
		t.Errorf("asked %v, want each ref once", asked)
	}

	// AllowAll, a nil Authorizer and one claiming to admit all make no calls and hide nothing.
	claimsAll := &countingAuthz{all: true}
	for _, az := range []Authorizer{AllowAll{}, &AllowAll{}, nil, claimsAll} {
		vis, hidden := Visible(req, az, items, func(r row) objects.Ref { t.Error("AllowAll must not resolve refs"); return r.ref })
		if len(vis) != 3 || hidden != 0 {
			t.Errorf("%T: visible %d, hidden %d", az, len(vis), hidden)
		}
	}
	if len(claimsAll.asked) != 0 {
		t.Errorf("AllowAll-like Authorizer asked about %v", claimsAll.asked)
	}
	// The result never shares spare capacity with items, so an append cannot overwrite the caller's data.
	spare := make([]row, 2, 4)
	vis, _ = Visible(req, AllowAll{}, spare, refOf)
	_ = append(vis, row{b})
	if spare[:3][2].ref == b {
		t.Error("appending to the result wrote into the caller's backing array")
	}
}

// An extension handler reads the publication once and serves from that one
// value; the API layer around it reads none.
func TestExtHandlersSeeOnePublication(t *testing.T) {
	e, _, _ := start(t, nil, "", nil, world()...)
	confirmed := time.Date(2026, 10, 17, 12, 0, 5, 0, time.UTC)
	src := &countingSource{ModelSource: confirmedAt{e, confirmed}}
	snap := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p := src.Current()
		WriteJSON(w, map[string]time.Time{"computedAt": p.Model.ComputedAt, "confirmedAt": p.ConfirmedAt})
	})
	srv := extServer(t, src, ExtMount{Name: "acme", Pattern: "GET /snapshot", Handler: snap})

	for path, want := range map[string]int32{"/api/ext/acme/snapshot": 1, "/api/ext/acme/missing": 0} {
		src.n.Store(0)
		code, _, body := fetch(t, srv, path)
		if n := src.n.Load(); n != want {
			t.Errorf("GET %s read Current %d times, want %d", path, n, want)
		}
		if code != http.StatusOK {
			continue
		}
		var got map[string]time.Time
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatal(err)
		}
		if !got["computedAt"].Equal(e.Current().Model.ComputedAt) || !got["confirmedAt"].Equal(confirmed) {
			t.Errorf("snapshot %v: want the model and confirmation of one publication", got)
		}
	}
}
