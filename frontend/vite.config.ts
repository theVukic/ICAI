import { defineConfig } from "vite";
import path from "path";

export default defineConfig({
  base: "./",

  server: {
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },

  build: {
    outDir: path.resolve(__dirname, "../app/dist"),
    emptyOutDir: true,
  },
});
