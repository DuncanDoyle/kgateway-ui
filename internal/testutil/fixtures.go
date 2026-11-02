// Package testutil builds fixture objects and fake clients for backend tests.
package testutil

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
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
// Its listeners admit routes from every namespace.
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
		specLs = append(specLs, map[string]any{"name": l.Name, "port": int64(8080), "protocol": "HTTP",
			"allowedRoutes": map[string]any{"namespaces": map[string]any{"from": "All"}}})
	}
	u.Object["spec"] = map[string]any{"gatewayClassName": class, "listeners": specLs}
	u.Object["status"] = map[string]any{"conditions": conds(cs), "listeners": ls, "addresses": []any{map[string]any{"type": "IPAddress", "value": "10.0.0.1"}}}
	return u
}

// GatewayWithListeners builds an Accepted, Programmed Gateway whose spec
// listeners are spec, as given, and whose status listeners are status.
func GatewayWithListeners(ns, name, class string, spec []objects.Listener, status ...Listener) *unstructured.Unstructured {
	u := Gateway(ns, name, class, nil, Listener{Name: "placeholder"})
	specLs := make([]any, 0, len(spec))
	for _, l := range spec {
		specLs = append(specLs, listenerSpec(l))
	}
	ls := make([]any, 0, len(status))
	for _, l := range status {
		ls = append(ls, map[string]any{"name": l.Name, "attachedRoutes": int64(l.Attached), "conditions": conds(l.Conds), "supportedKinds": []any{}})
	}
	u.Object["spec"].(map[string]any)["listeners"] = specLs
	u.Object["status"].(map[string]any)["listeners"] = ls
	return u
}

// listenerSpec writes l as a spec listener; zero fields are omitted.
func listenerSpec(l objects.Listener) map[string]any {
	m := map[string]any{"name": l.Name, "protocol": l.Protocol}
	if l.Port != 0 {
		m["port"] = int64(l.Port)
	}
	if l.Hostname != "" {
		m["hostname"] = l.Hostname
	}
	if l.TLS != nil {
		m["tls"] = map[string]any{"mode": l.TLS.Mode}
	}
	ar := map[string]any{}
	nsm := map[string]any{}
	if from := l.AllowedRoutes.Namespaces.From; from != "" {
		nsm["from"] = from
	}
	if sel := l.AllowedRoutes.Namespaces.Selector; sel != nil {
		s := map[string]any{}
		if len(sel.MatchLabels) > 0 {
			ml := map[string]any{}
			for k, v := range sel.MatchLabels {
				ml[k] = v
			}
			s["matchLabels"] = ml
		}
		if len(sel.MatchExpressions) > 0 {
			es := make([]any, 0, len(sel.MatchExpressions))
			for _, e := range sel.MatchExpressions {
				vs := make([]any, 0, len(e.Values))
				for _, v := range e.Values {
					vs = append(vs, v)
				}
				es = append(es, map[string]any{"key": e.Key, "operator": string(e.Operator), "values": vs})
			}
			s["matchExpressions"] = es
		}
		nsm["selector"] = s
	}
	if len(nsm) > 0 {
		ar["namespaces"] = nsm
	}
	if l.AllowedRoutes.Kinds != nil {
		ks := make([]any, 0, len(l.AllowedRoutes.Kinds))
		for _, k := range l.AllowedRoutes.Kinds {
			ks = append(ks, map[string]any{"group": k.Group, "kind": k.Kind})
		}
		ar["kinds"] = ks
	}
	if len(ar) > 0 {
		m["allowedRoutes"] = ar
	}
	return m
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

// HTTPRouteRef is a parentRef to an HTTPRoute.
func HTTPRouteRef(ns, name string) objects.ParentRef {
	return objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: ns, Name: name}
}

// Rule builds an HTTPRoute rule with the given matches; with none, matches is omitted.
func Rule(matches ...map[string]any) map[string]any {
	r := map[string]any{}
	if len(matches) > 0 {
		ms := make([]any, 0, len(matches))
		for _, m := range matches {
			ms = append(ms, m)
		}
		r["matches"] = ms
	}
	return r
}

// DelegateTo is a backendRef that delegates to the HTTPRoute ns/name.
func DelegateTo(ns, name string) map[string]any {
	return map[string]any{"group": objects.GatewayAPIGroup, "kind": "HTTPRoute", "namespace": ns, "name": name}
}

