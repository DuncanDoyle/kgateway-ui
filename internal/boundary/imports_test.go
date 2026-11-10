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

const kindsPath = "github.com/DuncanDoyle/snellius/pkg/kinds"

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
	"pkg/cache/crds.go":             "Gateway API CRD metadata through the metadata client",
	"pkg/cache/events.go":           "Warning Events on controller and proxy Pods",
	"pkg/cache/listonce.go":         "the list-only Kubernetes provider",
	"pkg/cache/services.go":         "Services and EndpointSlices, cut down at the informer",
	"pkg/ctrllogs/stream.go":        "follows controller Pod logs",
	"pkg/ctrlmetrics/scrape.go":     "scrapes controller Pods",
	"pkg/extension/pack.go":         "wraps detectors, which detect controller Deployments; gives pack collectors the client",
	"pkg/health/checks.go":          "controller and proxy Deployment readiness",
	"pkg/install/assign.go":         "Namespace objects for scope",
	"pkg/install/install.go":        "Install carries the controller Deployment and Pods",
	"pkg/install/scope.go":          "scope from Pod env and Namespace labels",
	"pkg/install/upstream.go":       "detects controller Deployments",
	"pkg/process/kube.go":           "process facts from Pods, Deployments and Events",
	"pkg/proxystats/kube.go":        "the Kubernetes proxy stats source: proxy Pod targets and transport",
	"pkg/scopeverify/verify.go":     "Namespace objects for scope",
	"pkg/setup/setup.go":            "builds cache and engine config from the Kubernetes clients",
}

// violations walks the Go files under root. bad lists non-test files outside
// allow that use a forbidden import or the discovery client; stale lists
// allow entries that no longer need to be there.
func violations(root string, allow map[string]string) (bad, stale []string, err error) {
	return walk(root, allow, func(rel string, f *ast.File) []string { return offending(f, strings.HasPrefix(rel, "pkg/kinds/")) })
}

// walk runs check on each non-test Go file under root. bad lists the files
// outside allow it reports; stale lists allow entries it no longer reports.
func walk(root string, allow map[string]string, check func(rel string, f *ast.File) []string) (bad, stale []string, err error) {
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
		uses := check(rel, f)
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

const module = "github.com/DuncanDoyle/snellius/"

// packPkgs are the kgateway pack's packages (Phase 5 design §4.1); a "/" suffix
// names a prefix.
var packPkgs = []string{"pkg/kgateway", "pkg/ctrlmetrics", "pkg/ctrllogs", "pkg/proxystats", "pkg/scopeverify", "pkg/proxymode",
	"pkg/computed/rulesets", "pkg/computed/kgw/"}

// inPack reports whether path, relative to the module, is in a pack package.
func inPack(path string) bool {
	return slices.ContainsFunc(packPkgs, func(p string) bool {
		if prefix, ok := strings.CutSuffix(p, "/"); ok {
			return strings.HasPrefix(path, prefix)
		}
		return path == p || strings.HasPrefix(path, p+"/")
	})
}

// packAllowed are the core files that import a pack package, with the reason.
// It only shrinks; a new entry needs a reviewed reason.
var packAllowed = map[string]string{
	"pkg/api/details.go":         "the Gateway data plane detail reads the controller metrics entry (§7.2); the Gateway CRD feature table",
	"pkg/app/app.go":             "wiring: runs the pack collectors and reports their capabilities",
	"pkg/engine/engine.go":       "compatibility surface (§11): Config's verifier, proxy mode and kgateway defaults without HooksFor",
	"pkg/engine/memo.go":         "prints the scope verification result in health.Input (§11)",
	"pkg/extension/extension.go": "wiring: the Registry hook fields and options (§11)",
	"pkg/extension/kgateway.go":  "wiring: builds the kgateway pack's collectors",
	"pkg/extension/pack.go":      "wiring: assembles the kgateway pack",
	"pkg/health/checks.go":       "the proxy check's self-managed KGWUI-010 and its optional deployer-error enrichment",
	"pkg/health/finding.go":      "compatibility surface (§11): health.Input's proxy mode and scope verification",
	"pkg/health/process.go":      "the restart and rollout checks' optional controller-log enrichment (§6)",
	"pkg/health/proxystats.go":   "pack checks housed in pkg/health: KGWUI-025 and 026",
	"pkg/health/withheld.go":     "pack checks housed in pkg/health: KGWUI-024",
	"pkg/health/xds.go":          "pack checks housed in pkg/health: KGWUI-001",
	"pkg/setup/setup.go":         "wiring: the kgateway pack's interval-sized scope verifier",
}

// packImports lists the pack packages a core file under pkg/ imports.
func packImports(rel string, f *ast.File) []string {
	if !strings.HasPrefix(rel, "pkg/") || inPack(filepath.ToSlash(filepath.Dir(rel))) {
		return nil
	}
	var out []string
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if rest, ok := strings.CutPrefix(p, module); ok && inPack(rest) {
			out = append(out, rest)
		}
	}
	return out
}

// The path model is core (Phase 6 design §4): no pack package and no
// packAllowed entry, so the check below covers all of pkg/paths.
func TestPathsIsCore(t *testing.T) {
	if inPack("pkg/paths") {
		t.Fatal("pkg/paths is a pack package")
	}
	for f := range packAllowed {
		if strings.HasPrefix(f, "pkg/paths/") {
			t.Fatalf("%s may import a pack package", f)
		}
	}
}

// Core code names no kgateway-pack package unless its file is listed with a
// reason (Phase 5 design §13): wiring, the compatibility surface, or a pack
// check still housed in pkg/health.
func TestCoreImportsNoPackPackage(t *testing.T) {
	bad, stale, err := walk(filepath.Join("..", ".."), packAllowed, packImports)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bad {
		t.Errorf("%s\n\tcore code must not import a kgateway-pack package; move the use into the pack, or add a reviewed packAllowed entry", b)
	}
	for _, s := range stale {
		t.Errorf("%s no longer imports a pack package; remove its packAllowed entry (the list only shrinks)", s)
	}
}

// The pack check catches a core import, ignores pack packages, test files and
// code outside pkg/, and reports a stale entry.
func TestPackBoundaryCatchesViolations(t *testing.T) {
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
	imp := func(pkg, path string) string { return "package " + pkg + "\n\nimport _ \"" + module + path + "\"\n" }
	write("pkg/core/a.go", imp("core", "pkg/kgateway"))
	write("pkg/core/b.go", imp("core", "pkg/computed/kgw24"))
	write("pkg/core/c_test.go", imp("core", "pkg/ctrlmetrics"))
	write("pkg/core/d.go", imp("core", "pkg/computed/viewa"))
	write("pkg/ctrllogs/e.go", imp("ctrllogs", "pkg/ctrlmetrics"))
	write("pkg/computed/kgw99/f.go", imp("kgw99", "pkg/computed/rulesets"))
	write("cmd/x/main.go", imp("main", "pkg/kgateway"))
	bad, stale, err := walk(root, map[string]string{"pkg/gone.go": "removed"}, packImports)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pkg/core/a.go: pkg/kgateway", "pkg/core/b.go: pkg/computed/kgw24"}; !slices.Equal(bad, want) {
		t.Errorf("bad: got %q, want %q", bad, want)
	}
	if !slices.Equal(stale, []string{"pkg/gone.go"}) {
		t.Errorf("stale: got %q", stale)
	}
}
