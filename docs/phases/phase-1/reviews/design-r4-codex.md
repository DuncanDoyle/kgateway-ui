# Review: Phase 1 design + plan, fourth pass
Cross-model: YES (author: Claude Opus 5.5, reviewer: Codex)
Scope: design + plan revision round 3
Diff: 01112cf..ab2b5d1
Timing: on time

**The plan is not ready to implement.** Three issues remain.

1. **P1 — Watch success clears unavailability before the cache catches up.** [implementation-plan.md:1723](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/implementation-plan.md:1723). In client-go v0.36.2, `reflector.go:863–903` opens the watch-list stream, consumes initial events into a temporary store, waits for the initial-events-end bookmark, then replaces the informer’s data. The recorder clears the error at stream opening. Since `HasSynced` remains true, recovery can expose the old healthy cache throughout this process—even if synchronization subsequently fails. The fake-client tests disable watch-list semantics and miss this. Row **1 remains incomplete**: recovery needs evidence that the cache has caught up.

2. **P1 — Errors inside an established watch stream never mark access unavailable.** [implementation-plan.md:1721](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/implementation-plan.md:1721). The recorder observes only the Watch method’s return. Client-go processes `watch.Error` events later (`reflector.go:1010`), including 429 errors that trigger backoff (`reflector.go:652`). Repeated HTTP-successful watches returning error events therefore leave `Access.OK` true indefinitely. Additionally, `rest/request.go:818–819` can return an empty watcher with **nil error** after transport EOF/timeout retries. These failures need coverage beyond fake reactors returning method errors.

3. **P2 — The documented timeout does not bound a silently stalled TCP connection.** [implementation-plan.md:1845](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/implementation-plan.md:1845), [design.md:229](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/design.md:229). The reflector supplies a server-side timeout; the wrapper uses the application-lifetime context. A blackholed connection cannot deliver the server’s timeout closure, and client-go’s `Request.Watch` does not install a local context deadline. Access can remain healthy beyond 5–10 minutes. Row **2** needs either a client-enforced bound or an explicit statement that this limitation is unbounded.

Rows **3–5** are resolved consistently. The grace map’s pruning and locking, `useNow` wiring, and parent dedupe look correct. No removed-API leftovers or new identifier mismatches found; remaining `Probe` names are unrelated test components. Changed backend waits poll observable conditions, with sleeps only between polls.

Source/plan review only; no files edited or tests executed.
## Triage
All three point at the same gap: "the Watch call returned" is not evidence that the cache is current. Replace the call-outcome rule with a heartbeat rule built on the API server's watch bookmarks, which the watch cache sends about every minute (`defaultBookmarkFrequency = time.Minute`, k8s.io/apiserver `pkg/storage/cacher/cacher.go`; client-go reflectors request bookmarks).

| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | Watch success clears unavailability before the cache catches up | reviewer | accept | After a failure, a kind is current again only once the informer has caught up: a successful List call (classic mode, the reflector replaces the store from it), or the `k8s.io/initial-events-end` bookmark (watch-list mode). |
| 2 | Error events inside a stream and empty watchers go unseen | reviewer | accept | The wrapper also wraps the returned `watch.Interface` and timestamps every event. `watch.Error` events record a failure. A kind is current only if a non-error event or bookmark arrived within the heartbeat window (150 s, 2.5 × the bookmark interval); an empty watcher or an erroring stream produces none. |
| 3 | A blackholed connection is not bounded | reviewer | accept | Bounded by the same heartbeat window: no bookmark for 150 s → unavailable. The design states the bound, and that it depends on the API server's bookmark interval. |
