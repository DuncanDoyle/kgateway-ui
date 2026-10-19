import { css, type SerializedStyles, useTheme } from "@emotion/react";
import styled from "@emotion/styled";
import { Link } from "react-router-dom";
import { useNow } from "../api/clock";
import { isStale, useInstalls } from "../api/hooks";
import type { InstallSummary } from "../api/types";
import { countsText, versionText } from "../format";
import { HealthBadge } from "../health/HealthBadge";
import { stateLabel } from "../health/state";
import { ResourceName } from "../resource/ResourceName";
import { PageState } from "../states/PageState";
import type { HealthState } from "../theme/tokens";

// region Styles
const Grid = styled.div<{ stylesOverride?: SerializedStyles }>`
  max-width: 1400px;
  margin: 0 auto;
  padding: ${(p) => p.theme.space(4)};
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(320px, 100%), 1fr));
  gap: ${(p) => p.theme.space(3)};
  ${(p) => p.stylesOverride}
`;
// Status is the badge (icon + text) plus a full status-tinted border; never a left stripe.
// Link forwards unknown props to the DOM, so the transient $state is filtered out.
const Card = styled(Link, { shouldForwardProp: (p) => p !== "$state" })<{ $state: HealthState }>`
  display: flex;
  flex-direction: column;
  gap: ${(p) => p.theme.space(2)};
  height: 100%;
  box-sizing: border-box;
  min-width: 0;
  padding: ${(p) => p.theme.space(4)};
  color: ${(p) => p.theme.color.text};
  text-decoration: none;
  background: ${(p) => p.theme.color.surface};
  border: 1px solid
    ${(p) => (p.$state === "healthy" ? p.theme.color.border : p.theme.color.status[p.$state].stripBorder)};
  border-radius: ${(p) => p.theme.radius.md};
  &:hover {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 4%, ${(p) => p.theme.color.surface});
    border-color: ${(p) => p.theme.color.borderStrong};
  }
  &:hover [data-part="name"] {
    text-decoration: underline;
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 8%, ${(p) => p.theme.color.surface});
    border-color: ${(p) => p.theme.color.text};
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: ${(p) => p.theme.space(2)};
  }
  .version {
    padding: 1px 7px;
    border-radius: ${(p) => p.theme.radius.sm};
    border: 1px solid ${(p) => p.theme.color.borderStrong};
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  .summary {
    font-size: ${(p) => p.theme.font.size.base};
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
  }
`;
const nameStyle = (size: string) => css`
  font-size: ${size};
  overflow: hidden;
  text-overflow: ellipsis;
`;

// region Helpers
function summary(i: InstallSummary, state: HealthState, stale: boolean): string {
  if (stale) return "Data is stale";
  const counts = countsText(i.health.counts);
  if (counts) return counts;
  if (state === "healthy") return "No problems";
  return state === "unknown" ? "Health unknown" : stateLabel(state);
}

function scopeText(s: InstallSummary["scope"]): string {
  if (s.mode === "all") return "All namespaces";
  if (s.mode === "unknown") return "Scope unknown";
  return `${s.watched} / ${s.total} namespaces`;
}

// region Component
interface ClusterLandingProps {
  stylesOverride?: SerializedStyles;
}

/** One card per controller install (layout C); shown only with more than one install that is not pinned (D-011). */
export function ClusterLanding({ stylesOverride }: ClusterLandingProps = {}) {
  const { data, error } = useInstalls();
  const now = useNow();
  const theme = useTheme();
  if (error?.status === 503)
    return <PageState kind="connecting" title="Connecting to cluster…" message={error.message} />;
  if (error)
    return <PageState kind="error" title="Cannot load installs" message={error.message} copyText={error.message} />;
  if (!data) return <PageState kind="loading" title="Loading…" />;
  return (
    <Grid stylesOverride={stylesOverride}>
      {data.map((i) => {
        // An old summary is not evidence: unknown, never its last state.
        const stale = isStale(i.computedAt, now);
        const state: HealthState = stale ? "unknown" : i.health.state;
        return (
          <Card key={i.id} to={`/i/${i.id}`} $state={state} data-testid="install-card">
            <div className="top">
              <HealthBadge state={state} />
              {i.version ? <span className="version">{versionText(i.version)}</span> : null}
            </div>
            <ResourceName namespace={i.namespace} name={i.name} stylesOverride={nameStyle(theme.font.size.base)} />
            <span className="summary">{summary(i, state, stale)}</span>
            <span className="muted">{scopeText(i.scope)}</span>
          </Card>
        );
      })}
    </Grid>
  );
}
