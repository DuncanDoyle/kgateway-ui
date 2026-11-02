package health

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

var sevRank = map[Severity]int{SeverityError: 0, SeverityWarning: 1, SeverityInfo: 2}

// rank orders severities; one outside the known set ranks last.
func rank(s Severity) int {
	if r, ok := sevRank[s]; ok {
		return r
	}
	return len(sevRank)
}

// RollUp computes the state: error > unknown > warning > healthy (design §5.3).
// A not-available check result never makes the state unknown.
func RollUp(fs []Finding, cs []CheckResult) State {
	for _, f := range fs {
		if f.Severity == SeverityError {
			return StateError
		}
	}
	for _, c := range cs {
		if c.Required && !c.OK && c.Unavailable == "" {
			return StateUnknown
		}
	}
	for _, f := range fs {
		if f.Severity == SeverityWarning {
			return StateWarning
		}
	}
	return StateHealthy
}

// Evaluate runs checks and assembles a Report. Findings are in a fixed
// order (severity, code, resource, parentRef, message, detail), so a list
// does not reorder between polls. A finding with a severity outside error,
// warning and info becomes a warning whose detail names the original, so a
// check can neither hide a problem from the roll-up nor invent a severity.
// Checks run in order, each with its Store from rt.States; a grace read
// inside the grace period wakes the report at the grace end (Report.Next).
// A check without Clock reuses its memo while its input print holds and its wake time is not due (§16.2).
// Every finding gets its ID; one without Since gets its first-seen time from its check's Store (design §5.1).
func Evaluate(in Input, checks []Check, rt Runtime) Report {
	r := Report{Install: in.Install.ID(), Counts: map[Severity]int{}, CollectedAt: in.Now}
	memoize := rt.Print != nil && rt.States != nil
	var outputs map[string]CheckOutput
	var outPrints map[string]uint64 // output prints of the checks a later check reads
	inputs := make([]InputSet, len(checks))
	for i, c := range checks {
		if inputs[i] = c.Inputs(); len(inputs[i].Checks) > 0 && outputs == nil {
			outputs = make(map[string]CheckOutput, len(checks))
		}
		if memoize && len(inputs[i].Checks) > 0 {
			if outPrints == nil {
				outPrints = map[string]uint64{}
			}
			for _, dep := range inputs[i].Checks {
				outPrints[dep] = 0
			}
		}
	}
	// Run every check first so the findings are allocated once.
	runs := make([][]Finding, len(checks))
	n := 0
	for i, c := range checks {
		fs, cr, wake, out := evaluateOne(in, c, inputs[i], rt, memoize, outputs, outPrints)
		runs[i] = fs
		n += len(fs)
		r.Checks = append(r.Checks, cr)
		if outputs != nil {
			outputs[c.Name()] = CheckOutput{Findings: fs, Result: cr}
		}
		if _, read := outPrints[c.Name()]; read {
			outPrints[c.Name()] = out
		}
		if !wake.IsZero() && (r.Next.IsZero() || wake.Before(r.Next)) {
			r.Next = wake
		}
	}
	r.Findings = make([]Finding, 0, n)
	for _, fs := range runs {
		for _, f := range fs {
			if _, known := sevRank[f.Severity]; !known {
				note := fmt.Sprintf("unrecognised severity %q", f.Severity)
				f.Detail = strings.TrimSuffix(note+": "+f.Detail, ": ")
				f.Severity = SeverityWarning
			}
			r.Findings = append(r.Findings, f)
		}
	}
	r.Findings = sortFindings(r.Findings)
	for _, f := range r.Findings {
		r.Counts[f.Severity]++
	}
	r.State = RollUp(r.Findings, r.Checks)
	return r
}

// evaluateOne runs c, or reuses its memo, and returns its findings, result,
// wake time and, when a later check reads it (outPrints has its name), its
// output print.
func evaluateOne(in Input, c Check, set InputSet, rt Runtime, memoize bool, outputs map[string]CheckOutput,
	outPrints map[string]uint64,
) (fs []Finding, cr CheckResult, wake time.Time, out uint64) {
	var key uint64
	keyed := false
	if memoize && !set.Clock {
		if key, keyed = rt.Print(set); keyed {
			for _, dep := range set.Checks {
				key = mix(key, outPrints[dep])
			}
		}
	}
	if keyed {
		if m := rt.States.memos[c.Name()]; m != nil && m.print == key && (m.wake.IsZero() || m.wake.After(in.Now)) {
			if at, fresh := m.stamp.restamp(in, set, rt.Collected, m.result.CollectedAt); fresh {
				cr = m.result
				cr.CollectedAt = at
				// Run did not execute, so the memo's kept IDs stand in for its Retain calls.
				pruneSince(rt.States.stores[c.Name()], m.keep)
				if _, read := outPrints[c.Name()]; read {
					out = stampPrint(m.out, at)
				}
				return m.findings, cr, m.wake, out
			}
		}
	}
	ctx := runContext(in, c, set, rt, outputs)
	fs, cr = c.Run(ctx)
	wake = ctx.sink.wake
	sinces := identify(in.Install.ID(), in.Now, fs, ctx.State, rt.States.checkIDs(c.Name()))
	var keep map[string]bool
	// When every since entry was just read and none is retained, none is stale.
	if keyed || len(ctx.sink.retained) > 0 || sinceEntries(ctx.State) != sinces {
		keep = keepOf(fs, ctx.sink.retained)
		pruneSince(ctx.State, keep)
	}
	var base uint64
	if _, read := outPrints[c.Name()]; read {
		base = outputPrint(fs, cr)
		out = stampPrint(base, cr.CollectedAt)
	}
	if keyed {
		rt.States.memos[c.Name()] = &memo{print: key, findings: fs, result: cr, wake: wake, stamp: stampOf(in, set, rt.Collected, cr.CollectedAt), out: base, keep: keep}
	} else if memoize {
		delete(rt.States.memos, c.Name())
	}
	return fs, cr, wake, out
}

