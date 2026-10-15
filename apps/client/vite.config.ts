import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";
import { readFileSync, existsSync } from "node:fs";
import { Agent } from "node:https";
import { defineConfig } from "vite";

const certificate = fileURLToPath(
  new URL("../../.cache/tls/cert.pem", import.meta.url),
);
const key = fileURLToPath(new URL("../../.cache/tls/key.pem", import.meta.url));
const apiProxy = {
  target: "https://localhost:8080",
  agent: existsSync(certificate)
    ? new Agent({ ca: readFileSync(certificate) })
    : undefined,
};
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    host: "127.0.0.1",
    port: 1420,
    strictPort: true,
    https: existsSync(certificate)
      ? { cert: readFileSync(certificate), key: readFileSync(key) }
      : undefined,
    proxy: {
      "^/auth/(config|csrf|start|callback|me|logout|broker/|flow/)": apiProxy,
      "/healthz": apiProxy,
      "/readyz": apiProxy,
    },
    watch: { ignored: ["**/src-tauri/**"] },
  },
  build: { target: "es2022" },
});
