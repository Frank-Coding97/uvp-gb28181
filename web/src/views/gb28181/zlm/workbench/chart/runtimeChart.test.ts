import { describe, expect, it } from "vitest";

import { runtimeMediaRateSamples, runtimeSnapshot, runtimeTrendSamples } from "./runtimeChart";

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
      workThreadLoad: 0.2,
      upstreamBytesPerSecond: 2048,
      downstreamBytesPerSecond: 4096,
      mediaTrafficAvailable: true
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
  it("does not turn unavailable media or metrics into zero", () => {
    const sample = runtimeSnapshot(runtime(2, "2026-08-30T10:00:00Z", {
      metricsComplete: false,
      mediaFreshness: "unavailable",
      streams: undefined,
      metrics: {
        ...runtime().metrics,
        mediaTrafficAvailable: false
      }
    }));

    expect(sample.streamCount).toBeNull();
    expect(sample.viewerCount).toBeNull();
    expect(sample.throughput).toBeNull();
    expect(sample.upstream).toBeNull();
    expect(sample.downstream).toBeNull();
    expect(sample.sessionCount).toBeNull();
    expect(sample.netThreadLoad).toBeNull();
    expect(sample.recordingCount).toBeNull();
  });


  it("reads directional media rates without inventing zero for unavailable traffic", () => {
    const available = runtimeSnapshot(runtime());
    const unavailable = runtimeSnapshot(runtime(2, "2026-08-30T10:00:05.000Z", {
      metrics: {
        ...runtime().metrics,
        upstreamBytesPerSecond: 0,
        downstreamBytesPerSecond: 0,
        mediaTrafficAvailable: false
      }
    }));

    expect(available).toMatchObject({ upstream: 2048, downstream: 4096 });
    expect(unavailable).toMatchObject({ upstream: null, downstream: null });
  });

  it("uses backend-owned node history for the media-rate chart", () => {
    const response = runtime(2, "2026-08-30T10:00:05.000Z", {
      mediaRateSamples: [
        { sampledAt: Date.parse("2026-08-30T10:00:00.000Z"), upstream: 1024, downstream: 2048 },
        { sampledAt: Date.parse("2026-08-30T10:00:05.000Z"), upstream: 3072, downstream: 4096 }
      ]
    });

    expect(runtimeMediaRateSamples(response)).toEqual(response.mediaRateSamples);
    expect(runtimeMediaRateSamples(runtime())).toEqual([]);
  });

  it("uses backend-owned runtime trends and ignores invalid timestamps", () => {
    const response = runtime(2, "2026-08-30T10:00:05.000Z", {
      trendSamples: [
        { sampledAt: Date.parse("2026-08-30T10:00:00.000Z"), streamCount: 2, viewerCount: 3, throughput: 4096, sessionCount: 5 },
        { sampledAt: Number.NaN, streamCount: 99 }
      ]
    });

    expect(runtimeTrendSamples(response)).toEqual([response.trendSamples[0]]);
  });

});
