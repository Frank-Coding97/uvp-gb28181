import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { createPinia, setActivePinia } from "pinia";
vi.mock("@/store/modules/sys-config", async () => {
    const { defineStore } = await import("pinia");
    const { ref } = await import("vue");
    return { useSysConfigStore: defineStore("sys-config", () => ({ systemConfig: ref({ systemBrand: "客户品牌", systemName: "客户视频平台" }) })) };
});
import { useSysConfigStore } from "@/store/modules/sys-config";
import UnplayedCover from "./UnplayedCover.vue";

describe("UnplayedCover", () => {
    it("clears the brand without restoring UVP", async () => {
        const wrapper = mount(UnplayedCover, { props: { index: 1 } });
        useSysConfigStore().systemConfig.systemBrand = "";
        await wrapper.vm.$nextTick();
        expect(wrapper.find(".cover-brand").exists()).toBe(false);
        expect(wrapper.text()).not.toContain("UVP");
    });
    beforeEach(() => setActivePinia(createPinia()));
    it("以配置的品牌字标展示黑色待机画面", () => {
        const wrapper = mount(UnplayedCover, { props: { index: 0 } });

        expect(wrapper.attributes("aria-label")).toBe("1号预览窗口，待接入");
        expect(wrapper.text()).toContain("窗口 01");
        expect(wrapper.text()).toContain("待接入");
        expect(wrapper.text()).toContain("客户品牌");
        expect(wrapper.text()).toContain("客户视频平台");
        expect(wrapper.text()).not.toContain("选择通道");
        expect(wrapper.find("svg").exists()).toBe(false);
    });
});
