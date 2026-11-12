import { describe, expect, it } from "vitest";
import {
  coverageLabel,
  downstreamAllMeasured,
  formatBytes,
  latestRequestGuard,
  realtimeDownstreamSpeed,
  summarizeRealtime
} from "./trafficState";

describe("device traffic state", () => {
  it("distinguishes zero from missing history", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(coverageLabel("not_started")).toBe("尚未开始统计");
    expect(coverageLabel("partial")).toBe("存在采集缺口");
  });

  it("rejects stale request versions", () => {
    const guard = latestRequestGuard();
    const first = guard.next();
    const second = guard.next();
    expect(guard.current(first)).toBe(false);
    expect(guard.current(second)).toBe(true);
    guard.cancel();
    expect(guard.current(second)).toBe(false);
  });

  it("prefers measured downstream speed and never mixes it with estimates", () => {
    // 实测与估算绝不能混加:同一条流要么走实测,要么走估算回退
    expect(
      realtimeDownstreamSpeed({
        upstreamBytesPerSecond: 10,
        estimatedDownstreamBytesPerSec: 999,
        downstreamMeasured: true,
        downstreamBytesPerSecond: 42
      })
    ).toBe(42);
    expect(realtimeDownstreamSpeed({ upstreamBytesPerSecond: 10, estimatedDownstreamBytesPerSec: 999 })).toBe(999);
    expect(
      realtimeDownstreamSpeed({ upstreamBytesPerSecond: 10, estimatedDownstreamBytesPerSec: 999, downstreamMeasured: true })
    ).toBe(0);
  });

  it("summarizes realtime across mixed measured/estimated streams", () => {
    const summary = summarizeRealtime([
      { upstreamBytesPerSecond: 10, estimatedDownstreamBytesPerSec: 20, downstreamMeasured: true, downstreamBytesPerSecond: 33 },
      { upstreamBytesPerSecond: 5, estimatedDownstreamBytesPerSec: 7 }
    ]);
    expect(summary).toEqual({ speed: 15, downstreamSpeed: 40 });
    // 只要有一条流是估算口径,整体说明就必须保留「估算」标注
    expect(
      downstreamAllMeasured([
        { upstreamBytesPerSecond: 1, estimatedDownstreamBytesPerSec: 0, downstreamMeasured: true },
        { upstreamBytesPerSecond: 1, estimatedDownstreamBytesPerSec: 0 }
      ])
    ).toBe(false);
    expect(
      downstreamAllMeasured([
        { upstreamBytesPerSecond: 1, estimatedDownstreamBytesPerSec: 0, downstreamMeasured: true },
        { upstreamBytesPerSecond: 1, estimatedDownstreamBytesPerSec: 0, downstreamMeasured: true, downstreamBytesPerSecond: 2 }
      ])
    ).toBe(true);
    expect(downstreamAllMeasured([])).toBe(false);
  });
});
