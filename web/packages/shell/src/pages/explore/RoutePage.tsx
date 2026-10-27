import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useMemo } from "react";
import { useParams } from "react-router-dom";
import { useRouteDetail } from "../../api/exploreHooks";
import { useListFilter } from "../../api/hooks";
import type { LinkedRef, ParentRef, RouteDetail } from "../../api/types";
import { delegationCopy, detailCopy, routeCopy } from "../../copy/explorer";
import { useInstallId } from "../../shell/extension";
import { DetailFrame, type ExtraTab, Facts, Section } from "./detailParts";
import { Muted } from "./listParts";
import { ChildRoutes } from "./route/ChildRoutes";
import { DelegationTab } from "./route/DelegationTab";
import { EffectiveMatches } from "./route/EffectiveMatches";
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

const refKey = (p: ParentRef) => [p.group, p.kind, p.namespace, p.name, p.sectionName, p.port].join("/");

/** Status entries' refs that no spec parentRef names, once each: a child's implicit parent (design §5.1). */
function unlistedRefs(d: RouteDetail): ParentRef[] {
  const out: ParentRef[] = [];
  for (const s of d.parents) {
    const p = s.parentRef;
    if (!d.parentRefs.some((r) => sameParentRef(r, p)) && !out.some((r) => sameParentRef(r, p))) out.push(p);
  }
  return out;
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
  const unlisted = useMemo(() => (data ? unlistedRefs(data) : []), [data]);
  // View B's tab, only for a parent route on a server that serves …/delegation (/meta filters.delegation, PF8).
  const viewB = useListFilter("delegation", "outcome");
  const rules = data?.rules;
  const extraTabs = useMemo<ExtraTab[] | undefined>(
    () =>
      viewB && hasDelegation && rules
        ? [
            {
              id: "delegation",
              label: delegationCopy.tab,
              content: <DelegationTab installId={id} route={ref} rules={rules} />,
            },
          ]
        : undefined,
    [viewB, hasDelegation, rules, id, ref],
  );
  return (
    <DetailFrame
      installId={id}
      refValue={ref}
      noun="HTTPRoute"
      data={data}
      error={error}
      extraTabs={extraTabs}
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
              {routeCopy.parents} <span className="note">{data.parentRefs.length + unlisted.length}</span>
            </h2>
            {data.parentRefs.length || unlisted.length ? (
              <Cards>
                {data.parentRefs.map((p, i) => (
                  <ParentCard
                    key={refKey(p)}
                    installId={id}
                    parentRef={p}
                    entries={data.parents.filter((s) => sameParentRef(s.parentRef, p))}
                    generation={data.generation}
                    hasDelegation={hasDelegation}
                    attachment={data.attachments?.find((a) => a.index === i)?.result}
                  />
                ))}
                {unlisted.map((p) => (
                  <ParentCard
                    key={`status/${refKey(p)}`}
                    installId={id}
                    parentRef={p}
                    entries={data.parents.filter((s) => sameParentRef(s.parentRef, p))}
                    generation={data.generation}
                    hasDelegation={hasDelegation}
                    origin={data.parentRefs.length ? "unlisted" : "implicit"}
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
          {data.effective?.length ? (
            <EffectiveMatches installId={id} edges={data.effective} truncated={data.effectiveTruncated} />
          ) : null}
          <ChildRoutes installId={id} route={data.ref} expected={data.childCount} />
        </>
      ) : null}
    </DetailFrame>
  );
}
