import { describe, expect, it } from "vitest";
import { DEVICE_STATUS_LABEL_FALLBACK, DICT_CODE_DEVICE_STATUS, deviceStatusKey, deviceStatusLabelFrom } from "./deviceStatus";

describe("device_status 字典（设备/通道在线状态）", () => {
  it("code 与兜底值域锁死（1=在线 / 0=离线 的展示口径）", () => {
    expect(DICT_CODE_DEVICE_STATUS).toBe("device_status");
    expect(DEVICE_STATUS_LABEL_FALLBACK).toEqual({ online: "在线", offline: "离线" });
  });

  it("三种形状归一化到同一把尺子：boolean / number / string", () => {
    for (const value of [true, 1, "1", "online", "ONLINE", " true "]) {
      expect(deviceStatusKey(value), `${String(value)} 应判在线`).toBe("online");
    }
    for (const value of [false, 0, "0", "offline", "OFFLINE", " false "]) {
      expect(deviceStatusKey(value), `${String(value)} 应判离线`).toBe("offline");
    }
  });

  it("认不出来的值返回空键（不猜成在线或离线）", () => {
    for (const value of [undefined, null, "", "   ", "unknown", NaN, {}, [], 2, -1]) {
      expect(deviceStatusKey(value), `${String(value)} 不该被猜出极性`).toBe("");
    }
  });

  it("字典缺失时用兜底常量", () => {
    expect(deviceStatusLabelFrom({}, true)).toBe("在线");
    expect(deviceStatusLabelFrom(undefined, false)).toBe("离线");
  });

  it("字典命中时以字典为准（现场改名界面跟着变）", () => {
    const labels = { online: "在网", offline: "脱网" };
    expect(deviceStatusLabelFrom(labels, true)).toBe("在网");
    expect(deviceStatusLabelFrom(labels, 0)).toBe("脱网");
  });

  it("未知值走 unknownText（默认空串，设备自报场景传「未上报」）", () => {
    expect(deviceStatusLabelFrom({}, undefined)).toBe("");
    expect(deviceStatusLabelFrom({}, undefined, "未上报")).toBe("未上报");
    expect(deviceStatusLabelFrom({}, "weird", "未上报")).toBe("未上报");
  });
});
