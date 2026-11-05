package api

import (
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/overview"
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
)

// Processes list parameters (design §13.1); processesSingle may appear once.
var (
	processesParams = []string{"health", "q", "skew", "restarts", "limit", "cursor"}
	processesSingle = []string{"q", "skew", "restarts", "limit", "cursor"}
)

// sortProcesses keeps processes cursors apart from every other list's.
const sortProcesses = "processes"

// processCodes are the codes behind a process set's health: process and data-plane findings.
var processCodes = []string{health.CodeXDSRejected, health.CodeControllerNotReady, health.CodeProxyNotReady, health.CodeProcessRestarting,
	health.CodeProbeFailing, health.CodeRolloutStuck, health.CodeVersionSkew, health.CodeProxyConfigWarming, health.CodeProxyDisconnected}

// subjectFacts are what an install's findings say about one process subject.
type subjectFacts struct {
	worst      health.Severity
	restarting bool
	skew       bool
	reject     *health.Finding
	// restarts are the restarts each Pod's 019 counts in the window.
	restarts map[string]int32
}

// procSummary holds one request's view of an install's processes.
type procSummary struct {
	v     *view
	i     *engine.InstallModel
	facts map[objects.Ref]*subjectFacts
	ctrl  []string // running controller versions
}

func newProcSummary(v *view, i *engine.InstallModel) *procSummary {
	ps := &procSummary{v: v, i: i, facts: map[objects.Ref]*subjectFacts{}}
	for k := range i.Health.Findings {
		f := &i.Health.Findings[k]
		if f.Resource == nil || !slices.Contains(processCodes, f.Code) {
			continue
		}
		sf := ps.facts[*f.Resource]
		if sf == nil {
			sf = &subjectFacts{}
			ps.facts[*f.Resource] = sf
		}
		if f.Severity == health.SeverityError || f.Severity == health.SeverityWarning && sf.worst != health.SeverityError {
			sf.worst = f.Severity
		}
		switch f.Code {
		case health.CodeProcessRestarting:
			sf.restarting = true
			if ev, ok := f.Facts.(*health.ProcessEvidence); ok && ev.Restarts != nil {
				if sf.restarts == nil {
					sf.restarts = map[string]int32{}
				}
				sf.restarts[ev.Pod] = *ev.Restarts
			}
		case health.CodeVersionSkew:
			sf.skew = true
		case health.CodeXDSRejected:
			sf.reject = f
		}
	}
	// Controller versions come from the Pods the caller may view, never through a hidden Deployment (§14 rule 16).
	if d := i.Resolved.Deployment; d != nil && v.can(objects.Ref{Group: "apps", Kind: "Deployment", Namespace: d.Namespace, Name: d.Name}) {
		for _, p := range ps.controller().Processes {
			if p.Waiting == "" && p.Version != "" && !slices.Contains(ps.ctrl, p.Version) && v.can(process.PodRef(d.Namespace, p)) {
				ps.ctrl = append(ps.ctrl, p.Version)
			}
		}
	}
	if v := strings.TrimPrefix(i.Resolved.Version, "v"); len(ps.ctrl) == 0 && v != "" {
		ps.ctrl = []string{v}
	}
	return ps
}

// controller is the install's controller set; empty without process data.
func (ps *procSummary) controller() *process.Set {
	if ps.i.Process == nil {
		return &process.Set{}
	}
	return &ps.i.Process.Controller
}

// controllerSet is the controller set for the caller; a hidden Deployment keeps the counts only.
func (ps *procSummary) controllerSet() ProcessSetDTO {
	d := ps.i.Resolved.Deployment
	if d == nil {
		return ps.set(ps.controller(), "", nil, true)
	}
	ref := objects.Ref{Group: "apps", Kind: "Deployment", Namespace: d.Namespace, Name: d.Name}
	return ps.set(ps.controller(), d.Namespace, ps.facts[ref], ps.v.can(ref))
}

// set is s for the caller: a visible subject shows its rollout and the Pods
// the caller may view; otherwise only the counts (§14 rule 16).
func (ps *procSummary) set(s *process.Set, podNS string, sf *subjectFacts, visible bool) ProcessSetDTO {
	out := ProcessSetDTO{Observed: s.Observed, Desired: s.Desired, Processes: []ProcessDTO{}}
	if s.Observed {
		n := int32(0)
		for _, p := range s.Processes {
			if p.Ready != nil && *p.Ready {
				n++
			}
		}
		out.Ready = &n
	}
	if !visible {
		out.Restricted = true
		return out
	}
	if r := s.Rollout; r != nil {
		out.Rollout = &RolloutDTO{Progressing: r.Progressing}
		// Only a stuck rollout names its reason; the Processes page shows it as stuck.
		if !r.Progressing && r.Reason == "ProgressDeadlineExceeded" {
			out.Rollout.Reason = r.Reason
		}
	}
	for _, p := range s.Processes {
		if ps.v.can(process.PodRef(podNS, p)) {
			out.Processes = append(out.Processes, processDTO(p, sf))
		}
	}
	return out
}

