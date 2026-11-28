import { readFileSync } from "node:fs";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import PlayWindow from "./PlayWindow.vue";

vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => ({ account: { permissions: [] } }) }));
vi.mock("./playerDecoderResources", () => ({ preparePlayerDecoder: async () => undefined }));

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.innerHTML = "";
});

it.each(["mediaSourceH265NotSupport", "webcodecsH265NotSupport", "webcodecsUnsupportedConfigurationError"])(
  "allows the bundled player's %s software fallback and reports only terminal failure",
  async code => {
    vi.stubGlobal(
      "Worker",
      class {
        onmessage: ((event: { data: unknown }) => void) | null = null;
        constructor() {
          queueMicrotask(() => this.onmessage?.({ data: { cmd: "init" } }));
        }
        postMessage = vi.fn();
        terminate = vi.fn();
      }
    );
    const source = readFileSync("public/easyplayer/EasyPlayer-pro.js", "utf8");
    const Player = new Function("module", "exports", `${source};return window.EasyPlayerPro;`)(undefined, undefined);
    const instances: any[] = [];
    vi.stubGlobal(
      "EasyPlayerPro",
      class extends Player {
        constructor(root: HTMLElement, options: unknown) {
          super(root, options);
          // Keep the real facade and error listeners; replace worker readiness and
          // network playback so this checks fallback ordering without a live device.
          vi.spyOn(this as any, "_loadend").mockReturnValue(true);
          vi.spyOn(this.player, "play").mockResolvedValue(undefined);
          instances.push(this);
        }
      }
    );
    const wrapper = mount(PlayWindow, { props: { url: "ws://media.example.test/live.flv" } });
    await flushPromises();
    const player = instances[0];
    const replay = vi.spyOn(player, "_replay").mockResolvedValue(undefined);
    const destroy = vi.spyOn(player, "destroy");
    try {
      player.player.emitError(code);
      await flushPromises();

      expect(wrapper.emitted("error")).toBeUndefined();
      expect(wrapper.emitted("playerError")).toBeUndefined();
      expect(destroy).not.toHaveBeenCalled();
      expect(replay).toHaveBeenCalledWith({ useMSE: false, useWCS: false });
      expect(wrapper.find('[data-testid="player-loading"]').exists()).toBe(true);

      player.emit("playFailedAndPaused", code);
      await flushPromises();

      expect(wrapper.emitted("error")).toEqual([[`EasyPlayer 错误: ${code}`]]);
      expect(wrapper.emitted("playerError")).toHaveLength(1);
      expect(destroy).toHaveBeenCalledOnce();
      expect(wrapper.find('[data-testid="player-loading"]').exists()).toBe(false);
      expect(wrapper.get(".err").text()).toContain(code);
    } finally {
      wrapper.unmount();
      await flushPromises();
    }
  }
);
