import type { SerializedStyles } from "@emotion/react";
import { useMemo } from "react";
import { useParams } from "react-router-dom";
import { useGatewayDetail, useListenerSets } from "../../api/exploreHooks";
import type { LinkedRef, ListenerSetRow } from "../../api/types";
import { columnCopy, detailCopy, gatewayCopy } from "../../copy/explorer";
import { ConditionList } from "../../explore/ConditionList";
import type { ObjectColumn } from "../../explore/ObjectTable";
import { HealthBadge } from "../../health/HealthBadge";
import { useInstallId } from "../../shell/extension";
import { DetailFrame, expectedRows, Facts, refParam, Section, SubList } from "./detailParts";
import { ListenerTable } from "./gateway/ListenerTable";
import { Muted, NameCell } from "./listParts";
import { RouteSections } from "./RouteSections";

// region Helpers
const GW = "gateway.networking.k8s.io";
const LS_NOUN = ["ListenerSet", "ListenerSets"] as const;

function lsColumns(installId: string): ObjectColumn<ListenerSetRow>[] {
  return [
    { id: "health", header: columnCopy.status, width: "120px", cell: (r) => <HealthBadge state={r.health} /> },
    { id: "name", header: columnCopy.listenerSet, cell: (r) => <NameCell r={r.ref} installId={installId} /> },
    { id: "kind", header: columnCopy.kind, width: "140px", secondary: true, cell: (r) => r.ref.kind },
    { id: "listeners", header: columnCopy.listeners, width: "100px", cell: (r) => r.listeners },
  ];
}
const lsRef = (r: ListenerSetRow) => r.ref;

// region Component
interface GatewayPageProps {
  stylesOverride?: SerializedStyles;
}

/** One Gateway: conditions, listeners, its ListenerSets, and the routes attached or failing to attach (design §8.5). */
export function GatewayPage({ stylesOverride }: GatewayPageProps = {}) {
  const id = useInstallId();
  const { ns = "", name = "" } = useParams();
  const { data, error } = useGatewayDetail(id, ns, name);
  const ref = useMemo<LinkedRef>(
    () => ({ group: GW, kind: "Gateway", namespace: ns, name, link: "detail" }),
    [ns, name],
  );
  const lsQuery = useMemo(() => ({ parent: refParam(ref) }), [ref]);
  const lsCols = useMemo(() => lsColumns(id), [id]);
  return (
    <DetailFrame installId={id} refValue={ref} noun="Gateway" data={data} error={error} stylesOverride={stylesOverride}>
      {data ? (
        <>
          <Section aria-labelledby="gw-conditions">
            <h2 id="gw-conditions">{detailCopy.conditions}</h2>
            <ConditionList conditions={data.conditions} generation={data.generation} />
          </Section>
          <Section aria-label={detailCopy.summary}>
            <Facts>
              <dt>{columnCopy.class}</dt>
              <dd>{data.class}</dd>
              <dt>{columnCopy.addresses}</dt>
              <dd>{data.addresses.length ? data.addresses.join(", ") : <Muted>{columnCopy.none}</Muted>}</dd>
            </Facts>
          </Section>
          <Section aria-labelledby="gw-listeners">
            <h2 id="gw-listeners">
              {gatewayCopy.listeners} <span className="note">{data.listeners.length}</span>
            </h2>
            <ListenerTable listeners={data.listeners} generation={data.generation} />
          </Section>
          <Section aria-labelledby="gw-listenersets">
            <h2 id="gw-listenersets">
              {gatewayCopy.listenerSets}
              {data.attachedListenerSets !== undefined ? (
                <span className="note">{gatewayCopy.attachedListenerSets(data.attachedListenerSets)}</span>
              ) : null}
            </h2>
            <SubList
              installId={id}
              label={gatewayCopy.listenerSets}
              noun={LS_NOUN}
              useList={useListenerSets}
              query={lsQuery}
              columns={lsCols}
              rowRef={lsRef}
              emptyTitle={gatewayCopy.noListenerSets}
              // The status count is a hint: the list counts visible ListenerSets, the status all attached ones.
              skeletonRows={expectedRows(data.attachedListenerSets ?? 1)}
            />
          </Section>
          <RouteSections installId={id} parent={ref} counts={data.routeCounts} />
        </>
      ) : null}
    </DetailFrame>
  );
}