// processDTO is p on the wire, times in UTC; never a termination message.
func processDTO(p process.Process, sf *subjectFacts) ProcessDTO {
	out := ProcessDTO{Name: p.Name, Ready: p.Ready, Restarts: p.Restarts, LastStart: wirePtr(p.LastStart), Waiting: p.Waiting, Version: p.Version}
	if sf != nil {
		if n, ok := sf.restarts[p.Name]; ok {
			out.RestartsRecent = &n
		}
	}
	if t := p.Termination; t != nil {
		out.Termination = &health.TerminationFacts{Reason: t.Reason, ExitCode: t.ExitCode, At: wireTime(t.At)}
	}
	if pr := p.Probe; pr != nil {
		out.Probe = &health.ProbeFacts{Type: pr.Type, Failures: pr.Count, Last: wireTime(pr.Last), HistoryUnknown: pr.HistoryUnknown}
	}
	return out
}

// controllerSummary is the controller line for the caller. Its state is the worst controller finding,
// which a restricted caller already sees as a restricted row's severity (D-013).
func (ps *procSummary) controllerSummary() ProcessSummary {
	s := ps.controllerSet()
	out := ProcessSummary{State: health.StateUnknown, Observed: s.Observed, Ready: s.Ready, Desired: s.Desired, Restricted: s.Restricted}
	if d := ps.i.Resolved.Deployment; d != nil {
		out.State = subjectState(ps.facts[objects.Ref{Group: "apps", Kind: "Deployment", Namespace: d.Namespace, Name: d.Name}], s.Observed)
	}
	if s.Restricted || !s.Observed {
		return out
	}
	for k, p := range s.Processes {
		if k == 0 {
			out.Version = p.Version
		} else if p.Version != out.Version {
			out.Version = ""
		}
	}
	return out
}

// gatewayProc is one owned Gateway's proxies with what its findings say.
type gatewayProc struct {
	ref   objects.Ref
	set   *process.Set
	facts *subjectFacts
}

// gateways are i's owned Gateways in namespace and name order.
func (ps *procSummary) gateways() []gatewayProc {
	if ps.i.Owned == nil {
		return nil
	}
	out := make([]gatewayProc, 0, len(ps.i.Owned.Gateways))
	for _, u := range ps.i.Owned.Gateways {
		ref := objects.RefOf(u)
		set := &process.Set{}
		if ps.i.Process != nil {
			if s, ok := ps.i.Process.Proxies[ref]; ok {
				set = &s
			}
		}
		out = append(out, gatewayProc{ref: ref, set: set, facts: ps.facts[ref]})
	}
	slices.SortFunc(out, func(a, b gatewayProc) int { return compareAffected(a.ref, b.ref) })
	return out
}

// subjectState is the worst of a subject's process and data-plane findings (KGWUI-005 among them);
// unknown when its processes are not observed and no finding says otherwise.
func subjectState(sf *subjectFacts, observed bool) health.State {
	switch {
	case sf != nil && sf.worst == health.SeverityError:
		return health.StateError
	case sf != nil && sf.worst == health.SeverityWarning:
		return health.StateWarning
	case !observed:
		return health.StateUnknown
	}
	return health.StateHealthy
}

// health is the worst of g's findings; unknown when its proxies are not observed.
func (g *gatewayProc) health() health.State { return subjectState(g.facts, g.set.Observed) }

func (g *gatewayProc) restarting() bool { return g.facts != nil && g.facts.restarting }

// skew reports a visible proxy on a version no visible controller replica has, or a 022.
func (ps *procSummary) skew(g *gatewayProc) bool {
	if g.facts != nil && g.facts.skew {
		return true
	}
	for _, p := range g.set.Processes {
		if p.Waiting == "" && p.Version != "" && len(ps.ctrl) > 0 && !slices.Contains(ps.ctrl, p.Version) && ps.v.can(process.PodRef(g.ref.Namespace, p)) {
			return true
		}
	}
	return false
}

// allReady reports a set with every observed proxy ready and the desired count met.
func allReady(s *process.Set) bool {
	if !s.Observed || len(s.Processes) == 0 {
		return false
	}
	for _, p := range s.Processes {
		if p.Ready == nil || !*p.Ready {
			return false
		}
	}
	return s.Desired == nil || int32(len(s.Processes)) >= *s.Desired // #nosec G115 -- a replica count fits in int32
}

