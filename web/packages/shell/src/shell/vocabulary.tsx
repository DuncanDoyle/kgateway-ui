import type { ReactNode } from "react";

// region Helpers
/** Platform copy (D-016). The defaults describe Kubernetes; a downstream overrides any entry through ShellExtension.vocabulary. */
export interface Vocabulary {
  /** Title while the backend has not read its data source yet. */
  connecting: string;
  /** Heading of the page that lists every install. */
  landingTitle: string;
  /** Title of the empty state when no install is found. */
  noInstallsTitle: string;
  /** How the UI finds an install, in that empty state. */
  noInstallsHelp: ReactNode;
  /** The --install hint in that empty state; pinned is the pinned install ID when set. */
  pinHint: (pinned?: string) => ReactNode;
  /** Label of the button that copies a resource's name. */
  copyName: string;
}

/** The Kubernetes vocabulary, the default. */
export const kubernetesVocabulary: Vocabulary = {
  connecting: "Waiting for the cluster",
  landingTitle: "Installs in this cluster",
  noInstallsTitle: "No kgateway install found",
  noInstallsHelp: (
    <>
      An install is a controller Deployment with the labels <code>kgateway=kgateway</code> and{" "}
      <code>app.kubernetes.io/component=controller</code>.
    </>
  ),
  pinHint: (pinned) =>
    pinned ? (
      <>
        <code>--install {pinned}</code> was set, but that install was not found.
      </>
    ) : (
      <>
        To always open one install, start the UI with <code>--install &lt;id&gt;</code> or{" "}
        <code>--install &lt;namespace&gt;/&lt;name&gt;</code>.
      </>
    ),
  copyName: "Copy namespace/name",
};
