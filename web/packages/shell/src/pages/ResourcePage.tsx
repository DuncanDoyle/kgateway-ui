import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useParams } from "react-router-dom";
import { useResource } from "../api/hooks";
import { detailCopy, listCopy } from "../copy/explorer";
import { CopyNameButton } from "../resource/CopyNameButton";
import { ResourceName } from "../resource/ResourceName";
import { LastAppliedToggle, useYaml, YamlBlock } from "../resource/ResourceYaml";
import { useInstallId, useVocabulary } from "../shell/extension";
import { layout } from "../shell/layout";
import { ObjectHeader } from "../shell/ObjectHeader";
import { installPath } from "../shell/paths";
import { BackLink, InlineLink } from "../states/controls";
import { PageNotice } from "../states/PageNotice";
import { PageState } from "../states/PageState";
import { backendParam } from "./explore/route/refs";

// region Styles
const Page = styled.div<{ stylesOverride?: SerializedStyles }>`
  max-width: ${layout.maxWidth}px;
  margin: 0 auto;
  padding: ${(p) => p.theme.space(4)};
  .tools {
    display: flex;
    justify-content: flex-end;
    min-height: 28px;
    margin-bottom: ${(p) => p.theme.space(2)};
  }
  ${(p) => p.stylesOverride}
`;

// region Helpers
const isBackend = (r: { group: string; kind: string }) => r.group === "gateway.kgateway.dev" && r.kind === "Backend";

// region Component
interface ResourcePageProps {
  stylesOverride?: SerializedStyles;
}

/** Read-only YAML view of one resource. */
export function ResourcePage({ stylesOverride }: ResourcePageProps = {}) {
  const id = useInstallId();
  const vocab = useVocabulary();
  const { group = "-", kind = "", rns = "-", rname = "" } = useParams();
  const ref = { group: group === "-" ? "" : group, kind, namespace: rns === "-" ? undefined : rns, name: rname };
  const { data, error, collectedAt } = useResource(id, ref);
  const y = useYaml(data);
  // The backend answers 404 alike for objects outside the install and objects the caller may not view (D-013).
  if (error?.status === 404)
    return (
      <PageState
        kind="empty"
        title={listCopy.notInScope}
        message={
          <>
            {kind} <ResourceName namespace={ref.namespace} name={rname} />
            <br />
            <BackLink to={installPath(id)}>{listCopy.backToInstall}</BackLink>
          </>
        }
      />
    );
  // The header shows from the first render; after a first load, other errors keep the YAML under a notice.
  const body = data ? (
    <>
      {y.lastApplied ? (
        <div className="tools">
          <LastAppliedToggle shown={y.showLastApplied} onToggle={y.toggle} />
        </div>
      ) : null}
      <YamlBlock data-testid="yaml">{y.yaml}</YamlBlock>
    </>
  ) : error?.status === 503 ? (
    <PageState kind="connecting" title={vocab.connecting} message={error.message} />
  ) : error ? (
    <PageState kind="error" title="Cannot load resource" message={error.message} copyText={error.message} />
  ) : (
    <PageState kind="loading" title="Loading resource…" />
  );
  return (
    <Page data-testid="page-frame" stylesOverride={stylesOverride}>
      {data && error ? <PageNotice error={error} title="Cannot load resource" /> : null}
      <ObjectHeader
        kind={{ group: ref.group, kind, label: kind }}
        name={rname}
        namespace={ref.namespace}
        collectedAt={data ? collectedAt : undefined}
        actions={
          <>
            <CopyNameButton resource={ref} />
            {/* The resource endpoint serves only admitted Backends (design §7.1); Services have no YAML view. */}
            {isBackend(ref) ? (
              <InlineLink to={`${installPath(id, "routes")}?${new URLSearchParams({ backend: backendParam(ref) })}`}>
                {detailCopy.backendLookup}
              </InlineLink>
            ) : null}
          </>
        }
      />
      {body}
    </Page>
  );
}
