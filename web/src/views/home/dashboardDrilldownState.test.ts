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
    streams: [stream({ readerCount: 2 }), stream({ readerCount: -1, recordingMp4: true, recordingHls: true })],
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
