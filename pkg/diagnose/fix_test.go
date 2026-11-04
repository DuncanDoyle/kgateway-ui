package diagnose_test

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/diagnose"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

// refsFinding is an attributed finding with code on shop/r whose one candidate is at path.
func refsFinding(code, explanation, path string, to objects.Ref, group *string) health.Finding {
	return health.Finding{Code: code, Resource: ref("HTTPRoute", "r"),
		Facts: &health.RefsEvidence{Explanation: explanation, Candidates: []health.RefLocation{{Path: path, Ref: to}}, Attributed: true, Group: group}}
}

// Design §7.3 and §7.7: no fix without one attributed ref, for a field the spec
// does not define, or for a wrong group discovery left no single answer for.
func TestFixNeedsOneSpecDefinedRef(t *testing.T) {
	core := ""
	store := objects.Ref{Kind: "Service", Namespace: "store", Name: "cart"}
	unattributed := refsFinding(health.CodeRefNotPermitted, health.RefExplainController, "spec.rules[0].backendRefs[0]", store, nil)
	unattributed.Facts.(*health.RefsEvidence).Attributed = false
	for name, f := range map[string]health.Finding{
		"not attributed":         unattributed,
		"CA certificate ref":     refsFinding(health.CodeRefNotPermitted, health.RefExplainController, "spec.tls.frontend.default.validation.caCertificateRefs[0]", store, nil),
		"013 controller message": refsFinding(health.CodeRefUnresolved, health.RefExplainController, "spec.rules[0].backendRefs[0]", store, nil),
		"no single group":        refsFinding(health.CodeRefUnresolved, health.RefExplainKindNotServed, "spec.rules[0].backendRefs[0]", store, nil),
		"other code":             refsFinding(health.CodeConditionFalse, health.RefExplainKindNotServed, "spec.rules[0].backendRefs[0]", store, &core),
	} {
		if fx := diagnose.Fix(&f); fx != nil {
			t.Errorf("%s: want no fix, got %+v", name, fx)
		}
	}
	mirror := refsFinding(health.CodeRefNotPermitted, health.RefExplainController, "spec.rules[0].backendRefs[0].filters[1].requestMirror.backendRef", store, nil)
	if fx := diagnose.Fix(&mirror); fx == nil || !fx.Create {
		t.Errorf("mirror backendRef: want a grant, got %+v", fx)
	}
}

// A long grant name is cut to a valid object name.
func TestGrantNameFitsObjectName(t *testing.T) {
	long := objects.Ref{Kind: "Secret", Namespace: "certs", Name: strings.Repeat("b", 253)}
	f := refsFinding(health.CodeRefNotPermitted, health.RefExplainController, "spec.rules[0].backendRefs[0]", long, nil)
	if fx := diagnose.Fix(&f); fx == nil || len(fx.Target.Name) > 253 || strings.HasSuffix(fx.Target.Name, "-") {
		t.Fatalf("grant %+v", fx)
	}
}

// noMetrics is a metrics source with no result.
type noMetrics struct{}

func (noMetrics) Get(string) (ctrlmetrics.Result, bool) { return ctrlmetrics.Result{}, false }

// E3: a caller outside the server builds the fix from a Compute result with
// the exported packages alone.
func TestFixFromComputeResult(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	route := testutil.WithRules(testutil.HTTPRoute("shop", "r", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}}),
		map[string]any{"backendRefs": []any{map[string]any{"group": "delegation.gateway.example", "kind": "Service", "name": "cart", "port": int64(80)}}})
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	objs := []runtime.Object{testutil.Namespace("kgw", nil), testutil.Namespace("infra", nil), testutil.Namespace("shop", nil), d,
		testutil.ControllerPod(d, "kgateway-0", true), testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.Gateway("infra", "edge", "kgateway", nil), route}
	served := testutil.ServedUpstream()
	dyn, kube := testutil.ClientsFor(t, served, objs...)
	det := install.UpstreamDetector{}
	c := cache.New(cache.Config{Dynamic: dyn, Kube: kube, Served: served, PodSelectors: []labels.Selector{det.ControllerPodSelector()},
		DeploymentSelectors: []labels.Selector{det.ControllerSelector(), det.ProxySelector()}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Start(ctx)
	cat := kinds.NewCatalog([]schema.GroupKind{{Kind: "Service"}, {Group: objects.GatewayAPIGroup, Kind: "HTTPRoute"}}, nil)
	e := engine.New(engine.Config{Snapshots: c, Served: served, Detectors: []install.Detector{det}, Scope: install.NamespaceSelectorScope{},
		Checks: health.Upstream(), Catalog: func() kinds.Catalog { return cat }, Metrics: noMetrics{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour)})
	m := e.Compute()
	for deadline := time.Now().Add(5 * time.Second); !m.Snapshot.Synced; m = e.Compute() {
		if time.Now().After(deadline) {
			t.Fatal("not synced")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var fx *diagnose.SuggestedFix
	for _, i := range m.Installs {
		for k := range i.Health.Findings {
			if f := &i.Health.Findings[k]; f.Code == health.CodeRefUnresolved {
				fx = diagnose.Fix(f)
			}
		}
	}
	if fx == nil || fx.Path != "spec.rules[0].backendRefs[0]" || *ref("HTTPRoute", "r") != fx.Target {
		t.Fatalf("fix %+v", fx)
	}
	var frag map[string]any
	if err := yaml.Unmarshal([]byte(fx.YAML), &frag); err != nil || frag["group"] != "" || frag["kind"] != "Service" || frag["name"] != "cart" {
		t.Fatalf("fragment %v (%v):\n%s", frag, err, fx.YAML)
	}
}
