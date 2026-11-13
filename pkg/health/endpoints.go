package health

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// EndpointsHold is how long a Service port must have no ready endpoint
// before KGWUI-023 is raised (design §11).
const EndpointsHold = 30 * time.Second

// endpointsStartsKey holds, per Service port, when it was first seen with no ready endpoint.
const endpointsStartsKey = "starts"

// Wording of KGWUI-023 and its availability (design §11). Draft wording, finalised in the wording pass.
const (
	endpointsMessage    = "This backend has no ready endpoints"
	endpointsDetail     = "Service `%s` has no ready endpoints for port %d, so HTTP requests to it get a 503."
	endpointsOffDetail  = "Services and EndpointSlices are not watched"
	endpointsSyncDetail = "Services and EndpointSlices are not synced yet"
	endpointsHoldReason = "Waiting for backends to get ready endpoints"
	endpointsNSDetail   = "Services or EndpointSlices are not available in namespaces %s"
)

// endpointsCheck: KGWUI-023, one per Service port that a resolved core Service
// backendRef names and that had no ready endpoint for EndpointsHold.
type endpointsCheck struct{}

// Name implements Check.
func (endpointsCheck) Name() string { return "backend endpoints" }

// Inputs implements Check: owned routes and their status, Services and
// EndpointSlices; the hold ends through WakeAt.
func (endpointsCheck) Inputs() InputSet {
	return InputSet{Owned: true, Kinds: []objects.GroupKind{cache.ServicesKind, cache.EndpointSlicesKind}}
}

// servicePort is one Service port a backendRef names, as the backendRef writes it.
type servicePort struct {
	ns, name string
	port     int32
}

func (p servicePort) key() string { return p.ns + "/" + p.name + ":" + strconv.Itoa(int(p.port)) }

