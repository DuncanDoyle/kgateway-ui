import styled from "@emotion/styled";
import { useNow } from "../../api/clock";
import { isStale, useHealth, useMeta, useOverview } from "../../api/hooks";
import { useInstallId } from "../../shell/extension";
import { PageState } from "../../states/PageState";
import { Shimmer } from "../../states/Shimmer";
import { CheckedList } from "./CheckedList";
import { CountTiles, TILE_H, TileGrid } from "./CountTiles";
import { FILTER_H, GatewayTable } from "./GatewayTable";
import { HealthStrip, STRIP_H } from "./HealthStrip";
import { TopFindings } from "./TopFindings";

// region Styles
const Page = styled.div`
  max-width: 1400px;
  margin: 0 auto;
  padding: ${(p) => p.theme.space(4)};
  h2 {
    font-size: ${(p) => p.theme.font.size.md};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    line-height: 20px;
    margin: ${(p) => p.theme.space(5)} 0 ${(p) => p.theme.space(2)};
  }
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
/** The install page (layout A, D-011): health strip, top findings, tiles, gateways. */
export function InstallPage() {
  const id = useInstallId();
  const ov = useOverview(id);
  const h = useHealth(id);
  const { data: meta } = useMeta();
  const now = useNow();
  const err = ov.error ?? h.error;
  if (err && err.status === 503)
    return <PageState kind="connecting" title="Connecting to cluster…" message={err.message} />;
  if (err && !(ov.data && h.data)) {
    return <PageState kind="error" title="Cannot load this install" message={err.message} copyText={err.message} />;
  }
  // Render only data that belongs to this route's install, never the previous install's (design §6.6).
  if (!ov.data || !h.data || ov.data.header.id !== id || h.data.install !== id) {
    // One tile per counted kind (meta lists Deployments, which have no tile) plus the scope tile.
    const tiles = meta ? meta.kinds.filter((k) => k.category !== "workload").length + 1 : 8;
    return (
      <Page role="status" aria-busy="true" aria-label="Loading install">
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
            <h2>What was checked</h2>
            <Shimmer h={CHECKS_SKELETON_H} />
          </div>
        </Split>
      </Page>
    );
  }
  // Stale health or overview turns the strip and the gateway rows unknown (D-006).
  const stale = isStale(h.data.collectedAt, now) || isStale(ov.data.collectedAt, now);
  const checkedAt =
    Date.parse(ov.data.collectedAt) < Date.parse(h.data.collectedAt) ? ov.data.collectedAt : h.data.collectedAt;
  const gateways = (
    <>
      <CountTiles overview={ov.data} />
      <h2>Gateways</h2>
      <GatewayTable rows={ov.data.gateways} installId={id} stale={stale} />
    </>
  );
  return (
    <Page>
      <HealthStrip health={h.data} stale={stale} now={now} checkedAt={checkedAt} />
      {h.data.state === "healthy" ? (
        <Split>
          <div>{gateways}</div>
          <div>
            <h2>What was checked</h2>
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
