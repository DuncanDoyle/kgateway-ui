package api

import (
	"cmp"
	"slices"
	"strings"

	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
)

// refKey is a backendRef by its rule index and its index in the rule.
type refKey struct{ rule, ref int }

// refTrace is one backendRef's visible paths, one per attachment, merged.
type refTrace struct {
	share   paths.Share
	shared  bool
	problem paths.Problem
	// atts index the serves entry each problem signal names; -1 for none.
	atts []int
	ep   *paths.Endpoints
}

// routeTrace is a route's visible paths in the path model (design §7.1): the
// attachments they run through and each backendRef's share, problem and
// endpoints. It reads the memoised projection and keeps nothing.
type routeTrace struct {
	serves []TraceAttachmentDTO
	refs   map[refKey]*refTrace
}

// trace is route's trace: the rows of the paths route= query whose rule is
// route's own, as the caller may see them.
func (d *detail) trace(route objects.Ref) routeTrace {
	t, proj, _ := d.h.pathModel(d.p, d.i)
	var visible func(explore.DepSet) bool
	if !d.v.all {
		visible = func(ds explore.DepSet) bool { return d.v.allowTable(t, ds) }
	}
	var rows []int32
	q := paths.Query{Route: &route, Limit: paths.MaxLimit}
	for {
		pg, err := paths.Run(t, proj, q, visible)
		if err != nil {
			return routeTrace{}
		}
		for _, k := range pg.Rows {
			if c := t.Chain(t.Rows[k]); c[len(c)-1] == route && d.hopsVisible(c) {
				rows = append(rows, k)
			}
		}
		if pg.Next == "" {
			break
		}
		q.Cursor = pg.Next
	}
	slices.SortFunc(rows, func(a, b int32) int {
		return cmp.Or(cmp.Compare(t.Attachment(t.Rows[a]), t.Attachment(t.Rows[b])), cmp.Compare(a, b))
	})

	// One row per attachment gives its own signals. Attachments that look the
	// same (a delegated route under several roots) share one serves entry.
	att := map[int32]int{}
	var firsts []int32
	for _, k := range rows {
		if _, ok := att[t.Attachment(t.Rows[k])]; !ok {
			att[t.Attachment(t.Rows[k])] = len(firsts)
			firsts = append(firsts, k)
		}
	}
	own := paths.AttachmentProblems(t, projectInput(d.p, d.i), firsts)
	out := routeTrace{serves: make([]TraceAttachmentDTO, 0, len(firsts)), refs: map[refKey]*refTrace{}}
	entry := make([]int, len(firsts))
	type look struct {
		gw, ls          objects.Ref
		listener, hosts string
	}
	seen := map[look]int{}
	for j, k := range firsts {
		r := t.Rows[k]
		ln, gw, ls := t.Listener(r), t.Gateway(r), t.ListenerSet(r)
		key := look{gw: gw, listener: ln.Name, hosts: strings.Join(t.Hostnames(r), "\x00")}
		if ls != nil {
			key.ls = *ls
		}
		if e, ok := seen[key]; ok {
			entry[j] = e
			if problemRank(own[j].State) < problemRank(out.serves[e].State) {
				out.serves[e].State = own[j].State
			}
			continue
		}
		s := TraceAttachmentDTO{Gateway: d.l.linked(gw), Listener: PathListenerDTO{Name: ln.Name, Protocol: ln.Protocol, Port: ln.Port},
			Hostnames: append([]string{}, t.Hostnames(r)...), State: own[j].State}
		if ls != nil {
			lr := d.l.linked(*ls)
			s.ListenerSet = &lr
		}
		seen[key], entry[j] = len(out.serves), len(out.serves)
		out.serves = append(out.serves, s)
	}

	for _, k := range rows {
		r := t.Rows[k]
		i, ok := t.RefIndex(r)
		if !ok {
			continue
		}
		key := refKey{t.Rule(r).Index, i}
		rt := out.refs[key]
		if rt == nil {
			rt = &refTrace{problem: paths.Problem{State: health.StateHealthy}}
			out.refs[key] = rt
		}
		a := att[t.Attachment(r)]
		rt.merge(proj, k, entry[a], own[a].Signals)
	}
	return out
}

// hopsVisible reports whether the caller may view every view B row of each
// delegation hop of chain, root eligibility included, as the delegation
// pages require (design §7.4 rule 4).
func (d *detail) hopsVisible(chain []objects.Ref) bool {
	if d.v.all {
		return true
	}
	for j := 1; j < len(chain); j++ {
		for _, i := range d.ix.Delegation[chain[j-1]] {
			if r := &d.ix.DelegRows[i]; r.Key.Child == chain[j] && !d.v.allowDeps(d.ix, r.Deps) {
				return false
			}
		}
	}
	return true
}

// merge adds row k, through serves entry a with its own signals, to rt: the
// worst state, each signal once per attachment it names, the worst share.
func (rt *refTrace) merge(proj *paths.Projection, k int32, a int, own []paths.Signal) {
	p := proj.Problems[k]
	if problemRank(p.State) < problemRank(rt.problem.State) {
		rt.problem.State = p.State
	}
	for _, s := range p.Signals {
		named := -1
		if slices.Contains(own, s) {
			named = a
		}
		dup := false
		for x, y := range rt.problem.Signals {
			dup = dup || y == s && rt.atts[x] == named
		}
		if !dup {
			rt.problem.Signals = append(rt.problem.Signals, s)
			rt.atts = append(rt.atts, named)
		}
	}
	if sh := proj.Shares[k]; !rt.shared || shareRank(sh.State) < shareRank(rt.share.State) {
		rt.share, rt.shared = sh, true
	}
	if rt.ep == nil {
		rt.ep = proj.Endpoints[k]
	}
}

// apply sets b's share, problem and endpoints from rt; titles are the finding titles of every trace.
func (rt *refTrace) apply(l linker, titles map[titleKey]string, b *BackendRefDTO) {
	b.ShareState = string(rt.share.State)
	switch rt.share.State {
	case paths.ShareShown, paths.ShareFailing, paths.ShareMayFail, paths.ShareNoTraffic:
		pct := rt.share.Percent
		b.Share = &pct
	}
	p := problemDTO(l, titles, rt.problem)
	for x := range p.Signals {
		if rt.atts[x] >= 0 {
			a := rt.atts[x]
			p.Signals[x].Attachment = &a
		}
	}
	b.Problem = &p
	if rt.ep != nil {
		e := endpointsDTO(rt.ep)
		b.Endpoints = &e
	}
}

// signalSets are the problem signals of every ref of tr, for signalTitles.
func (tr routeTrace) signalSets() [][]paths.Signal {
	out := make([][]paths.Signal, 0, len(tr.refs))
	for _, rt := range tr.refs {
		out = append(out, rt.problem.Signals)
	}
	return out
}

// problemRank orders problem states: error, warning, unknown, healthy.
func problemRank(s health.State) int {
	switch s {
	case health.StateError:
		return 0
	case health.StateWarning:
		return 1
	case health.StateUnknown:
		return 2
	}
	return 3
}

// shareRank puts an attributed failure before a candidate one, then the rest.
func shareRank(s paths.ShareState) int {
	switch s {
	case paths.ShareFailing:
		return 0
	case paths.ShareMayFail:
		return 1
	}
	return 2
}
