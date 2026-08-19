import { flushPromises, mount } from "@vue/test-utils";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
    listChannels: vi.fn()
}));
const favoriteDialog = vi.hoisted(() => ({ opened: false, channels: [] as ChannelVO[] }));
const playback = vi.hoisted(() => ({
    startPlay: vi.fn(),
    stopPlay: vi.fn(),
    controlPtz: vi.fn(),
    getControlCapabilities: vi.fn()
}));

vi.mock("../device-mgmt/api", () => api);
vi.mock("@/api/gb28181", () => playback);
vi.mock("./PlaybackSourceTree.vue", () => ({
    default: {
        name: "PlaybackSourceTree",
        props: ["usedChannelIds", "view"],
        emits: ["select", "select-group", "favorite-saved"],
        setup(_props: unknown, { expose }: { expose: (value: unknown) => void }) {
            expose({ openFavoriteDialogForChannels: (channels: ChannelVO[]) => { favoriteDialog.opened = true; favoriteDialog.channels = channels; } });
            return { favoriteDialog };
        },
        template: `
            <div data-test="playback-source-tree" :data-view="view">
                <button data-test="source-channel-1" @click="$emit('select', {
                    id: 1, channelId: 'channel-1', deviceId: 'device-1', name: '东门', status: 1, audioEnabled: false
                })">东门</button>
                <button data-test="source-channel-2" @click="$emit('select', {
                    id: 2, channelId: 'channel-2', deviceId: 'device-2', name: '西门', status: 1, audioEnabled: false
                })">西门</button>
                <button data-test="favorite-group-1" @click="$emit('select-group', {
                    id: 'group-1', name: '重点通道', channels: [{ id: 3, channelId: 'channel-3', deviceId: 'device-1', name: '后门', status: 1, audioEnabled: false }]
                })">重点通道</button>
                <button data-test="favorite-saved" @click="$emit('favorite-saved', '重点设备', 1, 0)">收藏成功</button>
                <span v-if="favoriteDialog.opened" data-test="favorite-dialog-opened" />
            </div>
        `
    }
}));
vi.mock("../components/PlayWindow.vue", () => ({
    default: {
        name: "PlayWindow",
        props: ["url", "zlmWebrtc"],
        template: "<div class=\"mock-play-window\" :data-url=\"url\" :data-zlm-webrtc=\"String(Boolean(zlmWebrtc))\" />"
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
import type { ChannelVO } from "../device-mgmt/api";

describe("multi-screen playback page", () => {
    beforeEach(() => {
        api.listChannels.mockReset();
        favoriteDialog.opened = false;
        favoriteDialog.channels = [];
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
    });

    it("renders four stable slots and the dedicated playback source tree", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();

        expect(wrapper.find("[data-test=playback-source-tree]").exists()).toBe(true);
        expect(wrapper.findAll("[data-test=screen-slot]")).toHaveLength(4);
        expect(wrapper.findAll(".unplayed-cover")).toHaveLength(4);
        expect(wrapper.find("[data-test=layout-9]").exists()).toBe(true);
        expect(wrapper.find("[data-test=my-favorites] .lucide-star").exists()).toBe(true);
    });

    it("keeps the monitor toolbar clickable when the mobile workspace shrinks", () => {
        const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/multi-screen-playback/index.vue"), "utf8");

        expect(source).toMatch(/\.monitor-toolbar\s*\{[^}]*flex:\s*0 0 auto;/s);
    });

    it("fills the playback window on tall large-screen slots", () => {
        const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/multi-screen-playback/index.vue"), "utf8");

        expect(source).toMatch(/\.slot-body :deep\(\.play-window\)\s*\{[^}]*height:\s*100%;[^}]*aspect-ratio:\s*auto;/s);
        expect(source).toMatch(/\.slot-body :deep\(\.play-window video\)\s*\{[^}]*object-fit:\s*contain !important;/s);
    });

    it("overlays the channel header on hover and removes the footer layout", async () => {
        const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/multi-screen-playback/index.vue"), "utf8");

        expect(source).toMatch(/\.slot-topline\s*\{[^}]*position:\s*absolute;[^}]*z-index:\s*6;/s);
        expect(source).toMatch(/\.slot-topline\s*\{[^}]*min-height:\s*28px;[^}]*background:\s*rgb\(15 23 42 \/ 72%\);/s);
        expect(source).toMatch(/\.screen-slot:hover \.slot-topline, \.screen-slot:focus-within \.slot-topline/);
        expect(source).not.toContain("<footer class=\"slot-footer\">");
        expect(source).toContain('class="slot-node-name">节点 {{ slot.result.node.name }}</span>');
        expect(source).toContain('<SlidersHorizontal :size="15" aria-hidden="true" />');
        expect(source).not.toContain('aria-label="打开通道控制台" title="打开通道控制台" @click.stop="openConsole(slot)"><Maximize2');

        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await flushPromises();
        expect(wrapper.find(".slot-footer").exists()).toBe(false);
    });

    it("shows the media node name in the floating channel header", async () => {
        playback.startPlay.mockResolvedValueOnce({
            code: 0,
            data: {
                streamId: "stream-channel-1",
                ssrc: "ssrc-channel-1",
                app: "rtp",
                node: { name: "zlm-220" },
                urls: { wsFlv: "ws://zlm/channel-1.live.flv" },
                wsflvUrl: "ws://zlm/channel-1.live.flv",
                httpFlvUrl: "",
                hlsUrl: "",
                expireAt: 0
            }
        });
        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await flushPromises();

        expect(wrapper.get(".slot-node-name").text()).toBe("节点 zlm-220");
    });

    it("opens the favorite group dialog for current playback without changing playback", async () => {
        const wrapper = mount(MultiScreenPlayback);
        expect(wrapper.get("[data-test=my-favorites]").attributes("aria-label")).toBe("收藏当前播放通道");
        expect(wrapper.get("[data-test=my-favorites]").attributes("title")).toBe("收藏当前播放通道");
        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await flushPromises();
        const callsBeforeOpen = playback.startPlay.mock.calls.length;

        await wrapper.get("[data-test=my-favorites]").trigger("click");
        await flushPromises();
        expect(favoriteDialog.opened).toBe(true);
        expect(favoriteDialog.channels).toEqual([expect.objectContaining({ channelId: "channel-1" })]);
        expect(wrapper.get("[data-test=playback-source-tree]").attributes("data-view")).toBe("devices");
        expect(playback.startPlay).toHaveBeenCalledTimes(callsBeforeOpen);
        expect(wrapper.findAll(".slot-channel-name").map(node => node.text())).toEqual(["东门"]);
    });

    it("asks for a playback channel before opening the favorite group dialog", async () => {
        const wrapper = mount(MultiScreenPlayback);

        await wrapper.get("[data-test=my-favorites]").trigger("click");

        expect(wrapper.text()).toContain("请先播放至少一路通道，再收藏当前播放通道");
    });

    it("shows a success toast after saving a favorite group", async () => {
        const wrapper = mount(MultiScreenPlayback);

        await wrapper.get("[data-test=favorite-saved]").trigger("click");

        expect(wrapper.get(".workspace-toast").text()).toBe("已将 1 个通道加入收藏组“重点设备”");
    });

    it("plays all channels from a favorite channel group", async () => {
        api.listChannels.mockResolvedValue({
            code: 0,
            data: { list: [{ id: 3, channelId: "channel-3", deviceId: "device-1", name: "后门", status: 1, audioEnabled: false }] }
        });
        const wrapper = mount(MultiScreenPlayback);

        await wrapper.get("[data-test=favorite-group-1]").trigger("click");
        await flushPromises();

        expect(api.listChannels).not.toHaveBeenCalled();
        expect(playback.startPlay).toHaveBeenCalledWith("device-1", "channel-3");
        expect(wrapper.findAll(".slot-channel-name").map(node => node.text())).toEqual(["后门"]);
        expect(wrapper.text()).toContain("已播放收藏组“重点通道”");
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

    it("keeps an independent protocol snapshot for every new slot", async () => {
        playback.startPlay.mockImplementation(async (_deviceId: string, channelId: string) => channelId === "channel-1"
            ? {
                code: 0,
                data: {
                    streamId: "stream-rtc", ssrc: "1", app: "rtp",
                    defaultProtocol: "webrtc", protocol: "webrtc",
                    url: "http://zlm/index/api/webrtc?stream=one", zlmWebrtc: true,
                    urls: { webrtc: "http://zlm/index/api/webrtc?stream=one" },
                    wsflvUrl: "", httpFlvUrl: "", hlsUrl: "", expireAt: 0
                }
            }
            : {
                code: 0,
                data: {
                    streamId: "stream-flv", ssrc: "2", app: "rtp",
                    defaultProtocol: "ws-flv", protocol: "ws-flv",
                    url: "ws://zlm/two.live.flv", zlmWebrtc: false,
                    urls: { wsFlv: "ws://zlm/two.live.flv" },
                    wsflvUrl: "", httpFlvUrl: "", hlsUrl: "", expireAt: 0
                }
            });
        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await wrapper.get("[data-test=source-channel-2]").trigger("click");
        await flushPromises();

        const players = wrapper.findAll(".mock-play-window");
        expect(players[0].attributes("data-url")).toBe("webrtc://zlm/index/api/webrtc?stream=one");
        expect(players[0].attributes("data-zlm-webrtc")).toBe("true");
        expect(players[1].attributes("data-url")).toBe("ws://zlm/two.live.flv");
        expect(players[1].attributes("data-zlm-webrtc")).toBe("false");
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

    it("exposes a dedicated close action for each playing slot", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await flushPromises();

        const closeButton = wrapper.get("[data-test=slot-remove-0]");
        expect(closeButton.attributes("aria-label")).toBe("关闭当前播放");
        expect(closeButton.attributes("title")).toBe("关闭当前播放");
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
