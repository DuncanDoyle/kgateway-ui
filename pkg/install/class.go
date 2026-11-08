package install

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ClassOnly returns one class-only install per GatewayClass controllerName
// that no install in claimed lists: an install of a controller no pack
// recognises, known only from its classes (Phase 5 design §5). It has no
// workload, edition or version, and its Impl is "" (the core). Sorted by ID.
func ClassOnly(classes []*unstructured.Unstructured, claimed []Install) []Install {
	taken := map[string]bool{}
	for _, in := range claimed {
		for _, n := range in.ControllerNames {
			taken[n] = true
		}
	}
	var out []Install
	for _, c := range classes {
		name, _, _ := unstructured.NestedString(c.Object, "spec", "controllerName")
		if name == "" || taken[name] {
			continue
		}
		taken[name] = true
		id := ClassOnlyID(name)
		// Gateway API names hold "/", so their IDs hold "~", which a workload ID never does.
		if ValidateID(id) != nil || slices.ContainsFunc(claimed, func(in Install) bool { return in.ID() == id }) {
			continue
		}
		out = append(out, Install{CustomID: id, DisplayName: "Gateway API controller " + name, ControllerNames: []string{name}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// ClassOnlyID is the install ID of controllerName's class-only install: every
// byte outside A-Z a-z 0-9 . - becomes "~" and two hex digits, so distinct
// names get distinct IDs. "example.com/gateway" → "example.com~2Fgateway".
func ClassOnlyID(controllerName string) string {
	var b strings.Builder
	for i := range len(controllerName) {
		switch c := controllerName[i]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '-':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "~%02X", c)
		}
	}
	return b.String()
}
