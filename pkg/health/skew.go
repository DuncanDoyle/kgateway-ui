package health

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/process"
)

const (
	skewCheckName = "version skew"
	// SkewSettle is how long after a controller rollout KGWUI-022 stays suppressed (design §9.4).
	SkewSettle = 10 * time.Minute
	// skewKey holds the skewState in the check's Store.
	skewKey = "settle"
	// maxSkewPods bounds the Pod names a version-skew evidence lists per version.
	maxSkewPods = 5
	// gatewayCRD is the CRD whose bundle stands for the install's Gateway API CRDs.
	gatewayCRD = "gateways." + objects.GatewayAPIGroup
)

// Wording of KGWUI-022 (design §9.4), from the wording pass (Task 51).
const (
	skewProxyMessage     = "Proxy version differs from the controller"
	skewProxyDetail      = "Proxy %s, controller %s. kgateway does not support mixed minor versions."
	skewPinnedDetail     = " GatewayParameters %s sets the proxy image. That is the likely cause."
	skewReplicasMessage  = "Controller Pods run different versions"
	skewReplicasDetail   = "Controller Pods still run %s after the rollout ended."
	skewCRDMessage       = "Gateway API CRDs do not match this kgateway version"
	skewCRDDetail        = "%s is Gateway API %s. kgateway %s expects %s."
	skewMixedMessage     = "Gateway API CRDs are from different versions or channels"
	skewMixedDetail      = "%s is %s, but most Gateway API CRDs are %s."
	skewSuppressedDetail = "Versions are compared 10 minutes after a controller rollout ends"
	skewUnknownDetail    = "Pods whose image has no version tag were not compared"
	skewNoExpected       = "No known Gateway API version for kgateway %s, so CRDs were not compared"
	skewCtrlUnknown      = "The controller version is not known, so CRDs were not compared"
	skewStuckDetail      = "Versions were not compared: the controller rollout is stuck (KGWUI-021)"
	skewCRDsOffDetail    = "Gateway API CRDs are not watched, so their versions were not checked"
	skewCRDsSyncDetail   = "The CRD watch has not synced yet"
)

var versionRE = regexp.MustCompile(`^v?(\d+)\.(\d+)(?:\.\d+)?(?:[-+][0-9A-Za-z.+-]*)?$`)

// versionMinor is v's "major.minor"; "" when v is not a version, such as
// "latest" or an empty digest-only tag (Review Focus 4).
func versionMinor(v string) string {
	m := versionRE.FindStringSubmatch(v)
	if m == nil {
		return ""
	}
	return m[1] + "." + m[2]
}

// skewCheck: KGWUI-022. Proxies or controller replicas on another minor than
// the controller, and Gateway API CRDs off the expected or a shared bundle.
type skewCheck struct{}

// Name implements Check.
func (skewCheck) Name() string { return skewCheckName }

// Inputs implements Check: processes, CRDs, proxy Pods access, and the objects
// that may pin the proxy image; the settle window ends through WakeAt.
func (skewCheck) Inputs() InputSet {
	return InputSet{Process: true, Kinds: []objects.GroupKind{cache.CRDsKind, cache.ProxyPodsKind,
		{Group: objects.GatewayAPIGroup, Kind: "Gateway"}, {Group: objects.GatewayAPIGroup, Kind: "GatewayClass"},
		{Group: kinds.KgatewayGroup, Kind: "GatewayParameters"}}}
}

// skewState is the check's Store value: whether the controller was rolling
// out at the last run, and when its last rollout ended.
type skewState struct {
	rolling bool
	settled time.Time
}

