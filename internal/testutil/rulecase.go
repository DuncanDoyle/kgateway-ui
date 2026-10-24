package testutil

import (
	"encoding/json"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// SettingValueFrom is a Settings value meaning the env var uses valueFrom, so the UI cannot read it.
const SettingValueFrom = "<valueFrom>"

// RuleCase is one computed-rules fixture (rules §11): a world of objects and
// the views expected from it, written from Provenance and never generated.
type RuleCase struct {
	// Provenance names the kgateway output (`translator/gateway/testutils/outputs/<file>@<tag>`)
	// or the rules section the expectation comes from.
	Provenance string
	// RuleOnly keeps the case out of the API matrix.
	RuleOnly          bool
	ControllerVersion string
	// Settings are controller env vars; an absent key is an absent env var.
	Settings map[string]string
	// NamespacesOK defaults to true when the file omits it.
	NamespacesOK bool
	// Namespaces maps each existing Namespace to its labels.
	Namespaces map[string]map[string]string
	// Bounds overrides computed.Bounds fields by name.
	Bounds  map[string]int
	Objects []*unstructured.Unstructured
	Expect  map[string]any
}

// LoadRuleCase reads one YAML rule case. Unknown top-level keys fail the
// test; an object without metadata.generation gets generation 1.
func LoadRuleCase(t *testing.T, path string) RuleCase {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test fixture path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var raw struct {
		Provenance        string                       `json:"provenance"`
		RuleOnly          bool                         `json:"ruleOnly"`
		ControllerVersion string                       `json:"controllerVersion"`
		Settings          map[string]string            `json:"settings"`
		NamespacesOK      *bool                        `json:"namespacesOK"`
		Namespaces        map[string]map[string]string `json:"namespaces"`
		Bounds            map[string]int               `json:"bounds"`
		Objects           []json.RawMessage            `json:"objects"`
		Expect            map[string]any               `json:"expect"`
	}
	if err := yaml.UnmarshalStrict(data, &raw); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	c := RuleCase{Provenance: raw.Provenance, RuleOnly: raw.RuleOnly, ControllerVersion: raw.ControllerVersion,
		Settings: raw.Settings, NamespacesOK: raw.NamespacesOK == nil || *raw.NamespacesOK,
		Namespaces: raw.Namespaces, Bounds: raw.Bounds, Expect: raw.Expect}
	for i, o := range raw.Objects {
		// UnmarshalJSON keeps integers as int64, as the API server's objects have them.
		u := &unstructured.Unstructured{}
		if err := u.UnmarshalJSON(o); err != nil {
			t.Fatalf("%s: object %d: %v", path, i, err)
		}
		if u.GetGeneration() == 0 {
			u.SetGeneration(1)
		}
		c.Objects = append(c.Objects, u)
	}
	return c
}
