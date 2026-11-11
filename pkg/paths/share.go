package paths

import "github.com/DuncanDoyle/snellius/pkg/objects"

// ShareState is how a backend ref's share is presented (design §4.4).
type ShareState string

// Share states.
const (
	ShareShown     ShareState = "shown"
	ShareNotShown  ShareState = "not-shown"
	ShareNoTraffic ShareState = "no-traffic"
	ShareFailing   ShareState = "failing"
	ShareMayFail   ShareState = "may-fail"
	ShareMirror    ShareState = "mirror"
)

// Share is a ref's percent of its own rule's requests, never of Gateway traffic.
type Share struct {
	Percent float64
	State   ShareState
}

// Shares returns one Share per rule.BackendRefs entry, as weight over the sum
// of the rule's weights. Rules with a delegation ref show no shares.
func Shares(rule *objects.Rule) []Share {
	refs := rule.BackendRefs
	out := make([]Share, len(refs))
	var sum int64
	for _, b := range refs {
		if b.Delegation != nil {
			for i := range out {
				out[i] = Share{State: ShareNotShown}
			}
			return out
		}
		sum += int64(b.Weight)
	}
	for i, b := range refs {
		if sum == 0 {
			out[i] = Share{State: ShareNoTraffic}
			continue
		}
		out[i] = Share{Percent: float64(b.Weight) * 100 / float64(sum), State: ShareShown}
	}
	return out
}

// WithAttribution marks a shown share failing when the failure is attributed
// to its ref, or may-fail when the ref is only a candidate. The percent is kept.
func (s Share) WithAttribution(attributed, candidate bool) Share {
	if s.State != ShareShown {
		return s
	}
	switch {
	case attributed:
		s.State = ShareFailing
	case candidate:
		s.State = ShareMayFail
	}
	return s
}

// MirrorShare is a mirror's written percent (100 when unset), not a share.
func MirrorShare(m *objects.MirrorFilter) Share {
	p := 100.0
	switch {
	case m.Percent != nil:
		p = float64(*m.Percent)
	case m.Fraction != nil && m.Fraction.Denominator > 0:
		p = float64(m.Fraction.Numerator) * 100 / float64(m.Fraction.Denominator)
	}
	return Share{Percent: p, State: ShareMirror}
}
