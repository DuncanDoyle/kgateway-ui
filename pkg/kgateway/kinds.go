package kgateway

import (
	"slices"

	"github.com/DuncanDoyle/snellius/pkg/kinds"
)

// Kinds returns kgateway's own kinds (group gateway.kgateway.dev), in the order
// of kinds.Upstream.
func Kinds() []kinds.Descriptor {
	return slices.DeleteFunc(kinds.Upstream(), func(d kinds.Descriptor) bool { return d.Group != kinds.KgatewayGroup })
}
