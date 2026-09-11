import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
    listPlaybackSchemes: vi.fn(),
    getPlaybackScheme: vi.fn(),
    createPlaybackScheme: vi.fn(),
    renamePlaybackScheme: vi.fn(),
    replacePlaybackSchemeLayout: vi.fn(),
    deletePlaybackScheme: vi.fn()
}));

vi.mock("@/api/gb28181", () => api);

import PlaybackSchemePanel from "./PlaybackSchemePanel.vue";

const summary = { id: 9, name: "门岗值班", layoutSize: 4, slotCount: 2, updatedAt: "2026-08-07T15:00:00Z" };
const detail = {
    ...summary,
    slots: [
        { id: 1, slotIndex: 0, deviceCode: "device-1", channelCode: "channel-1", deviceName: "园区", channelName: "东门", availability: "available", channelRecordId: 11, channelStatus: 1, audioEnabled: false },
        { id: 2, slotIndex: 3, deviceCode: "device-2", channelCode: "channel-2", deviceName: "园区", channelName: "西门", availability: "offline", channelRecordId: 12, channelStatus: 0, audioEnabled: false }
    ]
};

describe("PlaybackSchemePanel", () => {
    beforeEach(() => {
        Object.values(api).forEach(mock => mock.mockReset());
        api.listPlaybackSchemes.mockResolvedValue({ code: 0, data: { list: [summary], total: 1, page: 1, pageSize: 10 } });
        api.getPlaybackScheme.mockResolvedValue({ code: 0, data: detail });
        api.createPlaybackScheme.mockResolvedValue({ code: 0, data: summary });
        api.renamePlaybackScheme.mockResolvedValue({ code: 0, data: summary });
        api.replacePlaybackSchemeLayout.mockResolvedValue({ code: 0, data: summary });
        api.deletePlaybackScheme.mockResolvedValue({ code: 0, data: { id: 9 } });
    });

    function mountPanel(slots = [{ slotIndex: 0, deviceCode: "device-1", channelCode: "channel-1" }]) {
        return mount(PlaybackSchemePanel, {
            props: { visible: true, currentLayout: 4, currentSlots: slots }
        });
    }

    it("loads a bounded first page and opens details without applying", async () => {
        const wrapper = mountPanel();
        await flushPromises();

        expect(api.listPlaybackSchemes).toHaveBeenCalledWith({ page: 1, pageSize: 10, q: undefined });
        await wrapper.get("[data-test=scheme-name-9]").trigger("click");
        await flushPromises();
        expect(api.getPlaybackScheme).toHaveBeenCalledWith(9);
        expect(wrapper.text()).toContain("槽位 1");
        expect(wrapper.text()).toContain("离线");
        expect(wrapper.emitted("apply")).toBeUndefined();

        await wrapper.get("[data-test=apply-detail]").trigger("click");
        expect(wrapper.emitted("apply")?.[0]).toEqual([detail]);
    });

    it("saves the current non-empty layout and refreshes the list", async () => {
        const wrapper = mountPanel();
        await flushPromises();

        await wrapper.get("[data-test=new-scheme]").trigger("click");
        await wrapper.get("[data-test=scheme-name-input]").setValue(" 夜班 ");
        await wrapper.get("[data-test=confirm-save]").trigger("click");
        await flushPromises();

        expect(api.createPlaybackScheme).toHaveBeenCalledWith({
            name: "夜班",
            layoutSize: 4,
            slots: [{ slotIndex: 0, deviceCode: "device-1", channelCode: "channel-1" }]
        });
        expect(api.listPlaybackSchemes).toHaveBeenCalledTimes(2);
    });

    it("disables save for an empty workspace and exposes close semantics", async () => {
        const wrapper = mountPanel([]);
        await flushPromises();

        expect(wrapper.get("[data-test=new-scheme]").attributes("disabled")).toBeDefined();
        expect(wrapper.text()).toContain("先选择通道");
        await wrapper.get("[data-test=close-schemes]").trigger("click");
        expect(wrapper.emitted("update:visible")?.[0]).toEqual([false]);
    });
});
