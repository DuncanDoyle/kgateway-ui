package kgw24

import (
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// childKey is the report key of child's reporter under parent, and whether
// the status builder writes an entry under it (rules §7). On 2.4 the reporter
// takes the first ref that permits parent, as written (childParentRef,
// pkg/kgateway/query/route.go V4:300-310, via MatchingParentRef at
// utils/delegation/delegation.go V4:36-56; reporter at
// translator/httproute/delegation.go V4:81). With an empty list it is
// synthesized. Since the key comes from a spec ref, or from the report's own
// refs for an empty list, the builder always writes an entry
// (pkg/reports/status.go V4:489-496, 531-543). A port is not part of the key.
func childKey(child *objects.HTTPRouteView, childNS string, parent objects.Ref) (key computed.ReportKey, writes bool) {
	key = computed.ReportKey{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: parent.Namespace, Name: parent.Name}
	if len(child.WrittenRefs) == 0 {
		return key, true
	}
	for _, w := range child.WrittenRefs {
		ns := w.Namespace
		if ns == "" {
			ns = childNS
		}
		if w.Group == objects.GatewayAPIGroup && w.Kind == "HTTPRoute" && w.Name == parent.Name && ns == parent.Namespace {
			return computed.KeyOf(w), true
		}
	}
	// No permitting ref: the child is forbidden and never reported.
	return key, false
}
