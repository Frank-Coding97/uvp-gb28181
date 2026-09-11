import { describe, expect, it } from "vitest";

import {
  RUNTIME_TREND_WINDOW_MS,
  appendRuntimeSnapshot,
  buildRuntimeTrendChartState,
  createRuntimeTrendSpec,
  runtimeSnapshot,
  type RuntimeTrendHistory
} from "./runtimeChart";

function runtime(nodeId = 2, asOf = "2026-08-30T10:00:00Z", overrides: Record<string, unknown> = {}) {
  return {
    nodeId,
    name: `节点 ${nodeId}`,
    state: "active",
    status: "fresh",
    freshness: "fresh",
    asOf,
    heartbeatFreshness: "fresh",
    metrics: {
      mediaSourceCount: 2,
      multiMediaSourceMuxerCount: 2,
      tcpServerCount: 1,
      tcpSessionCount: 3,
      udpServerCount: 1,
      udpSessionCount: 1,
      tcpClientCount: 0,
      socketCount: 4,
      networkSessionCount: 4,
      netThreadLoad: 0.4,
      workThreadLoad: 0.2
    },
    metricsComplete: true,
    mediaFreshness: "fresh",
    streams: [{
      nodeId,
      media: { schema: "rtsp", vhost: "__defaultVhost__", app: "live", stream: "camera" },
      online: true,
      aliveSecond: 5,
      bytesSpeed: 1024,
      readerCount: 2,
      totalReaderCount: 2,
      originType: 1,
      recordingMp4: true,
      recordingHls: false,
      trackCount: 1
    }],
    ...overrides
  } as any;
}

describe("media runtime chart adapters", () => {
  it("keeps only the newest 60 samples and labels the session scope", () => {
    let state: RuntimeTrendHistory = { nodeId: 2, range: "session", samples: [] };
    for (let index = 0; index < 61; index += 1) {
      state = appendRuntimeSnapshot(state, runtime(2, `2026-08-30T10:${String(index).padStart(2, "0")}:00Z`), { nodeId: 2, range: "session" });
    }

    expect(state.samples).toHaveLength(60);
    expect(state.samples[0]?.asOf).toContain("10:01");
    expect(buildRuntimeTrendChartState(state).title).toBe("实时媒体速率");
    expect(buildRuntimeTrendChartState(state).sampledLabel).toContain("最近 5 分钟");
    expect(buildRuntimeTrendChartState(state).sampleCount).toBe(60);
  });

  it("clears history when node or range changes before adding the new sample", () => {
    const initial = appendRuntimeSnapshot({ nodeId: 2, range: "session", samples: [] }, runtime(2), { nodeId: 2, range: "session" });
    const switchedNode = appendRuntimeSnapshot(initial, runtime(3, "2026-08-30T11:00:00Z"), { nodeId: 3, range: "session" });
    const switchedRange = appendRuntimeSnapshot(switchedNode, runtime(3, "2026-08-30T11:01:00Z"), { nodeId: 3, range: "node" });

    expect(switchedNode.samples).toHaveLength(1);
    expect(switchedNode.samples[0]?.nodeId).toBe(3);
    expect(switchedRange.samples).toHaveLength(1);
    expect(switchedRange.range).toBe("node");
  });

  it("does not turn unavailable media or metrics into zero", () => {
    const sample = runtimeSnapshot(runtime(2, "2026-08-30T10:00:00Z", {
      metricsComplete: false,
      mediaFreshness: "unavailable",
      streams: undefined
    }));

    expect(sample.streamCount).toBeNull();
    expect(sample.viewerCount).toBeNull();
    expect(sample.throughput).toBeNull();
    expect(sample.sessionCount).toBeNull();
    expect(sample.netThreadLoad).toBeNull();
    expect(sample.recordingCount).toBeNull();
  });

  it("uses a fixed five-minute time window with compact local-time labels", () => {
    const samples = [
      runtimeSnapshot(runtime(2, "2026-08-30T10:00:00.000Z")),
      runtimeSnapshot(runtime(2, "2026-08-30T10:00:05.000Z"))
    ];
    const spec = createRuntimeTrendSpec(samples);
    const latest = Date.parse("2026-08-30T10:00:05.000Z");

    expect(spec.data?.[0]?.values).toEqual([
      { sampledAt: Date.parse("2026-08-30T10:00:00.000Z"), asOf: "2026-08-30T10:00:00.000Z", metric: "媒体速率 KB/s", value: 1 },
      { sampledAt: latest, asOf: "2026-08-30T10:00:05.000Z", metric: "媒体速率 KB/s", value: 1 }
    ]);
    expect(spec.series?.[0]).toMatchObject({ xField: "sampledAt", point: { visible: false } });
    expect(spec.axes?.[0]).toMatchObject({ min: 0, softMax: 1 });
    expect(spec.axes?.[0]).not.toHaveProperty("max");
    expect(spec.axes?.[1]).toMatchObject({
      orient: "bottom",
      type: "time",
      min: latest - RUNTIME_TREND_WINDOW_MS,
      max: latest,
      nice: false,
      layers: [{ tickCount: 5, timeFormat: "%H:%M:%S", timeFormatMode: "local" }]
    });
    expect(spec.tooltip).toMatchObject({
      activeType: "dimension",
      dimension: {
        title: { value: { field: "sampledAt" }, valueTimeFormat: "%Y-%m-%d %H:%M:%S", valueTimeFormatMode: "local" }
      }
    });
  });

  it("drops invalid timestamps instead of corrupting the continuous time axis", () => {
    const spec = createRuntimeTrendSpec([runtimeSnapshot(runtime(2, "not-a-time"))]);

    expect(spec.data?.[0]?.values).toEqual([]);
    expect(spec.axes?.[1]).toMatchObject({ type: "time" });
    expect(spec.axes?.[1]).not.toHaveProperty("min");
    expect(spec.axes?.[1]).not.toHaveProperty("max");
  });

  it("clips samples older than the visible five-minute window", () => {
    const latest = "2026-08-30T10:05:01.000Z";
    const spec = createRuntimeTrendSpec([
      runtimeSnapshot(runtime(2, "2026-08-30T10:00:00.000Z")),
      runtimeSnapshot(runtime(2, "2026-08-30T10:00:01.000Z")),
      runtimeSnapshot(runtime(2, latest))
    ]);

    expect(spec.data?.[0]?.values.map(datum => datum.asOf)).toEqual([
      "2026-08-30T10:00:01.000Z",
      latest
    ]);
  });

  it("describes the chart purpose without exposing implementation sampling details", () => {
    const state = buildRuntimeTrendChartState({
      nodeId: 2,
      range: "session",
      samples: [runtimeSnapshot(runtime())]
    });

    expect(state.sampledLabel).toBe("展示最近 5 分钟在线媒体流的实时传输速率合计");
    expect(state.sampledLabel).not.toContain("采样");
    expect(state.sampledLabel).not.toContain("60");
    expect(state.sampledLabel).not.toContain("2026");
  });
});
