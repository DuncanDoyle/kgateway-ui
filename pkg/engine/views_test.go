package engine

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/rulesets"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// resolved is an upstream install at version with scope, read from pod when set.
func resolved(id, version string, scope install.Scope, pod *corev1.Pod, controller string) install.Resolved {
	in := install.Install{CustomID: id, Namespace: "kgw", Name: id, Edition: "kgateway", Version: version,
		ControllerNames: []string{controller}, Container: "kgateway"}
	if pod != nil {
		in.Pods = []*corev1.Pod{pod}
	}
	return install.Resolved{Install: in, Scope: scope, Settings: install.ReadSettings(in)}
}

// viewSnapshot is a synced snapshot of objs with readable Namespaces nss.
func viewSnapshot(t *testing.T, nss []*corev1.Namespace, objs ...*unstructured.Unstructured) *cache.Snapshot {
	t.Helper()
	access := map[schema.GroupVersionResource]cache.Access{cache.NamespacesGVR: {OK: true}}
	for _, k := range testutil.ServedUpstream() {
		access[k.GVR()] = cache.Access{OK: true}
	}
	return &cache.Snapshot{Objects: testutil.ObjectsByGVR(t, objs...), Access: access, Namespaces: nss, Synced: true, TakenAt: testutil.Now}
}

func tcpRoute(ns, name string, parents ...objects.ParentRef) *unstructured.Unstructured {
	u := testutil.HTTPRoute(ns, name, parents)
	u.SetKind("TCPRoute")
	u.SetUID(types.UID("TCPRoute/" + ns + "/" + name))
	return u
}

func refsOf(in computed.AttachmentInput) (res, routes []string) {
	for _, r := range in.Resources {
		res = append(res, r.Ref.Kind+" "+r.Ref.String())
	}
	for _, r := range in.Routes {
		routes = append(routes, r.Ref.Kind+" "+r.Ref.String())
	}
	return res, routes
}

// attachmentInput takes owned Gateways and ListenerSets and the in-scope
// routes that name them; another install's Gateway and unwatched routes are absent.
func TestAttachmentInputScope(t *testing.T) {
	const ctlB = "example.com/other"
	listener := []objects.Listener{{Name: "http", Port: 80, Protocol: "HTTP", AllowedRoutes: objects.AllowedRoutes{Namespaces: objects.RouteNamespaces{From: "All"}}},
		{Name: "tcp", Port: 9000, Protocol: "TCP", AllowedRoutes: objects.AllowedRoutes{Namespaces: objects.RouteNamespaces{From: "All"}}}}
	gwA := testutil.GatewayWithListeners("infra", "a", "class-a", listener)
	gwB := testutil.GatewayWithListeners("infra", "b", "class-b", listener)
	// The ListenerSet's Accepted was observed at generation 1; its spec is at 2.
	ls := testutil.ListenerSet("infra", "ls", testutil.GwRef("infra", "a"), nil)
	ls.SetGeneration(2)
	refA, refLS := testutil.GwRef("infra", "a"), objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	objs := []*unstructured.Unstructured{
		testutil.GatewayClass("class-a", testutil.Controller, true), testutil.GatewayClass("class-b", ctlB, true), gwA, gwB, ls,
		testutil.HTTPRoute("shop", "in", []objects.ParentRef{refA, {Namespace: "infra", Name: "b"}}),
		testutil.HTTPRoute("shop", "via-ls", []objects.ParentRef{refLS}),
		testutil.HTTPRoute("other", "out", []objects.ParentRef{refA}),
		testutil.HTTPRoute("shop", "b-only", []objects.ParentRef{testutil.GwRef("infra", "b")}),
		tcpRoute("shop", "tcp", refA),
	}
	watched := map[string]string{"watched": "yes"}
	snap := viewSnapshot(t, []*corev1.Namespace{testutil.Namespace("infra", watched), testutil.Namespace("shop", watched), testutil.Namespace("other", nil)}, objs...)
	served := testutil.ServedUpstream()
	scope := install.ParseSelectors(`[{"matchLabels":{"watched":"yes"}}]`)
	for _, tc := range []struct {
		version string
		tcpExp  bool
	}{{"v2.4.7", false}, {"v2.3.11", true}} {
		a := resolved("a", tc.version, scope, nil, testutil.Controller)
		b := resolved("b", tc.version, install.Scope{Mode: install.ScopeAll}, nil, ctlB)
		asg := install.Assign(snap, served, []install.Resolved{a, b}, objects.Direct{})
		in := attachmentInput(a, asg.PerInstall["a"], snap, served, newDecodeCache())
		res, routes := refsOf(in)
		if want := []string{"Gateway infra/a", "ListenerSet infra/ls"}; !reflect.DeepEqual(res, want) {
			t.Fatalf("%s resources %v, want %v", tc.version, res, want)
		}
		if want := []string{"HTTPRoute shop/in", "HTTPRoute shop/via-ls", "TCPRoute shop/tcp"}; !reflect.DeepEqual(routes, want) {
			t.Fatalf("%s routes %v, want %v", tc.version, routes, want)
		}
		if r := in.Resources[1]; r.Acceptance != computed.NoStatus || r.Parent != refA.Ref() || r.Generation != 2 {
			t.Fatalf("ListenerSet %+v", r)
		}
		if in.Routes[2].ExperimentalOnly != tc.tcpExp || in.Routes[0].ExperimentalOnly {
			t.Fatalf("%s: TCPRoute experimental-only %v, want %v", tc.version, in.Routes[2].ExperimentalOnly, tc.tcpExp)
		}
		if r := in.Routes[0]; len(r.Written) != 2 || r.Written[1].Name != "b" || r.Written[1].Namespace != "infra" || r.Written[1].Kind != "" {
			t.Fatalf("written refs %+v", r.Written)
		}
		if !in.NamespacesOK || in.NamespaceLabels["shop"]["watched"] != "yes" || !reflect.DeepEqual(in.ControllerNames, []string{testutil.Controller}) {
			t.Fatalf("labels %v ok %v controllers %v", in.NamespaceLabels, in.NamespacesOK, in.ControllerNames)
		}
	}
}