// runContext returns c's RunContext: its Store, a fresh sink, and in with
// FirstSeen wrapped so a read inside the grace period wakes at its end.
func runContext(in Input, c Check, inputs InputSet, rt Runtime, outputs map[string]CheckOutput) RunContext {
	var st *Store
	if rt.States != nil {
		st = rt.States.store(c.Name())
	} else {
		st = NewStore()
	}
	ctx := RunContext{Input: in, State: st, sink: &runSink{}, inputs: inputs, outputs: outputs, collected: rt.Collected}
	sink, base := ctx.sink, in
	ctx.FirstSeen = func(u *unstructured.Unstructured) time.Time {
		t := base.firstSeen(u)
		if end := t.Add(Grace); in.Now.Before(end) { // in grace while now − t < Grace
			if sink.wake.IsZero() || end.Before(sink.wake) {
				sink.wake = end
			}
		}
		return t
	}
	return ctx
}

// sortFindings returns fs sorted stably by compareFindings. It sorts
// pointers, as moving a whole Finding per swap dominated a large report's sort.
func sortFindings(fs []Finding) []Finding {
	ps := make([]*Finding, len(fs))
	for i := range fs {
		ps[i] = &fs[i]
	}
	slices.SortStableFunc(ps, func(a, b *Finding) int { return compareFindings(*a, *b) })
	sorted := make([]Finding, len(fs))
	for i, p := range ps {
		sorted[i] = *p
	}
	return sorted
}

// compareFindings orders by severity, code, resource, parentRef, message and
// detail. Each key is compared only when the earlier ones tie.
func compareFindings(a, b Finding) int {
	if a.Severity != b.Severity {
		if c := cmp.Compare(rank(a.Severity), rank(b.Severity)); c != 0 {
			return c
		}
	}
	if c := cmp.Compare(a.Code, b.Code); c != 0 {
		return c
	}
	if c := compareOptRefs(a.Resource, b.Resource); c != 0 {
		return c
	}
	if a.parent != nil || b.parent != nil {
		if c := compareParents(a.parent, b.parent); c != 0 {
			return c
		}
	}
	if c := cmp.Compare(a.Message, b.Message); c != 0 {
		return c
	}
	return cmp.Compare(a.Detail, b.Detail)
}

// compareOptRefs orders findings without a resource first.
func compareOptRefs(a, b *objects.Ref) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	return compareRefs(*a, *b)
}

// compareRefs orders by group, kind, namespace and name.
func compareRefs(a, b objects.Ref) int { return a.Compare(b) }

func compareParents(pa, pb *objects.ParentRef) int {
	var a, b objects.ParentRef
	if pa != nil {
		a = *pa
	}
	if pb != nil {
		b = *pb
	}
	return cmp.Or(compareRefs(a.Ref(), b.Ref()), cmp.Compare(a.SectionName, b.SectionName), cmp.Compare(a.Port, b.Port))
}

// mix folds v into h: an FNV-1a step, then the splitmix64 finaliser.
func mix(h, v uint64) uint64 {
	x := (h^v)*1099511628211 + 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// stampPrint folds CollectedAt into an output print, so a restamp reruns the checks that read it.
func stampPrint(base uint64, at time.Time) uint64 {
	return mix(mix(base, uint64(at.UnixNano())), fnvString(at.Location().String())) //nolint:gosec // hashed bits
}

// fnvString is FNV-64a of s.
func fnvString(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// outputPrint is FNV-64a over every field of fs and of cr but CollectedAt,
// which stampPrint adds so a memo can restamp it.
func outputPrint(fs []Finding, cr CheckResult) uint64 {
	h := fnv.New64a()
	var b [8]byte
	u64 := func(v uint64) {
		binary.LittleEndian.PutUint64(b[:], v)
		_, _ = h.Write(b[:])
	}
	str := func(s string) {
		u64(uint64(len(s))) // length prefix keeps adjacent strings apart
		_, _ = h.Write([]byte(s))
	}
	flag := func(v bool) {
		if v {
			u64(1)
		} else {
			u64(0)
		}
	}
	ref := func(r objects.Ref) { str(r.Group); str(r.Kind); str(r.Namespace); str(r.Name) }
	at := func(t *time.Time) {
		if flag(t != nil); t != nil {
			u64(uint64(t.UnixNano()))
			str(t.Location().String())
		}
	}
	u64(uint64(len(fs)))
	for _, f := range fs {
		str(f.ID)
		str(f.Group)
		str(f.Code)
		str(f.Slug)
		str(string(f.Severity))
		str(f.Install)
		if flag(f.Resource != nil); f.Resource != nil {
			ref(*f.Resource)
		}
		str(f.Message)
		str(f.Detail)
		at(f.Since)
		flag(f.Restricted)
		u64(uint64(len(f.Evidence)))
		for _, e := range f.Evidence {
			ref(e)
		}
		at(f.ResourceSince)
		if flag(f.parent != nil); f.parent != nil {
			ref(f.parent.Ref())
			str(f.parent.SectionName)
			u64(uint64(f.parent.Port)) //nolint:gosec // hashed bits; the sign does not matter
		}
	}
	str(cr.Name)
	flag(cr.OK)
	flag(cr.Required)
	str(cr.Error)
	str(cr.Detail)
	flag(cr.Pending)
	str(string(cr.Unavailable))
	return h.Sum64()
}