// Run implements Check. Nothing is compared during a controller rollout and
// SkewSettle after it; an unobserved input is named in the result (R42).
func (c skewCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	res := gapResult(in, c.Name())
	var notes []string
	if res.Detail != "" {
		notes = append(notes, res.Detail)
	}
	crds, crdNote, crdWhy := skewCRDs(in)
	if crdNote != "" {
		notes = append(notes, crdNote)
		if res.Unavailable == "" {
			res.Unavailable = crdWhy
		}
	}
	if until, quiet, stuck := c.settle(ctx); quiet {
		ctx.WakeAt(until)
		note := skewSuppressedDetail
		if stuck {
			note = skewStuckDetail
		}
		res.Detail = strings.Join(append(notes, note), "; ")
		return nil, res
	}
	var fs []Finding
	ctrl, unknown := controllerVersion(in)
	if f, ok := replicaSkew(in); ok {
		fs = append(fs, f)
	}
	if in.Process != nil {
		for _, gw := range slices.SortedFunc(maps.Keys(in.Process.Proxies), objects.Ref.Compare) {
			f, ok, u := proxySkew(in, gw, in.Process.Proxies[gw], ctrl)
			unknown = unknown || u
			if ok {
				fs = append(fs, f)
			}
		}
	}
	if unknown {
		notes = append(notes, skewUnknownDetail)
	}
	if crds != nil {
		cf, note := crdSkew(in, crds, ctrl)
		if note != "" {
			notes = append(notes, note)
		}
		fs = append(fs, cf...)
	}
	res.Detail = strings.Join(notes, "; ")
	return fs, res
}

// settle tracks the controller's rollout in the Store and reports until when
// skew is suppressed; a stuck rollout never settles.
func (skewCheck) settle(ctx RunContext) (until time.Time, quiet, stuck bool) {
	in := ctx.Input
	old, _ := Load[*skewState](ctx.State, skewKey)
	if old == nil {
		old = &skewState{}
	}
	var r *process.Rollout
	if in.Process != nil {
		r = in.Process.Controller.Rollout
	}
	cur := &skewState{settled: old.settled}
	switch {
	case r == nil:
	case r.Progressing, r.Reason == "ProgressDeadlineExceeded":
		cur.rolling = true
	case r.SettledAt != nil:
		cur.settled = *r.SettledAt
	case old.rolling:
		cur.settled = in.Now // the end seen without a condition time
	}
	ctx.State.Set(skewKey, cur)
	if cur.rolling {
		return time.Time{}, true, !r.Progressing
	}
	until = cur.settled.Add(SkewSettle)
	return until, !cur.settled.IsZero() && in.Now.Before(until), false
}

// controllerVersion is the version the controller reports, as rule-set
// selection reads it; else the one its running Pods share, else the
// Deployment's. unknown is true when a running Pod's tag is not a version.
func controllerVersion(in Input) (v string, unknown bool) {
	var vs []string
	if in.Process != nil {
		for _, p := range in.Process.Controller.Processes {
			if p.Waiting != "" {
				continue
			}
			if versionMinor(p.Version) == "" {
				unknown = true
			} else if !slices.ContainsFunc(vs, func(x string) bool { return versionMinor(x) == versionMinor(p.Version) }) {
				vs = append(vs, p.Version)
			}
		}
	}
	if in.Install.Edition == "kgateway" && in.Install.ReportedVersion != "" {
		return in.Install.ReportedVersion, unknown
	}
	if len(vs) == 1 {
		return vs[0], unknown
	}
	if v := strings.TrimPrefix(in.Install.Version, "v"); versionMinor(v) != "" {
		return v, unknown
	}
	return "", unknown
}

// byVersion groups ps' running processes by version, in version order, with
// the refs of the Pods each group names.
func byVersion(ps []process.Process, ns string) ([]VersionPods, []objects.Ref) {
	groups := map[string]*VersionPods{}
	var refs []objects.Ref
	for _, p := range ps {
		if p.Waiting != "" {
			continue
		}
		g := groups[p.Version]
		if g == nil {
			g = &VersionPods{Version: p.Version}
			groups[p.Version] = g
		}
		if g.Total++; len(g.Pods) < maxSkewPods {
			g.Pods = append(g.Pods, p.Name)
			refs = append(refs, process.PodRef(ns, p))
		}
	}
	out := make([]VersionPods, 0, len(groups))
	for _, v := range slices.Sorted(maps.Keys(groups)) {
		out = append(out, *groups[v])
	}
	return out, refs
}

