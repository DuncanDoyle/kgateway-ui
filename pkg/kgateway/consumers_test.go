package kgateway

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

func listed(us ...*unstructured.Unstructured) *install.Owned {
	o := &install.Owned{}
	for _, u := range us {
		gv := u.GroupVersionKind()
		o.Listed = append(o.Listed, install.OwnedObject{Obj: u, Kind: kinds.Served{
			Descriptor: kinds.Descriptor{Group: gv.Group, Kind: gv.Kind}, Version: gv.Version}})
	}
	return o
}

func extension(ns, name string, spec map[string]any) *unstructured.Unstructured {
	u := testutil.GatewayExtension(ns, name)
	u.Object["spec"] = spec
	return u
}

func TestBackendConsumersOnePerRole(t *testing.T) {
	grpc := func(ref map[string]any) map[string]any { return map[string]any{"grpcService": map[string]any{"backendRef": ref}} }
	owned := listed(
		extension("shop", "auth", map[string]any{"extAuth": grpc(map[string]any{"name": "authz", "port": int64(9001)})}),
		extension("shop", "authhttp", map[string]any{"extAuth": map[string]any{"httpService": map[string]any{"backendRef": map[string]any{"name": "authh", "namespace": "sec"}}}}),
		extension("shop", "proc", map[string]any{"extProc": grpc(map[string]any{"name": "proc", "kind": "Backend", "group": "gateway.kgateway.dev"})}),
		extension("edge", "rl", map[string]any{"rateLimit": grpc(map[string]any{"name": "limiter", "port": int64(8081)})}),
	)
	ext := func(ns, name string) objects.Ref {
		return objects.Ref{Group: kinds.KgatewayGroup, Kind: "GatewayExtension", Namespace: ns, Name: name}
	}
	want := []objects.Consumer{
		{Consumer: ext("shop", "auth"), Role: "ext-auth", BackendRef: objects.Ref{Kind: "Service", Namespace: "shop", Name: "authz"}, Port: 9001, Field: "spec.extAuth.grpcService.backendRef"},
		{Consumer: ext("shop", "authhttp"), Role: "http", BackendRef: objects.Ref{Kind: "Service", Namespace: "sec", Name: "authh"}, Field: "spec.extAuth.httpService.backendRef"},
		{Consumer: ext("shop", "proc"), Role: "ext-proc", BackendRef: objects.Ref{Group: kinds.KgatewayGroup, Kind: "Backend", Namespace: "shop", Name: "proc"}, Field: "spec.extProc.grpcService.backendRef"},
		{Consumer: ext("edge", "rl"), Role: "rate-limit", BackendRef: objects.Ref{Kind: "Service", Namespace: "edge", Name: "limiter"}, Port: 8081, Field: "spec.rateLimit.grpcService.backendRef"},
	}
	if got := BackendConsumers(install.Install{}, owned); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestBackendConsumersNoBackendRef(t *testing.T) {
	owned := listed(
		testutil.GatewayExtension("shop", "bare"),
		extension("shop", "noref", map[string]any{"extAuth": map[string]any{"grpcService": map[string]any{}}}),
		extension("shop", "noname", map[string]any{"extProc": map[string]any{"grpcService": map[string]any{"backendRef": map[string]any{"port": int64(1)}}}}),
		extension("shop", "bad", map[string]any{"rateLimit": "oops"}),
		testutil.DirectResponse("shop", "dr"),
	)
	if got := BackendConsumers(install.Install{}, owned); len(got) != 0 {
		t.Fatalf("got %+v, want none", got)
	}
	if got := BackendConsumers(install.Install{}, nil); got != nil {
		t.Fatalf("nil Owned: got %+v", got)
	}
}
