import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Check, Search } from "lucide-react";
import { useDeferredValue, useMemo, useRef } from "react";
import { useHealth } from "../api/hooks";
import type { Finding, Severity } from "../api/types";
import { severityLabel } from "../health/state";
import { HEADER_H } from "../shell/constants";
import { useInstallId, useVocabulary } from "../shell/extension";
import { useUrlParams, useUrlText } from "../shell/urlParams";
import { Chip, SearchBox } from "../states/controls";
import { PageNotice } from "../states/PageNotice";
import { PageState } from "../states/PageState";
import { DataAge } from "./DataAge";
import { FindingRow } from "./install/FindingRow";
import { findingKeys } from "./install/TopFindings";

// region Styles
const Page = styled.div<{ stylesOverride?: SerializedStyles }>`
  /* A flex column of the viewport below the header: the list takes what the (possibly wrapping) bar leaves. */
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  height: calc(100dvh - ${HEADER_H}px);
  min-height: 360px;
  max-width: 1400px;
  margin: 0 auto;
  padding: ${(p) => p.theme.space(4)};
  .title {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(3)};
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
  .sep {
    width: 1px;
    height: 20px;
    background: ${(p) => p.theme.color.border};
  }
`;
// Shrinks to fit the page when long; a short list keeps its content height.
const Scroller = styled.div`
  flex: 0 1 auto;
  min-height: 0;
  overflow: auto;
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
`;
const Item = styled.div`
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  border-bottom: 1px solid ${(p) => p.theme.color.border};
  &[data-last="true"] {
    border-bottom: 0;
  }
  /* Hover only: the row itself is not clickable (its resource link is). */
  &:hover {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 4%, ${(p) => p.theme.color.surface});
  }
`;

// region Helpers
const SEVERITIES: Severity[] = ["error", "warning", "info"];

function listParam(params: URLSearchParams, key: string): string[] {
  return params.get(key)?.split(",").filter(Boolean) ?? [];
}

function toggle(list: string[], v: string): string[] {
  return list.includes(v) ? list.filter((x) => x !== v) : [...list, v];
}

/** What free text matches: code, message and the resource's name and namespace/name; absent fields add nothing. */
function haystack(f: Finding): string {
  const r = f.resource;
  const parts = [f.code, f.message];
  if (r) parts.push(r.namespace ? `${r.namespace}/${r.name}` : r.name);
  return parts.join(" ").toLowerCase();
}

// region Component
interface FindingsPageProps {
  stylesOverride?: SerializedStyles;
}

/** Every finding of the install, with URL-backed severity, kind and text filters over a virtualised list. */
export function FindingsPage({ stylesOverride }: FindingsPageProps = {}) {
  const id = useInstallId();
  const vocab = useVocabulary();
  const { data, error } = useHealth(id);
  const [params, update] = useUrlParams();
  const sev = listParam(params, "sev");
  const kinds = listParam(params, "kind");
  const [text, setText] = useUrlText(params, update, "q");
  // Typing stays responsive on long lists: filtering follows the input at lower priority.
  const q = useDeferredValue(text);
  // Never render another install's findings while this one loads.
  const current = data?.install === id ? data : undefined;
  const all = current?.findings ?? [];
  const sevKey = sev.join(",");
  const kindKey = kinds.join(",");
  const kindOptions = useMemo(() => {
    const found = new Set<string>();
    for (const f of all) if (f.resource?.kind) found.add(f.resource.kind);
    // A kind selected in the URL stays visible, so it can be cleared.
    for (const k of kindKey.split(",").filter(Boolean)) found.add(k);
    return [...found].sort();
  }, [all, kindKey]);
  const rows = useMemo(() => {
    const s = sevKey.split(",").filter(Boolean);
    const k = kindKey.split(",").filter(Boolean);
    const needle = q.trim().toLowerCase();
    return all.filter(
      (f) =>
        (s.length === 0 || s.includes(f.severity)) &&
        (k.length === 0 || (!!f.resource && k.includes(f.resource.kind))) &&
        (!needle || haystack(f).includes(needle)),
    );
  }, [all, sevKey, kindKey, q]);
  const keys = useMemo(() => findingKeys(rows), [rows]);
  const scroller = useRef<HTMLDivElement>(null);
  const v = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scroller.current,
    estimateSize: () => 64,
    overscan: 10,
    getItemKey: (i) => keys[i] ?? i,
  });
  // After a first load, a failed poll keeps the findings on screen under a notice; their age turns stale.
  if (!current) {
    if (error?.status === 503) return <PageState kind="connecting" title={vocab.connecting} message={error.message} />;
    if (error)
      return <PageState kind="error" title="Cannot load findings" message={error.message} copyText={error.message} />;
    return <PageState kind="loading" title="Loading findings…" />;
  }
  return (
    <Page stylesOverride={stylesOverride}>
      {error ? <PageNotice error={error} title="Cannot load findings" /> : null}
      <div className="title">
        <h1>Findings</h1>
        <DataAge collectedAt={current.collectedAt} />
      </div>
      <Bar>
        {SEVERITIES.map((s) => (
          <Chip
            key={s}
            type="button"
            aria-pressed={sev.includes(s)}
            onClick={() => update({ sev: toggle(sev, s).join(",") })}
          >
            {sev.includes(s) ? <Check size={14} aria-hidden="true" /> : null}
            {severityLabel[s]}
          </Chip>
        ))}
        {kindOptions.length ? <span className="sep" aria-hidden="true" /> : null}
        {kindOptions.map((k) => (
          <Chip
            key={k}
            type="button"
            aria-pressed={kinds.includes(k)}
            onClick={() => update({ kind: toggle(kinds, k).join(",") })}
          >
            {kinds.includes(k) ? <Check size={14} aria-hidden="true" /> : null}
            {k}
          </Chip>
        ))}
        <SearchBox>
          <Search size={14} aria-hidden="true" />
          <input
            aria-label="Filter findings"
            autoComplete="off"
            placeholder="Filter by name, message or code"
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
        </SearchBox>
        <span className="count">
          {rows.length} of {all.length}
        </span>
      </Bar>
      {all.length === 0 ? (
        <PageState kind="empty" title="No findings" />
      ) : rows.length === 0 ? (
        <PageState kind="empty" title="No findings match these filters" />
      ) : (
        <Scroller ref={scroller} role="region" aria-label="Findings" tabIndex={0}>
          <div style={{ height: v.getTotalSize(), position: "relative" }}>
            {v.getVirtualItems().map((it) => {
              const f = rows[it.index];
              return f ? (
                <Item
                  key={it.key}
                  ref={v.measureElement}
                  data-index={it.index}
                  data-last={it.index === rows.length - 1}
                  style={{ transform: `translateY(${it.start}px)` }}
                >
                  <FindingRow finding={f} installId={id} />
                </Item>
              ) : null;
            })}
          </div>
        </Scroller>
      )}
    </Page>
  );
}
