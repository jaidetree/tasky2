import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// In dev, Vite serves the app and proxies API calls to the Go service so the
// frontend talks to a same-origin "/" (ADR-0005). In build, output goes to the
// repo-root public/ directory the Go service serves in production.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/tasks": "http://localhost:8080",
    },
  },
  build: {
    outDir: "../public",
    emptyOutDir: true,
  },
});
