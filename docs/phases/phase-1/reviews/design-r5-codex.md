# Review: Phase 1 design + plan, fifth pass
Cross-model: YES (author: Claude Opus 5.5, reviewer: Codex)
Scope: design + plan revision round 4 (watch health)
Diff: ab2b5d1..19e056b
Timing: on time

**The plan is not ready to implement.** Four issues remain.

1. **P1 — The first event on a resumed classic watch does not prove catch-up.** [implementation-plan.md:1958](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/implementation-plan.md:1958), also specified at [design.md:229](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/design.md:229). After an outage, the server replays retained events in order; its first event can precede a substantial backlog. The recorder nevertheless clears the failure immediately. For example, an early unrelated update restores “healthy” before a later queued Gateway failure arrives. Apiserver’s `cache_watcher.go:480–516` sends the historical interval sequentially; client-go’s `reflector.go:620–638` processes events individually. Ordering guarantees eventual replay, not completion at the first event. Recovery needs a synchronization checkpoint, with a test that withholds the remaining backlog.

2. **P1 — Watch-list recovery still precedes updating the informer cache.** [implementation-plan.md:1945](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/implementation-plan.md:1945). `observe.run` calls `p.event` **before forwarding** the bookmark (`2054–2059`), making access healthy while the informer still holds old data. Client-go consumes that bookmark, exits initial streaming, and only then calls `r.store.Replace` (`reflector.go:891–905`); the informer subsequently processes the queued replacement. `HasSynced` remains true throughout recovery. This leaves fourth-pass finding 1 unresolved. The wrapper-level test drains events without applying them and therefore explicitly permits this premature recovery. Health must follow application of the synchronization checkpoint to the store used by `Snapshot`.

3. **P2 — Periodic bookmark scheduling does not guarantee periodic delivery.** [implementation-plan.md:259](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/implementation-plan.md:259). Apiserver schedules bookmarks roughly every minute, but `cache_watcher.go:535–539` suppresses bookmarks at the watch’s starting resource version once the initial bookmark requirement is satisfied. With no subsequent storage revision advance, a healthy idle watch can receive no periodic bookmarks—even with its watch cache enabled. The heartbeat then reports unknown and repeatedly restarts it. The API contract explicitly warns against assuming any bookmark interval (`apimachinery/pkg/apis/meta/v1/types.go:365–368`). Task 1’s busy kind cluster can satisfy the three-bookmark expectation without establishing the required guarantee. The default needs a freshness mechanism that also works when revisions remain unchanged.

4. **P3 — The injected clock does not test idle-watch termination or its recovery path.** [implementation-plan.md:1754](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/implementation-plan.md:1754). Advancing `Now` changes access calculations, but `observe.run` uses a real `time.NewTimer` (`2044`). The test then sends a bookmark through the original watcher; production is supposed to stop that watcher after the window. Thus these tests do not deterministically exercise the timer-driven behavior, cancellation, or replacement watch. Inject timer creation as well, assert that the old watcher stops, and recover through its replacement. Bound the raw `<-watchers` waits so regressions fail instead of hanging.

A materially simpler correct design is bounded periodic Lists with atomic snapshot replacement and freshness expiry, optionally retaining watches for prompt updates. That adds List traffic, but removes bookmark timing and replay-completion inference. Preserving the current traffic constraint requires explicit tracking of synchronization checkpoints through informer-store application; the proposed wrapper alone is insufficient.

Reviewed against the supplied v0.36.2 sources. No files edited or tests executed.
## Triage
The heartbeat/catch-up approach cannot be made exact without tracking client-go internals. Option A was chosen with the user (2026-10-19). client-go's HTTP/2 connection health check (`ReadIdleTimeout` 30 s + `PingTimeout` 15 s, apimachinery v0.36.2 `pkg/util/net/http.go`) already bounds a dead connection to about 45 s.

| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | First event on a resumed watch does not prove catch-up | reviewer | accept as documented limit (user, option A) | After an outage (shown as unknown), the UI may show the old state for the few seconds client-go needs to replay missed events. Same trust every controller places in informers. Recorded in design §5.4. |
| 2 | Watch-list recovery precedes store replacement | reviewer | accept as documented limit (option A) | Same window as #1. |
| 3 | Bookmarks are not guaranteed on idle watches | reviewer | accept | Heartbeat rule, `HeartbeatWindow`, `--watch-heartbeat` and the Task 1 bookmark check are removed. Dead connections are covered by client-go's HTTP/2 health check. |
| 4 | Idle-watch timer not tested | reviewer | n/a | The idle-watch stop is removed with the heartbeat. |
| — | Strict alternative: periodic relists with list-age freshness | reviewer | reject (user, option A) | A permanent API load of one full list per kind per minute, for a few-second post-outage window. |
| A4 | Idle kinds would stay unknown after a blip if recovery needed an event | author (plan agent) | accept | Recovery = a successful Watch call opened after the failure (or the initial-events-end bookmark in watch-list mode); no event needed. D-014 updated. |
