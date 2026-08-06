import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({}));
const playback = vi.hoisted(() => ({
    startPlay: vi.fn(),
    stopPlay: vi.fn()
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
    });

    it("renders four stable slots and the dedicated playback source tree", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();

        expect(wrapper.find("[data-test=playback-source-tree]").exists()).toBe(true);
        expect(wrapper.findAll("[data-test=screen-slot]")).toHaveLength(4);
        expect(wrapper.findAll(".unplayed-cover")).toHaveLength(4);
        expect(wrapper.find("[data-test=layout-9]").exists()).toBe(true);
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

    it("removes one slot locally without stopping the shared channel stream", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();
        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await flushPromises();

        await wrapper.get("[data-test=slot-remove-0]").trigger("click");
        expect(wrapper.findAll(".slot-channel-name").map(node => node.text())).toEqual([]);
        expect(playback.stopPlay).not.toHaveBeenCalled();
    });

    it("stops and resumes all visible players without releasing shared streams", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await wrapper.get("[data-test=source-channel-2]").trigger("click");
        await flushPromises();

        expect(wrapper.findAll(".mock-play-window")).toHaveLength(2);
        await wrapper.get("[data-test=stop-all]").trigger("click");
        expect(wrapper.findAll(".mock-play-window")).toHaveLength(0);
        expect(wrapper.findAll(".slot-channel-name").map(node => node.text())).toEqual(["东门", "西门"]);
        expect(playback.stopPlay).not.toHaveBeenCalled();

        await wrapper.get("[data-test=play-all]").trigger("click");
        await flushPromises();
        expect(wrapper.findAll(".mock-play-window")).toHaveLength(2);
        expect(playback.startPlay).toHaveBeenCalledTimes(2);
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