// Design §9.7: no Pods or Deployments leave only the experimental setting
// unknown; objects without uid or resourceVersion give the same views.
func TestComputedWithoutPodsOrUIDs(t *testing.T) {
	listener := []objects.Listener{{Name: "http", Port: 80, Protocol: "HTTP", AllowedRoutes: objects.AllowedRoutes{Namespaces: objects.RouteNamespaces{From: "All"}}},
		{Name: "tcp", Port: 9000, Protocol: "TCP", AllowedRoutes: objects.AllowedRoutes{Namespaces: objects.RouteNamespaces{From: "All"}}}}
	ref := testutil.GwRef("infra", "edge")
	objs := func() []*unstructured.Unstructured {
		gw := testutil.GatewayWithListeners("infra", "edge", "kgateway", listener,
			testutil.Listener{Name: "http", Attached: 1, Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")}},
			testutil.Listener{Name: "tcp", Attached: 1, Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")}})
		return []*unstructured.Unstructured{testutil.GatewayClass("kgateway", testutil.Controller, true), gw,
			testutil.HTTPRoute("shop", "web", []objects.ParentRef{ref}, testutil.AcceptedBy(ref, testutil.Controller)),
			tcpRoute("shop", "tcp", ref)}
	}
	nss := []*corev1.Namespace{testutil.Namespace("infra", nil), testutil.Namespace("shop", nil)}
	served := testutil.ServedUpstream()
	sel := rulesets.Select("v2.3.11")
	views := func(res install.Resolved, us []*unstructured.Unstructured, dec objects.Decoder) *computed.Views {
		snap := viewSnapshot(t, nss, us...)
		owned := install.Assign(snap, served, []install.Resolved{res}, dec).PerInstall[res.ID()]
		return computeViews(nil, sel, res, owned, snap, served, dec, computed.DefaultBounds)
	}
	d := testutil.ControllerDeployment("kgw", "kgateway", image23, 1, 1)
	all := install.Scope{Mode: install.ScopeAll}
	withPod := resolved("i", "v2.3.11", all, testutil.ControllerPod(d, "p", true), testutil.Controller)
	bare := resolved("i", "v2.3.11", all, nil, testutil.Controller)
	if _, known := bare.Settings.Experimental.Bool(); known {
		t.Fatal("without Pods the experimental setting must be unknown")
	}
	known := views(withPod, objs(), newDecodeCache())
	unknown := views(bare, objs(), newDecodeCache())

	outcome := func(v *computed.Views, route string) computed.RefOutcome {
		for _, o := range v.Attachment.Outcomes {
			if o.Route.Name == route {
				return o
			}
		}
		t.Fatalf("no outcome for %s", route)
		return computed.RefOutcome{}
	}
	if o := outcome(known, "tcp"); o.Outcome != computed.OutcomeAccepted || o.Unknown != "" {
		t.Fatalf("known setting: tcp %+v", o)
	}
	if o := outcome(unknown, "tcp"); o.Unknown != computed.UnknownSetting {
		t.Fatalf("unknown setting: tcp %+v", o)
	}
	if a, b := outcome(known, "web"), outcome(unknown, "web"); !reflect.DeepEqual(a, b) {
		t.Fatalf("web differs:\n%+v\n%+v", a, b)
	}
	for _, c := range unknown.Attachment.Counts {
		if c.Listener == "http" && (c.Unknown != "" || c.Count != 1) {
			t.Fatalf("http count %+v", c)
		}
	}

	// Without uid and resourceVersion the decode cache is bypassed; nothing else changes.
	bareObjs := objs()
	for _, u := range bareObjs {
		u.SetUID("")
		u.SetResourceVersion("")
	}
	if got := views(withPod, bareObjs, newDecodeCache()); !reflect.DeepEqual(got, known) {
		t.Fatalf("views without uids differ:\n%+v\n%+v", got, known)
	}
}
