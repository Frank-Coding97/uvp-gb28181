import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { createStreamFilters, overviewAsRuntime, sameNodeTargets } from "./monitoring/monitoringState";

const root = resolve(process.cwd(), "src/views/gb28181/zlm");

describe("media monitoring workbench", () => {
  it("composes the canonical monitoring panels instead of a pending placeholder", () => {
    const source = readFileSync(resolve(root, "workbench/MediaMonitoring.vue"), "utf8");
    expect(source).toContain("StreamPanel");
    expect(source).toContain("NetworkSessionPanel");
    expect(source).not.toContain("workspace-pending");
  });

  it("keeps the three legacy monitoring pages as thin panel wrappers", () => {
    for (const file of ["RuntimeOverview.vue", "StreamManagement.vue", "SessionManagement.vue"]) {
      const source = readFileSync(resolve(root, file), "utf8");
      expect(source).toContain("workbench/monitoring/");
      expect(source.match(/from \"\.\/workbench\/monitoring\//g)?.length ?? 0).toBeGreaterThan(0);
    }
  });

  it("keeps panel files within the monitoring boundary", () => {
    for (const file of ["RuntimeSummaryPanel.vue", "StreamPanel.vue", "NetworkSessionPanel.vue"]) {
      expect(existsSync(resolve(root, "workbench/monitoring", file))).toBe(true);
    }
  });

  it("passes active scope and node context into every panel without letting panels own route view state", () => {
    const source = readFileSync(resolve(root, "workbench/MediaMonitoring.vue"), "utf8");
    expect(source).toContain(":active=");
    expect(source).toContain(":scope=");
    expect(source).toContain(":node-id=");
    for (const file of ["RuntimeSummaryPanel.vue", "StreamPanel.vue", "NetworkSessionPanel.vue"]) {
      const panel = readFileSync(resolve(root, "workbench/monitoring", file), "utf8");
      expect(panel).toContain("active: boolean");
      expect(panel).toContain("scope: MediaScope");
      expect(panel).toContain("nodeId: number | null");
      expect(panel).not.toContain("useRoute");
      expect(panel).not.toContain("useRouter");
      expect(panel).toContain("useZLMRuntimePolling");
    }
  });

  it("keeps stream filters and ownership actions scoped to backend evidence", () => {
    expect(createStreamFilters({ recordingMp4: "true", app: " live " })).toMatchObject({ recording: "mp4", app: " live " });
    expect(sameNodeTargets([{ nodeId: 1, media: { schema: "rtsp", vhost: "v", app: "a", stream: "s" } }])).toBe(true);
    expect(sameNodeTargets([
      { nodeId: 1, media: { schema: "rtsp", vhost: "v", app: "a", stream: "s" } },
      { nodeId: 2, media: { schema: "rtsp", vhost: "v", app: "a", stream: "s2" } }
    ])).toBe(false);
  });

  it("keeps aggregate unknown samples unknown instead of fabricating zeroes", () => {
    const runtime = overviewAsRuntime({
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
      partial: true,
      asOf: "2026-08-30T00:00:00Z",
      metricsSampledNodeIds: [],
      mediaSampledNodeIds: [],
      successfulNodeIds: [],
      failedNodeIds: []
    });
    expect(runtime.mediaFreshness).toBe("unavailable");
    expect(runtime.metricsComplete).toBe(false);
    expect(runtime.streams).toBeUndefined();
  });
});
