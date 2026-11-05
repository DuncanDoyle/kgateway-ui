// Playwright runs these files under Node; only process.env and readFileSync are used, so @types/node is not needed.
declare const process: { env: Record<string, string | undefined> };
declare module "node:fs" {
  export function readFileSync(path: string, encoding: "utf8"): string;
}
