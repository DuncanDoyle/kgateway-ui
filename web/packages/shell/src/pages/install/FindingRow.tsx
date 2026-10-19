import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Copy } from "lucide-react";
import { Link } from "react-router-dom";
import { isLinkable, useMeta } from "../../api/hooks";
import type { Finding, Ref } from "../../api/types";
import { HealthBadge } from "../../health/HealthBadge";
import { severityToState } from "../../health/state";
import { ResourceName } from "../../resource/ResourceName";
import { useShellExtension } from "../../shell/extension";
import { Tooltip } from "../../shell/Tooltip";

// region Styles
const Row = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  grid-template-columns: 112px minmax(180px, 300px) 1fr 28px;
  align-items: start;
  gap: ${(p) => p.theme.space(3)};
  padding: ${(p) => p.theme.space(2.5)} ${(p) => p.theme.space(3)};
  border-bottom: 1px solid ${(p) => p.theme.color.border};
  &:last-of-type {
    border-bottom: 0;
  }
  a {
    color: inherit;
    text-decoration: none;
    border-radius: ${(p) => p.theme.radius.sm};
  }
  a:hover [data-part="name"] {
    text-decoration: underline;
  }
  a:active [data-part="name"] {
    color: ${(p) => p.theme.color.accentText};
  }
  a:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
  .msg {
    color: ${(p) => p.theme.color.text};
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
    margin-top: 2px;
    overflow-wrap: anywhere;
  }
  .code {
    font-family: ${(p) => p.theme.font.mono};
  }
  @media (max-width: 720px) {
    grid-template-columns: 1fr 28px;
    > :nth-of-type(3) {
      grid-column: 1 / -1;
    }
  }
  ${(p) => p.stylesOverride}
`;
const IconButton = styled.button`
  width: 28px;
  height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  background: transparent;
  color: ${(p) => p.theme.color.textMuted};
  border: 1px solid transparent;
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    border-color: ${(p) => p.theme.color.borderStrong};
    color: ${(p) => p.theme.color.text};
  }
  &:active {
    background: ${(p) => p.theme.color.surfaceRaised};
    border-color: ${(p) => p.theme.color.text};
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
  }
`;

// region Helpers
/** Path of the read-only YAML view of r. */
export function resourcePath(installId: string, r: Ref): string {
  return `/i/${installId}/r/${r.group || "-"}/${r.kind}/${r.namespace || "-"}/${r.name}`;
}

const severityLabel = { error: "Error", warning: "Warning", info: "Info" } as const;

function copyText(r: Ref): string {
  return r.namespace ? `${r.namespace}/${r.name}` : r.name;
}

// region Component
interface FindingRowProps {
  finding: Finding;
  installId: string;
  stylesOverride?: SerializedStyles;
}

/** One finding: severity, code, resource, message, detail. The resource links to its YAML view when the backend serves its kind. */
export function FindingRow({ finding: f, installId, stylesOverride }: FindingRowProps) {
  const { findingRenderers } = useShellExtension();
  const { data: meta } = useMeta();
  const extra = Object.entries(findingRenderers).find(([prefix]) => f.code.startsWith(prefix))?.[1];
  const [ins, inn] = f.install.split("/");
  const name = f.resource ? (
    <ResourceName namespace={f.resource.namespace} name={f.resource.name} />
  ) : (
    <ResourceName namespace={ins} name={inn ?? f.install} />
  );
  // A restricted finding (D-013) never links: its resource would 404 for this caller.
  const linked = !!f.resource && !f.restricted && isLinkable(meta, f.resource);
  return (
    <Row data-testid="finding-row" stylesOverride={stylesOverride}>
      <div>
        <HealthBadge state={severityToState(f.severity)} label={severityLabel[f.severity]} />
        <div className="muted code">{f.code}</div>
      </div>
      <div>
        {linked && f.resource ? <Link to={resourcePath(installId, f.resource)}>{name}</Link> : name}
        {f.resource ? <div className="muted">{f.resource.kind}</div> : null}
      </div>
      <div>
        {f.restricted ? (
          <div className="muted">Restricted</div>
        ) : (
          <>
            <div className="msg">{f.message}</div>
            {f.detail ? <div className="muted">{f.detail}</div> : null}
            {extra ? extra(f) : null}
          </>
        )}
      </div>
      {f.resource ? (
        <Tooltip label="Copy namespace/name" placement="end">
          <IconButton
            type="button"
            aria-label="Copy resource name"
            onClick={() => {
              if (f.resource) void navigator.clipboard?.writeText(copyText(f.resource)).catch(() => {});
            }}
          >
            <Copy size={14} aria-hidden="true" />
          </IconButton>
        </Tooltip>
      ) : (
        <span />
      )}
    </Row>
  );
}
