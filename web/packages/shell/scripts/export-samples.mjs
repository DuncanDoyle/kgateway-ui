// Writes light and dark SVG exports of the golden infra/edge Gateway map, for review.
// Usage, from web/: node packages/shell/scripts/export-samples.mjs <out-dir>
import { readFileSync, writeFileSync } from "node:fs";
import { createServer } from "vite";

const dir = process.argv[2];
if (!dir) {
  console.error("usage: node packages/shell/scripts/export-samples.mjs <out-dir>");
  process.exit(2);
}
const root = decodeURIComponent(new URL("..", import.meta.url).pathname);
const golden = new URL("../../../../pkg/api/testdata/golden/paths.json", import.meta.url);

// Vite loads the TypeScript sources as the app's build does.
const vite = await createServer({
  root,
  configFile: false,
  logLevel: "error",
  appType: "custom",
  server: { middlewareMode: true, hmr: false },
});
try {
  const load = (p) => vite.ssrLoadModule(p);
  const { exportSVG } = await load("/src/pages/map/export.ts");
  const { gatewayGraph } = await load("/src/pages/map/graph.ts");
  const { layoutColumns } = await load("/src/pages/map/layout.ts");
  const { controlRoomDark, controlRoomLight } = await load("/src/theme/themes.ts");
  const rows = JSON.parse(readFileSync(golden, "utf8")).body.rows.filter((r) => r.gateway.name === "edge");
  const g = gatewayGraph(rows[0].gateway, rows);
  for (const t of [controlRoomLight, controlRoomDark]) {
    const out = `${dir}/map-${t.mode}.svg`;
    writeFileSync(out, exportSVG(g, layoutColumns(g), t));
    console.log(out);
  }
} finally {
  await vite.close();
}
