package health

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrllogs"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
)

// Check names of the process findings (design §9.3).
const (
	restartsCheckName = "process restarts"
	probesCheckName   = "probe failures"
	rolloutsCheckName = "stuck rollouts"
)

const (
	// ProcessWindow is how long a restart stays KGWUI-019 (design §9.3).
	ProcessWindow = 15 * time.Minute
	// MinProbeFailures is how many observed probe failures on one Pod raise KGWUI-020.
	MinProbeFailures = 3
	// restartsKey holds the restartState in the restarts check's Store.
	restartsKey = "restarts"
	// maxRestartsSeen bounds the restart observations kept per process.
	maxRestartsSeen = 32
	// maxDeployerErrors bounds the deployer errors one finding quotes.
	maxDeployerErrors = 5
)

// Wording of the process findings (design §9.3), from the wording pass (Task 51).
const (
	restartedMessage   = "%s restarted in the last 15 minutes"
	restartedTimes     = "Pod %s restarted %s in the last 15 minutes."
	restartedOnce      = "Pod %s restarted in the last 15 minutes."
	lastExitReason     = " Last exit: %s (exit code %d)."
	lastExitCode       = " Last exit code %d."
	panicNote          = " A Go panic exits with code 2. The stack trace is in the previous container's log."
	probeMessage       = "%s probes failing"
	probeDetail        = "Events show Pod %s failed %s %d times in the last 15 minutes."
	probeUncounted     = " Earlier failures were not counted."
	rolloutMessage     = "%s rollout stuck"
	rolloutDetail      = "The rollout passed its progress deadline (ProgressDeadlineExceeded)."
	rolloutWaiting     = " Pod %s is waiting: %s."
	notReadyFacts      = "%d/%d ready: %s"
	eventsOffDetail    = "Events are turned off"
	eventsSyncDetail   = "The Events watch has not synced yet"
	eventsCappedDetail = "More than 10,000 Events, so some were dropped"
	proxiesUnchecked   = "Proxy Pods are not watched, so proxies were not checked"
)

// stuckWaiting are the waiting reasons KGWUI-021 quotes as evidence.
var stuckWaiting = []string{"ImagePullBackOff", "ErrImagePull", "InvalidImageName", "CreateContainerConfigError", "CrashLoopBackOff"}

// subject is the processes behind one finding resource: the controller
// Deployment, or a Gateway for its proxies. podNS is where their Pods are.
type subject struct {
	ref        objects.Ref
	what       string
	podNS      string
	controller bool
	set        *process.Set
}

// subjects lists in's process subjects; none without process data. An
// unobserved set has no processes, so it yields no finding (D-016 R3).
func subjects(in Input) []subject {
	if in.Process == nil {
		return nil
	}
	out := make([]subject, 0, 1+len(in.Process.Proxies))
	if d := in.Install.Deployment; d != nil {
		out = append(out, subject{ref: objects.Ref{Group: "apps", Kind: "Deployment", Namespace: d.Namespace, Name: d.Name},
			what: "Controller", podNS: d.Namespace, controller: true, set: &in.Process.Controller})
	}
	for gw, s := range in.Process.Proxies {
		out = append(out, subject{ref: gw, what: "Proxy", podNS: gw.Namespace, set: &s})
	}
	return out
}

// processFinding is a warning on s about process p, whose facts the evidence quotes.
func processFinding(in Input, s subject, code, slug, msg, detail string, ev *ProcessEvidence, p *process.Process) Finding {
	r := s.ref
	return Finding{Code: code, Slug: slug, Severity: SeverityWarning, Install: in.Install.ID(), Resource: &r,
		Message: fmt.Sprintf(msg, s.what), Detail: detail, Evidence: []objects.Ref{process.PodRef(s.podNS, *p)}, Facts: ev}
}

