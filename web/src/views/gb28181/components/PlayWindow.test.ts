import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import PlayWindow from "./PlayWindow.vue";
import playWindowSource from "./PlayWindow.vue?raw";

describe("PlayWindow playback embedding", () => {
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
});
