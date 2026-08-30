import { describe, expect, it } from "vitest";
import type { ZLMOverview } from "@/api/gb28181-zlm-runtime";

import { buildOverviewKpis } from "./overviewState";

function snapshot(overrides: Partial<ZLMOverview> = {}): ZLMOverview {
  return {
    nodes: [
      {
        nodeId: 2,
        name: "边缘节点 2",
        state: "active",
        status: "fresh",
        freshness: "fresh",
        asOf: "2026-08-30T10:00:00Z",
        heartbeatFreshness: "fresh",
        metrics: {
          mediaSourceCount: 2,
          multiMediaSourceMuxerCount: 2,
          tcpServerCount: 1,
          tcpSessionCount: 4,
          udpServerCount: 1,
          udpSessionCount: 2,
          tcpClientCount: 0,
          socketCount: 8,
          networkSessionCount: 6,
          netThreadLoad: 0.42,
          workThreadLoad: 0.31
        },
        metricsComplete: true,
        mediaFreshness: "fresh",
        streams: [
          {
            nodeId: 2,
            media: { schema: "rtsp", vhost: "__defaultVhost__", app: "live", stream: "camera-1" },
            online: true,
            aliveSecond: 20,
            bytesSpeed: 2048,
            readerCount: 3,
            totalReaderCount: 4,
            originType: 1,
            originTypeName: "RTP",
            recordingMp4: true,
            recordingHls: false,
            trackCount: 1
          }
        ]
      }
    ],
    streams: [
      {
        nodeId: 2,
        media: { schema: "rtsp", vhost: "__defaultVhost__", app: "live", stream: "camera-1" },
        online: true,
        aliveSecond: 20,
        bytesSpeed: 2048,
        readerCount: 3,
        totalReaderCount: 4,
        originType: 1,
        originTypeName: "RTP",
        recordingMp4: true,
        recordingHls: false,
        trackCount: 1
      }
    ],
    metrics: {
      sampledNodeCount: 1,
      mediaSourceCount: 2,
      multiMediaSourceMuxerCount: 2,
      tcpServerCount: 1,
      tcpSessionCount: 4,
      udpServerCount: 1,
      udpSessionCount: 2,
      tcpClientCount: 0,
      socketCount: 8,
      networkSessionCount: 6,
      netThreadLoadAvg: 0.42,
      workThreadLoadAvg: 0.31,
      streamCount: 1
    },
    partial: false,
    asOf: "2026-08-30T10:00:00Z",
    metricsSampledNodeIds: [2],
    mediaSampledNodeIds: [2],
    successfulNodeIds: [2],
    failedNodeIds: [],
    ...overrides
  };
}

describe("media overview KPI state", () => {
  it("derives node, stream, session, viewer, throughput and recording KPIs from one real snapshot", () => {
    const kpis = buildOverviewKpis(snapshot());

    expect(kpis.map(kpi => kpi.key)).toEqual([
      "nodes",
      "streams",
      "sessions",
      "viewers",
      "throughput",
      "recordings"
    ]);
    expect(kpis.find(kpi => kpi.key === "nodes")).toMatchObject({ value: 1, state: "ready" });
    expect(kpis.find(kpi => kpi.key === "streams")).toMatchObject({ value: 1, state: "ready" });
    expect(kpis.find(kpi => kpi.key === "viewers")).toMatchObject({ value: 3, state: "ready" });
    expect(kpis.find(kpi => kpi.key === "throughput")).toMatchObject({ value: 2048, state: "ready" });
    expect(kpis.find(kpi => kpi.key === "recordings")).toMatchObject({ value: 1, state: "ready" });
  });

  it("keeps runtime-derived KPIs unknown when no corresponding sample exists", () => {
    const state = buildOverviewKpis(snapshot({
      metricsSampledNodeIds: [],
      mediaSampledNodeIds: [],
      successfulNodeIds: [],
      failedNodeIds: [2],
      partial: true
    }));

    expect(state.find(kpi => kpi.key === "nodes")).toMatchObject({ value: 1, state: "ready" });
    for (const key of ["streams", "sessions", "viewers", "throughput", "recordings"] as const) {
      expect(state.find(kpi => kpi.key === key), key).toMatchObject({ value: null, state: "unknown" });
    }
  });

  it("labels partial samples without treating the returned subset as complete", () => {
    const state = buildOverviewKpis(snapshot({
      partial: true,
      failedNodeIds: [3]
    }));

    expect(state.find(kpi => kpi.key === "streams")).toMatchObject({ value: 1, state: "partial" });
    expect(state.find(kpi => kpi.key === "throughput")?.note).toContain("部分采样");
  });
});
