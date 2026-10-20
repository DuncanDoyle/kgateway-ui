package boundary

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// forbidden are the import path prefixes core code must not use (D-016 decision 4).
var forbidden = []string{"k8s.io/api/core/v1", "k8s.io/api/apps/v1", "k8s.io/client-go"}

const kindsPath = "github.com/DuncanDoyle/kgateway-ui/pkg/kinds"

// discovery are the pkg/kinds identifiers that reach the discovery API.
var discovery = []string{"DiscoveryClient", "Resolve", "NewMonitor", "Monitor"}

// allowed is today's Kubernetes provider code, per file, with the reason it
// may use the forbidden imports. It only shrinks; a new entry needs a reviewed reason.
var allowed = map[string]string{
	"internal/testutil/clients.go":  "fake clients for tests",
	"internal/testutil/fixtures.go": "typed fixtures for tests",
	"pkg/api/handlers.go":           "serves Deployments that findings reference",
	"pkg/app/app.go":                "builds the Kubernetes clients and resolves served kinds",
	"pkg/cache/cache.go":            "the Kubernetes informers",
	"pkg/ctrlmetrics/scrape.go":     "scrapes controller Pods",
	"pkg/health/checks.go":          "controller and proxy Deployment readiness",
	"pkg/install/assign.go":         "Namespace objects for scope",
	"pkg/install/install.go":        "Install carries the controller Deployment and Pods",
	"pkg/install/scope.go":          "scope from Pod env and Namespace labels",
	"pkg/install/upstream.go":       "detects controller Deployments",
	"pkg/scopeverify/verify.go":     "Namespace objects for scope",
}

// violations walks the Go files under root. bad lists non-test files outside
// allow that use a forbidden import or the discovery client; stale lists
// allow entries that no longer need to be there.
func violations(root string, allow map[string]string) (bad, stale []string, err error) {
	used := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); path != root && (strings.HasPrefix(n, ".") || n == "web" || n == "docs" || n == "bin" || n == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		uses := offending(f, strings.HasPrefix(rel, "pkg/kinds/"))
		if len(uses) == 0 {
			return nil
		}
		if _, ok := allow[rel]; ok {
			used[rel] = true
			return nil
		}
		bad = append(bad, rel+": "+strings.Join(uses, ", "))
		return nil
	})
	for f := range allow {
		if !used[f] {
			stale = append(stale, f)
		}
	}
	slices.Sort(bad)
	slices.Sort(stale)
	return bad, stale, err
}

// offending lists f's forbidden imports and, outside pkg/kinds, its uses of the discovery client.
func offending(f *ast.File, inKinds bool) []string {
	var out []string
	kindsName := ""
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		for _, fb := range forbidden {
			if p == fb || strings.HasPrefix(p, fb+"/") {
				out = append(out, p)
			}
		}
		if p == kindsPath {
			kindsName = "kinds"
			if imp.Name != nil {
				kindsName = imp.Name.Name
			}
		}
	}
	if kindsName == "" || inKinds {
		return out
	}
	seen := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && x.Name == kindsName && slices.Contains(discovery, sel.Sel.Name) && !seen[sel.Sel.Name] {
			seen[sel.Sel.Name] = true
			out = append(out, "kinds."+sel.Sel.Name)
		}
		return true
	})
	return out
}

// Core code takes no new dependency on Kubernetes workload types, client-go or
// the discovery client (D-016). Semantic assumptions, such as a new Pod-shaped
// field, are invisible here; review checks those.
func TestCoreImportsStayPlatformNeutral(t *testing.T) {
	bad, stale, err := violations(filepath.Join("..", ".."), allowed)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bad {
		t.Errorf("%s\n\tcore code must not use Kubernetes workload types, client-go or the discovery client (D-016); keep it in the provider, or add a reviewed allowlist entry", b)
	}
	for _, s := range stale {
		t.Errorf("%s no longer needs its allowlist entry; remove it (the list only shrinks)", s)
	}
}

// The check itself catches a new core import and a discovery use under an
// alias, ignores test files, and reports a stale allowlist entry.
func TestBoundaryCheckCatchesViolations(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("pkg/core/new.go", "package core\n\nimport _ \"k8s.io/api/core/v1\"\n")
	write("pkg/core/new_test.go", "package core\n\nimport _ \"k8s.io/api/apps/v1\"\n")
	write("pkg/core/disc.go", "package core\n\nimport k \""+kindsPath+"\"\n\nvar _ k.DiscoveryClient\n")
	write("pkg/kinds/own.go", "package kinds\n\nvar _ DiscoveryClient\n")
	bad, stale, err := violations(root, map[string]string{"pkg/gone.go": "removed"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pkg/core/disc.go: kinds.DiscoveryClient", "pkg/core/new.go: k8s.io/api/core/v1"}; !slices.Equal(bad, want) {
		t.Errorf("bad: got %q, want %q", bad, want)
	}
	if !slices.Equal(stale, []string{"pkg/gone.go"}) {
		t.Errorf("stale: got %q", stale)
	}
}
