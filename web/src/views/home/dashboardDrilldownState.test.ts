import { describe, expect, it } from "vitest";
import type { ZLMOverview, ZLMRuntimeMedia } from "@/api/gb28181-zlm-runtime";
import { buildMediaRuntimeLedger, DASHBOARD_DRILLDOWN_RANGES } from "./dashboardDrilldownState";

function stream(values: Partial<ZLMRuntimeMedia> = {}): ZLMRuntimeMedia {
  return {
    nodeId: 1,
    media: { schema: "rtmp", vhost: "__defaultVhost__", app: "live", stream: "340200" },
    online: true,
    aliveSecond: 10,
    bytesSpeed: 100,
    readerCount: 0,
    totalReaderCount: 0,
    originType: 1,
    recordingMp4: false,
    recordingHls: false,
    trackCount: 2,
    ...values
  };
}

function overview(): ZLMOverview {
  return {
    nodes: [{
      nodeId: 1, name: "node-a", state: "active", status: "fresh", freshness: "fresh", asOf: "2026-09-04T10:00:00+08:00", heartbeatFreshness: "fresh",
      metrics: { mediaSourceCount: 2, multiMediaSourceMuxerCount: 0, tcpServerCount: 0, tcpSessionCount: 2, udpServerCount: 0, udpSessionCount: 1, tcpClientCount: 0, socketCount: 3, networkSessionCount: 3, netThreadLoad: 0, workThreadLoad: 0 }, metricsComplete: true, mediaFreshness: "fresh"
    }],
    streams: [stream({ readerCount: 2 }), stream({ media: { schema: "hls", vhost: "__defaultVhost__", app: "live", stream: "340201" }, readerCount: -1, recordingMp4: true, recordingHls: true })],
    metrics: { sampledNodeCount: 1, mediaSourceCount: 2, multiMediaSourceMuxerCount: 0, tcpServerCount: 0, tcpSessionCount: 2, udpServerCount: 0, udpSessionCount: 1, tcpClientCount: 0, socketCount: 3, networkSessionCount: 3, netThreadLoadAvg: 0, workThreadLoadAvg: 0, streamCount: 2 },
    partial: false, asOf: "2026-09-04T10:00:00+08:00", metricsSampledNodeIds: [1], mediaSampledNodeIds: [1], successfulNodeIds: [1], failedNodeIds: []
  };
}

describe("dashboard drilldown state", () => {
  it("keeps the supported history ranges metric-specific", () => {
    expect(DASHBOARD_DRILLDOWN_RANGES["sip-rpm"]).toEqual(["1h", "24h", "7d"]);
    expect(DASHBOARD_DRILLDOWN_RANGES["media-traffic-today"]).toEqual(["24h", "7d"]);
  });

  it("builds all media ledgers and totals from one overview snapshot", () => {
    const result = buildMediaRuntimeLedger(overview());
    expect(result.totals).toEqual({ streams: 2, viewers: 2, sessions: 3, recordings: 1 });
    expect(result.viewers).toHaveLength(1);
    expect(result.recordings).toHaveLength(1);
    expect(result.recordings[0]).toMatchObject({ recordingMp4: true, recordingHls: true });
    expect(result.partial).toBe(false);
  });

  it("groups protocol variants into one business stream and attaches its device channel", () => {
    const value = overview();
    value.streams = [
      stream({ media: { schema: "rtsp", vhost: "__defaultVhost__", app: "rtp", stream: "0200000000" }, readerCount: 1, bytesSpeed: 381_397, aliveSecond: 21 }),
      stream({ media: { schema: "rtmp", vhost: "__defaultVhost__", app: "rtp", stream: "0200000000" }, readerCount: 0, bytesSpeed: 373_535, aliveSecond: 20 }),
      stream({ media: { schema: "hls", vhost: "__defaultVhost__", app: "rtp", stream: "0200000000" }, readerCount: 0, bytesSpeed: 369_625, aliveSecond: 19, recordingHls: true })
    ];
    value.metrics.streamCount = 3;
    const result = buildMediaRuntimeLedger(value, [{
      streamId: "0200000000", deviceId: "34020000002000000001", deviceName: "南门摄像机",
      channelId: "34020000001320000001", channelName: "南门通道"
    }]);
    expect(result.totals).toMatchObject({ streams: 1, viewers: 1, recordings: 0 });
    expect(result.streams[0]).toMatchObject({
      streamId: "0200000000", protocols: ["hls", "rtmp", "rtsp"], bytesSpeed: 381_397,
      deviceName: "南门摄像机", channelName: "南门通道", aliveSecond: 21
    });
    expect(result.streams[0].mediaTargets).toHaveLength(3);
  });

  it("does not report an HLS protocol output as an active recording", () => {
    const value = overview();
    value.streams = [stream({ media: { schema: "hls", vhost: "v", app: "rtp", stream: "stream-1" }, recordingHls: true })];
    value.metrics.streamCount = 1;
    const result = buildMediaRuntimeLedger(value);
    expect(result.recordings).toHaveLength(0);
    expect(result.totals.recordings).toBe(0);
  });

  it("surfaces reconciliation mismatches instead of silently changing totals", () => {
    const value = overview();
    value.metrics.streamCount = 9;
    value.metrics.networkSessionCount = 8;
    const result = buildMediaRuntimeLedger(value);
    expect(result.totals).toMatchObject({ streams: 2, sessions: 3 });
    expect(result.partial).toBe(true);
    expect(result.warnings).toHaveLength(2);
  });
});
