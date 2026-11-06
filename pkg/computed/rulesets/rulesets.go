// Package rulesets selects the computed rule set for a controller version
// (rules §2). It is apart from computed so the sets can import computed.
package rulesets

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/kgw23"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/kgw24"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/kgw25"
)

var minorRE = regexp.MustCompile(`^v?(\d+)\.(\d+)(?:[.+-]|$)`)

// IsVersion reports whether Select reads v as a version, not as unknown.
func IsVersion(v string) bool { return minorRE.MatchString(v) }

// Select returns the rule set for a kgateway controller version. An unknown,
// newer or older version gets the nearest set, labelled in Note.
func Select(controllerVersion string) computed.Selection {
	m := minorRE.FindStringSubmatch(controllerVersion)
	if m == nil {
		return computed.Selection{Rules: kgw24.New(), Note: "Controller version unknown; using the 2.4 rules"}
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	unverified := fmt.Sprintf("Rules not yet verified for %d.%d", major, minor)
	switch {
	case major == 2 && minor == 3:
		return computed.Selection{Rules: kgw23.New()}
	case major == 2 && minor == 4:
		return computed.Selection{Rules: kgw24.New()}
	case major == 2 && minor == 5:
		return computed.Selection{Rules: kgw25.New()}
	case major > 2 || major == 2 && minor > 5:
		return computed.Selection{Rules: kgw25.New(), Note: unverified}
	default:
		return computed.Selection{Rules: kgw23.New(), Note: unverified}
	}
}
