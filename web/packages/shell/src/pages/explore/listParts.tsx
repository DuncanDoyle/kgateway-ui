import { css } from "@emotion/react";
import styled from "@emotion/styled";
import type { ReactNode } from "react";
import { useNow } from "../../api/clock";
import { isStale } from "../../api/hooks";
import type { Attachment, LinkedRef, Page } from "../../api/types";
import { attachmentCopy, listCopy, tagCopy, viewACopy } from "../../copy/explorer";
import { ComputedChip } from "../../explore/ComputedBadge";
import { LinkedName } from "../../explore/LinkedName";
import { listValues } from "../../explore/listQuery";
import { type FilterOption, FilterTag, StateFilter } from "../../explore/StateFilter";
import { HealthIcon } from "../../health/HealthIcon";
import { stateLabel } from "../../health/state";
import { Tooltip } from "../../shell/Tooltip";
import type { ParamChanges } from "../../shell/urlParams";
import type { HealthState } from "../../theme/tokens";

// region Styles
/** Long names wrap inside their fixed-width column. */
export const wrapName = css`
  white-space: normal;
  overflow-wrap: anywhere;
`;
/** The Tooltip wrapper around a truncated cell: it fills the cell so the content can shrink. */
export const nameTip = css`
  display: flex;
  max-width: 100%;
  min-width: 0;
`;
/** A cell's content on one line, cut with an ellipsis; focusable so its Tooltip is keyboard-reachable. */
const Line = styled.span`
  display: block;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  border-radius: ${(p) => p.theme.radius.sm};
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
`;
const RoutesLine = styled.span`
  display: inline-flex;
  align-items: center;
  gap: ${(p) => p.theme.space(2)};
  white-space: nowrap;
  .disagrees {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: ${(p) => p.theme.font.size.sm};
    color: ${(p) => p.theme.color.status.warning.fg};
  }
`;
export const Muted = styled.span`
  color: ${(p) => p.theme.color.textMuted};
`;

// region Helpers
const HEALTH: HealthState[] = ["error", "warning", "unknown", "healthy"];
const healthOptions: FilterOption[] = HEALTH.map((h) => ({
  value: h,
  label: stateLabel(h),
  icon: <HealthIcon state={h} />,
}));

/** How a parent took the route, in words; the raw condition reason is never shown (design §8.8 owns the copy). */
export const attachmentLabel: Record<Attachment, string> = attachmentCopy;

/** Labels of the filters a link can set but the bar has no control for. */
const TAG_LABELS: Record<string, string> = tagCopy;

/** A tag's value in words: refs as "Kind ns/name", attachments by their label; anything else as written. */
export function tagValue(key: string, v: string): string {
  if (key === "attachment") return attachmentLabel[v as Attachment] ?? v;
  if (key === "parent" || key === "backend") {
    const parts = v.split("/");
    if (parts.length === 4 && parts.every(Boolean)) return `${parts[1]} ${parts[2]}/${parts[3]}`;
  }
  return v;
}

/** The page with every health shown unknown when its data is stale or its last poll failed (design §8.6, as the install page). */
export function useShownPage<T extends { health: HealthState }>(
  page: Page<T> | undefined,
  failed: boolean,
): Page<T> | undefined {
  const now = useNow();
  if (!page || (!failed && !isStale(page.collectedAt, now))) return page;
  return { ...page, items: page.items.map((r) => ({ ...r, health: "unknown" as const })) };
}

/** Table cells hold one line: content is cut with an ellipsis and the full text is in a Tooltip (design §8.7). */
export function OneLine({ text, children, placement }: { text: string; children?: ReactNode; placement?: "end" }) {
  return (
    <Tooltip label={text} wrap placement={placement} stylesOverride={nameTip}>
      <Line tabIndex={0}>{children ?? text}</Line>
    </Tooltip>
  );
}

/** A name cell: OneLine, with the name's own link as the focusable element. */
export function NameCell({ r, installId }: { r: LinkedRef; installId: string }) {
  return (
    <Tooltip label={refText(r)} stylesOverride={nameTip}>
      <LinkedName refValue={r} installId={installId} truncate />
    </Tooltip>
  );
}

/** namespace/name, or name for a cluster-scoped object. */
export const refText = (r: { namespace?: string; name: string }) => (r.namespace ? `${r.namespace}/${r.name}` : r.name);

// region Component
interface FiltersProps {
  params: URLSearchParams;
  setFilter: (c: ParamChanges) => void;
  /** URL params shown as removable tags, in order. */
  tags: string[];
  children?: React.ReactNode;
}

/** Health chips, any list-specific chips, and tags for filters set through the URL. */
export function ListFilters({ params, setFilter, tags, children }: FiltersProps) {
  return (
    <>
      <StateFilter
        label={listCopy.health}
        options={healthOptions}
        selected={listValues(params, "health")}
        onChange={(v) => setFilter({ health: v.join(",") })}
      />
      {children}
      {tags.flatMap((key) =>
        listValues(params, key).map((v) => {
          const rest = listValues(params, key).filter((x) => x !== v);
          return (
            <FilterTag
              key={`${key}=${v}`}
              text={`${TAG_LABELS[key]}: ${tagValue(key, v)}`}
              onRemove={() => setFilter({ [key]: rest.join(",") })}
            />
          );
        }),
      )}
    </>
  );
}

/** Marks a list's view A routes column as computed (design §8.4): the chip sits in the header, not in every cell. */
export const routesHeaderExtra = <ComputedChip label={viewACopy.routesTooltip} />;

/** View A's distinct-route count of a Gateway or ListenerSet row; an omitted count is never shown as a number. */
export function RoutesCell({ routes, disagreements }: { routes?: number; disagreements?: number }) {
  return (
    <RoutesLine data-testid="routes-cell">
      {routes ?? <Muted>{viewACopy.noCount}</Muted>}
      {disagreements ? (
        <span className="disagrees">
          <HealthIcon state="warning" />
          {viewACopy.disagreements(disagreements)}
        </span>
      ) : null}
    </RoutesLine>
  );
}
