import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";

import PlayConsole from "./PlayConsole.vue";

const channel = {
    id: 1,
    channelId: "0411212755",
    deviceId: "34020000001320000001",
    name: "园区北门",
    ptzType: 1,
    status: 1,
};

describe("PlayConsole 视频探针", () => {
    afterEach(() => {
        vi.useRealTimers();
    });

    it("按需执行 3 秒深度检测并展示 ZLM 可提供的逐帧指标", async () => {
        vi.useFakeTimers();
        const wrapper = mount(PlayConsole, {
            props: { visible: true, channel },
        });

        await vi.advanceTimersByTimeAsync(1500);
        await flushPromises();
        await wrapper.get("[data-testid='probe-tab']").trigger("click");

        const panel = wrapper.get("[data-testid='probe-panel']");
        expect(panel.text()).toContain("开始 3 秒检测");
        expect(panel.text()).toContain("等待检测");
        expect(panel.text()).toContain("帧到达抖动");
        expect(panel.text()).toContain("音视频交织");
        expect(panel.text()).not.toContain("观众数");
        expect(panel.text()).not.toContain("运行时长");
        expect(panel.text()).not.toContain("RTP 抖动");

        await panel.get("[data-testid='probe-start']").trigger("click");
        expect(panel.get("[data-testid='probe-start']").attributes("disabled")).toBeDefined();
        expect(panel.text()).toContain("检测中");

        await vi.advanceTimersByTimeAsync(3000);
        await flushPromises();

        expect(panel.text()).toContain("检测完成");
        expect(panel.text()).toContain("246帧");
        expect(panel.text()).toContain("H.264");
        expect(panel.text()).toContain("PCMA");
        wrapper.unmount();
    });
});
