package health

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrllogs"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxymode"
)

// Upstream returns the upstream checks (design §5.2, Phase 3 §9.3).
func Upstream() []Check {
	return []Check{cacheCheck{}, kindsCheck{}, scopeCheck{}, controllerCheck{}, conditionsCheck{}, proxyCheck{}, xdsCheck{}, unwatchedCheck{}, ambiguityCheck{}, verifyCheck{}, backendCheck{},
		restartsCheck{}, probesCheck{}, rolloutsCheck{}, skewCheck{}, proxyStatsCheck{proxyWarming}, proxyStatsCheck{proxyDisconnected}, computedCheck{}, progressCheck{}, endpointsCheck{}}
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
			Message: "Could not check " + name, Detail: why, Key: []string{name}}},
		CheckResult{Name: name, Required: true, CollectedAt: in.Now, Error: why}
}

// cacheCheck: the snapshot is synced and the core kinds are readable; optional
// watches are left to the checks that read them.
type cacheCheck struct{}

// Name implements Check.
func (cacheCheck) Name() string { return "cluster access" }

// Inputs implements Check: the served kinds and Namespaces through Owned, plus Pods and Deployments.
func (cacheCheck) Inputs() InputSet {
	return InputSet{Owned: true, Kinds: []objects.GroupKind{{Kind: "Pod"}, {Group: "apps", Kind: "Deployment"}}}
}

