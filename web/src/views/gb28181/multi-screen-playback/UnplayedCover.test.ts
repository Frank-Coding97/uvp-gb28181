import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import UnplayedCover from "./UnplayedCover.vue";

describe("UnplayedCover", () => {
    it("以 UVP 品牌字标展示黑色待机画面", () => {
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
