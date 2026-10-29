/**
 * 接口基址归一化。
 *
 * ⛔⛔ 这里绝不能直接拿 `import.meta.env.VITE_APP_BASE_URL` 去拼 `/api/`：基址一旦以
 * 斜杠结尾（根部署写成 `/`、子目录部署写成 `/uvp/` 都是常见写法），拼出来就是
 * `//api/login` —— 这是**协议相对 URL**，浏览器会把 `api` 当成主机名，
 * 于是全站请求变成 `https://api/login`，网络层直接 `ERR_NAME_NOT_RESOLVED`，
 * 表现为「登录页能看见、点登录没反应、整站白转」。2026-10-22 线上热修就踩过这一条。
 *
 * 所以：去掉首尾空白与**结尾的全部斜杠**，根部署归一成空串（`"" + "/api/x"` = `/api/x`）。
 * 与 `snapshotLibraryState.ts` 的 `snapshotContentImageUrl()` 同一口径，别各写一套。
 */
export const normalizeAppBaseUrl = (value: string | null | undefined): string => (value ?? "").trim().replace(/\/+$/, "");

/** 纯函数：基址 + 接口路径 ⇒ 站内绝对路径。抽出来是为了能单测，别再内联拼串。 */
export const joinApiPath = (baseUrl: string | null | undefined, url: string): string =>
  `${normalizeAppBaseUrl(baseUrl)}/api/${url}`;

const appBaseUrl = normalizeAppBaseUrl(import.meta.env.VITE_APP_BASE_URL);

export const baseUrlApi = (url: string) => joinApiPath(appBaseUrl, url);
// 获取基础API URL
export const getBaseUrl = () => appBaseUrl;
