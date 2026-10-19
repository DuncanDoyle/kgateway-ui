import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Check, Search } from "lucide-react";
import { useMemo, useRef } from "react";
import { useSearchParams } from "react-router-dom";
import { useHealth } from "../api/hooks";
import type { Finding, Severity } from "../api/types";
import { severityLabel } from "../health/state";
import { useInstallId } from "../shell/extension";
import { PageState } from "../states/PageState";
import { FindingRow } from "./install/FindingRow";
import { findingKeys } from "./install/TopFindings";

// region Styles
const Page = styled.div<{ stylesOverride?: SerializedStyles }>`
  max-width: 1400px;
  margin: 0 auto;
  padding: ${(p) => p.theme.space(4)};
  h1 {
    margin: 0 0 ${(p) => p.theme.space(3)};
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
const Chip = styled.button`
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 12px;
  cursor: pointer;
  font: inherit;
  font-size: ${(p) => p.theme.font.size.sm};
  border-radius: 16px;
  color: ${(p) => p.theme.color.text};
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  &:hover {
    border-color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.surface});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
  /* Selected: check icon and bold text, so the state never relies on colour. */
  &[aria-pressed="true"] {
    border-color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 10%, ${(p) => p.theme.color.surface});
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  &[aria-pressed="true"]:hover {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 12%, ${(p) => p.theme.color.surface});
  }
  &[aria-pressed="true"]:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
`;
const SearchBox = styled.label`
  position: relative;
  display: inline-flex;
  align-items: center;
  svg {
    position: absolute;
    left: 10px;
    color: ${(p) => p.theme.color.textMuted};
    pointer-events: none;
  }
  input {
    box-sizing: border-box;
    height: 32px;
    width: 280px;
    max-width: 100%;
    padding: 0 10px 0 30px;
    font: inherit;
    font-size: ${(p) => p.theme.font.size.sm};
    color: ${(p) => p.theme.color.text};
    background: ${(p) => p.theme.color.surface};
    border: 1px solid ${(p) => p.theme.color.borderStrong};
    border-radius: ${(p) => p.theme.radius.sm};
  }
  input::placeholder {
    color: ${(p) => p.theme.color.textMuted};
  }
  input:hover {
    border-color: ${(p) => p.theme.color.text};
  }
  input:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 1px;
  }
`;
// max-height, so a short list fits its content and a long one scrolls inside the page.
const Scroller = styled.div`
  max-height: calc(100vh - 220px);
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
  &:hover {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 4%, ${(p) => p.theme.color.surface});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 8%, ${(p) => p.theme.color.surface});
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
  const { data, error } = useHealth(id);
  const [params, setParams] = useSearchParams();
  const sev = listParam(params, "sev");
  const kinds = listParam(params, "kind");
  const q = params.get("q") ?? "";
  const set = (key: string, value: string) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value) next.set(key, value);
        else next.delete(key);
        return next;
      },
      { replace: true },
    );
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
  if (error?.status === 503)
    return <PageState kind="connecting" title="Connecting to cluster…" message={error.message} />;
  if (error)
    return <PageState kind="error" title="Cannot load findings" message={error.message} copyText={error.message} />;
  if (!current) return <PageState kind="loading" title="Loading findings…" />;
  return (
    <Page stylesOverride={stylesOverride}>
      <h1>Findings</h1>
      <Bar>
        {SEVERITIES.map((s) => (
          <Chip
            key={s}
            type="button"
            aria-pressed={sev.includes(s)}
            onClick={() => set("sev", toggle(sev, s).join(","))}
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
            onClick={() => set("kind", toggle(kinds, k).join(","))}
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
            value={q}
            onChange={(e) => set("q", e.target.value)}
          />
        </SearchBox>
        <span className="count">
          {rows.length} of {all.length}
        </span>
      </Bar>
      {rows.length === 0 ? (
        <PageState kind="empty" title="No findings match these filters" />
      ) : (
        <Scroller ref={scroller} role="region" aria-label="Findings">
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
