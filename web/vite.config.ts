import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

// In development, vite serves the app and passes the API through to a
// running "rflib serve".
export default defineConfig({
  plugins: [react()],
  // The app is served locally, so one larger bundle is fine.
  build: { outDir: "dist", emptyOutDir: false, chunkSizeWarningLimit: 1000 },
  server: { proxy: { "/api": "http://127.0.0.1:8080" } },
  test: { environment: "jsdom", include: ["src/**/*.test.ts", "src/**/*.test.tsx"] },
});
