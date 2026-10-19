package webui

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

// The binary must always embed an index.html, even before the frontend is built,
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
