import type { AccessMatchType, SecurityEventSeverity, SecurityFirewallState } from "@/api/gb28181-security";

/**
 * 台账筛选：把界面上的中文选项翻译成服务端参数。
 *
 * ⛔ 这些条件只在点「查询」时提交（与图像库、在线用户等列表页一致），
 *    不要改成 watch 自动提交 —— 那会让"翻页 + 自动刷新"每 10 秒重算一次。
 * ⛔ 未知文案一律返回 undefined（= 不过滤），不要猜。
 */

export const RISK_ANY = "全部风险";
export const STATE_ANY = "全部状态";
export const TYPE_ANY = "全部类型";

const severityOptions: Record<string, SecurityEventSeverity> = {
  高危: "high",
  中危: "medium",
  低危: "low"
};

const firewallStateOptions: Record<string, SecurityFirewallState> = {
  已生效: "applied",
  待同步: "pending",
  同步失败: "failed"
};

const matchTypeOptions: Record<string, AccessMatchType> = {
  IP: "ip",
  CIDR: "cidr",
  "User-Agent": "user_agent"
};

const statusOptions: Record<string, "enabled" | "disabled"> = {
  已启用: "enabled",
  已停用: "disabled"
};

export function severityParam(label: string): SecurityEventSeverity | undefined {
  return severityOptions[label];
}

export function firewallStateParam(label: string): SecurityFirewallState | undefined {
  return firewallStateOptions[label];
}

export function matchTypeParam(label: string): AccessMatchType | undefined {
  return matchTypeOptions[label];
}

export function statusParam(label: string): "enabled" | "disabled" | undefined {
  return statusOptions[label];
}

export function normalizeKeyword(value: string): string {
  return value.trim();
}

export interface SecurityQuery {
  keyword: string;
  [key: string]: unknown;
}

/**
 * 条件是否为空 —— 空条件下才允许刷新总览计数，避免筛选把"待处理"数算小。
 * 按值判断而不是按字段名，这样新增下拉条件不会漏判。
 */
export function isEmptyQuery(query: SecurityQuery): boolean {
  return Object.values(query).every(value => value === "" || value === undefined);
}
