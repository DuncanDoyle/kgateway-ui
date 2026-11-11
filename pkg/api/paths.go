package api

import (
	"net/http"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
)

// endpointsCheckName is the check whose availability is the endpoints capability state.
const endpointsCheckName = "backend endpoints"

// indexViews decodes HTTPRoutes through the index's decoded views, so the
// topology shares the engine's rules instead of decoding its own.
type indexViews struct {
	objects.Direct
	ix *explore.Index
}

// HTTPRoute is u's view in the index, else decoded directly.
func (d indexViews) HTTPRoute(u *unstructured.Unstructured) *objects.HTTPRouteView {
	if id, ok := d.ix.Row(objects.RefOf(u)); ok {
		if row := &d.ix.Routes[id]; row.Obj == u && row.View != nil {
			return row.View
		}
	}
	return objects.DecodeHTTPRoute(u)
}

// endpointsCap is install i's endpoints capability state for the projection:
// on, or why the endpoints check is unavailable; "" without the check.
func endpointsCap(i *engine.InstallModel) string {
	for _, c := range i.Health.Checks {
		if c.Name == endpointsCheckName {
			if c.Unavailable == "" {
				return paths.EndpointsOn
			}
			return string(c.Unavailable)
		}
	}
	return ""
}

// pathList is GET …/paths (design §5): the install's paths from the memoised
// topology and projection, authorised per row on every request (design §13).
func (h *handler) pathList(w http.ResponseWriter, r *http.Request) {
	p, i := h.find(w, r)
	if i == nil {
		return
	}
	q, err := parsePathsQuery(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	m, id := p.Model, i.Resolved.ID()
	key := paths.TopoKey{Install: id, Source: m.Snapshot.Source, Seq: m.Seq}
	if i.Computed != nil {
		key.Views = i.Computed.Generation
	}
	t, kept := h.pathMemo.Topology(key, func() *paths.Topology {
		in := paths.Input{Index: i.Index, Views: i.Computed, Delegates: i.Rules.Delegator() != nil}
		if i.Index != nil {
			in.Decode = indexViews{ix: i.Index}
		}
		return paths.Build(in)
	})
	project := func() *paths.Projection {
		return paths.Project(t, paths.ProjectInput{Model: i, Next: m.Next, Snapshot: m.Snapshot, Now: m.ComputedAt, EndpointsCap: endpointsCap(i)})
	}
	var proj *paths.Projection
	if kept {
		proj = h.pathMemo.Projection(p, id, t, project)
	} else {
		proj = project()
	}
	v := h.view(r)
	var visible func(explore.DepSet) bool
	if !v.all {
		visible = func(d explore.DepSet) bool { return v.allowTable(t, d) }
	}
	pg, err := paths.Run(t, proj, q, visible)
	if err != nil {
		fail(w, err)
		return
	}
	pg.Summary.Unmemoised = !kept
	l := linkerFor(v, m, i)
	titles := findingTitles(i)
	rows := make([]PathRowDTO, 0, len(pg.Rows))
	for _, k := range pg.Rows {
		rows = append(rows, pathRow(l, titles, t, proj, k))
	}
	sum := pathSummary(l, pg.Summary)
	if i.Computed == nil {
		sum.NotComputed = NotComputedNoAttachments
	}
	writeJSON(w, PathsResponse{Install: id, Rows: rows, Next: pg.Next, Prev: pg.Prev, Summary: sum, CollectedAt: wireTime(p.ConfirmedAt)})
}

// titleKey is a finding's code and resource.
type titleKey struct {
	code string
	ref  objects.Ref
}

// findingTitles maps each finding's code and resource to its message, the
// first finding's when several share both.
func findingTitles(i *engine.InstallModel) map[titleKey]string {
	out := map[titleKey]string{}
	for _, f := range i.Health.Findings {
		if f.Resource == nil || f.Message == "" {
			continue
		}
		k := titleKey{f.Code, *f.Resource}
		if _, ok := out[k]; !ok {
			out[k] = f.Message
		}
	}
	return out
}

// pathSummary is s on the wire; the caller may view every facet's Gateway.
func pathSummary(l linker, s paths.Summary) PathSummaryDTO {
	out := PathSummaryDTO{PathCountsDTO: PathCountsDTO{Paths: s.Paths, Routes: s.Routes, Gateways: s.Gateways, Backends: s.Backends},
		ByState: make(map[string]PathCountsDTO, len(s.ByState)), EndpointsNotRead: s.EndpointsNotRead, Hidden: s.Hidden, Unmemoised: s.Unmemoised}
	for st, c := range s.ByState {
		out.ByState[string(st)] = PathCountsDTO(c)
	}
	out.Facets = PathFacetsDTO{Gateways: make([]GatewayFacetDTO, 0, len(s.Facets.Gateways)),
		Namespaces: make([]NamespaceFacetDTO, 0, len(s.Facets.Namespaces))}
	for _, g := range s.Facets.Gateways {
		out.Facets.Gateways = append(out.Facets.Gateways, GatewayFacetDTO{Ref: l.linked(g.Ref), Paths: g.Paths})
	}
	for _, n := range s.Facets.Namespaces {
		out.Facets.Namespaces = append(out.Facets.Namespaces, NamespaceFacetDTO(n))
	}
	return out
}

// actions are the wire names of the topology's actions.
var actions = map[paths.Action]string{paths.ActionBackend: ActionBackend, paths.ActionRedirect: ActionRedirect,
	paths.ActionDirectResponse: ActionDirectResponse, paths.ActionNone: ActionNone}

// pathRow is row k of t, projected by proj, on the wire. The caller may view
// every dependency of the row, so its refs link as the linker decides.
func pathRow(l linker, titles map[titleKey]string, t *paths.Topology, proj *paths.Projection, k int32) PathRowDTO {
	r := t.Rows[k]
	ln := t.Listener(r)
	out := PathRowDTO{Gateway: l.linked(t.Gateway(r)), Listener: PathListenerDTO{Name: ln.Name, Protocol: ln.Protocol, Port: ln.Port},
		Hostnames: t.Hostnames(r), Action: actions[t.Action(r)], Problem: problemDTO(l, titles, proj.Problems[k])}
	if out.Hostnames == nil {
		out.Hostnames = []string{}
	}
	if ls := t.ListenerSet(r); ls != nil {
		lr := l.linked(*ls)
		out.ListenerSet = &lr
	}
	chain := t.Chain(r)
	out.Routes = make([]LinkedRef, len(chain))
	for j, ref := range chain {
		out.Routes[j] = l.linked(ref)
	}
	rule := t.Rule(r)
	ds := t.Delegation(r)
	out.Rule, out.Delegated, out.Note = rule.Index, len(chain) > 1, note(ds)
	switch {
	case out.Delegated && ds.Matches != nil:
		out.Matches = matchList(ds.Matches)
	case out.Delegated:
		// An unknown view C match stays null; the child's rule as written is not its match on this path.
	case chain[0].Group == objects.GatewayAPIGroup && chain[0].Kind == "HTTPRoute":
		out.Matches = matchDTOs(rule.Matches)
	default:
		out.Matches = matchList(rule.Matches)
		out.GRPCMatches = grpcMatches(rule.GRPCMatches)
	}

	sh := proj.Shares[k]
	out.ShareState = string(sh.State)
	switch sh.State {
	case paths.ShareShown, paths.ShareFailing, paths.ShareMayFail, paths.ShareNoTraffic:
		out.Share = &sh.Percent
	case paths.ShareMirror:
		out.MirrorPercent = &sh.Percent
	}
	if mf, ok := t.Mirror(r); ok {
		b := l.linked(mf.Backend)
		out.Backend, out.Port, out.Mirror = &b, mf.Port, true
	} else if b, ok := t.Backend(r); ok {
		out.Port, out.Weight = b.Port, b.Weight
		if dl := b.Delegation; dl != nil {
			out.Action = ActionDelegate
			out.Delegation = &DelegationRefDTO{Mode: dl.Mode, Namespace: dl.Namespace, Name: dl.Name, Label: dl.Label}
		} else {
			lr := l.linked(b.Ref)
			out.Backend = &lr
		}
	}
	if e := proj.Endpoints[k]; e != nil {
		out.Endpoints = &EndpointsDTO{Source: EndpointsSourceKubernetes, ReadAt: wireTime(e.ReadAt), Complete: e.Complete, Ready: e.Ready, Total: e.Total}
		if e.Port != 0 {
			out.Endpoints.Port = &PortDTO{Port: e.Port, Name: e.PortName}
		}
	}
	return out
}

// note is a row's open view C state, worst first; "" when certain.
func note(d paths.DelegState) string {
	switch {
	case d.Unknown != "":
		return NoteUnknown
	case d.Unconfirmable:
		return NoteUnconfirmable
	case d.NotComputed, d.Truncated:
		return NoteNotComputed
	}
	return ""
}

// problemDTO is p on the wire; signals is never null. A finding signal
// carries its finding's message as its title.
func problemDTO(l linker, titles map[titleKey]string, p paths.Problem) ProblemDTO {
	out := ProblemDTO{State: p.State, Signals: make([]SignalDTO, 0, len(p.Signals))}
	for _, s := range p.Signals {
		d := SignalDTO{Kind: s.Kind, Code: s.Code, State: s.State}
		if s.Kind == paths.SignalFinding {
			d.Title = titles[titleKey{s.Code, s.Ref}]
		}
		if s.Ref != (objects.Ref{}) {
			lr := l.linked(s.Ref)
			d.Ref = &lr
		}
		out.Signals = append(out.Signals, d)
	}
	return out
}

// grpcMatches is ms on the wire as written; nil stays absent.
func grpcMatches(ms []objects.GRPCMatch) []GRPCMatchDTO {
	if ms == nil {
		return nil
	}
	out := make([]GRPCMatchDTO, 0, len(ms))
	for _, m := range ms {
		d := GRPCMatchDTO{Headers: keyMatches(m.Headers)}
		if m.Method != nil {
			d.Method = &GRPCMethodDTO{Type: m.Method.Type, Service: m.Method.Service, Method: m.Method.Method}
		}
		out = append(out, d)
	}
	return out
}
