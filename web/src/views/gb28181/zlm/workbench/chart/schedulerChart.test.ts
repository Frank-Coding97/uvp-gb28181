import { describe, expect, it } from "vitest";

import { buildSchedulerChartState, createSchedulerNodeSpec, createSchedulerResultSpec } from "./schedulerChart";

const log = (id: number, happenedAt: string, nodeID: number, errorMessage = "") => ({
  id,
  happenedAt,
  algorithm: "weighted",
  nodeID,
  nodeName: `节点 ${nodeID}`,
  streamID: `cam-${id}`,
  deviceID: "d1",
  channelID: "c1",
  errorMessage
});

describe("scheduler chart adapters", () => {
  it("groups the last 24 clock hours on a time axis, including hours with no logs", () => {
    const to = new Date(2026, 8, 30, 10, 35);
    const previousHour = new Date(2026, 8, 30, 9, 10);
    const state = buildSchedulerChartState(
      [log(1, previousHour.toISOString(), 2), log(2, previousHour.toISOString(), 0, "节点不可用")],
      { to: to.toISOString(), limit: 1000 },
      { period: "24h" }
    );
    const result = createSchedulerResultSpec(state);
    const nodes = createSchedulerNodeSpec(state);

    expect(state.status).toBe("ready");
    expect(state.timeBuckets).toHaveLength(24);
    expect(state.timeBuckets[0]).toBe(new Date(2026, 8, 29, 11).toISOString());
    expect(state.timeBuckets.at(-1)).toBe(new Date(2026, 8, 30, 10).toISOString());
    expect(result.series?.[0]).toMatchObject({ xField: "bucket", yField: "count", seriesField: "category", stack: true });
    expect(result.axes?.[1]?.label).toMatchObject({ autoHide: true });
    expect((result.axes?.[1]?.label as { formatMethod: (value: string) => string }).formatMethod(state.timeBuckets[0])).toBe(
      "11:00"
    );
    expect(result.tooltip).toMatchObject({ activeType: "dimension", dimension: { title: { visible: false } } });
    expect(result.data?.[0]?.values).toContainEqual({
      bucket: new Date(2026, 8, 30, 9).toISOString(),
      category: "已命中",
      count: 1
    });
    expect(nodes.series?.[0]).toMatchObject({ xField: "bucket", yField: "count", seriesField: "category", stack: true });
    expect(nodes.data?.[0]?.values.some(item => item.category === "节点 2" && item.count === 1)).toBe(true);
  });

  it("uses seven local calendar dates on the weekly axis", () => {
    const to = new Date(2026, 9, 1, 12);
    const previousDay = new Date(2026, 8, 30, 9);
    const state = buildSchedulerChartState([log(1, previousDay.toISOString(), 2)], { to: to.toISOString() }, { period: "7d" });
    const spec = createSchedulerResultSpec(state);

    expect(state.timeBuckets).toHaveLength(7);
    expect(state.timeBuckets[0]).toBe(new Date(2026, 8, 25).toISOString());
    expect(state.timeBuckets.at(-1)).toBe(new Date(2026, 9, 1).toISOString());
    expect((spec.axes?.[1]?.label as { formatMethod: (value: string) => string }).formatMethod(state.timeBuckets[0])).toBe(
      "9/25"
    );
    expect(spec.tooltip).toMatchObject({ dimension: { title: { visible: false } } });
    expect(spec.data?.[0]?.values).toContainEqual({ bucket: new Date(2026, 8, 30).toISOString(), category: "已命中", count: 1 });
  });

  it("distinguishes no sample and unavailable input without inventing zero", () => {
    expect(buildSchedulerChartState([]).status).toBe("empty");
    expect(buildSchedulerChartState(undefined).status).toBe("unknown");
    expect(buildSchedulerChartState([], {}, { unavailable: true }).status).toBe("unavailable");
  });
});
