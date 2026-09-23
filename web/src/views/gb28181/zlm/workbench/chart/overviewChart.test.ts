import { describe, expect, it } from "vitest";

import {
  buildOverviewChartState,
  createOverviewDistributionSpec,
  createOverviewHealthSpec,
  createOverviewNodeLoadSpec
} from "./overviewChart";

function overview(overrides: Record<string, unknown> = {}) {
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
          mediaSourceCount: 3,
          multiMediaSourceMuxerCount: 3,
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
            aliveSecond: 10,
            bytesSpeed: 1024,
            readerCount: 2,
            totalReaderCount: 2,
            originType: 1,
            originTypeName: "RTP",
            recordingMp4: true,
            recordingHls: false,
            trackCount: 1
          }
        ]
      },
      {
        nodeId: 3,
        name: "边缘节点 3",
        state: "offline",
        status: "unavailable",
        freshness: "unavailable",
        asOf: "2026-08-30T09:59:00Z",
        heartbeatFreshness: "stale",
        metrics: {
          mediaSourceCount: 0,
          multiMediaSourceMuxerCount: 0,
          tcpServerCount: 0,
          tcpSessionCount: 0,
          udpServerCount: 0,
          udpSessionCount: 0,
          tcpClientCount: 0,
          socketCount: 0,
          networkSessionCount: 0,
          netThreadLoad: 0,
          workThreadLoad: 0
        },
        metricsComplete: false,
        mediaFreshness: "unavailable",
        error: { stage: "probe", code: "OFFLINE", message: "节点连接失败", retryable: true }
      }
    ],
    streams: [
      {
        nodeId: 2,
        media: { schema: "rtsp", vhost: "__defaultVhost__", app: "live", stream: "camera-1" },
        online: true,
        aliveSecond: 10,
        bytesSpeed: 1024,
        readerCount: 2,
        totalReaderCount: 2,
        originType: 1,
        originTypeName: "RTP",
        recordingMp4: true,
        recordingHls: false,
        trackCount: 1
      }
    ],
    metrics: {
      sampledNodeCount: 1,
      mediaSourceCount: 3,
      multiMediaSourceMuxerCount: 3,
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
    partial: true,
    asOf: "2026-08-30T10:00:00Z",
    metricsSampledNodeIds: [2],
    mediaSampledNodeIds: [2],
    successfulNodeIds: [2],
    failedNodeIds: [3],
    errors: [{ nodeId: 3, error: { stage: "probe", code: "OFFLINE", message: "节点连接失败", retryable: true } }],
    ...overrides
  } as any;
}

describe("media overview chart adapters", () => {
  it("keeps partial provenance and unknown node metrics instead of converting them to zero", () => {
    const state = buildOverviewChartState(overview());

    expect(state.status).toBe("partial");
    expect(state.sampled).toEqual({ nodeIds: [2], count: 1, streamNodeIds: [2] });
    expect(state.failed).toEqual({ nodeIds: [3], count: 1 });
    expect(state.asOf).toBe("2026-08-30T10:00:00Z");
    expect(state.nodeLoad.find(item => item.nodeId === 3)?.netThreadLoad).toBeNull();
    expect(state.nodeLoad.find(item => item.nodeId === 3)?.workThreadLoad).toBeNull();
    expect(state.health.find(item => item.nodeId === 3)?.statusText).toContain("采集失败");
    expect(state.summary).toContain("采样 1 个节点");
    expect(state.summary).toContain("失败 1 个");
  });

  it("creates sorted load, health matrix, and protocol/source distribution specs from one snapshot", () => {
    const state = buildOverviewChartState(overview());
    const load = createOverviewNodeLoadSpec(state);
    const health = createOverviewHealthSpec(state);
    const distribution = createOverviewDistributionSpec(state);

    expect(load.series?.[0]).toMatchObject({ type: "bar", direction: "horizontal" });
    expect(load.data?.[0]?.values).toEqual(expect.arrayContaining([
      expect.objectContaining({ nodeId: 2, metric: "NetThread", value: 0.42 }),
      expect.objectContaining({ nodeId: 3, metric: "NetThread", value: null })
    ]));
    expect(health.series?.[0]).toMatchObject({ type: "heatmap" });
    expect(health.data?.[0]?.values).toEqual(expect.arrayContaining([
      expect.objectContaining({ nodeId: 3, dimension: "状态", value: null, label: "采集失败" })
    ]));
    expect(distribution.series?.[0]).toMatchObject({ type: "bar" });
    expect(distribution.data?.[0]?.values).toEqual(expect.arrayContaining([
      expect.objectContaining({ dimension: "协议", category: "rtsp", count: 1 }),
      expect.objectContaining({ dimension: "来源", category: "RTP", count: 1 })
    ]));
  });

  it("distinguishes empty and unavailable snapshots", () => {
    expect(buildOverviewChartState(null).status).toBe("unknown");
    expect(buildOverviewChartState({ ...overview(), nodes: [], streams: [], partial: false, successfulNodeIds: [], failedNodeIds: [] } as any).status).toBe("empty");
    expect(buildOverviewChartState({ ...overview(), nodes: [], streams: [], partial: true, successfulNodeIds: [], failedNodeIds: [2] } as any).status).toBe("unavailable");
  });

  it("does not call a maintenance/offline cluster ready when no node was sampled", () => {
    const base = overview();
    const snapshot = overview({
      nodes: [
        { ...base.nodes[0], state: "maintenance", status: "maintenance", metricsComplete: false, streams: undefined },
        { ...base.nodes[1], state: "offline", status: "offline", streams: undefined }
      ],
      streams: [],
      partial: false,
      metricsSampledNodeIds: [],
      mediaSampledNodeIds: [],
      successfulNodeIds: [],
      failedNodeIds: []
    });

    expect(buildOverviewChartState(snapshot).status).toBe("unknown");
  });
});
