import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Search } from "lucide-react";
import type { ReactNode } from "react";
import type { ApiError } from "../api/client";
import { DataAge } from "../pages/DataAge";
import { useVocabulary } from "../shell/extension";
import { installPath } from "../shell/paths";
import { BackLink, QuietButton, SearchBox } from "../states/controls";
import { PageNotice } from "../states/PageNotice";
import { PageState } from "../states/PageState";
import { count } from "./ObjectTable";

// region Styles
const Page = styled.div<{ stylesOverride?: SerializedStyles }>`
  max-width: 1400px;
  margin: 0 auto;
  padding: ${(p) => p.theme.space(4)};
  .title {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(3)};
    /* Fixed, so the count and data age arriving later move nothing. */
    min-height: 28px;
    margin: 0 0 ${(p) => p.theme.space(3)};
  }
  h1 {
    margin: 0;
    font-size: ${(p) => p.theme.font.size.lg};
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  .count {
    margin-left: auto;
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
    white-space: nowrap;
  }
  ${(p) => p.stylesOverride}
`;
const Bar = styled.div`
  display: flex;
  flex-wrap: wrap;
  gap: ${(p) => p.theme.space(2)};
  align-items: center;
  margin-bottom: ${(p) => p.theme.space(3)};
`;

// region Component
interface ListFrameProps {
  title: string;
  installId: string;
  noun: readonly [string, string];
  collectedAt?: string;
  total?: number;
  error?: ApiError;
  /** Whether data has loaded; without it an error replaces the page. */
  loaded: boolean;
  search: { label: string; placeholder: string; value: string; onChange: (v: string) => void };
  /** Filter chips and tags, rendered from the URL, so they are there before the data. */
  filters?: ReactNode;
  onClearFilters?: () => void;
  children: ReactNode;
  stylesOverride?: SerializedStyles;
}

/** The chrome of a list page: title, count, data age, filter bar and the states a list page can be in (design §8.6). */
export function ListFrame({
  title,
  installId,
  noun,
  collectedAt,
  total,
  error,
  loaded,
  search,
  filters,
  onClearFilters,
  children,
  stylesOverride,
}: ListFrameProps) {
  const vocab = useVocabulary();
  const errorTitle = `Cannot load ${noun[1]}`;
  // A list 404s only for an unknown install (design §7.2).
  if (error?.status === 404)
    return (
      <PageState
        kind="empty"
        title="Not in this install's scope"
        message={<BackLink to={installPath(installId)}>Back to install</BackLink>}
      />
    );
  // A query that failed before loading keeps its title and filters, so its filters can still be changed or cleared.
  const failed = !loaded && error;
  return (
    <Page stylesOverride={stylesOverride}>
      {error && !failed ? <PageNotice error={error} title={errorTitle} /> : null}
      <div className="title">
        <h1>{title}</h1>
        {collectedAt && !failed ? <DataAge collectedAt={collectedAt} /> : null}
        {total !== undefined && !failed ? <span className="count">{count(total, noun)}</span> : null}
      </div>
      <Bar>
        <SearchBox>
          <Search size={14} aria-hidden="true" />
          <input
            type="search"
            aria-label={search.label}
            autoComplete="off"
            placeholder={search.placeholder}
            value={search.value}
            onChange={(e) => search.onChange(e.target.value)}
          />
        </SearchBox>
        {filters}
        {onClearFilters ? (
          <QuietButton type="button" onClick={onClearFilters}>
            Clear filters
          </QuietButton>
        ) : null}
      </Bar>
      {!failed ? (
        children
      ) : error.status === 503 ? (
        <PageState kind="connecting" title={vocab.connecting} message={error.message} />
      ) : (
        <PageState kind="error" title={errorTitle} message={error.message} copyText={error.message} />
      )}
    </Page>
  );
}
