import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { dump } from "js-yaml";
import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useResource } from "../api/hooks";
import { CopyNameButton } from "../resource/CopyNameButton";
import { ResourceName } from "../resource/ResourceName";
import { useInstallId } from "../shell/extension";
import { PageState } from "../states/PageState";

// region Styles
const Page = styled.div<{ stylesOverride?: SerializedStyles }>`
  max-width: 1400px;
  margin: 0 auto;
  padding: ${(p) => p.theme.space(4)};
  header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: ${(p) => p.theme.space(2)};
    margin-bottom: ${(p) => p.theme.space(3)};
  }
  h1 {
    display: inline-flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: ${(p) => p.theme.space(2)};
    min-width: 0;
    margin: 0;
    font-size: ${(p) => p.theme.font.size.lg};
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  .kind {
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.base};
    font-weight: ${(p) => p.theme.font.weight.regular};
  }
  .spacer {
    flex: 1;
  }
  pre {
    margin: 0;
    padding: ${(p) => p.theme.space(4)};
    overflow: auto;
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
    line-height: 1.55;
    color: ${(p) => p.theme.color.text};
    background: ${(p) => p.theme.color.surface};
    border: 1px solid ${(p) => p.theme.color.border};
    border-radius: ${(p) => p.theme.radius.md};
  }
  ${(p) => p.stylesOverride}
`;
const TextButton = styled.button`
  height: 28px;
  padding: 0 10px;
  cursor: pointer;
  font: inherit;
  font-size: ${(p) => p.theme.font.size.sm};
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
const BackLink = styled(Link)`
  display: inline-block;
  margin-top: ${(p) => p.theme.space(2)};
  padding: 4px 8px;
  border-radius: ${(p) => p.theme.radius.sm};
  color: ${(p) => p.theme.color.accentText};
  font-weight: ${(p) => p.theme.font.weight.semibold};
  text-decoration: none;
  &:hover {
    text-decoration: underline;
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.bg});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 12%, ${(p) => p.theme.color.bg});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
  }
`;

// region Helpers
const LAST_APPLIED = "kubectl.kubernetes.io/last-applied-configuration";

type Obj = Record<string, unknown>;

function isObj(v: unknown): v is Obj {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Status first, then the rest in served order: the health-relevant part leads (design §2 flow 2). */
function statusFirst(o: Obj): Obj {
  const { status, ...rest } = o;
  return status === undefined ? rest : { status, ...rest };
}

/** o without the kubectl last-applied annotation, which repeats the spec as one long JSON line. */
function withoutLastApplied(o: Obj): Obj {
  const meta = o.metadata;
  if (!isObj(meta) || !isObj(meta.annotations) || !(LAST_APPLIED in meta.annotations)) return o;
  const { [LAST_APPLIED]: _, ...annotations } = meta.annotations;
  const { annotations: __, ...restMeta } = meta;
  return { ...o, metadata: Object.keys(annotations).length ? { ...restMeta, annotations } : restMeta };
}

function hasLastApplied(o: Obj): boolean {
  return isObj(o.metadata) && isObj(o.metadata.annotations) && LAST_APPLIED in o.metadata.annotations;
}

// region Component
interface ResourcePageProps {
  stylesOverride?: SerializedStyles;
}

/** Read-only YAML view of one resource. */
export function ResourcePage({ stylesOverride }: ResourcePageProps = {}) {
  const id = useInstallId();
  const { group = "-", kind = "", rns = "-", rname = "" } = useParams();
  const ref = { group: group === "-" ? "" : group, kind, namespace: rns === "-" ? undefined : rns, name: rname };
  const { data, error } = useResource(id, ref);
  const [showLastApplied, setShowLastApplied] = useState(false);
  const yaml = useMemo(
    () =>
      data
        ? dump(statusFirst(showLastApplied ? data : withoutLastApplied(data)), { noRefs: true, lineWidth: 140 })
        : "",
    [data, showLastApplied],
  );
  // The backend answers 404 alike for objects outside the install and objects the caller may not view (D-013).
  if (error?.status === 404)
    return (
      <PageState
        kind="empty"
        title="Not in this install's scope"
        message={
          <>
            {kind} <ResourceName namespace={ref.namespace} name={rname} />
            <br />
            <BackLink to={`/i/${id}`}>Back to install</BackLink>
          </>
        }
      />
    );
  if (error?.status === 503)
    return <PageState kind="connecting" title="Connecting to cluster…" message={error.message} />;
  if (error)
    return <PageState kind="error" title="Cannot load resource" message={error.message} copyText={error.message} />;
  if (!data) return <PageState kind="loading" title="Loading…" />;
  const lastApplied = hasLastApplied(data);
  return (
    <Page stylesOverride={stylesOverride}>
      <header>
        <h1>
          <span className="kind">{kind}</span>
          <ResourceName namespace={ref.namespace} name={rname} />
        </h1>
        <CopyNameButton resource={ref} />
        <span className="spacer" />
        {lastApplied ? (
          <TextButton type="button" aria-expanded={showLastApplied} onClick={() => setShowLastApplied((s) => !s)}>
            {showLastApplied ? "Hide last-applied configuration" : "Show last-applied configuration"}
          </TextButton>
        ) : null}
      </header>
      <pre data-testid="yaml">{yaml}</pre>
    </Page>
  );
}
