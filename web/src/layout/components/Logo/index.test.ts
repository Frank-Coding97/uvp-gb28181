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
            systemConfig: ref({ systemName: "UVP 统一视频接入平台", systemLogo: "" })
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

describe("Logo", () => {
    beforeEach(() => setActivePinia(createPinia()));

    it("fills the 40px logo frame without an inset", () => {
        const wrapper = mount(Logo);

        expect(wrapper.find(".logo-svg").attributes("width")).toBe("40");
        expect(wrapper.find(".logo-svg").attributes("height")).toBe("40");
        expect(wrapper.find(".logo_mark").attributes("style")).toBeUndefined();
    });
});
