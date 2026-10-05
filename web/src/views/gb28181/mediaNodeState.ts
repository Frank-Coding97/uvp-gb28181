/**
 * 流媒体节点状态（`sys_dict.code = media_node_state`）
 *
 * 节点上有**两个维度**，别混：
 * - `node.state`  生命周期（`active` / `maintenance` / `offline`）——走进本字典。
 * - `node.status` 数据采集新鲜度（`fresh` / `partial` / `unavailable`）——**不是**本字典值域，
 *                 是本模块的常量（`MEDIA_NODE_DATA_STATUS_LABEL`）。
 *
 * ⛔ 2026-10-05 收敛：`workbench/overview/MediaOverviewPanel.vue` 与
 *    `workbench/chart/overviewChart.ts` **各写了一份**「state + status 合成文案」，
 *    分支顺序不同（`unavailable` 与 `offline` 谁优先）、文案也不同（`维护` vs `维护中`）。
 *    同一份语义两处实现 = 迟早不一致，故收成本模块。
 *
 * ⛔ 判定与文案分开（`mediaNodeRuntimeKey` 判、`mediaNodeStateRuntimeText` 译）：
 *    原先 `overviewChart` 是 `if (status === "状态未知")` 这种**拿中文串反判逻辑**，
 *    字典一改名（或换个说法）判定就会静默走错分支。
 */
import type { DictLabelFallback } from "@/hooks/useDictOptions";

/** 字典 code（开发库 `sys_dict` id=18）。 */
export const DICT_CODE_MEDIA_NODE_STATE = "media_node_state";

/**
 * 字典未加载时的兜底，与开发库 `sys_dict_item`（id=138/139/140）逐字对齐。
 * ⛔ 收敛口径：此前 `active` 在四处被写成 `活跃` / `可用` / `在线` 三种，
 *    现统一为 `在线`。
 */
export const MEDIA_NODE_STATE_LABEL_FALLBACK: DictLabelFallback = {
  active: "在线",
  maintenance: "维护中",
  offline: "离线"
};

/**
 * 下拉筛选项兜底（顺序即展示顺序）。
 * ⛔ 含 `maintenance` —— 此前节点列表的「在线状态」筛选只有 在线/离线，
 *    而节点确有「维护中」态，等于筛不出来。
 */
export const MEDIA_NODE_STATE_OPTIONS_FALLBACK: ReadonlyArray<{ label: string; value: string }> = [
  { label: "在线", value: "active" },
  { label: "维护中", value: "maintenance" },
  { label: "离线", value: "offline" }
];

/** `node.status` —— 数据采集新鲜度，与生命周期状态是两个维度，故**不入**本字典。 */
export const MEDIA_NODE_DATA_STATUS_LABEL: Readonly<Record<string, string>> = {
  fresh: "在线",
  partial: "部分数据",
  unavailable: "采集失败"
};

/** 两个维度都认不出来时的文案。 */
export const MEDIA_NODE_UNKNOWN_TEXT = "状态未知";

/** 合成文案时命中的维度键（判定用，**与展示文案解耦**）。 */
export type MediaNodeRuntimeKey = "maintenance" | "offline" | "unavailable" | "partial" | "fresh" | "unknown";

/**
 * 判定节点当前该显示哪个维度 —— 生命周期状态优先，其次看数据采集新鲜度。
 * ⛔ 逻辑判断一律用本函数，**不要**去比合成出来的中文串。
 */
export function mediaNodeRuntimeKey(node: { state?: unknown; status?: unknown } | null | undefined): MediaNodeRuntimeKey {
  const state = typeof node?.state === "string" ? node.state : "";
  if (state === "maintenance") return "maintenance";
  if (state === "offline") return "offline";

  const status = typeof node?.status === "string" ? node.status : "";
  if (status === "unavailable" || status === "partial" || status === "fresh") return status;
  return "unknown";
}

/**
 * 节点运行时文案。
 *
 * @param labels 已合并兜底的 state 查表（`useDictLabelMap` 的产物）；省略则纯用兜底常量
 * @param node   至少要能取到 `state` / `status`
 */
export function mediaNodeStateRuntimeText(
  labels: DictLabelFallback | undefined,
  node: { state?: unknown; status?: unknown } | null | undefined
): string {
  const key = mediaNodeRuntimeKey(node);
  if (key === "maintenance" || key === "offline") {
    return { ...MEDIA_NODE_STATE_LABEL_FALLBACK, ...(labels || {}) }[key] ?? MEDIA_NODE_STATE_LABEL_FALLBACK[key]!;
  }
  if (key !== "unknown") return MEDIA_NODE_DATA_STATUS_LABEL[key];
  return MEDIA_NODE_UNKNOWN_TEXT;
}
