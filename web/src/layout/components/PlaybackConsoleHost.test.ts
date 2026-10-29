import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/views/gb28181/components/PlayConsoleLinked.vue", () => ({
  default: {
    name: "PlayConsoleLinked",
    props: ["visible", "channel", "displayMode", "openSeq"],
    emits: ["update:visible", "update:displayMode"],
    template: `
      <div v-if="visible" data-testid="global-playback-console" :data-mode="displayMode" :data-open-seq="openSeq">
        <span>{{ channel?.name }}</span>
        <button data-testid="host-minimize" @click="$emit('update:displayMode', 'minimized')" />
        <button data-testid="host-close" @click="$emit('update:visible', false)" />
      </div>
    `
  }
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
    // ⚠️ 这里原本写死 `<PlaybackConsoleHost />`，而布局实际给了 `v-if="!mustChangePassword"`
    //    （强制改密时不该挂控制台）—— 断言跟着写法漂移成了假红。要守的是"挂在路由出口之外"，
    //    不是"标签里没有属性"。
    expect(layoutSource).toMatch(/<PlaybackConsoleHost\b[^>]*\/>/);

    const store = usePlaybackConsoleStore();
    const wrapper = mount(PlaybackConsoleHost, { global: { plugins: [pinia] } });
    store.open({ id: 1, deviceId: "device-1", channelId: "channel-1", name: "北门", status: 1 });
    await flushPromises();

    expect(wrapper.get("[data-testid='global-playback-console']").text()).toContain("北门");
    // 点播请求序号必须透传到控制台：控制台开着同一通道时，只有它能表达"这是新的一次点播"。
    expect(wrapper.get("[data-testid='global-playback-console']").attributes("data-open-seq")).toBe("1");
    store.open({ id: 1, deviceId: "device-1", channelId: "channel-1", name: "北门", status: 1 });
    await flushPromises();
    expect(wrapper.get("[data-testid='global-playback-console']").attributes("data-open-seq")).toBe("2");

    await wrapper.get("[data-testid='host-minimize']").trigger("click");
    expect(store.displayMode).toBe("minimized");

    await wrapper.get("[data-testid='host-close']").trigger("click");
    expect(store.visible).toBe(false);
    expect(store.channel).toBeNull();
  });
});
