import { describe, expect, it } from "vitest";

import {
  filterNodeRecords,
  nodeHealth,
  resolveNodeDetailView,
  type NodeDetailView
} from "./nodeManagementState";

const nodes = [
  {
    id: 1,
    name: "主节点",
    host: "10.0.0.1",
    state: "active" as const,
    autoOnDemandReady: true,
    nearCapacity: false,
    recoveryRequired: false
  },
  {
    id: 2,
    name: "离线边缘节点",
    host: "10.0.0.2",
    state: "offline" as const,
    autoOnDemandReady: true,
    nearCapacity: false,
    recoveryRequired: false
  },
  {
    id: 3,
    name: "维护节点",
    host: "10.0.0.3",
    state: "maintenance" as const,
    autoOnDemandReady: true,
    nearCapacity: false,
    recoveryRequired: false
  }
];

describe("node management state", () => {
  it("keeps the canonical node detail view allowlist in memory", () => {
    const views: Array<[unknown, NodeDetailView]> = [
      ["overview", "overview"],
      ["runtime", "runtime"],
      ["config", "config"],
      ["logs", "overview"],
      [undefined, "overview"]
    ];
    for (const [value, expected] of views) expect(resolveNodeDetailView(value)).toBe(expected);
  });

  it("derives health from lifecycle and backend readiness without inventing zero data", () => {
    expect(nodeHealth(nodes[0])).toBe("healthy");
    expect(nodeHealth({ ...nodes[0], autoOnDemandReady: false })).toBe("warning");
    expect(nodeHealth({ ...nodes[0], nearCapacity: true })).toBe("warning");
    expect(nodeHealth(nodes[1])).toBe("critical");
    expect(nodeHealth(nodes[2])).toBe("unknown");
    expect(nodeHealth({ ...nodes[0], recoveryRequired: true })).toBe("critical");
  });

  it("filters by safe keyword, lifecycle and health while retaining real rows", () => {
    expect(filterNodeRecords(nodes, { keyword: "边缘" }).map(node => node.id)).toEqual([2]);
    expect(filterNodeRecords(nodes, { state: "maintenance" }).map(node => node.id)).toEqual([3]);
    expect(filterNodeRecords(nodes, { health: "critical" }).map(node => node.id)).toEqual([2]);
  });
});
