// Package diagnose groups an install's findings by shared cause and ranks the
// groups, so the worst and widest problem comes first (design §5.2, §5.3).
package diagnose

import (
	"slices"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Class is a ranking class of findings (design §5.3); a lower class ranks first.
type Class int

// Classes, in rank order.
const (
	ClassDataPlane Class = 1 // data plane
	ClassProcesses Class = 2 // controller and proxy processes
	ClassTraffic   Class = 3 // traffic-affecting configuration
	ClassHygiene   Class = 4 // consistency and install hygiene
)

// classes maps each code outside ClassHygiene to its class; KGWUI-003 is decided by ClassOf.
var classes = map[string]Class{
	health.CodeXDSRejected: ClassDataPlane, "KGWUI-024": ClassDataPlane, "KGWUI-025": ClassDataPlane, "KGWUI-026": ClassDataPlane,
	health.CodeControllerNotReady: ClassProcesses, health.CodeProxyNotReady: ClassProcesses,
	"KGWUI-019": ClassProcesses, "KGWUI-020": ClassProcesses, "KGWUI-021": ClassProcesses,
	"KGWUI-013": ClassTraffic, "KGWUI-014": ClassTraffic, health.CodeRuleReplaced: ClassTraffic, "KGWUI-016": ClassTraffic,
	health.CodeListenerConflicted: ClassTraffic, "KGWUI-023": ClassTraffic, health.CodeDelegatedNotAttached: ClassTraffic,
}

// ClassOf returns f's class (design §5.3): KGWUI-003 is traffic once a
// warning (after grace), else hygiene; an unlisted code is hygiene.
func ClassOf(f health.Finding) Class {
	if f.Code == health.CodeRouteUnattached {
		if f.Severity == health.SeverityWarning {
			return ClassTraffic
		}
		return ClassHygiene
	}
	if c, ok := classes[f.Code]; ok {
		return c
	}
	return ClassHygiene
}

// Input is what a Family may look at to find a cause and its affected objects.
type Input struct {
	Install  install.Resolved
	Owned    *install.Owned
	Snapshot *cache.Snapshot
	Served   []kinds.Served
	Views    objects.Decoder
	// IDs, if set, memoises group IDs across recomputes.
	IDs *health.IDs
}

// Family is one kind of shared cause, a row of the design §5.2 table. Upstream families
// claim a finding's own resource as its cause; the API still checks a title's member.
type Family interface {
	// Name identifies the family; it is part of its groups' IDs.
	Name() string
	// Claim returns the cause of f in this family, or ok false.
	Claim(f health.Finding, in Input) (cause *objects.Ref, causeKey []string, ok bool)
	// Affected returns, index-aligned with gs, each group's affected objects; it runs once
	// per Build with the family's groups (valid only during the call), Members indexing fs.
	Affected(gs []Claimed, fs []health.Finding, in Input) [][]objects.Ref
}

// Claimed is one group of a family: its cause, cause key and member indexes.
type Claimed struct {
	Cause   *objects.Ref
	Key     []string
	Members []int
}

// Group is one cause with the findings it explains.
type Group struct {
	// ID is health.GroupID of the family, cause and cause key.
	ID, Family string
	// Cause is the group subject; nil for an install-wide cause.
	Cause *objects.Ref
	// Members are indexes into the report's findings, in report order.
	Members []int
	// Severity is the worst member's.
	Severity health.Severity
	// Class is the lowest class of any member.
	Class    Class
	Affected []objects.Ref
	// Since is the oldest member's since; nil when no member has one.
	Since *time.Time
	// Ranked is false for a group of KGWUI-008 findings, which sorts last
	// and is left out of the diagnostics groups.
	Ranked bool
}

// Diagnosis is an install's findings grouped and ranked.
type Diagnosis struct {
	// Groups are in rank order.
	Groups []Group
	// Rank are the report's finding indexes in rank order.
	Rank []int
	// Index serves the findings list in each order (design §13.1).
	Index *Index
}

// Build sets each finding's Group, trying families in order (first claim wins,
// else "Other" on its resource), and ranks the groups; findings keep their order.
// It reads only rep's findings and in's Install, Owned, Views and IDs: the engine reuses
// a diagnosis on exactly those, so a family must not read Snapshot or Served (Steps may).
func Build(rep *health.Report, in Input, families []Family) *Diagnosis {
	id := in.Install.ID()
	families = bindAll(rep.Findings, families)
	groups := make([]Group, 0, len(rep.Findings))
	// meta is per group: its family's index in families (len for "Other"), cause key and member count.
	type groupMeta struct {
		fam   int
		key   []string
		count int
	}
	meta := make([]groupMeta, 0, len(rep.Findings))
	at := make(map[string]int, len(rep.Findings)) // group ID to index
	of := make([]int, len(rep.Findings))          // finding to group index
	copied := false
	for i := range rep.Findings {
		f := &rep.Findings[i]
		fam, cause, key := claim(*f, in, families)
		name := "Other"
		if fam < len(families) {
			name = families[fam].Name()
		}
		gid := in.IDs.GroupID(id, name, cause, key)
		class := ClassOf(*f)
		k, ok := at[gid]
		if !ok {
			k = len(groups)
			at[gid] = k
			groups = append(groups, Group{ID: gid, Family: name, Cause: cause, Severity: f.Severity, Class: class})
			meta = append(meta, groupMeta{fam: fam, key: key})
		}
		of[i] = k
		meta[k].count++
		g := &groups[k]
		if sevRank(f.Severity) < sevRank(g.Severity) {
			g.Severity = f.Severity
		}
		g.Class = min(g.Class, class)
		if f.Since != nil && (g.Since == nil || f.Since.Before(*g.Since)) {
			g.Since = f.Since
		}
		g.Ranked = g.Ranked || f.Code != health.CodeSignalUnavailable
		// A grouped finding belongs to a report a published model may serve (health.Evaluate
		// reuses it): the first changed group copies the findings, so that slice is never written.
		if f.Group != gid {
			if f.Group != "" && !copied {
				rep.Findings, copied = slices.Clone(rep.Findings), true
				f = &rep.Findings[i]
			}
			f.Group = gid
		}
	}
	in.IDs.Sweep()
	groups = slices.Clip(groups)
	// Every group's Members is a clipped window of one backing array, filled in report order.
	backing, off := make([]int, len(rep.Findings)), 0
	for k := range meta {
		n := meta[k].count
		groups[k].Members, off = backing[off:off:off+n], off+n
	}
	for i, k := range of {
		groups[k].Members = append(groups[k].Members, i)
	}
	// One Affected call per family that claimed a group; "Other" affects nothing beyond its members.
	var cs []Claimed
	var idx []int
	for j, fam := range families {
		if _, ok := fam.(other); ok {
			continue
		}
		cs, idx = cs[:0], idx[:0]
		for k := range groups {
			if meta[k].fam == j {
				cs, idx = append(cs, Claimed{Cause: groups[k].Cause, Key: meta[k].key, Members: groups[k].Members}), append(idx, k)
			}
		}
		if len(cs) == 0 {
			continue
		}
		for n, a := range fam.Affected(cs, rep.Findings, in) {
			groups[idx[n]].Affected = a
		}
	}
	d := rank(rep.Findings, groups)
	d.Index = NewIndex(rep.Findings, d.Groups, d.Rank)
	return d
}

// binder is a family whose claims depend on the report's other findings.
type binder interface {
	bind(fs []health.Finding) Family
}

// bindAll returns families with each binder bound to fs; a copy only when one is bound.
func bindAll(fs []health.Finding, families []Family) []Family {
	out, copied := families, false
	for j, fam := range families {
		if b, ok := fam.(binder); ok {
			if !copied {
				out, copied = slices.Clone(families), true
			}
			out[j] = b.bind(fs)
		}
	}
	return out
}

// claim returns the index in families of the family claiming f first and f's
// cause in it; len(families) is the "Other" family.
func claim(f health.Finding, in Input, families []Family) (int, *objects.Ref, []string) {
	for j, fam := range families {
		if cause, key, ok := fam.Claim(f, in); ok {
			return j, cause, key
		}
	}
	cause, key, _ := other{}.Claim(f, in)
	return len(families), cause, key
}

// Families returns the families in design §5.2 table order; "Other" is last.
func Families() []Family {
	return []Family{dataPlane{}, replacedByPolicy{}, statusNotProgressing{}, processFamily{}, other{}}
}

// dataPlane groups a Gateway's KGWUI-001. Its affected objects have a current
// Accepted=True or Programmed=True status for the Gateway (design §8.4).
type dataPlane struct{}

func (dataPlane) Name() string { return "Data plane rejects" }

func (dataPlane) Claim(f health.Finding, _ Input) (*objects.Ref, []string, bool) {
	return f.Resource, nil, f.Code == health.CodeXDSRejected && f.Resource != nil
}

func (dataPlane) Affected(gs []Claimed, _ []health.Finding, in Input) [][]objects.Ref {
	gws := make(map[objects.Ref]bool, len(gs))
	for _, g := range gs {
		gws[*g.Cause] = true
	}
	byGW := explore.StatusTrueFor(in.Install, in.Owned, in.Views, gws)
	out := make([][]objects.Ref, len(gs))
	for k, g := range gs {
		out[k] = byGW[*g.Cause]
	}
	return out
}

// replacedByPolicy groups every KGWUI-015 whose message names the same policy
// (design §7.4); a 015 that names none stays in "Other". Its routes are affected.
type replacedByPolicy struct{}

func (replacedByPolicy) Name() string { return "Replaced by policy" }

func (replacedByPolicy) Claim(f health.Finding, _ Input) (*objects.Ref, []string, bool) {
	ev, ok := f.Facts.(*health.RuleReplacedEvidence)
	if f.Code != health.CodeRuleReplaced || !ok || ev.Policy == nil {
		return nil, nil, false
	}
	return ev.Policy, nil, true
}

func (replacedByPolicy) Affected(gs []Claimed, fs []health.Finding, _ Input) [][]objects.Ref {
	out := make([][]objects.Ref, len(gs))
	for k, g := range gs {
		for _, i := range g.Members {
			if r := fs[i].Resource; r != nil && !slices.Contains(out[k], *r) {
				out[k] = append(out[k], *r)
			}
		}
		slices.SortFunc(out[k], objects.Ref.Compare)
	}
	return out
}

// statusNotProgressing groups KGWUI-018 with every KGWUI-003 warning of the
// install, which it explains (design §5.2, R77). The cause is 018's controller
// Deployment; the 003s' objects are affected. Unbound, it claims only 018.
type statusNotProgressing struct{ cause *objects.Ref }

func (statusNotProgressing) Name() string { return "Status not progressing" }

func (statusNotProgressing) bind(fs []health.Finding) Family {
	for i := range fs {
		if fs[i].Code == health.CodeStatusNotProgressing && fs[i].Resource != nil {
			return statusNotProgressing{cause: fs[i].Resource}
		}
	}
	return statusNotProgressing{}
}

func (s statusNotProgressing) Claim(f health.Finding, _ Input) (*objects.Ref, []string, bool) {
	switch {
	case f.Code == health.CodeStatusNotProgressing:
		return f.Resource, nil, f.Resource != nil
	case f.Code == health.CodeRouteUnattached && f.Severity == health.SeverityWarning:
		return s.cause, nil, s.cause != nil
	}
	return nil, nil, false
}

func (statusNotProgressing) Affected(gs []Claimed, fs []health.Finding, _ Input) [][]objects.Ref {
	out := make([][]objects.Ref, len(gs))
	for k, g := range gs {
		seen := make(map[objects.Ref]bool, len(g.Members))
		for _, i := range g.Members {
			if r := fs[i].Resource; r != nil && fs[i].Code == health.CodeRouteUnattached && !seen[*r] {
				seen[*r] = true
				out[k] = append(out[k], *r)
			}
		}
		slices.SortFunc(out[k], objects.Ref.Compare)
	}
	return out
}

// processFamily groups KGWUI-005, 006 and 019–021 on their subject: the
// controller Deployment, or the Gateway for proxies. The cause is the
// finding's own resource, so a group id hashes only what its row shows (D-040).
type processFamily struct{}

func (processFamily) Name() string { return "Process" }

func (processFamily) Claim(f health.Finding, _ Input) (*objects.Ref, []string, bool) {
	switch f.Code {
	case health.CodeControllerNotReady, health.CodeProxyNotReady, health.CodeProcessRestarting, health.CodeProbeFailing, health.CodeRolloutStuck:
		return f.Resource, nil, f.Resource != nil
	}
	return nil, nil, false
}

func (processFamily) Affected(gs []Claimed, _ []health.Finding, _ Input) [][]objects.Ref {
	return make([][]objects.Ref, len(gs))
}

// other is the catch-all family: a cause is the finding's own resource.
// KGWUI-008 findings form groups of their own, so they never mix with ranked ones.
type other struct{}

func (other) Name() string { return "Other" }

func (other) Claim(f health.Finding, _ Input) (*objects.Ref, []string, bool) {
	if f.Code == health.CodeSignalUnavailable {
		return f.Resource, []string{f.Code}, true
	}
	return f.Resource, nil, true
}

func (other) Affected(gs []Claimed, _ []health.Finding, _ Input) [][]objects.Ref {
	return make([][]objects.Ref, len(gs))
}