// replicaSkew is 022 on the controller Deployment when its running Pods are on two minors.
func replicaSkew(in Input) (Finding, bool) {
	d := in.Install.Deployment
	if in.Process == nil || d == nil {
		return Finding{}, false
	}
	groups, refs := byVersion(in.Process.Controller.Processes, d.Namespace)
	var minors, versions []string
	for _, g := range groups {
		if m := versionMinor(g.Version); m != "" && !slices.Contains(minors, m) {
			minors, versions = append(minors, m), append(versions, g.Version)
		}
	}
	if len(minors) < 2 {
		return Finding{}, false
	}
	r := objects.Ref{Group: "apps", Kind: "Deployment", Namespace: d.Namespace, Name: d.Name}
	return Finding{Code: CodeVersionSkew, Slug: "version-skew", Severity: SeverityWarning, Install: in.Install.ID(), Resource: &r,
		Message: skewReplicasMessage, Detail: fmt.Sprintf(skewReplicasDetail, strings.Join(versions, " and ")), Key: []string{"controller"},
		Evidence: refs, Facts: &VersionSkewEvidence{Controller: in.Install.ControllerVersion(), Replicas: groups}}, true
}

// proxySkew is 022 on gw when one of its running proxies is on another minor
// than ctrl; unknown is true when a proxy's tag is not a version.
func proxySkew(in Input, gw objects.Ref, set process.Set, ctrl string) (f Finding, ok, unknown bool) {
	groups, refs := byVersion(set.Processes, gw.Namespace)
	var skewed string
	for _, g := range groups {
		switch m := versionMinor(g.Version); {
		case m == "":
			unknown = true
		case skewed == "" && ctrl != "" && m != versionMinor(ctrl):
			skewed = g.Version
		}
	}
	if skewed == "" {
		return Finding{}, false, unknown
	}
	detail := fmt.Sprintf(skewProxyDetail, skewed, ctrl)
	if p := pinnedParams(in, gw); p != nil {
		detail += fmt.Sprintf(skewPinnedDetail, p.Namespace+"/"+p.Name)
		refs = append(refs, *p)
	}
	r := gw
	return Finding{Code: CodeVersionSkew, Slug: "version-skew", Severity: SeverityWarning, Install: in.Install.ID(), Resource: &r,
		Message: skewProxyMessage, Detail: detail, Key: []string{"proxy"}, Evidence: refs,
		Facts: &VersionSkewEvidence{Controller: ctrl, Proxies: groups}}, true, unknown
}

// pinnedParams is the GatewayParameters that pins gw's proxy image with a tag
// or digest: the Gateway's own, else its class's; nil when none does.
func pinnedParams(in Input, gw objects.Ref) *objects.Ref {
	u := in.stored(gw)
	if u == nil {
		return nil
	}
	if ns, name, ok := paramsRef(u, gw.Namespace, "spec", "infrastructure", "parametersRef"); ok {
		if r := pins(in, ns, name); r != nil {
			return r
		}
	}
	class := in.stored(objects.Ref{Group: objects.GatewayAPIGroup, Kind: "GatewayClass", Name: objects.GatewayClassName(u)})
	if class == nil {
		return nil
	}
	if ns, name, ok := paramsRef(class, "", "spec", "parametersRef"); ok {
		return pins(in, ns, name)
	}
	return nil
}

// paramsRef reads a GatewayParameters reference at path; a reference without
// a namespace is in ns.
func paramsRef(u *unstructured.Unstructured, ns string, path ...string) (string, string, bool) {
	m, found, _ := unstructured.NestedStringMap(u.Object, path...)
	if !found || m["group"] != kinds.KgatewayGroup || m["kind"] != "GatewayParameters" || m["name"] == "" {
		return "", "", false
	}
	if m["namespace"] != "" {
		ns = m["namespace"]
	}
	return ns, m["name"], true
}

// pins returns the ref of GatewayParameters ns/name when it sets the proxy
// image's tag or digest (spec.kube.envoyContainer.image, v2.3 and v2.4).
func pins(in Input, ns, name string) *objects.Ref {
	r := objects.Ref{Group: kinds.KgatewayGroup, Kind: "GatewayParameters", Namespace: ns, Name: name}
	p := in.stored(r)
	if p == nil {
		return nil
	}
	img, _, _ := unstructured.NestedStringMap(p.Object, "spec", "kube", "envoyContainer", "image")
	if img["tag"] == "" && img["digest"] == "" {
		return nil
	}
	return &r
}

