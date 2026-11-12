import pathsGolden from "../../../../../../pkg/api/testdata/golden/paths.json";
import type { PathRow, PathsResponse } from "../../api/types";
import { mapCopy } from "../../copy/map";
import { controlRoomDark, controlRoomLight } from "../../theme/themes";
import { esc, exportFileName, exportSVG } from "./export";
import { gatewayGraph, type MapGraph } from "./graph";
import { layoutColumns } from "./layout";

const AT = new Date("2026-11-12T14:03:00Z");
const rows = (pathsGolden.body as unknown as PathsResponse).rows.filter((r) => r.gateway.name === "edge");
const gateway = (rows[0] as PathRow).gateway;

const parse = (svg: string) => new DOMParser().parseFromString(svg, "image/svg+xml");
const draw = (g: MapGraph, theme = controlRoomLight, stale = false) =>
  parse(exportSVG(g, layoutColumns(g), theme, { at: AT, stale }));

it.each([controlRoomLight, controlRoomDark].map((t) => [t.id, t] as const))(
  "exportSVG draws one group per node and one path per edge, in the %s theme's colours",
  (_, theme) => {
    const g = gatewayGraph(gateway, rows);
    const doc = draw(g, theme);
    expect(doc.querySelector("parsererror")).toBeNull();
    expect([...doc.querySelectorAll("[data-node]")].map((n) => n.getAttribute("data-node"))).toEqual(
      g.nodes.map((n) => n.id),
    );
    expect([...doc.querySelectorAll("path[data-edge]")].map((e) => e.getAttribute("data-edge"))).toEqual(
      g.edges.map((e) => e.id),
    );
    expect(doc.querySelector("svg > rect")?.getAttribute("fill")).toBe(theme.color.bg);
    for (const n of doc.querySelectorAll("[data-node]"))
      expect(n.querySelector("rect")?.getAttribute("fill")).toBe(theme.color.surface);
    // Each edge is stroked in its state's colour; the state is also its pattern, and each node's state is text.
    for (const e of g.edges) {
      const path = doc.querySelector(`path[data-edge="${CSS.escape(e.id)}"]`);
      expect(path?.getAttribute("stroke")).toBe(theme.color.status[e.state].fg);
    }
    for (const n of g.nodes)
      expect(doc.querySelector(`[data-node="${CSS.escape(n.id)}"]`)?.textContent).toContain(
        { healthy: "Healthy", warning: "Warning", error: "Error", unknown: "Unknown" }[n.state],
      );
    expect(doc.querySelector("svg > title")?.textContent).toBe(
      mapCopy.exportTitle("infra/edge", "2026-11-12 14:03 UTC"),
    );
  },
);

it("exportSVG gives each edge state its own pattern, and marks error edges as broken or partly failing", () => {
  const n = (id: string, kind: "gateway" | "listener") => ({
    id,
    kind,
    name: id,
    state: "healthy" as const,
    rows: [],
    problems: [],
  });
  const e = (id: string, state: MapGraph["edges"][number]["state"], broken = false) => ({
    id,
    source: "g",
    target: "l",
    state,
    broken,
    rows: [],
  });
  const g: MapGraph = {
    nodes: [n("g", "gateway"), n("l", "listener")],
    edges: [e("h", "healthy"), e("w", "warning"), e("u", "unknown"), e("p", "error"), e("b", "error", true)],
    rows: [],
  };
  const doc = draw(g);
  const dash = (id: string) => doc.querySelector(`path[data-edge="${id}"]`)?.getAttribute("stroke-dasharray");
  expect([dash("h"), dash("w"), dash("u"), dash("p"), dash("b")]).toEqual([null, "2 5", "10 4 2 4", null, "8 6"]);
  // A broken edge carries a filled error chip, a partly failing one a hollow ring.
  const marks = doc.querySelectorAll("svg > g:nth-of-type(2) > circle");
  expect([...marks].map((c) => c.getAttribute("fill"))).toEqual([
    controlRoomLight.color.bg,
    controlRoomLight.color.status.error.iconBg,
  ]);
  // A stale view draws every state unknown and no edge broken, as the canvas does.
  const stale = draw(g, controlRoomLight, true);
  expect(new Set([...stale.querySelectorAll("[data-state]")].map((x) => x.getAttribute("data-state")))).toEqual(
    new Set(["unknown"]),
  );
  expect(stale.querySelectorAll("[data-broken]")).toHaveLength(0);
});

it("exportSVG escapes names, so a name with <, & and quotes stays text", () => {
  const evil = `a<b>&c"d'e`;
  const gw = { ...gateway, name: `gw<&">` };
  const odd = rows.map((r) => ({ ...r, gateway: gw, routes: r.routes.map((x) => ({ ...x, name: evil })) }));
  const g = gatewayGraph(gw, odd);
  const svg = exportSVG(g, layoutColumns(g), controlRoomLight, { at: AT });
  expect(svg).not.toContain(evil);
  expect(svg).toContain(esc(evil));
  const doc = parse(svg);
  expect(doc.querySelector("parsererror")).toBeNull();
  expect(doc.querySelectorAll("script, b")).toHaveLength(0);
  const route = g.nodes.find((x) => x.kind === "route") as MapGraph["nodes"][number];
  expect(doc.querySelector(`[data-node="${CSS.escape(route.id)}"] text:nth-of-type(2)`)?.textContent).toBe(
    route.name.length > 26 ? `${route.name.slice(0, 25)}…` : route.name,
  );
  expect(doc.querySelector("svg > title")?.textContent).toContain(`gw<&">`);
});

it("export file names carry the Gateway and the date, safe for a file system", () => {
  expect(exportFileName("infra/edge", "svg", AT)).toBe("gateway-map-infra-edge-2026-11-12.svg");
  expect(exportFileName("a b/<c>", "png", AT)).toBe("gateway-map-a-b-c-2026-11-12.png");
});

// MAP_EXPORT_DIR=<dir> writes a light and a dark sample of the golden edge Gateway for review.
it("writes review samples when MAP_EXPORT_DIR is set", async () => {
  const env = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env;
  const dir = env?.MAP_EXPORT_DIR;
  if (!dir) return;
  const fsName = "node:fs";
  const fs = (await import(/* @vite-ignore */ fsName)) as { writeFileSync: (p: string, d: string) => void };
  const g = gatewayGraph(gateway, rows);
  for (const t of [controlRoomLight, controlRoomDark])
    fs.writeFileSync(`${dir}/map-${t.mode}.svg`, exportSVG(g, layoutColumns(g), t, { at: AT }));
});
