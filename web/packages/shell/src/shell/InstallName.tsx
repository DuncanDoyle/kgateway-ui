import type { SerializedStyles } from "@emotion/react";
import type { InstallHeader } from "../api/types";
import { ResourceName } from "../resource/ResourceName";

// region Helpers
type Named = Pick<InstallHeader, "id" | "displayName" | "namespace" | "name">;

/** An install's name as plain text: its display name, else its ID. */
export function installLabel(i: Named): string {
  return i.displayName || i.id;
}

// region Component
interface InstallNameProps {
  install: Named;
  stylesOverride?: SerializedStyles;
}

/** An install's name: namespace/name in the resource style where the platform has both, else its display name (D-016). */
export function InstallName({ install: i, stylesOverride }: InstallNameProps) {
  if (i.namespace && i.name)
    return <ResourceName namespace={i.namespace} name={i.name} stylesOverride={stylesOverride} />;
  return <ResourceName name={installLabel(i)} stylesOverride={stylesOverride} />;
}
