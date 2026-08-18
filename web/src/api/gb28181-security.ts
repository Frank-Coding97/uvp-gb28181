import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import type { BaseResult } from "./types";

export type SecurityMode = "observe" | "protect" | "strict";
export type SecurityRiskScope = "source" | "device";
export interface SecurityEventAggregate { bucketAt: string; sourceIp: string; deviceId?: string; riskScope?: SecurityRiskScope; transport: string; method: string; userAgent?: string; reason: string; action: string; count: number; scoreDelta: number; firstSeenAt: string; lastSeenAt: string }
export interface FirewallBan { decision: { decisionId: string; sourceIp: string; deviceId?: string; riskScope?: SecurityRiskScope; reason: string; score: number; ttl: number; permanent?: boolean; createdAt: string; triggerMethod?: string; triggerCount?: number; triggerThreshold?: number; windowSeconds?: number; policyMode?: SecurityMode }; status: string; ruleId: string; origin: string; agentState: string; firewallAppliedAt?: string; blockedCountAfterBan?: number; lastBlockedAt?: string; unbannedAt?: string; unbannedBy?: string; lastError?: string }
export interface SecurityPolicy { mode: SecurityMode; window: number; banScore: number; maxPacketBytes: number; maxUdpPerWindow: number; maxTcpConnections: number; samplePerSource: number; nonceTtl: number; permanentAutoBan?: boolean; banTTLs: { score: number; ttl: number }[]; allowlist: string[] }
export interface AgentStatus { connected: boolean; appliedRules: number; lastError?: string; checkedAt?: string }
export interface SecuritySnapshot { mode: SecurityMode; dropped: number; sampled: number; events: SecurityEventAggregate[]; bans: FirewallBan[]; agent: AgentStatus; asOf: string }
export type AccessListType = "blacklist" | "allowlist";
export type AccessMatchType = "ip" | "cidr" | "user_agent";
export interface SecurityAccessRule { id: number; listType: AccessListType; matchType: AccessMatchType; matchValue: string; scope: string; status: "enabled" | "disabled"; expiresAt?: string; note: string; createdBy: string; createdAt: string; updatedAt: string }
export interface SecurityPageParams { page?: number; pageSize?: number }
export interface SecurityBanPageParams extends SecurityPageParams { activeOnly?: boolean }
export interface SecurityPage<T> { items: T[]; total: number; page: number; pageSize: number }

export const getSecuritySnapshot = () => http.request<BaseResult<SecuritySnapshot>>("get", baseUrlApi("gb28181/security/snapshot"));
export const listSecurityEvents = (params: SecurityPageParams = {}) => http.request<BaseResult<SecurityPage<SecurityEventAggregate>>>("get", baseUrlApi("gb28181/security/events"), { params });
export const listSecurityBans = (params: SecurityBanPageParams = {}) => http.request<BaseResult<SecurityPage<FirewallBan>>>("get", baseUrlApi("gb28181/security/bans"), { params });
export const unbanSecurity = (id: string) => http.request<BaseResult<{ id: string; status: string }>>("post", baseUrlApi(`gb28181/security/bans/${encodeURIComponent(id)}/unban`));
export const getSecurityPolicy = () => http.request<BaseResult<SecurityPolicy>>("get", baseUrlApi("gb28181/security/policy"));
export const updateSecurityPolicy = (policy: SecurityPolicy) => http.request<BaseResult<SecurityPolicy>>("put", baseUrlApi("gb28181/security/policy"), { data: policy });
export const getSecurityAgentHealth = () => http.request<BaseResult<AgentStatus>>("get", baseUrlApi("gb28181/security/agent/health"));
export const buildSecurityStreamUrl = () => baseUrlApi("gb28181/security/stream");
export const listSecurityAccessRules = (listType?: AccessListType, params: SecurityPageParams = {}) => http.request<BaseResult<SecurityPage<SecurityAccessRule>>>("get", baseUrlApi("gb28181/security/access-rules"), { params: { ...params, ...(listType ? { listType } : {}) } });
export const createSecurityAccessRule = (rule: Partial<SecurityAccessRule>) => http.request<BaseResult<SecurityAccessRule>>("post", baseUrlApi("gb28181/security/access-rules"), { data: rule });
export const updateSecurityAccessRule = (id: number, rule: Partial<SecurityAccessRule>) => http.request<BaseResult<SecurityAccessRule>>("put", baseUrlApi(`gb28181/security/access-rules/${id}`), { data: rule });
export const deleteSecurityAccessRule = (id: number) => http.request<BaseResult<{ id: number; deleted: boolean }>>("delete", baseUrlApi(`gb28181/security/access-rules/${id}`));
