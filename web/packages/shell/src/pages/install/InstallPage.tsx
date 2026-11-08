import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useContext, useId } from "react";
import { Link } from "react-router-dom";
import { useNow } from "../../api/clock";
import { useDiagnostics } from "../../api/diagHooks";
import { isStale, useCapabilities, useInstallFeature, useInstalls, useMeta, useOverview } from "../../api/hooks";
import { diagnosticsCopy } from "../../copy/explorer";
import { plural } from "../../format";
import { StaleView } from "../../health/staleView";
import { Slot, useInstallPanels } from "../../shell/contributions";
import { CorePagesContext, type DiagnosticsSectionProps, useInstallId, useVocabulary } from "../../shell/extension";
import { layout } from "../../shell/layout";
import { installPath } from "../../shell/paths";
import { PageNotice } from "../../states/PageNotice";
import { PageState } from "../../states/PageState";
import { Shimmer } from "../../states/Shimmer";
import { VisuallyHidden } from "../../states/VisuallyHidden";
import { CheckedSection } from "./CheckedList";
import { CountTiles, TILE_H, TileGrid } from "./CountTiles";
import { FILTER_H, GatewayTable } from "./GatewayTable";
import { HealthStrip, STRIP_H } from "./HealthStrip";
import { NeedsAttention, NeedsAttentionSkeleton } from "./NeedsAttention";
import { PROCESSES_H, ProcessesSection } from "./ProcessesSection";

// region Styles
const Page = styled.div<{ stylesOverride?: SerializedStyles }>`
  max-width: ${layout.maxWidth}px;
  margin: 0 auto;
  padding: ${(p) => p.theme.space(4)};
  h2 {
    font-size: ${(p) => p.theme.font.size.md};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    line-height: 20px;
    margin: ${(p) => p.theme.space(5)} 0 ${(p) => p.theme.space(2)};
  }
  .info {
    display: inline-block;
    margin-top: ${(p) => p.theme.space(2)};
    padding: 2px 4px;
    margin-left: -4px;
    border-radius: ${(p) => p.theme.radius.sm};
    color: ${(p) => p.theme.color.accentText};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    text-decoration: none;
  }
  .info:hover {
    text-decoration: underline;
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 5%, ${(p) => p.theme.color.bg});
  }
  .info:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 12%, ${(p) => p.theme.color.bg});
  }
  ${(p) => p.stylesOverride}
`;
const Split = styled.div`
  display: grid;
  grid-template-columns: minmax(0, 1fr) 280px;
  gap: ${(p) => p.theme.space(4)};
  align-items: start;
  @media (max-width: 900px) {
    grid-template-columns: minmax(0, 1fr);
  }
`;
const FilterShimmer = styled.div`
  width: 300px;
  max-width: 100%;
  margin-bottom: ${(p) => p.theme.space(2)};
`;

// region Helpers
// Table header plus two rows; rows below the header only add height at the end of the page.
const TABLE_SKELETON_H = 112;
const CHECKS_SKELETON_H = 180;

// region Component
/** The upstream diagnostics block: Needs attention (only with groups) and Processes; CorePages can replace it. */
export function DiagnosticsSection({ installId, diagnostics: d, stale }: DiagnosticsSectionProps) {
  // An install without a process view has no Processes block, not one of Unknown lines (design §10).
  const processes = useInstallFeature(installId, "processes") !== false;
  // Stale data shows every group and process state as unknown (D-006).
  return (
    <StaleView.Provider value={stale}>
      <NeedsAttention groups={d.groups} groupTotal={d.groupTotal} installId={installId} />
      {processes ? <ProcessesSection processes={d.processes} installId={installId} /> : null}
    </StaleView.Provider>
  );
}

interface InstallPageProps {
  stylesOverride?: SerializedStyles;
}

