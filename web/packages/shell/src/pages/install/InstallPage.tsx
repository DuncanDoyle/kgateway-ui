import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Link } from "react-router-dom";
import { useNow } from "../../api/clock";
import { isStale, useCapabilities, useHealth, useMeta, useOverview } from "../../api/hooks";
import { plural } from "../../format";
import { useInstallId, useVocabulary } from "../../shell/extension";
import { installPath } from "../../shell/paths";
import { PageNotice } from "../../states/PageNotice";
import { PageState } from "../../states/PageState";
import { Shimmer } from "../../states/Shimmer";
import { VisuallyHidden } from "../../states/VisuallyHidden";
import { CheckedList } from "./CheckedList";
import { CountTiles, TILE_H, TileGrid } from "./CountTiles";
import { FILTER_H, GatewayTable } from "./GatewayTable";
import { HealthStrip, STRIP_H } from "./HealthStrip";
import { TopFindings } from "./TopFindings";

// region Styles
const Page = styled.div<{ stylesOverride?: SerializedStyles }>`
  max-width: 1400px;
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
interface InstallPageProps {
  stylesOverride?: SerializedStyles;
}

/** The install page (layout A, D-011): health strip, top findings, tiles, gateways. */
export function InstallPage({ stylesOverride }: InstallPageProps = {}) {
  const id = useInstallId();
  const vocab = useVocabulary();
  const ov = useOverview(id);
  const h = useHealth(id);
  const { data: meta } = useMeta();
  const caps = useCapabilities();
  const now = useNow();
  const err = ov.error ?? h.error;
  // Render only data that belongs to this route's install, never the previous install's (design §6.6).
  // Once loaded, a failed poll keeps the page under a notice, with its health shown unknown.
  if (!ov.data || !h.data || ov.data.header.id !== id || h.data.install !== id) {
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
            <h2>Checks</h2>
            <Shimmer h={CHECKS_SKELETON_H} />
          </div>
        </Split>
      </Page>
    );
  }
  // Stale health or overview turns the strip and the gateway rows unknown (D-006).
  // A failing poll leaves the data unconfirmed, which is not evidence either.
  const stale = !!err || isStale(h.data.collectedAt, now) || isStale(ov.data.collectedAt, now);
  const signalsMissing = h.data.checks.some((c) => c.required && !c.ok);
  const checkedAt =
    Date.parse(ov.data.collectedAt) < Date.parse(h.data.collectedAt) ? ov.data.collectedAt : h.data.collectedAt;
  const gateways = (
    <>
      <CountTiles overview={ov.data} />
      <h2>Gateways</h2>
      <GatewayTable
        rows={ov.data.gateways}
        findings={h.data.findings}
        installId={id}
        stale={stale}
        signalsMissing={signalsMissing}
      />
    </>
  );
  return (
    <Page data-testid="install-page" stylesOverride={stylesOverride}>
      {/* The header shows the install; the h1 gives the page its heading for screen readers. */}
      <VisuallyHidden as="h1">{ov.data.header.displayName || id}</VisuallyHidden>
      {err ? <PageNotice error={err} title="Cannot load this install" /> : null}
      <HealthStrip health={h.data} stale={stale} now={now} checkedAt={checkedAt} />
      {h.data.state === "healthy" ? (
        <Split>
          <div>
            {/* Healthy can still carry info findings; they stay one click away. */}
            {h.data.findings.length > 0 ? (
              <Link className="info" to={installPath(id, "findings")}>
                {plural(h.data.findings.length, "info finding")}
              </Link>
            ) : null}
            {gateways}
          </div>
          <div>
            <h2>Checks</h2>
            <CheckedList checks={h.data.checks} stale={stale} />
          </div>
        </Split>
      ) : (
        <>
          <TopFindings findings={h.data.findings} installId={id} />
          {gateways}
        </>
      )}
    </Page>
  );
}
