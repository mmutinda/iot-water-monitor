import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    proxy: {
      "/latest": {
        target: "http://localhost:5001",
        changeOrigin: true,
      },
      "/analytics": {
        target: "http://localhost:5001",
        changeOrigin: true,
      },
      "/tanks": {
        target: "http://localhost:5001",
        changeOrigin: true,
      },
      "/pump": {
        target: "http://localhost:5001",
        changeOrigin: true,
      },
      "/environment": {
        target: "http://localhost:5001",
        changeOrigin: true,
      },
      "/login": {
        target: "http://localhost:5001",
        changeOrigin: true,
      },
      "/events": {
        target: "http://localhost:5001",
        changeOrigin: true,
      },
    },
  },
});
