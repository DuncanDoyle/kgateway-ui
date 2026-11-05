package health

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// findingsDoc is the finding reference that "Learn more" links to.
const findingsDoc = "../../docs/findings.md"

// notRaisedYet marks the entry of a reserved code that no check raises yet.
const notRaisedYet = "This version of the UI does not raise it yet."

// docHeading is one entry's heading; GitHub turns "KGWUI-001" into the anchor #kgwui-001.
var docHeading = regexp.MustCompile(`(?m)^## (KGWUI-\d{3})$`)

// codeConstants returns every Code* constant of this package's non-test files.
func codeConstants(t *testing.T) map[string]string {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Code") || i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					v, _ := strconv.Unquote(lit.Value)
					out[name.Name] = v
				}
			}
			return true
		})
	}
	return out
}

// docEntries maps each entry's code to its body, up to the next entry.
func docEntries(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(findingsDoc)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	out := map[string]string{}
	locs := docHeading.FindAllStringSubmatchIndex(doc, -1)
	for i, l := range locs {
		end := len(doc)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		code := doc[l[2]:l[3]]
		if _, dup := out[code]; dup {
			t.Errorf("%s has two entries", code)
		}
		out[code] = doc[l[1]:end]
	}
	return out
}

func TestEveryCodeHasAFindingsDocAnchor(t *testing.T) {
	entries := docEntries(t)
	consts := codeConstants(t)
	if len(consts) < 20 {
		t.Fatalf("found %d Code constants; the parse missed some", len(consts))
	}
	for name, code := range consts {
		if _, ok := entries[code]; !ok {
			t.Errorf("%s (%s) has no ## %s entry in %s", name, code, code, findingsDoc)
		}
	}
	raised := map[string]bool{}
	for _, code := range consts {
		raised[code] = true
	}
	// One entry per code 001–026: a raised code has its cause, evidence and
	// fix; a reserved one says the UI does not raise it yet.
	for n := 1; n <= 26; n++ {
		code := fmt.Sprintf("KGWUI-%03d", n)
		body, ok := entries[code]
		switch {
		case !ok:
			t.Errorf("no entry for %s", code)
		case raised[code]:
			for _, part := range []string{"**Cause.**", "**Evidence.**", "**Fix.**"} {
				if !strings.Contains(body, part) {
					t.Errorf("%s has no %s", code, part)
				}
			}
		case !strings.Contains(body, notRaisedYet):
			t.Errorf("%s has no constant, so its entry must say %q", code, notRaisedYet)
		}
	}
}
