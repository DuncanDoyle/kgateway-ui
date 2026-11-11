package kgateway

import (
	"strings"

	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// consumerFields are the GatewayExtension backendRef paths, the same in kgateway
// 2.3, 2.4 and 2.5.
var consumerFields = []struct {
	role string
	path []string
}{
	{"ext-auth", []string{"spec", "extAuth", "grpcService", "backendRef"}},
	{"http", []string{"spec", "extAuth", "httpService", "backendRef"}},
	{"ext-proc", []string{"spec", "extProc", "grpcService", "backendRef"}},
	{"rate-limit", []string{"spec", "rateLimit", "grpcService", "backendRef"}},
}

// BackendConsumers lists the backends the install's GatewayExtensions use, in
// listed order, one per backendRef that names a backend.
func BackendConsumers(_ install.Install, owned *install.Owned) []objects.Consumer {
	if owned == nil {
		return nil
	}
	var out []objects.Consumer
	for _, o := range owned.Listed {
		if o.Kind.Group != kinds.KgatewayGroup || o.Kind.Kind != "GatewayExtension" {
			continue
		}
		self := objects.Ref{Group: kinds.KgatewayGroup, Kind: "GatewayExtension", Namespace: o.Obj.GetNamespace(), Name: o.Obj.GetName()}
		for _, f := range consumerFields {
			if ref, port, ok := objects.DecodeBackendRefAt(o.Obj, f.path...); ok {
				out = append(out, objects.Consumer{Consumer: self, Role: f.role, BackendRef: ref, Port: port, Field: strings.Join(f.path, ".")})
			}
		}
	}
	return out
}
