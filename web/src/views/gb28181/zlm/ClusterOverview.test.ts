import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import type { ZLMOverview, ZLMRuntimeMedia } from "@/api/gb28181-zlm-runtime";

import { overviewHealthSummary, nodeOverviewLocation, streamOverviewLocation } from "./clusterOverviewState";

function overview(options: Partial<ZLMOverview> = {}): ZLMOverview {
  return {
    nodes: [],
    streams: [],
    metrics: {
      sampledNodeCount: 0,
      mediaSourceCount: 0,
      multiMediaSourceMuxerCount: 0,
      tcpServerCount: 0,
      tcpSessionCount: 0,
      udpServerCount: 0,
      udpSessionCount: 0,
      tcpClientCount: 0,
      socketCount: 0,
      networkSessionCount: 0,
      netThreadLoadAvg: 0,
      workThreadLoadAvg: 0,
      streamCount: 0
    },
    partial: false,
    asOf: "2026-08-30T00:00:00Z",
    metricsSampledNodeIds: [],
    mediaSampledNodeIds: [],
    successfulNodeIds: [],
    failedNodeIds: [],
    ...options
  };
}

describe("cluster overview presentation", () => {
  it("distinguishes partial success from a completely unavailable cluster", () => {
    expect(overviewHealthSummary(overview({ partial: true, successfulNodeIds: [1], failedNodeIds: [2] }))).toMatchObject({
      kind: "partial",
      successfulCount: 1,
      failedCount: 1
    });
    expect(overviewHealthSummary(overview({ partial: true, failedNodeIds: [1, 2] }))).toMatchObject({
      kind: "unavailable",
      failedCount: 2
    });
  });

  it("does not call an all-maintenance cluster healthy", () => {
    const maintenanceNode = {
      nodeId: 3,
      name: "maintenance-node",
      state: "maintenance",
      status: "maintenance",
      freshness: "maintenance"
    } as ZLMOverview["nodes"][number];
    expect(overviewHealthSummary(overview({ nodes: [maintenanceNode] }))).toMatchObject({
      kind: "inactive",
      accessibleLabel: expect.stringContaining("1 个维护")
    });
  });

  it("routes node and stream clicks with the complete identity", () => {
    const media = {
      nodeId: 9,
      media: { schema: "rtsp", vhost: "__defaultVhost__", app: "live/main", stream: "cam 01" }
    } as ZLMRuntimeMedia;
    expect(nodeOverviewLocation(9)).toEqual({ path: "/media/nodes/9", query: { view: "overview", nodeId: "9" } });
    expect(streamOverviewLocation(media)).toEqual({
      path: "/media/monitoring",
      query: {
        view: "streams",
        nodeId: "9",
        schema: "rtsp",
        vhost: "__defaultVhost__",
        app: "live/main",
        stream: "cam 01"
      }
    });
  });

  it("keeps the old page as a thin shell over the canonical panel", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/ClusterOverview.vue"), "utf8");
    expect(source).toContain("MediaOverviewPanel");
    expect(source).not.toContain("getZLMOverview");
    expect(source).not.toContain("useZLMRuntimePolling");
  });
});
