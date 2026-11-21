import { computed, ref } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { RecordQueryOptions } from "../../gb28181/device-mgmt/api";
import { useDayRecords } from "./useDayRecords";

const api = vi.hoisted(() => ({ queryDeviceRecords: vi.fn() }));
vi.mock("../../gb28181/device-mgmt/api", () => api);

function queryOptions(serverNow = "2026-11-20T16:00:00"): RecordQueryOptions {
  return {
    device: { id: 1, code: "34020000001110000001", name: "东门设备", online: true },
    channel: { id: 11, code: "34020000001320000001", name: "东门出入口" },
    timezone: "Asia/Shanghai",
    serverNow,
    maxRangeHours: 24,
    timeoutSeconds: 15,
    supportedTypes: ["all", "manual", "alarm"]
  };
}

function record(startTime: string, endTime: string, recordKey = `${startTime}|${endTime}`) {
  return {
    recordKey,
    deviceId: "34020000001320000001",
    name: null,
    filePath: null,
    address: null,
    startTime,
    endTime,
    secrecy: 0,
    type: "time",
    recorderId: null,
    fileSize: null,
    recordLocation: null,
    streamNumber: null
  };
}

function setup() {
  const channelId = ref(11);
  const dayRecords = useDayRecords({ channelId, canQuery: computed(() => true) });
  return { channelId, dayRecords };
}

describe("按天查询录像", () => {
  beforeEach(() => {
    api.queryDeviceRecords.mockReset();
  });

  it("按整天发起查询，并把结果按时间排序", async () => {
    api.queryDeviceRecords.mockResolvedValue({
      code: 0,
      data: {
        status: "complete",
        declaredTotal: 2,
        receivedCount: 2,
        incomplete: false,
        timezone: "Asia/Shanghai",
        elapsedMs: 120,
        list: [record("2026-11-20T14:00:00", "2026-11-20T15:00:00"), record("2026-11-20T09:00:00", "2026-11-20T10:00:00")]
      }
    });
    const { dayRecords } = setup();

    await dayRecords.load("2026-11-20", queryOptions());

    const [channel, payload] = api.queryDeviceRecords.mock.calls[0];
    expect(channel).toBe(11);
    expect(payload).toMatchObject({ startTime: "2026-11-20T00:00:00", endTime: "2026-11-20T16:00:00", type: "all" });
    expect(dayRecords.records.value.map(item => item.startTime)).toEqual(["2026-11-20T09:00:00", "2026-11-20T14:00:00"]);
    expect(dayRecords.state.value).toBe("complete");
  });

  it("查过的日期再次打开直接命中缓存，不再打扰设备", async () => {
    api.queryDeviceRecords.mockResolvedValue({
      code: 0,
      data: {
        status: "complete",
        declaredTotal: 1,
        receivedCount: 1,
        incomplete: false,
        timezone: "Asia/Shanghai",
        elapsedMs: 90,
        list: [record("2026-11-20T09:00:00", "2026-11-20T10:00:00")]
      }
    });
    const { dayRecords } = setup();

    await dayRecords.load("2026-11-20", queryOptions());
    await dayRecords.load("2026-11-20", queryOptions());

    expect(api.queryDeviceRecords).toHaveBeenCalledTimes(1);
    expect(dayRecords.records.value).toHaveLength(1);
  });

  it("force 会绕过缓存重新查", async () => {
    api.queryDeviceRecords.mockResolvedValue({
      code: 0,
      data: {
        status: "empty",
        declaredTotal: 0,
        receivedCount: 0,
        incomplete: false,
        timezone: "Asia/Shanghai",
        elapsedMs: 80,
        list: []
      }
    });
    const { dayRecords } = setup();

    await dayRecords.load("2026-11-20", queryOptions());
    await dayRecords.load("2026-11-20", queryOptions(), { force: true });

    expect(api.queryDeviceRecords).toHaveBeenCalledTimes(2);
  });

  it("查询失败时保留上一次的结果并标记 stale，而不是清空", async () => {
    api.queryDeviceRecords.mockResolvedValueOnce({
      code: 0,
      data: {
        status: "complete",
        declaredTotal: 1,
        receivedCount: 1,
        incomplete: false,
        timezone: "Asia/Shanghai",
        elapsedMs: 90,
        list: [record("2026-11-20T09:00:00", "2026-11-20T10:00:00")]
      }
    });
    const { dayRecords } = setup();
    await dayRecords.load("2026-11-20", queryOptions());

    api.queryDeviceRecords.mockRejectedValueOnce({
      errorCode: "record_query_device_offline",
      message: "device offline"
    });
    await dayRecords.load("2026-11-21", queryOptions());

    expect(dayRecords.records.value).toHaveLength(1);
    expect(dayRecords.stale.value).toBe(true);
    expect(dayRecords.state.value).toBe("offline");
    expect(dayRecords.message.value).toContain("离线");
  });

  it("确认没有录像时清空并给出 empty 状态", async () => {
    api.queryDeviceRecords.mockResolvedValue({
      code: 0,
      data: {
        status: "empty",
        declaredTotal: 0,
        receivedCount: 0,
        incomplete: false,
        timezone: "Asia/Shanghai",
        elapsedMs: 80,
        list: []
      }
    });
    const { dayRecords } = setup();

    await dayRecords.load("2026-11-20", queryOptions());

    expect(dayRecords.records.value).toHaveLength(0);
    expect(dayRecords.state.value).toBe("empty");
    expect(dayRecords.stale.value).toBe(false);
  });

  it("没有查询权限时一次请求都不发", async () => {
    const channelId = ref(11);
    const dayRecords = useDayRecords({ channelId, canQuery: computed(() => false) });

    await dayRecords.load("2026-11-20", queryOptions());

    expect(api.queryDeviceRecords).not.toHaveBeenCalled();
  });

  it("clear 之后缓存也一并失效", async () => {
    api.queryDeviceRecords.mockResolvedValue({
      code: 0,
      data: {
        status: "empty",
        declaredTotal: 0,
        receivedCount: 0,
        incomplete: false,
        timezone: "Asia/Shanghai",
        elapsedMs: 80,
        list: []
      }
    });
    const { dayRecords } = setup();
    await dayRecords.load("2026-11-20", queryOptions());
    dayRecords.clear();
    await dayRecords.load("2026-11-20", queryOptions());

    expect(api.queryDeviceRecords).toHaveBeenCalledTimes(2);
  });
});
