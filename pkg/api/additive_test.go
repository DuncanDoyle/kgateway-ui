package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// appended lists, per JSON array path in a fixture, the elements a later
// sub-phase may append after the v1 ones, identified by Key and in this order
// (design §7.6).
var appended = map[string]struct {
	Key    string
	Values []string
}{
	"body.checks": {"name", []string{"backend status", "computed views"}},
	"body.counts": {"kind", []string{"Backend"}},
	"body.kinds":  {"kind", []string{"Backend"}},
}

// snapshotTime in a golden-v1 fixture stands for a cache snapshot time, which
// v1 did not pin.
const snapshotTime = "<snapshot time>"

// TestGoldenChangesAreAdditive checks the current goldens against the frozen v1
// copy: existing goldens change only by optional fields and listed appends.
func TestGoldenChangesAreAdditive(t *testing.T) {
	for _, name := range []string{"meta", "installs", "overview", "health", "resource", "not-found", "bare-meta", "bare-installs", "bare-overview"} {
		additive(t, readGolden(t, "golden-v1", name), readGolden(t, "golden", name), name)
	}
}

func TestAdditiveHelper(t *testing.T) {
	for _, tc := range []struct {
		name, old, cur string
		ok             bool
	}{
		{"appended Backend kind", `{"body":{"kinds":[{"kind":"Gateway"}]}}`, `{"body":{"kinds":[{"kind":"Gateway"},{"kind":"Backend"}]}}`, true},
		{"appended checks in order", `{"body":{"checks":[{"name":"a"}]}}`, `{"body":{"checks":[{"name":"a"},{"name":"backend status"},{"name":"computed views"}]}}`, true},
		{"appended computed views alone", `{"body":{"checks":[{"name":"a"}]}}`, `{"body":{"checks":[{"name":"a"},{"name":"computed views"}]}}`, true},
		{"added optional key", `{"body":[{"computedAt":"t"}]}`, `{"body":[{"computedAt":"t","confirmedAt":"u"}]}`, true},
		{"snapshot time as UTC", `{"body":{"checks":[{"collectedAt":"<snapshot time>"}]}}`, `{"body":{"checks":[{"collectedAt":"2026-10-17T12:00:00Z"}]}}`, true},
		{"snapshot time with an offset", `{"body":{"checks":[{"collectedAt":"<snapshot time>"}]}}`, `{"body":{"checks":[{"collectedAt":"2026-10-17T14:00:00+02:00"}]}}`, false},
		{"reordered", `{"body":{"kinds":[{"kind":"A"},{"kind":"B"}]}}`, `{"body":{"kinds":[{"kind":"B"},{"kind":"A"}]}}`, false},
		{"appended unlisted element", `{"body":{"kinds":[{"kind":"A"}]}}`, `{"body":{"kinds":[{"kind":"A"},{"kind":"Foo"}]}}`, false},
		{"appended where nothing is listed", `{"body":{"gateways":[]}}`, `{"body":{"gateways":[{"kind":"Backend"}]}}`, false},
		{"appended out of order", `{"body":{"checks":[]}}`, `{"body":{"checks":[{"name":"computed views"},{"name":"backend status"}]}}`, false},
		{"path includes the body wrapper", `{"kinds":[]}`, `{"kinds":[{"kind":"Backend"}]}`, false},
		{"removed key", `{"body":{"a":1,"b":2}}`, `{"body":{"a":1}}`, false},
		{"changed value", `{"body":{"a":1}}`, `{"body":{"a":2}}`, false},
		{"changed type", `{"body":{"a":"1"}}`, `{"body":{"a":1}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := additions(decode(t, tc.old), decode(t, tc.cur), "")
			if tc.ok != (len(got) == 0) {
				t.Errorf("ok = %v, want %v: %v", len(got) == 0, tc.ok, got)
			}
		})
	}
}

// additive fails t for every way cur is more than old plus permitted additions.
func additive(t *testing.T, old, cur any, fixture string) {
	t.Helper()
	for _, d := range additions(old, cur, "") {
		t.Errorf("%s: %s", fixture, d)
	}
}

// additions returns every way cur, at JSON path p, is not old plus permitted additions.
func additions(old, cur any, p string) []string {
	switch o := old.(type) {
	case map[string]any:
		c, ok := cur.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: an object became %v", p, cur)}
		}
		var out []string
		for _, k := range slices.Sorted(maps.Keys(o)) {
			kp := k
			if p != "" {
				kp = p + "." + k
			}
			v, ok := c[k]
			if !ok {
				out = append(out, kp+": removed")
				continue
			}
			out = append(out, additions(o[k], v, kp)...)
		}
		return out
	case []any:
		c, ok := cur.([]any)
		if !ok || len(c) < len(o) {
			return []string{fmt.Sprintf("%s: %d elements became %v", p, len(o), cur)}
		}
		var out []string
		for i := range o {
			out = append(out, additions(o[i], c[i], fmt.Sprintf("%s[%d]", p, i))...)
		}
		return append(out, appendedOK(c[len(o):], p)...)
	case string:
		if o == snapshotTime {
			s, _ := cur.(string)
			if at, err := time.Parse(time.RFC3339Nano, s); err != nil || s != at.UTC().Format(time.RFC3339Nano) {
				return []string{fmt.Sprintf("%s: %v is not a UTC RFC 3339 time", p, cur)}
			}
			return nil
		}
	}
	// Scalars: old's dynamic type is comparable, so this never panics.
	if old != cur {
		return []string{fmt.Sprintf("%s: %v became %v", p, old, cur)}
	}
	return nil
}

// appendedOK checks the elements appended to the array at p against the
// appended rules: each must be listed for p, in the listed order.
func appendedOK(extra []any, p string) []string {
	if len(extra) == 0 {
		return nil
	}
	rule, ok := appended[p]
	if !ok {
		return []string{fmt.Sprintf("%s: %d elements appended, none are allowed here", p, len(extra))}
	}
	var out []string
	next := 0
	for _, e := range extra {
		m, _ := e.(map[string]any)
		v, _ := m[rule.Key].(string)
		j := slices.Index(rule.Values[next:], v)
		if j < 0 {
			out = append(out, fmt.Sprintf("%s: appended %s %q is not listed, or out of order", p, rule.Key, v))
			continue
		}
		next += j + 1
	}
	return out
}

func readGolden(t *testing.T, dir, name string) any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", dir, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return decode(t, string(raw))
}

// decode parses s as plain JSON values, keeping numbers exact.
func decode(t *testing.T, s string) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}
