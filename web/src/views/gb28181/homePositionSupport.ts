/**
 * 看守位「能力三态」的展示口径（纯函数模块，⛔ 不 import vue/pinia）。
 *
 * 值域由后端 `HomePositionSupport.status` 决定（`supported/unsupported/unknown`），
 * 属**纯展示** —— 只出现在看守位卡片的诊断提示里，既不参与判定、也不下发给设备，
 * 所以**不进「前端只读」名单**。
 *
 * ⚠️ 与 `components/PlayConsoleLinked.vue` 里 `homePresentation` 那个 8 态状态机严格分工：
 *    那是**派生状态**（由控制能力 × 查询能力 × 查询阶段 × 已确认配置组合而来，文案如
 *    「设备不支持看守位」「尚未确认设备能力」），不是"一码一名"，必须留在代码里。
 *    字典只覆盖这一处「某个能力态怎么念」。
 * ⚠️ 同文件 `capabilityActionTitle` 里另有「设备上报不支持 / 设备未明确声明支持」——
 *    那是**拼进整句的说明**（还要带上 action 名与 reason），不是一码一名，同样留代码。
 */
export const DICT_CODE_HOME_POSITION_SUPPORT = "home_position_support";

/** 兜底口径 —— 按后端 `HomePositionSupportStatus` 值域写死，与字典项逐条对齐。 */
export const HOME_POSITION_SUPPORT_LABEL_FALLBACK: Readonly<Record<string, string>> = {
  supported: "支持",
  unsupported: "不支持",
  unknown: "尚未确认"
};

/**
 * 能力态 → 展示名。
 *
 * ⛔ 空值按 `unknown` 处理 —— "还没读到"本就等于"尚未确认"，回显空串会让诊断提示里
 *    出现「控制能力：」这种读不通的半句。
 *    非空但未在表里的码值**原样回显**（后端加了第四态时要看得见它到底叫什么）。
 */
export function homePositionSupportLabel(
  status: string | null | undefined,
  labels: Readonly<Record<string, string>> = HOME_POSITION_SUPPORT_LABEL_FALLBACK
): string {
  const key = (status || "").trim() || "unknown";
  return labels[key] || key;
}
