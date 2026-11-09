import { useMemo } from "react";
import { useInstallFeature } from "../api/hooks";
import type { LinkedRef, RouteDetail } from "../api/types";
import { DelegationTab } from "../pages/explore/route/DelegationTab";
import type { DetailContext } from "../shell/contributions";
import { Shimmer } from "../states/Shimmer";

// region Helpers
/** Whether a route delegates: one of its backendRefs names a route (view B, design §8.5). */
export const hasDelegationRefs = (d: RouteDetail) => d.rules.some((r) => r.backendRefs.some((b) => b.delegation));

// region Component
/** The Delegation tab's body: view B once the server is known to serve it and the route has loaded. */
export function DelegationContribution({ ctx }: { ctx: DetailContext }) {
  const served = useInstallFeature(ctx.installId, "delegation");
  const { group, kind, namespace, name } = ctx.ref;
  const route = useMemo<LinkedRef>(
    () => ({ group, kind, namespace, name, link: "detail" }),
    [group, kind, namespace, name],
  );
  const rules = (ctx.detail as RouteDetail | undefined)?.rules;
  return served && rules ? (
    <DelegationTab installId={ctx.installId} route={route} rules={rules} />
  ) : (
    <Shimmer h={120} />
  );
}
