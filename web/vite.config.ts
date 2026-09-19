import { defineConfig, loadEnv } from "vite";
import fs from "node:fs";
import path from "path";
import { resolve } from "path";
import { include } from "./build/optimize";
import { createVitePlugins } from "./build/vite-plugin";
import postcssPresetEnv from "postcss-preset-env";

export default defineConfig(({ mode }) => {
  const root = process.cwd();
  const env: any = loadEnv(mode, root);
  const apiProxyTarget = env.VITE_APP_BASE_URL || "http://127.0.0.1:8280";

  const certDir = path.resolve(__dirname, "certs");
  const certKeyPath = path.join(certDir, "dev-key.pem");
  const certCrtPath = path.join(certDir, "dev.pem");
  const devHttps =
    fs.existsSync(certKeyPath) && fs.existsSync(certCrtPath)
      ? { key: fs.readFileSync(certKeyPath), cert: fs.readFileSync(certCrtPath) }
      : undefined;

  return {
    base: "/",
    server: {
      host: "0.0.0.0",
      open: false,
      port: 5177,
      https: false,
      proxy: {
        "/api": { target: apiProxyTarget, changeOrigin: true, xfwd: true },
        "/public": { target: apiProxyTarget, changeOrigin: true }
      }
    },
    plugins: [...createVitePlugins(env)],
    resolve: {
      alias: {
        "@assets": path.join(__dirname, "src/assets"),
        "@": resolve(__dirname, "./src")
      }
    },
    css: {
      postcss: { plugins: [postcssPresetEnv()] },
      preprocessorOptions: {
        scss: { additionalData: `@use "@/style/var/index.scss" as *; ` }
      }
    },
    optimizeDeps: { include },
    build: {
      outDir: "dist",
      minify: "terser",
      terserOptions: {
        compress: { keep_infinity: true, drop_console: true, drop_debugger: true },
        format: { comments: false }
      },
      assetsInlineLimit: 50 * 1024,
      chunkSizeWarningLimit: 50000,
      rollupOptions: {
        output: {
          chunkFileNames: "static/js/[name]-[hash].js",
          entryFileNames: "static/js/[name]-[hash].js",
          assetFileNames: "static/[ext]/[name]-[hash].[ext]"
        }
      }
    }
  };
});
