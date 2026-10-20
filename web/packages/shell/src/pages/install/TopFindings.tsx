import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Link } from "react-router-dom";
import type { Finding } from "../../api/types";
import { installPath } from "../../shell/paths";
import { FindingRow } from "./FindingRow";

// region Styles
const Box = styled.section<{ stylesOverride?: SerializedStyles }>`
  margin-top: ${(p) => p.theme.space(2)};
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  overflow: hidden;
  .more {
    display: block;
    padding: ${(p) => p.theme.space(2.5)} ${(p) => p.theme.space(3)};
    border-top: 1px solid ${(p) => p.theme.color.border};
    color: ${(p) => p.theme.color.accentText};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    text-decoration: none;
  }
  .more:hover {
    text-decoration: underline;
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 5%, ${(p) => p.theme.color.surface});
  }
  .more:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 12%, ${(p) => p.theme.color.surface});
  }
  .more:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: -2px;
  }
  ${(p) => p.stylesOverride}
`;

// region Helpers
/** Keys from the finding's identity; repeats of the same identity get an occurrence suffix. */
export function findingKeys(findings: Finding[]): string[] {
  const seen = new Map<string, number>();
  return findings.map((f) => {
    const r = f.resource;
    const base = [f.code, r?.group, r?.kind, r?.namespace, r?.name, f.message, f.detail].join("|");
    const n = seen.get(base) ?? 0;
    seen.set(base, n + 1);
    return n ? `${base}#${n}` : base;
  });
}

// region Component
interface TopFindingsProps {
  findings: Finding[];
  installId: string;
  stylesOverride?: SerializedStyles;
}

/** The top 5 findings (sorted by severity by the backend) and a link to all. */
export function TopFindings({ findings, installId, stylesOverride }: TopFindingsProps) {
  if (findings.length === 0) return null;
  const top = findings.slice(0, 5);
  const keys = findingKeys(top);
  return (
    <Box aria-label="Findings" stylesOverride={stylesOverride}>
      {top.map((f, i) => (
        <FindingRow key={keys[i]} finding={f} installId={installId} />
      ))}
      <Link className="more" to={installPath(installId, "findings")}>
        Show all {findings.length} findings
      </Link>
    </Box>
  );
}
