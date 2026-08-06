import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import DeviceRecordPlayback from "./index.vue";
import playbackPageSource from "./index.vue?raw";
import RecordTimeline from "./components/RecordTimeline.vue";
import timelineSource from "./components/RecordTimeline.vue?raw";

const api = vi.hoisted(() => ({
    getRecordQueryOptions: vi.fn(),
    queryDeviceRecords: vi.fn(),
    createPlaybackSession: vi.fn(),
    getPlaybackSession: vi.fn(),
    actionPlaybackSession: vi.fn(),
    deletePlaybackSession: vi.fn(),
    routerPush: vi.fn(),
    routerGetRoutes: vi.fn(() => [
        { name: "device-mgmt-list", path: "/gb28181/device-mgmt/index" }
    ])
}));

vi.mock("../device-mgmt/api", async importOriginal => ({
    ...await importOriginal<typeof import("../device-mgmt/api")>(),
    ...api
}));

vi.mock("./api", async importOriginal => ({
    ...await importOriginal<typeof import("./api")>(),
    createPlaybackSession: api.createPlaybackSession,
    getPlaybackSession: api.getPlaybackSession,
    actionPlaybackSession: api.actionPlaybackSession,
    deletePlaybackSession: api.deletePlaybackSession
}));

vi.mock("../components/PlayWindow.vue", () => ({
    default: {
        name: "PlayWindow",
        props: ["url", "playback", "hasAudio"],
        emits: ["timeupdate", "loading"],
        template: '<div data-testid="playback-player" :data-media-url="url" />'
    }
}));

vi.mock("vue-router", async importOriginal => ({
    ...await importOriginal<typeof import("vue-router")>(),
    useRoute: () => ({ params: { channelId: "31" }, query: { recordQueryMock: "complete", returnKey: "return-key" } }),
    useRouter: () => ({ push: api.routerPush, getRoutes: api.routerGetRoutes })
}));