// DelegateWildcard is a backendRef that delegates to every HTTPRoute in ns.
func DelegateWildcard(ns string) map[string]any { return DelegateTo(ns, "*") }

// DelegateLabel is a backendRef that delegates to the HTTPRoutes in ns
// labelled with the delegation label value label.
func DelegateLabel(ns, label string) map[string]any {
	return map[string]any{"group": "delegation.kgateway.dev", "kind": "label", "namespace": ns, "name": label}
}

// WithRules sets u's spec.rules and returns u.
func WithRules(u *unstructured.Unstructured, rules ...map[string]any) *unstructured.Unstructured {
	rs := make([]any, 0, len(rules))
	for _, r := range rules {
		rs = append(rs, r)
	}
	spec(u)["rules"] = rs
	return u
}

// WithHostnames sets u's spec.hostnames and returns u.
func WithHostnames(u *unstructured.Unstructured, h ...string) *unstructured.Unstructured {
	hs := make([]any, 0, len(h))
	for _, s := range h {
		hs = append(hs, s)
	}
	spec(u)["hostnames"] = hs
	return u
}

func spec(u *unstructured.Unstructured) map[string]any {
	s, ok := u.Object["spec"].(map[string]any)
	if !ok {
		s = map[string]any{}
		u.Object["spec"] = s
	}
	return s
}

// Backend builds a kgateway Backend of type Static. With no conditions given
// it is Accepted.
func Backend(ns, name string, cs []metav1.Condition) *unstructured.Unstructured {
	u := obj("gateway.kgateway.dev/v1alpha1", "Backend", ns, name)
	if cs == nil {
		cs = []metav1.Condition{Cond("Accepted", metav1.ConditionTrue, "Accepted")}
	}
	u.Object["spec"] = map[string]any{"type": "Static", "static": map[string]any{"hosts": []any{map[string]any{"host": "example.com", "port": int64(80)}}}}
	u.Object["status"] = map[string]any{"conditions": conds(cs)}
	return u
}

// ProxyPod builds Pod number i of the proxy Deployment of a Gateway, Ready,
// with restarts container restarts.
func ProxyPod(gwNS, gwName string, i int, suffix string, restarts int32) *corev1.Pod {
	name := fmt.Sprintf("%s-%s-%d", gwName, suffix, i)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: gwNS, Name: name, UID: types.UID("pod/" + gwNS + "/" + name), CreationTimestamp: Old,
			Labels: map[string]string{"app.kubernetes.io/managed-by": "kgateway", "app.kubernetes.io/component": "proxy",
				"gateway.networking.k8s.io/gateway-name": gwName},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: gwName + "-" + suffix}}},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "envoy", Image: "cr.kgateway.dev/kgateway-dev/envoy-wrapper:v2.4.7"}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.2.0.1",
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "envoy", Ready: true, RestartCount: restarts}}},
	}
}

// WarningEvent builds a Warning Event about a Pod, last seen at at.
func WarningEvent(pod *corev1.Pod, name, reason string, at time.Time) *corev1.Event {
	return &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: pod.Namespace, Name: name, UID: types.UID("event/" + pod.Namespace + "/" + name)},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: pod.Namespace, Name: pod.Name, UID: pod.UID},
		Type:           corev1.EventTypeWarning, Reason: reason, Count: 1, LastTimestamp: metav1.NewTime(at),
	}
}

// Service builds a ClusterIP Service with one port.
func Service(ns, name string, lbls map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: lbls, UID: types.UID("svc/" + ns + "/" + name), CreationTimestamp: Old},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 80}}, Selector: map[string]string{"app": name}},
	}
}

// EndpointSlice builds a slice of service svc with one endpoint per ready
// entry, each Ready as given.
func EndpointSlice(ns, name, svc string, ready ...bool) *discoveryv1.EndpointSlice {
	es := &discoveryv1.EndpointSlice{
		ObjectMeta:  metav1.ObjectMeta{Namespace: ns, Name: name, UID: types.UID("slice/" + ns + "/" + name), Labels: map[string]string{discoveryv1.LabelServiceName: svc}},
		AddressType: discoveryv1.AddressTypeIPv4,
	}
	for i, r := range ready {
		es.Endpoints = append(es.Endpoints, discoveryv1.Endpoint{Addresses: []string{fmt.Sprintf("10.3.0.%d", i+1)}, Conditions: discoveryv1.EndpointConditions{Ready: &r}})
	}
	return es
}

