package health

import (
	"fmt"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxymode"
)

// Upstream returns the Phase 1 checks (design §5.2).
func Upstream() []Check {
	return []Check{cacheCheck{}, kindsCheck{}, scopeCheck{}, controllerCheck{}, conditionsCheck{}, proxyCheck{}, xdsCheck{}, unwatchedCheck{}, ambiguityCheck{}, verifyCheck{}}
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

// kindsCheck: the cluster still serves exactly the watched kinds (design §10).
type kindsCheck struct{}

// Name implements Check.
func (kindsCheck) Name() string { return "served kinds" }

// Run implements Check: a served-kinds change is KGWUI-008 until the UI restarts
// or the served set matches again, since the cache does not re-watch.
func (c kindsCheck) Run(in Input) ([]Finding, CheckResult) {
	if in.KindsChange.Empty() {
		return nil, ok(c.Name(), true, in.Now)
	}
	why := "served kinds changed: " + in.KindsChange.String()
	return []Finding{{Code: CodeSignalUnavailable, Slug: "signal-unavailable", Severity: SeverityInfo, Install: in.Install.ID(),
			Message: "Served kinds changed; restart the UI to watch them", Detail: in.KindsChange.String()}},
		CheckResult{Name: c.Name(), Required: true, CollectedAt: in.Now, Error: why}
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
	r := ok(c.Name(), true, in.Now)
	if in.Install.Scope.Source == install.SourceDeploymentTemplate {
		r.Detail = "scope from Deployment template; no controller Pod running"
	}
	return nil, r
}

// controllerCheck: KGWUI-005.
type controllerCheck struct{}

// Name implements Check.
func (controllerCheck) Name() string { return "controller pods" }

// Run implements Check.
func (c controllerCheck) Run(in Input) ([]Finding, CheckResult) {
	d := in.Install.Deployment
	if f, bad := notReady(d, CodeControllerNotReady, "controller-not-ready", "Controller", in.Install.ID(), in.workloads()); bad {
		f.Resource = &objects.Ref{Group: "apps", Kind: "Deployment", Namespace: d.Namespace, Name: d.Name}
		return []Finding{f}, ok(c.Name(), true, in.Now)
	}
	return nil, ok(c.Name(), true, in.Now)
}

// workloads reports whether replica wording applies; only the zero Platform
// defaults to Kubernetes, so the platform name is never consulted.
func (in Input) workloads() bool { return in.Platform.IsZero() || in.Platform.Has(platform.Workloads) }

// notReady reports d as not ready when it has fewer ready replicas than
// desired, or 0 desired replicas (D-017 §1). The finding is an error.
func notReady(d *appsv1.Deployment, code, slug, what, inst string, workloads bool) (Finding, bool) {
	want := Desired(d)
	if want > 0 && d.Status.ReadyReplicas >= want {
		return Finding{}, false
	}
	msg := what + " not ready"
	switch {
	case workloads && want == 0:
		msg = what + " scaled to 0 replicas"
	case workloads:
		msg = fmt.Sprintf("%s not ready (%d of %d replicas)", what, d.Status.ReadyReplicas, want)
	}
	return Finding{Code: code, Slug: slug, Severity: SeverityError, Install: inst, Message: msg}, true
}

// Desired returns d's desired replicas; an unset count defaults to 1.
func Desired(d *appsv1.Deployment) int32 {
	if d.Spec.Replicas != nil {
		return *d.Spec.Replicas
	}
	return 1
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

// conditionsCheck: KGWUI-002, KGWUI-003 and KGWUI-011's status trigger. Freshness is decided before
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
		f := Finding{Code: CodeConditionFalse, Slug: "condition-false", Severity: sev, Install: id, Resource: ref(u),
			Message: message(cond), Detail: detail(cond, extra), Since: since(cond)}
		delegationCondition(&f, u, cond)
		fs = append(fs, f)
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
	// pendingFor reports a block of u that is not processed; parent is set for
	// route parents. An experimental-only kind gets a hint (D-018 §2).
	pendingFor := func(u *unstructured.Unstructured, parent objects.ParentRef, why string) {
		notProcessed(u, parent, "Not yet processed by the controller", why+in.experimentalHint(u))
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
	// evaluate reports every present condition of a current block that is False;
	// current() has already turned a required Unknown into "not processed".
	evaluate := func(u *unstructured.Unstructured, parent objects.ParentRef, cs []metav1.Condition, required, optional []string, extra string) {
		if !current(u, parent, cs, required, optional, extra) {
			return
		}
		for _, t := range append(slices.Clone(required), optional...) {
			if cond := objects.FindCondition(cs, t); cond != nil && cond.Status == metav1.ConditionFalse {
				add(u, SeverityError, *cond, extra)
			}
		}
	}
	for _, gc := range in.Owned.GatewayClasses {
		evaluate(gc, objects.ParentRef{}, objects.Conditions(gc), classRequired, nil, "")
	}
	// listeners evaluates the listener status of a Gateway or ListenerSet; both
	// share the listener status shape. With all, every configured listener
	// needs a status block; otherwise only present blocks are judged.
	listeners := func(u *unstructured.Unstructured, all bool) {
		byName := map[string]objects.ListenerStatus{}
		for _, l := range objects.GatewayListeners(u) {
			byName[l.Name] = l
		}
		for _, name := range objects.GatewaySpecListeners(u) {
			l, ok := byName[name]
			if !ok {
				if all {
					pending(u, "no status for listener "+name)
				}
				continue
			}
			evaluate(u, objects.ParentRef{}, l.Conditions, listenerRequired, listenerOptional, "listener "+l.Name)
			if cf := objects.FindCondition(l.Conditions, "Conflicted"); cf != nil && cf.Status == metav1.ConditionTrue && !objects.IsStale(u, *cf) {
				add(u, SeverityError, *cf, "listener "+l.Name)
			}
		}
	}
	// owned are the Gateways and ListenerSets of this install: the only
	// ancestors whose policy status counts here.
	owned := map[objects.Ref]bool{}
	// ListenerSet status is required like a Gateway's (D-017 §2). A rejected
	// ListenerSet may have no listener status (reason NotAllowed), so while it
	// is Accepted=False only the listener blocks present are judged.
	for _, ls := range in.Owned.ListenerSets {
		owned[objects.RefOf(ls)] = true
		cs := objects.Conditions(ls)
		if !current(ls, objects.ParentRef{}, cs, gatewayRequired, nil, "") {
			continue
		}
		evaluate(ls, objects.ParentRef{}, cs, gatewayRequired, nil, "")
		listeners(ls, objects.FindCondition(cs, "Accepted").Status != metav1.ConditionFalse)
	}
	for _, gw := range in.Owned.Gateways {
		owned[objects.RefOf(gw)] = true
		evaluate(gw, objects.ParentRef{}, objects.Conditions(gw), gatewayRequired, nil, "")
		listeners(gw, true)
	}
	// Routes: one status block per current parentRef of this install, and per
	// reported HTTPRoute parent, matched on the full parentRef including section name and port.
	for _, r := range in.Owned.Routes {
		if in.ignored(r.Obj) {
			continue
		}
		status := objects.RouteParents(r.Obj)
		for _, p := range slices.Concat(r.Parents, r.RouteParents) {
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
		if !in.readable(u.Parent.Group, u.Parent.Kind) || in.ignored(u.Route.Obj) {
			continue
		}
		notProcessed(u.Route.Obj, u.Parent, "Not attached: parent not found",
			"parent "+parentDesc(u.Parent)+" does not exist")
	}
	// Declared candidates: KGWUI-011's status trigger. Inside its grace the
	// route is being processed, so this check is pending (design §6.2).
	for _, d := range in.Owned.Declared {
		f := declaredFinding(in, d)
		if f.Severity == SeverityInfo {
			inGrace[*f.Resource] = true
		}
		fs = append(fs, f)
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
			// Only the freshness-checked types are judged; others may be stale.
			for _, t := range append(slices.Clone(policyRequired), policyOptional...) {
				if cond := objects.FindCondition(a.Conditions, t); cond != nil {
					if sev, report := policySeverity(*cond); report {
						add(p.Obj, sev, *cond, extra)
					}
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

// experimentalKind returns u's served kind when kgateway watches it, at its
// served version, only with experimental Gateway API features on (kinds.ExperimentalOnly).
func (in Input) experimentalKind(u *unstructured.Unstructured) (kinds.Served, bool) {
	gvk := u.GroupVersionKind()
	for _, k := range in.Served {
		if k.Group == gvk.Group && k.Kind == gvk.Kind {
			return k, k.ExperimentalOnly(in.Install.KgatewayVersion())
		}
	}
	return kinds.Served{}, false
}

// ExperimentalHint is appended to the detail of a "not processed" finding on
// an experimental-only kind (D-018 §2); %s is the kind and served version.
const ExperimentalHint = "; the controller watches %s only when its experimental Gateway API features are on, and they may be off here"

// experimentalHint returns ExperimentalHint for u's kind, or "" for a stable kind.
func (in Input) experimentalHint(u *unstructured.Unstructured) string {
	k, exp := in.experimentalKind(u)
	if !exp {
		return ""
	}
	return fmt.Sprintf(ExperimentalHint, k.Kind+" "+k.Version)
}

// ignored reports whether route u is left unjudged: its kind is
// experimental-only and only other controllers wrote its status. A route
// with no status at all follows the normal rule (D-018 §1).
func (in Input) ignored(u *unstructured.Unstructured) bool {
	if _, exp := in.experimentalKind(u); !exp {
		return false
	}
	status := objects.RouteParents(u)
	for _, p := range status {
		if slices.Contains(in.Install.ControllerNames, p.ControllerName) {
			return false
		}
	}
	return len(status) > 0
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

// proxyCheck: KGWUI-006. Proxies are Deployments in the Gateway's namespace
// that match the install's ProxySelector, found by ownerReference first, then
// by the gateway-name label, which is truncated for names over 63 characters.
type proxyCheck struct{}

// Name implements Check.
func (proxyCheck) Name() string { return "gateway proxies" }

// Run implements Check. A missing proxy is reported after the grace period;
// inside it the check is pending, so the install is unknown (D-017 §3). A
// self-managed Gateway is the exception: KGWUI-010 (info) says the UI cannot
// see that proxy's readiness, so it never drives the roll-up (D-016). A proxy
// Deployment that exists is judged the same either way.
func (c proxyCheck) Run(in Input) ([]Finding, CheckResult) {
	var fs []Finding
	inGrace := 0
	for _, gw := range in.Owned.Gateways {
		d := proxyFor(gw, in.Snapshot.Deployments, in.Install.ProxySelector)
		if d == nil {
			if in.proxyMode(gw) == proxymode.SelfManaged {
				fs = append(fs, Finding{Code: CodeSelfManagedProxy, Slug: "self-managed-proxy", Severity: SeverityInfo, Install: in.Install.ID(),
					Resource: ref(gw), Message: "Self-managed proxy: the UI cannot see its readiness"})
				continue
			}
			if in.Now.Sub(in.firstSeen(gw)) < Grace {
				inGrace++
				continue
			}
			fs = append(fs, Finding{Code: CodeProxyNotReady, Slug: "proxy-not-ready", Severity: SeverityWarning, Install: in.Install.ID(),
				Resource: ref(gw), Message: "Proxy deployment not found"})
			continue
		}
		if f, bad := notReady(d, CodeProxyNotReady, "proxy-not-ready", "Proxy", in.Install.ID(), in.workloads()); bad {
			f.Resource = ref(gw)
			// A proxy scaled to zero may be deliberate (D-017 §1).
			if Desired(d) == 0 {
				f.Severity = SeverityWarning
				if in.workloads() {
					f.Message = "Proxy has 0 replicas"
				}
			}
			fs = append(fs, f)
		}
	}
	if inGrace > 0 {
		msg := fmt.Sprintf("%d proxies not found yet", inGrace)
		if inGrace == 1 {
			msg = "1 proxy not found yet"
		}
		return fs, CheckResult{Name: c.Name(), Required: true, Pending: true, CollectedAt: in.Snapshot.TakenAt, Error: msg}
	}
	return fs, ok(c.Name(), true, in.Snapshot.TakenAt)
}

// gatewayNameLabel is the standard Gateway API label on generated proxy resources.
const gatewayNameLabel = "gateway.networking.k8s.io/gateway-name"

// proxyFor returns gw's proxy Deployment among ds; a nil sel matches none.
func proxyFor(gw *unstructured.Unstructured, ds []*appsv1.Deployment, sel labels.Selector) *appsv1.Deployment {
	if sel == nil {
		return nil
	}
	var byLabel *appsv1.Deployment
	for _, d := range ds {
		if d.Namespace != gw.GetNamespace() || !sel.Matches(labels.Set(d.Labels)) {
			continue
		}
		for _, o := range d.OwnerReferences {
			if o.Kind == "Gateway" && o.Name == gw.GetName() {
				return d
			}
		}
		if byLabel == nil && d.Labels[gatewayNameLabel] == gw.GetName() {
			byLabel = d
		}
	}
	return byLabel
}

// xdsCheck: KGWUI-001.
type xdsCheck struct{}

// Name implements Check.
func (xdsCheck) Name() string { return "xDS rejects" }

// Run implements Check. Missing, failed or stale metrics, or a scrape without
// the resources_managed family while Gateways are owned, are KGWUI-008.
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
	// Every owned Gateway has a resources_managed series (D-012); without the
	// family the endpoint or metric names are wrong, and zero rejects proves nothing.
	if n := len(in.Owned.Gateways); n > 0 && !in.Metrics.Metrics.ManagedPresent {
		return unavailable(in, c.Name(), fmt.Sprintf("%s missing from controller metrics although the install owns %d Gateway(s); check the metrics endpoint",
			ctrlmetrics.ResourcesManaged, n))
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
			Resource: &r, Message: "Namespace not watched by the controller", Detail: "targets " + u.Gateway.Kind + " " + u.Gateway.String()})
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
