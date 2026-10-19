// Package webui embeds the built single-page application into the binary.
package webui

import (
	"embed"
	"io/fs"
)

// dist holds the built SPA; only .gitkeep is tracked so a fresh checkout compiles.
//
//go:embed all:dist
var dist embed.FS

// placeholder is the page served until the frontend is built.
//
//go:embed placeholder
var placeholder embed.FS

// FS returns the built SPA when dist/index.html exists, else the placeholder page.
func FS() fs.FS {
	return pick(dist, "dist", placeholder, "placeholder")
}

// pick returns the primary sub-tree if it has an index.html, otherwise the fallback sub-tree.
func pick(primary fs.FS, primaryDir string, fallback fs.FS, fallbackDir string) fs.FS {
	if p, err := fs.Sub(primary, primaryDir); err == nil {
		if _, err := fs.Stat(p, "index.html"); err == nil {
			return p
		}
	}
	sub, err := fs.Sub(fallback, fallbackDir)
	if err != nil {
		panic(err) // embedded at compile time; this cannot fail at runtime
	}
	return sub
}
