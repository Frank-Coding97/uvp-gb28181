import { flushPromises, mount } from "@vue/test-utils";
import { reactive } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import PlayWindow from "./PlayWindow.vue";
import playWindowSource from "./PlayWindow.vue?raw";

const userState = vi.hoisted(() => ({
    account: { permissions: [] as string[] }
}));

vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => userState }));

describe("PlayWindow playback embedding", () => {
    beforeEach(() => {
        userState.account = reactive({ permissions: ["*:*:*"] });
    });

    afterEach(() => {
        delete (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro;
    });

    it("keeps the original EasyPlayer controls visible in playback mode", async () => {
        const options: Record<string, unknown>[] = [];
        class FakeEasyPlayer {
            constructor(_element: HTMLElement, value: Record<string, unknown>) {
                options.push(value);
            }
            on = vi.fn();
            play = vi.fn();
            destroy = vi.fn();
        }
        (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

        const wrapper = mount(PlayWindow, {
            props: { url: "ws://zlm/playback.live.flv", playback: true, hasAudio: false }
        });
        await flushPromises();

        expect(wrapper.classes()).toContain("playback");
        expect(options[0]).toMatchObject({
            isBand: true,
            btns: { fullscreen: true, screenshot: true, play: true, audio: true, record: false, stretch: true }
        });
        expect(playWindowSource).not.toContain(".play-window.playback :deep(.easyplayer-controls)");
        expect(playWindowSource).not.toContain(".play-window.playback :deep(.easyplayer-zoom-controls)");
    });

    it("hides EasyPlayer screenshot control when the user lacks snapshot permission", async () => {
        userState.account.permissions = ["gb28181:play:start"];
        const options: Record<string, any>[] = [];
        class FakeEasyPlayer {
            constructor(_element: HTMLElement, value: Record<string, any>) {
                options.push(value);
            }
            on = vi.fn();
            play = vi.fn();
            destroy = vi.fn();
        }
        (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

        mount(PlayWindow, { props: { url: "ws://zlm/live.flv" } });
        await flushPromises();

        expect(options[0]).toMatchObject({ btns: { screenshot: false } });
    });

    it("rebuilds the player when snapshot permission changes", async () => {
        const options: Record<string, any>[] = [];
        const instances: FakeEasyPlayer[] = [];
        class FakeEasyPlayer {
            constructor(_element: HTMLElement, value: Record<string, any>) {
                options.push(value);
                instances.push(this);
            }
            on = vi.fn();
            play = vi.fn();
            destroy = vi.fn();
        }
        (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

        const wrapper = mount(PlayWindow, { props: { url: "ws://zlm/live.flv", hasAudio: false } });
        await flushPromises();
        expect(options[0]).toMatchObject({ hasAudio: false, btns: { screenshot: true } });

        userState.account.permissions = ["gb28181:play:start"];
        await flushPromises();
        expect(instances[0].destroy).toHaveBeenCalledTimes(1);
        expect(options[1]).toMatchObject({ hasAudio: false, btns: { screenshot: false } });
        expect(instances[1].play).toHaveBeenCalledWith("ws://zlm/live.flv");

        userState.account.permissions = ["gb28181:play:start", "gb28181:play:snapshot"];
        await flushPromises();
        expect(instances[1].destroy).toHaveBeenCalledTimes(1);
        expect(options[2]).toMatchObject({ hasAudio: false, btns: { screenshot: true } });
        expect(instances[2].play).toHaveBeenCalledWith("ws://zlm/live.flv");

        wrapper.unmount();
    });

    it("enables the ZLM WebRTC adapter only when requested by the caller", async () => {
        const options: Record<string, unknown>[] = [];
        class FakeEasyPlayer {
            constructor(_element: HTMLElement, value: Record<string, unknown>) {
                options.push(value);
            }
            on = vi.fn();
            play = vi.fn();
            destroy = vi.fn();
        }
        (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

        const wrapper = mount(PlayWindow, {
            props: {
                url: "webrtc://zlm:18080/index/api/webrtc?app=rtp&stream=stream-1&type=play",
                zlmWebrtc: true
            }
        });
        await flushPromises();

        expect(options[0]).toMatchObject({ isRtcZLM: true });
        wrapper.unmount();
    });

    it("forwards EasyPlayer time and loading events", async () => {
        const listeners: Record<string, (value?: unknown) => void> = {};
        class FakeEasyPlayer {
            constructor(_element: HTMLElement, _value: Record<string, unknown>) {}
            on = vi.fn((event: string, callback: (value?: unknown) => void) => {
                listeners[event] = callback;
            });
            play = vi.fn();
            destroy = vi.fn();
        }
        (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

        const wrapper = mount(PlayWindow, { props: { url: "ws://zlm/playback.live.flv" } });
        await flushPromises();

        listeners.timeUpdate?.(1234);
        listeners.loading?.(true);

        expect(wrapper.emitted("timeupdate")).toEqual([[1234]]);
        expect(wrapper.emitted("loading")).toEqual([[true]]);
    });

    it("reuses the EasyPlayer instance when the stream protocol URL changes", async () => {
        const instances: FakeEasyPlayer[] = [];
        class FakeEasyPlayer {
            on = vi.fn();
            play = vi.fn();
            destroy = vi.fn();

            constructor(_element: HTMLElement, _value: Record<string, unknown>) {
                instances.push(this);
            }
        }
        (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

        const wrapper = mount(PlayWindow, { props: { url: "ws://zlm/live.flv" } });
        await flushPromises();
        await wrapper.setProps({ url: "http://zlm/live.flv" });
        await flushPromises();

        expect(instances).toHaveLength(1);
        expect(instances[0].play).toHaveBeenNthCalledWith(1, "ws://zlm/live.flv");
        expect(instances[0].play).toHaveBeenNthCalledWith(2, "http://zlm/live.flv");
        expect(instances[0].destroy).not.toHaveBeenCalled();

        wrapper.unmount();
        expect(instances[0].destroy).toHaveBeenCalledTimes(1);
    });

    /**
     * 通道音频开关 → 播放器音频链路/控件/出声状态。
     * ⚠️ EasyPlayer 的 `isMute` 语义与命名相反（库内 `void 0!==e.isMute&&(t.isNotMute=e.isMute)`），
     * 传 true 才是「不静音」，所以通道开音频时它必须是 true。
     */
    function mountWithAudio(hasAudio?: boolean) {
        const options: Record<string, any>[] = [];
        const instances: any[] = [];
        class FakeEasyPlayer {
            constructor(_element: HTMLElement, value: Record<string, any>) {
                options.push(value);
                instances.push(this);
            }
            on = vi.fn();
            play = vi.fn().mockResolvedValue(undefined);
            destroy = vi.fn();
            setMute = vi.fn();
        }
        (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;
        const props: { url: string; hasAudio?: boolean } = { url: "ws://zlm/live.flv" };
        if (hasAudio !== undefined) props.hasAudio = hasAudio;
        return { options, instances, props };
    }

    it("开启音频的通道：建音频链路、播放后解锁静音并显示音频控件", async () => {
        const { options, instances, props } = mountWithAudio(true);

        const wrapper = mount(PlayWindow, { props });
        await flushPromises();

        expect(options[0]).toMatchObject({ hasAudio: true, isMute: true });
        expect(instances[0].setMute).toHaveBeenCalledWith(0);

        wrapper.unmount();
    });

    it("关闭音频的通道：不建音频链路、不出现音频控件、也不解锁静音", async () => {
        const { options, instances, props } = mountWithAudio(false);

        const wrapper = mount(PlayWindow, { props });
        await flushPromises();

        expect(options[0]).toMatchObject({ hasAudio: false, isMute: false });
        expect(instances[0].setMute).not.toHaveBeenCalled();

        wrapper.unmount();
    });

    it("未声明 hasAudio 时按无音频处理", async () => {
        const { options, instances, props } = mountWithAudio();

        const wrapper = mount(PlayWindow, { props });
        await flushPromises();

        expect(options[0]).toMatchObject({ hasAudio: false, isMute: false });
        expect(instances[0].setMute).not.toHaveBeenCalled();

        wrapper.unmount();
    });
});