// proxyGap is the result detail and reason when proxy Pods are not watched
// although a Gateway has a proxy Deployment; "" when every proxy was checked.
func proxyGap(in Input) (string, Availability) {
	if in.Process == nil {
		return "", ""
	}
	a, running := in.Snapshot.Access[cache.ProxyPodsGVR]
	if running && a.OK {
		return "", ""
	}
	for _, s := range in.Process.Proxies {
		if s.Desired != nil && !s.Observed {
			why := AvailOff
			if running {
				why = AccessAvailability(a) // "" while syncing
			}
			return proxiesUnchecked, why
		}
	}
	return "", ""
}

// gapResult is ok(name) with the proxy gap as Detail and Unavailable; the
// controller findings stand beside it.
func gapResult(in Input, name string) CheckResult {
	res := ok(name, false, takenAt(in))
	res.Detail, res.Unavailable = proxyGap(in)
	return res
}

// takenAt is the time of in's process data.
func takenAt(in Input) time.Time {
	if in.Snapshot != nil {
		return in.Snapshot.TakenAt
	}
	return in.Now
}

func terminationFacts(t *process.Termination) *TerminationFacts {
	if t == nil {
		return nil
	}
	return &TerminationFacts{Reason: t.Reason, ExitCode: t.ExitCode, At: t.At.UTC()}
}

// lastExit is " Last exit: <reason> (exit code n).", or the code alone.
func lastExit(t *process.Termination) string {
	if t.Reason == "" {
		return fmt.Sprintf(lastExitCode, t.ExitCode)
	}
	return fmt.Sprintf(lastExitReason, t.Reason, t.ExitCode)
}

// restartsCheck: KGWUI-019. A container started again within ProcessWindow:
// its restart count grew while watched, or its last termination is that recent.
type restartsCheck struct{}

// Name implements Check.
func (restartsCheck) Name() string { return restartsCheckName }

// Inputs implements Check: process data, proxy Pods access for the gap note,
// and controller logs for the panic; the window ends through WakeAt.
func (restartsCheck) Inputs() InputSet {
	return InputSet{Process: true, Kinds: []objects.GroupKind{cache.ProxyPodsKind}, Collected: []string{ctrllogs.Key}}
}

// restartState is the restarts check's Store value: the last restart count
// per process ID, and the restarts seen in the window.
type restartState struct {
	counts map[string]int32
	seen   map[string][]restartSeen
}

// restartSeen is a restart count growth of n, dated to the restart.
type restartSeen struct {
	at time.Time
	n  int32
}

// Run implements Check: one finding per subject, on its most recent restart.
// Processes that are gone leave no state behind (Review Focus 2).
func (c restartsCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	res := gapResult(in, c.Name())
	old, _ := Load[*restartState](ctx.State, restartsKey)
	if old == nil {
		old = &restartState{}
	}
	cur := &restartState{counts: make(map[string]int32, len(old.counts))}
	from := in.Now.Add(-ProcessWindow)
	events, logs := installLogs(ctx)
	var fs []Finding
	for _, s := range subjects(in) {
		var best *process.Process
		var bestAt time.Time
		var bestN int32
		for i := range s.set.Processes {
			p := &s.set.Processes[i]
			if last, n := cur.observe(old, p, in.Now, from); !last.IsZero() && (best == nil || last.After(bestAt)) {
				best, bestAt, bestN = p, last, n
			}
		}
		if best == nil {
			continue
		}
		ctx.WakeAt(bestAt.Add(ProcessWindow))
		f := restartFinding(in, s, best, bestN)
		if s.controller {
			addPanic(ctx, &f, events, logs, from)
		}
		fs = append(fs, f)
	}
	ctx.State.Set(restartsKey, cur)
	return fs, res
}

