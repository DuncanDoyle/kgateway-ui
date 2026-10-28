package health

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// A match is worded in full when short, and an inherited match with
// thousands of headers is cut to maxMatchText bytes at a rune boundary.
func TestMatchTextBounded(t *testing.T) {
	m := objects.Match{Path: &objects.PathMatch{Type: "PathPrefix", Value: "/api"}, Method: "GET",
		Headers: []objects.KeyMatch{{Type: "Exact", Name: "x-env", Value: "prod"}}}
	if got, want := matchText(m), "PathPrefix /api Header x-env Exact prod Method GET"; got != want {
		t.Fatalf("matchText = %q, want %q", got, want)
	}
	for k := range 3200 {
		m.Headers = append(m.Headers, objects.KeyMatch{Type: "Exact", Name: fmt.Sprintf("x-é%04d", k), Value: "v"})
	}
	got := matchText(m)
	body, ok := strings.CutSuffix(got, "…")
	if !ok || len(body) > maxMatchText || len(body) < maxMatchText-3 || !utf8.ValidString(got) ||
		!strings.HasPrefix(got, "PathPrefix /api Header x-env Exact prod Header x-é0000") {
		t.Fatalf("long matchText (%d bytes) = %.80q…", len(got), got)
	}
}
