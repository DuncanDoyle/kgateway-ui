package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/app"
	"github.com/DuncanDoyle/kgateway-ui/pkg/extension"
)

// The README's extension example is this program, so the build compiles it.
func TestReadmeShowsThisExample(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(readme), "## Extending")
	if ok {
		_, block, ok = strings.Cut(block, "```go\n")
	}
	if ok {
		block, _, ok = strings.Cut(block, "```\n")
	}
	if !ok {
		t.Fatal("README has no Go block under ## Extending")
	}
	if block != string(src) {
		t.Fatal("README's extension example differs from examples/downstream/main.go; copy main.go into it")
	}
}

// The example's options and extensions build, and the app serves its UI.
func TestExampleBuildsAndServes(t *testing.T) {
	ui, err := fs.Sub(dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	dyn, kube := testutil.Clients(t)
	a, err := app.Build(app.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery()}, options(ui), extension.New(extensions()...))
	if err != nil {
		t.Fatalf("the example must build: %v", err)
	}
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "example") {
		t.Fatalf("GET /: %d %q", rec.Code, rec.Body.String())
	}
}
