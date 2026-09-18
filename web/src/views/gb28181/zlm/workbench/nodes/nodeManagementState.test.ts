import { describe, expect, it } from "vitest";

import {
  filterNodeRecords,
  nodeHealth
} from "./nodeManagementState";

const nodes = [
  {
    id: 1,
    name: "主节点",
    host: "10.0.0.1",
    state: "active" as const,
    enabled: true,
    autoOnDemandReady: true,
    nearCapacity: false,
    recoveryRequired: false
  },
  {
    id: 2,
    name: "离线边缘节点",
    host: "10.0.0.2",
    state: "offline" as const,
    enabled: true,
    autoOnDemandReady: true,
    nearCapacity: false,
    recoveryRequired: false
  },
  {
    id: 3,
    name: "停用节点",
    host: "10.0.0.3",
    state: "active" as const,
    enabled: false,
    autoOnDemandReady: true,
    nearCapacity: false,
    recoveryRequired: false
  }
];

describe("node management state", () => {
  it("derives health from lifecycle and backend readiness without inventing zero data", () => {
    expect(nodeHealth(nodes[0])).toBe("healthy");
    expect(nodeHealth({ ...nodes[0], autoOnDemandReady: false })).toBe("warning");
    expect(nodeHealth({ ...nodes[0], nearCapacity: true })).toBe("warning");
    expect(nodeHealth(nodes[1])).toBe("critical");
    expect(nodeHealth(nodes[2])).toBe("healthy");
    expect(nodeHealth({ ...nodes[0], recoveryRequired: true })).toBe("critical");
  });

  it("filters management intent and heartbeat health independently", () => {
    expect(filterNodeRecords(nodes, { keyword: "边缘" }).map(node => node.id)).toEqual([2]);
    expect(filterNodeRecords(nodes, { enabled: false }).map(node => node.id)).toEqual([3]);
    expect(filterNodeRecords(nodes, { state: "active" }).map(node => node.id)).toEqual([1, 3]);
    expect(filterNodeRecords(nodes, { health: "critical" }).map(node => node.id)).toEqual([2]);
  });

  it("treats legacy rows without enabled as enabled", () => {
    const legacy = { ...nodes[0], enabled: undefined };
    expect(filterNodeRecords([legacy], { enabled: true })).toEqual([legacy]);
    expect(filterNodeRecords([legacy], { enabled: false })).toEqual([]);
  });
});
