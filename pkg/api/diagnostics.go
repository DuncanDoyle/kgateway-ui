package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/diagnose"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Sizes of the diagnostics payload (design §5.2, §6).
const (
	maxGroups        = 10
	maxGroupAffected = 5
)

// xdsCheckName is the check whose failure leaves the data plane unknown.
const xdsCheckName = "xDS rejects"

// hiddenTitle is a group's title when the caller may not view its top member's resource.
const hiddenTitle = "Details hidden"

// WithCommands sets the next-step commands; the default is diagnose.Kubectl.
func WithCommands(c diagnose.Commands) HandlerOption {
	return func(cfg *handlerConfig) { cfg.commands = c }
}

// xdsPlain is health.XDSRejectsEvidence without its methods.
type xdsPlain health.XDSRejectsEvidence

// XDSEvidence is KGWUI-001's evidence on the wire: recently changed objects
// are those the caller may view, each with its link (§14 rule 14).
type XDSEvidence struct {
	xdsPlain
	RecentlyChanged []LinkedRef `json:"recentlyChanged,omitempty"`
}

// EvidenceKind implements health.Evidence.
func (*XDSEvidence) EvidenceKind() string { return health.EvidenceXDSRejects }

// MarshalJSON adds the kind tag.
func (e *XDSEvidence) MarshalJSON() ([]byte, error) {
	type plain XDSEvidence
	return json.Marshal(struct {
		Kind string `json:"kind"`
		*plain
	}{health.EvidenceXDSRejects, (*plain)(e)})
}

// rulePlain is health.RuleReplacedEvidence without its methods.
type rulePlain health.RuleReplacedEvidence

// RuleEvidence is KGWUI-015's evidence on the wire: the policy carries its link.
type RuleEvidence struct {
	rulePlain
	Policy *LinkedRef `json:"policy,omitempty"`
}

// EvidenceKind implements health.Evidence.
func (*RuleEvidence) EvidenceKind() string { return health.EvidenceRuleReplaced }

// MarshalJSON adds the kind tag.
func (e *RuleEvidence) MarshalJSON() ([]byte, error) {
	type plain RuleEvidence
	return json.Marshal(struct {
		Kind string `json:"kind"`
		*plain
	}{health.EvidenceRuleReplaced, (*plain)(e)})
}

// conflictPlain is health.ListenerConflictEvidence without its methods.
type conflictPlain health.ListenerConflictEvidence

// ConflictEvidence is KGWUI-017's evidence on the wire: each other listener's owner carries its link.
type ConflictEvidence struct {
	conflictPlain
	Others []ConflictListener `json:"others,omitempty"`
}

// ConflictListener is a health.ConflictingListener whose owner carries its link.
type ConflictListener struct {
	Owner    LinkedRef `json:"owner"`
	Name     string    `json:"name"`
	Protocol string    `json:"protocol,omitempty"`
	Hostname string    `json:"hostname,omitempty"`
}

// EvidenceKind implements health.Evidence.
func (*ConflictEvidence) EvidenceKind() string { return health.EvidenceListenerConflict }

// MarshalJSON adds the kind tag.
func (e *ConflictEvidence) MarshalJSON() ([]byte, error) {
	type plain ConflictEvidence
	return json.Marshal(struct {
		Kind string `json:"kind"`
		*plain
	}{health.EvidenceListenerConflict, (*plain)(e)})
}

// decodeEvidence decodes evidence as the API sends it: xds-rejects as
// XDSEvidence, rule-replaced as RuleEvidence, listener-conflict as ConflictEvidence.
func decodeEvidence(b []byte) (health.Evidence, error) {
	ev, err := health.DecodeEvidence(b)
	if err != nil {
		return ev, err
	}
	var out health.Evidence
	switch ev.(type) {
	case *health.XDSRejectsEvidence:
		out = &XDSEvidence{}
	case *health.RuleReplacedEvidence:
		out = &RuleEvidence{}
	case *health.ListenerConflictEvidence:
		out = &ConflictEvidence{}
	default:
		return ev, nil
	}
	return out, json.Unmarshal(b, out)
}

