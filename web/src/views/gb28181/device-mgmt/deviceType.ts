/**
 * 设备类型：IPC（摄像机）/ NVR（硬盘录像机）/ 下级国标平台。
 *
 * 三个值由用户**手动维护**（默认 IPC），只驱动卡片图标与分类展示，
 * 不参与任何接入/协议逻辑 —— 平台从注册报文里推不出真实形态，自动推断必然猜错。
 * 值域与后端 `models.NormalizeDeviceType` 逐字对齐（ipc / nvr / platform，小写）。
 */
export const DEVICE_TYPES = ["ipc", "nvr", "platform"] as const;

export type DeviceType = (typeof DEVICE_TYPES)[number];

/** 新建 / 自动建档时的默认类型。 */
export const DEFAULT_DEVICE_TYPE: DeviceType = "ipc";

export const DEVICE_TYPE_OPTIONS: ReadonlyArray<{ label: string; value: DeviceType }> = [
  { label: "IPC 摄像机", value: "ipc" },
  { label: "NVR 硬盘录像机", value: "nvr" },
  { label: "下级国标平台", value: "platform" }
];

const DEVICE_TYPE_LABELS: Record<DeviceType, string> = {
  ipc: "IPC 摄像机",
  nvr: "NVR 硬盘录像机",
  platform: "下级国标平台"
};

const DEVICE_TYPE_SHORT_LABELS: Record<DeviceType, string> = {
  ipc: "IPC",
  nvr: "NVR",
  platform: "下级平台"
};

function isDeviceType(value: string): value is DeviceType {
  return (DEVICE_TYPES as readonly string[]).includes(value);
}

/**
 * 归一化接口/存量数据里的类型值。
 * 认不出来（含 null / 空串 / 历史脏数据）一律回落 IPC —— 老库刚加列时全是默认值，
 * 这里再猜一次没有收益，反而会让卡片图标抖动。
 */
export function normalizeDeviceType(raw?: string | null): DeviceType {
  const token = typeof raw === "string" ? raw.trim().toLowerCase() : "";
  return isDeviceType(token) ? token : DEFAULT_DEVICE_TYPE;
}

/** 下拉框 / tooltip 用的完整文案。 */
export function deviceTypeLabel(raw?: string | null): string {
  return DEVICE_TYPE_LABELS[normalizeDeviceType(raw)];
}

/** 卡片上窄条、表格里用的短文案。 */
export function deviceTypeShortLabel(raw?: string | null): string {
  return DEVICE_TYPE_SHORT_LABELS[normalizeDeviceType(raw)];
}
