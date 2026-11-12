import { getBezierPath, Position } from "@xyflow/react";
import { mapCopy } from "../../copy/map";
import { cellCopy } from "../../copy/traffic";
import { stateLabel } from "../../health/state";
import type { HealthState, Theme } from "../../theme/tokens";
import { strokeWidth } from "./edges";
import { kindLabel, type MapEdge, type MapGraph } from "./graph";
import { NODE_H, NODE_W, type Point } from "./layout";

// region Helpers
const PAD = 32;
const HEAD = 44;
const LEGEND = 40;
/** The largest PNG side, in pixels; browsers refuse bigger canvases. */
const MAX_SIDE = 16384;
/** Edge patterns, as the canvas draws them (map.css.ts). */
const DASH: Partial<Record<HealthState, string>> = { warning: "2 5", unknown: "10 4 2 4" };
const BROKEN_DASH = "8 6";
const GLYPH: Record<HealthState, string> = { healthy: "✓", warning: "!", error: "✕", unknown: "?" };

/** Escapes text for SVG content and attribute values. */
export const esc = (s: string) =>
  s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c] ?? c);

/** Cuts text to max characters with an ellipsis; the full text stays in the node's title. */
const cut = (s: string, max: number) => (s.length > max ? `${s.slice(0, max - 1)}…` : s);

const px = (n: number) => String(Math.round(n * 100) / 100);

/** "2026-11-12 14:03 UTC" */
const stamp = (at: Date) => `${at.toISOString().slice(0, 16).replace("T", " ")} UTC`;

/** A small status chip: the state's glyph on its icon colour, so the state never rests on colour alone. */
function chip(t: Theme, state: HealthState, cx: number, cy: number, r: number): string {
  const s = t.color.status[state];
  return (
    `<circle cx="${px(cx)}" cy="${px(cy)}" r="${r}" fill="${s.iconBg}"/>` +
    `<text x="${px(cx)}" y="${px(cy)}" fill="${s.iconFg}" font-size="${r * 1.4}" font-weight="700" ` +
    `text-anchor="middle" dominant-baseline="central">${GLYPH[state]}</text>`
  );
}

/** An edge's stroke, pattern and mark: dashed with a filled mark when broken, a hollow ring when partly failing. */
function edgeShape(t: Theme, e: Pick<MapEdge, "state" | "broken" | "share">, d: string, mx: number, my: number) {
  const broken = e.state === "error" && e.broken;
  const dash = broken ? BROKEN_DASH : DASH[e.state];
  const line =
    `<path d="${d}" fill="none" stroke="${t.color.status[e.state].fg}" stroke-width="${px(strokeWidth(e.share))}"` +
    `${dash ? ` stroke-dasharray="${dash}"` : ""}${e.state === "warning" ? ' stroke-linecap="round"' : ""}`;
  const mark = broken
    ? chip(t, "error", mx, my, 7)
    : e.state === "error"
      ? `<circle cx="${px(mx)}" cy="${px(my)}" r="4.75" fill="${t.color.bg}" stroke="${t.color.status.error.fg}" stroke-width="2.5"/>`
      : "";
  return { line, mark };
}

/** The legend: each edge pattern beside its meaning. */
function legend(t: Theme, y: number): string {
  const items: [Pick<MapEdge, "state" | "broken">, string][] = [
    [{ state: "healthy", broken: false }, stateLabel("healthy")],
    [{ state: "warning", broken: false }, stateLabel("warning")],
    [{ state: "unknown", broken: false }, stateLabel("unknown")],
    [{ state: "error", broken: false }, mapCopy.legendPartial],
    [{ state: "error", broken: true }, mapCopy.legendBroken],
  ];
  let x = PAD;
  return items
    .map(([e, label]) => {
      const { line, mark } = edgeShape(t, e, `M ${x} ${y} L ${x + 36} ${y}`, x + 18, y);
      const out = `${line}/>${mark}<text x="${x + 44}" y="${y}" fill="${t.color.textMuted}" font-size="12" dominant-baseline="central">${esc(label)}</text>`;
      x += 60 + label.length * 7;
      return out;
    })
    .join("");
}

// region Component
export interface ExportOptions {
  /** The export time in the title; now by default. */
  at?: Date;
  /** A stale view: every state shows unknown and no edge is broken, as on the canvas. */
  stale?: boolean;
}

/**
 * The map as a standalone SVG, built from our layout and node data rather than React Flow's DOM: one group per
 * node and one path per edge of g, in theme colours, each state as text or pattern as well as colour, under a
 * title with the Gateway and time.
 */
