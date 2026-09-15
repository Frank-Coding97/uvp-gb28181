import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import type { ZLMNodeRuntime } from "@/api/gb28181-zlm-runtime";

import { appendRuntimeSample, runtimeTrendPoint } from "./runtimeOverviewState";

function runtime(index: number): ZLMNodeRuntime {
  return {
    nodeId: 7,
    name: "zlm-a",
    state: "active",
    status: "fresh",
    freshness: "fresh",
    asOf: new Date(Date.UTC(2026, 7, 30, 0, 0, index)).toISOString(),
    heartbeatFreshness: "fresh",
    metrics: {
      mediaSourceCount: index,
      multiMediaSourceMuxerCount: 0,
      tcpServerCount: 1,
      tcpSessionCount: 2,
      udpServerCount: 3,
      udpSessionCount: 4,
      tcpClientCount: 5,
      socketCount: 6,
      networkSessionCount: 7,
      netThreadLoad: 0.25,
      workThreadLoad: 0.5
    },
    metricsComplete: true,
    mediaFreshness: "fresh",
    streams: [
      {
        nodeId: 7,
        media: { schema: "rtsp", vhost: "__defaultVhost__", app: "live", stream: `cam-${index}` },
        online: true,
        aliveSecond: 10,
        bytesSpeed: 1024,
        readerCount: 2,
        totalReaderCount: 3,
        originType: 0,
        recordingMp4: index % 2 === 0,
        recordingHls: false,
        trackCount: 2
      }
    ]
  };
}

describe("runtime overview state", () => {
  it("derives viewers, throughput, FD and recording counts with the sample timestamp", () => {
    expect(runtimeTrendPoint(runtime(2))).toMatchObject({
      streamCount: 1,
      viewerCount: 2,
      throughput: 1024,
      sessionCount: 7,
      fdCount: 6,
      recordingCount: 1,
      asOf: "2026-08-30T00:00:02.000Z"
    });
  });

  it("keeps only the newest 60 samples", () => {
    const history = Array.from({ length: 61 }, (_, index) => runtimeTrendPoint(runtime(index)));
    const next = appendRuntimeSample(history.slice(0, 60), runtime(60));
    expect(next).toHaveLength(60);
    expect(next[0].asOf).toBe("2026-08-30T00:00:01.000Z");
    expect(next.at(-1)?.asOf).toBe("2026-08-30T00:01:00.000Z");
  });

  it("uses typed polling and keeps a single-node operational overview", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/monitoring/RuntimeSummaryPanel.vue"), "utf8");
    expect(source).toContain("getZLMNodeRuntime");
    expect(source).toContain("useZLMRuntimePolling");
    expect(source).not.toContain("getZLMOverview");
    expect(source).not.toContain("scope === 'all'");
    expect(source).not.toContain("title=\"在线节点\"");
    expect(source).not.toContain("title=\"异常节点\"");
    expect(source.match(/<StatCard/g)).toHaveLength(6);
    expect(source).not.toContain("文件描述符 / Socket");
    expect(source).not.toContain("WorkThread 负载");
    expect(source).toContain("实时媒体速率");
    expect(source).toContain('legend-label="媒体速率（KB/s）"');
    expect(source).toContain(":show-summary=\"false\"");
    expect(source).not.toContain(':as-of="chart.asOf"');
    expect(source).not.toContain("实时吞吐趋势");
    expect(source).toContain("事件线程负载");
    expect(source).toContain("thread-heatmap");
    expect(source).toContain("平均负载");
    expect(source).toContain("最高负载");
    expect(source).toContain("高负载线程");
    expect(source).toContain("对象实例");
    expect(source).toContain("objectStatisticItems");
    expect(source).toContain("objectStatisticHistory");
    expect(source).toContain("object-stat-sparkline");
    expect(source).toContain("查看全部对象");
    expect(source).toContain("媒体源");
    expect(source).toContain("网络套接字");
    expect(source).toContain("runtime-object-panel--summary");
    expect(source).toContain("runtime-thread-panel--full");
    expect(source).toContain("thread-load-distribution");
    expect(source).toContain("热点线程排行");
    expect(source).not.toContain("节点健康");
    expect(source).not.toContain("selectNode");
    expect(source).not.toContain("monitoring-panel__header");
    expect(source).not.toContain("运行态采样</div>");
    expect(source).not.toContain("当前媒体采样");
    expect(source).toContain("drilldown");
    expect(source).toContain("align-items: stretch");
    expect(source).not.toContain(".media-vchart { min-height: 100%; }");
    expect(source).not.toMatch(/font-size:\s*(?:8|9|10|11)px/);
    expect(source).not.toContain("color: var(--zlm-text-4)");
    const chartSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/components/MediaVChart.vue"), "utf8");
    expect(chartSource).not.toMatch(/font-size:\s*11px/);
    expect(chartSource).not.toContain("var(--zlm-text-4, var(--color-text-3))");
    const retiredSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/RuntimeOverview.vue"), "utf8");
    expect(retiredSource).toContain("router.replace");
    expect(retiredSource).toContain("/gb28181/zlm/overview");
    expect(retiredSource).not.toContain("getZLMNodeRuntime");
  });

  it("plots only the aggregated media rate on a single-unit axis", async () => {
    const { createRuntimeTrendSpec } = await import("./workbench/chart/runtimeChart");
    const spec = createRuntimeTrendSpec([
      {
        nodeId: 7,
        asOf: "2026-08-30T00:00:00.000Z",
        streamCount: 1,
        viewerCount: 2,
        throughput: 2048,
        sessionCount: 3,
        netThreadLoad: 0.2,
        workThreadLoad: 0.3,
        fdCount: 4,
        recordingCount: 0,
        mediaKnown: true,
        metricsKnown: true
      }
    ]);
    const values = spec.data?.[0]?.values ?? [];
    expect(values).toEqual([{
      sampledAt: Date.parse("2026-08-30T00:00:00.000Z"),
      asOf: "2026-08-30T00:00:00.000Z",
      metric: "媒体速率 KB/s",
      value: 2
    }]);
    expect(spec.axes?.[0]).toMatchObject({ title: { text: "KB/s" } });
    expect(spec.axes?.[1]).toMatchObject({ type: "time", layers: [{ timeFormat: "%H:%M:%S" }] });
  });
});
