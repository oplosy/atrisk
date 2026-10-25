import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  root: "apps/web",
  plugins: [react()],
  server: {
    // The web client calls relative /api paths; forward them to the API's default listen address.
    proxy: { "/api": "http://127.0.0.1:8080" },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: "./src/test-setup.ts",
    include: ["./src/**/*.test.{ts,tsx}", "./scripts/**/*.test.mjs"],
  },
  build: {
    outDir: "./dist",
    emptyOutDir: true,
  },
});
