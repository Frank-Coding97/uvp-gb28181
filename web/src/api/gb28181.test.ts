import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());

vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("./utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import {
  controlPtzWiper,
  createDeviceSnapshotSession,
  createStreamProbe,
  getControlCapabilities,
  getDeviceStatus,
  getDeviceSnapshotSession,
  getHomePosition,
  getPtzOperation,
  getStreamMonitor,
  getStreamProbeOperation,
  listCruiseTracks,
  listPtzPresets,
  reportPlaybackClientEvent,
  startPlay,
  updateHomePosition,
  type DeviceStatusResult,
  type HomePositionPatch
} from "./gb28181";

describe("国标服务配置 API", () => {
  beforeEach(() => {
    request.mockReset();
    request.mockResolvedValue({ code: 0, message: "", data: { enabled: true, retentionDays: 7 } });
  });

  it("点播请求可关闭公共错误消息", async () => {
    await startPlay("device-1", "channel-1", { silent: true });

    expect(request).toHaveBeenCalledWith("post", "/api/gb28181/play/device-1/channel-1", undefined, { showErrorMessage: false });
  });

  it("客户端播放事实使用独立反馈凭据并静默失败", async () => {
    await reportPlaybackClientEvent("life-1", "feedback-token", {
      event: "first_frame",
      clientElapsedMs: 321
    });

    expect(request).toHaveBeenCalledWith(
      "post",
      "/api/gb28181/play/lifecycles/life-1/client-events",
      {
        data: { event: "first_frame", clientElapsedMs: 321 },
        headers: { "X-Playback-Feedback-Token": "feedback-token" }
      },
      { showErrorMessage: false }
    );
  });

  it("视频探针创建与查询接口都使用固定短超时", async () => {
    await createStreamProbe("stream-1", 60000);

    expect(request).toHaveBeenCalledWith(
      "post",
      "/api/gb28181/stream-probes/stream-1",
      { data: { durationMs: 60000 } },
      { showErrorMessage: false, timeout: 10000 }
    );

    await getStreamProbeOperation("probe-op-1");
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/stream-probes/operations/probe-op-1", undefined, {
      showErrorMessage: false,
      timeout: 10000
    });
  });

  it("使用聚合接口读取并保存全部国标服务配置", async () => {
    const aggregate = { positionHistory: { enabled: true, retentionDays: 7 }, ptzDefaultSpeed: { level: 6 } };
    const { fetchServiceConfig, updateServiceConfig } = await import("./gb28181");
    await fetchServiceConfig();
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip/service-config");
    await updateServiceConfig(aggregate as any);
    expect(request).toHaveBeenLastCalledWith("put", "/api/gb28181/sip/service-config", { data: aggregate });
  });

  it("创建并读取设备图像抓拍任务", async () => {
    await createDeviceSnapshotSession(31, { snapNum: 2, interval: 3 });
    expect(request).toHaveBeenLastCalledWith("post", "/api/gb28181/device-mgmt/channel/31/snapshot-sessions", {
      data: { snapNum: 2, interval: 3 },
      headers: { "Idempotency-Key": expect.stringMatching(/^snapshot-31-/) }
    });

    await getDeviceSnapshotSession(31, "snap/1");
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/device-mgmt/channel/31/snapshot-sessions/snap%2F1");
  });

  it("雨刷走独立路由,只发 on/off(编号 1 由后端钉死)", async () => {
    await controlPtzWiper(12, { action: "on" });
    // ⛔ 路径里没有编号,请求体里也没有 auxiliaryId:标准在 A.3.7 只命名了编号 1 = 雨刷,
    //    编号由后端固定 —— 放开这个入参等于把编号 2~5 那些标准未定义的语义请进 API 面。
    expect(request).toHaveBeenLastCalledWith("post", "/api/gb28181/device-mgmt/channel/12/ptz/wiper", {
      data: { action: "on" }
    });

    await controlPtzWiper(12, { action: "off" });
    expect(request).toHaveBeenLastCalledWith("post", "/api/gb28181/device-mgmt/channel/12/ptz/wiper", {
      data: { action: "off" }
    });
  });
});

