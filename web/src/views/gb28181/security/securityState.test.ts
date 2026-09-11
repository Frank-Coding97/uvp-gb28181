import { describe, expect, it } from "vitest";
import { applySecuritySnapshot, createSecurityState, setSecurityDegraded } from "./securityState";

describe("security state", () => {
  it("applies snapshot and agent health", () => {
    const state = createSecurityState();
    applySecuritySnapshot(state, { mode: "protect", dropped: 4, sampled: 1, events: [], bans: [], agent: { connected: true, appliedRules: 2 }, asOf: "2026-08-08T00:00:00Z" });
    expect(state.snapshot?.mode).toBe("protect"); expect(state.agent.appliedRules).toBe(2); expect(state.degraded).toBe(false);
  });
  it("represents an SSE failure without losing previous data", () => {
    const state = createSecurityState(); state.snapshot = { mode: "observe", dropped: 1, sampled: 0, events: [], bans: [], agent: { connected: true, appliedRules: 0 }, asOf: "now" };
    setSecurityDegraded(state, true); expect(state.degraded).toBe(true); expect(state.snapshot?.dropped).toBe(1);
  });
});