/** The install page (layout A, D-011): verdict strip, Needs attention, Processes, what was checked, tiles, Gateways. */
export function InstallPage({ stylesOverride }: InstallPageProps = {}) {
  const id = useInstallId();
  const vocab = useVocabulary();
  const ov = useOverview(id);
  const d = useDiagnostics(id);
  const { data: meta } = useMeta();
  const { data: installs } = useInstalls();
  const caps = useCapabilities();
  const panels = useInstallPanels();
  const Section = useContext(CorePagesContext).DiagnosticsSection ?? DiagnosticsSection;
  const panelId = useId();
  const processes = useInstallFeature(id, "processes") !== false;
  const now = useNow();
  const err = ov.error ?? d.error;
  // Render only data that belongs to this route's install, never the previous install's (design §6.6).
  // Once loaded, a failed poll keeps the page under a notice, with its health shown unknown.
  if (!ov.data || !d.data || ov.data.header.id !== id || d.data.install !== id) {
    if (err?.status === 503) return <PageState kind="connecting" title={vocab.connecting} message={err.message} />;
    if (err)
      return <PageState kind="error" title="Cannot load this install" message={err.message} copyText={err.message} />;
    // One tile per counted kind (Deployments and parameters kinds have none) plus the scope tile where the platform has scope.
    const tiles = meta
      ? meta.kinds.filter((k) => k.category !== "workload" && k.category !== "parameters").length +
        (caps?.has("scope") ? 1 : 0)
      : 8;
    return (
      <Page role="status" aria-busy="true" aria-label="Loading this install…" stylesOverride={stylesOverride}>
        <Shimmer h={STRIP_H} />
        {/* Needs attention reserves the rows the server counted; Processes always shows. */}
        <NeedsAttentionSkeleton rows={installs?.find((s) => s.id === id)?.groupRows ?? 0} />
        {processes ? (
          <>
            <h2>{diagnosticsCopy.processesTitle}</h2>
            <div data-testid="processes-shimmer">
              <Shimmer h={PROCESSES_H} />
            </div>
          </>
        ) : null}
        <Split>
          <div>
            <TileGrid>
              {Array.from({ length: tiles }, (_, i) => `tile-${i}`).map((k) => (
                <div key={k} data-testid="tile-shimmer">
                  <Shimmer h={TILE_H} />
                </div>
              ))}
            </TileGrid>
            <h2>Gateways</h2>
            <FilterShimmer>
              <Shimmer h={FILTER_H} />
            </FilterShimmer>
            <Shimmer h={TABLE_SKELETON_H} />
          </div>
          <div>
            <h2>{diagnosticsCopy.whatWasChecked}</h2>
            <Shimmer h={CHECKS_SKELETON_H} />
          </div>
        </Split>
      </Page>
    );
  }
  const diag = d.data;
  // Stale diagnostics or overview turn every state unknown (D-006).
  // A failing poll leaves the data unconfirmed, which is not evidence either.
  const stale = !!err || isStale(diag.collectedAt, now) || isStale(ov.data.collectedAt, now);
  const signalsMissing = diag.checks.some((c) => c.required && !c.ok && !c.unavailable);
  const checkedAt =
    Date.parse(ov.data.collectedAt) < Date.parse(diag.collectedAt) ? ov.data.collectedAt : diag.collectedAt;
  const infos = diag.verdict.counts.info ?? 0;
  return (
    <Page data-testid="install-page" stylesOverride={stylesOverride}>
      {/* The header shows the install; the h1 gives the page its heading for screen readers. */}
      <VisuallyHidden as="h1">{ov.data.header.displayName || id}</VisuallyHidden>
      {err ? <PageNotice error={err} title="Cannot load this install" /> : null}
      <HealthStrip
        verdict={diag.verdict}
        checks={diag.checks}
        collectedAt={diag.collectedAt}
        stale={stale}
        now={now}
        checkedAt={checkedAt}
      />
      {/* Healthy can still carry info findings; they stay one click away. */}
      {diag.verdict.state === "healthy" && infos > 0 ? (
        <Link className="info" to={installPath(id, "findings")}>
          {plural(infos, "info finding")}
        </Link>
      ) : null}
      <Section installId={id} diagnostics={diag} stale={stale} />
      <Split>
        <div>
          <CountTiles overview={ov.data} />
          <h2>Gateways</h2>
          <GatewayTable rows={ov.data.gateways} installId={id} stale={stale} signalsMissing={signalsMissing} />
          {panels.map((p) => (
            <Slot
              key={p.id}
              id={`install-panel:${p.id}`}
              resetKey={id}
              render={() => (
                <section aria-labelledby={`${panelId}-${p.id}`} data-slot="install-panel">
                  <h2 id={`${panelId}-${p.id}`}>{p.title}</h2>
                  {p.render({ installId: id })}
                </section>
              )}
            />
          ))}
        </div>
        <div>
          {/* Collapsed while Needs attention has groups to look at first (design §6). */}
          <CheckedSection key={id} checks={diag.checks} stale={stale} collapsed={diag.groups.length > 0} />
        </div>
      </Split>
    </Page>
  );
}
