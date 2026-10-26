package kgw23

import (
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// childKey is the report key of child's reporter under parent, and whether
// the status builder writes an entry under it (rules §7). On 2.3 the key is
// always synthesized (pkg/kgateway/query/route.go V3:252-259; reporter at
// translator/httproute/delegation.go V3:83-88). The builder walks the spec
// parentRefs, or the report's refs when there are none, and writes an entry
// only for a ref whose key has a report (pkg/reports/status.go V3:437-442,
// 471-483): so only an empty list, or a spec ref written with the same key,
// gets one. A port is not part of the key. childNS is unused on 2.3.
func childKey(child *objects.HTTPRouteView, _ string, parent objects.Ref) (key computed.ReportKey, writes bool) {
	key = computed.ReportKey{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: parent.Namespace, Name: parent.Name}
	if len(child.WrittenRefs) == 0 {
		return key, true
	}
	for _, w := range child.WrittenRefs {
		if computed.KeyOf(w) == key {
			return key, true
		}
	}
	return key, false
}
