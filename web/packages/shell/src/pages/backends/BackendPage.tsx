import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useMemo } from "react";
import { useParams } from "react-router-dom";
import { useBackendDetail } from "../../api/exploreHooks";
import type { BackendDetail, LinkedRef } from "../../api/types";
import { factCopy } from "../../copy/backends";
import { detailCopy } from "../../copy/explorer";
import { ConditionList } from "../../explore/ConditionList";
import { useInstallId, useVocabulary } from "../../shell/extension";
import { DetailFrame, Facts, Section } from "../explore/detailParts";
import { EndpointsSection } from "./EndpointsSection";
import { Explanations } from "./Explanations";
import { ExposureSection } from "./ExposureSection";
import { PortChain } from "./PortChain";
import { StatusLine } from "./StatusLine";

// region Styles
const Conditions = styled.div`
  margin-top: ${(p) => p.theme.space(3)};
`;

// region Helpers
const isService = (r: { group: string; kind: string }) => r.group === "" && r.kind === "Service";

/** The refs the page links, for the trail: the routes and Gateways of its paths, and its consumers. */
function linkedRefs(d: BackendDetail): LinkedRef[] {
  const out: LinkedRef[] = [];
  for (const r of d.exposure.rows) out.push(r.gateway, ...r.routes);
  for (const c of d.consumers) out.push(c.consumer);
  return out;
}

// region Component
interface BackendPageProps {
  stylesOverride?: SerializedStyles;
}

/**
 * How one backend is exposed (Phase 6 design §8.2): the impact, its paths and other users, endpoints per port, the
 * port chain, why refs to it do not resolve, and its conditions or cached Service facts. A Service has no YAML tab:
 * the cache keeps its facts, not the object.
 */
export function BackendPage({ stylesOverride }: BackendPageProps = {}) {
  const id = useInstallId();
  const vocab = useVocabulary();
  const { group = "", kind = "", ns = "", name = "" } = useParams();
  const g = group === "-" ? "" : group;
  const ref = useMemo<LinkedRef>(() => ({ group: g, kind, namespace: ns, name, link: "none" }), [g, kind, ns, name]);
  const { data, error, mutate } = useBackendDetail(id, ref);
  const frameData = useMemo(
    () => (data ? { health: data.problem.state, collectedAt: data.collectedAt } : undefined),
    [data],
  );
  const trailRefs = useMemo(() => (data ? linkedRefs(data) : undefined), [data]);
  const service = isService(ref);
  const typeLabel = vocab.backendKinds.find((k) => k.group === g && k.kind === kind)?.typeLabel;
  const generation = Math.max(0, ...(data?.conditions ?? []).map((c) => c.observedGeneration ?? 0));
  return (
    <DetailFrame
      installId={id}
      refValue={ref}
      noun={kind}
      data={frameData}
      error={error}
      trailRefs={trailRefs}
      yaml={!service}
      stylesOverride={stylesOverride}
    >
      {data ? (
        <>
          <StatusLine detail={data} />
          <ExposureSection installId={id} backend={ref} exposure={data.exposure} consumers={data.consumers} />
          <EndpointsSection sources={data.sources} onRetry={() => mutate()} />
          {data.portChain.length ? <PortChain chains={data.portChain} /> : null}
          {data.explanations.length ? <Explanations installId={id} explanations={data.explanations} /> : null}
          <Section aria-labelledby="backend-facts">
            <h2 id="backend-facts">{service ? factCopy.service : detailCopy.conditions}</h2>
            <Facts>
              <dt>{factCopy.kind}</dt>
              <dd>{data.ref.kind}</dd>
              {data.type ? (
                <>
                  <dt>{typeLabel ?? factCopy.type}</dt>
                  <dd>{data.type}</dd>
                </>
              ) : null}
              {data.service ? (
                <>
                  <dt>{factCopy.serviceType}</dt>
                  <dd>{data.service.type}</dd>
                  <dt>{factCopy.ports}</dt>
                  <dd>{data.service.ports.map((p) => (p.name ? `${p.port} (${p.name})` : `${p.port}`)).join(", ")}</dd>
                </>
              ) : null}
            </Facts>
            {service ? null : (
              <Conditions>
                <ConditionList conditions={data.conditions ?? []} generation={generation} />
              </Conditions>
            )}
          </Section>
        </>
      ) : null}
    </DetailFrame>
  );
}
