import { describe, expect, it } from "vitest";

import {
  busiestEventThreads,
  eventThreadHeatmapColumns,
  eventThreadIndex,
  eventThreadLoadDistribution,
  eventThreadTone,
  summarizeEventThreadLoads
} from "./threadLoadState";

describe("event thread load presentation", () => {
  it("summarizes 128 event pollers without dropping the outlier", () => {
    const loads = Array.from({ length: 128 }, (_, index) => ({
      name: `event poller ${index}`,
      load: index === 127 ? 90 : 10,
      fdCount: 17
    }));

    expect(summarizeEventThreadLoads(loads)).toEqual({
      count: 128,
      average: 11,
      peak: 90,
      highCount: 1
    });
  });

  it("uses stable warning thresholds and compact thread indexes", () => {
    expect(eventThreadTone(49)).toBe("normal");
    expect(eventThreadTone(50)).toBe("warning");
    expect(eventThreadTone(80)).toBe("danger");
    expect(eventThreadIndex({ name: "event poller 127", load: 0, fdCount: 0 }, 3)).toBe("127");
    expect(eventThreadIndex({ name: "custom worker", load: 0, fdCount: 0 }, 3)).toBe("3");
  });

  it("builds a compact distribution and ranks the busiest threads", () => {
    const loads = [
      { name: "event poller 0", load: 12, fdCount: 4 },
      { name: "event poller 1", load: 82, fdCount: 8 },
      { name: "event poller 2", load: 55, fdCount: 6 },
      { name: "event poller 3", load: 110, fdCount: 9 }
    ];

    expect(eventThreadLoadDistribution(loads)).toEqual([
      { key: "normal", label: "正常", range: "0–49%", count: 1, ratio: 25 },
      { key: "warning", label: "关注", range: "50–79%", count: 1, ratio: 25 },
      { key: "danger", label: "高负载", range: "80–100%", count: 2, ratio: 50 }
    ]);
    expect(busiestEventThreads(loads, 3).map(thread => thread.name)).toEqual([
      "event poller 3",
      "event poller 1",
      "event poller 2"
    ]);
  });

  it("adapts heatmap density for 16 through 128 threads", () => {
    expect(eventThreadHeatmapColumns(16)).toBe(16);
    expect(eventThreadHeatmapColumns(32)).toBe(16);
    expect(eventThreadHeatmapColumns(64)).toBe(24);
    expect(eventThreadHeatmapColumns(128)).toBe(32);
  });
});
