// Playwright runs these files under Node; only process.env is used, so @types/node is not needed.
declare const process: { env: Record<string, string | undefined> };
