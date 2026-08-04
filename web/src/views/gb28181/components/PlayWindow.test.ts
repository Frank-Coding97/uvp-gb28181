import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import PlayWindow from "./PlayWindow.vue";

describe("PlayWindow playback embedding", () => {
    afterEach(() => {
        delete (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro;
    });

    it("keeps EasyPlayer control nodes available and hides them with the playback shell", async () => {
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
    });
});
