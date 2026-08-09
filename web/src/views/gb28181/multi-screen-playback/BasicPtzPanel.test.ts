import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
    controlPtz: vi.fn(),
    getControlCapabilities: vi.fn(),
    fetchPTZDefaultSpeedConfig: vi.fn()
}));

vi.mock("@/api/gb28181", () => api);

import BasicPtzPanel from "./BasicPtzPanel.vue";

const onlineChannel = {
    id: 1,
    channelId: "channel-1",
    deviceId: "device-1",
    name: "东门",
    status: 1
} as any;

describe("BasicPtzPanel", () => {
    beforeEach(() => {
        api.getControlCapabilities.mockResolvedValue({
            code: 0,
            data: { basicPtz: { state: "supported", reason: "" } }
        });
        api.controlPtz.mockResolvedValue({ code: 0, data: { action: "accepted" } });
        api.fetchPTZDefaultSpeedConfig.mockResolvedValue({ code: 0, data: { level: 6 } });
    });

    it("collapses the controls while keeping the panel header available", async () => {
        const wrapper = mount(BasicPtzPanel, { props: { channel: null } });

        expect(wrapper.get(".ptz-content").isVisible()).toBe(true);
        await wrapper.get("[aria-label='收起云台控制']").trigger("click");

        expect(wrapper.get(".basic-ptz").classes()).toContain("collapsed");
        expect(wrapper.get("[aria-label='展开云台控制']").attributes("aria-expanded")).toBe("false");
        expect(wrapper.get(".ptz-content").attributes("style")).toContain("display: none");

        await wrapper.get("[aria-label='展开云台控制']").trigger("click");
        expect(wrapper.get(".ptz-content").attributes("style") || "").not.toContain("display: none");
    });

    it("disables controls without a focused online channel or when PTZ is explicitly unsupported", async () => {
        const wrapper = mount(BasicPtzPanel, { props: { channel: null } });
        expect(wrapper.get("[data-test=ptz-up]").attributes()).toHaveProperty("disabled");

        await wrapper.setProps({ channel: { ...onlineChannel, status: 0 } });
        await flushPromises();
        expect(wrapper.get("[data-test=ptz-up]").attributes()).toHaveProperty("disabled");

        api.getControlCapabilities.mockResolvedValueOnce({
            code: 0,
            data: { basicPtz: { state: "unsupported", reason: "not supported" } }
        });
        await wrapper.setProps({ channel: onlineChannel });
        await flushPromises();

        expect(wrapper.get("[data-test=ptz-up]").attributes()).toHaveProperty("disabled");
    });

    it("sends movement while held and stop when released", async () => {
        const wrapper = mount(BasicPtzPanel, { props: { channel: onlineChannel } });
        await flushPromises();

        await wrapper.get("[data-test=ptz-up]").trigger("pointerdown");
        expect(wrapper.emitted("actionChange")?.[0]).toEqual([{ channelId: onlineChannel.id, action: "up" }]);

        await wrapper.get("[data-test=ptz-up]").trigger("pointerup");

        expect(api.controlPtz).toHaveBeenNthCalledWith(1, onlineChannel.id, expect.objectContaining({ action: "up", speed: 153 }));
        expect(api.controlPtz).toHaveBeenNthCalledWith(2, onlineChannel.id, expect.objectContaining({ action: "stop" }));
        expect(wrapper.emitted("actionChange")?.[1]).toEqual([null]);
    });

    it("loads level ten and sends the full GB28181 speed byte", async () => {
        api.fetchPTZDefaultSpeedConfig.mockResolvedValueOnce({ code: 0, data: { level: 10 } });
        const wrapper = mount(BasicPtzPanel, { props: { channel: onlineChannel } });
        await flushPromises();

        expect(wrapper.get("input[type='range']").element).toHaveProperty("value", "10");
        await wrapper.get("[data-test=ptz-up]").trigger("pointerdown");

        expect(api.controlPtz).toHaveBeenCalledWith(
            onlineChannel.id,
            expect.objectContaining({ action: "up", speed: 255 })
        );
    });

    it("falls back to level six when the default speed cannot be loaded", async () => {
        api.fetchPTZDefaultSpeedConfig.mockRejectedValueOnce(new Error("network error"));
        const wrapper = mount(BasicPtzPanel, { props: { channel: onlineChannel } });
        await flushPromises();

        expect(wrapper.get("input[type='range']").element).toHaveProperty("value", "6");
        await wrapper.get("[data-test=ptz-up]").trigger("pointerdown");
        expect(api.controlPtz).toHaveBeenCalledWith(
            onlineChannel.id,
            expect.objectContaining({ action: "up", speed: 153 })
        );
    });

    it("stops an active action on channel change and window blur", async () => {
        const wrapper = mount(BasicPtzPanel, { props: { channel: onlineChannel } });
        await flushPromises();

        await wrapper.get("[data-test=ptz-up]").trigger("pointerdown");
        await wrapper.setProps({ channel: { ...onlineChannel, id: 2, channelId: "channel-2", name: "西门" } });
        await flushPromises();
        expect(api.controlPtz).toHaveBeenCalledWith(onlineChannel.id, expect.objectContaining({ action: "stop" }));

        await wrapper.get("[data-test=ptz-right]").trigger("pointerdown");
        window.dispatchEvent(new Event("blur"));
        await flushPromises();
        expect(api.controlPtz).toHaveBeenCalledWith(2, expect.objectContaining({ action: "stop" }));

        await wrapper.get("[data-test=ptz-right]").trigger("pointerdown");
        wrapper.unmount();
        await flushPromises();
        expect(api.controlPtz).toHaveBeenCalledWith(2, expect.objectContaining({ action: "stop" }));
    });
});
