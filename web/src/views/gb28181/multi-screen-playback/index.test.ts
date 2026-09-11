import { flushPromises, mount } from "@vue/test-utils";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { createPinia, setActivePinia } from "pinia";
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
const workOrders = vi.hoisted(() => ({
    getActiveWorkOrder: vi.fn(),
    getWorkOrder: vi.fn(),
    stopWorkOrder: vi.fn(),
    workOrderDownloadUrl: vi.fn((id: string) => `/api/gb28181/work-orders/${id}/download?token=t`)

}));
const message = vi.hoisted(() => ({
    success: vi.fn(),
    info: vi.fn(),
    warning: vi.fn(),
    error: vi.fn()
}));
const account = vi.hoisted(() => ({ permissions: ["*:*:*"] as string[] }));

vi.mock("../device-mgmt/api", () => api);
vi.mock("@/api/gb28181", () => playback);
vi.mock("@/api/gb28181-work-recording", () => workOrders);
vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => ({ account }) }));
vi.mock("@arco-design/web-vue", () => ({ Message: message }));
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
                <button data-test="source-channel-audio" @click="$emit('select', {
                    id: 4, channelId: 'channel-4', deviceId: 'device-1', name: '南门', status: 1, audioEnabled: true
                })">南门</button>
                <button data-test="source-channel-alias" @click="$emit('select', {
                    id: 5, channelId: 'channel-5', deviceId: 'device-1', name: '北门', alias: '北门闸机', status: 1, audioEnabled: false
                })">北门</button>
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
        props: ["url", "zlmWebrtc", "hasAudio", "muted"],
        template:
            "<div class=\"mock-play-window\" :data-url=\"url\" :data-zlm-webrtc=\"String(Boolean(zlmWebrtc))\" :data-has-audio=\"String(Boolean(hasAudio))\" :data-muted=\"String(Boolean(muted))\" />"
    }
}));
// 作业单表单弹窗单独测试；页面测试只关心它拿到了哪些通道。
vi.mock("../work-orders/WorkOrderFormDialog.vue", () => ({
    default: {
        name: "WorkOrderFormDialog",
        props: ["visible", "channels", "canCreate"],
        template:
            '<div v-if="visible" data-test="work-order-form-dialog" :data-channels="(channels || []).map(c => c.id).join(\',\')" :data-labels="(channels || []).map(c => c.label).join(\',\')" />'
    }
}));
import MultiScreenPlayback from "./index.vue";
import type { ChannelVO } from "../device-mgmt/api";
import { usePlaybackConsoleStore } from "@/store/modules/playback-console";

/**
 * `<script setup>` 暴露的状态在类型上不可见，测试通过这个视图读取，
 * 避免 `wrapper.vm.xxx` 触发 TS2339（build:prod 会跑 vue-tsc）。
 */
interface DownloadPromptView {
    visible: boolean;
    waiting: boolean;
    ready: boolean;
    slices: number;
    channelCount: number;
}

function prompt(wrapper: { vm: unknown }): DownloadPromptView {
    return (wrapper.vm as unknown as { downloadPrompt: DownloadPromptView }).downloadPrompt;
}

