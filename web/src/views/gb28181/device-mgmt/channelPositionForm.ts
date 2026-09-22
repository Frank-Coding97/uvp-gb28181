/**
 * 通道「人工录入坐标」的入参归一与展示文案。
 *
 * 抽成纯模块而不是留在 index.vue 的 setup 里，是为了能真单测 —— 这段逻辑的三个分支
 * （不修改 / 设置 / 清除）在 UI 上肉眼不可区分，只能靠用例钉住。
 *
 * ## 契约（与后端 `UpdateChannel` 严格对齐）
 *
 * - 两个框都留空  → 返回 `{}`，请求体里**不带** longitude/latitude ⇒ 不动坐标
 * - 两个框都有值  → 返回数值对 ⇒ 设置坐标，并标 `manual` 来源
 * - 两个都是 0    → 返回 `0/0` ⇒ **清除**坐标（0 在本仓全链路表示"无坐标"）
 * - 只填一个      → 非法（后端会 400，前端先拦）
 * - 恰好一个为 0  → 非法（会让库里出现"经度新值 + 纬度旧值"的解释不了的半对状态）
 */

export type ChannelCoordPayload = { longitude?: number; latitude?: number };

export type ChannelCoordResolution = { payload: ChannelCoordPayload } | { error: string };

/** 坐标来源的展示名 —— 回答"这个坐标是谁写的"。空串（无坐标）不展示。 */
export const POSITION_SOURCE_TEXT: Record<string, string> = {
  catalog: "目录",
  mobile: "实时",
  manual: "人工"
};

/** 十进制数（可带符号与小数）。用于把 `0x10` / `1e5` 这类挡在坐标框外。 */
const DECIMAL_PATTERN = /^[+-]?(\d+(\.\d+)?|\.\d+)$/;

export function positionSourceText(item: { positionSource?: string | null }): string {
  return POSITION_SOURCE_TEXT[(item.positionSource || "").trim()] || "";
}

/**
 * 归一人工坐标入参。输入是表单里的**原始字符串**（`a-input` 绑定值），
 * 因为"空"与"0"在坐标语义里完全不同，必须有独立的表达。
 */
export function resolveChannelCoordPayload(longitude: string, latitude: string): ChannelCoordResolution {
  const lngRaw = (longitude ?? "").trim();
  const latRaw = (latitude ?? "").trim();
  if (!lngRaw && !latRaw) return { payload: {} };
  if (!lngRaw || !latRaw) return { error: "经度与纬度需成对填写, 只填一个是无效坐标" };
  // ⛔ 不用裸 Number()：`0x10` / `1e5` 这类也能过 isFinite，坐标框里不该接受。
  if (!DECIMAL_PATTERN.test(lngRaw) || !DECIMAL_PATTERN.test(latRaw)) return { error: "经纬度必须是十进制数字" };
  const lng = Number(lngRaw);
  const lat = Number(latRaw);
  if (lng === 0 && lat === 0) return { payload: { longitude: 0, latitude: 0 } };
  if (lng === 0 || lat === 0) {
    return { error: "经度与纬度都不能为 0（0 表示未设置坐标；如需清除请将两者都填 0）" };
  }
  if (lng < -180 || lng > 180) return { error: "经度非法, 应在 -180 ~ 180 之间" };
  if (lat < -90 || lat > 90) return { error: "纬度非法, 应在 -90 ~ 90 之间" };
  return { payload: { longitude: lng, latitude: lat } };
}

/**
 * 保存成功后本地回填的坐标三件套。
 *
 * ⛔ 必须跟后端语义一致，否则界面会显示一个库里没有的状态：
 *   `positionUpdatedAt` 在清除时置 null（后端写的是 NULL），不是"当前时间"。
 */
export function coordPatchAfterSave(payload: ChannelCoordPayload): {
  longitude: number;
  latitude: number;
  positionSource: string;
  positionUpdatedAt: string | null;
} | null {
  if (payload.longitude === undefined || payload.latitude === undefined) return null;
  const cleared = payload.longitude === 0 && payload.latitude === 0;
  return {
    longitude: payload.longitude,
    latitude: payload.latitude,
    positionSource: cleared ? "" : "manual",
    positionUpdatedAt: cleared ? null : new Date().toISOString()
  };
}
