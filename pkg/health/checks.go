package health

import (
	"fmt"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Upstream returns the Phase 1 checks (design §5.2).
func Upstream() []Check {
	return []Check{cacheCheck{}, scopeCheck{}, controllerCheck{}, conditionsCheck{}, proxyCheck{}, xdsCheck{}, unwatchedCheck{}, ambiguityCheck{}, verifyCheck{}}
}

func ref(u *unstructured.Unstructured) *objects.Ref { r := objects.RefOf(u); return &r }

func since(c metav1.Condition) *time.Time {
	if c.LastTransitionTime.IsZero() {
		return nil
	}
	t := c.LastTransitionTime.Time
	return &t
}

func ok(name string, required bool, at time.Time) CheckResult {
	return CheckResult{Name: name, OK: true, Required: required, CollectedAt: at}
}

func unavailable(in Input, name, why string) ([]Finding, CheckResult) {
	return []Finding{{Code: CodeSignalUnavailable, Slug: "signal-unavailable", Severity: SeverityInfo, Install: in.Install.ID(),
			Message: "Could not check " + name, Detail: why}},
		CheckResult{Name: name, Required: true, CollectedAt: in.Now, Error: why}
}

// cacheCheck: the snapshot is synced and every served kind, plus Namespaces,
// Pods and Deployments, is currently readable.
type cacheCheck struct{}

// Name implements Check.
func (cacheCheck) Name() string { return "cluster access" }

// Run implements Check: one KGWUI-008 per kind that cannot be read.
func (c cacheCheck) Run(in Input) ([]Finding, CheckResult) {
	if !in.Snapshot.Synced {
		return unavailable(in, c.Name(), "cache not synced")
	}
	var fs []Finding
	for gvr, a := range in.Snapshot.Access {
		if !a.OK {
			fs = append(fs, Finding{Code: CodeSignalUnavailable, Slug: "signal-unavailable", Severity: SeverityInfo, Install: in.Install.ID(),
				Message: "No access to " + resourceName(gvr), Detail: a.Err})
		}
	}
	if len(fs) > 0 {
		return fs, CheckResult{Name: c.Name(), Required: true, CollectedAt: in.Snapshot.TakenAt, Error: fmt.Sprintf("%d kinds not readable", len(fs))}
	}
	return nil, ok(c.Name(), true, in.Snapshot.TakenAt)
}

// resourceName returns "resource.group", or "resource" for the core group.
func resourceName(gvr schema.GroupVersionResource) string {
	if gvr.Group == "" {
		return gvr.Resource
	}
	return gvr.Resource + "." + gvr.Group
}

// scopeCheck: the install's scope could be determined.
type scopeCheck struct{}

// Name implements Check.
func (scopeCheck) Name() string { return "controller scope" }

// Run implements Check: an unknown scope (for example a selector env set
// through valueFrom) is KGWUI-008 with the scope's error as detail.
func (c scopeCheck) Run(in Input) ([]Finding, CheckResult) {
	if in.Install.Scope.Mode == install.ScopeUnknown {
		return unavailable(in, c.Name(), in.Install.Scope.Err)
	}
	return nil, ok(c.Name(), true, in.Now)
}

// controllerCheck: KGWUI-005.
type controllerCheck struct{}

// Name implements Check.
func (controllerCheck) Name() string { return "controller pods" }

// Run implements Check.
func (c controllerCheck) Run(in Input) ([]Finding, CheckResult) {
	d := in.Install.Deployment
	if f, bad := notReady(d, CodeControllerNotReady, "controller-not-ready", "Controller", in.Install.ID()); bad {
		f.Resource = &objects.Ref{Group: "apps", Kind: "Deployment", Namespace: d.Namespace, Name: d.Name}
		return []Finding{f}, ok(c.Name(), true, in.Now)
	}
	return nil, ok(c.Name(), true, in.Now)
}

func notReady(d *appsv1.Deployment, code, slug, what, inst string) (Finding, bool) {
	want := int32(1)
	if d.Spec.Replicas != nil {
		want = *d.Spec.Replicas
	}
	if d.Status.ReadyReplicas >= want {
		return Finding{}, false
	}
	return Finding{Code: code, Slug: slug, Severity: SeverityError, Install: inst,
		Message: fmt.Sprintf("%s not ready (%d of %d replicas)", what, d.Status.ReadyReplicas, want)}, true
}

// Required and optional condition types per status block (design §5.2).
// Optional types are evaluated only when present.
var (
	classRequired    = []string{"Accepted"}
	gatewayRequired  = []string{"Accepted", "Programmed"}
	listenerRequired = []string{"Accepted", "Programmed"}
	listenerOptional = []string{"ResolvedRefs"}
	routeRequired    = []string{"Accepted", "ResolvedRefs"}
	routeOptional    = []string{"kgateway.dev/Programmed"}
	policyRequired   = []string{"Accepted"}
	policyOptional   = []string{"Attached"}
)

// conditionsCheck: KGWUI-002 and KGWUI-003. Freshness is decided before
// status (D-006): a missing status block, a missing required condition, a
// condition with reason Pending, or any stale condition makes the resource
// "not yet processed"; only current conditions are reported or count as
// success. Inside the grace period such a resource is "being processed"
// (info) and the check is not OK, so the install is unknown, never healthy.
type conditionsCheck struct{}

// Name implements Check.
func (conditionsCheck) Name() string { return "resource status" }

// Run implements Check.
func (c conditionsCheck) Run(in Input) ([]Finding, CheckResult) {
	var fs []Finding
	id := in.Install.ID()
	add := func(u *unstructured.Unstructured, sev Severity, cond metav1.Condition, extra string) {
		fs = append(fs, Finding{Code: CodeConditionFalse, Slug: "condition-false", Severity: sev, Install: id, Resource: ref(u),
			Message: message(cond), Detail: detail(cond, extra), Since: since(cond)})
	}
	// One KGWUI-003 per (resource, parentRef); parent is the zero value for
	// everything but route parents.
	type pendingKey struct {
		res    objects.Ref
		parent objects.ParentRef
	}
	reported := map[pendingKey]bool{}
	inGrace := map[objects.Ref]bool{} // resources being processed
	// notProcessed reports u (for parent) once: info while u's current
	// generation was first seen less than Grace ago, warning after.
	notProcessed := func(u *unstructured.Unstructured, parent objects.ParentRef, msg, why string) {
		r := objects.RefOf(u)
		k := pendingKey{r, parent}
		if reported[k] {
			return
		}
		reported[k] = true
		sev := SeverityWarning
		if in.Now.Sub(in.firstSeen(u)) < Grace {
			sev, msg = SeverityInfo, "Being processed by the controller"
			inGrace[r] = true
		}
		fs = append(fs, Finding{Code: CodeRouteUnattached, Slug: "route-unattached", Severity: sev, Install: id, Resource: &r,
			Message: msg, Detail: why, parent: parent})
	}
	// pendingFor reports a block of u that is not processed; parent is set for route parents.
	pendingFor := func(u *unstructured.Unstructured, parent objects.ParentRef, why string) {
		notProcessed(u, parent, "Not yet processed by the controller", why)
	}
	pending := func(u *unstructured.Unstructured, why string) { pendingFor(u, objects.ParentRef{}, why) }
	// current reports false when the block is pending: a required condition is
	// missing or Unknown, a present condition has reason Pending, or a present
	// condition may describe an older generation (including observedGeneration 0).
	current := func(u *unstructured.Unstructured, parent objects.ParentRef, cs []metav1.Condition, required, optional []string, extra string) bool {
		for _, t := range required {
			if objects.FindCondition(cs, t) == nil {
				pendingFor(u, parent, fmt.Sprintf("no %s condition%s", t, within(extra)))
				return false
			}
		}
		for _, t := range append(slices.Clone(required), optional...) {
			cond := objects.FindCondition(cs, t)
			if cond == nil {
				continue
			}
			if cond.Reason == "Pending" {
				pendingFor(u, parent, fmt.Sprintf("%s is Pending%s", t, within(extra)))
				return false
			}
			if objects.IsStale(u, *cond) {
				pendingFor(u, parent, "status describes an older generation"+within(extra))
				return false
			}
		}
		for _, t := range required {
			if objects.FindCondition(cs, t).Status == metav1.ConditionUnknown {
				pendingFor(u, parent, fmt.Sprintf("%s is Unknown%s", t, within(extra)))
				return false
			}
		}
		return true
	}
	// evaluate reports every present condition of a current block that is not True.
	evaluate := func(u *unstructured.Unstructured, parent objects.ParentRef, cs []metav1.Condition, required, optional []string, extra string) {
		if !current(u, parent, cs, required, optional, extra) {
			return
		}
		for _, t := range append(slices.Clone(required), optional...) {
			if cond := objects.FindCondition(cs, t); cond != nil && cond.Status != metav1.ConditionTrue {
				add(u, SeverityError, *cond, extra)
			}
		}
	}
	for _, gc := range in.Owned.GatewayClasses {
		evaluate(gc, objects.ParentRef{}, objects.Conditions(gc), classRequired, nil, "")
	}
	// owned are the Gateways and ListenerSets of this install: the only
	// ancestors whose policy status counts here.
	owned := map[objects.Ref]bool{}
	for _, ls := range in.Owned.ListenerSets {
		owned[objects.RefOf(ls)] = true
	}
	for _, gw := range in.Owned.Gateways {
		owned[objects.RefOf(gw)] = true
		evaluate(gw, objects.ParentRef{}, objects.Conditions(gw), gatewayRequired, nil, "")
		byName := map[string]objects.ListenerStatus{}
		for _, l := range objects.GatewayListeners(gw) {
			byName[l.Name] = l
		}
		// Every configured listener needs a status block.
		for _, name := range objects.GatewaySpecListeners(gw) {
			l, ok := byName[name]
			if !ok {
				pending(gw, "no status for listener "+name)
				continue
			}
			evaluate(gw, objects.ParentRef{}, l.Conditions, listenerRequired, listenerOptional, "listener "+l.Name)
			if cf := objects.FindCondition(l.Conditions, "Conflicted"); cf != nil && cf.Status == metav1.ConditionTrue && !objects.IsStale(gw, *cf) {
				add(gw, SeverityError, *cf, "listener "+l.Name)
			}
		}
	}
	// Routes: one status block per current parentRef of this install, matched on
	// the full parentRef including section name and port.
	for _, r := range in.Owned.Routes {
		status := objects.RouteParents(r.Obj)
		for _, p := range r.Parents {
			extra := "parent " + parentDesc(p)
			var st *objects.RouteParentStatus
			for i := range status {
				if status[i].ParentRef == p && slices.Contains(in.Install.ControllerNames, status[i].ControllerName) {
					st = &status[i]
					break
				}
			}
			if st == nil {
				pendingFor(r.Obj, p, "no status from the controller for "+extra)
				continue
			}
			evaluate(r.Obj, p, st.Conditions, routeRequired, routeOptional, extra)
		}
	}
	// Routes whose parent does not exist. A parent of a kind the cache cannot
	// read may only be missing from the cache; cluster access already reports it.
	for _, u := range in.Owned.Unattached {
		if !in.readable(u.Parent.Group, u.Parent.Kind) {
			continue
		}
		notProcessed(u.Route.Obj, u.Parent, "Not attached: parent not found",
			"parent "+parentDesc(u.Parent)+" does not exist")
	}
	// Policies: ancestors written by this install's controller, for a Gateway
	// or ListenerSet it owns or for any other kind. Assign drops policies
	// processed only for another install, so none here means not yet processed;
	// on v2.3/v2.4 that includes a policy whose target is missing.
	for _, p := range in.Owned.Policies {
		var mine []objects.AncestorStatus
		for _, a := range objects.PolicyAncestors(p.Obj) {
			if !slices.Contains(in.Install.ControllerNames, a.ControllerName) {
				continue
			}
			// Gateway-like ancestors must be this install's; others (backends,
			// the status summary) count by controller name.
			if !in.parentKind(a.AncestorRef.Group, a.AncestorRef.Kind) || owned[a.AncestorRef.Ref()] {
				mine = append(mine, a)
			}
		}
		if len(mine) == 0 {
			pending(p.Obj, "no status from the controller")
			continue
		}
		for _, a := range mine {
			extra := "ancestor " + a.AncestorRef.Kind + " " + a.AncestorRef.Ref().String()
			if !current(p.Obj, objects.ParentRef{}, a.Conditions, policyRequired, policyOptional, extra) {
				continue
			}
			for _, cond := range a.Conditions {
				if sev, report := policySeverity(cond); report {
					add(p.Obj, sev, cond, extra)
				}
			}
		}
	}
	if n := len(inGrace); n > 0 {
		noun := "resources are"
		if n == 1 {
			noun = "resource is"
		}
		return fs, CheckResult{Name: c.Name(), Required: true, Pending: true, CollectedAt: in.Snapshot.TakenAt,
			Error: fmt.Sprintf("%d %s being processed by the controller", n, noun)}
	}
	return fs, ok(c.Name(), true, in.Snapshot.TakenAt)
}

// readable reports whether the served kind group/kind can currently be read.
// A kind missing from Served is treated as readable.
func (in Input) readable(group, kind string) bool {
	for _, k := range in.Served {
		if k.Group == group && k.Kind == kind {
			return in.Snapshot.Access[k.GVR()].OK
		}
	}
	return true
}

// parentDesc describes p as "Kind ns/name", plus its section and port when set.
func parentDesc(p objects.ParentRef) string {
	d := p.Kind + " " + p.Namespace + "/" + p.Name
	if p.SectionName != "" {
		d += " section " + p.SectionName
	}
	if p.Port != 0 {
		d += fmt.Sprintf(" port %d", p.Port)
	}
	return d
}

// parentKind reports whether group/kind is a served Gateway or ListenerSet kind.
func (in Input) parentKind(group, kind string) bool {
	for _, k := range in.Served {
		if k.Group == group && k.Kind == kind && (k.Category == kinds.CategoryGateway || k.Category == kinds.CategoryListenerSet) {
			return true
		}
	}
	return false
}

// within formats extra as a trailing " (extra)", or "" when empty.
func within(extra string) string {
	if extra == "" {
		return ""
	}
	return " (" + extra + ")"
}

// policySeverity maps a current policy ancestor condition to a severity
// (design §5.2: Overridden is info, PartiallyValid a warning).
// Reason Pending never reaches it: current() treats it as not processed.
func policySeverity(c metav1.Condition) (Severity, bool) {
	switch c.Reason {
	case "PartiallyValid":
		return SeverityWarning, true
	case "Overridden":
		return SeverityInfo, true
	}
	if c.Status == metav1.ConditionFalse {
		return SeverityError, true
	}
	return "", false
}

// proxyCheck: KGWUI-006. Proxies are matched by ownerReference first, then by
// the gateway-name label, which is truncated for names over 63 characters.
type proxyCheck struct{}

// Name implements Check.
func (proxyCheck) Name() string { return "gateway proxies" }

// Run implements Check. A missing proxy is reported only after the grace period.
func (c proxyCheck) Run(in Input) ([]Finding, CheckResult) {
	var fs []Finding
	for _, gw := range in.Owned.Gateways {
		d := proxyFor(gw, in.Snapshot.Deployments)
		if d == nil {
			if in.Now.Sub(in.firstSeen(gw)) >= Grace {
				fs = append(fs, Finding{Code: CodeProxyNotReady, Slug: "proxy-not-ready", Severity: SeverityWarning, Install: in.Install.ID(),
					Resource: ref(gw), Message: "Proxy deployment not found"})
			}
			continue
		}
		if f, bad := notReady(d, CodeProxyNotReady, "proxy-not-ready", "Proxy", in.Install.ID()); bad {
			f.Resource = ref(gw)
			fs = append(fs, f)
		}
	}
	return fs, ok(c.Name(), true, in.Snapshot.TakenAt)
}

func proxyFor(gw *unstructured.Unstructured, ds []*appsv1.Deployment) *appsv1.Deployment {
	for _, d := range ds {
		if d.Namespace != gw.GetNamespace() || d.Labels["app.kubernetes.io/component"] != "proxy" {
			continue
		}
		for _, o := range d.OwnerReferences {
			if o.Kind == "Gateway" && o.Name == gw.GetName() {
				return d
			}
		}
	}
	for _, d := range ds {
		if d.Namespace == gw.GetNamespace() && d.Labels["app.kubernetes.io/component"] == "proxy" &&
			d.Labels["gateway.networking.k8s.io/gateway-name"] == gw.GetName() {
			return d
		}
	}
	return nil
}

// xdsCheck: KGWUI-001.
type xdsCheck struct{}

// Name implements Check.
func (xdsCheck) Name() string { return "xDS rejects" }

// Run implements Check. Missing, failed or stale metrics are KGWUI-008.
func (c xdsCheck) Run(in Input) ([]Finding, CheckResult) {
	if !in.HasMetrics {
		return unavailable(in, c.Name(), "controller metrics not collected yet")
	}
	if in.Metrics.Err != "" {
		return unavailable(in, c.Name(), in.Metrics.Err)
	}
	// Recomputing the model must not make an old zero-reject result look fresh.
	if in.Metrics.Stale(in.Now, in.MetricsMaxAge) {
		return unavailable(in, c.Name(), fmt.Sprintf("controller metrics are stale (last collected %s ago)", in.Now.Sub(in.Metrics.CollectedAt).Round(time.Second)))
	}
	var fs []Finding
	for _, gw := range in.Owned.Gateways {
		if n := in.Metrics.Metrics.Rejects[ctrlmetrics.GatewayKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}]; n > 0 {
			fs = append(fs, Finding{Code: CodeXDSRejected, Slug: "xds-rejected", Severity: SeverityError, Install: in.Install.ID(),
				Resource: ref(gw), Message: "Configuration rejected by Envoy", Detail: fmt.Sprintf("%.0f active rejects", n)})
		}
	}
	return fs, ok(c.Name(), true, in.Metrics.CollectedAt)
}

