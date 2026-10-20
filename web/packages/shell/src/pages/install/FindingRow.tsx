import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Link } from "react-router-dom";
import { isLinkable, useInstalls, useMeta } from "../../api/hooks";
import type { Finding, Ref } from "../../api/types";
import { HealthBadge } from "../../health/HealthBadge";
import { severityLabel, severityToState } from "../../health/state";
import { CopyNameButton } from "../../resource/CopyNameButton";
import { ResourceName } from "../../resource/ResourceName";
import { type FindingRenderer, useShellExtension } from "../../shell/extension";
import { InstallName } from "../../shell/InstallName";
import { installPath } from "../../shell/paths";

// region Styles
const Row = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  grid-template-columns: 112px minmax(180px, 300px) 1fr 28px;
  grid-template-areas: "sev res msg copy";
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
  .sev {
    grid-area: sev;
  }
  .res {
    grid-area: res;
    min-width: 0;
  }
  .text {
    grid-area: msg;
    min-width: 0;
  }
  .copy {
    grid-area: copy;
  }
  @media (max-width: 720px) {
    grid-template-columns: minmax(0, 1fr) 28px;
    grid-template-areas: "sev copy" "res res" "msg msg";
    row-gap: ${(p) => p.theme.space(1.5)};
  }
  ${(p) => p.stylesOverride}
`;

// region Helpers
/** Path of the read-only YAML view of r. */
export function resourcePath(installId: string, r: Ref): string {
  return installPath(installId, `r/${r.group || "-"}/${r.kind}/${r.namespace || "-"}/${r.name}`);
}

/** The renderer registered for the longest prefix of code, if any (see ShellExtension.findingRenderers). */
export function rendererFor(renderers: Record<string, FindingRenderer>, code: string): FindingRenderer | undefined {
  let best: string | undefined;
  for (const prefix of Object.keys(renderers)) {
    if (code.startsWith(prefix) && (best === undefined || prefix.length > best.length)) best = prefix;
  }
  return best === undefined ? undefined : renderers[best];
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
  const extra = rendererFor(findingRenderers, f.code);
  const { data: installs } = useInstalls();
  // A finding without a resource is about the install; it is named, never split from its ID.
  const install = installs?.find((i) => i.id === f.install);
  const name = f.resource ? (
    <ResourceName namespace={f.resource.namespace} name={f.resource.name} />
  ) : install ? (
    <InstallName install={install} />
  ) : (
    <ResourceName name={f.install} />
  );
  // A restricted finding (D-013) never links: its resource would 404 for this caller.
  const linked = !!f.resource && !f.restricted && isLinkable(meta, f.resource);
  return (
    <Row data-testid="finding-row" stylesOverride={stylesOverride}>
      <div className="sev">
        <HealthBadge state={severityToState(f.severity)} label={severityLabel[f.severity]} />
        <div className="muted code">{f.code}</div>
      </div>
      <div className="res">
        {linked && f.resource ? <Link to={resourcePath(installId, f.resource)}>{name}</Link> : name}
        {f.resource ? <div className="muted">{f.resource.kind}</div> : null}
      </div>
      <div className="text">
        {f.restricted ? (
          <div className="muted">Restricted: ask the owning team</div>
        ) : (
          <>
            <div className="msg">{f.message}</div>
            {f.detail ? <div className="muted">{f.detail}</div> : null}
            {extra ? extra(f) : null}
          </>
        )}
      </div>
      <div className="copy">{f.resource ? <CopyNameButton resource={f.resource} placement="end" /> : null}</div>
    </Row>
  );
}
