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

  it("keeps the stream and session legacy pages as thin panel wrappers", () => {
    for (const file of ["StreamManagement.vue", "SessionManagement.vue"]) {
      const source = readFileSync(resolve(root, file), "utf8");
      expect(source).toContain("workbench/monitoring/");
      expect(source.match(/from \"\.\/workbench\/monitoring\//g)?.length ?? 0).toBeGreaterThan(0);
    }
    expect(readFileSync(resolve(root, "RuntimeOverview.vue"), "utf8")).toContain("/gb28181/zlm/overview");
  });

  it("keeps panel files within the monitoring boundary", () => {
    for (const file of ["RuntimeSummaryPanel.vue", "StreamPanel.vue", "NetworkSessionPanel.vue"]) {
      expect(existsSync(resolve(root, "workbench/monitoring", file))).toBe(true);
    }
  });

  it("flattens streams, network sessions and viewers into one workspace tab row", () => {
    const page = readFileSync(resolve(root, "workbench/MediaMonitoring.vue"), "utf8");
    const panel = readFileSync(resolve(root, "workbench/monitoring/NetworkSessionPanel.vue"), "utf8");
    expect(page).toContain('{ key: "streams", label: "流媒体"');
    expect(page).toContain('{ key: "sessions", label: "网络会话"');
    expect(page).toContain('{ key: "viewers", label: "媒体观看者"');
    expect(panel).toContain('view: "network" | "viewers"');
    expect(panel).not.toContain("<a-tabs");
    expect(panel).not.toContain("<a-tab-pane");
    expect(panel).not.toContain('class="monitoring-panel__header"');
  });

  it("starts the stream view directly with actionable content", () => {
    const panel = readFileSync(resolve(root, "workbench/monitoring/StreamPanel.vue"), "utf8");
    expect(panel).not.toContain('class="monitoring-panel__header"');
    expect(panel).not.toContain("所有列表、详情和危险操作都通过 UVP 后端");
  });

  it("keeps the recording status select at the system filter width", () => {
    const panel = readFileSync(resolve(root, "workbench/monitoring/StreamPanel.vue"), "utf8");
    expect(panel).toContain('style="width: 132px; min-width: 132px; max-width: 132px; flex: 0 0 132px"');
    expect(panel).toMatch(/\.filter-recording\s*\{[^}]*width:\s*132px;[^}]*flex:\s*0 0 132px;/s);
    expect(panel).not.toMatch(/@media\s*\(max-width:\s*900px\)[^{]*\{[^}]*\.filter-recording[^}]*width:\s*100%/s);
  });

  it("uses the shared search-control colors for the recording select", () => {
    const panel = readFileSync(resolve(root, "workbench/monitoring/StreamPanel.vue"), "utf8");
    expect(panel).toMatch(/\.stream-search\s+:deep\(\.arco-select-view\)\s*\{[^}]*background:\s*var\(--uvp-search-control-bg\)\s*!important;/s);
    expect(panel).toMatch(/\.stream-search\s+:deep\(\.arco-select-view-focus\)\s*\{[^}]*box-shadow:\s*var\(--uvp-search-control-focus-shadow\)\s*!important;/s);
  });

  it("passes active scope and node context into every panel without letting panels own route view state", () => {
    const source = readFileSync(resolve(root, "workbench/MediaMonitoring.vue"), "utf8");
    expect(source).toContain(":active=");
    expect(source).toContain(":scope=");
    expect(source).toContain(":node-id=");
    for (const file of ["StreamPanel.vue", "NetworkSessionPanel.vue"]) {
      const panel = readFileSync(resolve(root, "workbench/monitoring", file), "utf8");
      expect(panel).toContain("active: boolean");
      expect(panel).toContain("scope: MediaScope");
      expect(panel).toContain("nodeId: number | null");
      expect(panel).not.toContain("useRoute");
      expect(panel).not.toContain("useRouter");
      expect(panel).toContain("useZLMRuntimePolling");
    }
    const runtimePanel = readFileSync(resolve(root, "workbench/monitoring/RuntimeSummaryPanel.vue"), "utf8");
    expect(runtimePanel).toContain("active: boolean");
    expect(runtimePanel).toContain("nodeId: number | null");
    expect(runtimePanel).not.toContain("scope: MediaScope");
    expect(runtimePanel).not.toContain("useRoute");
    expect(runtimePanel).not.toContain("useRouter");
    expect(runtimePanel).toContain("useZLMRuntimePolling");
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
