import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { ChevronDown, ChevronRight } from "lucide-react";
import { useId, useState } from "react";
import { useNow } from "../api/clock";
import type { GroupRow as GroupRowData } from "../api/types";
import { diagnosticsCopy, findingsCopy } from "../copy/explorer";
import { LinkedName } from "../explore/LinkedName";
import { HealthBadge } from "../health/HealthBadge";
import { severityLabel, severityToState } from "../health/state";
import { ago } from "../pages/install/HealthStrip";
import { NextSteps } from "./NextSteps";

// region Styles
/** Height of a collapsed row, fixed from the first render so polling and late data shift nothing (design §17.4). */
export const GROUP_ROW_H = 64;
/** Below this viewport width the state takes its own line, and a collapsed row is GROUP_ROW_H_NARROW. */
export const GROUP_ROW_NARROW = 720;
export const GROUP_ROW_H_NARROW = 88;

const Row = styled.div<{ stylesOverride?: SerializedStyles }>`
  border-bottom: 1px solid ${(p) => p.theme.color.border};
  &:last-of-type {
    border-bottom: 0;
  }
  .line {
    display: grid;
    grid-template-columns: 112px minmax(0, 1fr) auto;
    align-items: center;
    gap: ${(p) => p.theme.space(3)};
    box-sizing: border-box;
    min-height: ${GROUP_ROW_H}px;
    padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
  }
  .main {
    min-width: 0;
  }
  .title {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    color: ${(p) => p.theme.color.text};
  }
  .meta {
    display: flex;
    align-items: center;
    gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(3)};
    min-width: 0;
    margin-top: 2px;
    overflow: hidden;
    white-space: nowrap;
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
  }
  .cause {
    display: inline-flex;
    gap: ${(p) => p.theme.space(1)};
    min-width: 0;
  }
  .code {
    padding: 0 6px;
    font-family: ${(p) => p.theme.font.mono};
    color: ${(p) => p.theme.color.textMuted};
    border: 1px solid ${(p) => p.theme.color.borderStrong};
    border-radius: ${(p) => p.theme.radius.sm};
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
  }
  .panel {
    padding: 0 ${(p) => p.theme.space(3)} ${(p) => p.theme.space(3)} calc(112px + ${(p) => p.theme.space(6)});
  }
  @media (max-width: ${GROUP_ROW_NARROW}px) {
    .line {
      grid-template-columns: minmax(0, 1fr) auto;
      min-height: ${GROUP_ROW_H_NARROW}px;
    }
    .sev {
      grid-column: 1 / -1;
    }
    .panel {
      padding-left: ${(p) => p.theme.space(3)};
    }
  }
  ${(p) => p.stylesOverride}
`;
const Expander = styled.button`
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 28px;
  padding: 0 10px 0 6px;
  cursor: pointer;
  font: inherit;
  font-size: ${(p) => p.theme.font.size.sm};
  white-space: nowrap;
  color: ${(p) => p.theme.color.text};
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    border-color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.surface});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
`;

// region Component
interface GroupRowProps {
  group: GroupRowData;
  installId: string;
  stylesOverride?: SerializedStyles;
}

/**
 * One "Needs attention" group: state, title, cause, blast radius, age, code and its next steps.
 * A restricted group shows only its severity, code and the restricted label (design §14 rule 13).
 */
export function DiagnosticsGroupRow({ group: g, installId, stylesOverride }: GroupRowProps) {
  const now = useNow();
  const [open, setOpen] = useState(false);
  const panelId = useId();
  const steps = g.restricted ? undefined : g.nextSteps;
  return (
    <Row data-testid="group-row" stylesOverride={stylesOverride}>
      <div className="line">
        <div className="sev">
          <HealthBadge state={severityToState(g.severity)} label={severityLabel[g.severity]} />
        </div>
        <div className="main">
          {g.restricted ? (
            <div className="title muted">{findingsCopy.restricted}</div>
          ) : (
            <div className="title" title={g.title}>
              {g.title}
            </div>
          )}
          <div className="meta">
            {!g.restricted && g.cause ? (
              <span className="cause">
                {g.cause.kind} <LinkedName refValue={g.cause} installId={installId} truncate />
              </span>
            ) : null}
            {!g.restricted && g.affectedTotal > 0 ? <span>{diagnosticsCopy.affected(g.affectedTotal)}</span> : null}
            {!g.restricted && g.members > 1 ? <span>{diagnosticsCopy.findings(g.members)}</span> : null}
            {!g.restricted && g.since ? <span>{ago(g.since, now)}</span> : null}
            <span className="code">{g.code}</span>
          </div>
        </div>
        {steps?.length ? (
          <Expander type="button" aria-expanded={open} aria-controls={panelId} onClick={() => setOpen((o) => !o)}>
            {open ? <ChevronDown size={14} aria-hidden="true" /> : <ChevronRight size={14} aria-hidden="true" />}
            {diagnosticsCopy.nextStep}
          </Expander>
        ) : null}
      </div>
      {steps?.length ? (
        <div className="panel" id={panelId} hidden={!open}>
          {open ? <NextSteps steps={steps} /> : null}
        </div>
      ) : null}
    </Row>
  );
}
