import type { SecurityAccessRule } from "@/api/gb28181-security";

export interface AccessRuleToggleModel {
  listType: SecurityAccessRule["listType"];
  matchType: SecurityAccessRule["matchType"];
  value: string;
  scope: string;
  note: string;
  expiresAt?: SecurityAccessRule["expiresAt"];
}

export function buildAccessRuleTogglePayload(rule: AccessRuleToggleModel, enabled: boolean): Partial<SecurityAccessRule> {
  return {
    listType: rule.listType,
    matchType: rule.matchType,
    matchValue: rule.value,
    scope: rule.scope,
    status: enabled ? "enabled" : "disabled",
    expiresAt: rule.expiresAt,
    note: rule.note
  };
}
