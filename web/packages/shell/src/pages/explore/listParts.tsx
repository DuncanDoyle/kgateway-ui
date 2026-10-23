import { css } from "@emotion/react";
import styled from "@emotion/styled";
import { useNow } from "../../api/clock";
import { isStale } from "../../api/hooks";
import type { Attachment, LinkedRef, Page } from "../../api/types";
import { LinkedName } from "../../explore/LinkedName";
import { listValues } from "../../explore/listQuery";
import { type FilterOption, FilterTag, StateFilter } from "../../explore/StateFilter";
import { HealthIcon } from "../../health/HealthIcon";
import { stateLabel } from "../../health/state";
import type { ParamChanges } from "../../shell/urlParams";
import type { HealthState } from "../../theme/tokens";

// region Styles
/** Long names wrap inside their fixed-width column. */
export const wrapName = css`
  white-space: normal;
  overflow-wrap: anywhere;
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
export const attachmentLabel: Record<Attachment, string> = {
  accepted: "Accepted",
  "not-accepted": "Not accepted",
  pending: "Waiting for status",
  unwatched: "Namespace not watched",
  declared: "Parent route has no status",
};

/** Labels of the filters a link can set but the bar has no control for. */
const TAG_LABELS: Record<string, string> = {
  namespace: "Namespace",
  kind: "Kind",
  parent: "Parent",
  attachment: "Attachment",
  backend: "Backend",
};

/** The page with every health shown unknown when its data is stale or its last poll failed (design §8.6, as the install page). */
export function useShownPage<T extends { health: HealthState }>(
  page: Page<T> | undefined,
  failed: boolean,
): Page<T> | undefined {
  const now = useNow();
  if (!page || (!failed && !isStale(page.collectedAt, now))) return page;
  return { ...page, items: page.items.map((r) => ({ ...r, health: "unknown" as const })) };
}

export function NameCell({ r, installId }: { r: LinkedRef; installId: string }) {
  return <LinkedName refValue={r} installId={installId} stylesOverride={wrapName} />;
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
        label="Health"
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
              text={`${TAG_LABELS[key]}: ${v}`}
              onRemove={() => setFilter({ [key]: rest.join(",") })}
            />
          );
        }),
      )}
    </>
  );
}
