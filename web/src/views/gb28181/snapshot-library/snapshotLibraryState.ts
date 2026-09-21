import type { SnapshotLibraryQuery, SnapshotLibrarySource } from "@/api/gb28181";

/**
 * 图像库页面的纯逻辑：权限判定、筛选参数归一、取图地址拼装、文案与格式化。
 *
 * 抽到这里而不是写在组件里，是因为这几条**都是会接错的东西**：
 * 取图地址漏 token ⇒ 整页缩略图 401；筛选空值原样传给后端 ⇒ 后端把空串当"不筛选"
 * 倒也没错，但 `source=` 一旦写歪就是「参数错被当成没数据」这类最难查的故障。
 */

/** 与菜单授权、两个接口的 casbin 授权**同码**（见迁移 2026-09-20-channel-snapshot-library）。 */
export const SNAPSHOT_LIBRARY_PERMISSION = "gb28181:device:snapshot";

/**
 * 图像库页面路由，与后端菜单行的 `path` 逐字一致。
 *
 * ⛔ 菜单树是后端下发的（`route-output.ts` 按 `sys_menu` 生成路由），所以这个路径**不是**
 * 前端自己说了算：从别处跳过来之前得先确认路由已注册（见 SnapshotConfigPanel 的门禁），
 * 否则 push 到一个不存在的路由，用户看到的是空白而不是"菜单还没启用"。
 */
export const SNAPSHOT_LIBRARY_PATH = "/gb28181/snapshot-library";

const ALL_PERMISSION = "*:*:*";

/** 与后端默认值一致（`parsePositiveInt(c.DefaultQuery("pageSize", "40"), 40)`）。 */
export const SNAPSHOT_LIBRARY_PAGE_SIZE = 40;

/**
 * 每页条数选项。
 * ⛔ 不能超过 200：后端把 pageSize 硬性截到 200，给 500 会变成「显示 500/页、实际只有 200 条」，
 * 而分页器算出的页数也跟着错（总数 1000 时以为 2 页，其实 5 页）。
 */
export const SNAPSHOT_LIBRARY_PAGE_SIZE_OPTIONS = [20, 40, 80, 120, 200];

const SOURCE_LABELS: Record<SnapshotLibrarySource, string> = {
  // 「设备抓拍」= 设备收到 SnapshotConfig 后自己拍、自己 POST 回来（A.2.5.7）
  device: "设备抓拍",
  // 「平台抓帧」= 平台在起播时从 ZLM 拉流抓的一帧
  zlm: "平台抓帧",
  // 浏览器本地截图，当前**不落库**，保留映射是为了历史数据不至于显示成空白
  browser: "本地截图"
};

export const SNAPSHOT_SOURCE_OPTIONS: Array<{ value: SnapshotLibrarySource; label: string }> = (
  Object.keys(SOURCE_LABELS) as SnapshotLibrarySource[]
).map(value => ({ value, label: SOURCE_LABELS[value] }));

export function mayViewSnapshotLibrary(permissions: string[]): boolean {
  return permissions.includes(ALL_PERMISSION) || permissions.includes(SNAPSHOT_LIBRARY_PERMISSION);
}

/** 把任意字符串收窄成合法来源；不认识的一律当"未选"。 */
export function resolveSnapshotSource(raw: unknown): SnapshotLibrarySource | "" {
  return typeof raw === "string" && raw in SOURCE_LABELS ? (raw as SnapshotLibrarySource) : "";
}

export function snapshotSourceLabel(source: unknown): string {
  const resolved = resolveSnapshotSource(source);
  if (resolved) return SOURCE_LABELS[resolved];
  // ⛔ 不返回空串：来源列留白会被读成"页面没渲染出来"，而不是"这条数据来源是别的值"。
  return typeof source === "string" && source.trim() ? source.trim() : "未知来源";
}

export interface SnapshotLibraryFilters {
  /** 设备 20 位国标编码 */
  deviceCode: string;
  /** 通道国标编码 */
  channelCode: string;
  source: SnapshotLibrarySource | "";
  /** [from, to]，RFC3339；Arco range-picker 的 value-format 产出 */
  range: string[];
  /** 抓拍会话 id（从设备详情会话面板跳过来时带入） */
  sessionId: string;
}

