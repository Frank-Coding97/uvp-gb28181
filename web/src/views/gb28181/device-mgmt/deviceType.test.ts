import { describe, expect, it } from "vitest";
import {
  DEFAULT_DEVICE_TYPE,
  DEVICE_TYPE_FILTER_OPTIONS,
  DEVICE_TYPE_OPTIONS,
  DEVICE_TYPES,
  deviceTypeLabel,
  deviceTypeShortLabel,
  normalizeDeviceType
} from "./deviceType";

describe("设备类型（ipc / nvr / platform）", () => {
  it("值域与默认值锁死：默认 IPC", () => {
    expect(DEVICE_TYPES).toEqual(["ipc", "nvr", "platform"]);
    expect(DEFAULT_DEVICE_TYPE).toBe("ipc");
    expect(DEVICE_TYPE_OPTIONS.map(option => option.value)).toEqual(["ipc", "nvr", "platform"]);
  });

  it("大小写与空白都被归一", () => {
    expect(normalizeDeviceType("NVR")).toBe("nvr");
    expect(normalizeDeviceType(" Platform ")).toBe("platform");
    expect(normalizeDeviceType("IPC")).toBe("ipc");
  });

  it("认不出来的值回落 IPC（不猜、不抛错）", () => {
    for (const value of [undefined, null, "", "   ", "camera", "下级平台", "nvr2"]) {
      expect(normalizeDeviceType(value), String(value)).toBe("ipc");
    }
  });

  it("空值/未知值也有可展示的文案", () => {
    expect(deviceTypeLabel("nvr")).toBe("NVR 硬盘录像机");
    expect(deviceTypeLabel("platform")).toBe("下级国标平台");
    expect(deviceTypeLabel(undefined)).toBe("IPC 摄像机");
    expect(deviceTypeShortLabel("platform")).toBe("下级平台");
    expect(deviceTypeShortLabel("bogus")).toBe("IPC");
  });

  it("工具栏筛选选项与值域同源，只换短文案", () => {
    // 筛选下拉必须与后端的三个合法值逐个对齐：多一个会 422，少一个就筛不出来。
    expect(DEVICE_TYPE_FILTER_OPTIONS.map(option => option.value)).toEqual([...DEVICE_TYPES]);
    expect(DEVICE_TYPE_FILTER_OPTIONS.map(option => option.label)).toEqual(["IPC", "NVR", "下级平台"]);
  });
});
