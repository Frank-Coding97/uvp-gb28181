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

export function saveDeviceMgmtReturnSnapshot(snapshot: DeviceMgmtReturnSnapshot, key: string = crypto.randomUUID()): string {
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
        if (!Number.isInteger(candidate.page) || !Number.isInteger(candidate.listPageSize) || !Number.isInteger(candidate.cardPageSize)) return null;
        return candidate as DeviceMgmtReturnSnapshot;
    } catch {
        return null;
    }
}