// addPanic adds the controller log state and a panic read in the window to
// 019 f: the newest with a top frame, else the newest. The read time is the
// panic's only time, so it lowers since.
func addPanic(ctx RunContext, f *Finding, events []ctrllogs.Event, logs string, from time.Time) {
	ev := f.Facts.(*ProcessEvidence)
	ev.ControllerLogs = logs
	var p *ctrllogs.Event
	for i := len(events) - 1; i >= 0; i-- {
		if e := &events[i]; e.Class == computed.LogClassPanic && e.Time.After(from) && (p == nil || p.Frame == "" && e.Frame != "") {
			p = e
		}
	}
	if p == nil {
		return
	}
	ev.Panic = &PanicFacts{Class: p.Class, Frame: p.Frame, ReadAt: p.Time}
	ctx.WakeAt(p.Time.Add(ProcessWindow))
	key := sincePrefix + FindingID(ctx.Install.ID(), f.Code, f.Resource, nil)
	t, seen := ctx.State.Get(key)
	if old, _ := t.(time.Time); p.Time.Before(ctx.Now) && (!seen || p.Time.Before(old)) {
		ctx.State.Set(key, p.Time)
	}
}

// observe records p's restart count in s and returns its last restart in the
// window, zero when none, and the restarts observed in it.
func (s *restartState) observe(old *restartState, p *process.Process, now, from time.Time) (time.Time, int32) {
	if p.Restarts == nil {
		return time.Time{}, 0
	}
	var seen []restartSeen
	if p.ID != "" {
		n := *p.Restarts
		prev, known := old.counts[p.ID]
		s.counts[p.ID] = n
		seen = slices.DeleteFunc(slices.Clone(old.seen[p.ID]), func(r restartSeen) bool { return !r.at.After(from) })
		if known && n > prev {
			seen = append(seen, restartSeen{at: restartTime(p, now), n: n - prev})
			seen = seen[max(0, len(seen)-maxRestartsSeen):]
		}
		if len(seen) > 0 {
			if s.seen == nil {
				s.seen = map[string][]restartSeen{}
			}
			s.seen[p.ID] = seen
		}
	}
	var last time.Time
	var n int32
	for _, r := range seen {
		n += r.n
		if r.at.After(last) {
			last = r.at
		}
	}
	// A termination counts only after a restart: a deleted Pod's container also ends (R37).
	if t := p.Termination; t != nil && *p.Restarts > 0 && t.At.After(from) && t.At.After(last) {
		last = t.At
	}
	return last, n
}

// restartTime dates a restart seen at now: the latest of p's start and last
// termination, else now; never after now.
func restartTime(p *process.Process, now time.Time) time.Time {
	var at time.Time
	if p.LastStart != nil {
		at = *p.LastStart
	}
	if t := p.Termination; t != nil && t.At.After(at) {
		at = t.At
	}
	if at.IsZero() || at.After(now) {
		return now
	}
	return at
}

// restartFinding is 019 on s for p, with n restarts observed in the window.
func restartFinding(in Input, s subject, p *process.Process, n int32) Finding {
	ev := &ProcessEvidence{Pod: p.Name, Termination: terminationFacts(p.Termination), Waiting: p.Waiting}
	detail := fmt.Sprintf(restartedOnce, p.Name)
	if n > 0 {
		ev.Restarts = &n
		detail = fmt.Sprintf(restartedTimes, p.Name, times(int(n)))
	}
	if t := p.Termination; t != nil {
		detail += lastExit(t)
		if s.controller && t.ExitCode == 2 {
			detail += panicNote // the command is a next step (diagnose.Steps)
		}
	}
	return processFinding(in, s, CodeProcessRestarting, "process-restarting", restartedMessage, detail, ev, p)
}

// times is "once" or "n times".
func times(n int) string {
	if n == 1 {
		return "once"
	}
	return fmt.Sprintf("%d times", n)
}

// probesCheck: KGWUI-020. At least MinProbeFailures observed probe failures
// on one Pod in the Events window (design §9.2, §9.3).
type probesCheck struct{}

// Name implements Check.
func (probesCheck) Name() string { return probesCheckName }

// Inputs implements Check: process data, Events for their availability, and
// proxy Pods access for the gap note.
func (probesCheck) Inputs() InputSet {
	return InputSet{Process: true, Kinds: []objects.GroupKind{cache.EventsKind, cache.ProxyPodsKind}}
}

