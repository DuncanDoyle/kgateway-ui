package webui

import (
	"io/fs"
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
