package viewa

import (
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// testKinds are the protocol defaults the tests assume: kgateway's (rules §4.2).
func testKinds(l objects.Listener) []objects.GroupKind {
	switch l.Protocol {
	case "HTTP", "HTTPS":
		return []objects.GroupKind{gk("HTTPRoute"), gk("GRPCRoute")}
	case "TLS":
		if l.TLS != nil && l.TLS.Mode == "Terminate" {
			return []objects.GroupKind{gk("TLSRoute"), gk("TCPRoute")}
		}
		return []objects.GroupKind{gk("TLSRoute")}
	case "TCP":
		return []objects.GroupKind{gk("TCPRoute")}
	case "UDP":
		return nil
	}
	return []objects.GroupKind{gk("HTTPRoute")}
}

// testRules is a rule set of view A alone, with testKinds.
type testRules struct{}

func testSet() testRules { return testRules{} }

func (testRules) Version() string { return "test" }

func (testRules) Attachment(in computed.AttachmentInput) computed.AttachmentResult {
	return Attach(in, Options{AllowedKinds: testKinds})
}
