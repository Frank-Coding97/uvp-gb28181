/**
 * 设备配置族里两处「值是下发报文码值」的枚举 —— 字典化（P4b）。
 *
 * ⛔⛔ 这两个值会**原样写进下发报文**（`FrameMirror` / `VideoRecordPlan.streamNumber`，
 *    见 `deviceConfigPayload.ts` 的 `requiredInt` 收窄），所以合并口径必须是
 *    【白名单式】：字典**只能给已声明的 value 改名**，槽位集合与顺序恒由代码锁死。
 *    字典里冒出来的新 value 一律**丢弃** —— 设备/后端收到没见过的档位只会静默忽略
 *    （或收窄失败），而界面上看不出任何异常。口径见技能 `uvp-dict-rollout` §5.3。
 *
 * ⛔ `label` 可换、`value` **绝不可动**；`shortLabel`（镜像按钮上的短字）也**不进字典** ——
 *    它跟图标一样是**控件形态**的一部分，而字典表只有"一码一名"三位，装不下第二个名字。
 *
 * ⛔ 本模块是**纯函数层**：不 import vue / pinia，单测可直接跑
 *    （同 `channelAttributeText.ts` / `deviceStatus.ts` 的分层）。
 */
import type { ConfigSelectOption } from "./deviceConfigGroups";

/** `sys_dict.code`：画面镜像（FrameMirror 枚举）。 */
export const DICT_CODE_FRAME_MIRROR = "frame_mirror";
/** `sys_dict.code`：码流编号（录像码流 / 视频参数）。 */
export const DICT_CODE_STREAM_NUMBER = "stream_number";

/**
 * 画面镜像值域兜底（FrameMirror enumeration，抄自 A.2.1.23）。
 *
 * ⛔ 1/2 别写反：**1 = 水平镜像（左右翻转）**、2 = 上下镜像（上下翻转）。
 *    写反的后果是"下发上下翻转、画面左右翻"，回读对账还会显示一致
 *    （因为对账比的是值，不是画面对不对），只能靠人眼发现。
 */
export const FRAME_MIRROR_LABEL_FALLBACK: Readonly<Record<string, string>> = {
  "0": "不启用镜像",
  "1": "水平镜像（左右翻转）",
  "2": "上下镜像（上下翻转）",
  "3": "中心镜像（旋转 180°）"
};

/** 码流编号值域兜底（0 = 主码流，1 = 子码流 1…）。 */
export const STREAM_NUMBER_LABEL_FALLBACK: Readonly<Record<string, string>> = {
  "0": "主码流",
  "1": "子码流 1",
  "2": "子码流 2",
  "3": "子码流 3"
};

/**
 * 白名单式取名：**只有当 `key` 在代码声明的 `fallback` 里**时才认字典给的名字。
 * 用于那些不经过 `options` 数组、直接按编号取名的调用点（如播放控制台的视频参数卡）。
 */
export function whitelistedLabel(
  labels: Readonly<Record<string, string>>,
  fallback: Readonly<Record<string, string>>,
  key: string
): string | undefined {
  if (!Object.prototype.hasOwnProperty.call(fallback, key)) return undefined;
  return labels[key] ?? fallback[key];
}

/**
 * 把字典里的名字盖到**代码已声明的**选项上（白名单式合并）。
 *
 * ⛔ 遍历源是 `options`（代码），**不是**字典的 items —— 字典多出来的 value
 *    天然落不进结果，槽位集合与顺序因此恒等于代码；`shortLabel` / `sample`
 *    这些"第二个名字 / 模板"也原样保留。
 */
export function applyOptionLabels(
  options: readonly ConfigSelectOption[],
  labels: Readonly<Record<string, string>>
): ConfigSelectOption[] {
  return options.map(option => ({
    ...option,
    label: labels[option.value] ?? option.label
  }));
}

/**
 * 码流编号 → 名字（**任意**编号）。
 *
 * 配置表单只列 0–3（协议里录像码流就这几档），但播放控制台的「视频参数」卡
 * 是按设备**实际上报**的码流数逐行渲染的，编号可能超出 3。
 * ⇒ 0–3 走字典（且只在兜底值域内认字典），超出部分回落**过程式**「子码流 N」。
 */
export function streamNumberLabelFrom(labels: Readonly<Record<string, string>>, value: unknown): string {
  if (value === null || value === undefined || value === "") return "";
  const key = String(value);
  const hit = whitelistedLabel(labels, STREAM_NUMBER_LABEL_FALLBACK, key);
  if (hit) return hit;
  const num = Number(value);
  if (Number.isFinite(num)) return num === 0 ? "主码流" : `子码流 ${num}`;
  return key;
}
