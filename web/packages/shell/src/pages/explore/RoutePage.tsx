import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useMemo } from "react";
import { useParams } from "react-router-dom";
import { useRouteDetail } from "../../api/exploreHooks";
import type { LinkedRef, RouteDetail } from "../../api/types";
import { detailCopy, routeCopy } from "../../copy/explorer";
import { useInstallId } from "../../shell/extension";
import { DetailFrame, Facts, Section } from "./detailParts";
import { Muted } from "./listParts";
import { ChildRoutes } from "./route/ChildRoutes";
import { ParentCard } from "./route/ParentCard";
import { RuleList } from "./route/RuleList";
import { isRouteRef, sameParentRef } from "./route/refs";
import { UpChain } from "./route/UpChain";

// region Styles
const Cards = styled.div`
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 320px), 1fr));
  gap: ${(p) => p.theme.space(3)};
`;

// region Helpers
/** A child takes its hostnames from a parent route; a route under a listener from the listener. */
function hostnameText(d: RouteDetail) {
  if (d.hostnames.length) return d.hostnames.join(", ");
  const child = d.parentRefs.some(isRouteRef) || d.parents.some((p) => isRouteRef(p.target));
  return <Muted>{child ? routeCopy.inheritsParent : routeCopy.inheritsListener}</Muted>;
}

/** Which parent routes may delegate here, from the spec's parentRefs (computed-rules §5.3). */
function permissionText(d: RouteDetail) {
  if (!d.parentRefs.length) return routeCopy.allowsAny;
  const routes = d.parentRefs.filter(isRouteRef).map((p) => `${p.namespace}/${p.name}`);
  return routes.length ? routeCopy.allows(routes) : routeCopy.allowsNone;
}

// region Component
interface RoutePageProps {
  stylesOverride?: SerializedStyles;
}

/** One HTTPRoute: up-chain, parents, rules as written and its child routes (design §8.5). */
export function RoutePage({ stylesOverride }: RoutePageProps = {}) {
  const id = useInstallId();
  const { group = "", kind = "", ns = "", name = "" } = useParams();
  const ref = useMemo<LinkedRef>(() => ({ group, kind, namespace: ns, name, link: "detail" }), [group, kind, ns, name]);
  const { data, error } = useRouteDetail(id, ref);
  const hasDelegation = !!data?.rules.some((r) => r.backendRefs.some((b) => b.delegation));
  return (
    <DetailFrame
      installId={id}
      refValue={ref}
      noun="HTTPRoute"
      data={data}
      error={error}
      stylesOverride={stylesOverride}
    >
      {data ? (
        <>
          <UpChain installId={id} self={ref} paths={data.ancestors} truncated={data.ancestorsTruncated} />
          <Section aria-label={detailCopy.summary}>
            <Facts>
              <dt>{routeCopy.hostnames}</dt>
              <dd>{hostnameText(data)}</dd>
              <dt>{routeCopy.parentRoutes}</dt>
              <dd>{permissionText(data)}</dd>
            </Facts>
          </Section>
          <Section aria-labelledby="route-parents">
            <h2 id="route-parents">
              {routeCopy.parents} <span className="note">{data.parentRefs.length}</span>
            </h2>
            {data.parentRefs.length ? (
              <Cards>
                {data.parentRefs.map((p) => (
                  <ParentCard
                    key={[p.group, p.kind, p.namespace, p.name, p.sectionName, p.port].join("/")}
                    installId={id}
                    parentRef={p}
                    entries={data.parents.filter((s) => sameParentRef(s.parentRef, p))}
                    generation={data.generation}
                    hasDelegation={hasDelegation}
                  />
                ))}
              </Cards>
            ) : (
              <Muted>{routeCopy.noParentRefs}</Muted>
            )}
          </Section>
          <Section aria-labelledby="route-rules">
            <h2 id="route-rules">
              {routeCopy.rules} <span className="note">{data.rules.length}</span>
            </h2>
            <RuleList installId={id} rules={data.rules} />
          </Section>
          <ChildRoutes installId={id} route={data.ref} expected={data.childCount} />
        </>
      ) : null}
    </DetailFrame>
  );
}
