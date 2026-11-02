// Package diagnose groups an install's findings by shared cause and ranks the
// groups, so the worst and widest problem comes first (design §5.2, §5.3).
package diagnose

import (
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
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
	"KGWUI-013": ClassTraffic, "KGWUI-014": ClassTraffic, "KGWUI-015": ClassTraffic, "KGWUI-016": ClassTraffic,
	"KGWUI-017": ClassTraffic, "KGWUI-023": ClassTraffic, health.CodeDelegatedNotAttached: ClassTraffic,
}

// ClassOf returns f's class (design §5.3). A KGWUI-003 is traffic-affecting
// once its grace period is over (a warning) and hygiene before; an unlisted
// code is hygiene.
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
}

// Family is one kind of shared cause, a row of the design §5.2 table.
type Family interface {
	// Name identifies the family; it is part of its groups' IDs.
	Name() string
	// Claim returns the cause of f in this family, or ok false.
	Claim(f health.Finding, in Input) (cause *objects.Ref, causeKey []string, ok bool)
	// Affected returns the objects the cause affects. members is valid only
	// during the call; the result must not alias it.
	Affected(cause *objects.Ref, causeKey []string, members []health.Finding, in Input) []objects.Ref
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
}

// Build groups rep's findings, setting each finding's Group, and ranks them.
// Families are tried in order and the first claim wins; an unclaimed finding
// joins the "Other" group on its own resource. Build keeps the findings' order.
func Build(rep *health.Report, in Input, families []Family) *Diagnosis {
	id := in.Install.ID()
	groups := make([]Group, 0, len(rep.Findings))
	var fams []Family      // per group
	var keys [][]string    // per group, the cause key
	at := map[string]int{} // group ID to index
	for i := range rep.Findings {
		f := &rep.Findings[i]
		fam, cause, key := claim(*f, in, families)
		gid := health.GroupID(id, fam.Name(), cause, key)
		k, ok := at[gid]
		if !ok {
			k = len(groups)
			at[gid] = k
			groups = append(groups, Group{ID: gid, Family: fam.Name(), Cause: cause, Severity: f.Severity, Class: ClassOf(*f)})
			fams, keys = append(fams, fam), append(keys, key)
		}
		g := &groups[k]
		g.Members = append(g.Members, i)
		if sevRank(f.Severity) < sevRank(g.Severity) {
			g.Severity = f.Severity
		}
		g.Class = min(g.Class, ClassOf(*f))
		if f.Since != nil && (g.Since == nil || f.Since.Before(*g.Since)) {
			g.Since = f.Since
		}
		g.Ranked = g.Ranked || f.Code != health.CodeSignalUnavailable
		f.Group = gid
	}
	var buf []health.Finding
	for k := range groups {
		g := &groups[k]
		if _, ok := fams[k].(other); ok {
			continue // affects nothing beyond its members
		}
		buf = buf[:0]
		for _, m := range g.Members {
			buf = append(buf, rep.Findings[m])
		}
		g.Affected = fams[k].Affected(g.Cause, keys[k], buf, in)
	}
	return rank(rep.Findings, groups)
}

// claim returns the family claiming f first and f's cause in it, falling
// back to the "Other" family.
func claim(f health.Finding, in Input, families []Family) (Family, *objects.Ref, []string) {
	for _, fam := range families {
		if cause, key, ok := fam.Claim(f, in); ok {
			return fam, cause, key
		}
	}
	cause, key, _ := other{}.Claim(f, in)
	return other{}, cause, key
}

// Families returns the families in design §5.2 table order; "Other" is last.
func Families() []Family { return []Family{other{}} }

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

func (other) Affected(*objects.Ref, []string, []health.Finding, Input) []objects.Ref { return nil }
