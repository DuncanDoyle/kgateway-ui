package computed

import (
	"strings"
	"unicode/utf8"
)

// MaxPathBytes bounds a generated path. A longer one is never cut: the
// context is unknown, too-large.
const MaxPathBytes = 4096

// MaxDisplayBytes bounds one value inside a display string such as a reason.
const MaxDisplayBytes = 256

// MaxStatusBytes bounds a status message quoted in evidence (design §18.2).
const MaxStatusBytes = 1024

// truncatedMarker ends a clipped display string.
const truncatedMarker = "…[truncated]"

// ClipDisplay cuts a display string to limit bytes at a UTF-8 boundary and
// adds a marker. Computed paths never pass through it.
func ClipDisplay(p string, limit int) string {
	if len(p) <= limit {
		return p
	}
	n := limit
	for n > 0 && !utf8.RuneStart(p[n]) {
		n--
	}
	return strings.ToValidUTF8(p[:n], "�") + truncatedMarker
}
