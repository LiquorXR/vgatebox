import { fileURLToPath, URL } from "node:url";
import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  // P2-3 载荷契约单源文件在 root 之外（../testdata），vite 默认不 serve，
  // 这里显式放行该目录；生产构建不受影响。
  server: {
    fs: {
      allow: [fileURLToPath(new URL("./", import.meta.url)), fileURLToPath(new URL("../testdata", import.meta.url))],
    },
  },
  test: {
    environment: "happy-dom",
    include: ["src/**/*.spec.ts"],
  },
});
