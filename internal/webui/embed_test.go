package webui

import (
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// With dist holding only .gitkeep (a fresh checkout), the placeholder is served.
func TestFallsBackToPlaceholder(t *testing.T) {
	built := fstest.MapFS{"dist/.gitkeep": {}}
	ph := fstest.MapFS{"placeholder/index.html": {Data: []byte("placeholder")}}
	b, err := fs.ReadFile(pick(built, "dist", ph, "placeholder"), "index.html")
	if err != nil || string(b) != "placeholder" {
		t.Fatalf("placeholder not served: %q, %v", b, err)
	}
	built["dist/index.html"] = &fstest.MapFile{Data: []byte("spa")}
	b, _ = fs.ReadFile(pick(built, "dist", ph, "placeholder"), "index.html")
	if string(b) != "spa" {
		t.Fatalf("built SPA not preferred: %q", b)
	}
}

// The binary must always serve an index.html, even before the frontend is built,
// so `go build` works on a fresh checkout.
func TestFSServesIndex(t *testing.T) {
	b, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Fatalf("index.html not embedded: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("index.html is empty")
	}
}

// After `make web`, the embedded index.html is the real SPA, not the placeholder.
func TestEmbeddedSPAWhenBuilt(t *testing.T) {
	if os.Getenv("REQUIRE_SPA") == "" {
		t.Skip("skipped: run `make build && REQUIRE_SPA=1 go test ./internal/webui/...` to check the embedded SPA")
	}
	b, err := fs.ReadFile(FS(), "index.html")
	if err != nil || !strings.Contains(string(b), `<div id="root">`) {
		t.Fatalf("built SPA not embedded: %v", err)
	}
}
