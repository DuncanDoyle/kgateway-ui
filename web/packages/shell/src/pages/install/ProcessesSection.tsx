import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useId } from "react";
import { Link } from "react-router-dom";
import type { Diagnostics } from "../../api/types";
import { diagnosticsCopy } from "../../copy/explorer";
import { PROCESS_LINE_H, ProcessLine } from "../../diagnostics/ProcessLine";
import { installPath } from "../../shell/paths";

// region Styles
const PAD = 8;
const LINK_H = 32;
/** The box's height with both lines, shared with the loading skeleton so nothing shifts. */
export const PROCESSES_H = 2 * PAD + 2 * PROCESS_LINE_H + LINK_H + 2;

const Box = styled.div<{ stylesOverride?: SerializedStyles }>`
  box-sizing: border-box;
  min-height: ${PROCESSES_H}px;
  padding: ${PAD}px ${(p) => p.theme.space(3)};
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  .view {
    display: inline-flex;
    align-items: center;
    height: ${LINK_H}px;
    padding: 0 4px;
    margin-left: -4px;
    border-radius: ${(p) => p.theme.radius.sm};
    color: ${(p) => p.theme.color.accentText};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    text-decoration: none;
  }
  .view:hover {
    text-decoration: underline;
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 5%, ${(p) => p.theme.color.surface});
  }
  .view:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 12%, ${(p) => p.theme.color.surface});
  }
  .view:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 0;
  }
  ${(p) => p.stylesOverride}
`;

// region Component
interface ProcessesSectionProps {
  processes: Diagnostics["processes"];
  installId: string;
  stylesOverride?: SerializedStyles;
}

/** The controller and proxies lines, each with the server's state, and a link to the Processes page (design §6). */
export function ProcessesSection({ processes, installId, stylesOverride }: ProcessesSectionProps) {
  const id = useId();
  return (
    <section aria-labelledby={id}>
      <h2 id={id}>{diagnosticsCopy.processesTitle}</h2>
      <Box stylesOverride={stylesOverride}>
        <ProcessLine kind="controller" summary={processes.controller} />
        <ProcessLine kind="proxies" summary={processes.proxies} />
        <Link className="view" to={installPath(installId, "processes")}>
          {diagnosticsCopy.viewProcesses}
        </Link>
      </Box>
    </section>
  );
}
