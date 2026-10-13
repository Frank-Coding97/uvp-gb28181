/**
 * 临时预览配置 —— 仅供本地截图/视觉验收使用，用完即删。
 *
 * 存在的唯一理由：主配置允许用 VITE_DEV_HTTPS=true 走 HTTPS（自签证书），
 * 而无头浏览器（CLI 无法关掉证书校验）打不开 https 页面。
 * 这里继承主配置，把 `server.https` 显式置空并换端口，
 * 因此**不需要动 `vite.config.ts`，也不影响正在跑的 5177**。
 */
import base from "./vite.config";

export default async (env: Record<string, unknown>) => {
  const config = (await (base as unknown as (e: unknown) => Promise<Record<string, any>>)(env)) ?? {};
  return {
    ...config,
    server: {
      ...(config.server ?? {}),
      https: undefined,
      host: "127.0.0.1",
      port: 5199,
      open: false
    }
  };
};
