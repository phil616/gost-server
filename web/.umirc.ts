import { defineConfig } from "@umijs/max";
import type Config from "webpack-5-chain";
const LicenseManifestPlugin = require("./license-manifest-plugin.cjs");
export default defineConfig({
  npmClient: "npm",
  hash: true,
  esbuildMinifyIIFE: true,
  title: "AltasCI Tunnel",
  stagewise: false,
  chainWebpack(config: Config) {
    config.plugin("license-manifest").use(LicenseManifestPlugin);
  },
  routes: [
    { path: "/login", component: "./Login" },
    { path: "/*", component: "./Console" },
  ],
  proxy: { "/api": { target: "http://127.0.0.1:18081", changeOrigin: true } },
  define: { "process.env.APP_VERSION": process.env.ALTASCI_VERSION || "dev" },
});