// skewCRDs returns the Gateway API CRDs, or nil with the note and reason when
// the CRD watch is off, denied, failing or syncing (D-034).
func skewCRDs(in Input) ([]cache.CRDMeta, string, Availability) {
	a, running := in.Snapshot.Access[cache.CRDsGVR]
	switch {
	case !running:
		return nil, skewCRDsOffDetail, AvailOff
	case !a.OK && a.Err == "":
		return nil, skewCRDsSyncDetail, AvailFailing
	case !a.OK:
		return nil, a.Err, AccessAvailability(a)
	}
	return in.Snapshot.CRDs, "", ""
}

// crdLabel is a CRD's bundle and channel, as the details quote them.
func crdLabel(c cache.CRDMeta) string { return strings.TrimSpace(c.BundleVersion + " " + c.Channel) }

// crdSkew raises 022 when the common bundle is not the expected one, and on each
// CRD off it; note says why no expected bundle was compared.
func crdSkew(in Input, crds []cache.CRDMeta, ctrl string) (fs []Finding, note string) {
	var annotated []cache.CRDMeta
	count := map[string]int{}
	for _, c := range crds {
		if c.BundleVersion != "" {
			annotated = append(annotated, c)
			count[crdLabel(c)]++
		}
	}
	kgw := versionMinor(in.Install.KgatewayVersion())
	var want string
	switch {
	case kgw == "":
		note = skewCtrlUnknown
	case in.GatewayAPI != nil:
		want, _ = in.GatewayAPI(kgw)
	}
	if want == "" && note == "" {
		note = fmt.Sprintf(skewNoExpected, kgw)
	}
	if len(annotated) == 0 {
		return nil, note
	}
	// The common label is the most frequent one; a tie goes to the Gateway CRD's, else the first by name.
	rep := annotated[0]
	for _, c := range annotated {
		n, best := count[crdLabel(c)], count[crdLabel(rep)]
		if n > best || n == best && c.Name == gatewayCRD && crdLabel(c) != crdLabel(rep) {
			rep = c
		}
	}
	for _, c := range annotated {
		if c.Name == gatewayCRD && crdLabel(c) == crdLabel(rep) {
			rep = c
		}
	}
	ver := func(c cache.CRDMeta) CRDVersion {
		return CRDVersion{Name: c.Name, BundleVersion: c.BundleVersion, Channel: c.Channel}
	}
	if want != "" && versionMinor(rep.BundleVersion) != versionMinor(want) {
		fs = append(fs, crdFinding(in, rep, skewCRDMessage, fmt.Sprintf(skewCRDDetail, rep.Name, rep.BundleVersion, kgw, want), nil,
			&VersionSkewEvidence{Controller: ctrl, CRDs: []CRDVersion{ver(rep)}, Expected: want}))
	}
	for _, c := range annotated {
		if crdLabel(c) == crdLabel(rep) {
			continue
		}
		ref := crdRef(rep.Name)
		fs = append(fs, crdFinding(in, c, skewMixedMessage, fmt.Sprintf(skewMixedDetail, c.Name, crdLabel(c), crdLabel(rep)), []objects.Ref{ref},
			&VersionSkewEvidence{Controller: ctrl, CRDs: []CRDVersion{ver(c), ver(rep)}, Expected: want}))
	}
	return fs, note
}

// crdRef is the cluster-scoped ref of the CRD named name.
func crdRef(name string) objects.Ref {
	return objects.Ref{Group: cache.CRDsKind.Group, Kind: cache.CRDsKind.Kind, Name: name}
}

func crdFinding(in Input, c cache.CRDMeta, msg, detail string, deps []objects.Ref, ev *VersionSkewEvidence) Finding {
	r := crdRef(c.Name)
	return Finding{Code: CodeVersionSkew, Slug: "version-skew", Severity: SeverityWarning, Install: in.Install.ID(), Resource: &r,
		Message: msg, Detail: detail, Key: []string{c.Name}, Evidence: deps, Facts: ev}
}