describe("gb28181 home position API", () => {
  beforeEach(() => {
    request.mockResolvedValue({ code: 0, message: "", data: {} });
  });

  it("uses cache and explicit refresh URLs without creating an implicit refresh", async () => {
    await getHomePosition(12);
    expect(request).toHaveBeenLastCalledWith(
      "get",
      "/api/gb28181/device-mgmt/channel/12/ptz/home-position",
      { params: undefined },
      { showErrorMessage: false }
    );

    await getHomePosition(12, true, "refresh-key");
    expect(request).toHaveBeenLastCalledWith(
      "get",
      "/api/gb28181/device-mgmt/channel/12/ptz/home-position",
      {
        params: { refresh: true },
        headers: { "Idempotency-Key": "refresh-key" }
      },
      { showErrorMessage: false }
    );
  });

  it("keeps preset zero and sends idempotency only in the header", async () => {
    await updateHomePosition(12, { enabled: true, resetTime: 10, presetId: 0 }, "control-key");

    expect(request).toHaveBeenCalledWith("patch", "/api/gb28181/device-mgmt/channel/12/ptz/home-position", {
      data: { enabled: true, resetTime: 10, presetId: 0 },
      headers: { "Idempotency-Key": "control-key" }
    });
  });

  it("normalizes a disable request to enabled only", async () => {
    await updateHomePosition(12, { enabled: false }, "disable-key");

    expect(request).toHaveBeenCalledWith("patch", "/api/gb28181/device-mgmt/channel/12/ptz/home-position", {
      data: { enabled: false },
      headers: { "Idempotency-Key": "disable-key" }
    });
  });

  it("loads the exact operation for the channel", async () => {
    await getPtzOperation(12, "home-op-1");

    expect(request).toHaveBeenCalledWith("get", "/api/gb28181/device-mgmt/channel/12/ptz/operations/home-op-1", undefined, {
      showErrorMessage: false
    });
  });

  it("exposes a discriminated update payload", () => {
    const enabled: HomePositionPatch = {
      enabled: true,
      resetTime: 10,
      presetId: 0
    };
    const disabled: HomePositionPatch = { enabled: false };
    expect([enabled, disabled]).toHaveLength(2);

    if (false) {
      // @ts-expect-error enabled requests require resetTime and presetId
      updateHomePosition(12, { enabled: true });
      // @ts-expect-error disabled requests cannot carry enabled-only fields
      updateHomePosition(12, { enabled: false, resetTime: 10, presetId: 1 });
    }
  });
});

describe("gb28181 device status API", () => {
  beforeEach(() => {
    request.mockReset();
    request.mockResolvedValue({ code: 0, message: "", data: { freshness: "unknown" } });
  });

  it("keeps DeviceStatus refresh asynchronous and sends the exact channel URL", async () => {
    await getDeviceStatus(12);
    expect(request).toHaveBeenLastCalledWith(
      "get",
      "/api/gb28181/device-mgmt/channel/12/device-status",
      { params: undefined },
      { showErrorMessage: false }
    );

    await getDeviceStatus(12, true);
    expect(request).toHaveBeenLastCalledWith(
      "get",
      "/api/gb28181/device-mgmt/channel/12/device-status",
      { params: { refresh: true } },
      { showErrorMessage: false }
    );
  });

  it("silences all auxiliary reads started by the playback panel", async () => {
    await getStreamMonitor("stream-1");
    await getControlCapabilities(12);
    await listPtzPresets(12);
    await listCruiseTracks(12, true);

    expect(request).toHaveBeenNthCalledWith(1, "get", "/api/gb28181/play/stream-1/monitor", undefined, {
      showErrorMessage: false
    });
    expect(request).toHaveBeenNthCalledWith(2, "get", "/api/gb28181/device-mgmt/channel/12/control-capabilities", undefined, {
      showErrorMessage: false
    });
    expect(request).toHaveBeenNthCalledWith(
      3,
      "get",
      "/api/gb28181/device-mgmt/channel/12/ptz/presets",
      { params: undefined },
      { showErrorMessage: false }
    );
    expect(request).toHaveBeenNthCalledWith(
      4,
      "get",
      "/api/gb28181/device-mgmt/channel/12/ptz/cruise-tracks",
      { params: { refresh: true } },
      { showErrorMessage: false }
    );
  });

  it("exposes independent record and alarm refresh operations and ALARM facts", () => {
    const result: DeviceStatusResult = {
      state: { recordState: "on", guardState: "alarm", freshness: "fresh" },
      recordState: "on",
      guardState: "alarm",
      freshness: "fresh",
      completeness: "complete",
      record: {
        state: "on",
        freshness: "fresh",
        targetScope: "channel",
        targetCode: "C1"
      },
      alarmResolution: {
        status: "resolved",
        source: "direct_parent",
        targetCode: "A1",
        state: "alarm",
        freshness: "fresh",
        candidates: [{ code: "A1", name: "门磁" }]
      },
      alarmFacts: [{ targetCode: "A1", guardState: "alarm", freshness: "fresh" }],
      refreshOperationId: "record-op",
      recordRefreshOperationId: "record-op",
      alarmRefreshOperationId: "alarm-op",
      refreshOperationIds: { record: "record-op", alarm: "alarm-op" }
    };

    expect(result.refreshOperationIds).toEqual({ record: "record-op", alarm: "alarm-op" });
    expect(result.alarmFacts?.[0].guardState).toBe("alarm");
  });

  it("allows DeviceStatus facts to be absent instead of coercing them to false", () => {
    const result: DeviceStatusResult = {
      state: { freshness: "fresh" },
      freshness: "fresh"
    };

    expect(result.state?.recordState).toBeUndefined();
    expect(result.state?.guardState).toBeUndefined();
  });
});