export function emptySnapshotFilters(): SnapshotLibraryFilters {
  return { deviceCode: "", channelCode: "", source: "", range: [], sessionId: "" };
}

/** 把筛选里所有"没填"的项丢掉，只把真值发给后端。 */
export function normalizeSnapshotQuery(filters: SnapshotLibraryFilters, page: number, pageSize: number): SnapshotLibraryQuery {
  const query: SnapshotLibraryQuery = { page, pageSize };
  const deviceCode = filters.deviceCode.trim();
  if (deviceCode) query.deviceCode = deviceCode;
  const channelCode = filters.channelCode.trim();
  if (channelCode) query.channelCode = channelCode;
  const sessionId = filters.sessionId.trim();
  if (sessionId) query.sessionId = sessionId;
  if (filters.source) query.source = filters.source;
  const [from, to] = filters.range ?? [];
  // ⛔ 时间窗只取 range-picker 实际给出的两个端点；`from`/`to` 分开判空，
  // 因为允许只填一端（"某时刻之后"）。
  if (from) query.from = from;
  if (to) query.to = to;
  return query;
}

function firstQueryValue(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === "string" ? value[0] : "";
  return typeof value === "string" ? value : "";
}

/**
 * 从路由 query 初始化筛选。用于「设备详情里跑完一次抓拍 → 跳到图像库看这次会话的图」。
 *
 * ⛔ 会话面板装在内存 Registry 里，页面一刷新就没了，而图已经落库 ——
 * 所以 `sessionId` 是**唯一**能把一次抓拍重新聚起来的线索，必须支持从 URL 带进来。
 */
export function snapshotFiltersFromRoute(query: Record<string, unknown>): SnapshotLibraryFilters {
  return {
    ...emptySnapshotFilters(),
    deviceCode: firstQueryValue(query.deviceCode).trim(),
    channelCode: firstQueryValue(query.channelCode).trim(),
    source: resolveSnapshotSource(firstQueryValue(query.source)),
    sessionId: firstQueryValue(query.sessionId).trim()
  };
}

/**
 * 给取图地址补鉴权（以及子路径部署时补 base）。
 *
 * ⛔ 图像库读接口在鉴权组里（`gb28181:device:snapshot`），`<img src>` 带不了
 * Authorization 头 ⇒ 不补 token 的话整页缩略图全是 401 破图。
 * 走 `?token=` 是本仓既有约定（后端 `common.GetAccessToken` 同时接受 header 与 query，
 * 见 `sipDashboardStreamUrl` 的同款处理）。
 */
export function snapshotContentImageUrl(
  url: string | null | undefined,
  accessToken: string | null | undefined,
  baseUrl = ""
): string {
  const path = (url ?? "").trim();
  if (!path) return "";
  // 后端给的是站内绝对路径（/api/...），子路径部署时要补上 VITE_APP_BASE_URL；
  // 万一将来换成绝对 URL 就不要再缀东西。
  const prefix = /^https?:\/\//i.test(path) ? "" : baseUrl.replace(/\/+$/, "");
  const token = (accessToken ?? "").trim();
  if (!token) return `${prefix}${path}`;
  const separator = path.includes("?") ? "&" : "?";
  return `${prefix}${path}${separator}token=${encodeURIComponent(token)}`;
}

/** 字节数转人类可读。抓拍图典型 100KB~3MB，所以 B 档也要保留（0 字节的脏行要看得见）。 */
export function formatSnapshotSize(bytes: unknown): string {
  const value = typeof bytes === "number" && Number.isFinite(bytes) ? bytes : 0;
  if (value <= 0) return "0 B";
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / 1024 / 1024).toFixed(2)} MB`;
}

/**
 * 拍摄时刻显示。
 * ⛔ 用 `capturedAt`（从文件名反解）而不是接收时刻：设备补传时两者能差几小时，
 * 操作员找图时说的"几点拍的"指的是前者（后端筛选也打在它上面，显示另一列会自相矛盾）。
 */
export function formatCapturedAt(value: unknown): string {
  if (typeof value !== "string" || !value.trim()) return "-";
  // Go 的零值时间会序列化成 0001-01-01T00:00:00Z，直接显示成"公元 1 年"很吓人。
  if (value.startsWith("0001-01-01")) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "-";
  return date.toLocaleString("zh-CN", { hour12: false });
}
