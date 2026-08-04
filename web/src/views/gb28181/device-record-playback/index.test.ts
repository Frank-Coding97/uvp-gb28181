import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import DeviceRecordPlayback from "./index.vue";
import playbackPageSource from "./index.vue?raw";

const api = vi.hoisted(() => ({
    getRecordQueryOptions: vi.fn(),
    queryDeviceRecords: vi.fn(),
    routerPush: vi.fn(),
    routerGetRoutes: vi.fn(() => [
        { name: "device-mgmt-list", path: "/gb28181/device-mgmt/index" }
    ])
}));

vi.mock("../device-mgmt/api", async importOriginal => ({
    ...await importOriginal<typeof import("../device-mgmt/api")>(),
    ...api
}));

vi.mock("vue-router", async importOriginal => ({
    ...await importOriginal<typeof import("vue-router")>(),
    useRoute: () => ({ params: { channelId: "31" }, query: { recordQueryMock: "complete", returnKey: "return-key" } }),
    useRouter: () => ({ push: api.routerPush, getRoutes: api.routerGetRoutes })
}));

describe("device record playback workspace", () => {
    beforeEach(() => {
        api.routerPush.mockReset();
        api.getRecordQueryOptions.mockResolvedValue({ code: 0, data: {
            device: { id: 7, code: "34020000002000000001", name: "园区 NVR-A", online: true },
            channel: { id: 31, code: "34020000001320000001", name: "东门出入口" },
            timezone: "Asia/Shanghai",
            serverNow: "2026-08-02T19:40:20+08:00",
            maxRangeHours: 24,
            timeoutSeconds: 15,
            supportedTypes: ["all", "manual", "alarm"]
        }});
        api.queryDeviceRecords.mockResolvedValue({ code: 0, data: {
            status: "complete",
            declaredTotal: 1,
            receivedCount: 1,
            incomplete: false,
            timezone: "Asia/Shanghai",
            elapsedMs: 842,
            list: [{
                deviceId: "34020000001320000001",
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
        }});
    });

    it("fits the playback workspace into its layout host instead of the browser viewport", () => {
        expect(playbackPageSource).toContain('class="snow-fill-inner uvp-page-shell-flat playback-workspace"');
        expect(playbackPageSource).toContain(".record-playback-page { height: 100%; min-height: 0;");
        expect(playbackPageSource).toContain(".playback-workspace { display: flex; flex-direction: column; height: 100%; min-height: 0;");
        expect(playbackPageSource).toContain(".timeline-panel { flex: none;");
        expect(playbackPageSource).not.toContain("min-height: 100vh");
    });

    it("uses the device-list search panel language for the query area", () => {
        expect(playbackPageSource).toContain("background: var(--uvp-search-panel-bg);");
        expect(playbackPageSource).toContain("border: 1px solid var(--uvp-list-panel-border);");
        expect(playbackPageSource).toContain("border-radius: var(--uvp-panel-radius);");
        expect(playbackPageSource).toContain("box-shadow: var(--uvp-search-panel-shadow);");
    });

    it("returns to the dynamic device management route with its saved state key", async () => {
        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await wrapper.get('[aria-label="返回设备管理"]').trigger("click");
        expect(api.routerPush).toHaveBeenCalledWith({
            name: "device-mgmt-list",
            query: { returnKey: "return-key" }
        });
    });

    it("queries recordings automatically after loading the channel options", async () => {
        mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();

        expect(api.getRecordQueryOptions).toHaveBeenCalledTimes(1);
        expect(api.queryDeviceRecords).toHaveBeenCalledTimes(1);
        expect(api.queryDeviceRecords).toHaveBeenCalledWith(
            31,
            expect.objectContaining({ type: "all" }),
            expect.any(AbortSignal)
        );
    });

    it("uses a clean recording cover before playback starts", async () => {
        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();

        const viewport = wrapper.get('[data-testid="playback-viewport"]');
        expect(viewport.find('[data-testid="playback-idle-cover"]').attributes("aria-label")).toBe("录像未播放");
        expect(viewport.find(".camera-scene").exists()).toBe(false);
        expect(viewport.text()).not.toContain("设备录像 MOCK");
        expect(viewport.text()).not.toContain("从右侧录像段或下方时间轴选择一段录像");
    });

    it("renders the five-zone playback workspace and query result", async () => {
        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();
        await wrapper.get('[data-testid="record-query-submit"]').trigger("click");
        await flushPromises();
        expect(wrapper.find('[data-testid="playback-query-bar"]').exists()).toBe(true);
        expect(wrapper.find('[data-testid="playback-viewport"]').exists()).toBe(true);
        expect(wrapper.find('[data-testid="record-segment-list"]').exists()).toBe(true);
        expect(wrapper.find('[data-testid="playback-controls"]').exists()).toBe(true);
        expect(wrapper.find('[data-testid="record-timeline"]').exists()).toBe(true);
        expect(wrapper.text()).toContain("上午巡检录像");
        expect(wrapper.find('[aria-label="静音"]').exists()).toBe(false);
        expect(wrapper.find('[aria-label="取消静音"]').exists()).toBe(false);
        expect(wrapper.find('[aria-label="音量"]').exists()).toBe(false);
    });

    it("selects first, then starts mock playback explicitly", async () => {
        vi.useFakeTimers();
        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();
        await wrapper.get('[data-testid="record-query-submit"]').trigger("click");
        await flushPromises();
        await wrapper.get('[data-testid="record-segment-0"]').trigger("click");
        expect(wrapper.get('[data-testid="playback-status"]').text()).toContain("已选择");
        await wrapper.get('[data-testid="playback-primary-action"]').trigger("click");
        await vi.advanceTimersByTimeAsync(650);
        expect(wrapper.get('[data-testid="playback-status"]').text()).toContain("正在回放");
        await vi.advanceTimersByTimeAsync(1000);
        expect(wrapper.get('[data-testid="playback-time"]').text()).toContain("08:10:01");
        vi.useRealTimers();
    });

    it("offers a download entry for the selected recording", async () => {
        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();
        await wrapper.get('[data-testid="record-query-submit"]').trigger("click");
        await flushPromises();
        const download = wrapper.get('[data-testid="playback-download"]');
        expect(download.attributes("disabled")).toBeUndefined();
        await download.trigger("click");
        expect(wrapper.get('[data-testid="download-notice"]').text()).toContain("已创建下载任务");
        expect(wrapper.get('[data-testid="download-notice"]').text()).toContain("上午巡检录像");
    });

    it("offers an independent download entry on every record segment", async () => {
        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();
        await wrapper.get('[data-testid="record-query-submit"]').trigger("click");
        await flushPromises();
        const download = wrapper.get('[data-testid="record-segment-download-0"]');
        expect(download.attributes("aria-label")).toBe("下载 上午巡检录像");
        await download.trigger("click");
        expect(wrapper.get('[data-testid="download-notice"]').text()).toContain("已创建下载任务");
        expect(wrapper.get('[data-testid="download-notice"]').text()).toContain("上午巡检录像");
    });
});
