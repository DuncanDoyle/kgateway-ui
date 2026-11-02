package rulesets

import (
	"regexp"
	"strconv"
)

// gatewayAPIBundles is the Gateway API minor each kgateway minor is built against,
// from its go.mod (v1.5.1, v1.6.1, v1.7.0-rc.3); the 2.5 row is re-read at GA.
var gatewayAPIBundles = map[string]string{"2.3": "v1.5", "2.4": "v1.6", "2.5": "v1.7"}

// ExpectedGatewayAPI returns the Gateway API bundle minor ("v1.6") that kgateway
// kgatewayMinor ("2.4") is built against; false for a minor without a row.
// It is release metadata, not translation (design §9.4).
func ExpectedGatewayAPI(kgatewayMinor string) (bundle string, ok bool) {
	bundle, ok = gatewayAPIBundles[kgatewayMinor]
	return bundle, ok
}

var bundleRE = regexp.MustCompile(`^v1\.(\d+)(?:\.\d+)?(?:-[0-9A-Za-z.-]+)?$`)

// GatewayCRDHasAllowedListeners reports whether a Gateway CRD of channel and bundle
// defines spec.allowedListeners: experimental from v1.3, standard from v1.5.
// known is false when the channel or bundle is not one it can tell.
func GatewayCRDHasAllowedListeners(channel, bundle string) (has, known bool) {
	m := bundleRE.FindStringSubmatch(bundle)
	if m == nil {
		return false, false
	}
	minor, _ := strconv.Atoi(m[1])
	switch channel {
	case "experimental":
		return minor >= 3, true
	case "standard":
		return minor >= 5, true
	}
	return false, false
}
