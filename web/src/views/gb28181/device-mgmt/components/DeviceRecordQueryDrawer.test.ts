import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ChannelVO, RecordQueryOptions, RecordQueryResult } from "../api";
import DeviceRecordQueryDrawer from "./DeviceRecordQueryDrawer.vue";

const api = vi.hoisted(() => ({
    getRecordQueryOptions: vi.fn(),
    queryDeviceRecords: vi.fn()
}));

vi.mock("../api", async importOriginal => ({
    ...await importOriginal<typeof import("../api")>(),
    ...api
}));

const channel = (id = 31): ChannelVO => ({
    id,
    channelId: `340200000013200000${String(id).padStart(2, "0")}`,
    deviceId: "34020000002000000001",
    name: id === 31 ? "东门出入口" : "西门出入口",
    alias: "",
    manufacturer: "Demo",
    model: "NVR",
    owner: "",
    civilCode: "",
    parentId: "",
    ptzType: 0,
    longitude: 0,
    latitude: 0,
    status: 1,
    streamId: "",
    onDemandLive: true,
    streamTransport: "UDP",
    audioEnabled: true,
    cloudRecordingEnabled: false,
    cloudRecordingState: "disabled",
    cloudRecordingError: "",
    createdAt: "2026-08-02T08:00:00+08:00",
    updatedAt: "2026-08-02T19:00:00+08:00"
});

const options = (id = 31): RecordQueryOptions => ({
    device: { id: 7, code: "34020000002000000001", name: "园区 NVR-A", online: true },
    channel: { id, code: channel(id).channelId, name: channel(id).name },
    timezone: "Asia/Shanghai",
    serverNow: "2026-08-02T19:40:20+08:00",
    maxRangeHours: 24,
    timeoutSeconds: 15,
    supportedTypes: ["all", "manual", "alarm"]
});

const result = (status: RecordQueryResult["status"]): RecordQueryResult => ({
    status,
    partialReason: status === "partial" ? "deadline" : null,
    declaredTotal: status === "partial" ? 8 : status === "empty" ? 0 : 1,
    receivedCount: status === "empty" ? 0 : status === "partial" ? 1 : 1,
    incomplete: status === "partial",
    timezone: "Asia/Shanghai",
    elapsedMs: 800,
    list: status === "empty" ? [] : [{
        deviceId: channel().channelId,
        name: "上午巡检录像",
        filePath: "/record/001.dav",
        address: "园区东门",
        startTime: "2026-08-02T08:10:00+08:00",
        endTime: "2026-08-02T08:42:16+08:00",
        secrecy: 0,
        type: "time",
        recorderId: "NVR-A",
        fileSize: 248635904,
        recordLocation: "34020000002000000001",
        streamNumber: 0
    }]
});

function mountDrawer() {
    return mount(DeviceRecordQueryDrawer, {
        props: { visible: true, channel: channel() },
        global: {
            stubs: {
                "a-drawer": { props: ["visible"], template: "<section v-if='visible'><slot name='title' /><slot /></section>" },
                "a-spin": { template: "<div><slot /></div>" },
                "a-button": { template: "<button @click='$emit(`click`)'><slot name='icon' /><slot /></button>" },
                "a-range-picker": { template: "<div data-testid='range-picker' />" },
                "a-select": { template: "<select><slot /></select>" },
                "a-option": { template: "<option><slot /></option>" },
                "a-input": { template: "<input />" },
                "a-input-number": { template: "<input type='number' />" },
                "a-table": { template: "<div data-testid='result-table'><slot /></div>" },
                "a-table-column": { template: "<div><slot name='cell' :record='{}' /></div>" },
                "a-pagination": { template: "<div data-testid='result-pagination' />" },
                "a-tooltip": { template: "<span><slot /></span>" },
                "a-empty": { props: ["description"], template: "<div>{{ description }}</div>" },
                "a-alert": { template: "<div><slot /></div>" }
            }
        }
    });
}

describe("DeviceRecordQueryDrawer", () => {
    beforeEach(() => {
        api.getRecordQueryOptions.mockResolvedValue({ code: 0, message: "ok", data: options() });
        api.queryDeviceRecords.mockResolvedValue({ code: 0, message: "ok", data: result("complete") });
    });

    it("loads the selected channel context and platform-time default range", async () => {
        const wrapper = mountDrawer();
        await flushPromises();
        expect(api.getRecordQueryOptions).toHaveBeenCalledWith(31);
        expect(wrapper.text()).toContain("园区 NVR-A");
        expect(wrapper.text()).toContain("东门出入口");
        expect(wrapper.text()).toContain("Asia/Shanghai");
        expect(wrapper.text()).toContain("2026-08-02 00:00:00");
    });

    it.each([
        ["complete", "查询完成"],
        ["empty", "未查询到录像"],
        ["partial", "结果可能不完整"]
    ] as Array<[RecordQueryResult["status"], string]>)
    ("renders the %s result state", async (status, expectedText) => {
        api.queryDeviceRecords.mockResolvedValue({ code: 0, message: "ok", data: result(status) });
        const wrapper = mountDrawer();
        await flushPromises();
        await wrapper.get('[data-testid="record-query-submit"]').trigger("click");
        await flushPromises();
        expect(wrapper.text()).toContain(expectedText);
        expect(wrapper.find('[data-testid="result-table"]').exists()).toBe(status !== "empty");
    });

    it("keeps conditions and shows a distinct timeout state", async () => {
        api.queryDeviceRecords.mockRejectedValue({ errorCode: "record_query_timeout", message: "设备未返回录像目录" });
        const wrapper = mountDrawer();
        await flushPromises();
        await wrapper.get('[data-testid="record-query-submit"]').trigger("click");
        await flushPromises();
        expect(wrapper.text()).toContain("查询超时");
        expect(wrapper.text()).toContain("2026-08-02 00:00:00");
    });

    it("reloads a new channel and ignores the stale options response", async () => {
        let resolveOld!: (value: unknown) => void;
        api.getRecordQueryOptions.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve; }));
        api.getRecordQueryOptions.mockResolvedValueOnce({ code: 0, message: "ok", data: options(32) });
        const wrapper = mountDrawer();
        await wrapper.setProps({ channel: channel(32) });
        await flushPromises();
        resolveOld({ code: 0, message: "ok", data: options(31) });
        await flushPromises();
        expect(wrapper.text()).toContain("西门出入口");
        expect(wrapper.text()).not.toContain("东门出入口");
    });
});
