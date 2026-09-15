import { describe, expect, it } from "vitest";

import { buildSchedulerChartState, createSchedulerNodeSpec, createSchedulerResultSpec } from "./schedulerChart";

describe("scheduler chart adapters", () => {
  it("aggregates only the returned filtered sample and keeps its filter summary", () => {
    const state = buildSchedulerChartState([
      { id: 1, happenedAt: "2026-08-30T10:00:00Z", algorithm: "weighted", nodeID: 2, nodeName: "节点 2", streamID: "cam-1", deviceID: "d1", channelID: "c1", errorMessage: "" },
      { id: 2, happenedAt: "2026-08-30T10:01:00Z", algorithm: "weighted", nodeID: 3, nodeName: "节点 3", streamID: "cam-2", deviceID: "d2", channelID: "c2", errorMessage: "节点不可用" }
    ], { from: "2026-08-30T00:00:00Z", result: "success", nodeId: 2, algorithm: "weighted", limit: 2 });

    expect(state.status).toBe("partial");
    expect(state.sampleCount).toBe(2);
    expect(state.limit).toBe(2);
    expect(state.filterText).toContain("2026-08-30");
    expect(state.filterText).toContain("加权轮询");
    expect(state.filterText).toContain("2 条样本");
    expect(state.resultDistribution).toEqual([
      { category: "成功", count: 1 },
      { category: "失败", count: 1 }
    ]);
    expect(state.nodeDistribution).toEqual([
      { category: "节点 2", nodeId: 2, count: 1 },
      { category: "节点 3", nodeId: 3, count: 1 }
    ]);
    expect(createSchedulerResultSpec(state).data?.[0]?.values).toHaveLength(2);
    expect(createSchedulerNodeSpec(state).data?.[0]?.values).toHaveLength(2);
  });

  it("distinguishes no sample and unavailable input without inventing zero", () => {
    expect(buildSchedulerChartState([]).status).toBe("empty");
    expect(buildSchedulerChartState(undefined).status).toBe("unknown");
    expect(buildSchedulerChartState([], {}, { unavailable: true }).status).toBe("unavailable");
  });
});
