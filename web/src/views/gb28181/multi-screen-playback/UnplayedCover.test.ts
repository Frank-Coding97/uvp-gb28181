import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { createPinia, setActivePinia } from "pinia";
vi.mock("@/store/modules/sys-config", async () => {
    const { defineStore } = await import("pinia");
    const { ref } = await import("vue");
    return { useSysConfigStore: defineStore("sys-config", () => ({ systemConfig: ref({ systemBrand: "客户品牌", systemName: "客户视频平台", playbackCover: "uvp" }) })) };
});
import { useSysConfigStore } from "@/store/modules/sys-config";
import UnplayedCover from "./UnplayedCover.vue";

describe("UnplayedCover", () => {
    it("switches to a neutral icon without UVP branding", async () => {
        const wrapper = mount(UnplayedCover, { props: { index: 1 } });
        useSysConfigStore().systemConfig.playbackCover = "icon";
        await wrapper.vm.$nextTick();
        expect(wrapper.find(".cover-brand").exists()).toBe(false);
        expect(wrapper.text()).not.toContain("UVP");
        expect(wrapper.find("svg").exists()).toBe(true);
        useSysConfigStore().systemConfig.playbackCover = "uvp";
        await wrapper.vm.$nextTick();
        expect(wrapper.text()).toContain("UVP");
        expect(wrapper.find("svg").exists()).toBe(false);
    });
    beforeEach(() => setActivePinia(createPinia()));
    it("默认恢复原来的 UVP 封面", () => {
        const wrapper = mount(UnplayedCover, { props: { index: 0 } });

        expect(wrapper.attributes("aria-label")).toBe("1号预览窗口，待接入");
        expect(wrapper.text()).toContain("窗口 01");
        expect(wrapper.text()).toContain("待接入");
        expect(wrapper.text()).toContain("UVP");
        expect(wrapper.text()).toContain("统一视频接入平台");
        expect(wrapper.text()).not.toContain("选择通道");
        expect(wrapper.find("svg").exists()).toBe(false);
    });
});
