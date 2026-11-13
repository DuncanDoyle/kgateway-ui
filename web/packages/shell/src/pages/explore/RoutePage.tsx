import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useMemo } from "react";
import { useParams } from "react-router-dom";
import { useRouteDetail } from "../../api/exploreHooks";
import { useInstallFeature } from "../../api/hooks";
import type { LinkedRef, ParentRef, RouteDetail } from "../../api/types";
import { detailCopy, routeCopy, traceCopy } from "../../copy/explorer";
import { trafficCopy } from "../../copy/traffic";
import { firstFailing } from "../../explore/ProblemStrip";
import { useInstallId } from "../../shell/extension";
import { useTwoColumns } from "../../shell/layout";
import { installPath } from "../../shell/paths";
import { InlineLink } from "../../states/controls";
import { DetailFrame, Facts, refParam, Section } from "./detailParts";
import { Muted } from "./listParts";
import { ChildRoutes } from "./route/ChildRoutes";
import { EffectiveMatches } from "./route/EffectiveMatches";
import { ParentCard } from "./route/ParentCard";
import { RuleList } from "./route/RuleList";
import { RuleTable } from "./route/RuleTable";
import { isRouteRef, sameParentRef } from "./route/refs";
import { Serves } from "./route/Serves";
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

/** The refs a route's data links: spec and status parents, and up-chain steps. */
function linkedRefs(d: RouteDetail): LinkedRef[] {
  const out: LinkedRef[] = [];
  for (const p of d.parentRefs) if (p.target) out.push(p.target);
  for (const p of d.parents) out.push(p.target);
  for (const a of d.ancestors) for (const s of a.steps) if (s.ref) out.push(s.ref);
  return out;
}

/** One route of any kind: up-chain, parents, the route trace and, for an HTTPRoute, its child routes (design §8.5, Phase 6 §7). */
export function RoutePage({ stylesOverride }: RoutePageProps = {}) {
  const id = useInstallId();
  const { group = "", kind = "", ns = "", name = "" } = useParams();
  const ref = useMemo<LinkedRef>(() => ({ group, kind, namespace: ns, name, link: "detail" }), [group, kind, ns, name]);
  const { data, error } = useRouteDetail(id, ref);
  const hasDelegation = !!data?.rules.some((r) => r.backendRefs.some((b) => b.delegation));
  // Delegation copy and blocks need a rule set that computes it and an HTTPRoute; the tab is the kgateway web pack's.
  const delegation = useInstallFeature(id, "delegation") !== false && kind === "HTTPRoute";
  const unlisted = useMemo(() => (data ? unlistedRefs(data) : []), [data]);
  // The parents and up-chain this page links decide the links of the same objects in the trail.
  const trailRefs = useMemo(() => (data ? linkedRefs(data) : undefined), [data]);
  // From 1600 px the up-chain and parents sit beside the structure; below, where they always were.
  const wide = useTwoColumns();
  const upChain = data ? (
    <UpChain installId={id} self={ref} paths={data.ancestors} truncated={data.ancestorsTruncated} />
  ) : null;
  const parentsBlock = data ? (
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
  ) : null;
  return (
    <DetailFrame
      installId={id}
      refValue={ref}
      noun={kind}
      side={
        wide ? (
          <>
            {upChain}
            {parentsBlock}
          </>
        ) : null
      }
      sideWhenWide
      data={data}
      error={error}
      // Conditions live per parent; only this install's controller's entries count.
      problem={
        data && {
          condition: firstFailing(data.parents.filter((p) => p.ours).flatMap((p) => p.conditions)),
          conditionsId: "route-parents",
        }
      }
      trailRefs={trailRefs}
      stylesOverride={stylesOverride}
    >
      {data ? (
        <>
          {wide ? null : upChain}
          <Section aria-label={detailCopy.summary}>
            <Facts>
              {/* A TCPRoute has no hostnames. */}
              {kind === "TCPRoute" ? null : (
                <>
                  <dt>{routeCopy.hostnames}</dt>
                  <dd>{hostnameText(data)}</dd>
                </>
              )}
              {delegation ? (
                <>
                  <dt>{routeCopy.parentRoutes}</dt>
                  <dd>{permissionText(data)}</dd>
                </>
              ) : null}
              <dt>{trafficCopy.title}</dt>
              <dd>
                <InlineLink to={`${installPath(id, "traffic")}?${new URLSearchParams({ route: refParam(ref) })}`}>
                  {trafficCopy.routeEntry}
                </InlineLink>
              </dd>
            </Facts>
          </Section>
          {wide ? null : parentsBlock}
          {data.serves ? (
            <Section aria-labelledby="route-serves">
              <h2 id="route-serves">
                {traceCopy.serves} <span className="note">{data.serves.length}</span>
              </h2>
              <Serves installId={id} serves={data.serves} hostnames={kind !== "TCPRoute"} />
            </Section>
          ) : null}
          <Section aria-labelledby="route-rules">
            <h2 id="route-rules">
              {routeCopy.rules} <span className="note">{data.rules.length}</span>
            </h2>
            {data.rules.length ? (
              <RuleTable installId={id} kind={kind} rules={data.rules} serves={data.serves} />
            ) : (
              <RuleList installId={id} kind={kind} rules={data.rules} serves={data.serves} />
            )}
          </Section>
          {delegation && data.effective?.length ? (
            <EffectiveMatches installId={id} edges={data.effective} truncated={data.effectiveTruncated} />
          ) : null}
          {delegation ? <ChildRoutes installId={id} route={data.ref} expected={data.childCount} /> : null}
        </>
      ) : null}
    </DetailFrame>
  );
}