// Run implements Check. Endpoints off, denied, failing or syncing make it not
// available (D-034); holds then restart. In a scoped snapshot where only some
// namespaces cannot be read, it judges the rest and stays not available.
func (c endpointsCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	at := takenAt(in)
	why, detail := endpointsAvailability(in.Snapshot)
	scoped := len(in.Snapshot.RequestedNamespaces) > 0
	if why != "" && (!scoped || !slices.ContainsFunc(in.Snapshot.RequestedNamespaces, func(ns string) bool { return endpointsReadable(in.Snapshot, ns) })) {
		ctx.State.Set(endpointsStartsKey, nil)
		return nil, NotAvailable(c.Name(), why, detail, at)
	}
	// unread are the namespaces a scoped snapshot cannot judge: requested ones not OK, and referenced ones not read.
	unread := map[string]bool{}
	if scoped {
		for _, ns := range in.Snapshot.RequestedNamespaces {
			if !endpointsReadable(in.Snapshot, ns) {
				unread[ns] = true
			}
		}
	}
	result := func() CheckResult {
		if len(unread) == 0 {
			return ok(c.Name(), false, at)
		}
		if why == "" {
			why = AvailNotObserved
		}
		return NotAvailable(c.Name(), why, fmt.Sprintf(endpointsNSDetail, strings.Join(slices.Sorted(maps.Keys(unread)), ", ")), at)
	}
	if in.Owned == nil {
		return nil, result()
	}
	svcs := make(map[string]*cache.ServiceFact, len(in.Snapshot.Services))
	for i := range in.Snapshot.Services {
		s := &in.Snapshot.Services[i]
		svcs[s.Namespace+"/"+s.Name] = s
	}
	byService := map[string][]*cache.SliceFact{}
	for i := range in.Snapshot.EndpointSlices {
		s := &in.Snapshot.EndpointSlices[i]
		byService[s.Namespace+"/"+s.Service] = append(byService[s.Namespace+"/"+s.Service], s)
	}
	// Each empty Service port with the routes naming it, in first-seen order.
	var order []servicePort
	evidence := map[servicePort]*EndpointsEvidence{}
	ready := map[servicePort]bool{}
	for _, r := range in.Owned.Routes {
		if in.ignored(r.Obj) || !in.resolvedHere(r.Obj, slices.Concat(r.Parents, r.RouteParents)) {
			continue
		}
		route := objects.RefOf(r.Obj)
		for _, sp := range serviceRefs(r.Obj) {
			if scoped && !endpointsReadable(in.Snapshot, sp.ns) {
				unread[sp.ns] = true
				continue // unknown, never ready
			}
			ev := evidence[sp]
			if ev == nil {
				// A port the Service lacks is KGWUI-013's.
				if ev, ready[sp] = PortEndpoints(svcs[sp.ns+"/"+sp.name], byService[sp.ns+"/"+sp.name], sp.port); ev == nil {
					continue
				}
				evidence[sp] = ev
				order = append(order, sp)
			}
			// A ready port is never reported; routes come one at a time, so a repeat is the last entry.
			if n := len(ev.Routes); !ready[sp] && (n == 0 || ev.Routes[n-1] != route) {
				ev.Routes = append(ev.Routes, route)
			}
		}
	}
	old, _ := Load[map[string]time.Time](ctx.State, endpointsStartsKey)
	starts := map[string]time.Time{}
	var fs []Finding
	for _, sp := range order {
		ev := evidence[sp]
		if ready[sp] {
			continue // a ready endpoint ends the hold
		}
		t, seen := old[sp.key()]
		if !seen {
			t = in.Now
		}
		starts[sp.key()] = t
		if in.Now.Sub(t) < EndpointsHold {
			ctx.Hold(t.Add(EndpointsHold), 1, endpointsHoldReason)
			continue
		}
		slices.SortFunc(ev.Routes, objects.Ref.Compare)
		since := t
		fs = append(fs, Finding{Code: CodeBackendNoReadyEndpoints, Slug: "backend-no-ready-endpoints", Severity: SeverityWarning, Install: in.Install.ID(),
			Resource: &objects.Ref{Kind: "Service", Namespace: sp.ns, Name: sp.name}, Message: endpointsMessage,
			Detail: fmt.Sprintf(endpointsDetail, sp.ns+"/"+sp.name, sp.port), Since: &since, Key: []string{strconv.Itoa(int(sp.port))}, Facts: ev})
	}
	ctx.State.Set(endpointsStartsKey, starts)
	slices.SortFunc(fs, func(a, b Finding) int {
		return cmp.Or(a.Resource.Compare(*b.Resource), cmp.Compare(a.Facts.(*EndpointsEvidence).Port, b.Facts.(*EndpointsEvidence).Port))
	})
	return fs, result()
}

// endpointsAvailability says why Services or EndpointSlices cannot feed 023:
// a denied watch wins over a failing one; "" when both are OK.
func endpointsAvailability(snap *cache.Snapshot) (Availability, string) {
	var why Availability
	var detail string
	for _, gvr := range []schema.GroupVersionResource{cache.ServicesGVR, cache.EndpointSlicesGVR} {
		a, running := snap.Access[gvr]
		switch {
		case !running:
			return AvailOff, endpointsOffDetail
		case a.OK:
		case a.Err == "":
			if why == "" {
				why, detail = AvailFailing, endpointsSyncDetail
			}
		case AccessAvailability(a) == AvailDenied:
			why, detail = AvailDenied, a.Err
		case why != AvailDenied:
			why, detail = AvailFailing, a.Err
		}
	}
	return why, detail
}

// endpointsReadable reports whether a scoped snapshot read both Services and
// EndpointSlices in namespace ns; a namespace it did not request is not read.
func endpointsReadable(snap *cache.Snapshot, ns string) bool {
	return snap.NamespaceAccess[cache.ServicesGVR][ns].OK && snap.NamespaceAccess[cache.EndpointSlicesGVR][ns].OK
}

// EndpointsRead reports whether snap holds the Services and EndpointSlices of
// namespace ns: always for a cluster-wide snapshot, else when it read both there.
func EndpointsRead(snap *cache.Snapshot, ns string) bool {
	return len(snap.RequestedNamespaces) == 0 || endpointsReadable(snap, ns)
}

