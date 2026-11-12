import { css, type SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Link } from "react-router-dom";
import type { LinkedRef } from "../api/types";
import { ResourceName } from "../resource/ResourceName";
import { useLinkVia } from "../shell/trail";
import { linkTarget, withVia } from "./objectPath";

// region Styles
const oneLine = css`
  display: block;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
`;
const NameLink = styled(Link, { shouldForwardProp: (p) => p !== "$oneLine" })<{ $oneLine?: boolean }>`
  ${(p) => (p.$oneLine ? oneLine : null)}
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
  /** One line with an ellipsis; text names become focusable so a Tooltip around them is keyboard-reachable. */
  truncate?: boolean;
  /** Opens here instead of where the ref's link says, e.g. a backend's detail page. */
  to?: string;
  /** Set by a wrapping Tooltip. */
  "aria-describedby"?: string;
  stylesOverride?: SerializedStyles;
}

/** A resource name that opens where its server-computed link says, with the trail; a "none" link renders as text. */
export function LinkedName({
  refValue,
  installId,
  truncate,
  to: own,
  "aria-describedby": describedBy,
  stylesOverride,
}: LinkedNameProps) {
  const via = useLinkVia(installId);
  const to = own ? withVia(own, via) : linkTarget(installId, refValue, via);
  const style = truncate ? css`${oneLine}${stylesOverride}` : stylesOverride;
  const name = (
    <ResourceName
      namespace={refValue.namespace}
      name={refValue.name}
      stylesOverride={style}
      aria-describedby={to ? undefined : describedBy}
      tabIndex={truncate && !to ? 0 : undefined}
    />
  );
  return to ? (
    <NameLink to={to} aria-describedby={describedBy} $oneLine={truncate}>
      {name}
    </NameLink>
  ) : (
    name
  );
}
