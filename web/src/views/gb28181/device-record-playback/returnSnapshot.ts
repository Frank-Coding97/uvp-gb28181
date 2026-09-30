export type DeviceMgmtViewMode = "list" | "card" | "map";
export type DeviceMgmtAssetKind = "device" | "channel";

export interface DeviceMgmtReturnSnapshot {
  version: 1;
  viewMode: DeviceMgmtViewMode;
  assetKind: DeviceMgmtAssetKind;
  keyword: string;
  deviceIdFilter: string;
  statusFilter?: "online" | "offline";
  directoryView: string;
  directorySelectedKey: string | null;
  page: number;
  listPageSize: number;
  cardPageSize: number;
}

const prefix = "uvp.device-record-return:";

/**
 * 生成返回快照的 key。
 * ⛔ `crypto.randomUUID` 是 **Secure Context 限定** API：用 http 通过局域网 IP / 域名访问时
 * `window.isSecureContext === false`，`crypto.randomUUID` 是 `undefined`，直接调用会抛
 * `TypeError: crypto.randomUUID is not a function`。此处是 `openRecordQuery` 里
 * `router.push` 的**前一行**，抛错会整条中断点击处理 ⇒ 按钮「点了没反应」。
 * 与 `DeviceRebootDialog` / `StorageCardFormatDialog` / `DeviceFirmwareUpgradePanel` /
 * `PlayConsoleLinked` 保持同一套降级写法。
 */
function createReturnSnapshotKey(): string {
  if (typeof globalThis.crypto?.randomUUID === "function") return globalThis.crypto.randomUUID();
  return `record-return-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

export function saveDeviceMgmtReturnSnapshot(
  snapshot: DeviceMgmtReturnSnapshot,
  key: string = createReturnSnapshotKey()
): string {
  window.sessionStorage.setItem(`${prefix}${key}`, JSON.stringify(snapshot));
  return key;
}

export function consumeDeviceMgmtReturnSnapshot(key: string | null | undefined): DeviceMgmtReturnSnapshot | null {
  if (!key) return null;
  const storageKey = `${prefix}${key}`;
  const raw = window.sessionStorage.getItem(storageKey);
  window.sessionStorage.removeItem(storageKey);
  if (!raw) return null;
  try {
    const candidate = JSON.parse(raw) as Partial<DeviceMgmtReturnSnapshot>;
    if (candidate.version !== 1 || !["list", "card", "map"].includes(candidate.viewMode || "")) return null;
    if (!candidate.assetKind || !["device", "channel"].includes(candidate.assetKind)) return null;
    if (
      !Number.isInteger(candidate.page) ||
      !Number.isInteger(candidate.listPageSize) ||
      !Number.isInteger(candidate.cardPageSize)
    )
      return null;
    return candidate as DeviceMgmtReturnSnapshot;
  } catch {
    return null;
  }
}