// CRDMetadata builds the metadata a metadata client returns for a Gateway API
// CRD: name, bundle-version and channel annotations.
func CRDMetadata(name, bundleVersion, channel string) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{APIVersion: "apiextensions.k8s.io/v1", Kind: "CustomResourceDefinition"},
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID("crd/" + name), CreationTimestamp: Old,
			Annotations: map[string]string{"gateway.networking.k8s.io/bundle-version": bundleVersion, "gateway.networking.k8s.io/channel": channel}},
	}
}

// ScaleShape is the size of a scale tier's typed Phase 3 objects; package scale
// fills it from its Tier.
type ScaleShape struct {
	Gateways, Namespaces                int
	Services, ReferencedServices        int
	EndpointSlices, ProxyPodsPerGateway int
	WarningEvents, GatewayAPICRDs       int
	// Worst makes every Gateway reject, 5 % of referenced Services lack a ready
	// endpoint and every proxy Pod restart once.
	Worst bool
}

// ScaleRejectEvery makes a seeded 1 in ScaleRejectEvery Gateways of a
// non-worst shape reject: 5 %.
const ScaleRejectEvery = 20

// ScaleGateway is the name of scale Gateway i.
func ScaleGateway(i int) string { return fmt.Sprintf("gw-%03d", i) }

// ScaleNoReadyEvery makes every ScaleNoReadyEvery-th referenced Service of a
// worst-case shape lack a ready endpoint: 5 %.
const ScaleNoReadyEvery = 20

// ScaleReferencedLabel is "true" on the Services a Backend or route names.
const ScaleReferencedLabel = "scale/referenced"

// scaleRand seeds a Phase 3 generator: equal seeds give equal objects.
func scaleRand(seed int64, stream uint64) *rand.Rand {
	return rand.New(rand.NewPCG(uint64(seed), stream)) //nolint:gosec // deterministic test data
}

func scalePodHash(r *rand.Rand) string { return fmt.Sprintf("%010x", r.Uint64()&0xffffffffff) }

// ScaleProxyPods returns ProxyPodsPerGateway Ready proxy Pods for each
// Gateway, in the Gateway's namespace. Under Worst each restarted once.
func ScaleProxyPods(t ScaleShape, seed int64) []*corev1.Pod {
	r := scaleRand(seed, 0x90d5)
	var restarts int32
	if t.Worst {
		restarts = 1
	}
	out := make([]*corev1.Pod, 0, t.Gateways*t.ProxyPodsPerGateway)
	for g := range t.Gateways {
		hash := scalePodHash(r)
		for i := range t.ProxyPodsPerGateway {
			out = append(out, ProxyPod(ScaleNamespace(g%t.Namespaces), ScaleGateway(g), i, hash, restarts))
		}
	}
	return out
}

var scaleEventReasons = []string{"Unhealthy", "BackOff", "FailedMount", "Killing", "FailedScheduling"}

// ScaleEvents returns WarningEvents Warning Events, spread round-robin over
// the proxy Pods, with seeded reasons and times in the hour before Now.
func ScaleEvents(t ScaleShape, seed int64) []*corev1.Event {
	r := scaleRand(seed, 0xe7e47)
	pods := ScaleProxyPods(t, seed)
	out := make([]*corev1.Event, 0, t.WarningEvents)
	for i := range t.WarningEvents {
		at := Now.Add(-time.Duration(r.IntN(3600)) * time.Second)
		out = append(out, WarningEvent(pods[i%len(pods)], fmt.Sprintf("ev-%05d", i), scaleEventReasons[r.IntN(len(scaleEventReasons))], at))
	}
	return out
}

// svcName names Service i: the first ReferencedServices are "ref-", the rest "svc-".
func scaleSvcName(t ScaleShape, i int) string {
	if i < t.ReferencedServices {
		return fmt.Sprintf("ref-%05d", i)
	}
	return fmt.Sprintf("svc-%05d", i)
}

func scaleSvcNS(t ScaleShape, i int) string { return ScaleNamespace(i % t.Namespaces) }

// ScaleServices returns Services Services over the namespaces, the first
// ReferencedServices labelled ScaleReferencedLabel.
func ScaleServices(t ScaleShape, _ int64) []*corev1.Service {
	out := make([]*corev1.Service, 0, t.Services)
	for i := range t.Services {
		var lbls map[string]string
		if i < t.ReferencedServices {
			lbls = map[string]string{ScaleReferencedLabel: "true"}
		}
		out = append(out, Service(scaleSvcNS(t, i), scaleSvcName(t, i), lbls))
	}
	return out
}

