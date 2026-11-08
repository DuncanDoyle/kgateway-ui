import type { ShellExtension } from "../shell/extension";

// region Helpers
/** How the kgateway pack finds an install, for the empty state. */
const noInstallsHelp = (
  <>
    An install is a controller Deployment with the labels <code>kgateway=kgateway</code> and{" "}
    <code>app.kubernetes.io/component=controller</code>.
  </>
);

// region Component
/** The kgateway pack's web half (Phase 5 design §10): its copy and contributions, merged into the default app. */
export const kgatewayWebPack: ShellExtension = {
  vocabulary: { noInstallsHelp },
};