// Run implements Check: one finding per subject, on its Pod with the most
// failures. Events off, denied or failing make the check not available (D-034).
func (c probesCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	at := takenAt(in)
	switch a, running := in.Snapshot.Access[cache.EventsGVR]; {
	case !running:
		return nil, NotAvailable(c.Name(), AvailOff, eventsOffDetail, at)
	case !a.OK:
		why, detail := AccessAvailability(a), a.Err
		if why == "" {
			why, detail = AvailFailing, eventsSyncDetail
		}
		return nil, NotAvailable(c.Name(), why, detail, at)
	}
	res := gapResult(in, c.Name())
	if in.Snapshot.EventsCapped {
		res.Detail = strings.TrimPrefix(res.Detail+"; "+eventsCappedDetail, "; ")
	}
	var fs []Finding
	for _, s := range subjects(in) {
		var best *process.Process
		for i := range s.set.Processes {
			p := &s.set.Processes[i]
			if p.Probe != nil && p.Probe.Count >= MinProbeFailures && (best == nil || p.Probe.Count > best.Probe.Count) {
				best = p
			}
		}
		if best == nil {
			continue
		}
		// The oldest observation leaves the window just after First + EventWindow.
		ctx.WakeAt(best.Probe.First.Add(cache.EventWindow + time.Nanosecond))
		fs = append(fs, probeFinding(in, s, best))
	}
	return fs, res
}

// probeFinding is 020 on s for p's probe failures.
func probeFinding(in Input, s subject, p *process.Process) Finding {
	pf := p.Probe
	what := "a probe"
	if pf.Type != "" {
		what = "its " + pf.Type + " probe"
	}
	detail := fmt.Sprintf(probeDetail, p.Name, what, pf.Count)
	if pf.HistoryUnknown {
		detail += probeUncounted
	}
	ev := &ProcessEvidence{Pod: p.Name, Probe: &ProbeFacts{Type: pf.Type, Failures: pf.Count, Last: pf.Last.UTC(), HistoryUnknown: pf.HistoryUnknown}}
	return processFinding(in, s, CodeProbeFailing, "probe-failing", probeMessage, detail, ev, p)
}

// rolloutsCheck: KGWUI-021, a Deployment past its progress deadline. With no
// ready replica 005 or 006 covers it instead (design §9.3).
type rolloutsCheck struct{}

// Name implements Check.
func (rolloutsCheck) Name() string { return rolloutsCheckName }

// Inputs implements Check: process data, Deployments for ready replicas, and
// controller logs for the deployer's errors.
func (rolloutsCheck) Inputs() InputSet {
	return InputSet{Process: true, Kinds: []objects.GroupKind{{Group: "apps", Kind: "Deployment"}}, Collected: []string{ctrllogs.Key}}
}

// Run implements Check. A Pod's waiting reason is evidence; template hashes
// are not compared (review finding 8).
func (c rolloutsCheck) Run(ctx RunContext) ([]Finding, CheckResult) {
	in := ctx.Input
	events, logs := installLogs(ctx)
	var fs []Finding
	for _, s := range subjects(in) {
		r := s.set.Rollout
		if r == nil || r.Progressing || r.Reason != "ProgressDeadlineExceeded" || readyReplicas(in, s) == 0 {
			continue
		}
		ref := s.ref
		f := Finding{Code: CodeRolloutStuck, Slug: "rollout-stuck", Severity: SeverityWarning, Install: in.Install.ID(), Resource: &ref,
			Message: fmt.Sprintf(rolloutMessage, s.what), Detail: rolloutDetail}
		if i := slices.IndexFunc(s.set.Processes, func(p process.Process) bool { return slices.Contains(stuckWaiting, p.Waiting) }); i >= 0 {
			p := &s.set.Processes[i]
			f = processFinding(in, s, CodeRolloutStuck, "rollout-stuck", rolloutMessage, rolloutDetail+fmt.Sprintf(rolloutWaiting, p.Name, p.Waiting),
				&ProcessEvidence{Pod: p.Name, Waiting: p.Waiting}, p)
		}
		if !s.controller {
			ev, _ := f.Facts.(*ProcessEvidence)
			if ev == nil {
				ev = &ProcessEvidence{}
				f.Facts = ev
			}
			ev.ControllerLogs = logs
			ev.Deployer = deployerErrors(ctx, events, s.ref)
		}
		fs = append(fs, f)
	}
	return fs, ok(c.Name(), false, takenAt(in))
}

