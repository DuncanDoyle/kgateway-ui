package main

import (
	"slices"
	"testing"

	"github.com/DuncanDoyle/snellius/pkg/kinds"
)

// The core-only binary runs the core alone, with class-only installs on.
func TestCoreBinary(t *testing.T) {
	_, o, reg, err := build([]string{"--class-only-installs=false"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.ClassOnlyInstalls {
		t.Fatal("class-only installs must be on")
	}
	if len(reg.Detectors) != 0 || slices.ContainsFunc(reg.Kinds, func(d kinds.Descriptor) bool { return d.Group == kinds.KgatewayGroup }) {
		t.Fatalf("want the core registry, got detectors %v kinds %+v", reg.Detectors, reg.Kinds)
	}
}
