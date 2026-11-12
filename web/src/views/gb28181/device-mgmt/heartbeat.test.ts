import { describe, expect, it } from "vitest";
import type { TraceMessageSummary } from "@/api/gb28181-trace";
import {
  buildHeartbeatRows,
  classifyHeartbeat,
  formatDuration,
  heartbeatDriftWarning,
  heartbeatWindowRange,
  summarizeHeartbeats
} from "./heartbeat";

function message(overrides: Partial<TraceMessageSummary> = {}): TraceMessageSummary {
  return {
    eventId: "event",
    occurredAt: "2026-11-12T10:00:00.000Z",
    direction: "inbound",
    transport: "UDP",
    localAddr: "10.0.0.1:51064",
    remoteAddr: "182.33.246.128:36159",
    deviceId: "37010301021180000010",
    method: "MESSAGE",
    statusCode: 0,
    callId: "call-a",
    cseq: 1,
    cseqMethod: "MESSAGE",
    businessCode: "keepalive",
    businessType: "心跳保持",
    malformed: false,
    ...overrides
  };
}

describe("心跳记录整形", () => {
  it("把一问一答配成一行，并算出应答耗时", () => {
    const rows = buildHeartbeatRows(
      [message({ occurredAt: "2026-11-12T10:00:58.000Z" })],
      [message({ direction: "outbound", statusCode: 200, occurredAt: "2026-11-12T10:00:58.120Z" })]
    );
    expect(rows).toHaveLength(1);
    expect(rows[0].kind).toBe("ok");
    expect(rows[0].latencyMs).toBe(120);
    expect(rows[0].resultCode).toBe(200);
  });

  it("403 单独成一档：它是「注册过期」而不是普通拒收", () => {
    expect(classifyHeartbeat(403, true)).toBe("registration_expired");
    expect(classifyHeartbeat(480, true)).toBe("rejected");
    expect(classifyHeartbeat(0, false)).toBe("no_response");
    expect(classifyHeartbeat(200, true)).toBe("ok");
  });

  it("没有应答的心跳不得被当成正常", () => {
    const rows = buildHeartbeatRows([message({ callId: "call-b" })], []);
    expect(rows[0].kind).toBe("no_response");
    expect(rows[0].latencyMs).toBeNull();
    expect(summarizeHeartbeats(rows).abnormal).toBe(1);
  });

  it("刚落库、应答还没写进去的心跳标为待确认，不算异常", () => {
    const now = new Date("2026-11-12T10:02:00.000Z").getTime();
    const rows = buildHeartbeatRows([message({ occurredAt: "2026-11-12T10:01:59.000Z" })], [], null, now);
    expect(rows[0].kind).toBe("pending_response");
    expect(summarizeHeartbeats(rows).abnormal).toBe(0);
  });

  it("同一 Call-ID 有多条应答时取最早那条（重传不算新心跳）", () => {
    const rows = buildHeartbeatRows(
      [message({ occurredAt: "2026-11-12T10:00:58.000Z" })],
      [
        message({ direction: "outbound", statusCode: 200, occurredAt: "2026-11-12T10:00:58.400Z" }),
        message({ direction: "outbound", statusCode: 200, occurredAt: "2026-11-12T10:00:58.100Z" })
      ]
    );
    expect(rows[0].latencyMs).toBe(100);
  });

  it("间隔跟「更早一条」比，列表按时间倒序", () => {
    const rows = buildHeartbeatRows(
      [
        message({ callId: "newer", occurredAt: "2026-11-12T10:02:00.000Z" }),
        message({ callId: "older", occurredAt: "2026-11-12T10:01:00.000Z" })
      ],
      [],
      "2026-11-12T10:00:00.000Z"
    );
    expect(rows.map(row => row.key)).toEqual(["newer", "older"]);
    expect(rows[0].intervalMs).toBe(60_000);
    expect(rows[1].intervalMs).toBe(60_000);
  });

  it("首屏最早一条没有可比的更早记录时为 null，不编造间隔", () => {
    const rows = buildHeartbeatRows([message()], []);
    expect(rows[0].intervalMs).toBeNull();
  });

  it("汇总只统计已加载的部分，异常口径与表格一致", () => {
    const rows = buildHeartbeatRows(
      [
        message({ callId: "b", occurredAt: "2026-11-12T10:01:00.000Z" }),
        message({ callId: "a", occurredAt: "2026-11-12T10:00:00.000Z" })
      ],
      [
        message({ direction: "outbound", statusCode: 403, occurredAt: "2026-11-12T10:01:00.050Z" }),
        message({ callId: "a", direction: "outbound", statusCode: 200, occurredAt: "2026-11-12T10:00:00.050Z" })
      ]
    );
    const summary = summarizeHeartbeats(rows);
    expect(summary.total).toBe(2);
    expect(summary.abnormal).toBe(1);
    expect(summary.latestAt).toBe("2026-11-12T10:01:00.000Z");
    expect(summary.averageIntervalMs).toBe(60_000);
  });
});

describe("心跳时间窗", () => {
  it("按选择换算成后端要的 RFC3339 区间", () => {
    const range = heartbeatWindowRange("24h", new Date("2026-11-12T12:00:00.000Z"));
    expect(range.to).toBe("2026-11-12T12:00:00.000Z");
    expect(range.from).toBe("2026-11-11T12:00:00.000Z");
  });

  it("7 天窗口不超过后端 8 天上的上限", () => {
    const range = heartbeatWindowRange("7d", new Date("2026-11-12T12:00:00.000Z"));
    const span = new Date(range.to).getTime() - new Date(range.from).getTime();
    expect(span).toBeLessThanOrEqual(8 * 24 * 60 * 60 * 1000);
  });
});

describe("「收到了却没算数」判据", () => {
  it("报文时间明显新于平台记账时间时给出提示", () => {
    const warning = heartbeatDriftWarning("2026-11-12T10:30:00.000Z", "2026-11-12T09:50:00.000Z", 60_000);
    expect(warning).toContain("没有计入心跳");
  });

  it("差异在一个心跳周期内属于正常抖动，不提示", () => {
    expect(heartbeatDriftWarning("2026-11-12T10:00:30.000Z", "2026-11-12T10:00:00.000Z", 60_000)).toBeNull();
  });

  it("平台从没记过心跳（空值）时不误报", () => {
    expect(heartbeatDriftWarning("2026-11-12T10:00:00.000Z", null, 60_000)).toBeNull();
  });
});

describe("间隔可读化", () => {
  it("按量级选单位", () => {
    expect(formatDuration(800)).toBe("800 毫秒");
    expect(formatDuration(5_000)).toBe("5 秒");
    expect(formatDuration(65_000)).toBe("1 分 05 秒");
    expect(formatDuration(3_600_000)).toBe("1 小时");
    expect(formatDuration(null)).toBe("-");
  });
});
