// @vitest-environment node
import { build, type Rollup } from "vite";

// Builds the app as npm run build does, in memory, so web/app/dist is never touched and never stale.
const app = decodeURIComponent(new URL("../../../../../app/", import.meta.url).pathname);
// React Flow alone is ≈60 kB gzipped; the rest is map code.
const MAP_MAX_GZIP = 80 * 1024;
const MAP_DEPS = /\/node_modules\/(@xyflow|d3-|zustand)/;

/** Gzipped size in bytes, without @types/node. */
async function gzipSize(code: string): Promise<number> {
  const stream = new Blob([code]).stream().pipeThrough(new CompressionStream("gzip"));
  return (await new Response(stream).arrayBuffer()).byteLength;
}

afterEach(() => {
  vi.unstubAllEnvs();
});

it("React Flow ships only in the map's lazy chunk and its own CSS, the chunk at most 80 kB gzipped", async () => {
  // Vitest sets NODE_ENV=test, which would keep React Flow's development branches in the bundle.
  vi.stubEnv("NODE_ENV", "production");
  const out = (await build({
    root: app,
    configFile: `${app}vite.config.ts`,
    logLevel: "silent",
    build: { write: false },
  })) as Rollup.RollupOutput;
  const chunks = out.output.filter((o): o is Rollup.OutputChunk => o.type === "chunk");
  const css = new Map(
    out.output
      .filter((o): o is Rollup.OutputAsset => o.type === "asset" && o.fileName.endsWith(".css"))
      .map((a) => [a.fileName, String(a.source)]),
  );
  const carries = (c: Rollup.OutputChunk) => Object.keys(c.modules).some((m) => MAP_DEPS.test(m));
  const map = chunks.filter((c) => c.isDynamicEntry && c.facadeModuleId?.endsWith("/pages/map/GatewayMap.tsx"));
  expect(map).toHaveLength(1);
  const [mapChunk] = map as [Rollup.OutputChunk];
  expect(carries(mapChunk)).toBe(true);
  // No entry chunk and no other chunk carries React Flow or its dependencies.
  expect(chunks.some((c) => c.isEntry)).toBe(true);
  expect(chunks.filter((c) => c !== mapChunk && carries(c)).map((c) => c.fileName)).toEqual([]);
  // React Flow's base CSS is the map chunk's own asset, never in the entry's CSS.
  const mapCss = [...(mapChunk.viteMetadata?.importedCss ?? [])];
  expect(mapCss.some((f) => css.get(f)?.includes(".react-flow__pane"))).toBe(true);
  const entryCss = chunks.filter((c) => c.isEntry).flatMap((c) => [...(c.viteMetadata?.importedCss ?? [])]);
  expect(entryCss.length).toBeGreaterThan(0);
  for (const f of entryCss) expect(css.get(f)).not.toContain(".react-flow__");
  expect(await gzipSize(mapChunk.code)).toBeLessThanOrEqual(MAP_MAX_GZIP);
}, 120_000);
