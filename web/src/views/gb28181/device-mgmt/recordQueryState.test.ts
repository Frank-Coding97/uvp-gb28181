import dayjs from "dayjs";
import utc from "dayjs/plugin/utc";
import timezone from "dayjs/plugin/timezone";
import { describe, expect, it } from "vitest";
import type { RecordQueryItem, RecordQueryOptions } from "./api";
import {
  createDefaultRecordQueryForm,
  mapRecordQueryError,
  paginateRecordQueryItems,
  recordQueryOptionsPath,
  recordQueryPath,
  recordQueryTimeoutMs,
  readableHttpError,
  recordQueryTypeText,
  serializeRecordQueryForm,
  sortRecordQueryItems,
  validateRecordQueryForm
} from "./recordQueryState";

dayjs.extend(utc);
dayjs.extend(timezone);

const options: RecordQueryOptions = {
  device: { id: 7, code: "34020000002000000001", name: "园区 NVR", online: true },
  channel: { id: 31, code: "34020000001320000001", name: "东门通道" },
  timezone: "Asia/Shanghai",
  serverNow: "2026-08-02T19:40:20+08:00",
  maxRangeHours: 24,
  timeoutSeconds: 15,
  supportedTypes: ["all", "manual", "alarm"]
};

describe("record query contract helpers", () => {
  it("builds the locked channel URLs", () => {
    expect(recordQueryOptionsPath(31)).toBe("gb28181/device-mgmt/channel/31/record-query/options");
    expect(recordQueryPath(31)).toBe("gb28181/device-mgmt/channel/31/record-query");
  });

  it("creates today range in the platform timezone without a UTC conversion", () => {
    expect(createDefaultRecordQueryForm(options)).toEqual({
      startTime: "2026-08-02T00:00:00",
      endTime: "2026-08-02T19:40:20",
      type: "all",
      secrecy: 0,
      recorderId: ""
    });
  });

  it("serializes only the approved request fields", () => {
    const form = { ...createDefaultRecordQueryForm(options), deviceId: "forbidden", timezone: "UTC" };
    expect(serializeRecordQueryForm(form)).toEqual({
      startTime: "2026-08-02T00:00:00",
      endTime: "2026-08-02T19:40:20",
      type: "all",
      secrecy: 0,
      recorderId: ""
    });
  });

  it("rejects inverted, oversized and non-standard queries before sending", () => {
    expect(
      validateRecordQueryForm(
        {
          startTime: "2026-08-02T20:00:00",
          endTime: "2026-08-02T19:00:00",
          type: "all",
          secrecy: 0,
          recorderId: ""
        },
        options
      )
    ).toMatchObject({ endTime: expect.any(String) });
    expect(
      validateRecordQueryForm(
        {
          startTime: "2026-08-01T00:00:00",
          endTime: "2026-08-02T19:00:00",
          type: "manual",
          secrecy: 0,
          recorderId: ""
        },
        options
      )
    ).toMatchObject({ endTime: expect.stringContaining("24") });
    expect(
      validateRecordQueryForm(
        {
          startTime: "2026-08-02T00:00:00",
          endTime: "2026-08-02T19:00:00",
          type: "time" as never,
          secrecy: -1,
          recorderId: ""
        },
        options
      )
    ).toMatchObject({ type: expect.any(String), secrecy: expect.any(String) });
  });

  it.each([
    ["record_query_device_offline", "offline"],
    ["record_query_timeout", "timeout"],
    ["record_query_busy", "error"],
    ["record_query_send_failed", "error"]
  ])("maps %s to a stable UI state", (errorCode, state) => {
    expect(mapRecordQueryError({ response: { data: { message: "后端消息", data: { errorCode } } } })).toMatchObject({ state });
  });
});

describe("record query result helpers", () => {
  const item = (name: string, startTime: string | null): RecordQueryItem => ({
    recordKey: `record-${name}`,
    deviceId: "34020000001320000001",
    name,
    filePath: null,
    address: null,
    startTime,
    endTime: null,
    secrecy: 0,
    type: "time",
    recorderId: null,
    fileSize: null,
    recordLocation: null,
    streamNumber: null
  });

  it("sorts known starts first and keeps equal or unknown items stable", () => {
    const sorted = sortRecordQueryItems([
      item("unknown-a", null),
      item("late", "2026-08-02T11:00:00+08:00"),
      item("early-a", "2026-08-02T09:00:00+08:00"),
      item("early-b", "2026-08-02T09:00:00+08:00"),
      item("unknown-b", null)
    ]);
    expect(sorted.map(row => row.name)).toEqual(["early-a", "early-b", "late", "unknown-a", "unknown-b"]);
  });

  it("paginates locally and presents only standard display types", () => {
    expect(
      paginateRecordQueryItems(
        Array.from({ length: 45 }, (_, index) => item(String(index + 1), null)),
        2,
        20
      )
    ).toHaveLength(20);
    expect(recordQueryTypeText("time")).toBe("定时录像");
    expect(recordQueryTypeText("vendor-x")).toBe("未知类型");
    expect(recordQueryTypeText(null)).toBe("未知类型");
  });
});

describe("readableHttpError", () => {
  it("axios 英文技术串一律换成中文，并按状态码给出可行动的原因", () => {
    // 关键回归：这串英文以前会原样出现在「录像段」区域里。
    expect(mapRecordQueryError({ message: "Request failed with status code 404", response: { status: 404 } }).message).toBe(
      "通道不存在或无权访问"
    );
    expect(readableHttpError({ message: "Network Error" }, "兜底")).toContain("网络连接异常");
    expect(readableHttpError({ response: { status: 503 } }, "兜底")).toBe("服务暂不可用，请稍后重试");
    expect(readableHttpError({ code: "ECONNABORTED", message: "timeout of 15000ms exceeded" }, "兜底")).toContain("请求超时");
  });

  it("后端中文文案优先，专属错误码说明次之", () => {
    expect(
      mapRecordQueryError({ response: { data: { message: "设备未响应", data: { errorCode: "record_query_timeout" } } } }).message
    ).toBe("设备未响应");
    expect(mapRecordQueryError({ response: { data: { data: { errorCode: "record_query_device_offline" } } } }).message).toBe(
      "设备当前离线，无法查询录像"
    );
  });

  // 前端请求超时必须严格长于后端聚合时限：两者都是 15 秒时实测后端 15015ms 才写出
  // 「部分结果」，前端 15000ms 就断了，用户永远看不到已经查到的录像段。
  describe("recordQueryTimeoutMs", () => {
    it("在后端时限之上多留 5 秒余量", () => {
      expect(recordQueryTimeoutMs({ timeoutSeconds: 15 })).toBe(20000);
      expect(recordQueryTimeoutMs({ timeoutSeconds: 30 })).toBe(35000);
    });

    it("后端时限缺失或非法时按 15 秒兜底", () => {
      expect(recordQueryTimeoutMs(null)).toBe(20000);
      expect(recordQueryTimeoutMs(undefined)).toBe(20000);
      expect(recordQueryTimeoutMs({ timeoutSeconds: 0 })).toBe(20000);
      expect(recordQueryTimeoutMs({ timeoutSeconds: Number.NaN })).toBe(20000);
    });

    it("必须严格大于后端时限，否则后端结论送不出去", () => {
      for (const seconds of [5, 15, 30, 60]) {
        expect(recordQueryTimeoutMs({ timeoutSeconds: seconds })).toBeGreaterThan(seconds * 1000);
      }
    });
  });
});
