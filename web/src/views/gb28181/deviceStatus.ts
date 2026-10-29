/**
 * 设备 / 通道「在线状态」字典（`sys_dict.code = device_status`）
 *
 * ⛔ 这个值域是**展示语义**，不是协议码 —— 后端在不同接口里给了三种形状：
 *   `online: boolean`、`status: 1 | 0`、设备自报的 `"online" | "offline"` 字符串。
 *   所以先**归一化**成字典键，再查字典；三种形状才落到同一把尺子上。
 *
 * ⛔ 调用方必须保持**原站点的极性**（即"值缺失时算在线还是离线"）：
 *     `x ? "在线" : "离线"`          ⇒ 传 `!!x`
 *     `x === false ? "离线" : "在线"` ⇒ 传 `x !== false`
 *     `x === 1 ? "在线" : "离线"`     ⇒ 传 `x === 1`
 *   本模块只负责"值 → 文案"，不替调用方决定未知值怎么算（那会静默改变行为）。
 */
import type { DictLabelFallback } from "@/hooks/useDictOptions";

/** 字典 code（开发库 `sys_dict` id=17）。 */
export const DICT_CODE_DEVICE_STATUS = "device_status";

/**
 * 字典未加载时的兜底，与开发库 `sys_dict_item`（id=136 在线 / id=137 离线）逐字对齐。
 * ⛔ 兜底只在字典缺失时生效 —— 现场改了字典名，界面必须跟着变。
 */
export const DEVICE_STATUS_LABEL_FALLBACK: DictLabelFallback = { online: "在线", offline: "离线" };

/** 归一化后的字典键；无法判定时为空串。 */
export type DeviceStatusKey = "online" | "offline" | "";

/**
 * 把 `boolean` / `number` / `string` 三种形状归一化成字典键。
 * 认不出来的一律返回 `""` —— 让调用方用自己的 `unknownText` 收口，**不猜**。
 */
export function deviceStatusKey(value: unknown): DeviceStatusKey {
  if (value === true || value === 1) return "online";
  if (value === false || value === 0) return "offline";
  const token = typeof value === "string" ? value.trim().toLowerCase() : "";
  if (token === "online" || token === "1" || token === "true") return "online";
  if (token === "offline" || token === "0" || token === "false") return "offline";
  return "";
}

/**
 * 值 → 文案。
 * @param labels 已合并兜底的查表（`useDictLabelMap` 的产物）
 * @param value 三种形状之一
 * @param unknownText 无法判定时的文案（默认空串）
 */
export function deviceStatusLabelFrom(labels: DictLabelFallback | undefined, value: unknown, unknownText = ""): string {
  const key = deviceStatusKey(value);
  if (!key) return unknownText;
  return labels?.[key] ?? DEVICE_STATUS_LABEL_FALLBACK[key] ?? key;
}
