import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useParams } from "react-router-dom";
import { useResource } from "../api/hooks";
import { detailCopy, listCopy } from "../copy/explorer";
import { CopyNameButton } from "../resource/CopyNameButton";
import { ResourceName } from "../resource/ResourceName";
import { LastAppliedToggle, useYaml, YamlBlock } from "../resource/ResourceYaml";
import { useInstallId, useVocabulary } from "../shell/extension";
import { installPath } from "../shell/paths";
import { BackLink, InlineLink } from "../states/controls";
import { PageNotice } from "../states/PageNotice";
import { PageState } from "../states/PageState";
import { DataAge } from "./DataAge";
import { backendParam } from "./explore/route/refs";

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
  // After a first load, other errors keep the YAML on screen under a notice.
  if (!data) {
    if (error?.status === 503) return <PageState kind="connecting" title={vocab.connecting} message={error.message} />;
    if (error)
      return <PageState kind="error" title="Cannot load resource" message={error.message} copyText={error.message} />;
    return <PageState kind="loading" title="Loading resource…" />;
  }
  return (
    <Page stylesOverride={stylesOverride}>
      {error ? <PageNotice error={error} title="Cannot load resource" /> : null}
      <header>
        <h1>
          <span className="kind">{kind}</span>
          <ResourceName namespace={ref.namespace} name={rname} />
        </h1>
        <CopyNameButton resource={ref} />
        {/* The resource endpoint serves only admitted Backends (design §7.1); Services have no YAML view. */}
        {isBackend(ref) ? (
          <InlineLink to={`${installPath(id, "routes")}?${new URLSearchParams({ backend: backendParam(ref) })}`}>
            {detailCopy.backendLookup}
          </InlineLink>
        ) : null}
        <span className="spacer" />
        {collectedAt ? <DataAge collectedAt={collectedAt} /> : null}
        {y.lastApplied ? <LastAppliedToggle shown={y.showLastApplied} onToggle={y.toggle} /> : null}
      </header>
      <YamlBlock data-testid="yaml">{y.yaml}</YamlBlock>
    </Page>
  );
}
