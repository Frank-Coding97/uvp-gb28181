import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/views/gb28181/components/PlayConsoleLinked.vue", () => ({
  default: {
    name: "PlayConsoleLinked",
    props: ["visible", "channel", "displayMode"],
    emits: ["update:visible", "update:displayMode"],
    template: `
      <div v-if="visible" data-testid="global-playback-console" :data-mode="displayMode">
        <span>{{ channel?.name }}</span>
        <button data-testid="host-minimize" @click="$emit('update:displayMode', 'minimized')" />
        <button data-testid="host-close" @click="$emit('update:visible', false)" />
      </div>
    `,
  },
}));

import PlaybackConsoleHost from "./PlaybackConsoleHost.vue";
import { usePlaybackConsoleStore } from "@/store/modules/playback-console";

describe("global playback console host", () => {
  let pinia: ReturnType<typeof createPinia>;

  beforeEach(() => {
    pinia = createPinia();
    setActivePinia(pinia);
  });

  it("挂在路由出口之外，并把最小化和关闭动作写回全局单例", async () => {
    const layoutSource = readFileSync(resolve(process.cwd(), "src/layout/index.vue"), "utf8");
    expect(layoutSource).toContain('import PlaybackConsoleHost from "@/layout/components/PlaybackConsoleHost.vue"');
    expect(layoutSource).toContain("<PlaybackConsoleHost />");

    const store = usePlaybackConsoleStore();
    const wrapper = mount(PlaybackConsoleHost, { global: { plugins: [pinia] } });
    store.open({ id: 1, deviceId: "device-1", channelId: "channel-1", name: "北门", status: 1 });
    await flushPromises();

    expect(wrapper.get("[data-testid='global-playback-console']").text()).toContain("北门");
    await wrapper.get("[data-testid='host-minimize']").trigger("click");
    expect(store.displayMode).toBe("minimized");

    await wrapper.get("[data-testid='host-close']").trigger("click");
    expect(store.visible).toBe(false);
    expect(store.channel).toBeNull();
  });
});
