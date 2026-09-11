import { describe, expect, it } from "vitest";

import {
  formatZLMByteRate,
  formatZLMBytes,
  formatZLMDuration,
  formatZLMProtocol,
  zlmErrorPresentation,
  zlmFreshnessPresentation
} from "./zlmFormatters";

describe("ZLM shared formatters", () => {
  it("formats byte, rate, duration and protocol values consistently", () => {
    expect(formatZLMBytes(0)).toBe("0 B");
    expect(formatZLMBytes(1536)).toBe("1.5 KB");
    expect(formatZLMByteRate(1_048_576)).toBe("1 MB/s");
    expect(formatZLMDuration(3661)).toBe("1小时 1分 1秒");
    expect(formatZLMProtocol("http-flv")).toBe("HTTP-FLV");
    expect(formatZLMProtocol("webrtc")).toBe("WebRTC");
  });

  it("returns explicit freshness text in addition to a visual tone", () => {
    const now = Date.parse("2026-08-30T00:00:30Z");
    expect(zlmFreshnessPresentation("2026-08-30T00:00:25Z", now)).toMatchObject({ label: "实时", tone: "success" });
    expect(zlmFreshnessPresentation("2026-08-30T00:00:00Z", now)).toMatchObject({ label: "延迟 30 秒", tone: "warning" });
    expect(zlmFreshnessPresentation(null, now)).toMatchObject({ label: "暂无采样", tone: "neutral" });
  });

  it("maps status/code without exposing raw backend error text", () => {
    expect(zlmErrorPresentation({ response: { status: 503, data: { message: "secret=http://internal" } } })).toMatchObject({
      label: "媒体节点暂不可用",
      retryable: true
    });
    expect(zlmErrorPresentation({ response: { status: 409 } })).toMatchObject({ label: "目标状态已变化，请重新确认" });
    expect(zlmErrorPresentation(new Error("apiSecret=hidden"))).toMatchObject({ label: "请求失败，请稍后重试" });
  });
});