// ScaleEndpointSlices returns EndpointSlices slices: one per Service, the
// surplus a second slice of referenced Services in turn. Endpoints are Ready,
// except those of every ScaleNoReadyEvery-th referenced Service under Worst.
func ScaleEndpointSlices(t ScaleShape, seed int64) []*discoveryv1.EndpointSlice {
	r := scaleRand(seed, 0x511ce)
	out := make([]*discoveryv1.EndpointSlice, 0, t.EndpointSlices)
	for k := range t.EndpointSlices {
		i := k
		if k >= t.Services {
			i = (k - t.Services) % t.ReferencedServices
		}
		ready := !t.Worst || i >= t.ReferencedServices || i%ScaleNoReadyEvery != 0
		n := 1 + r.IntN(3)
		rs := make([]bool, n)
		for j := range rs {
			rs[j] = ready
		}
		out = append(out, EndpointSlice(scaleSvcNS(t, i), fmt.Sprintf("%s-%d", scaleSvcName(t, i), k), scaleSvcName(t, i), rs...))
	}
	return out
}

var scaleCRDNames = []string{"gatewayclasses", "gateways", "httproutes", "grpcroutes", "referencegrants",
	"backendtlspolicies", "listenersets", "tcproutes", "tlsroutes", "udproutes"}

// ScaleCRDs returns the metadata of GatewayAPICRDs Gateway API CRDs.
func ScaleCRDs(t ScaleShape) []*metav1.PartialObjectMetadata {
	out := make([]*metav1.PartialObjectMetadata, 0, t.GatewayAPICRDs)
	for i := range t.GatewayAPICRDs {
		n := fmt.Sprintf("extra%d", i)
		if i < len(scaleCRDNames) {
			n = scaleCRDNames[i]
		}
		out = append(out, CRDMetadata(n+".gateway.networking.k8s.io", "v1.4.1", "standard"))
	}
	return out
}

// ScaleMetrics returns a controller /metrics capture in the shape of the
// rejects captures of package ctrlmetrics: a managed-resource gauge per
// Gateway and xDS reject series for the rejecting ones. Under Worst every
// Gateway rejects; otherwise a seeded 1 in ScaleRejectEvery does.
func ScaleMetrics(t ScaleShape, seed int64) []byte {
	r := scaleRand(seed, 0x4e7c5)
	const cluster, route = "envoy.config.cluster.v3.Cluster", "envoy.config.route.v3.RouteConfiguration"
	var b strings.Builder
	b.WriteString("# HELP kgateway_envoy_xds_rejects_active Active xDS rejects.\n# TYPE kgateway_envoy_xds_rejects_active gauge\n")
	var rej []int
	for g := range t.Gateways {
		if t.Worst || r.IntN(ScaleRejectEvery) == 0 {
			rej = append(rej, g)
		}
	}
	types := map[int][]string{}
	for _, g := range rej {
		types[g] = []string{cluster}
		if r.IntN(2) == 0 {
			types[g] = append(types[g], route)
		}
	}
	series := func(name string, val func(int) int) {
		for _, g := range rej {
			for _, u := range types[g] {
				fmt.Fprintf(&b, "%s{gateway_name=%q,gateway_namespace=%q,type_url=%q} %d\n", name, ScaleGateway(g), ScaleNamespace(g%t.Namespaces), u, val(g))
			}
		}
	}
	series("kgateway_envoy_xds_rejects_active", func(int) int { return 1 })
	b.WriteString("# HELP kgateway_envoy_xds_rejects_total Total xDS rejects.\n# TYPE kgateway_envoy_xds_rejects_total counter\n")
	series("kgateway_envoy_xds_rejects_total", func(int) int { return 2 + r.IntN(3) })
	b.WriteString("# HELP kgateway_resources_managed Managed resources.\n# TYPE kgateway_resources_managed gauge\n")
	for g := range t.Gateways {
		fmt.Fprintf(&b, "kgateway_resources_managed{namespace=%q,parent=%q,resource=\"Gateway\"} 1\n", ScaleNamespace(g%t.Namespaces), ScaleGateway(g))
	}
	return []byte(b.String())
}
