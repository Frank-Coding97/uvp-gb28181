import { describe, expect, it } from "vitest";
import type { SecurityEventAggregate } from "@/api/gb28181-security";
import { buildSecurityTrend } from "./securityTrend";

const event = (lastSeenAt: string, action: string, count: number): SecurityEventAggregate => ({
  bucketAt: lastSeenAt,
  sourceIp: "203.0.113.10",
  transport: "UDP",
  method: "INVITE",
  reason: "unknown_invite_rate",
  action,
  count,
  scoreDelta: 20,
  firstSeenAt: lastSeenAt,
  lastSeenAt
});

describe("buildSecurityTrend", () => {
  it("aggregates real security events into detected and banned series", () => {
    const now = Date.parse("2026-08-09T12:00:00Z");
    const result = buildSecurityTrend([
      event("2026-08-09T11:50:00Z", "drop", 4),
      event("2026-08-09T11:50:00Z", "ban", 1)
    ], "1h", now);

    expect(result.hasData).toBe(true);
    expect(result.points.filter(point => point.series === "识别").reduce((sum, point) => sum + point.value, 0)).toBe(5);
    expect(result.points.filter(point => point.series === "封禁").reduce((sum, point) => sum + point.value, 0)).toBe(1);
  });

  it("returns a zero-filled timeline and marks empty periods", () => {
    const result = buildSecurityTrend([], "24h", Date.parse("2026-08-09T12:00:00Z"));

    expect(result.hasData).toBe(false);
    expect(result.points).toHaveLength(24);
    expect(result.points.every(point => point.value === 0)).toBe(true);
  });
});
