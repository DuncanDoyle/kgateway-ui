import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  // The shell's built JS uses extensionless imports, so Vite bundles it rather than Node loading it.
  test: { environment: "jsdom", server: { deps: { inline: ["@snellius/shell"] } } },
});
