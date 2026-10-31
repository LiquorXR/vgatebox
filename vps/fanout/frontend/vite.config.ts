import { writeFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vite";

const apiTarget = process.env.VGATEBOX_API_TARGET || "http://127.0.0.1:8899";

export default defineConfig({
  base: "./",
  plugins: [
    vue(),
    tailwindcss(),
    {
      // webdist is embedded by Go and otherwise ignored by git. Keep the marker
      // so a checkout without a frontend build still compiles.
      name: "keep-webdist-marker",
      closeBundle() {
        writeFileSync(fileURLToPath(new URL("../webdist/.gitkeep", import.meta.url)), "");
      },
    },
  ],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  build: {
    outDir: "../webdist",
    emptyOutDir: true,
    assetsDir: "assets",
    sourcemap: false,
    chunkSizeWarningLimit: 700,
    rollupOptions: {
      input: {
        main: fileURLToPath(new URL("./index.html", import.meta.url)),
        login: fileURLToPath(new URL("./login.html", import.meta.url)),
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": apiTarget,
      "/sub": apiTarget,
      "^/login$": apiTarget,
      "/advanced": apiTarget,
    },
  },
});
