import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import type { BaseResult } from "./types";

export type SecurityMode = "observe" | "protect" | "strict";
export interface SecurityEventAggregate { bucketAt: string; sourceIp: string; transport: string; method: string; reason: string; action: string; count: number; scoreDelta: number; firstSeenAt: string; lastSeenAt: string }
export interface FirewallBan { decision: { decisionId: string; sourceIp: string; reason: string; score: number; ttl: number; createdAt: string }; status: string; ruleId: string; origin: string; agentState: string; unbannedAt?: string; unbannedBy?: string }
export interface SecurityPolicy { mode: SecurityMode; window: number; banScore: number; maxPacketBytes: number; maxUdpPerWindow: number; maxTcpConnections: number; samplePerSource: number; nonceTtl: number; banTTLs: { score: number; ttl: number }[]; allowlist: string[] }
export interface AgentStatus { connected: boolean; appliedRules: number; lastError?: string; checkedAt?: string }
export interface SecuritySnapshot { mode: SecurityMode; dropped: number; sampled: number; events: SecurityEventAggregate[]; bans: FirewallBan[]; agent: AgentStatus; asOf: string }

export const getSecuritySnapshot = () => http.request<BaseResult<SecuritySnapshot>>("get", baseUrlApi("gb28181/security/snapshot"));
export const listSecurityEvents = (params: { limit?: number } = {}) => http.request<BaseResult<{ items: SecurityEventAggregate[]; total: number; limit: number }>>("get", baseUrlApi("gb28181/security/events"), { params });
export const listSecurityBans = () => http.request<BaseResult<{ items: FirewallBan[]; total: number }>>("get", baseUrlApi("gb28181/security/bans"));
export const unbanSecurity = (id: string) => http.request<BaseResult<{ id: string; status: string }>>("post", baseUrlApi(`gb28181/security/bans/${encodeURIComponent(id)}/unban`));
export const getSecurityPolicy = () => http.request<BaseResult<SecurityPolicy>>("get", baseUrlApi("gb28181/security/policy"));
export const updateSecurityPolicy = (policy: SecurityPolicy) => http.request<BaseResult<SecurityPolicy>>("put", baseUrlApi("gb28181/security/policy"), { data: policy });
export const getSecurityAgentHealth = () => http.request<BaseResult<AgentStatus>>("get", baseUrlApi("gb28181/security/agent/health"));
export const buildSecurityStreamUrl = () => baseUrlApi("gb28181/security/stream");
