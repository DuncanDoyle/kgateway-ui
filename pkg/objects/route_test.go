package objects_test

import (
	"reflect"
	"testing"

	. "github.com/DuncanDoyle/snellius/pkg/objects"
)

func TestDecodeGRPCRules(t *testing.T) {
	v := DecodeRoute(obj("GRPCRoute", `{"rules":[
	 {"name":"a","matches":[
	   {"method":{"service":"pkg.Svc","method":"Get"},"headers":[{"name":"x","value":"1"},{"type":"RegularExpression","name":"y","value":"2.*"}]},
	   {"method":{"type":"RegularExpression","service":"p.*"}}],
	  "filters":[{"type":"RequestMirror","requestMirror":{"backendRef":{"name":"shadow","port":9090},"percent":10}}],
	  "backendRefs":[{"name":"a","port":50051,"weight":3,"filters":[{"type":"RequestHeaderModifier"}]},{"name":"b","port":50051}]},
	 {"matches":[]}]}`, ""))
	if len(v.Rules) != 2 {
		t.Fatalf("rules: %+v", v.Rules)
	}
	r := v.Rules[0]
	wantM := []GRPCMatch{
		{Method: &GRPCMethodMatch{Type: "Exact", Service: "pkg.Svc", Method: "Get"},
			Headers: []KeyMatch{{Type: "Exact", Name: "x", Value: "1"}, {Type: "RegularExpression", Name: "y", Value: "2.*"}}},
		{Method: &GRPCMethodMatch{Type: "RegularExpression", Service: "p.*"}},
	}
	if r.Name != "a" || r.Matches != nil || !reflect.DeepEqual(r.GRPCMatches, wantM) {
		t.Fatalf("matches: %+v", r)
	}
	if len(r.BackendRefs) != 2 {
		t.Fatalf("refs: %+v", r.BackendRefs)
	}
	a, b := r.BackendRefs[0], r.BackendRefs[1]
	if a.Ref.Name != "a" || a.Ref.Kind != "Service" || a.Ref.Namespace != "shop" || a.Port != 50051 || a.Weight != 3 || a.WeightDefaulted || len(a.Filters) != 1 {
		t.Fatalf("a: %+v", a)
	}
	if b.Weight != 1 || !b.WeightDefaulted {
		t.Fatalf("b: %+v", b)
	}
	if len(r.Filters) != 1 || r.Filters[0].Mirror == nil || r.Filters[0].Mirror.Backend.Name != "shadow" ||
		r.Filters[0].Mirror.Port != 9090 || *r.Filters[0].Mirror.Percent != 10 {
		t.Fatalf("mirror: %+v", r.Filters)
	}
	if v.Rules[1].GRPCMatches == nil || len(v.Rules[1].GRPCMatches) != 0 {
		t.Fatal("explicit empty matches")
	}
}

func TestDecodeTCPTLSRules(t *testing.T) {
	for _, kind := range []string{"TCPRoute", "TLSRoute"} {
		v := DecodeRoute(obj(kind, `{"rules":[{"backendRefs":[{"name":"a","port":5432,"weight":2},{"name":"b","namespace":"db","port":5433}]}]}`, ""))
		if len(v.Rules) != 1 || v.Rules[0].Matches != nil || v.Rules[0].GRPCMatches != nil {
			t.Fatalf("%s: %+v", kind, v.Rules)
		}
		a, b := v.Rules[0].BackendRefs[0], v.Rules[0].BackendRefs[1]
		if a.Port != 5432 || a.Weight != 2 || a.WeightDefaulted || b.Port != 5433 || b.Weight != 1 || !b.WeightDefaulted || b.Ref.Namespace != "db" {
			t.Fatalf("%s: %+v %+v", kind, a, b)
		}
	}
}

func TestRouteBackendTargetsUnchanged(t *testing.T) {
	cases := []struct {
		kind, spec string
		want       []BackendTarget
	}{
		{"GRPCRoute", `{"rules":[{"filters":[{"type":"RequestMirror","requestMirror":{"backendRef":{"name":"m"}}}],
		  "backendRefs":[{"name":"a","port":1,"weight":5,"filters":[{"type":"RequestMirror","requestMirror":{"backendRef":{"name":"n"}}}]},{"name":"b"}]}]}`,
			[]BackendTarget{{Ref: Ref{Kind: "Service", Namespace: "shop", Name: "m"}, Mirror: true}, {Ref: Ref{Kind: "Service", Namespace: "shop", Name: "a"}},
				{Ref: Ref{Kind: "Service", Namespace: "shop", Name: "n"}, Mirror: true}, {Ref: Ref{Kind: "Service", Namespace: "shop", Name: "b"}}}},
		{"TCPRoute", `{"rules":[{"backendRefs":[{"name":"a","port":1},{"name":"b","namespace":"db"}]}]}`,
			[]BackendTarget{{Ref: Ref{Kind: "Service", Namespace: "shop", Name: "a"}}, {Ref: Ref{Kind: "Service", Namespace: "db", Name: "b"}}}},
		{"TLSRoute", `{"rules":[{}, {"backendRefs":[{"name":"a"}]}]}`,
			[]BackendTarget{{Ref: Ref{Kind: "Service", Namespace: "shop", Name: "a"}}}},
	}
	for _, c := range cases {
		u := obj(c.kind, c.spec, "")
		if got := RouteBackendTargets(u); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s RouteBackendTargets: %+v", c.kind, got)
		}
		if got := DecodeRoute(u).Targets; !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s DecodeRoute.Targets: %+v", c.kind, got)
		}
	}
}
