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

  it("uses typed node polling and exposes stream/session drill-downs", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/monitoring/RuntimeSummaryPanel.vue"), "utf8");
    expect(source).toContain("getZLMNodeRuntime");
    expect(source).toContain("useZLMRuntimePolling");
    expect(source).toContain("文件描述符");
    expect(source).toContain("drilldown");
    expect(readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/RuntimeOverview.vue"), "utf8")).toContain("ZLMNodeContextBar");
  });
});