// unwatchedCheck: KGWUI-004.
type unwatchedCheck struct{}

// Name implements Check.
func (unwatchedCheck) Name() string { return "watched namespaces" }

// Run implements Check.
func (c unwatchedCheck) Run(in Input) ([]Finding, CheckResult) {
	var fs []Finding
	for _, u := range in.Assignment.Unwatched {
		if u.Install != in.Install.ID() {
			continue
		}
		r := u.Route
		fs = append(fs, Finding{Code: CodeNamespaceNotWatched, Slug: "namespace-not-watched", Severity: SeverityWarning, Install: u.Install,
			Resource: &r, Message: "Namespace not watched by the controller", Detail: "targets Gateway " + u.Gateway.String()})
	}
	return fs, ok(c.Name(), false, in.Now)
}

// ambiguityCheck: KGWUI-009.
type ambiguityCheck struct{}

// Name implements Check.
func (ambiguityCheck) Name() string { return "gateway ownership" }

// Run implements Check.
func (c ambiguityCheck) Run(in Input) ([]Finding, CheckResult) {
	var fs []Finding
	for _, a := range in.Assignment.Ambiguous {
		if !slices.Contains(a.Installs, in.Install.ID()) {
			continue
		}
		g := a.Gateway
		fs = append(fs, Finding{Code: CodeAmbiguousOwnership, Slug: "ambiguous-ownership", Severity: SeverityWarning, Install: in.Install.ID(),
			Resource: &g, Message: "Claimed by more than one controller install", Detail: fmt.Sprint(a.Installs)})
	}
	return fs, ok(c.Name(), false, in.Now)
}

