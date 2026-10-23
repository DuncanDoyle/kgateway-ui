// Package testutil builds fixture objects and fake clients for backend tests.
package testutil

import (
	"fmt"
	"maps"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Now is the fixed clock used by fixtures and by tests that compare times.
var Now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

// Old is a creation time well past every grace period.
var Old = metav1.NewTime(Now.Add(-time.Hour))

// Controller is the upstream kgateway controller name.
const Controller = "kgateway.dev/kgateway"

// Cond builds a condition observed at generation 1.
func Cond(typ string, status metav1.ConditionStatus, reason string) metav1.Condition {
	return metav1.Condition{Type: typ, Status: status, Reason: reason, ObservedGeneration: 1, LastTransitionTime: Old}
}

func conds(cs []metav1.Condition) []any {
	out := make([]any, 0, len(cs))
	for _, c := range cs {
		out = append(out, map[string]any{"type": c.Type, "status": string(c.Status), "reason": c.Reason,
			"message": c.Message, "observedGeneration": c.ObservedGeneration, "lastTransitionTime": c.LastTransitionTime.UTC().Format(time.RFC3339)})
	}
	return out
}

func obj(apiVersion, kind, ns, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{"apiVersion": apiVersion, "kind": kind}}
	u.SetNamespace(ns)
	u.SetName(name)
	u.SetGeneration(1)
	u.SetUID(types.UID(kind + "/" + ns + "/" + name))
	u.SetCreationTimestamp(Old)
	return u
}

// Namespace builds a namespace with labels.
func Namespace(name string, lbls map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: lbls, UID: types.UID("ns/" + name)}}
}

// GatewayClass builds a GatewayClass with an Accepted condition.
func GatewayClass(name, controller string, accepted bool) *unstructured.Unstructured {
	u := obj("gateway.networking.k8s.io/v1", "GatewayClass", "", name)
	st := metav1.ConditionTrue
	if !accepted {
		st = metav1.ConditionFalse
	}
	u.Object["spec"] = map[string]any{"controllerName": controller}
	u.Object["status"] = map[string]any{"conditions": conds([]metav1.Condition{Cond("Accepted", st, "Accepted")})}
	return u
}

// Listener is a listener status entry for Gateway fixtures.
type Listener struct {
	Name     string
	Attached int32
	Conds    []metav1.Condition
}

// Gateway builds a Gateway. With no conditions given it is Accepted and Programmed.
func Gateway(ns, name, class string, cs []metav1.Condition, listeners ...Listener) *unstructured.Unstructured {
	u := obj("gateway.networking.k8s.io/v1", "Gateway", ns, name)
	if cs == nil {
		cs = []metav1.Condition{Cond("Accepted", metav1.ConditionTrue, "Accepted"), Cond("Programmed", metav1.ConditionTrue, "Programmed")}
	}
	if len(listeners) == 0 {
		listeners = []Listener{{Name: "http", Conds: []metav1.Condition{Cond("Accepted", metav1.ConditionTrue, "Accepted"), Cond("Programmed", metav1.ConditionTrue, "Programmed")}}}
	}
	ls := []any{}
	specLs := []any{}
	for _, l := range listeners {
		ls = append(ls, map[string]any{"name": l.Name, "attachedRoutes": int64(l.Attached), "conditions": conds(l.Conds), "supportedKinds": []any{}})
		specLs = append(specLs, map[string]any{"name": l.Name, "port": int64(8080), "protocol": "HTTP"})
	}
	u.Object["spec"] = map[string]any{"gatewayClassName": class, "listeners": specLs}
	u.Object["status"] = map[string]any{"conditions": conds(cs), "listeners": ls, "addresses": []any{map[string]any{"type": "IPAddress", "value": "10.0.0.1"}}}
	return u
}

// AddSpecListener adds a listener to gw's spec without a status block, as when
// the controller has not processed it yet.
func AddSpecListener(gw *unstructured.Unstructured, name string) {
	spec := gw.Object["spec"].(map[string]any)
	spec["listeners"] = append(spec["listeners"].([]any), map[string]any{"name": name, "port": int64(8443), "protocol": "HTTP"})
}

