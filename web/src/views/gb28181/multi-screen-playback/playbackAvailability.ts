/**
 * 多屏回放「槽位可播性」的展示口径（纯函数模块，⛔ 不 import vue/pinia）。
 *
 * 值域由后端 `PlaybackSchemeSlot.availability` 决定，属**纯展示**：槽位不可播时
 * 给操作员一句话，既不参与校验、也不下发给设备，所以**不进「前端只读」名单**。
 *
 * ⚠️ 与本目录 `PlaybackSchemePanel.vue` 里 `availability-<值>` 那个 CSS 类严格分工：
 *    样式类取的是**原始值**（后端给什么就拼什么类名），字典只覆盖"这句话怎么说"。
 *    改字典不影响配色，加档位也不会凭空多出一个没有样式定义的类。
 */
export const DICT_CODE_PLAYABLE_STATE = "playable_state";

/** 兜底口径 —— 按后端 `availability` 值域写死，与字典项逐条对齐。 */
export const PLAYABLE_STATE_LABEL_FALLBACK: Readonly<Record<string, string>> = {
  available: "可播放",
  offline: "离线",
  missing: "通道不存在",
  forbidden: "无权访问"
};

/**
 * ⛔ 未命中话术：后端给了我们不认识的档位时用它。
 * 这是**调用点自己的兜底**，不放进字典 —— 字典能改的是"已知档位叫什么"，
 * 不是"不认识时说什么"。
 */
export const PLAYABLE_STATE_UNKNOWN_TEXT = "不可用";

/**
 * 槽位可播性 → 展示名。
 * `labels` 由注入层从字典取（见 `usePlayableStateDict.ts`），纯函数侧默认用兜底表。
 */
export function playableStateLabel(
  value: string | null | undefined,
  labels: Readonly<Record<string, string>> = PLAYABLE_STATE_LABEL_FALLBACK
): string {
  return labels[(value || "").trim()] || PLAYABLE_STATE_UNKNOWN_TEXT;
}