// resolvedHere reports whether route u has, on one of parents, a current
// ResolvedRefs=True written by this install's controller.
func (in Input) resolvedHere(u *unstructured.Unstructured, parents []objects.ParentRef) bool {
	for _, st := range in.routeParents(u) {
		if !slices.Contains(parents, st.ParentRef) || !slices.Contains(in.Install.ControllerNames, st.ControllerName) {
			continue
		}
		if c := objects.FindCondition(st.Conditions, "ResolvedRefs"); c != nil && c.Status == metav1.ConditionTrue && !objects.IsStale(u, *c) {
			return true
		}
	}
	return false
}

// serviceRefs returns route u's rule backendRefs to a core Service with a
// port and a weight above 0 (unset is 1). Mirror targets are not rule backendRefs.
func serviceRefs(u *unstructured.Unstructured) []servicePort {
	rules, _, _ := unstructured.NestedSlice(u.Object, "spec", "rules")
	var out []servicePort
	for _, r := range rules {
		for _, b := range asList(asMap(r)["backendRefs"]) {
			m := asMap(b)
			group, _ := m["group"].(string)
			kind, hasKind := m["kind"].(string)
			name, _ := m["name"].(string)
			port, hasPort := m["port"].(int64)
			weight, hasWeight := m["weight"].(int64)
			if group != "" || hasKind && kind != "Service" || name == "" || !hasPort || hasWeight && weight <= 0 {
				continue
			}
			ns, _ := m["namespace"].(string)
			if ns == "" {
				ns = u.GetNamespace()
			}
			out = append(out, servicePort{ns: ns, name: name, port: int32(port)}) //nolint:gosec // a port fits in int32
		}
	}
	return out
}

// PortEndpoints counts the ready and total endpoints of svc's port numbered
// port in slices, the Service's slices, as KGWUI-023 does. It returns nil for
// a nil or ExternalName Service, or a port the Service lacks.
func PortEndpoints(svc *cache.ServiceFact, slices []*cache.SliceFact, port int32) (ev *EndpointsEvidence, anyReady bool) {
	if svc == nil || svc.Type == "ExternalName" {
		return nil, false
	}
	i := servicePortIndex(svc.Ports, port)
	if i < 0 {
		return nil, false
	}
	ev, anyReady = sliceCounts(slices, svc.Ports[i].Name)
	ev.Service, ev.Port = svc.Name, port
	return ev, anyReady
}

// servicePortIndex is the index of the TCP Service port numbered port (route
// backends are TCP), else of any port with that number; -1 for none.
func servicePortIndex(ps []cache.ServicePort, port int32) int {
	if i := slices.IndexFunc(ps, func(p cache.ServicePort) bool { return p.Port == port && (p.Protocol == "" || p.Protocol == "TCP") }); i >= 0 {
		return i
	}
	return slices.IndexFunc(ps, func(p cache.ServicePort) bool { return p.Port == port })
}

// sliceCounts counts ready and total endpoints for port name per address
// family and reports the larger family, since a dual-stack Service lists its
// Pods once per family. Slices counts every slice of the Service.
func sliceCounts(ss []*cache.SliceFact, name string) (ev *EndpointsEvidence, anyReady bool) {
	type counts struct{ ready, total int }
	fams := map[string]counts{}
	for _, s := range ss {
		if i := slices.IndexFunc(s.Ports, func(p cache.SlicePort) bool { return p.Name == name }); i >= 0 && i < len(s.Ready) {
			n := s.Ready[i]
			anyReady = anyReady || n > 0
			c := fams[s.AddressType]
			c.ready, c.total = c.ready+n, c.total+s.Total
			fams[s.AddressType] = c
		}
	}
	ev = &EndpointsEvidence{PortName: name, Slices: len(ss)}
	for _, c := range fams {
		if c.total > ev.Total || c.total == ev.Total && c.ready > ev.Ready {
			ev.Ready, ev.Total = c.ready, c.total
		}
	}
	return ev, anyReady
}
