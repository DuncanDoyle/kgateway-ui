import { GitFork } from "lucide-react";
import type { RouteDetail } from "../api/types";
import { delegationCopy } from "../copy/explorer";
import type { DetailTabContribution } from "../shell/contributions";
import type { WebPack } from "../shell/extension";
import type { BackendKind } from "../shell/vocabulary";
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
  icon: GitFork,
  feature: "delegation",
  applies: (ctx) => hasDelegationRefs(ctx.detail as RouteDetail),
  render: (ctx) => <DelegationContribution ctx={ctx} />,
};

/** kgateway's Backend resource: its Backends list chip and the label of its spec.type (Phase 6 design §8.1). */
const backendKind: BackendKind = {
  group: "gateway.kgateway.dev",
  kind: "Backend",
  chip: "Backend resources", // S744
  typeLabel: "Backend type", // S745
};

// region Component
/** The kgateway pack's web half (Phase 5 design §10): its copy and contributions, on where /meta lists "kgateway". */
export const kgatewayWebPack: WebPack = {
  name: "kgateway",
  vocabulary: { noInstallsTitle: "No kgateway install found", noInstallsHelp, backendKinds: [backendKind] },
  contributions: { detailTabs: { "gateway.networking.k8s.io/HTTPRoute": [delegationTab] } },
};
