# Sub-phase 3.2 — Evidence (Task 53)

The kind e2e and the ui-loop run for data-plane and process health (design §21). Fixtures: `test/e2e/fixtures/nack.yaml` (`NACK=1`) and `test/e2e/fixtures/diagnostics.yaml`, which the test applies itself. API checks: `TestDataPlaneDiagnostics` in `test/e2e/diagnostics_test.go`; browser checks: `web/e2e/diagnostics.spec.ts`.

## e2e runs

| kgateway / Gateway API | `NACK=1 make e2e` |
|---|---|
| v2.4.7 / v1.6.1 | pass: Go 6 tests (1 skipped: needs `--poll-interval=10m`), Playwright 50 tests (local, 2026-11-04) |
| v2.3.11 / v1.5.1 | pass: Go 6 tests (same skip), Playwright 50 tests (local, 2026-11-04) |

`TestDataPlaneDiagnostics` runs five subtests in order, each restoring the cluster:
1. **rejects**: 001 on `infra/nack` with 2 streams per type (2 proxies, 1 replica), reported by the controller Pod, the status-says-healthy message, the replica in the log command, and Envoy route config `listener~8080` from proxy stats. `infra/edge` may carry 001 for the Cluster only.
2. **stuck rollout**: a new Gateway with an unpullable image is 006 with `ErrImagePull`/`ImagePullBackOff` and no 021, even past its deadline (R38). After a good image, a rollout to the bad tag beside the ready Pod is 021 with the waiting reason. `deploymentOverlay.spec.progressDeadlineSeconds: 15` exists in the GatewayParameters CRD on both tags.
3. **restart hold** (R37): `kubectl delete pod` of the controller, then the rejecting objects are removed once the replacement is ready. The check reads pending with the hold message, `infra/nack` reads unknown (overview) and unavailable (Gateway detail), never clear. Re-applied during the hold, the reject keeps its `since`.
4. **container restart** (R37): `kill 1` from an ephemeral debug container in the controller's PID namespace. 019 on `kgw-a/kgateway` names that Pod and its termination: `Completed (exit code 0)` on 2.4.7, `Error (exit code 2)` with the Go panic note on 2.3.11.
5. **controller scaled to zero**: 026 on `infra/nack` at least 60 s after the scale-down, both Pods disconnected; the xDS rejects check is not OK meanwhile.

The install itself reads error throughout, because the explorer fixture keeps error findings; the hold is asserted on the check and on `infra/nack`'s data plane instead.

## Envoy names seen (for the namer, Task 41)

| | v2.3.11 | v2.4.7 |
|---|---|---|
| Route configs (`http.<prefix>.rds.<name>`) | prefix `http`; `listener~8080` (rejected on nack), `https`, `listener~8090` | same |
| Clusters (`cluster.<name>.membership_total`, usedonly) | `kube_<ns>_<svc>_<port>` (e.g. `kube_shop_httpbin-nack_8000`, `kube_infra_edge_8080`), `backend_shop_static-be_0`, `xds_cluster`, `admin_port_cluster` | only `backend_shop_static-be_0`, `xds_cluster`, `admin_port_cluster`: no `kube_*` cluster was ever accepted |

CDS rejects never name a cluster in the stats on either tag (the update is rejected whole), so 001's per-Pod evidence lists Envoy names for RouteConfiguration only.

## ui-loop

Run against v2.4.7 on 2026-11-04. Local, not committed.
- Run dir with `goal.md` and `journal.md`: `/var/folders/l9/kvw9vb8121720jclk572xwpw0000gp/T/ui-loop/kgateway-ui-claude/task-53/`
- Probe: `artifacts/probe-3-2-health-pages-themes-widths-loading-stale-states/`: `video.webm`, `{dark,light}-{720,1280,1600,3008}-{install,processes,gateway}.png`, `loading.png`, `loaded.png`, `stale.png`, `*-expander-active.png`, `*-showall-focus.png`, `report.json` (scroll, contrast, borders, CLS).
- Before-shot: `baseline/peek-processes.png`.

Verdict: met. No horizontal scroll, no text under 4.5:1, no left-border cards, CLS 0 while `/diagnostics` loads, every group unknown when stale, hover, active and focus-visible on the new controls.

## Findings

Fixed:
- A settled rollout read "Rollout stuck: NewReplicaSetAvailable" on the Processes page: the server sent the reason of every rollout. `/processes` now sends a reason only for `ProgressDeadlineExceeded`, as `RolloutDTO` documents.
- At 720 px the Gateway page scrolled sideways by 15 px: the Data plane body and the next-steps list were grids whose auto column grew to the longest command. Both use `minmax(0, 1fr)`; the command scrolls in its block.

Fixed in review round 1:
- Every process row read "A probe failed 0 times": the model set probe facts on every Pod while Events were readable. Probe facts now come only with a counted failure, so the column is hidden and `probe.last` is never the zero time.
- 026's detail for two or more Pods is plural: "… are disconnected from the controller. They still serve their last config." (S471).