export function exportSVG(g: MapGraph, positions: Map<string, Point>, theme: Theme, opts: ExportOptions = {}): string {
  const at = (id: string) => positions.get(id) ?? { x: 0, y: 0 };
  const pts = g.nodes.map((n) => at(n.id));
  const minX = Math.min(0, ...pts.map((p) => p.x));
  const minY = Math.min(0, ...pts.map((p) => p.y));
  const maxX = Math.max(NODE_W, ...pts.map((p) => p.x + NODE_W));
  const maxY = Math.max(NODE_H, ...pts.map((p) => p.y + NODE_H));
  const width = Math.max(maxX - minX + 2 * PAD, 720);
  const height = maxY - minY + 2 * PAD + HEAD + LEGEND;
  const dx = PAD - minX;
  const dy = HEAD + PAD - minY;
  const c = theme.color;
  const gateway = g.nodes.find((n) => n.kind === "gateway")?.name ?? "";
  const title = mapCopy.exportTitle(gateway, stamp(opts.at ?? new Date()));

  const lines: string[] = [];
  const marks: string[] = [];
  for (const e of g.edges) {
    const s = at(e.source);
    const tg = at(e.target);
    const [d, mx, my] = getBezierPath({
      sourceX: s.x + dx + NODE_W,
      sourceY: s.y + dy + NODE_H / 2,
      targetX: tg.x + dx,
      targetY: tg.y + dy + NODE_H / 2,
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
    });
    const shown = opts.stale ? { ...e, state: "unknown" as const, broken: false } : e;
    const { line, mark } = edgeShape(theme, shown, d, mx, my);
    lines.push(
      `${line} data-edge="${esc(e.id)}" data-state="${shown.state}"${shown.broken ? ' data-broken=""' : ""}/>`,
    );
    if (mark) marks.push(mark);
  }

  const nodes = g.nodes.map((n) => {
    const p = at(n.id);
    const state = opts.stale ? "unknown" : n.state;
    const kind = `${kindLabel(n)}${n.delegatedUnder ? ` · ${cellCopy.delegated}` : ""}${n.hiddenRoutes ? ` · ${mapCopy.hiddenDelegated(n.hiddenRoutes)}` : ""}`;
    const label = stateLabel(state);
    return (
      `<g data-node="${esc(n.id)}" data-state="${state}" transform="translate(${px(p.x + dx)} ${px(p.y + dy)})">` +
      `<title>${esc(mapCopy.nodeLabel(kindLabel(n), n.name, label))}</title>` +
      `<rect width="${NODE_W}" height="${NODE_H}" rx="8" fill="${c.surface}" stroke="${c.borderStrong}"/>` +
      `<text x="12" y="20" fill="${c.textMuted}" font-size="10">${esc(cut(kind, 36))}</text>` +
      `<text x="12" y="40" fill="${c.text}" font-family="${esc(theme.font.mono)}" font-size="12.5">${esc(cut(n.name, 26))}</text>` +
      chip(theme, state, 19, 56, 7) +
      `<text x="32" y="56" fill="${c.status[state].fg}" font-size="12" dominant-baseline="central">${esc(label)}</text>` +
      "</g>"
    );
  });

  return (
    `<svg xmlns="http://www.w3.org/2000/svg" width="${px(width)}" height="${px(height)}" viewBox="0 0 ${px(width)} ${px(height)}" ` +
    `font-family="${esc(theme.font.ui)}">` +
    `<title>${esc(title)}</title>` +
    `<rect width="100%" height="100%" fill="${c.bg}"/>` +
    `<text x="${PAD}" y="${PAD}" fill="${c.text}" font-size="14" font-weight="600">${esc(title)}</text>` +
    `<g>${lines.join("")}</g><g>${marks.join("")}</g><g>${nodes.join("")}</g>` +
    `<g>${legend(theme, height - LEGEND / 2 - 4)}</g>` +
    "</svg>"
  );
}

/** Rasterises an SVG from exportSVG to a PNG at scale, smaller when the image would pass the browser's canvas limit. */
export async function exportPNG(svg: string, scale: number): Promise<Blob> {
  const m = /<svg[^>]*\swidth="([\d.]+)"[^>]*\sheight="([\d.]+)"/.exec(svg);
  const w = Number(m?.[1] ?? 0);
  const h = Number(m?.[2] ?? 0);
  if (!w || !h) throw new Error("exportPNG: the SVG has no size");
  const s = Math.min(scale, MAX_SIDE / Math.max(w, h));
  const url = URL.createObjectURL(new Blob([svg], { type: "image/svg+xml" }));
  try {
    const img = new Image();
    img.src = url;
    await img.decode();
    const canvas = document.createElement("canvas");
    canvas.width = Math.round(w * s);
    canvas.height = Math.round(h * s);
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("exportPNG: no 2D canvas");
    ctx.drawImage(img, 0, 0, canvas.width, canvas.height);
    return await new Promise((resolve, reject) =>
      canvas.toBlob((b) => (b ? resolve(b) : reject(new Error("exportPNG: the canvas made no image"))), "image/png"),
    );
  } finally {
    URL.revokeObjectURL(url);
  }
}

/** The download name: gateway-map-<namespace>-<name>-<date>.<ext>. */
export const exportFileName = (gateway: string, ext: "svg" | "png", at = new Date()) =>
  `gateway-map-${gateway.replace(/[^A-Za-z0-9._-]+/g, "-").replace(/^-+|-+$/g, "")}-${at.toISOString().slice(0, 10)}.${ext}`;

/** Saves blob under name through a temporary link. */
export function download(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.append(a);
  a.click();
  a.remove();
  // The browser reads the URL when the click is handled; revoke after that task.
  setTimeout(() => URL.revokeObjectURL(url));
}
