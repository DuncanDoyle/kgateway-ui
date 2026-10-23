import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { dump } from "js-yaml";
import { useMemo, useState } from "react";
import { useResource } from "../api/hooks";
import type { Ref } from "../api/types";
import { useVocabulary } from "../shell/extension";
import { TextButton } from "../states/controls";
import { PageNotice } from "../states/PageNotice";
import { PageState } from "../states/PageState";

// The YAML view's body, shared by the YAML page and the detail pages' YAML tab (design §8.1).

// region Styles
export const YamlBlock = styled.pre<{ stylesOverride?: SerializedStyles }>`
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
  ${(p) => p.stylesOverride}
`;
const Toolbar = styled.div`
  display: flex;
  justify-content: flex-end;
  /* Reserved, so the toggle arriving with the data moves nothing. */
  min-height: 28px;
  margin-bottom: ${(p) => p.theme.space(2)};
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

/** The object as YAML, status first, with the last-applied annotation hidden until asked. */
export function useYaml(data: Obj | undefined) {
  const [showLastApplied, setShowLastApplied] = useState(false);
  const yaml = useMemo(
    () =>
      data
        ? dump(statusFirst(showLastApplied ? data : withoutLastApplied(data)), { noRefs: true, lineWidth: 140 })
        : "",
    [data, showLastApplied],
  );
  return {
    yaml,
    lastApplied: !!data && hasLastApplied(data),
    showLastApplied,
    toggle: () => setShowLastApplied((s) => !s),
  };
}

// region Component
interface LastAppliedToggleProps {
  shown: boolean;
  onToggle: () => void;
}

export function LastAppliedToggle({ shown, onToggle }: LastAppliedToggleProps) {
  return (
    <TextButton type="button" aria-expanded={shown} onClick={onToggle}>
      {shown ? "Hide last-applied config" : "Show last-applied config"}
    </TextButton>
  );
}

interface ResourceYamlProps {
  installId: string;
  refValue: Ref;
  stylesOverride?: SerializedStyles;
}

/** The YAML of one resource with its own load states, for a tab inside a page that has its own header. */
export function ResourceYaml({ installId, refValue, stylesOverride }: ResourceYamlProps) {
  const vocab = useVocabulary();
  const { data, error } = useResource(installId, refValue);
  const y = useYaml(data);
  if (!data) {
    if (error?.status === 404) return <PageState kind="empty" title="Not in this install's scope" />;
    if (error?.status === 503) return <PageState kind="connecting" title={vocab.connecting} message={error.message} />;
    if (error)
      return <PageState kind="error" title="Cannot load resource" message={error.message} copyText={error.message} />;
    return <PageState kind="loading" title="Loading resource…" />;
  }
  return (
    <>
      {error ? <PageNotice error={error} title="Cannot load resource" /> : null}
      <Toolbar>{y.lastApplied ? <LastAppliedToggle shown={y.showLastApplied} onToggle={y.toggle} /> : null}</Toolbar>
      <YamlBlock data-testid="yaml" stylesOverride={stylesOverride}>
        {y.yaml}
      </YamlBlock>
    </>
  );
}