// verifyCheck: KGWUI-007, and KGWUI-008 (info, not required) when it cannot run.
type verifyCheck struct{}

// Name implements Check.
func (verifyCheck) Name() string { return "scope matches controller" }

// Run implements Check. It is optional: verification never decides health on its own.
func (c verifyCheck) Run(in Input) ([]Finding, CheckResult) {
	if !in.Verify.Available {
		return []Finding{{Code: CodeSignalUnavailable, Slug: "signal-unavailable", Severity: SeverityInfo, Install: in.Install.ID(),
			Message: "Could not verify scope against the controller", Detail: in.Verify.Reason}}, CheckResult{Name: c.Name(), CollectedAt: in.Now, Error: in.Verify.Reason}
	}
	var fs []Finding
	for _, m := range in.Verify.Mismatches {
		s := m.Since
		fs = append(fs, Finding{Code: CodeScopeMismatch, Slug: "scope-mismatch", Severity: SeverityWarning, Install: in.Install.ID(),
			Message: "UI scope differs from the controller", Since: &s,
			Detail: fmt.Sprintf("namespace %s, %s: UI counts %.0f, controller counts %.0f", m.Namespace, m.Resource, m.UI, m.Controller)})
	}
	return fs, ok(c.Name(), false, in.Now)
}
