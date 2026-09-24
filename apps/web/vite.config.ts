import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  root: "apps/web",
  plugins: [react()],
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
