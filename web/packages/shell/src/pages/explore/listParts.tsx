import { css } from "@emotion/react";
import styled from "@emotion/styled";
import { useNow } from "../../api/clock";
import { isStale } from "../../api/hooks";
import type { Attachment, LinkedRef, Page } from "../../api/types";
import { attachmentCopy, listCopy, tagCopy } from "../../copy/explorer";
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
/** The Tooltip wrapper around a truncated name: it fills the cell so the name can shrink. */
export const nameTip = css`
  display: flex;
  max-width: 100%;
  min-width: 0;
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

/** A table's name cell: one truncated line, the full name in a keyboard-reachable tooltip (design §8.7). */
export function NameCell({ r, installId }: { r: LinkedRef; installId: string }) {
  return (
    <Tooltip label={r.namespace ? `${r.namespace}/${r.name}` : r.name} stylesOverride={nameTip}>
      <LinkedName refValue={r} installId={installId} truncate />
    </Tooltip>
  );
}

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
