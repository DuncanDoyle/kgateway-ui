package engine

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

func routeRef(ns, name string) objects.Ref {
	return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: ns, Name: name}
}

func outcome(route objects.Ref, o computed.Outcome, unknown computed.UnknownReason, deps ...objects.Ref) computed.RefOutcome {
	return computed.RefOutcome{Route: route, Outcome: o, Unknown: unknown, Deps: deps}
}

// Roots follow view A (rules §5.1, §5.6).
func TestRootsFromViewA(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	okParent := testutil.HTTPRouteRef("shop", "ok")
	gwRef, nsRef := gw.Ref(), objects.Ref{Kind: "Namespace", Name: "shop"}
	routes := []*unstructured.Unstructured{
		testutil.HTTPRoute("shop", "ok", []objects.ParentRef{gw}),
		testutil.HTTPRoute("shop", "rej", []objects.ParentRef{gw}),
		testutil.HTTPRoute("shop", "status", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.HTTPRoute("shop", "stale", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.HTTPRoute("shop", "denied", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
			Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners")}}),
		testutil.HTTPRoute("shop", "nostatus", []objects.ParentRef{gw}),
		testutil.HTTPRoute("shop", "mixed", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		// A Gateway and an HTTPRoute parent: top-level, rooted by view A.
		testutil.HTTPRoute("shop", "both", []objects.ParentRef{gw, okParent}, testutil.AcceptedBy(okParent, testutil.Controller)),
		// An unknown Gateway parent; the Accepted=True entry is for the HTTPRoute parent.
		testutil.HTTPRoute("shop", "viahttp", []objects.ParentRef{gw, okParent}, testutil.AcceptedBy(okParent, testutil.Controller)),
	}
	routes[3].SetGeneration(2) // the Accepted entry was observed at generation 1
	nss := []*corev1.Namespace{testutil.Namespace("infra", nil), testutil.Namespace("shop", nil)}
	objs := append([]*unstructured.Unstructured{testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.GatewayWithListeners("infra", "edge", "kgateway", nil)}, routes...)
	snap := viewSnapshot(t, nss, objs...)
	served := testutil.ServedUpstream()
	res := resolved("i", "v2.4.7", install.Scope{Mode: install.ScopeAll}, nil, testutil.Controller)
	dec := newDecodeCache()
	owned := install.Assign(snap, served, []install.Resolved{res}, dec).PerInstall[res.ID()]

	r := func(n string) objects.Ref { return routeRef("shop", n) }
	a := computed.AttachmentResult{Outcomes: []computed.RefOutcome{
		outcome(r("ok"), computed.OutcomeAccepted, "", r("ok"), gwRef),
		outcome(r("ok"), computed.OutcomeNoHostname, "", r("ok"), nsRef),
		outcome(r("rej"), computed.OutcomeNotAllowed, "", r("rej"), gwRef),
		outcome(r("status"), "", computed.UnknownNamespaceLabels, r("status")),
		outcome(r("stale"), "", computed.UnknownNamespaceLabels, r("stale")),
		outcome(r("denied"), "", computed.UnknownNamespaceLabels, r("denied")),
		outcome(r("nostatus"), "", computed.UnknownNamespaceLabels, r("nostatus")),
		outcome(r("mixed"), computed.OutcomeNotAllowed, "", r("mixed")),
		outcome(r("mixed"), "", computed.UnknownNamespaceLabels, r("mixed")),
		outcome(r("both"), computed.OutcomeAccepted, "", r("both"), gwRef),
		outcome(r("viahttp"), "", computed.UnknownNamespaceLabels, r("viahttp")),
	}}
	in := delegationInput(res, owned, snap, served, dec, a)
	got := map[objects.Ref]computed.Root{}
	for _, rt := range in.Roots {
		got[rt.Route] = rt
	}
	want := map[objects.Ref]computed.Root{
		r("ok"):       {Route: r("ok"), Deps: []objects.Ref{r("ok"), gwRef, nsRef}},
		r("status"):   {Route: r("status"), FromStatus: true, Deps: []objects.Ref{r("status"), gwRef}},
		r("both"):     {Route: r("both"), Deps: []objects.Ref{r("both"), gwRef}},
		r("viahttp"):  {Route: r("viahttp"), Unknown: true, Deps: []objects.Ref{r("viahttp")}},
		r("mixed"):    {Route: r("mixed"), Unknown: true, Deps: []objects.Ref{r("mixed")}},
		r("denied"):   {Route: r("denied"), Unknown: true, Deps: []objects.Ref{r("denied")}},
		r("stale"):    {Route: r("stale"), Unknown: true, Deps: []objects.Ref{r("stale")}},
		r("nostatus"): {Route: r("nostatus"), Unknown: true, Deps: []objects.Ref{r("nostatus")}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("roots\n got %+v\nwant %+v", got, want)
	}
	if len(in.Roots) != len(want) {
		t.Fatalf("roots %+v", in.Roots)
	}
}

// Out-of-scope HTTPRoutes are only in Unwatched; an unknown scope is flagged.
func TestDelegationInputScope(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	watched := map[string]string{"watched": "yes"}
	nss := []*corev1.Namespace{testutil.Namespace("infra", watched), testutil.Namespace("shop", watched), testutil.Namespace("other", nil)}
	child := testutil.HTTPRoute("shop", "child", []objects.ParentRef{testutil.HTTPRouteRef("shop", "web")},
		testutil.AcceptedBy(testutil.HTTPRouteRef("shop", "web"), testutil.Controller))
	objs := []*unstructured.Unstructured{testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.GatewayWithListeners("infra", "edge", "kgateway", nil),
		testutil.HTTPRoute("shop", "web", []objects.ParentRef{gw}),
		child,
		testutil.HTTPRoute("shop", "free", nil),
		testutil.HTTPRoute("other", "out", []objects.ParentRef{gw}),
	}
	snap := viewSnapshot(t, nss, objs...)
	served := testutil.ServedUpstream()
	scope := install.ParseSelectors(`[{"matchLabels":{"watched":"yes"}}]`)
	res := resolved("i", "v2.4.7", scope, nil, testutil.Controller)
	dec := newDecodeCache()
	owned := install.Assign(snap, served, []install.Resolved{res}, dec).PerInstall[res.ID()]

	in := delegationInput(res, owned, snap, served, dec, computed.AttachmentResult{})
	var names []string
	for _, r := range in.Routes {
		names = append(names, r.Ref.String())
		switch r.Ref.Name {
		case "web":
			if !r.Owned || !r.TopLevel || r.View == nil {
				t.Fatalf("web %+v", r)
			}
		case "child":
			if !r.Owned || r.TopLevel {
				t.Fatalf("child %+v", r)
			}
		case "free":
			if r.Owned || r.TopLevel {
				t.Fatalf("free %+v", r)
			}
		}
		if r.Generation != 1 {
			t.Fatalf("generation %+v", r)
		}
	}
	if want := []string{"shop/child", "shop/free", "shop/web"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("routes %v, want %v", names, want)
	}
	if want := map[objects.Ref]bool{routeRef("other", "out"): true}; !reflect.DeepEqual(in.Unwatched, want) {
		t.Fatalf("unwatched %v", in.Unwatched)
	}
	if in.ScopeUnknown || !reflect.DeepEqual(in.ControllerNames, res.ControllerNames) || in.Bounds != computed.DefaultBounds || in.Wildcard != res.Settings.WildcardNamespace {
		t.Fatalf("input %+v", in)
	}

	unknown := resolved("i", "v2.4.7", install.Scope{Mode: install.ScopeUnknown}, nil, testutil.Controller)
	if !delegationInput(unknown, owned, snap, served, dec, computed.AttachmentResult{}).ScopeUnknown {
		t.Fatal("an unknown scope must set ScopeUnknown")
	}
}
