import type { RouteDetail } from "../api/types";
import { delegationCopy } from "../copy/explorer";
import type { DetailTabContribution } from "../shell/contributions";
import type { ShellExtension } from "../shell/extension";
import { DelegationContribution, hasDelegationRefs } from "./DelegationContribution";

// region Helpers
/** How the kgateway pack finds an install, for the empty state. */
const noInstallsHelp = (
  <>
    An install is a controller Deployment with the labels <code>kgateway=kgateway</code> and{" "}
    <code>app.kubernetes.io/component=controller</code>.
  </>
);

/** View B's tab on a parent route, for installs whose rule set computes delegation. */
const delegationTab: DetailTabContribution = {
  id: "delegation",
  label: delegationCopy.tab,
  feature: "delegation",
  applies: (ctx) => hasDelegationRefs(ctx.detail as RouteDetail),
  render: (ctx) => <DelegationContribution ctx={ctx} />,
};

// region Component
/** The kgateway pack's web half (Phase 5 design §10): its copy and contributions, merged into the default app. */
export const kgatewayWebPack: ShellExtension = {
  vocabulary: { noInstallsHelp },
  contributions: { detailTabs: { "gateway.networking.k8s.io/HTTPRoute": [delegationTab] } },
};
