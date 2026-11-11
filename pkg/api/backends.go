package api

import (
	"cmp"
	"context"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/endpoints"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
)

// WithBackendConsumers gives each install's non-route backend consumers,
// such as GatewayExtensions (design §8.3). Without it there are none.
func WithBackendConsumers(f func(install.Install, *install.Owned) []objects.Consumer) HandlerOption {
	return func(c *handlerConfig) { c.consumersFor = f }
}

// backendsInput is what the backends of install i read for the caller of v, over t.
func (h *handler) backendsInput(p *engine.Published, i *engine.InstallModel, v *view, t *paths.Topology) paths.BackendsInput {
	in := paths.BackendsInput{ProjectInput: projectInput(p, i), Served: p.Model.Served}
	if h.consumersFor != nil {
		in.Consumers = h.consumersFor(i.Resolved.Install, i.Owned)
	}
	if !v.all {
		in.Visible = func(d explore.DepSet) bool { return v.allowTable(t, d) }
		in.Can = v.can
	}
	return in
}

// backendList is GET …/backends (design §8.1): the install's backends as the
// caller may see them, problems first by default.
func (h *handler) backendList(w http.ResponseWriter, r *http.Request) {
	p, i := h.find(w, r)
	if i == nil {
		return
	}
	q, err := parseBackendsQuery(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	v := h.view(r)
	t, proj, _ := h.pathModel(p, i)
	set := paths.Backends(t, proj, h.backendsInput(p, i, v, t))
	pg, err := paths.RunBackends(set, q)
	if err != nil {
		fail(w, err)
		return
	}
	l := linkerFor(v, p.Model, i)
	sets := make([][]paths.Signal, 0, len(pg.Items))
	for _, k := range pg.Items {
		sets = append(sets, set.Items[k].Problem.Signals)
	}
	titles := signalTitles(i, sets...)
	rows := make([]BackendRowDTO, 0, len(pg.Items))
	for _, k := range pg.Items {
		b := &set.Items[k]
		row := BackendRowDTO{Ref: l.linked(b.Ref), Kind: backendKind(b), Endpoints: make([]EndpointsDTO, 0, len(b.Endpoints)),
			Routes: len(b.Routes), Gateways: len(b.Gateways), Consumers: len(b.Consumers), Confirmed: b.Confirmed, Problem: problemDTO(l, titles, b.Problem)}
		for _, e := range b.Endpoints {
			row.Endpoints = append(row.Endpoints, endpointsDTO(e))
		}
		rows = append(rows, row)
	}
	writeJSON(w, BackendsResponse{Install: i.Resolved.ID(), Rows: rows, Next: pg.Next, Prev: pg.Prev, Hidden: set.Hidden,
		Facets: backendFacetsDTO(l, pg.Facets), CollectedAt: wireTime(p.ConfirmedAt)})
}

// backendFacetsDTO is f on the wire; every facet's Gateway carries a visible path.
func backendFacetsDTO(l linker, f paths.BackendFacets) *BackendFacetsDTO {
	out := &BackendFacetsDTO{Gateways: make([]BackendGatewayFacetDTO, 0, len(f.Gateways)),
		Namespaces: make([]BackendNamespaceFacetDTO, 0, len(f.Namespaces))}
	for _, g := range f.Gateways {
		out.Gateways = append(out.Gateways, BackendGatewayFacetDTO{Ref: l.linked(g.Ref), Backends: g.Backends})
	}
	for _, n := range f.Namespaces {
		out.Namespaces = append(out.Namespaces, BackendNamespaceFacetDTO(n))
	}
	return out
}

// backendKind is the list's kind column: "Backend · <spec.type>" for a Backend with a type, else the ref's kind.
func backendKind(b *paths.Backend) string {
	if b.Type != "" {
		return b.Ref.Kind + " · " + b.Type
	}
	return b.Ref.Kind
}

// endpointsDTO is a cached summary on the wire.
func endpointsDTO(e *paths.Endpoints) EndpointsDTO {
	out := EndpointsDTO{Source: EndpointsSourceKubernetes, ReadAt: wireTime(e.ReadAt), Complete: e.Complete, Ready: e.Ready, Total: e.Total}
	if e.Port != 0 {
		out.Port = &PortDTO{Port: e.Port, Name: e.PortName}
	}
	return out
}

// backendDetail is GET …/backends/{group}/{kind}/{ns}/{name} (design §8.2).
// The backend is authorised before anything is read, the scoped reader
// included; a hidden or unlisted backend is a 404, as other details are.
func (h *handler) backendDetail(w http.ResponseWriter, r *http.Request) {
	p, i := h.find(w, r)
	if i == nil {
		return
	}
	group := r.PathValue("group")
	if group == "-" {
		group = ""
	}
	ref := objects.Ref{Group: group, Kind: r.PathValue("kind"), Namespace: r.PathValue("ns"), Name: r.PathValue("name")}
	v := h.view(r)
	if !v.can(ref) {
		writeErr(w, http.StatusNotFound, notFoundInScope)
		return
	}
	t, proj, kept := h.pathModel(p, i)
	in := h.backendsInput(p, i, v, t)
	in.Only = &ref
	set := paths.Backends(t, proj, in)
	if len(set.Items) == 0 {
		writeErr(w, http.StatusNotFound, notFoundInScope)
		return
	}
	b := &set.Items[0]
	exposure, err := pathsPage(p, i, v, t, proj, kept, paths.Query{Backends: []objects.Ref{ref}})
	if err != nil {
		fail(w, err)
		return
	}
	l := linkerFor(v, p.Model, i)
	titles := signalTitles(i, b.Problem.Signals)
	out := BackendDetail{Install: i.Resolved.ID(), Ref: l.linked(ref), Kind: backendKind(b), Type: b.Type, Problem: problemDTO(l, titles, b.Problem),
		Status: problemDTO(l, titles, paths.Problem{Signals: b.Own}).Signals, Exposure: exposure, Consumers: make([]ConsumerDTO, 0, len(b.Consumers)),
		Sources: []EndpointsDTO{}, PortChain: []PortChainDTO{}, Explanations: []RefExplanationDTO{}, CollectedAt: wireTime(p.ConfirmedAt)}
	for _, c := range b.Consumers {
		out.Consumers = append(out.Consumers, ConsumerDTO{Consumer: l.linked(c.Consumer), Role: c.Role, Field: c.Field, Port: c.Port})
	}
	for _, e := range refExplanations(i, b.Routes, ref) {
		if v.can(e.route) {
			out.Explanations = append(out.Explanations, explanationDTO(v, l, e))
		}
	}
	switch {
	case ref.Group == "" && ref.Kind == "Service":
		h.serviceDetail(r.Context(), &out, p.Model, i.Resolved.ID(), b)
	default:
		if u, _ := stored(p.Model, ref); u != nil {
			out.Conditions = conditions(u, objects.Conditions(u))
		}
		for _, e := range b.Endpoints {
			out.Sources = append(out.Sources, endpointsDTO(e))
		}
	}
	writeJSON(w, out)
}

// serviceDetail fills a Service's facts, its endpoints per referenced port
// and its port chain (design §9). The caller has authorised the Service.
func (h *handler) serviceDetail(ctx context.Context, out *BackendDetail, m *engine.Model, install string, b *paths.Backend) {
	svc, ss := serviceFacts(m.Snapshot, b.Ref)
	if svc != nil {
		out.Service = &ServiceFactDTO{Type: svc.Type, Ports: make([]PortDTO, 0, len(svc.Ports))}
		for _, sp := range svc.Ports {
			out.Service.Ports = append(out.Service.Ports, PortDTO{Port: sp.Port, Name: sp.Name})
		}
	}
	external := svc != nil && svc.Type == "ExternalName"
	var res endpoints.Result
	if !external && len(b.Ports) > 0 {
		var err error
		if res, err = h.scoped.Endpoints(ctx, install, b.Ref); err != nil || res.Complete == "" {
			res.Complete = cmp.Or(res.Complete, endpoints.Failed)
		}
	}
	read := res.Complete == endpoints.Complete || res.Complete == endpoints.Partial
	cached := map[int32]*paths.Endpoints{}
	for _, e := range b.Endpoints {
		cached[e.Port] = e
	}
	for _, port := range b.Ports {
		var sp *cache.ServicePort
		if svc != nil {
			sp = servicePort(svc.Ports, port)
		}
		src := EndpointsDTO{Source: EndpointsSourceKubernetes, Port: &PortDTO{Port: port}, Complete: res.Complete, ReadAt: wireTime(res.ReadAt)}
		chain := PortChainDTO{RefPort: port, SlicePorts: []int32{}, NoSuchPort: svc != nil && sp == nil}
		c := cached[port]
		switch {
		case external:
			src.Complete, src.ReadAt = paths.EndpointsNotApplicable, wireTime(m.Snapshot.TakenAt)
		case read && sp == nil:
			// Without the Service port's name the read cannot be split, so this port has no endpoints to show.
			src.Complete = endpoints.Unavailable
		case read:
			src.Items = portItems(res.Items, sp.Name)
			if res.Complete == endpoints.Complete {
				src.Ready, src.Total = familyCounts(src.Items)
			} else if c != nil && c.Complete == paths.EndpointsComplete {
				src.Ready, src.Total, src.ReadAt = c.Ready, c.Total, wireTime(c.ReadAt)
			}
		case c != nil && c.Complete == paths.EndpointsComplete:
			// The scoped read established nothing: the cached counts show, with their own read time.
			src.Ready, src.Total, src.ReadAt = c.Ready, c.Total, wireTime(c.ReadAt)
		}
		if sp != nil {
			src.Port.Name = sp.Name
			n := sp.Port
			chain.ServicePort, chain.ServicePortName, chain.TargetName = &n, sp.Name, sp.TargetName
			if sp.Target != 0 {
				chain.TargetPort = strconv.Itoa(int(sp.Target))
			}
			chain.SlicePorts = slicePorts(ss, res.Items, read, sp.Name)
			chain.NoSlicePort = res.Complete == endpoints.Complete && len(chain.SlicePorts) == 0
		}
		out.Sources = append(out.Sources, src)
		out.PortChain = append(out.PortChain, chain)
	}
}

// serviceFacts are the cached fact of Service ref, nil when absent, and its
// slices. Services are sorted by "namespace/name"; slices are scanned.
func serviceFacts(snap *cache.Snapshot, ref objects.Ref) (*cache.ServiceFact, []*cache.SliceFact) {
	if snap == nil {
		return nil, nil
	}
	j, ok := slices.BinarySearchFunc(snap.Services, ref.Namespace+"/"+ref.Name, func(s cache.ServiceFact, key string) int {
		return strings.Compare(s.Namespace+"/"+s.Name, key)
	})
	if !ok {
		return nil, nil
	}
	var ss []*cache.SliceFact
	for k := range snap.EndpointSlices {
		if s := &snap.EndpointSlices[k]; s.Namespace == ref.Namespace && s.Service == ref.Name {
			ss = append(ss, s)
		}
	}
	return &snap.Services[j], ss
}

// servicePort is the TCP port numbered port in ps (route backends are TCP),
// else any port with that number, as KGWUI-023 matches it; nil for none.
func servicePort(ps []cache.ServicePort, port int32) *cache.ServicePort {
	i := slices.IndexFunc(ps, func(p cache.ServicePort) bool { return p.Port == port && (p.Protocol == "" || p.Protocol == "TCP") })
	if i < 0 {
		i = slices.IndexFunc(ps, func(p cache.ServicePort) bool { return p.Port == port })
	}
	if i < 0 {
		return nil
	}
	return &ps[i]
}

// portItems are the read's endpoints on the slice port named name.
func portItems(items []endpoints.Endpoint, name string) []EndpointDTO {
	out := []EndpointDTO{}
	for _, e := range items {
		if e.PortName == name {
			out = append(out, EndpointDTO{Address: e.Address, Port: e.Port, Ready: e.Ready, Serving: e.Serving, Terminating: e.Terminating, Pod: e.Pod})
		}
	}
	return out
}

// familyCounts counts ready and total endpoints in the address family with
// the most, as KGWUI-023 counts a dual-stack Service's slices.
func familyCounts(items []EndpointDTO) (ready, total *int) {
	var fams [3]struct{ ready, total int } // IPv4, IPv6, other
	for _, e := range items {
		f := 2
		if a, err := netip.ParseAddr(e.Address); err == nil {
			f = 1
			if a.Is4() {
				f = 0
			}
		}
		fams[f].total++
		if e.Ready {
			fams[f].ready++
		}
	}
	r, n := 0, 0
	for _, c := range fams {
		if c.total > n || c.total == n && c.ready > r {
			r, n = c.ready, c.total
		}
	}
	return &r, &n
}

// slicePorts are the distinct port numbers observed for the port named
// name, in the cached slices and, when read, the read's endpoints; sorted.
func slicePorts(ss []*cache.SliceFact, items []endpoints.Endpoint, read bool, name string) []int32 {
	out := []int32{}
	add := func(p int32) {
		if i, ok := slices.BinarySearch(out, p); !ok {
			out = slices.Insert(out, i, p)
		}
	}
	for _, s := range ss {
		for _, p := range s.Ports {
			if p.Name == name && p.Port != 0 {
				add(p.Port)
			}
		}
	}
	if read {
		for _, e := range items {
			if e.PortName == name && e.Port != 0 {
				add(e.Port)
			}
		}
	}
	return out
}