// ListenerSet builds a ListenerSet attached to parent. With no conditions
// given it is Accepted and Programmed; with no listeners it has one accepted
// listener "extra". A listener with nil Conds has no status block.
func ListenerSet(ns, name string, parent objects.ParentRef, cs []metav1.Condition, listeners ...Listener) *unstructured.Unstructured {
	u := obj("gateway.networking.k8s.io/v1", "ListenerSet", ns, name)
	if cs == nil {
		cs = []metav1.Condition{Cond("Accepted", metav1.ConditionTrue, "Accepted"), Cond("Programmed", metav1.ConditionTrue, "Programmed")}
	}
	if len(listeners) == 0 {
		listeners = []Listener{{Name: "extra", Conds: []metav1.Condition{Cond("Accepted", metav1.ConditionTrue, "Accepted"), Cond("Programmed", metav1.ConditionTrue, "Programmed")}}}
	}
	ls, specLs := []any{}, []any{}
	for _, l := range listeners {
		if l.Conds != nil {
			ls = append(ls, map[string]any{"name": l.Name, "attachedRoutes": int64(l.Attached), "conditions": conds(l.Conds), "supportedKinds": []any{}})
		}
		specLs = append(specLs, map[string]any{"name": l.Name, "port": int64(8081), "protocol": "HTTP"})
	}
	u.Object["spec"] = map[string]any{"parentRef": refMap(parent), "listeners": specLs}
	u.Object["status"] = map[string]any{"conditions": conds(cs), "listeners": ls}
	return u
}

// RouteParent is a status.parents entry for route fixtures.
type RouteParent struct {
	Ref        objects.ParentRef
	Controller string
	Conds      []metav1.Condition
}

func refMap(p objects.ParentRef) map[string]any {
	m := map[string]any{"name": p.Name}
	if p.Group != "" {
		m["group"] = p.Group
	}
	if p.Kind != "" {
		m["kind"] = p.Kind
	}
	if p.Namespace != "" {
		m["namespace"] = p.Namespace
	}
	if p.SectionName != "" {
		m["sectionName"] = p.SectionName
	}
	if p.Port != 0 {
		m["port"] = int64(p.Port)
	}
	return m
}

// GwRef is a parentRef to a Gateway.
func GwRef(ns, name string) objects.ParentRef {
	return objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: ns, Name: name}
}

// HTTPRoute builds an HTTPRoute with the given parentRefs and status.parents.
func HTTPRoute(ns, name string, parents []objects.ParentRef, status ...RouteParent) *unstructured.Unstructured {
	u := obj("gateway.networking.k8s.io/v1", "HTTPRoute", ns, name)
	prs := []any{}
	for _, p := range parents {
		prs = append(prs, refMap(p))
	}
	st := []any{}
	for _, s := range status {
		st = append(st, map[string]any{"parentRef": refMap(s.Ref), "controllerName": s.Controller, "conditions": conds(s.Conds)})
	}
	u.Object["spec"] = map[string]any{"parentRefs": prs}
	u.Object["status"] = map[string]any{"parents": st}
	return u
}

// AcceptedBy is the status.parents entry of a healthy route attached to p by controller.
func AcceptedBy(p objects.ParentRef, controller string) RouteParent {
	return RouteParent{Ref: p, Controller: controller, Conds: []metav1.Condition{
		Cond("Accepted", metav1.ConditionTrue, "Accepted"), Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs"),
		Cond("kgateway.dev/Programmed", metav1.ConditionTrue, "Programmed")}}
}

// PolicyAncestor is a status.ancestors entry for policy fixtures.
type PolicyAncestor struct {
	Ref        objects.ParentRef
	Controller string
	Conds      []metav1.Condition
}

// Policy builds a policy of any kind with status.ancestors.
func Policy(apiVersion, kind, ns, name string, ancestors ...PolicyAncestor) *unstructured.Unstructured {
	u := obj(apiVersion, kind, ns, name)
	as := []any{}
	for _, a := range ancestors {
		as = append(as, map[string]any{"ancestorRef": refMap(a.Ref), "controllerName": a.Controller, "conditions": conds(a.Conds)})
	}
	u.Object["spec"] = map[string]any{}
	u.Object["status"] = map[string]any{"ancestors": as}
	return u
}

// TrafficPolicy builds a kgateway TrafficPolicy with status.ancestors.
func TrafficPolicy(ns, name string, ancestors ...PolicyAncestor) *unstructured.Unstructured {
	return Policy("gateway.kgateway.dev/v1alpha1", "TrafficPolicy", ns, name, ancestors...)
}

// BackendTLSPolicy builds a Gateway API BackendTLSPolicy with status.ancestors.
func BackendTLSPolicy(ns, name string, ancestors ...PolicyAncestor) *unstructured.Unstructured {
	return Policy("gateway.networking.k8s.io/v1", "BackendTLSPolicy", ns, name, ancestors...)
}

