import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
    listChannels: vi.fn()
}));
const playback = vi.hoisted(() => ({
    startPlay: vi.fn(),
    stopPlay: vi.fn(),
    controlPtz: vi.fn(),
    getControlCapabilities: vi.fn(),
    listPlaybackSchemes: vi.fn(),
    getPlaybackScheme: vi.fn(),
    createPlaybackScheme: vi.fn(),
    renamePlaybackScheme: vi.fn(),
    replacePlaybackSchemeLayout: vi.fn(),
    deletePlaybackScheme: vi.fn()
}));

vi.mock("../device-mgmt/api", () => api);
vi.mock("@/api/gb28181", () => playback);
vi.mock("./PlaybackSourceTree.vue", () => ({
    default: {
        name: "PlaybackSourceTree",
        props: ["usedChannelIds"],
        emits: ["select"],
        template: `
            <div data-test="playback-source-tree">
                <button data-test="source-channel-1" @click="$emit('select', {
                    id: 1, channelId: 'channel-1', deviceId: 'device-1', name: '东门', status: 1, audioEnabled: false
                })">东门</button>
                <button data-test="source-channel-2" @click="$emit('select', {
                    id: 2, channelId: 'channel-2', deviceId: 'device-2', name: '西门', status: 1, audioEnabled: false
                })">西门</button>
            </div>
        `
    }
}));
vi.mock("../components/PlayWindow.vue", () => ({
    default: {
        name: "PlayWindow",
        props: ["url"],
        template: "<div class=\"mock-play-window\" :data-url=\"url\" />"
    }
}));
vi.mock("../components/PlayConsoleLinked.vue", () => ({
    default: {
        name: "PlayConsoleLinked",
        props: ["visible", "channel"],
        emits: ["update:visible"],
        template: "<div v-if=\"visible\" data-test=\"linked-console\">{{ channel?.name }}</div>"
    }
}));

import MultiScreenPlayback from "./index.vue";

