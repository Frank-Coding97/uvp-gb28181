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
});