describe("multi-screen playback page", () => {
    beforeEach(() => {
        account.permissions = ["*:*:*"];
        setActivePinia(createPinia());
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
        // 默认没有进行中的作业单；个别用例再覆盖。
        workOrders.getActiveWorkOrder.mockResolvedValue({ code: 0, data: null, message: "" });
        workOrders.stopWorkOrder.mockResolvedValue({
            code: 0,
            data: { id: "order-1", requestId: "request-1", state: "stopped", formState: "submitted", formVersion: 2, cameras: [] },
            message: ""
        });
        workOrders.getWorkOrder.mockResolvedValue({
            code: 0,
            data: { snapshot: { id: "order-1", requestId: "request-1", state: "stopped", formState: "submitted", formVersion: 2, cameras: [] } },
            message: ""
        });
        message.success.mockReset();
        message.info.mockReset();
        message.warning.mockReset();
        message.error.mockReset();
    });

    it("keeps the four-screen layout fixed without rendering the layout switcher", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();

        expect(wrapper.find('[aria-label="选择分屏布局"]').exists()).toBe(false);
        expect(wrapper.get('[aria-label="多屏播放格子"]').classes()).toContain("layout-4");
    });

    // 决策：只录「正在播放」的画面，没有正在播放的画面时按钮必须禁用并说明原因。
    it("keeps the recording button disabled until something is actually playing", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();

        const toggle = wrapper.get("[data-test=work-order-toggle]");
        expect(toggle.attributes("disabled")).toBeDefined();
        expect(toggle.attributes("title")).toBe("请先播放需要录制的画面");
        expect(wrapper.get("[data-test=work-order-status]").text()).toContain("没有进行中的作业单");

        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await flushPromises();

        const ready = wrapper.get("[data-test=work-order-toggle]");
        expect(ready.attributes("disabled")).toBeUndefined();
        expect(ready.text()).toContain("开始录像");
    });

    // 决策：多路画面同时出声会互相干扰，就算通道开启了音频，画面也必须静音启动。
    it("starts every screen muted even when the channel has audio enabled", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=source-channel-audio]").trigger("click");
        await flushPromises();

        const player = wrapper.get(".mock-play-window");
        expect(player.attributes("data-has-audio")).toBe("true");
        expect(player.attributes("data-muted")).toBe("true");
    });

    // 决策：画面与作业单用的通道名跟左侧设备树保持一致，统一优先取别名。
    it("labels screens and recording targets with the channel alias", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=source-channel-alias]").trigger("click");
        await flushPromises();

        expect(wrapper.get(".slot-channel-name").text()).toBe("北门闸机");
        await wrapper.get("[data-test=work-order-toggle]").trigger("click");
        await flushPromises();
        expect(wrapper.get("[data-test=work-order-form-dialog]").attributes("data-labels")).toBe("北门闸机");
    });

    // 决策：先填作业单再开录，所以按钮只负责打开表单，不直接开录。
    it("opens the work order form with exactly the playing channels", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await wrapper.get("[data-test=source-channel-2]").trigger("click");
        await flushPromises();

        await wrapper.get("[data-test=work-order-toggle]").trigger("click");
        await flushPromises();

        const dialog = wrapper.get("[data-test=work-order-form-dialog]");
        expect(dialog.attributes("data-channels")).toBe("1,2");
        expect(dialog.attributes("data-labels")).toBe("东门,西门");
        expect(workOrders.stopWorkOrder).not.toHaveBeenCalled();
    });

    it("opens the work order form only for channels that reached playing", async () => {
        playback.startPlay.mockImplementation(async (_deviceId: string, channelId: string) => (channelId === "channel-2"
            ? { code: 1, message: "点播失败", data: null }
            : { code: 0, data: { streamId: `stream-${channelId}`, ssrc: `ssrc-${channelId}`, app: "rtp", urls: { wsFlv: `ws://zlm/${channelId}.live.flv` }, wsflvUrl: `ws://zlm/${channelId}.live.flv`, httpFlvUrl: "", hlsUrl: "", expireAt: 0 } }));
        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await wrapper.get("[data-test=source-channel-2]").trigger("click");
        await flushPromises();

        await wrapper.get("[data-test=work-order-toggle]").trigger("click");
        expect(wrapper.get("[data-test=work-order-form-dialog]").attributes("data-channels")).toBe("1");
    });

    // 刷新后按钮状态必须来自服务端，否则会显示"未录制"而其实还在录。
    it("restores the recording button from the server after a reload", async () => {
        workOrders.getActiveWorkOrder.mockResolvedValue({
            code: 0,
            data: {
                id: "9b057ecd-215f-4caf-af40-6ca570ff1f62",
                requestId: "request-1",
                state: "recording",
                formState: "submitted",
                formVersion: 1,
                cameras: [{ channelId: 1, jobId: "job-1", state: "recording", fileState: "pending", startedAt: null, stoppedAt: null }]
            },
            message: ""
        });
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();

        const toggle = wrapper.get("[data-test=work-order-toggle]");
        // 即使当前没有任何画面在播，服务端有进行中作业单也必须能结束录像
        expect(toggle.text()).toContain("结束录像");
        expect(toggle.attributes("disabled")).toBeUndefined();
        const status = wrapper.get("[data-test=work-order-status]").text();
        expect(status).toContain("9b057ecd");
        expect(status).toContain("录像中");
        expect(status).toContain("1 路");
    });

    it("stops the running work order from the same button", async () => {
        workOrders.getActiveWorkOrder.mockResolvedValue({
            code: 0,
            data: { id: "order-1", requestId: "request-1", state: "recording", formState: "submitted", formVersion: 1, cameras: [] },
            message: ""
        });
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();

        await wrapper.get("[data-test=work-order-toggle]").trigger("click");
        await flushPromises();

        expect(workOrders.stopWorkOrder).toHaveBeenCalledWith("order-1");
        expect(message.success).toHaveBeenCalled();
    });

    it("falls back to the server state when stopping fails", async () => {
        workOrders.getActiveWorkOrder.mockResolvedValue({
            code: 0,
            data: { id: "order-1", requestId: "request-1", state: "recording", formState: "submitted", formVersion: 1, cameras: [] },
            message: ""
        });
        workOrders.stopWorkOrder.mockRejectedValue(new Error("网络超时"));
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();

        await wrapper.get("[data-test=work-order-toggle]").trigger("click");
        await flushPromises();

        expect(wrapper.get("[data-test=work-order-error]").text()).toContain("网络超时");
        // 失败后仍以服务端为准，避免界面停在错误状态
        expect(workOrders.getActiveWorkOrder.mock.calls.length).toBeGreaterThan(1);
    });

    /** 已结束、带一段已归档分片的作业单快照。 */
    const stoppedOrderWithSlice = (files: Array<Record<string, unknown>>) => ({
        id: "order-1",
        requestId: "request-1",
        state: "stopped",
        formState: "submitted",
        formVersion: 2,
        projectName: "沪宁线放线作业",
        cameras: [
            {
                channelId: 12,
                channelName: "K12+300 左线",
                jobId: "job-1",
                state: "stopped",
                fileState: "ready",
                startedAt: null,
                stoppedAt: null,
                files
            }
        ]
    });

    // 结束录像后顺势问一句「是否立即下载」，省掉再跑一趟作业单列表。
    it("asks whether to download the recording right after stopping", async () => {
        const stopped = stoppedOrderWithSlice([{ id: 88, channelId: 12, fileName: "12.mp4", fileSize: 1048576, state: "ready" }]);
        workOrders.getActiveWorkOrder
            .mockResolvedValueOnce({ code: 0, data: { ...stopped, state: "recording" }, message: "" })
            .mockResolvedValue({ code: 0, data: null, message: "" });
        workOrders.stopWorkOrder.mockResolvedValue({ code: 0, data: stopped, message: "" });
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();

        await wrapper.get("[data-test=work-order-toggle]").trigger("click");
        await flushPromises();

        expect(prompt(wrapper).visible).toBe(true);
        expect(prompt(wrapper).waiting).toBe(false);
        expect(prompt(wrapper).slices).toBe(1);
        const promptText = wrapper.get("[data-test=work-order-download]").text();
        expect(promptText).toContain("是否立即下载");
        expect(promptText).toContain("1 个分片");
    });

    it("downloads the archive straight away when the operator confirms", async () => {
        const stopped = stoppedOrderWithSlice([{ id: 88, channelId: 12, fileName: "12.mp4", fileSize: 1048576, state: "ready" }]);
        workOrders.getActiveWorkOrder
            .mockResolvedValueOnce({ code: 0, data: { ...stopped, state: "recording" }, message: "" })
            .mockResolvedValue({ code: 0, data: null, message: "" });
        workOrders.stopWorkOrder.mockResolvedValue({ code: 0, data: stopped, message: "" });
        const open = vi.spyOn(window, "open").mockImplementation(() => null);
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();

        await wrapper.get("[data-test=work-order-toggle]").trigger("click");
        await flushPromises();
        await wrapper.get("[data-test=work-order-download-now]").trigger("click");

        expect(open).toHaveBeenCalledWith("/api/gb28181/work-orders/order-1/download?token=t", "_blank", "noopener");
        expect(prompt(wrapper).visible).toBe(false);
        open.mockRestore();
    });

    // 只要有一路没结束，后端打包必然 409，所以这里不给「立即下载」。
    it("withholds the download until every camera has stopped", async () => {
        const stopped = stoppedOrderWithSlice([{ id: 88, channelId: 12, fileName: "12.mp4", fileSize: 1048576, state: "ready" }]);
        stopped.cameras[0].state = "unknown";
        workOrders.getActiveWorkOrder
            .mockResolvedValueOnce({ code: 0, data: { ...stopped, state: "recording" }, message: "" })
            .mockResolvedValue({ code: 0, data: null, message: "" });
        workOrders.stopWorkOrder.mockResolvedValue({ code: 0, data: stopped, message: "" });
        const open = vi.spyOn(window, "open").mockImplementation(() => null);
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();

        await wrapper.get("[data-test=work-order-toggle]").trigger("click");
        await flushPromises();

        expect(prompt(wrapper).ready).toBe(false);
        expect(wrapper.get("[data-test=work-order-download]").text()).toContain("部分通道尚未结束");
        expect(wrapper.get("[data-test=work-order-download-now]").attributes("disabled")).toBeDefined();
        await wrapper.get("[data-test=work-order-download-now]").trigger("click");
        expect(open).not.toHaveBeenCalled();
        open.mockRestore();
    });

    // 分片还没入库时（ZLM 停止录制那一刻才落 MP4）短暂等待，等不到就引导去作业单页。
    it("waits for archiving and then points at the work-order page", async () => {
        vi.useFakeTimers();
        try {
            const stopped = stoppedOrderWithSlice([]);
            workOrders.getActiveWorkOrder
                .mockResolvedValueOnce({ code: 0, data: { ...stopped, state: "recording" }, message: "" })
                .mockResolvedValue({ code: 0, data: null, message: "" });
            workOrders.stopWorkOrder.mockResolvedValue({ code: 0, data: stopped, message: "" });
            workOrders.getWorkOrder.mockResolvedValue({ code: 0, data: { snapshot: stopped }, message: "" });
            const wrapper = mount(MultiScreenPlayback);
            await flushPromises();

            await wrapper.get("[data-test=work-order-toggle]").trigger("click");
            await flushPromises();
            expect(prompt(wrapper).waiting).toBe(true);

            await vi.advanceTimersByTimeAsync(1500 * 5 + 100);
            await flushPromises();

            expect(prompt(wrapper).waiting).toBe(false);
            expect(wrapper.get("[data-test=work-order-download]").text()).toContain("稍后在「作业单」页面下载");
            expect(wrapper.get("[data-test=work-order-download-now]").attributes("disabled")).toBeDefined();
        } finally {
            vi.useRealTimers();
        }
    });


    it("does not stop a work recording when a window, all windows, or the page is removed", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await flushPromises();
        await wrapper.get("[data-test=slot-remove-0]").trigger("click");
        await wrapper.get("[data-test=stop-all]").trigger("click");
        wrapper.unmount();

        expect(workOrders.stopWorkOrder).not.toHaveBeenCalled();
    });

    it("renders four stable slots and the dedicated playback source tree", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();

        expect(wrapper.find("[data-test=playback-source-tree]").exists()).toBe(true);
        expect(wrapper.findAll("[data-test=screen-slot]")).toHaveLength(4);
        expect(wrapper.findAll(".unplayed-cover")).toHaveLength(4);
        expect(wrapper.find('[aria-label="选择分屏布局"]').exists()).toBe(false);
        expect(wrapper.find("[data-test=my-favorites] .lucide-star").exists()).toBe(true);
    });

    it("keeps guest batch viewing and polling while hiding favorite persistence and PTZ", async () => {
        account.permissions = [
            "gb28181:device:view",
            "gb28181:play:start",
            "gb28181:play:monitor",
            "gb28181:device-record:query",
            "gb28181:device-record:play"
        ];
        const wrapper = mount(MultiScreenPlayback);
        await flushPromises();

        expect(wrapper.find("[data-test=play-all]").exists()).toBe(true);
        expect(wrapper.find("[data-test=polling-settings]").exists()).toBe(true);
        expect(wrapper.find("[data-test=my-favorites]").exists()).toBe(false);
        expect(wrapper.find(".basic-ptz").exists()).toBe(false);

        api.listChannels.mockResolvedValue({
            code: 0,
            data: {
                list: [{ id: 11, channelId: "channel-1", deviceId: "device-1", name: "东门", status: 1, audioEnabled: false }],
                total: 1,
                page: 1,
                pageSize: 200
            }
        });
        await wrapper.get("[data-test=play-all]").trigger("click");
        await flushPromises();
        expect(api.listChannels).toHaveBeenCalled();
        expect(playback.startPlay).toHaveBeenCalled();
        wrapper.unmount();
    });

    it("keeps the monitor toolbar clickable when the mobile workspace shrinks", () => {
        const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/multi-screen-playback/index.vue"), "utf8");

        expect(source).toMatch(/\.monitor-toolbar\s*\{[^}]*flex:\s*0 0 auto;/s);
    });

    it("keeps the desktop polling countdown content inside its status button", () => {
        const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/multi-screen-playback/index.vue"), "utf8");

        expect(source).toMatch(/\.playback-actions button\.polling-control\.counting\s*\{[^}]*width:\s*44px;[^}]*flex-shrink:\s*0;/s);
        expect(source).not.toContain("class=\"countdown-label\"");
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

    it("opens the singleton playback console and leaves it alive when this route unmounts", async () => {
        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=source-channel-1]").trigger("click");
        await flushPromises();

        await wrapper.get("[data-test=slot-console-0]").trigger("click");
        const consoleStore = usePlaybackConsoleStore();
        expect(consoleStore.visible).toBe(true);
        expect(consoleStore.channel).toEqual(expect.objectContaining({ channelId: "channel-1" }));

        wrapper.unmount();
        expect(consoleStore.visible).toBe(true);
        expect(consoleStore.channel?.channelId).toBe("channel-1");
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

        expect(message.info).toHaveBeenCalledWith("请先播放至少一路通道，再收藏当前播放通道");
    });

    it("shows a system message after saving a favorite group", async () => {
        const wrapper = mount(MultiScreenPlayback);

        await wrapper.get("[data-test=favorite-saved]").trigger("click");

        expect(message.success).toHaveBeenCalledWith("已将 1 个通道加入收藏组“重点设备”");
        expect(wrapper.find(".workspace-toast").exists()).toBe(false);
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
        expect(message.success).toHaveBeenCalledWith("已播放收藏组“重点通道”的 1 路通道");
    });

    it("keeps four visible playback slots", async () => {
        const wrapper = mount(MultiScreenPlayback);

        expect(wrapper.findAll("[data-test=screen-slot]")).toHaveLength(4);
        expect(wrapper.get("[aria-label=多屏播放格子]").classes()).toContain("layout-4");
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

        expect(wrapper.get("[data-test=play-all]").attributes("disabled")).toBeUndefined();
        await wrapper.get("[data-test=play-all]").trigger("click");
        await flushPromises();

        expect(api.listChannels).toHaveBeenCalledWith({ status: "online", page: 1, pageSize: 200 });
        expect(wrapper.findAll(".mock-play-window")).toHaveLength(4);
        expect(wrapper.findAll(".unplayed-cover")).toHaveLength(0);
        expect(wrapper.findAll(".slot-channel-name").map(node => node.text())).toEqual([
            "在线通道1", "在线通道2", "在线通道3", "在线通道4"
        ]);
        expect(message.error).not.toHaveBeenCalled();
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
            expect(wrapper.get("[data-test=polling-countdown]").attributes("data-remaining-seconds")).toBe("5");
            expect(wrapper.get("[data-test=polling-countdown]").attributes("aria-label")).toBe("距离下一轮轮询还有 5 秒");

            await vi.advanceTimersByTimeAsync(1000);
            expect(wrapper.get("[data-test=polling-countdown]").attributes("data-remaining-seconds")).toBe("4");

            await vi.advanceTimersByTimeAsync(4000);
            await flushPromises();
            expect(wrapper.findAll(".slot-channel-name").map(node => node.text())).toEqual([
                "轮询通道5", "轮询通道1", "轮询通道2", "轮询通道3"
            ]);
            expect(wrapper.get("[data-test=polling-countdown]").attributes("data-remaining-seconds")).toBe("5");

            await wrapper.get("[data-test=stop-all]").trigger("click");
            const callsAfterStop = playback.startPlay.mock.calls.length;
            await vi.advanceTimersByTimeAsync(5000);
            await flushPromises();
            expect(playback.startPlay).toHaveBeenCalledTimes(callsAfterStop);
            expect(wrapper.findAll(".unplayed-cover")).toHaveLength(4);
            expect(wrapper.get("[data-test=polling-settings]").classes()).not.toContain("active");
            expect(wrapper.find("[data-test=polling-countdown]").exists()).toBe(false);
        } finally {
            vi.useRealTimers();
        }
    });

    it("immediately replaces a failed polling channel without waiting for the interval", async () => {
        const channels = Array.from({ length: 5 }, (_, index) => ({
            id: index + 31,
            channelId: `fallback-channel-${index + 1}`,
            deviceId: `fallback-device-${index + 1}`,
            name: `补位通道${index + 1}`,
            status: 1,
            audioEnabled: false
        }));
        api.listChannels.mockResolvedValue({ code: 0, data: { list: channels, total: channels.length, page: 1, pageSize: 200 } });
        playback.startPlay.mockImplementation(async (_deviceId: string, channelId: string) => channelId === "fallback-channel-1"
            ? { code: 500, message: "点播失败" }
            : {
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
            });

        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=polling-settings]").trigger("click");
        await wrapper.get("[data-test=polling-enabled]").setValue(true);
        await wrapper.get("[data-test=polling-interval]").setValue("30");
        await wrapper.get("[data-test=save-polling]").trigger("click");
        await flushPromises();

        expect(playback.startPlay).toHaveBeenCalledTimes(5);
        expect(playback.startPlay).toHaveBeenNthCalledWith(
            1,
            "fallback-device-1",
            "fallback-channel-1",
            { silent: true }
        );
        expect(message.warning).toHaveBeenCalledWith(
            "“补位通道1”点播失败，已跳过该通道，继续点播下一个通道"
        );
        expect(message.error).not.toHaveBeenCalled();
        expect(wrapper.findAll(".slot-channel-name").map(node => node.text())).toEqual([
            "补位通道5", "补位通道2", "补位通道3", "补位通道4"
        ]);
        expect(wrapper.findAll(".mock-play-window")).toHaveLength(4);
    });

    it("stops polling from the active toolbar button without opening the settings dialog", async () => {
        vi.useFakeTimers();
        const channels = Array.from({ length: 5 }, (_, index) => ({
            id: index + 61,
            channelId: `toggle-channel-${index + 1}`,
            deviceId: `toggle-device-${index + 1}`,
            name: `切换通道${index + 1}`,
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
            const callsBeforeStop = playback.startPlay.mock.calls.length;

            await wrapper.get("[data-test=polling-settings]").trigger("click");
            await flushPromises();

            expect(wrapper.find(".polling-settings").exists()).toBe(false);
            expect(wrapper.get("[data-test=polling-settings]").classes()).not.toContain("active");
            expect(wrapper.find("[data-test=polling-countdown]").exists()).toBe(false);
            expect(wrapper.findAll(".slot-channel-name").map(node => node.text())).toEqual([
                "切换通道1", "切换通道2", "切换通道3", "切换通道4"
            ]);
            expect(message.info).toHaveBeenCalledWith("轮询已停止");

            await vi.advanceTimersByTimeAsync(5000);
            await flushPromises();
            expect(playback.startPlay).toHaveBeenCalledTimes(callsBeforeStop);
        } finally {
            vi.useRealTimers();
        }
    });

    it("tries each polling channel at most once per cycle when all playback requests fail", async () => {
        vi.useFakeTimers();
        const channels = Array.from({ length: 5 }, (_, index) => ({
            id: index + 41,
            channelId: `failed-channel-${index + 1}`,
            deviceId: `failed-device-${index + 1}`,
            name: `失败通道${index + 1}`,
            status: 1,
            audioEnabled: false
        }));
        api.listChannels.mockResolvedValue({ code: 0, data: { list: channels, total: channels.length, page: 1, pageSize: 200 } });
        playback.startPlay.mockResolvedValue({ code: 500, message: "点播失败" });

        try {
            const wrapper = mount(MultiScreenPlayback);
            await wrapper.get("[data-test=polling-settings]").trigger("click");
            await wrapper.get("[data-test=polling-enabled]").setValue(true);
            await wrapper.get("[data-test=polling-interval]").setValue("30");
            await wrapper.get("[data-test=save-polling]").trigger("click");
            await flushPromises();

            expect(playback.startPlay).toHaveBeenCalledTimes(5);
            await vi.advanceTimersByTimeAsync(29_999);
            expect(playback.startPlay).toHaveBeenCalledTimes(5);

            await vi.advanceTimersByTimeAsync(1);
            await flushPromises();
            expect(playback.startPlay).toHaveBeenCalledTimes(10);
        } finally {
            vi.useRealTimers();
        }
    });

    it("does not retry another polling channel after stop all interrupts an in-flight cycle", async () => {
        const channels = Array.from({ length: 5 }, (_, index) => ({
            id: index + 51,
            channelId: `interrupt-channel-${index + 1}`,
            deviceId: `interrupt-device-${index + 1}`,
            name: `中断通道${index + 1}`,
            status: 1,
            audioEnabled: false
        }));
        const resolvePlaybacks: Array<(value: { code: number; message: string }) => void> = [];
        api.listChannels.mockResolvedValue({ code: 0, data: { list: channels, total: channels.length, page: 1, pageSize: 200 } });
        playback.startPlay.mockImplementation(() => new Promise(resolve => {
            resolvePlaybacks.push(resolve);
        }));

        const wrapper = mount(MultiScreenPlayback);
        await wrapper.get("[data-test=polling-settings]").trigger("click");
        await wrapper.get("[data-test=polling-enabled]").setValue(true);
        await wrapper.get("[data-test=save-polling]").trigger("click");
        await flushPromises();
        expect(playback.startPlay).toHaveBeenCalledTimes(4);
        expect(wrapper.find("[data-test=polling-countdown]").exists()).toBe(false);
        expect(wrapper.get("[data-test=polling-settings]").attributes("aria-label")).toBe("停止轮询");
        expect(wrapper.find("[data-test=polling-starting]").exists()).toBe(true);

        await wrapper.get("[data-test=stop-all]").trigger("click");
        resolvePlaybacks.forEach(resolve => resolve({ code: 500, message: "点播失败" }));
        await flushPromises();

        expect(playback.startPlay).toHaveBeenCalledTimes(4);
        expect(wrapper.findAll(".unplayed-cover")).toHaveLength(4);
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
        expect(message.success).toHaveBeenCalledWith("轮询设置已保存，间隔 45 秒");

        delete (HTMLElement.prototype as Partial<HTMLElement>).requestFullscreen;
    });
});