describe("multi-screen playback page", () => {
    beforeEach(() => {
        api.listChannels.mockReset();
        playback.startPlay.mockImplementation(async (_deviceId: string, channelId: string) => ({
            code: 0,
            data: {
                streamId: `stream-${channelId}`,
                ssrc: `ssrc-${channelId}`,
                app: "rtp",
                urls: { wsFlv: `ws://zlm/${channelId}.live.flv` },
                wsflvUrl: `ws://zlm/${channelId}.live.flv`,
                httpFlvUrl: "",
                hlsUrl: "",
                expireAt: 0
            }
        }));
        playback.stopPlay.mockResolvedValue({ code: 0, data: { released: true, streamId: "" } });
        playback.controlPtz.mockResolvedValue({ code: 0, data: { action: "accepted" } });
        playback.getControlCapabilities.mockResolvedValue({
            code: 0,
            data: { basicPtz: { state: "supported", reason: "" } }
        });
        playback.listPlaybackSchemes.mockResolvedValue({ code: 0, data: { list: [], total: 0, page: 1, pageSize: 10 } });
    });

    it("renders four stable slots and the dedicated playback source tree", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();

        expect(wrapper.find("[data-test=playback-source-tree]").exists()).toBe(true);
        expect(wrapper.findAll("[data-test=screen-slot]")).toHaveLength(4);
        expect(wrapper.findAll(".unplayed-cover")).toHaveLength(4);
        expect(wrapper.find("[data-test=layout-9]").exists()).toBe(true);
    });

    it("opens and closes playback schemes without changing current playback", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await flushPromises();
        const callsBeforeOpen = playback.startPlay.mock.calls.length;

        await wrapper.get("[data-test=playback-schemes]").trigger("click");
        await flushPromises();
        expect(wrapper.find(".scheme-panel").exists()).toBe(true);
        expect(playback.listPlaybackSchemes).toHaveBeenCalledWith({ page: 1, pageSize: 10, q: undefined });

        await wrapper.get("[data-test=close-schemes]").trigger("click");
        expect(wrapper.find(".scheme-panel").exists()).toBe(false);
        expect(playback.startPlay).toHaveBeenCalledTimes(callsBeforeOpen);
        expect(wrapper.findAll(".slot-channel-name").map(node => node.text())).toEqual(["东门"]);
    });

    it.each([
        [1, "layout-1"],
        [4, "layout-4"],
        [6, "layout-6"],
        [8, "layout-8"],
        [9, "layout-9"],
        [16, "layout-16"]
    ])("switches to %i visible playback slots", async (count, layoutClass) => {
        const wrapper = mount(MultiScreenPlayback);

        await wrapper.get(`[data-test=layout-${count}]`).trigger("click");

        expect(wrapper.findAll("[data-test=screen-slot]")).toHaveLength(count);
        expect(wrapper.get("[aria-label=多屏播放格子]").classes()).toContain(layoutClass);
    });

    it("starts independent streams when two sources are assigned", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();

        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await wrapper.get("[data-test=source-channel-2]").trigger("click");
        await flushPromises();

        expect(wrapper.findAll(".unplayed-cover")).toHaveLength(2);
        expect(playback.startPlay).toHaveBeenNthCalledWith(1, "device-1", "channel-1");
        expect(playback.startPlay).toHaveBeenNthCalledWith(2, "device-2", "channel-2");
        expect(wrapper.findAll(".slot-channel-name").map(node => node.text())).toEqual(["东门", "西门"]);
    });

    it("shows the focused player's PTZ direction while movement is held", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await flushPromises();

        await wrapper.get("[data-test=ptz-up]").trigger("pointerdown");
        const indicator = wrapper.get("[data-test=ptz-direction-indicator]");
        expect(indicator.attributes("data-direction")).toBe("上");
        expect(indicator.attributes("aria-label")).toBe("云台正在向上移动");

        await wrapper.get("[data-test=ptz-up]").trigger("pointerup");
        expect(wrapper.find("[data-test=ptz-direction-indicator]").exists()).toBe(false);
    });

    it("removes one slot locally without stopping the shared channel stream", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();
        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await flushPromises();

        await wrapper.get("[data-test=slot-remove-0]").trigger("click");
        expect(wrapper.findAll(".slot-channel-name").map(node => node.text())).toEqual([]);
        expect(playback.stopPlay).not.toHaveBeenCalled();
    });

    it("plays available online channels until the active layout is filled", async () => {
        const channels = Array.from({ length: 8 }, (_, index) => ({
            id: index + 11,
            channelId: `online-channel-${index + 1}`,
            deviceId: `online-device-${index + 1}`,
            name: `在线通道${index + 1}`,
            status: 1,
            audioEnabled: false
        }));
        api.listChannels.mockResolvedValue({ code: 0, data: { list: channels, total: channels.length, page: 1, pageSize: 200 } });
        const wrapper = mount(MultiScreenPlayback);

        await wrapper.get("[data-test=layout-6]").trigger("click");
        expect(wrapper.get("[data-test=play-all]").attributes("disabled")).toBeUndefined();
        await wrapper.get("[data-test=play-all]").trigger("click");
        await flushPromises();

        expect(api.listChannels).toHaveBeenCalledWith({ status: "online", page: 1, pageSize: 200 });
        expect(wrapper.findAll(".mock-play-window")).toHaveLength(6);
        expect(wrapper.findAll(".unplayed-cover")).toHaveLength(0);
        expect(wrapper.findAll(".slot-channel-name").map(node => node.text())).toEqual([
            "在线通道1", "在线通道2", "在线通道3", "在线通道4", "在线通道5", "在线通道6"
        ]);
        expect(wrapper.find(".workspace-toast").exists()).toBe(false);
    });

    it("leaves remaining windows covered when online channels are insufficient", async () => {
        const channels = Array.from({ length: 2 }, (_, index) => ({
            id: index + 31,
            channelId: `limited-channel-${index + 1}`,
            deviceId: `limited-device-${index + 1}`,
            name: `有限通道${index + 1}`,
            status: 1,
            audioEnabled: false
        }));
        api.listChannels.mockResolvedValue({ code: 0, data: { list: channels, total: channels.length, page: 1, pageSize: 200 } });
        const wrapper = mount(MultiScreenPlayback);

        await wrapper.get("[data-test=play-all]").trigger("click");
        await flushPromises();

        expect(wrapper.findAll(".mock-play-window")).toHaveLength(2);
        expect(wrapper.findAll(".unplayed-cover")).toHaveLength(2);
    });

    it("stops all players by clearing every visible channel back to covers", async () => {
        const channels = Array.from({ length: 3 }, (_, index) => ({
            id: index + 21,
            channelId: `stop-channel-${index + 1}`,
            deviceId: `stop-device-${index + 1}`,
            name: `停止通道${index + 1}`,
            status: 1,
            audioEnabled: false
        }));
        api.listChannels.mockResolvedValue({ code: 0, data: { list: channels, total: channels.length, page: 1, pageSize: 200 } });
        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=play-all]").trigger("click");
        await flushPromises();

        expect(wrapper.findAll(".mock-play-window")).toHaveLength(3);
        await wrapper.get("[data-test=stop-all]").trigger("click");
        expect(wrapper.findAll(".mock-play-window")).toHaveLength(0);
        expect(wrapper.findAll(".unplayed-cover")).toHaveLength(4);
        expect(wrapper.findAll(".slot-channel-name")).toHaveLength(0);
        expect(playback.stopPlay).not.toHaveBeenCalled();
    });

    it("polls channel groups by the active layout and stops polling with stop all", async () => {
        vi.useFakeTimers();
        const channels = Array.from({ length: 5 }, (_, index) => ({
            id: index + 11,
            channelId: `poll-channel-${index + 1}`,
            deviceId: `poll-device-${index + 1}`,
            name: `轮询通道${index + 1}`,
            status: 1,
            audioEnabled: false
        }));
        api.listChannels.mockResolvedValue({ code: 0, data: { list: channels, total: channels.length, page: 1, pageSize: 200 } });

        try {
            const wrapper = mount(MultiScreenPlayback);
            await wrapper.get("[data-test=polling-settings]").trigger("click");
            await wrapper.get("[data-test=polling-enabled]").setValue(true);
            await wrapper.get("[data-test=polling-interval]").setValue("5");
            await wrapper.get("[data-test=save-polling]").trigger("click");
            await flushPromises();

            expect(api.listChannels).toHaveBeenCalledWith({ status: "online", page: 1, pageSize: 200 });
            expect(wrapper.findAll(".slot-channel-name").map(node => node.text())).toEqual([
                "轮询通道1", "轮询通道2", "轮询通道3", "轮询通道4"
            ]);
            expect(wrapper.get("[data-test=polling-settings]").classes()).toContain("active");

            await vi.advanceTimersByTimeAsync(5000);
            await flushPromises();
            expect(wrapper.findAll(".slot-channel-name").map(node => node.text())).toEqual([
                "轮询通道5", "轮询通道1", "轮询通道2", "轮询通道3"
            ]);

            await wrapper.get("[data-test=stop-all]").trigger("click");
            const callsAfterStop = playback.startPlay.mock.calls.length;
            await vi.advanceTimersByTimeAsync(5000);
            await flushPromises();
            expect(playback.startPlay).toHaveBeenCalledTimes(callsAfterStop);
            expect(wrapper.findAll(".unplayed-cover")).toHaveLength(4);
            expect(wrapper.get("[data-test=polling-settings]").classes()).not.toContain("active");
        } finally {
            vi.useRealTimers();
        }
    });

    it("provides fullscreen and polling controls", async () => {
        const requestFullscreen = vi.fn().mockResolvedValue(undefined);
        Object.defineProperty(HTMLElement.prototype, "requestFullscreen", {
            configurable: true,
            value: requestFullscreen
        });
        const wrapper = mount(MultiScreenPlayback);

        await wrapper.get("[data-test=fullscreen]").trigger("click");
        expect(requestFullscreen).toHaveBeenCalledTimes(1);
        expect(wrapper.find("[data-test=polling-settings]").exists()).toBe(true);
        expect(wrapper.find(".polling-settings").exists()).toBe(false);

        await wrapper.get("[data-test=polling-settings]").trigger("click");
        expect(wrapper.find(".polling-settings").exists()).toBe(true);
        const interval = wrapper.get<HTMLInputElement>("[data-test=polling-interval]");
        expect(interval.element.value).toBe("30");

        await interval.setValue("45");
        await wrapper.get("[data-test=save-polling]").trigger("click");
        expect(wrapper.find(".polling-settings").exists()).toBe(false);
        expect(wrapper.get("[role=status]").text()).toContain("45 秒");

        delete (HTMLElement.prototype as Partial<HTMLElement>).requestFullscreen;
    });
});