// UnmarshalJSON decodes the typed evidence by its kind; Go clients decode a detail with it.
func (d *FindingDetail) UnmarshalJSON(b []byte) error {
	type plain FindingDetail
	w := struct {
		*plain
		Evidence json.RawMessage `json:"evidence"`
	}{plain: (*plain)(d)}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	ev, err := decodeEvidence(w.Evidence)
	d.Evidence = ev
	return err
}

// visibleNames is names without those whose mapped object v may not view (§14); nil when none is left.
func visibleNames(v *view, names []string, objs map[string]objects.Ref) []string {
	out := slices.DeleteFunc(slices.Clone(names), func(n string) bool {
		o, mapped := objs[n]
		return mapped && !v.can(o)
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

// wireEvidence is e for the caller of v, times in UTC: KGWUI-001 keeps the
// recently changed objects v may view; KGWUI-015 links its policy and KGWUI-017
// its other listeners' owners. The model's evidence is never changed.
func wireEvidence(v *view, l linker, e health.Evidence) health.Evidence {
	switch ev := e.(type) {
	case *health.XDSRejectsEvidence:
		out := &XDSEvidence{xdsPlain: xdsPlain(*ev)}
		out.xdsPlain.RecentlyChanged = nil
		out.Types = slices.Clone(ev.Types)
		for i := range out.Types {
			out.Types[i].FirstSeen = wireTime(out.Types[i].FirstSeen)
		}
		out.ProxyStats = slices.Clone(ev.ProxyStats)
		for i := range out.ProxyStats {
			ts := slices.Clone(out.ProxyStats[i].Types)
			for j := range ts {
				ts[j].RejectingSince = wirePtr(ts[j].RejectingSince)
				ts[j].EnvoyResources = visibleNames(v, ts[j].EnvoyResources, ts[j].Objects)
				ts[j].Objects = nil
			}
			out.ProxyStats[i].Types = ts
		}
		for _, r := range ev.RecentlyChanged {
			if v.can(r) {
				out.RecentlyChanged = append(out.RecentlyChanged, l.linked(r))
			}
		}
		out.LogEvents = slices.Clone(ev.LogEvents)
		for i := range out.LogEvents {
			le := &out.LogEvents[i]
			le.Since = wireTime(le.Since)
			le.EnvoyResources = visibleNames(v, le.EnvoyResources, le.Objects)
			le.Objects = nil
		}
		return out
	case *health.ProcessEvidence:
		out := *ev
		if p := ev.Panic; p != nil {
			c := *p
			c.ReadAt = wireTime(c.ReadAt)
			out.Panic = &c
		}
		// Each deployer error's object is filtered on its own (§14).
		out.Deployer = nil
		for _, d := range ev.Deployer {
			if d.Object == nil || v.can(*d.Object) {
				d.Last = wireTime(d.Last)
				out.Deployer = append(out.Deployer, d)
			}
		}
		if t := ev.Termination; t != nil {
			c := *t
			c.At = wireTime(c.At)
			out.Termination = &c
		}
		if p := ev.Probe; p != nil {
			c := *p
			c.Last = wireTime(c.Last)
			out.Probe = &c
		}
		return &out
	case *health.RuleReplacedEvidence:
		// The policy is in Finding.Evidence, so a caller who gets here may view it.
		out := &RuleEvidence{rulePlain: rulePlain(*ev)}
		out.rulePlain.Policy = nil
		if ev.Policy != nil {
			lr := l.linked(*ev.Policy)
			out.Policy = &lr
		}
		return out
	case *health.ListenerConflictEvidence:
		// The owners are in Finding.Evidence, so a caller who gets here may view them.
		out := &ConflictEvidence{conflictPlain: conflictPlain(*ev)}
		out.conflictPlain.Others = nil
		for _, o := range ev.Others {
			out.Others = append(out.Others, ConflictListener{Owner: l.linked(o.Owner), Name: o.Name, Protocol: o.Protocol, Hostname: o.Hostname})
		}
		return out
	case *health.StatusProgressEvidence:
		out := *ev
		out.Oldest = wireTime(ev.Oldest)
		return &out
	}
	return e
}

// wirePtr is wireTime of *t, nil for nil.
func wirePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	at := wireTime(*t)
	return &at
}

// stepsInput is what diagnose.Steps reads of i.
func stepsInput(m *engine.Model, i *engine.InstallModel) diagnose.Input {
	return diagnose.Input{Install: i.Resolved, Owned: i.Owned, Snapshot: m.Snapshot, Served: m.Served}
}

// nextSteps converts steps to the wire; nil for none.
func nextSteps(ss []diagnose.NextStep) []NextStep {
	if len(ss) == 0 {
		return nil
	}
	out := make([]NextStep, 0, len(ss))
	for _, s := range ss {
		out = append(out, NextStep{Title: s.Title, Command: s.Command, Needs: s.Needs, Note: s.Note})
	}
	return out
}

// shownSince is the since f's row shows the caller of v: none when restricted,
// the resource's when evidence-restricted (§14 rule 12).
func shownSince(v *view, f *health.Finding) *time.Time {
	switch {
	case f.Resource != nil && !v.can(*f.Resource):
		return nil
	case !v.allOf(f.Evidence):
		return f.ResourceSince
	}
	return f.Since
}

// verdict is rep's state and counts over every finding (§14 rule 18), with
// the oldest problem since the caller of v may see.
func verdict(v *view, rep *health.Report) Verdict {
	out := Verdict{State: rep.State, Counts: rep.Counts, Problems: rep.Counts[health.SeverityError] + rep.Counts[health.SeverityWarning]}
	if out.Counts == nil {
		out.Counts = map[health.Severity]int{}
	}
	for k := range rep.Findings {
		f := &rep.Findings[k]
		if f.Severity == health.SeverityInfo {
			continue
		}
		if s := shownSince(v, f); s != nil && (out.OldestSince == nil || s.Before(*out.OldestSince)) {
			out.OldestSince = s
		}
	}
	out.OldestSince = wirePtr(out.OldestSince)
	if rep.State == health.StateUnknown {
		if j := slices.IndexFunc(rep.Checks, func(c health.CheckResult) bool { return c.Required && !c.OK && c.Unavailable == "" }); j >= 0 {
			out.Reason = rep.Checks[j].Name
		}
	}
	return out
}

// groupRow is g on the wire for the caller of v; top is its first member in
// rank order. A hidden cause leaves severity, code and counts (§14 rule 13).
func (h *handler) groupRow(v *view, l linker, g *diagnose.Group, fs []health.Finding, top int, in diagnose.Input) GroupRow {
	row := GroupRow{ID: g.ID, Code: fs[top].Code, Severity: g.Severity, Class: g.Class, Members: len(g.Members), Affected: []LinkedRef{}}
	if g.Cause != nil {
		lr := l.linked(*g.Cause)
		row.Cause = &lr
		if !v.can(*g.Cause) {
			row.Restricted = true
			return row
		}
	}
	row.Title = fs[top].Message
	if r := fs[top].Resource; r != nil && !v.can(*r) {
		row.Title = hiddenTitle
	}
	refs := visibleAffected(v, g)
	row.AffectedTotal = len(refs)
	for _, r := range refs[:min(len(refs), maxGroupAffected)] {
		row.Affected = append(row.Affected, l.linked(r))
	}
	since := g.Since
	if !v.all {
		since = nil
		for _, k := range g.Members {
			if s := shownSince(v, &fs[k]); s != nil && (since == nil || s.Before(*since)) {
				since = s
			}
		}
	}
	row.Since = wirePtr(since)
	row.NextSteps = nextSteps(diagnose.Steps(*g, fs, in, h.commands, v.visible()))
	row.Grant = grantShape(v, g, &fs[top])
	return row
}

// notPermittedFamily is the name of diagnose's "Not permitted" family.
const notPermittedFamily = "Not permitted"

// grantShape is the shape of a "Not permitted" group from its top member, whose
// one attributed ref every member shares (diagnose notPermitted.Claim); nil when the
// caller may not view that member or its ref.
func grantShape(v *view, g *diagnose.Group, top *health.Finding) *GrantShape {
	ev, ok := top.Facts.(*health.RefsEvidence)
	if g.Family != notPermittedFamily || top.Resource == nil || !ok || !ev.Attributed || len(ev.Candidates) != 1 {
		return nil
	}
	to := ev.Candidates[0].Ref
	if !v.can(*top.Resource) || !v.can(to) {
		return nil
	}
	return &GrantShape{FromNamespace: top.Resource.Namespace, ToNamespace: to.Namespace, ToGroup: to.Group, ToKind: to.Kind}
}

// needsAttention reports whether g is a Needs attention group: ranked, with
// members, above info. Every caller sees it, restricted when its cause is hidden.
func needsAttention(g *diagnose.Group) bool {
	return g.Ranked && g.Severity != health.SeverityInfo && len(g.Members) > 0
}

// groupTotal is the number of Needs attention groups in d, the diagnostics groupTotal.
func groupTotal(d *diagnose.Diagnosis) int {
	n := 0
	for k := range d.Groups {
		if needsAttention(&d.Groups[k]) {
			n++
		}
	}
	return n
}

func (h *handler) diagnostics(w http.ResponseWriter, r *http.Request) {
	p, i := h.find(w, r)
	if i == nil {
		return
	}
	if vals, err := url.ParseQuery(r.URL.RawQuery); err != nil || len(vals) > 0 {
		writeErr(w, http.StatusBadRequest, badParam("diagnostics takes no parameters").Error())
		return
	}
	v := h.view(r)
	l := linkerFor(v, p.Model, i)
	d, rep, at := diagnosis(i), &i.Health, wireTime(p.ConfirmedAt)
	in := stepsInput(p.Model, i)
	out := Diagnostics{Install: i.Resolved.ID(), Verdict: verdict(v, rep), Groups: []GroupRow{}, Checks: wireChecks(rep.Checks), CollectedAt: at}
	// d.Rank lists each group's members in group order, so a group's first is its top member.
	off := 0
	for k := range d.Groups {
		g := &d.Groups[k]
		top := off
		off += len(g.Members)
		if !needsAttention(g) {
			continue
		}
		if out.GroupTotal++; len(out.Groups) < maxGroups {
			out.Groups = append(out.Groups, h.groupRow(v, l, g, rep.Findings, d.Rank[top], in))
		}
	}
	ps := newProcSummary(v, i)
	out.Processes = ProcessSummaries{Controller: ps.controllerSummary(), Proxies: ps.proxySummary()}
	out.DataPlane = dataPlane(v, i)
	writeJSON(w, out)
}

// dataPlane counts i's rejecting and withheld Gateways over every finding; a
// hidden Gateway counts only when its restricted 001 or 024 row already names it.
func dataPlane(v *view, i *engine.InstallModel) DataPlaneSummary {
	var out DataPlaneSummary
	rejecting, withheld := rejectingGateways(i), withheldGateways(i)
	out.Rejecting, out.Withheld = len(rejecting), len(withheld)
	if i.Owned != nil {
		for _, u := range i.Owned.Gateways {
			if ref := objects.RefOf(u); rejecting[ref] || withheld[ref] || v.can(ref) {
				out.Gateways++
			}
		}
	}
	out.Unavailable = xdsUnavailable(i.Health.Checks)
	return out
}

// rejectingGateways is the set of i's Gateways with a KGWUI-001.
func rejectingGateways(i *engine.InstallModel) map[objects.Ref]bool {
	return gatewaysWith(i, health.CodeXDSRejected, "")
}

// withheldGateways is the set of i's Gateways with a KGWUI-024 error; its
// warning (config released early) still updates the proxies.
func withheldGateways(i *engine.InstallModel) map[objects.Ref]bool {
	return gatewaysWith(i, health.CodeConfigWithheld, health.SeverityError)
}

// gatewaysWith is the set of resources of i's findings with code, of severity sev unless it is "".
func gatewaysWith(i *engine.InstallModel, code string, sev health.Severity) map[objects.Ref]bool {
	out := map[objects.Ref]bool{}
	for k := range i.Health.Findings {
		if f := &i.Health.Findings[k]; f.Code == code && f.Resource != nil && (sev == "" || f.Severity == sev) {
			out[*f.Resource] = true
		}
	}
	return out
}

// xdsUnavailable reports an xDS rejects check that is not OK, pending included.
func xdsUnavailable(checks []health.CheckResult) bool {
	j := slices.IndexFunc(checks, func(c health.CheckResult) bool { return c.Name == xdsCheckName })
	return j >= 0 && !checks[j].OK
}

// wireChecks is cs with UTC times, never nil.
func wireChecks(cs []health.CheckResult) []health.CheckResult {
	out := make([]health.CheckResult, len(cs))
	for k, c := range cs {
		c.CollectedAt = wireTime(c.CollectedAt)
		out[k] = c
	}
	return out
}
