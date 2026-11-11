/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": new URL("./src", import.meta.url).pathname } },
  // themes.test.ts reads the stylesheets as text (?raw); Vitest blanks CSS
  // modules it isn't told to load.
  test: { css: { include: [/\/src\/(style|themes)\.css/] } },
});
