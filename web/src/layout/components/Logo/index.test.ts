import { createPinia, setActivePinia } from "pinia";
import { mount } from "@vue/test-utils";
import { computed } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.stubGlobal("computed", computed);

vi.mock("@/store/modules/theme-config", async () => {
    const { defineStore } = await import("pinia");
    const { ref } = await import("vue");
    return {
        useThemeConfig: defineStore("theme-config", () => ({
            collapsed: ref(false),
            asideDark: ref(false),
            layoutType: ref("layoutDefaults")
        }))
    };
});

vi.mock("@/store/modules/sys-config", async () => {
    const { defineStore } = await import("pinia");
    const { ref } = await import("vue");
    return {
        useSysConfigStore: defineStore("sys-config", () => ({
            systemConfig: ref({ systemBrand: "客户品牌", systemName: "客户视频平台", systemLogo: "" })
        }))
    };
});

vi.mock("@/utils/app", () => ({
    handleUrl: (url?: string) => url || ""
}));

vi.mock("@/components/s-logo/index.vue", () => ({
    default: {
        name: "LogoSvg",
        props: ["width", "height"],
        template: "<img class='logo-svg' :width='width' :height='height' />"
    }
}));

import Logo from "./index.vue";
import { useSysConfigStore } from "@/store/modules/sys-config";

describe("Logo", () => {
    beforeEach(() => setActivePinia(createPinia()));

    it("uses the configured brand and hides an empty brand", async () => {
        const wrapper = mount(Logo);
        expect(wrapper.find(".logo_title").text()).toBe("客户品牌");
        expect(wrapper.find(".logo_subtitle").text()).toBe("客户视频平台");
        expect(wrapper.text()).toContain("GB28181");
        useSysConfigStore().systemConfig.systemBrand = "  ";
        await wrapper.vm.$nextTick();
        expect(wrapper.find(".logo_title").exists()).toBe(false);
        expect(wrapper.text()).not.toContain("UVP");
    });

    it("fills the 40px logo frame without an inset", () => {
        const wrapper = mount(Logo);

        expect(wrapper.find(".logo-svg").attributes("width")).toBe("40");
        expect(wrapper.find(".logo-svg").attributes("height")).toBe("40");
        expect(wrapper.find(".logo_mark").attributes("style")).toBeUndefined();
    });
});
