import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { dump } from "js-yaml";
import { useMemo } from "react";
import { useParams } from "react-router-dom";
import { useListenerSetDetail } from "../../api/exploreHooks";
import type { AllowedListeners, Condition, LinkedRef } from "../../api/types";
import { ConditionList } from "../../explore/ConditionList";
import { LinkedName } from "../../explore/LinkedName";
import { ResourceName } from "../../resource/ResourceName";
import { YamlBlock } from "../../resource/ResourceYaml";
import { useInstallId } from "../../shell/extension";
import { DetailFrame, Facts, Section } from "./detailParts";
import { ListenerTable } from "./gateway/ListenerTable";
import { Muted } from "./listParts";
import { RouteSections } from "./RouteSections";

// region Styles
const Allowed = styled.div`
  margin-top: ${(p) => p.theme.space(2)};
  padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.surface};
  font-size: ${(p) => p.theme.font.size.sm};
  h3 {
    margin: 0 0 ${(p) => p.theme.space(1)};
    font-size: ${(p) => p.theme.font.size.sm};
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  pre {
    padding: ${(p) => p.theme.space(2)};
  }
  p {
    margin: ${(p) => p.theme.space(1)} 0 0;
    line-height: 1.5;
  }
  code {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
  }
`;

// region Helpers
const isNotAllowed = (c: Condition) => c.type === "Accepted" && c.status === "False" && c.reason === "NotAllowed";

function AllowedListenersBlock({ a }: { a: AllowedListeners }) {
  return (
    <Allowed data-testid="allowed-listeners">
      <h3>
        Parent Gateway's <code>allowedListeners</code>
      </h3>
      {a.set ? (
        <YamlBlock>{dump(a.value ?? {}, { noRefs: true, lineWidth: 140 })}</YamlBlock>
      ) : (
        <p>Not set: the Gateway allows no ListenerSets.</p>
      )}
      {a.crdHint ? (
        <p>
          If you set <code>allowedListeners</code> and it is missing here, the Gateway CRD in this cluster may not have
          the field. The API server drops fields its CRD does not define. Install a Gateway CRD from the same Gateway
          API channel and version as the ListenerSet CRD.
        </p>
      ) : null}
    </Allowed>
  );
}

// region Component
interface ListenerSetPageProps {
  stylesOverride?: SerializedStyles;
}

/** One ListenerSet: its parent Gateway, conditions, listeners and routes (design §8.5). */
export function ListenerSetPage({ stylesOverride }: ListenerSetPageProps = {}) {
  const id = useInstallId();
  const { group = "", kind = "", ns = "", name = "" } = useParams();
  const ref = useMemo<LinkedRef>(() => ({ group, kind, namespace: ns, name, link: "detail" }), [group, kind, ns, name]);
  const { data, error } = useListenerSetDetail(id, ref);
  const allowed = data?.parentAllowedListeners;
  return (
    <DetailFrame
      installId={id}
      refValue={ref}
      noun="ListenerSet"
      data={data}
      error={error}
      stylesOverride={stylesOverride}
    >
      {data ? (
        <>
          <Section aria-label="Parent">
            <Facts>
              <dt>Parent Gateway</dt>
              <dd>
                {/* parentFound is false for a hidden parent too, so the page never says it does not exist (D-013). */}
                {data.parentFound ? (
                  <LinkedName refValue={data.parent} installId={id} />
                ) : (
                  <>
                    <ResourceName namespace={data.parent.namespace} name={data.parent.name} />{" "}
                    <Muted>Not available</Muted>
                  </>
                )}
              </dd>
            </Facts>
          </Section>
          <Section aria-labelledby="ls-conditions">
            <h2 id="ls-conditions">Conditions</h2>
            <ConditionList
              conditions={data.conditions}
              generation={data.generation}
              empty={data.experimentalHint}
              extra={(c) => (allowed && isNotAllowed(c) ? <AllowedListenersBlock a={allowed} /> : null)}
            />
          </Section>
          <Section aria-labelledby="ls-listeners">
            <h2 id="ls-listeners">
              Listeners <span className="note">{data.listeners.length}</span>
            </h2>
            <ListenerTable listeners={data.listeners} generation={data.generation} />
          </Section>
          <RouteSections installId={id} parent={data.ref} counts={data.routeCounts} />
        </>
      ) : null}
    </DetailFrame>
  );
}
