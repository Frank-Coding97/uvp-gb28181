/**
 * 出厂产品名（前端最后一道兜底）。
 *
 * 产品名的**真源**是 `server/config/config.example.yml` 的 `system.systemname`：
 * 出包时由 make-config.py 写进 config.yml，再经 `/config/get` 下发给前端，
 * `App.vue` 拿到后覆盖 `document.title`。
 *
 * 这里只负责「后端还没起来 / 配置接口失败」的那一瞬间：
 *   · `web/index.html` 的 `<title>` 管首屏（JS 还没跑）
 *   · 本常量管 Logo 与登录页的标题兜底
 *
 * ⛔ 三处取值必须逐字一致，否则会出现「首屏一个新名、接口挂了又变旧名」的跳变。
 *    `scripts/config-factory-defaults.test.mjs` 里钉了这条跨文件断言。
 * ⛔ 不要改成读 `import.meta.env.VITE_GLOB_APP_TITLE` 了事：`web/.env` 与
 *    `.env.production` 都**不在版本库里**（.gitignore），换台机器出包就可能是空值。
 */
export const DEFAULT_PRODUCT_TITLE = "UVP-GB28181统一视频接入平台";