// proxySummary counts the visible Gateways' proxies. State orders their health as Gateway rows do
// (overview.RowState): an unobserved or absent Gateway makes it unknown unless one errors (D-006).
func (ps *procSummary) proxySummary() ProxySummary {
	var out ProxySummary
	var worst health.State
	unknown := false
	for _, g := range ps.gateways() {
		if !ps.v.can(g.ref) {
			continue
		}
		switch g.health() {
		case health.StateError:
			worst = health.StateError
		case health.StateWarning:
			if worst != health.StateError {
				worst = health.StateWarning
			}
		case health.StateUnknown:
			unknown = true
		}
		out.Gateways++
		if allReady(g.set) {
			out.Ready++
		}
		if g.facts != nil && g.facts.reject != nil {
			out.Rejecting++
		}
		if ps.skew(&g) {
			out.Skew++
		}
		if g.restarting() {
			out.Restarting++
		}
		if !g.set.Observed {
			out.NotObserved++
		}
	}
	// No visible Gateway is no signal, never green.
	out.State = overview.RowState(worst, unknown || out.Gateways == 0)
	return out
}

// row is g on the wire; Rejects needs every dependency of its 001 (§14 rule 12).
func (ps *procSummary) row(l linker, g *gatewayProc) GatewayProcesses {
	out := GatewayProcesses{Gateway: l.linked(g.ref), Health: g.health(), Set: ps.set(g.set, g.ref.Namespace, g.facts, true), Skew: ps.skew(g)}
	if g.facts != nil && g.facts.reject != nil && ps.v.allOf(g.facts.reject.Evidence) {
		if ev, ok := g.facts.reject.Facts.(*health.XDSRejectsEvidence); ok {
			n := 0
			for _, t := range ev.Types {
				n += t.Streams
			}
			out.Rejects = &n
		}
	}
	return out
}

// processesQuery is a parsed GET …/processes query.
type processesQuery struct {
	health         []health.State
	q              string // lower case
	skew, restarts *bool
	limit          int
	cursor         string
}

func parseProcessesQuery(r *http.Request) (processesQuery, error) {
	q := processesQuery{limit: explore.DefaultLimit}
	vals, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, badParam("malformed query: %v", err)
	}
	for _, name := range slices.Sorted(maps.Keys(vals)) {
		if !slices.Contains(processesParams, name) {
			return q, badParam("unknown parameter %q", name)
		}
		if len(vals[name]) > 1 && slices.Contains(processesSingle, name) {
			return q, badParam("parameter %q given more than once", name)
		}
	}
	for _, v := range vals["health"] {
		s := health.State(v)
		if s != health.StateHealthy && s != health.StateWarning && s != health.StateError && s != health.StateUnknown {
			return q, badParam("health %q: want healthy, warning, error or unknown", v)
		}
		q.health = append(q.health, s)
	}
	for name, dst := range map[string]**bool{"skew": &q.skew, "restarts": &q.restarts} {
		if !vals.Has(name) {
			continue
		}
		switch v := vals.Get(name); v {
		case "true", "false":
			b := v == "true"
			*dst = &b
		default:
			return q, badParam("%s %q: want true or false", name, v)
		}
	}
	q.q, q.cursor = strings.ToLower(vals.Get("q")), vals.Get("cursor")
	if vals.Has("limit") {
		v := vals.Get("limit")
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > explore.MaxLimit {
			return q, badParam("limit %q: want 1–%d", v, explore.MaxLimit)
		}
		q.limit = n
	}
	return q, nil
}

// match reports whether g passes q; q matches the Gateway's identity only.
func (q *processesQuery) match(ps *procSummary, g *gatewayProc) bool {
	if len(q.health) > 0 && !slices.Contains(q.health, g.health()) ||
		q.skew != nil && *q.skew != ps.skew(g) || q.restarts != nil && *q.restarts != g.restarting() {
		return false
	}
	return q.q == "" || strings.Contains(strings.ToLower(g.ref.Namespace+"/"+g.ref.Name), q.q)
}

func (h *handler) processes(w http.ResponseWriter, r *http.Request) {
	p, i := h.find(w, r)
	if i == nil {
		return
	}
	q, err := parseProcessesQuery(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	v := h.view(r)
	l := linkerFor(v, p.Model, i)
	ps := newProcSummary(v, i)
	gws := ps.gateways()
	byRef := make(map[objects.Ref]*gatewayProc, len(gws))
	var refs []objects.Ref
	denied := 0
	for k := range gws {
		g := &gws[k]
		if !v.can(g.ref) {
			denied++
			continue
		}
		if q.match(ps, g) {
			refs = append(refs, g.ref)
			byRef[g.ref] = g
		}
	}
	pg, err := pageRefs(refs, q.cursor, q.limit, sortProcesses)
	if err != nil {
		fail(w, err)
		return
	}
	items := make([]GatewayProcesses, 0, len(pg.refs))
	for _, ref := range pg.refs {
		items = append(items, ps.row(l, byRef[ref]))
	}
	at := wireTime(p.ConfirmedAt)
	writeJSON(w, ProcessesResponse{Install: i.Resolved.ID(), Controller: ps.controllerSet(), CollectedAt: at,
		Proxies: Page[GatewayProcesses]{Install: i.Resolved.ID(), Items: items, Total: len(refs), Next: pg.next, Prev: pg.prev, DeniedInInstall: denied, CollectedAt: at}})
}
