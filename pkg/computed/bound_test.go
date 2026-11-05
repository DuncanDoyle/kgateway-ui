package computed

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClipDisplay(t *testing.T) {
	short := strings.Repeat("a", MaxDisplayBytes)
	if got := ClipDisplay(short, MaxDisplayBytes); got != short {
		t.Errorf("a value at the limit changed: %d bytes", len(got))
	}
	long := strings.Repeat("a", MaxDisplayBytes+1)
	if got, want := ClipDisplay(long, MaxDisplayBytes), short+truncatedMarker; got != want {
		t.Errorf("long value: %d bytes, want %d", len(got), len(want))
	}
	// A multi-byte rune across the limit is dropped whole.
	multi := strings.Repeat("a", MaxDisplayBytes-1) + strings.Repeat("é", 10)
	got := ClipDisplay(multi, MaxDisplayBytes)
	if !utf8.ValidString(got) || !strings.HasSuffix(got, truncatedMarker) || strings.Contains(got, "�") {
		t.Errorf("cut inside a rune: %q", got[len(got)-20:])
	}
	if want := strings.Repeat("a", MaxDisplayBytes-1) + truncatedMarker; got != want {
		t.Errorf("rune not dropped whole")
	}
	// The status limit uses the same marker.
	status := strings.Repeat("a", MaxStatusBytes)
	if got := ClipDisplay(status+"b", MaxStatusBytes); got != status+truncatedMarker {
		t.Errorf("status value: %d bytes", len(got))
	}
}
