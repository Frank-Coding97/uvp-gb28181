import { defineConfig, loadEnv } from "vite";
import fs from "node:fs";
import path from "path";
import { resolve } from "path";
import { include } from "./build/optimize";
import { createVitePlugins } from "./build/vite-plugin";
import postcssPresetEnv from "postcss-preset-env";

/**
 * 开发服务器 HTTPS 开关（默认关）。
 *
 * 用途只有一个：给需要**安全上下文**的浏览器能力提供 origin —— 对讲（getUserMedia
 * 拿麦克风 + WebRTC 上行）、摄像头采集等。`http://<局域网 IP>` 下
 * `window.isSecureContext === false`，`navigator.mediaDevices` 直接是 undefined，
 * 表现为「按钮点了没反应」，与后端无关。
 *
 * ⛔ 不要改成「certs 存在就自动启用」：手机模拟器（Ktor CIO）没有 TLS 豁免，
 * 扫描 http/https 混用下的接入二维码会握手失败（历史回滚原因，见 d2b4ce75）。
 *
 * ⛔ 开而不生效比不开更坏：证书缺失时显式失败，而不是静默回落 HTTP 让人对着
 * 「麦克风不可用」排查半天。
 */
// 返回 undefined（而不是 false）：vite 的 ServerOptions.https 只接受 https.ServerOptions，
// `false` 不是合法值，会让 defineConfig 的返回类型对不上 UserConfigFnObject。
function resolveDevHttps(env: Record<string, string>): { key: Buffer; cert: Buffer } | undefined {
  if (String(env.VITE_DEV_HTTPS ?? "").toLowerCase() !== "true") return undefined;
  const certDir = resolve(__dirname, "certs");
  const keyPath = path.join(certDir, "dev-key.pem");
  const crtPath = path.join(certDir, "dev.pem");
  for (const file of [keyPath, crtPath]) {
    if (!fs.existsSync(file)) {
      throw new Error(`VITE_DEV_HTTPS=true 但缺少证书 ${file}（可用 mkcert 生成到 web/certs/）`);
    }
  }
  return { key: fs.readFileSync(keyPath), cert: fs.readFileSync(crtPath) };
}

export default defineConfig(({ mode }) => {
  const root = process.cwd();
  const env: any = loadEnv(mode, root);
  const apiProxyTarget = env.VITE_APP_BASE_URL || "http://127.0.0.1:8280";

  return {
    base: "/",
    server: {
      host: "0.0.0.0",
      open: false,
      port: 5177,
      https: resolveDevHttps(env),
      proxy: {
        "/api": { target: apiProxyTarget, changeOrigin: true, xfwd: true },
        "/public": { target: apiProxyTarget, changeOrigin: true }
      }
    },
    plugins: [...createVitePlugins()],
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
