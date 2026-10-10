/**
 * 登录失败原因的取值与展示。
 *
 * ⛔ **值是后端契约**：这 7 个 key 既作查询参数（`failureReason=`）发给后端，
 *    又是后端 `server/app/models/sysloginlogparam.go` 的 **switch 白名单**值
 *    （常量定义在 `server/app/service/loginlogservice.go`）⇒ 字典只能改展示名，
 *    key 一个都不许增删（删一个 = 筛选选项消失 + 与后端白名单对不上）。
 *
 * 本文件是**纯函数**（不 import vue/pinia），查表靠注入。
 */

export const DICT_CODE_LOGIN_FAILURE_REASON = "login_failure_reason";

/** 值域唯一真源（键序 = 下拉顺序）；⛔ 改这里必须同步 `loginFailureReason.test.ts` 与后端白名单。 */
export const LOGIN_FAILURE_REASON_LABEL_FALLBACK: Readonly<Record<string, string>> = {
  captcha_invalid: "验证码错误",
  user_not_found: "用户不存在",
  user_disabled: "用户未启用",
  account_locked: "账户已锁定",
  password_incorrect: "密码错误",
  session_create_failed: "会话创建失败",
  server_error: "服务器错误"
};

export interface LoginFailureReasonDictItem {
  name?: unknown;
  value?: unknown;
  status?: unknown;
}

export interface LoginFailureReasonOption {
  label: string;
  value: string;
}

/** 与 `useDictOptions` 同一口径：`status` 缺省视为启用，仅显式 0 / false 判停用。 */
function isEnabled(item: LoginFailureReasonDictItem): boolean {
  return item.status !== 0 && item.status !== false;
}

/**
 * 字典项 → `{ key: 展示名 }` 查表。
 * ⛔ **白名单式**：key 不在 `fallback` 里的字典项直接丢弃（字典只能改名，不能新增档）。
 */
export function loginFailureReasonLabelsFrom(
  items: readonly LoginFailureReasonDictItem[] | null | undefined,
  fallback: Readonly<Record<string, string>> = LOGIN_FAILURE_REASON_LABEL_FALLBACK
): Record<string, string> {
  const labels: Record<string, string> = { ...fallback };
  for (const item of items || []) {
    if (!isEnabled(item)) continue;
    const value = String(item.value ?? "").trim();
    const label = typeof item.name === "string" ? item.name.trim() : "";
    if (!value || !label || !(value in labels)) continue;
    labels[value] = label;
  }
  return labels;
}

/** 筛选下拉：key 与顺序都取自 `fallback`，只有 label 走字典。 */
export function loginFailureReasonOptionsFromLabels(
  labels: Readonly<Record<string, string>>,
  fallback: Readonly<Record<string, string>> = LOGIN_FAILURE_REASON_LABEL_FALLBACK
): LoginFailureReasonOption[] {
  return Object.keys(fallback).map(value => ({ value, label: labels[value] ?? fallback[value]! }));
}

/** 展示名；未命中回「其他失败」，空值回 `"-"`（与改动前逐字一致）。 */
export function loginFailureReasonLabelFrom(
  labels: Readonly<Record<string, string>> | null | undefined,
  reason?: string | null
): string {
  const key = (reason || "").trim();
  if (!key) return "-";
  const label = labels?.[key];
  return (typeof label === "string" && label.trim()) || "其他失败";
}
