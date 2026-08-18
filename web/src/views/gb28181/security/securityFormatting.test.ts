import { describe, expect, it } from "vitest";
import { formatAutomaticBanTTL, formatRemaining } from "./securityFormatting";

describe("security formatting", () => {
  it("formats Go duration nanoseconds as a remaining minute", () => {
    const now = Date.parse("2026-08-18T09:00:00Z");
    expect(formatRemaining("2026-08-18T09:00:00Z", 60_000_000_000, now)).toBe("剩余 1 分钟");
  });

  it("formats escalating finite automatic ban steps", () => {
    expect(formatAutomaticBanTTL([
      { score: 100, ttl: 60 },
      { score: 200, ttl: 600 },
      { score: 500, ttl: 3600 }
    ])).toBe("1 分钟起，最高 1 小时");
  });
});