// ControllerDeployment builds a kgateway controller Deployment with the upstream
// labels. The Pod template carries the upstream v2.3/v2.4 Pod labels exactly:
// the selector labels plus app.kubernetes.io/component=controller.
func ControllerDeployment(ns, name, image string, desired, ready int32, env ...corev1.EnvVar) *appsv1.Deployment {
	lbls := map[string]string{"kgateway": "kgateway", "app.kubernetes.io/component": "controller",
		"app.kubernetes.io/name": "kgateway", "app.kubernetes.io/instance": name, "app.kubernetes.io/version": "2.4.7"}
	sel := map[string]string{"kgateway": "kgateway", "app.kubernetes.io/name": "kgateway", "app.kubernetes.io/instance": name}
	podLbls := map[string]string{"kgateway": "kgateway", "app.kubernetes.io/name": "kgateway", "app.kubernetes.io/instance": name,
		"app.kubernetes.io/component": "controller"}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: lbls, UID: types.UID("deploy/" + ns + "/" + name), CreationTimestamp: Old},
		Spec: appsv1.DeploymentSpec{
			Replicas: &desired,
			Selector: &metav1.LabelSelector{MatchLabels: sel},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: podLbls}, Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: "kgateway", Image: image, Env: env,
				Ports: []corev1.ContainerPort{{Name: "metrics", ContainerPort: 9092}},
			}}}},
		},
		Status: appsv1.DeploymentStatus{Replicas: desired, ReadyReplicas: ready},
	}
}

// ControllerPod builds a Pod of d. ready controls its Ready condition and phase.
func ControllerPod(d *appsv1.Deployment, name string, ready bool) *corev1.Pod {
	phase, st := corev1.PodRunning, corev1.ConditionTrue
	if !ready {
		phase, st = corev1.PodPending, corev1.ConditionFalse
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: d.Namespace, Name: name, Labels: maps.Clone(d.Spec.Template.Labels), UID: types.UID("pod/" + d.Namespace + "/" + name)},
		Spec:       d.Spec.Template.Spec,
		Status:     corev1.PodStatus{Phase: phase, PodIP: "10.1.0.5", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: st}}},
	}
}

// ProxyDeployment builds the proxy Deployment kgateway creates for a Gateway.
func ProxyDeployment(gwNS, gwName string, desired, ready int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: gwNS, Name: gwName, UID: types.UID("proxy/" + gwNS + "/" + gwName),
			Labels: map[string]string{"app.kubernetes.io/managed-by": "kgateway", "app.kubernetes.io/component": "proxy",
				"gateway.networking.k8s.io/gateway-name": gwName},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "gateway.networking.k8s.io/v1", Kind: "Gateway", Name: gwName, UID: types.UID("Gateway/" + gwNS + "/" + gwName)}}},
		Spec:   appsv1.DeploymentSpec{Replicas: &desired},
		Status: appsv1.DeploymentStatus{Replicas: desired, ReadyReplicas: ready},
	}
}

// Env is a shorthand for a literal env var.
func Env(name, value string) corev1.EnvVar { return corev1.EnvVar{Name: name, Value: value} }

// PodList converts pods for use in a Snapshot.
func PodList(ps ...*corev1.Pod) []*corev1.Pod { return ps }

// GatewayParameters builds a kgateway GatewayParameters. selfManaged sets
// spec.selfManaged; otherwise it carries only a spec.kube block.
func GatewayParameters(ns, name string, selfManaged bool) *unstructured.Unstructured {
	u := obj("gateway.kgateway.dev/v1alpha1", "GatewayParameters", ns, name)
	if selfManaged {
		u.Object["spec"] = map[string]any{"selfManaged": map[string]any{}}
	} else {
		u.Object["spec"] = map[string]any{"kube": map[string]any{"deployment": map[string]any{"replicas": int64(1)}}}
	}
	return u
}

// SetClassParameters sets class's spec.parametersRef.
func SetClassParameters(class *unstructured.Unstructured, group, kind, ns, name string) {
	class.Object["spec"].(map[string]any)["parametersRef"] = map[string]any{
		"group": group, "kind": kind, "namespace": ns, "name": name}
}

// SetGatewayParameters sets gw's spec.infrastructure.parametersRef.
func SetGatewayParameters(gw *unstructured.Unstructured, group, kind, name string) {
	gw.Object["spec"].(map[string]any)["infrastructure"] = map[string]any{
		"parametersRef": map[string]any{"group": group, "kind": kind, "name": name}}
}

// ScaleTeamLabel is the label of every fourth scale namespace; each scale
// Gateway has one Selector listener that selects it.
var ScaleTeamLabel = map[string]string{"team": "web"}

// ScaleLabelled reports whether scale namespace i carries ScaleTeamLabel.
func ScaleLabelled(i int) bool { return i%4 == 0 }

// ScaleNamespaces builds the n namespaces ns-000 … of a scale world, every
// fourth one labelled with ScaleTeamLabel.
func ScaleNamespaces(n int) []*corev1.Namespace {
	out := make([]*corev1.Namespace, 0, n)
	for i := range n {
		var lbls map[string]string
		if ScaleLabelled(i) {
			lbls = maps.Clone(ScaleTeamLabel)
		}
		out = append(out, Namespace(ScaleNamespace(i), lbls))
	}
	return out
}

// ScaleNamespace is the name of scale namespace i.
func ScaleNamespace(i int) string { return fmt.Sprintf("ns-%03d", i) }
