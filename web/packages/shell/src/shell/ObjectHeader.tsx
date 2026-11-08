import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Box, Layers, Network, Route, Server } from "lucide-react";
import type { ReactNode } from "react";
import { useKindCategory } from "../api/hooks";
import type { Category, LinkedRef } from "../api/types";
import { objectHeaderCopy } from "../copy/navigation";
import { HealthBadge } from "../health/HealthBadge";
import { DataAge } from "../pages/DataAge";
import { VisuallyHidden } from "../states/VisuallyHidden";
import type { HealthState } from "../theme/tokens";
import { Breadcrumbs, type CurrentState } from "./Breadcrumbs";

// region Styles
/** Height of the object header, fixed from the first render: state and age fill reserved space. */
export const OBJECT_HEADER_H = 64;

const Header = styled.header<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  grid-template-rows: 20px minmax(0, 1fr);
  column-gap: ${(p) => p.theme.space(4)};
  align-items: center;
  height: ${OBJECT_HEADER_H}px;
  min-height: ${OBJECT_HEADER_H}px;
  margin-bottom: ${(p) => p.theme.space(2)};
  .kind {
    display: inline-flex;
    align-items: center;
    gap: ${(p) => p.theme.space(1.5)};
    min-width: 0;
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
    white-space: nowrap;
  }
  .title {
    display: flex;
    align-items: baseline;
    gap: ${(p) => p.theme.space(2)};
    min-width: 0;
  }
  h1 {
    position: relative;
    flex: 0 1 auto;
    min-width: 0;
    margin: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.xl};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    line-height: 1.3;
  }
  .ns {
    flex: 0 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
  }
  .ns code {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
  }
  .actions {
    display: inline-flex;
    flex: none;
    align-items: center;
    gap: ${(p) => p.theme.space(1)};
    align-self: center;
  }
  .state,
  .age {
    justify-self: end;
    display: inline-flex;
    align-items: center;
    min-height: 20px;
  }
  ${(p) => p.stylesOverride}
`;

// region Helpers
const GW = "gateway.networking.k8s.io";

/** The default icon for a kind, a backend by its /meta category; a downstream kind passes its own. */
function defaultIcon({ group, kind }: { group: string; kind: string }, category?: Category): ReactNode {
  const Icon =
    group === GW && kind === "Gateway"
      ? Network
      : group === GW && kind.endsWith("Route")
        ? Route
        : kind.endsWith("ListenerSet")
          ? Layers
          : category === "backend"
            ? Server
            : Box;
  return <Icon size={14} aria-hidden="true" />;
}

// region Component
/** Props of {@link ObjectHeader}. */
export interface ObjectHeaderProps {
  /** The object's kind: label as shown, and an icon (a default by kind otherwise). */
  kind: { group: string; kind: string; label: string; icon?: ReactNode };
  name: string;
  /** Absent for a cluster-scoped object. */
  namespace?: string;
  /** Absent while loading, or for an object without a health state. */
  state?: HealthState;
  /** When the shown data was collected; absent while loading. */
  collectedAt?: string;
  /** Buttons and links for the object, e.g. copy buttons. */
  actions?: ReactNode;
  /** Refs the page's data holds, which decide the trail's links (see {@link Breadcrumbs}). */
  trailRefs?: readonly LinkedRef[];
  /** Whether the page's detail has confirmed the object, for the trail's current crumb. */
  current?: CurrentState;
  stylesOverride?: SerializedStyles;
}

/**
 * The identity of a detail page: kind, name in the page's largest type, namespace, state and data age, with the
 * trail above it on an install's object pages (design §17.6).
 */
export function ObjectHeader({
  kind,
  name,
  namespace,
  state,
  collectedAt,
  actions,
  trailRefs,
  current,
  stylesOverride,
}: ObjectHeaderProps) {
  const category = useKindCategory(kind);
  return (
    <>
      <Breadcrumbs refs={trailRefs} current={current} />
      <Header data-testid="detail-header" data-object-header="" stylesOverride={stylesOverride}>
        {/* The heading reads "Kind namespace/name"; the visible kind and namespace repeat it, so they are hidden from readers. */}
        <span className="kind" aria-hidden="true">
          {kind.icon ?? defaultIcon(kind, category)}
          {kind.label}
        </span>
        <span className="state">{state ? <HealthBadge state={state} /> : null}</span>
        <div className="title">
          <h1 title={namespace ? `${namespace}/${name}` : name}>
            <VisuallyHidden>{namespace ? `${kind.label} ${namespace}/` : `${kind.label} `}</VisuallyHidden>
            {name}
          </h1>
          {namespace ? (
            <span className="ns" aria-hidden="true">
              {objectHeaderCopy.namespace} <code>{namespace}</code>
            </span>
          ) : null}
          {actions ? <span className="actions">{actions}</span> : null}
        </div>
        <span className="age">{collectedAt ? <DataAge collectedAt={collectedAt} /> : null}</span>
      </Header>
    </>
  );
}
