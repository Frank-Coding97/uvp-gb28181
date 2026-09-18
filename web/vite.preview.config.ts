/**
 * 临时预览配置 —— 仅供本地截图/视觉验收使用，用完即删。
 *
 * 存在的唯一理由：项目 dev server 在 `web/certs/` 存在时会自动启用 HTTPS，
 * 而自签证书会让无头浏览器（CLI 无法关掉证书校验）打不开页面。
 * 这里继承主配置，只把 `server.https` 覆盖为 false 并换端口，
 * 因此**不需要动 `vite.config.ts`，也不影响正在跑的 5177**。
 */
import base from "./vite.config";

export default async (env: Record<string, unknown>) => {
    const config = (await (base as unknown as (e: unknown) => Promise<Record<string, any>>)(env)) ?? {};
    return {
        ...config,
        server: {
            ...(config.server ?? {}),
            https: false,
            host: "127.0.0.1",
            port: 5199,
            open: false
        }
    };
};