// deployerClasses are the error classes only deployer lines carry.
var deployerClasses = map[string]bool{computed.LogClassApply: true, computed.LogClassParameters: true, computed.LogClassRender: true,
	computed.LogClassNoValidPorts: true, computed.LogClassStatusUpdate: true, computed.LogClassPrune: true}

// deployerErrors are the deployer errors on Gateway gw in the window, one
// per class and object with its latest time, newest first. A line of class
// other is a deployer's only when it names an object, which rejects never do.
func deployerErrors(ctx RunContext, events []ctrllogs.Event, gw objects.Ref) []DeployerError {
	from := ctx.Now.Add(-ProcessWindow)
	var out []DeployerError
	for _, e := range events {
		deployer := deployerClasses[e.Class] || e.Class == computed.LogClassOther && e.Object != nil
		if e.Gateway != gw || e.TypeURL != "" || !deployer || !e.Time.After(from) {
			continue
		}
		i := slices.IndexFunc(out, func(d DeployerError) bool { return d.Class == e.Class && ptrEqual(d.Object, e.Object) })
		if i < 0 {
			d := DeployerError{Class: e.Class}
			if e.Object != nil {
				o := *e.Object
				d.Object = &o
			}
			out = append(out, d)
			i = len(out) - 1
		}
		if e.Time.After(out[i].Last) {
			out[i].Last = e.Time
		}
	}
	// Stable over the time-ordered events, so equal times keep a fixed order.
	slices.SortStableFunc(out, func(a, b DeployerError) int { return b.Last.Compare(a.Last) })
	out = out[:min(len(out), maxDeployerErrors)]
	for _, d := range out {
		ctx.WakeAt(d.Last.Add(ProcessWindow))
	}
	return out
}

func ptrEqual(a, b *objects.Ref) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// readyReplicas is the ready count of s's Deployment; 0 when it is not found.
func readyReplicas(in Input, s subject) int32 {
	d := in.Install.Deployment
	if !s.controller {
		d = process.ProxyDeployment(s.ref, in.Snapshot.Deployments, in.Install.ProxySelector)
	}
	if d == nil {
		return 0
	}
	return d.Status.ReadyReplicas
}

// addNotReadyFacts sets 005/006's detail from the first not-ready process with
// a waiting reason or exit; that Pod becomes its evidence (§14 rule 16).
func addNotReadyFacts(f *Finding, set *process.Set, podNS string, ready, want int32) {
	if set == nil || want == 0 {
		return
	}
	for i := range set.Processes {
		p := &set.Processes[i]
		if p.Ready != nil && *p.Ready || p.Waiting == "" && p.Termination == nil {
			continue
		}
		var parts []string
		if p.Waiting != "" {
			parts = append(parts, p.Waiting)
		}
		if t := p.Termination; t != nil && t.Reason != "" {
			parts = append(parts, "last exit "+t.Reason)
		} else if t != nil {
			parts = append(parts, fmt.Sprintf("last exit code %d", t.ExitCode))
		}
		f.Detail = fmt.Sprintf(notReadyFacts, ready, want, strings.Join(parts, ", "))
		f.Evidence = append(f.Evidence, process.PodRef(podNS, *p))
		f.Facts = &ProcessEvidence{Pod: p.Name, Termination: terminationFacts(p.Termination), Waiting: p.Waiting}
		return
	}
}
