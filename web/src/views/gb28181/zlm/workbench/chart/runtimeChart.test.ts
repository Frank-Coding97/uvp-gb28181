import { describe, expect, it } from "vitest";

import { appendRuntimeSnapshot, buildRuntimeTrendChartState, runtimeSnapshot, type RuntimeTrendHistory } from "./runtimeChart";

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
    expect(buildRuntimeTrendChartState(state).title).toBe("实时吞吐趋势");
    expect(buildRuntimeTrendChartState(state).sampledLabel).toContain("进入页面后采样");
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
});
