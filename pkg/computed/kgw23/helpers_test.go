package kgw23

import (
	"slices"

	"github.com/DuncanDoyle/snellius/pkg/objects"
)

func sameRefs(got, want []objects.Ref) bool {
	g, w := slices.Clone(got), slices.Clone(want)
	slices.SortFunc(g, compareRef)
	slices.SortFunc(w, compareRef)
	return slices.Equal(g, w)
}