describe("device record playback workspace", () => {
    beforeEach(() => {
        vi.useRealTimers();
        sessionStorage.clear();
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
                recordKey: "opaque-record-key",
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
        api.createPlaybackSession.mockResolvedValue({ code: 0, data: {
            sessionId: "session-1",
            state: "playing",
            channelId: "31",
            recordKey: "opaque-record-key",
            segmentStart: "2026-08-02T08:10:00+08:00",
            segmentEnd: "2026-08-02T08:42:16+08:00",
            positionSeconds: 0,
            scale: 1,
            hasAudio: false,
            media: { urls: { wsFlv: "ws://zlm/playback/session-1.live.flv" } },
            expiresAt: "2026-08-02T09:10:00+08:00",
            errorStage: "",
            errorCode: ""
        }});
        api.getPlaybackSession.mockResolvedValue({ code: 0, data: {
            sessionId: "session-1",
            state: "playing",
            channelId: "31",
            recordKey: "opaque-record-key",
            segmentStart: "2026-08-02T08:10:00+08:00",
            segmentEnd: "2026-08-02T08:42:16+08:00",
            positionSeconds: 0,
            scale: 1,
            hasAudio: false,
            media: { urls: { wsFlv: "ws://zlm/playback/session-1.live.flv" } },
            expiresAt: "2026-08-02T09:10:00+08:00",
            errorStage: "",
            errorCode: ""
        }});
        api.actionPlaybackSession.mockImplementation((_channelId, _sessionId, action) => Promise.resolve({ code: 0, data: {
            ...(api.createPlaybackSession.mock.results[0]?.value?.data || {}),
            sessionId: "session-1",
            state: action.action === "pause" ? "paused" : "playing",
            recordKey: "opaque-record-key",
            segmentStart: "2026-08-02T08:10:00+08:00",
            segmentEnd: "2026-08-02T08:42:16+08:00",
            positionSeconds: action.positionSeconds || 0,
            scale: action.scale || 1,
            hasAudio: false,
            media: { urls: { wsFlv: "ws://zlm/playback/session-1.live.flv" } }
        }}));
        api.deletePlaybackSession.mockResolvedValue({ code: 0, data: { state: "stopped" } });
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
        expect(playbackPageSource).toContain(".channel-context { display: flex; align-items: center; gap: 9px; min-width: 260px; }");
    });

    it("aligns and rounds the query, playback, and timeline regions", () => {
        expect(playbackPageSource).toContain("min-height: 72px; margin: 0 8px 10px;");
        expect(playbackPageSource).toContain(".playback-main { display: grid; grid-template-columns: minmax(0, 1fr) 312px; flex: 1; min-height: 0; margin: 0 8px;");
        expect(playbackPageSource).toContain("background: var(--uvp-shell-muted); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); overflow: hidden; }");
        expect(playbackPageSource).toContain(".timeline-panel { flex: none; margin: 10px 8px 12px; border-radius: var(--uvp-panel-radius); overflow: hidden; }");
        expect(playbackPageSource).toContain(".playback-main { display: flex; flex: none; flex-direction: column; margin: 0 8px;");
    });

    it("uses a card selection state and a compact recording type dot", () => {
        expect(playbackPageSource).toContain(':aria-current="playback.recordKey === record.recordKey ? \'true\' : undefined"');
        expect(playbackPageSource).toContain(".segment-item.selected { z-index: 1; background: var(--uvp-table-row-checked-bg); border-color: var(--uvp-brand);");
        expect(playbackPageSource).toContain(".segment-marker { flex: none; width: 7px; height: 7px;");
        expect(playbackPageSource).not.toContain("box-shadow: inset 3px 0 0 var(--uvp-brand)");
    });

    it("returns to the dynamic device management route with its saved state key", async () => {
        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await wrapper.get('[aria-label="返回设备管理"]').trigger("click");
        expect(api.routerPush).toHaveBeenCalledWith({
            name: "device-mgmt-list",
            query: { returnKey: "return-key" }
        });
    });

    it("cleans up with the channel id captured before leaving the route", () => {
        expect(playbackPageSource).toContain("const playbackChannelId = channelId.value;");
        expect(playbackPageSource).toContain("getPlaybackSession(playbackChannelId, sessionId)");
        expect(playbackPageSource).toContain("deletePlaybackSession(playbackChannelId, sessionId)");
        expect(playbackPageSource).not.toContain("deletePlaybackSession(channelId.value, sessionId).catch");
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

    it("automatically plays the first playable recording instead of a one-second boundary fragment", async () => {
        api.queryDeviceRecords.mockResolvedValueOnce({ code: 0, data: {
            status: "complete",
            declaredTotal: 2,
            receivedCount: 2,
            incomplete: false,
            timezone: "Asia/Shanghai",
            elapsedMs: 151,
            list: [
                {
                    recordKey: "boundary-fragment",
                    name: "边界片段",
                    startTime: "2026-08-02T00:00:00+08:00",
                    endTime: "2026-08-02T00:00:01+08:00"
                },
                {
                    recordKey: "playable-recording",
                    name: "有效录像",
                    startTime: "2026-08-02T00:00:01+08:00",
                    endTime: "2026-08-02T00:32:37+08:00"
                }
            ]
        }});

        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();

        expect(wrapper.get('[data-testid="record-segment-1"]').classes()).toContain("selected");
        expect(wrapper.get('[data-testid="record-segment-0"]').classes()).not.toContain("selected");
        expect(api.createPlaybackSession).toHaveBeenCalledWith(
            31,
            { recordKey: "playable-recording", playFrom: "2026-08-02T00:00:01+08:00" },
            expect.any(String)
        );
    });

    it("keeps a stable idempotency key and restores the last recording after remount", async () => {
        const firstWrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();
        const firstRequest = api.createPlaybackSession.mock.calls[0];
        firstWrapper.unmount();

        api.createPlaybackSession.mockClear();
        const secondWrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();

        expect(api.createPlaybackSession).toHaveBeenCalledTimes(1);
        expect(api.createPlaybackSession.mock.calls[0][2]).toBe(firstRequest[2]);
        expect(secondWrapper.get('[data-testid="record-segment-0"]').classes()).toContain("selected");
    });

    it("stops the active session before automatically playing another recording", async () => {
        api.queryDeviceRecords.mockResolvedValueOnce({ code: 0, data: {
            status: "complete",
            declaredTotal: 2,
            receivedCount: 2,
            incomplete: false,
            timezone: "Asia/Shanghai",
            elapsedMs: 151,
            list: [
                {
                    recordKey: "recording-one",
                    name: "录像一",
                    startTime: "2026-08-02T00:00:01+08:00",
                    endTime: "2026-08-02T00:32:37+08:00"
                },
                {
                    recordKey: "recording-two",
                    name: "录像二",
                    startTime: "2026-08-02T00:32:37+08:00",
                    endTime: "2026-08-02T01:19:20+08:00"
                }
            ]
        }});
        let finishStop!: () => void;
        api.deletePlaybackSession.mockReturnValueOnce(new Promise(resolve => {
            finishStop = () => resolve({ code: 0, data: { state: "stopped" } });
        }));
        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();

        expect(api.createPlaybackSession).toHaveBeenCalledTimes(1);
        await wrapper.get('[data-testid="record-segment-1"]').trigger("click");
        await Promise.resolve();
        expect(api.deletePlaybackSession).toHaveBeenCalledWith(31, "session-1");
        expect(api.createPlaybackSession).toHaveBeenCalledTimes(1);
        expect(wrapper.get('[data-testid="playback-loading"]').text()).toContain("正在停止");

        finishStop();
        await flushPromises();
        expect(api.createPlaybackSession).toHaveBeenCalledTimes(2);
        expect(api.createPlaybackSession).toHaveBeenLastCalledWith(
            31,
            { recordKey: "recording-two", playFrom: "2026-08-02T00:32:37+08:00" },
            expect.any(String)
        );
        expect(wrapper.find('[data-testid="playback-loading"]').exists()).toBe(false);
        expect(wrapper.find('[data-testid="playback-player"]').exists()).toBe(true);
    });

    it("does not start another recording when stopping the active session fails", async () => {
        api.queryDeviceRecords.mockResolvedValueOnce({ code: 0, data: {
            status: "complete",
            declaredTotal: 2,
            receivedCount: 2,
            incomplete: false,
            timezone: "Asia/Shanghai",
            elapsedMs: 151,
            list: [
                { recordKey: "recording-one", name: "录像一", startTime: "2026-08-02T00:00:01+08:00", endTime: "2026-08-02T00:32:37+08:00" },
                { recordKey: "recording-two", name: "录像二", startTime: "2026-08-02T00:32:37+08:00", endTime: "2026-08-02T01:19:20+08:00" }
            ]
        }});
        api.deletePlaybackSession.mockRejectedValueOnce(new Error("stop failed"));
        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();

        await wrapper.get('[data-testid="record-segment-1"]').trigger("click");
        await flushPromises();

        expect(api.createPlaybackSession).toHaveBeenCalledTimes(1);
        expect(wrapper.get('[data-testid="record-segment-0"]').classes()).toContain("selected");
        expect(wrapper.get('[data-testid="playback-status"]').text()).toContain("回放失败");
    });

    it("replaces the idle cover with the player after automatic playback starts", async () => {
        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();

        const viewport = wrapper.get('[data-testid="playback-viewport"]');
        expect(viewport.find('[data-testid="playback-idle-cover"]').exists()).toBe(false);
        expect(viewport.find('[data-testid="playback-player"]').exists()).toBe(true);
        expect(viewport.find(".camera-scene").exists()).toBe(false);
        expect(viewport.text()).not.toContain("设备录像 MOCK");
    });

    it("keeps only the channel label over the video surface", () => {
        expect(playbackPageSource).toContain('class="video-overlay top-overlay"');
        expect(playbackPageSource).toContain('CH-01 {{ options?.channel.name');
        expect(playbackPageSource).not.toContain('class="video-overlay bottom-overlay"');
        expect(playbackPageSource).not.toContain('class="stream-badge"');
        expect(playbackPageSource).not.toContain("localDateTime(playback.currentTime");
    });

    it("keeps the timeline as the only dynamic current-time display", () => {
        expect(playbackPageSource).toContain('@timeupdate="handlePlayerTimeUpdate"');
        expect(playbackPageSource).toContain('@loading="handlePlayerLoading"');
        expect(playbackPageSource).not.toContain('data-testid="playback-time"');
        expect(timelineSource).toContain('data-testid="timeline-range"');
        expect(timelineSource).toContain('当前录像 · {{ selectedRecordRangeText }}');
        expect(timelineSource).not.toContain('当前视窗 · {{ visibleRangeText }}');
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
        expect(wrapper.get('[data-testid="record-segment-size-0"]').text()).toBe("237.1 MB");
        expect(wrapper.find('[aria-label="静音"]').exists()).toBe(false);
        expect(wrapper.find('[aria-label="取消静音"]').exists()).toBe(false);
        expect(wrapper.find('[aria-label="音量"]').exists()).toBe(false);
    });

    it("does not render a file size placeholder when the device omits FileSize", async () => {
        api.queryDeviceRecords.mockResolvedValueOnce({ code: 0, data: {
            status: "complete",
            declaredTotal: 1,
            receivedCount: 1,
            incomplete: false,
            timezone: "Asia/Shanghai",
            elapsedMs: 120,
            list: [{
                recordKey: "without-file-size",
                name: "未返回大小的录像",
                startTime: "2026-08-02T09:00:00+08:00",
                endTime: "2026-08-02T09:30:00+08:00",
                fileSize: null
            }]
        }});

        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();

        expect(wrapper.find('[data-testid="record-segment-size-0"]').exists()).toBe(false);
    });

    it("creates a real playback session and renders its media URL", async () => {
        vi.useFakeTimers();
        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();
        expect(api.createPlaybackSession).toHaveBeenCalledWith(
            31,
            { recordKey: "opaque-record-key", playFrom: "2026-08-02T08:10:00+08:00" },
            expect.any(String)
        );
        expect(wrapper.get('[data-testid="playback-status"]').text()).toContain("正在回放");
        expect(wrapper.get('[data-testid="playback-player"]').attributes("data-media-url")).toBe("ws://zlm/playback/session-1.live.flv");
        const player = wrapper.findComponent({ name: "PlayWindow" });
        player.vm.$emit("timeupdate", 10_000);
        player.vm.$emit("timeupdate", 11_000);
        await flushPromises();
        expect(wrapper.get('[data-testid="timeline-playhead"]').text()).toContain("08:10:01");
    });

    it("freezes the timeline while the media is buffering", async () => {
        vi.useFakeTimers();
        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();
        const player = wrapper.findComponent({ name: "PlayWindow" });

        player.vm.$emit("timeupdate", 10_000);
        player.vm.$emit("loading", true);
        player.vm.$emit("timeupdate", 13_000);
        await vi.advanceTimersByTimeAsync(3000);
        expect(wrapper.get('[data-testid="timeline-playhead"]').text()).toContain("08:10:00");

        player.vm.$emit("loading", false);
        player.vm.$emit("timeupdate", 11_000);
        await flushPromises();
        expect(wrapper.get('[data-testid="timeline-playhead"]').text()).toContain("08:10:01");
    });

    it("sends playback controls and stops the active session", async () => {
        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();
        await wrapper.get('[data-testid="playback-primary-action"]').trigger("click");
        await flushPromises();

        await wrapper.get('[data-testid="playback-primary-action"]').trigger("click");
        await flushPromises();
        expect(api.actionPlaybackSession).toHaveBeenCalledWith(31, "session-1", { action: "pause" });

        await wrapper.get('[data-testid="playback-primary-action"]').trigger("click");
        await flushPromises();
        expect(api.actionPlaybackSession).toHaveBeenCalledWith(31, "session-1", { action: "resume" });

        await wrapper.get(".scale-select select").setValue("2");
        await flushPromises();
        expect(api.actionPlaybackSession).toHaveBeenCalledWith(31, "session-1", { action: "scale", scale: 2 });

        wrapper.findComponent(RecordTimeline).vm.$emit("locate", {
            recordKey: "opaque-record-key",
            time: "2026-08-02T08:10:42+08:00"
        });
        await flushPromises();
        expect(api.actionPlaybackSession).toHaveBeenCalledWith(31, "session-1", { action: "seek", positionSeconds: 42 });

        await wrapper.get('[aria-label="停止"]').trigger("click");
        await flushPromises();
        expect(api.deletePlaybackSession).toHaveBeenCalledWith(31, "session-1");
    });

    it("switches recordings and starts playback from the time released on the timeline", async () => {
        api.queryDeviceRecords.mockResolvedValueOnce({ code: 0, data: {
            status: "complete",
            declaredTotal: 2,
            receivedCount: 2,
            incomplete: false,
            timezone: "Asia/Shanghai",
            elapsedMs: 151,
            list: [
                { recordKey: "recording-one", name: "录像一", startTime: "2026-08-02T08:00:00+08:00", endTime: "2026-08-02T09:00:00+08:00" },
                { recordKey: "recording-two", name: "录像二", startTime: "2026-08-02T09:00:00+08:00", endTime: "2026-08-02T10:00:00+08:00" }
            ]
        }});
        const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
        await flushPromises();

        wrapper.findComponent(RecordTimeline).vm.$emit("locate", {
            recordKey: "recording-two",
            time: "2026-08-02T09:12:34+08:00"
        });
        await flushPromises();

        expect(api.deletePlaybackSession).toHaveBeenCalledWith(31, "session-1");
        expect(api.createPlaybackSession).toHaveBeenCalledTimes(2);
        expect(api.createPlaybackSession).toHaveBeenLastCalledWith(
            31,
            { recordKey: "recording-two", playFrom: "2026-08-02T09:12:34+08:00" },
            expect.any(String)
        );
        expect(wrapper.get('[data-testid="timeline-playhead"]').text()).toContain("09:12:34");
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