// Run implements Check: one KGWUI-008 per kind that cannot be read.
func (c cacheCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	if !in.Snapshot.Synced {
		return unavailable(in, c.Name(), "cache not synced")
	}
	var fs []Finding
	core := []schema.GroupVersionResource{cache.NamespacesGVR, cache.PodsGVR, cache.DeploymentsGVR}
	for _, s := range in.Served {
		if !s.ListOnly { // list-only kinds are optional (D-034); the overview tile shows their access
			core = append(core, s.GVR())
		}
	}
	for _, gvr := range core {
		if a, known := in.Snapshot.Access[gvr]; known && !a.OK {
			fs = append(fs, Finding{Code: CodeSignalUnavailable, Slug: "signal-unavailable", Severity: SeverityInfo, Install: in.Install.ID(),
				Message: "No access to " + resourceName(gvr), Detail: a.Err, Key: []string{c.Name(), resourceName(gvr)}})
		}
	}
	if len(fs) > 0 {
		slices.SortFunc(fs, func(a, b Finding) int {
			return cmp.Or(cmp.Compare(a.Message, b.Message), cmp.Compare(a.Detail, b.Detail))
		})
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

// Inputs implements Check.
func (kindsCheck) Inputs() InputSet { return InputSet{KindsChange: true} }

// Run implements Check: a served-kinds change is KGWUI-008 until the UI restarts
// or the served set matches again, since the cache does not re-watch.
func (c kindsCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	if in.KindsChange.Empty() {
		return nil, ok(c.Name(), true, in.Now)
	}
	why := "served kinds changed: " + in.KindsChange.String()
	return []Finding{{Code: CodeSignalUnavailable, Slug: "signal-unavailable", Severity: SeverityInfo, Install: in.Install.ID(),
			Message: "Served kinds changed; restart the UI to watch them", Detail: in.KindsChange.String(), Key: []string{c.Name()}}},
		CheckResult{Name: c.Name(), Required: true, CollectedAt: in.Now, Error: why}
}

// scopeCheck: the install's scope could be determined.
type scopeCheck struct{}

// Name implements Check.
func (scopeCheck) Name() string { return "controller scope" }

// Inputs implements Check: none beyond Install, which is always an input.
func (scopeCheck) Inputs() InputSet { return InputSet{} }

// Run implements Check: an unknown scope (for example a selector env set
// through valueFrom) is KGWUI-008 with the scope's error as detail.
func (c scopeCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
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

// Inputs implements Check: AllInputs plus the process data its detail quotes.
func (controllerCheck) Inputs() InputSet {
	in := AllInputs()
	in.Process = true
	return in
}

// Run implements Check.
func (c controllerCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	d := in.Install.Deployment
	if f, bad := notReady(d, CodeControllerNotReady, "controller-not-ready", "Controller", in.Install.ID(), in.workloads()); bad {
		f.Resource = &objects.Ref{Group: "apps", Kind: "Deployment", Namespace: d.Namespace, Name: d.Name}
		if in.Process != nil {
			addNotReadyFacts(&f, &in.Process.Controller, d.Namespace, d.Status.ReadyReplicas, Desired(d))
		}
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

// conditionsCheck: KGWUI-002, 003, 013–017 and KGWUI-011's status trigger. Freshness is decided before
// status (D-006): a missing status block, a missing required condition, a
// condition with reason Pending, or any stale condition makes the resource
// "not yet processed"; only current conditions are reported or count as
// success. Inside the grace period such a resource is "being processed"
// (info) and the check is not OK, so the install is unknown, never healthy.
type conditionsCheck struct{}

// Name implements Check.
func (conditionsCheck) Name() string { return "resource status" }

// Inputs implements Check.
func (conditionsCheck) Inputs() InputSet {
	s := AllInputs()
	s.Catalog = true // 013 and 014 explanation 1
	return s
}

// Run implements Check.
func (c conditionsCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	var fs []Finding
	id := in.Install.ID()
	// owner is the condition's discriminator owner: self, a listener, a parent or an ancestor.
	add := func(u *unstructured.Unstructured, sev Severity, cond metav1.Condition, extra string, owner []string) {
		f := Finding{Code: CodeConditionFalse, Slug: "condition-false", Severity: sev, Install: id, Resource: ref(u),
			Message: message(cond), Detail: detail(cond, extra), Since: since(cond), Key: append(slices.Clip(owner), cond.Type)}
		delegationCondition(&f, u, cond)
		fs = append(fs, f)
	}
	// One KGWUI-003 per (resource, parentRef); parent is the zero value for
	// everything but route parents.
	type pendingKey struct {
		res    objects.Ref
		parent objects.ParentRef
	}
	reported := map[pendingKey]int{}  // index in fs
	inGrace := map[objects.Ref]bool{} // resources being processed
	// notProcessed reports u (for parent) once: info while u's current
	// generation was first seen less than Grace ago, warning after. A later
	// block whose kind comes first in §10.1 order replaces the evidence.
	notProcessed := func(u *unstructured.Unstructured, parent objects.ParentRef, msg, why string, ev *FreshnessEvidence) {
		r := objects.RefOf(u)
		k := pendingKey{r, parent}
		if i, ok := reported[k]; ok {
			if prev := fs[i].Facts.(*FreshnessEvidence); freshnessRank[ev.Freshness] < freshnessRank[prev.Freshness] {
				fs[i].Detail, fs[i].Facts = why, ev
			}
			return
		}
		reported[k] = len(fs)
		sev := SeverityWarning
		if in.Now.Sub(in.firstSeen(u)) < Grace {
			sev, msg = SeverityInfo, "Being processed by the controller"
			inGrace[r] = true
		}
		key := []string{ownerSelf}
		if parent != (objects.ParentRef{}) {
			key = in.parentKey(u, parent)
		}
		fs = append(fs, Finding{Code: CodeRouteUnattached, Slug: "route-unattached", Severity: sev, Install: id, Resource: &r,
			Message: msg, Detail: why, Key: key, Facts: ev, parent: &parent})
	}
	// pendingFor reports a block of u that is not processed; parent is set for
	// route parents. An experimental-only kind gets a hint (D-018 §2).
	pendingFor := func(u *unstructured.Unstructured, parent objects.ParentRef, why, kind string, observed int64) {
		notProcessed(u, parent, "Not yet processed by the controller", why+in.experimentalHint(u), in.freshnessEvidence(u, kind, observed))
	}
	pending := func(u *unstructured.Unstructured, why, kind string) { pendingFor(u, objects.ParentRef{}, why, kind, 0) }
	// current reports false when the block is pending: a required condition is
	// missing or Unknown, a present condition has reason Pending, or a present
	// condition may describe an older generation (including observedGeneration 0).
	current := func(u *unstructured.Unstructured, parent objects.ParentRef, cs []metav1.Condition, required, optional []string, extra string) bool {
		if why, kind, observed := blockFreshness(u, cs, required, optional); why != "" {
			pendingFor(u, parent, why+within(extra), kind, observed)
			return false
		}
		return true
	}
	// evaluate reports every present condition of a current block that is False;
	// current() has already turned a required Unknown into "not processed".
	// A nil owner is a route parent's, resolved on its first finding.
	// While conflict is set, 017 owns the listener's Accepted and Programmed False with that reason.
	var conflict string
	evaluate := func(u *unstructured.Unstructured, parent objects.ParentRef, cs []metav1.Condition, required, optional []string, extra string, owner []string) {
		if !current(u, parent, cs, required, optional, extra) {
			return
		}
		for _, ts := range [2][]string{required, optional} {
			for _, t := range ts {
				if cond := objects.FindCondition(cs, t); cond != nil && cond.Status == metav1.ConditionFalse {
					if conflict != "" && cond.Reason == conflict && (t == "Accepted" || t == "Programmed") {
						continue
					}
					if owner == nil {
						owner = in.parentKey(u, parent)
					}
					// 013, 014 and 015 own their reasons; 002 takes the rest (D-030).
					if l := listenerName(parent, owner); parent != (objects.ParentRef{}) || l != "" {
						if f, ok := in.refsFinding(u, *cond, l, extra, owner); ok {
							fs = append(fs, f)
							continue
						}
					}
					if parent != (objects.ParentRef{}) {
						if f, ok := in.ruleFinding(u, *cond, extra, owner, ctx.WakeAt); ok {
							fs = append(fs, f)
							continue
						}
					}
					add(u, SeverityError, *cond, extra, owner)
				}
			}
		}
	}
	self := []string{ownerSelf}
	for _, gc := range in.Owned.GatewayClasses {
		evaluate(gc, objects.ParentRef{}, objects.Conditions(gc), classRequired, nil, "", self)
	}
	// listeners evaluates the listener status of a Gateway or ListenerSet; both
	// share the listener status shape. With all, every configured listener
	// needs a status block; otherwise only present blocks are judged.
	// A conflict's other side is any listener on the same Gateway (017); built on the first conflict.
	var sides map[objects.Ref][]listenerSide
	listeners := func(u *unstructured.Unstructured, gw objects.Ref, spec []objects.Listener, status []objects.ListenerStatus, all bool) {
		byName := map[string]objects.ListenerStatus{}
		for _, l := range status {
			byName[l.Name] = l
		}
		for _, name := range objects.GatewaySpecListeners(u) {
			l, ok := byName[name]
			if !ok {
				if all {
					pending(u, "No status for listener "+name, FreshnessNeverWritten)
				}
				continue
			}
			owner := []string{ownerListener, l.Name}
			// 017 owns a current Conflicted=True and the conditions with its reason; 002 reports none (D-030, R94).
			cf := objects.FindCondition(l.Conditions, "Conflicted")
			if cf != nil && (cf.Status != metav1.ConditionTrue || objects.IsStale(u, *cf)) {
				cf = nil
			}
			if cf != nil {
				conflict = cf.Reason
			}
			evaluate(u, objects.ParentRef{}, l.Conditions, listenerRequired, listenerOptional, "listener "+l.Name, owner)
			conflict = ""
			if cf != nil {
				if sides == nil {
					sides = in.gatewayListeners()
				}
				i := slices.IndexFunc(spec, func(s objects.Listener) bool { return s.Name == name })
				sl := objects.Listener{Name: name}
				if i >= 0 {
					sl = spec[i]
				}
				fs = append(fs, in.conflictFinding(u, sl, *cf, sides[gw], owner))
			}
		}
	}
	// owned are the Gateways and ListenerSets of this install: the only
	// ancestors whose policy status counts here.
	owned := map[objects.Ref]bool{}
	// ListenerSet status is required like a Gateway's (D-017 §2). The CRDs
	// default top-level conditions, so none at all is never written (§10.1). A rejected
	// ListenerSet may have no listener status (reason NotAllowed), so while it
	// is Accepted=False only the listener blocks present are judged.
	for _, ls := range in.Owned.ListenerSets {
		owned[objects.RefOf(ls)] = true
		v := in.views().ListenerSet(ls)
		cs := v.Conditions
		if len(cs) == 0 {
			pending(ls, noControllerStatus, FreshnessNeverWritten)
			continue
		}
		if !current(ls, objects.ParentRef{}, cs, gatewayRequired, nil, "") {
			continue
		}
		evaluate(ls, objects.ParentRef{}, cs, gatewayRequired, nil, "", self)
		listeners(ls, v.Parent.Ref(), v.Listeners, v.ListenerStatus, objects.FindCondition(cs, "Accepted").Status != metav1.ConditionFalse)
	}
	for _, gw := range in.Owned.Gateways {
		owned[objects.RefOf(gw)] = true
		v := in.views().Gateway(gw)
		if len(v.Conditions) == 0 {
			pending(gw, noControllerStatus, FreshnessNeverWritten)
		} else {
			evaluate(gw, objects.ParentRef{}, v.Conditions, gatewayRequired, nil, "", self)
		}
		listeners(gw, objects.RefOf(gw), v.Listeners, v.ListenerStatus, true)
	}
	// Routes: one status block per current parentRef of this install, and per
	// reported HTTPRoute parent, matched on the full parentRef including section name and port.
	for _, r := range in.Owned.Routes {
		if in.ignored(r.Obj) {
			continue
		}
		status := in.routeParents(r.Obj)
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
				pendingFor(r.Obj, p, noControllerStatus+" for "+extra, FreshnessNeverWritten, 0)
				continue
			}
			evaluate(r.Obj, p, st.Conditions, routeRequired, routeOptional, extra, nil)
		}
	}
	// Routes whose parent does not exist. A parent of a kind the cache cannot
	// read may only be missing from the cache; cluster access already reports it.
	for _, u := range in.Owned.Unattached {
		if !in.readable(u.Parent.Group, u.Parent.Kind) || in.ignored(u.Route.Obj) {
			continue
		}
		notProcessed(u.Route.Obj, u.Parent, "Not attached: parent not found",
			"Parent "+parentDesc(u.Parent)+" does not exist", in.freshnessEvidence(u.Route.Obj, FreshnessNeverWritten, 0))
	}
	// Declared candidates: KGWUI-011's status trigger. Inside its grace the
	// route is being processed, so this check is pending (design §6.2).
	declared := len(fs)
	for _, d := range in.Owned.Declared {
		f := declaredFinding(in, d)
		if f.Severity == SeverityInfo {
			inGrace[*f.Resource] = true
		}
		fs = append(fs, f)
	}
	// The computed and orphan triggers never make this check pending; a
	// computed one joins the status trigger of its (route, parent) (PF13).
	fs = mergeTriggers(fs, declared, computedTriggerFindings(in))
	// Policies: ancestors written by this install's controller, for a Gateway
	// or ListenerSet it owns or for any other kind. Assign drops policies
	// processed only for another install, so none here means not yet processed;
	// on v2.3/v2.4 that includes a policy whose target is missing. A computed
	// 016 for every target replaces that 003 (design §7.6).
	targets := in.newPolicyTargets(ctx.State, ctx.WakeAt)
	for _, p := range in.Owned.Policies {
		tfs, allMissing := targets.findings(p.Obj)
		fs = append(fs, tfs...)
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
			if !allMissing {
				pending(p.Obj, noControllerStatus, FreshnessNoAncestor)
			}
			continue
		}
		for _, a := range mine {
			extra := "ancestor " + a.AncestorRef.Kind + " " + a.AncestorRef.Ref().String()
			owner := append([]string{ownerAncestor}, refKey(a.AncestorRef.Ref())...)
			owner = append(owner, a.AncestorRef.SectionName, strconv.Itoa(int(a.AncestorRef.Port)))
			if !current(p.Obj, objects.ParentRef{}, a.Conditions, policyRequired, policyOptional, extra) {
				continue
			}
			// 016 owns TargetNotFound where the rule set reads it from status (D-030).
			tnf, fromStatus := in.statusTargetFinding(p.Obj, a.Conditions, extra, owner)
			if fromStatus {
				fs = append(fs, tnf)
			}
			// Only the freshness-checked types are judged; others may be stale.
			for _, t := range append(slices.Clone(policyRequired), policyOptional...) {
				if cond := objects.FindCondition(a.Conditions, t); cond != nil && (!fromStatus || cond.Reason != "TargetNotFound") {
					if sev, report := policySeverity(*cond); report {
						add(p.Obj, sev, *cond, extra, owner)
					}
				}
			}
		}
	}
	targets.save(ctx.State)
	// Status-only owners become ranks; 002 then ranks the condition type and a computed 011 its parent.
	rankOwners(fs, ownerAncestor, ownerAncestor)
	rankOwners(fs, ownerStatusParent, ownerParent)
	ordinalKeys(fs, func(f *Finding) (int, bool) {
		switch {
		case f.Code == CodeConditionFalse:
			return len(f.Key) - 1, true
		case f.Code == CodeRuleReplaced: // the match index is status data
			return len(f.Key) - 2, true
		case f.Code == CodeDelegatedNotAttached && len(f.Key) > 0 && f.Key[0] == computedOwner:
			return 1, true
		case f.Code == CodeComputedDisagrees: // the policy-target verifier's check key
			return 0, true
		}
		return 0, false
	})
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

// listenerName returns the listener a status block's owner names, "" for a
// route parent or any other owner.
func listenerName(parent objects.ParentRef, owner []string) string {
	if parent == (objects.ParentRef{}) && len(owner) == 2 && owner[0] == ownerListener {
		return owner[1]
	}
	return ""
}

// pendingReason returns why a status block of u is not current, or "" when
// it is (D-006, design §10.1).
func pendingReason(u *unstructured.Unstructured, cs []metav1.Condition, required, optional []string) string {
	why, _, _ := blockFreshness(u, cs, required, optional)
	return why
}

// RoutePending returns why one status.parents entry of route u is not
// current under the rules the resource status check applies, or "" when it is.
func RoutePending(u *unstructured.Unstructured, cs []metav1.Condition) string {
	return pendingReason(u, cs, routeRequired, routeOptional)
}

// GatewayPending is RoutePending for a Gateway or ListenerSet status block.
func GatewayPending(u *unstructured.Unstructured, cs []metav1.Condition) string {
	if len(cs) == 0 {
		return noControllerStatus
	}
	return pendingReason(u, cs, gatewayRequired, nil)
}

// PolicyPending is RoutePending for one status.ancestors entry of policy u.
func PolicyPending(u *unstructured.Unstructured, cs []metav1.Condition) string {
	return pendingReason(u, cs, policyRequired, policyOptional)
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

// ExperimentalSentence is the same hint as a page sentence, for an experimental-only
// object with no status (design §6.2); %s is the kind and served version.
const ExperimentalSentence = "The controller watches %s only with its experimental Gateway API features on. They may be off here."

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
	status := in.routeParents(u)
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
// that match the install's ProxySelector, found by process.ProxyDeployment:
// by ownerReference first, then by the safe gateway-name label value.
type proxyCheck struct{}

// Name implements Check.
func (proxyCheck) Name() string { return "gateway proxies" }

// Inputs implements Check: AllInputs plus the apps Deployments the proxies are
// found in and the process data its detail quotes.
func (proxyCheck) Inputs() InputSet {
	in := AllInputs()
	in.Kinds = []objects.GroupKind{{Group: "apps", Kind: "Deployment"}}
	in.Process = true
	in.Collected = []string{ctrllogs.Key}
	return in
}

// Run implements Check. A missing proxy is reported after the grace period;
// inside it the check is pending, so the install is unknown (D-017 §3). A
// self-managed Gateway is the exception: KGWUI-010 (info) says the UI cannot
// see that proxy's readiness, so it never drives the roll-up (D-016). A proxy
// Deployment that exists is judged the same either way.
func (c proxyCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	var fs []Finding
	inGrace := 0
	for _, gw := range in.Owned.Gateways {
		gwRef := objects.RefOf(gw)
		d, ambiguous := process.LookupProxyDeployment(gwRef, in.Snapshot.Deployments, in.Install.ProxySelector)
		if ambiguous {
			fs = append(fs, Finding{Code: CodeProxyNotReady, Slug: "proxy-not-ready", Severity: SeverityWarning, Install: in.Install.ID(),
				Resource: ref(gw), Message: proxyAmbiguousMessage})
			continue
		}
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
			f.Resource = &gwRef
			if in.Process != nil {
				if set, known := in.Process.Proxies[gwRef]; known {
					addNotReadyFacts(&f, &set, gwRef.Namespace, d.Status.ReadyReplicas, Desired(d))
				}
			}
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
		addProxyDeployerErrors(ctx, fs)
		return fs, CheckResult{Name: c.Name(), Required: true, Pending: true, CollectedAt: in.Snapshot.TakenAt, Error: msg}
	}
	addProxyDeployerErrors(ctx, fs)
	return fs, ok(c.Name(), true, in.Snapshot.TakenAt)
}

// addProxyDeployerErrors adds the deployer's errors to each 006 in fs, as a
// failed apply is often why a proxy Deployment is missing or not ready.
func addProxyDeployerErrors(ctx RunContext, fs []Finding) {
	events, logs := installLogs(ctx)
	for i := range fs {
		if fs[i].Code == CodeProxyNotReady {
			addDeployerErrors(ctx, &fs[i], events, logs)
		}
	}
}

// proxyAmbiguousMessage is 006's message when several Deployments carry the Gateway's label.
const proxyAmbiguousMessage = "More than one proxy Deployment matches this Gateway"

// unwatchedCheck: KGWUI-004.
type unwatchedCheck struct{}

// Name implements Check.
func (unwatchedCheck) Name() string { return "watched namespaces" }

// Inputs implements Check.
func (unwatchedCheck) Inputs() InputSet { return AllInputs() }

// Run implements Check.
func (c unwatchedCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	var fs []Finding
	for _, u := range in.Assignment.Unwatched {
		if u.Install != in.Install.ID() {
			continue
		}
		r := u.Route
		fs = append(fs, Finding{Code: CodeNamespaceNotWatched, Slug: "namespace-not-watched", Severity: SeverityWarning, Install: u.Install,
			Resource: &r, Message: "Namespace not watched by the controller", Detail: "targets " + u.Gateway.Kind + " " + u.Gateway.String(),
			Key: refKey(u.Gateway)})
	}
	return fs, ok(c.Name(), false, in.Now)
}

// ambiguityCheck: KGWUI-009.
type ambiguityCheck struct{}

// Name implements Check.
func (ambiguityCheck) Name() string { return "gateway ownership" }

// Inputs implements Check.
func (ambiguityCheck) Inputs() InputSet { return AllInputs() }

// Run implements Check.
func (c ambiguityCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
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

// Inputs implements Check.
func (verifyCheck) Inputs() InputSet { return AllInputs() }

// Run implements Check. It is optional: verification never decides health on its own.
func (c verifyCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	if !in.Verify.Available {
		return []Finding{{Code: CodeSignalUnavailable, Slug: "signal-unavailable", Severity: SeverityInfo, Install: in.Install.ID(),
			Message: "Could not verify scope against the controller", Detail: in.Verify.Reason, Key: []string{c.Name()}}}, CheckResult{Name: c.Name(), CollectedAt: in.Now, Error: in.Verify.Reason}
	}
	var fs []Finding
	for _, m := range in.Verify.Mismatches {
		s := m.Since
		fs = append(fs, Finding{Code: CodeScopeMismatch, Slug: "scope-mismatch", Severity: SeverityWarning, Install: in.Install.ID(),
			Message: "UI scope differs from the controller", Since: &s, Key: []string{m.Namespace, m.Resource},
			Detail: fmt.Sprintf("namespace %s, %s: UI counts %.0f, controller counts %.0f", m.Namespace, m.Resource, m.UI, m.Controller)})
	}
	return fs, ok(c.Name(), false, in.Now)
}
