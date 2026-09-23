import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { buildOverviewChartState, createOverviewDistributionSpec } from "./chart/overviewChart";
import { runtimeSnapshot } from "./chart/runtimeChart";
import { buildSchedulerChartState, createSchedulerNodeSpec } from "./chart/schedulerChart";
import { boundedPageRows, boundedPageSize, WORKBENCH_PAGE_HARD_LIMIT } from "./boundedData";

const LARGE_DTO_COUNT = 20_000;

function largeStreams() {
  return Array.from({ length: LARGE_DTO_COUNT }, (_, index) => ({
    nodeId: index + 1,
    media: { schema: `schema-${index}`, vhost: "v", app: "live", stream: `stream-${index}` },
    online: true,
    aliveSecond: 10,
    bytesSpeed: 1,
    readerCount: 1,
    totalReaderCount: 1,
    originType: index,
    originTypeName: `origin-${index}`,
    recordingMp4: false,
    recordingHls: false,
    trackCount: 1
  }));
}

describe("media workbench large DTO boundaries", () => {
  it("caps malformed paged responses before they reach table DOM", () => {
    const rows = Array.from({ length: LARGE_DTO_COUNT }, (_, id) => ({ id }));
    const startedAt = performance.now();

    expect(boundedPageRows(rows, 20)).toHaveLength(20);
    expect(boundedPageRows(rows, LARGE_DTO_COUNT)).toHaveLength(WORKBENCH_PAGE_HARD_LIMIT);
    expect(boundedPageSize(LARGE_DTO_COUNT, 20)).toBe(WORKBENCH_PAGE_HARD_LIMIT);
    expect(rows).toHaveLength(LARGE_DTO_COUNT);
    expect(performance.now() - startedAt).toBeLessThan(1_000);
  });

  it("keeps overview, runtime, and scheduler chart points bounded for 20k DTOs", () => {
    const streams = largeStreams();
    const startedAt = performance.now();
    const runtimeNode = {
        nodeId: 1,
        name: "节点 1",
        state: "active",
        status: "fresh",
        freshness: "fresh",
        asOf: "2026-08-30T00:00:00Z",
        heartbeatFreshness: "fresh",
        metrics: { mediaSourceCount: LARGE_DTO_COUNT, multiMediaSourceMuxerCount: 0, tcpServerCount: 0, tcpSessionCount: 0, udpServerCount: 0, udpSessionCount: 0, tcpClientCount: 0, socketCount: 0, networkSessionCount: 0, netThreadLoad: 0, workThreadLoad: 0 },
        metricsComplete: true,
        mediaFreshness: "fresh",
        streams
    };
    const overview = buildOverviewChartState({
      nodes: [runtimeNode],
      streams,
      metrics: { sampledNodeCount: 1, mediaSourceCount: LARGE_DTO_COUNT, multiMediaSourceMuxerCount: 0, tcpServerCount: 0, tcpSessionCount: 0, udpServerCount: 0, udpSessionCount: 0, tcpClientCount: 0, socketCount: 0, networkSessionCount: 0, netThreadLoadAvg: 0, workThreadLoadAvg: 0, streamCount: LARGE_DTO_COUNT },
      partial: false,
      asOf: "2026-08-30T00:00:00Z",
      metricsSampledNodeIds: [1],
      mediaSampledNodeIds: [1],
      successfulNodeIds: [1],
      failedNodeIds: [],
      errors: []
    } as any);
    const overviewPoints = createOverviewDistributionSpec(overview).data?.[0]?.values.length ?? 0;

    const runtime = runtimeSnapshot(runtimeNode as any);
    const logs = Array.from({ length: LARGE_DTO_COUNT }, (_, index) => ({
      id: index + 1,
      happenedAt: `2026-08-30T00:00:${String(index % 60).padStart(2, "0")}Z`,
      algorithm: "roundrobin",
      nodeID: index + 1,
      nodeName: `节点 ${index + 1}`,
      streamID: `stream-${index}`,
      deviceID: "device",
      channelID: "channel",
      errorMessage: ""
    })) as any;
    const scheduler = buildSchedulerChartState(logs, { limit: 1_000 });
    const defaultBoundedScheduler = buildSchedulerChartState(logs);
    const schedulerPoints = createSchedulerNodeSpec(scheduler).data?.[0]?.values.length ?? 0;

    expect(overviewPoints).toBeLessThanOrEqual(40);
    expect(runtime.streamCount).toBe(LARGE_DTO_COUNT);
    expect(scheduler.sampleCount).toBeLessThanOrEqual(1_000);
    expect(schedulerPoints).toBeLessThanOrEqual(1_000);
    expect(scheduler.status).toBe("partial");
    expect(scheduler.warning).toContain("1000");
    expect(scheduler.warning).not.toContain("null");
    expect(defaultBoundedScheduler.warning).toContain("1000");
    expect(defaultBoundedScheduler.warning).not.toContain("null");
    expect(performance.now() - startedAt).toBeLessThan(2_000);
  });

  it("routes every paged table through the defensive row bound", () => {
    const files = [
      "monitoring/StreamPanel.vue",
      "monitoring/NetworkSessionPanel.vue",
      "ingress/ProxyPanel.vue",
      "ingress/FFmpegPanel.vue",
      "ingress/RTPPanel.vue",
      "../../cloud-recordings/RecordingWorkspacePanel.vue"
    ];
    for (const file of files) {
      const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench", file), "utf8");
      expect(source, file).toContain("boundedPageRows");
    }
  });

  it("does not turn the aggregated other protocol bucket into a schema filter", () => {
    const source = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/zlm/workbench/overview/MediaOverviewPanel.vue"),
      "utf8"
    );

    expect(source).toContain('protocol !== "其他"');
  });
});
