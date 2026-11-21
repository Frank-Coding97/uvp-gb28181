/**
 * 任务调度策略（执行策略 / 阻塞策略）的取值与展示。
 *
 * ⛔ **值是后端契约**：`executionPolicy` / `blockingPolicy` 是 int，直接提交并落库
 *    （`server/app/models/sysjobs.go`；枚举见 `server/app/utils/schedulerhelper/job.go`
 *    的 `BlockDiscard = 0` / `BlockParallel = 1`）⇒ 字典只能改**展示名**，档位一个都不许增删。
 *
 * ⭐ 已核实**不存在"前后端两个真源"**：`job.go` 里那两个 `getExecutionPolicyName()` /
 *    `getBlockingPolicyName()` 是**包私有**、调用方只有日志（`logger.go` / `zap_logger.go`），
 *    API 返回的是 **int** ⇒ 前端本地译是对的口径，不需要像告警那样先搬翻译层。
 *
 * 本文件是**纯函数**（不 import vue/pinia），查表靠注入，便于单测。
 */

export const DICT_CODE_JOB_EXECUTE_POLICY = "job_execute_policy";
export const DICT_CODE_JOB_BLOCKING_POLICY = "job_blocking_policy";

/**
 * 值域唯一真源（键序 = 下拉顺序）。
 * ⛔ 改这里必须同步 `jobPolicy.test.ts` 与后端枚举，否则界面与库里的 int 会对不上。
 */
export const JOB_EXECUTE_POLICY_LABEL_FALLBACK: Readonly<Record<number, string>> = {
  0: "单次执行",
  1: "重复执行"
};

export const JOB_BLOCKING_POLICY_LABEL_FALLBACK: Readonly<Record<number, string>> = {
  0: "丢弃",
  1: "并行"
};

export interface JobPolicyDictItem {
  name?: unknown;
  value?: unknown;
  status?: unknown;
}

export interface JobPolicyOption {
  label: string;
  value: number;
}

/** 与 `useDictOptions` 同一口径：`status` 缺省视为启用，仅显式 0 / false 判停用。 */
function isEnabled(item: JobPolicyDictItem): boolean {
  return item.status !== 0 && item.status !== false;
}

/**
 * 字典项 → `{ "0": 展示名 }` 查表。
 * `fallback` 先铺底，命中的字典项覆盖之。
 * ⛔ **白名单式**：`value` 不在 `fallback` 里的字典项**直接丢弃** ——
 *    这几档要提交给后端落库，多一档就是契约破坏。
 */
export function jobPolicyLabelsFrom(
  items: readonly JobPolicyDictItem[] | null | undefined,
  fallback: Readonly<Record<number, string>>
): Record<string, string> {
  const labels: Record<string, string> = {};
  for (const [value, label] of Object.entries(fallback)) labels[value] = label;
  for (const item of items || []) {
    if (!isEnabled(item)) continue;
    const value = String(item.value ?? "").trim();
    const label = typeof item.name === "string" ? item.name.trim() : "";
    if (!value || !label || !(value in labels)) continue;
    labels[value] = label;
  }
  return labels;
}

/** 下拉选项：档位与顺序都取自 `fallback`，只有 label 走字典。 */
export function jobPolicyOptionsFromLabels(
  labels: Readonly<Record<string, string>>,
  fallback: Readonly<Record<number, string>>
): JobPolicyOption[] {
  return Object.keys(fallback).map(value => {
    const num = Number(value);
    return { value: num, label: labels[value] ?? fallback[num]! };
  });
}

/** 表格里按码值取展示名；未命中回 `"-"`（与改动前一致）。 */
export function jobPolicyLabelFrom(labels: Readonly<Record<string, string>> | null | undefined, value: unknown): string {
  if (value === null || value === undefined) return "-";
  const label = labels?.[String(value)];
  return (typeof label === "string" && label.trim()) || "-";
}
