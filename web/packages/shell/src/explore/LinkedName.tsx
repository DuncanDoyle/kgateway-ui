import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Link } from "react-router-dom";
import type { LinkedRef } from "../api/types";
import { ResourceName } from "../resource/ResourceName";
import { objectPath } from "./objectPath";

// region Styles
const NameLink = styled(Link)`
  color: inherit;
  text-decoration: none;
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover [data-part="name"] {
    text-decoration: underline;
  }
  &:active [data-part="name"] {
    color: ${(p) => p.theme.color.accentText};
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
`;

// region Component
interface LinkedNameProps {
  refValue: LinkedRef;
  installId: string;
  stylesOverride?: SerializedStyles;
}

/** A resource name that opens where its server-computed link says; a "none" link renders as text. */
export function LinkedName({ refValue, installId, stylesOverride }: LinkedNameProps) {
  const to = objectPath(installId, refValue);
  const name = <ResourceName namespace={refValue.namespace} name={refValue.name} stylesOverride={stylesOverride} />;
  return to ? <NameLink to={to}>{name}</NameLink> : name;
}
