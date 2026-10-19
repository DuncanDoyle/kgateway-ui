import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// In dev, the Go backend runs on :8080 (make run); the Vite dev server proxies the API to it.
export default defineConfig({
  plugins: [react({ jsxImportSource: "@emotion/react" })],
  server: { proxy: { "/api": "http://127.0.0.1:8080" } },
  build: { outDir: "dist", sourcemap: true },
});
