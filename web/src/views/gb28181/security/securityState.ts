import type { AgentStatus, FirewallBan, SecurityEventAggregate, SecurityPolicy, SecuritySnapshot } from "@/api/gb28181-security";

export interface SecurityState { loading: boolean; degraded: boolean; snapshot: SecuritySnapshot | null; events: SecurityEventAggregate[]; bans: FirewallBan[]; policy: SecurityPolicy | null; agent: AgentStatus; paused: boolean }
export function createSecurityState(): SecurityState {
  return { loading: false, degraded: false, snapshot: null, events: [], bans: [], policy: null, agent: { connected: false, appliedRules: 0 }, paused: false };
}
export function applySecuritySnapshot(state: SecurityState, snapshot: SecuritySnapshot): void {
  state.snapshot = snapshot; state.events = snapshot.events || []; state.bans = snapshot.bans || []; state.agent = snapshot.agent || state.agent; state.degraded = !!snapshot.agent?.lastError;
}
export function setSecurityDegraded(state: SecurityState, degraded: boolean): void { state.degraded = degraded; }
