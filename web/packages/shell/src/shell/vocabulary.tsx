import type { ReactNode } from "react";
import type { Ref } from "../api/types";
import { navCopy } from "../copy/explorer";

// region Helpers
/** An implementation's own backend kind: a Backends list chip and the label of its spec.type on backend detail. */
export interface BackendKind {
  group: string;
  kind: string;
  /** The chip that lists only this kind, e.g. "Backend resources". */
  chip: string;
  /** The label of the kind's type on its detail page. */
  typeLabel: string;
}

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
  /** The command that prints ref; resource is its plural from /api/v1/meta. Return undefined to hide the copy button. */
  getCommand: (ref: Ref, resource: string) => string | undefined;
  /** Label of the button that copies that command. */
  copyCommand: string;
  /** Backend kinds the implementation adds; the core lists Services only. */
  backendKinds: BackendKind[];
}

/** The Kubernetes vocabulary, the default. */
export const kubernetesVocabulary: Vocabulary = {
  connecting: "Waiting for the cluster",
  landingTitle: "Installs in this cluster",
  noInstallsTitle: "No Gateway API controller found",
  noInstallsHelp: "An install is a Gateway API controller found in the cluster.",
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
  getCommand: (ref, resource) => {
    // The core group has no ".<group>" suffix; a cluster-scoped resource has no namespace flag.
    const type = ref.group ? `${resource}.${ref.group}` : resource;
    const ns = ref.namespace ? ` -n ${ref.namespace}` : "";
    return `kubectl get ${type}${ns} ${ref.name} -o yaml`;
  },
  copyCommand: navCopy.copyCommand,
  backendKinds: [],
};
