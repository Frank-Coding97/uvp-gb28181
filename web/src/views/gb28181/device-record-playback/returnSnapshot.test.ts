import { beforeEach, describe, expect, it } from "vitest";
import {
    consumeDeviceMgmtReturnSnapshot,
    saveDeviceMgmtReturnSnapshot,
    type DeviceMgmtReturnSnapshot
} from "./returnSnapshot";

const snapshot: DeviceMgmtReturnSnapshot = {
    version: 1,
    viewMode: "list",
    assetKind: "channel",
    keyword: "东门",
    deviceIdFilter: "34020000002000000001",
    statusFilter: "online",
    directoryView: "administrative",
    directorySelectedKey: "administrative:340200",
    page: 2,
    listPageSize: 20,
    cardPageSize: 12
};

describe("device management return snapshot", () => {
    beforeEach(() => window.sessionStorage.clear());

    it("saves and consumes a snapshot exactly once", () => {
        const key = saveDeviceMgmtReturnSnapshot(snapshot, "fixed-key");
        expect(key).toBe("fixed-key");
        expect(consumeDeviceMgmtReturnSnapshot(key)).toEqual(snapshot);
        expect(consumeDeviceMgmtReturnSnapshot(key)).toBeNull();
    });

    it("ignores corrupt and unsupported snapshots", () => {
        window.sessionStorage.setItem("uvp.device-record-return:bad", "{");
        window.sessionStorage.setItem("uvp.device-record-return:old", JSON.stringify({ ...snapshot, version: 0 }));
        expect(consumeDeviceMgmtReturnSnapshot("bad")).toBeNull();
        expect(consumeDeviceMgmtReturnSnapshot("old")).toBeNull();
    });
});
