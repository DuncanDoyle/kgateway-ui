package diagnose

import (
	"cmp"
	"slices"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
)

// sevRank orders severities worst first; one outside the known set ranks last.
func sevRank(s health.Severity) int {
	switch s {
	case health.SeverityError:
		return 0
	case health.SeverityWarning:
		return 1
	case health.SeverityInfo:
		return 2
	}
	return 3
}

// compareSince orders the oldest time first and nil last.
func compareSince(a, b *time.Time) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	}
	return a.Compare(*b)
}

// compareGroups orders ranked groups first, then by severity, class, affected
// count descending, oldest since and ID (design §5.3).
func compareGroups(a, b *Group) int {
	if a.Ranked != b.Ranked {
		if a.Ranked {
			return -1
		}
		return 1
	}
	if c := cmp.Compare(sevRank(a.Severity), sevRank(b.Severity)); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Class, b.Class); c != 0 {
		return c
	}
	if c := cmp.Compare(len(b.Affected), len(a.Affected)); c != 0 {
		return c
	}
	if c := compareSince(a.Since, b.Since); c != 0 {
		return c
	}
	return cmp.Compare(a.ID, b.ID)
}

// rank sorts groups into rank order and ranks fs: by group, then severity,
// since and ID.
func rank(fs []health.Finding, groups []Group) *Diagnosis {
	slices.SortFunc(groups, func(a, b Group) int { return compareGroups(&a, &b) })
	d := &Diagnosis{Groups: groups, Rank: make([]int, 0, len(fs))}
	for _, g := range groups {
		start := len(d.Rank)
		d.Rank = append(d.Rank, g.Members...)
		slices.SortStableFunc(d.Rank[start:], func(i, j int) int {
			a, b := &fs[i], &fs[j]
			if c := cmp.Compare(sevRank(a.Severity), sevRank(b.Severity)); c != 0 {
				return c
			}
			if c := compareSince(a.Since, b.Since); c != 0 {
				return c
			}
			return cmp.Compare(a.ID, b.ID)
		})
	}
	return d
}
