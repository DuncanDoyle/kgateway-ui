// @vitest-environment node
import { build, type Rollup } from "vite";

// Builds the app into web/app/dist (as npm run build does) and checks what it wrote, so it never sees a stale dist.
const app = decodeURIComponent(new URL("../../../../../app/", import.meta.url).pathname);
const MAP_MAX_GZIP = 60 * 1024;

/** Gzipped size in bytes, without @types/node. */
async function gzipSize(code: string): Promise<number> {
  const stream = new Blob([code]).stream().pipeThrough(new CompressionStream("gzip"));
  return (await new Response(stream).arrayBuffer()).byteLength;
}

it("React Flow ships only in the map's lazy chunk, at most 60 kB gzipped", async () => {
  // Vitest sets NODE_ENV=test, which would keep React Flow's development branches in the bundle.
  vi.stubEnv("NODE_ENV", "production");
  const out = (await build({
    root: app,
    configFile: `${app}vite.config.ts`,
    logLevel: "silent",
  })) as Rollup.RollupOutput;
  vi.unstubAllEnvs();
  const chunks = out.output.filter((o): o is Rollup.OutputChunk => o.type === "chunk");
  const xyflow = (c: Rollup.OutputChunk) => Object.keys(c.modules).some((m) => m.includes("/node_modules/@xyflow/"));
  const map = chunks.filter((c) => c.isDynamicEntry && c.facadeModuleId?.endsWith("/pages/map/GatewayMap.tsx"));
  expect(map).toHaveLength(1);
  const [mapChunk] = map as [Rollup.OutputChunk];
  expect(xyflow(mapChunk)).toBe(true);
  // No entry chunk and no other chunk carries a React Flow module.
  expect(chunks.some((c) => c.isEntry)).toBe(true);
  expect(chunks.filter((c) => c !== mapChunk && xyflow(c)).map((c) => c.fileName)).toEqual([]);
  expect(await gzipSize(mapChunk.code)).toBeLessThanOrEqual(MAP_MAX_GZIP);
}, 120_000);
