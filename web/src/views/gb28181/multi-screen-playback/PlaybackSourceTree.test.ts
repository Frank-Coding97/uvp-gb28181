import { flushPromises, mount } from "@vue/test-utils";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import PlaybackSourceTree from "./PlaybackSourceTree.vue";

const api = vi.hoisted(() => ({
    listDevices: vi.fn(),
    listChannels: vi.fn(),
    listDirectoryTree: vi.fn()
}));
const favoritesApi = vi.hoisted(() => ({ listChannelFavoriteGroups: vi.fn(), createChannelFavoriteGroup: vi.fn(), appendChannelFavoriteGroup: vi.fn(), removeChannelFavoriteItem: vi.fn(), deleteChannelFavoriteGroup: vi.fn() }));

vi.mock("../device-mgmt/api", () => api);
vi.mock("@/api/gb28181", () => favoritesApi);

const device = (id: number, deviceId: string, name: string) => ({
    id, deviceId, name, alias: "", status: 1, channelCount: 1, channelOnlineCount: 1,
    onlineRate: 1, transport: "UDP", manufacturer: "", model: "", firmware: "", ip: "", port: 5060,
    online: true, createdAt: "", updatedAt: ""
});

const channel = { id: 11, channelId: "channel-11", deviceId: "device-1", name: "东门", alias: "", status: 1, audioEnabled: false };

function mountTree() {
    return mount(PlaybackSourceTree, {
        props: { usedChannelIds: [] },
        global: {
            stubs: {
                "a-spin": { template: "<div><slot /></div>" },
                "a-pagination": {
                    emits: ["change"],
                    template: "<button data-test='device-pagination' @click='$emit(\"change\", 2)'>下一页</button>"
                },
                "a-select": {
                    props: ["modelValue"],
                    emits: ["update:modelValue", "change"],
                    template: "<select :value='modelValue' @change='$emit(\"update:modelValue\", $event.target.value); $emit(\"change\", $event.target.value)'><option value=''></option><slot /></select>"
                },
                "a-option": {
                    props: ["value"],
                    template: "<option :value='value'><slot /></option>"
                },
                "a-popconfirm": {
                    emits: ["ok", "cancel"],
                    template: "<div class='popconfirm-stub'><slot /></div>"
                }
            }
        }
    });
}

describe("PlaybackSourceTree", () => {
    beforeEach(() => {
        window.localStorage.clear();
        api.listDevices.mockReset();
        api.listChannels.mockReset();
        api.listDirectoryTree.mockReset();
        favoritesApi.listChannelFavoriteGroups.mockReset();
        favoritesApi.createChannelFavoriteGroup.mockReset();
        favoritesApi.appendChannelFavoriteGroup.mockReset();
        favoritesApi.removeChannelFavoriteItem.mockReset();
        favoritesApi.deleteChannelFavoriteGroup.mockReset();
        api.listDevices.mockImplementation(async (params: { page?: number }) => {
            const id = params.page === 2 ? 2 : 1;
            const name = id === 1 ? "一号设备" : "二号设备";
            return { code: 0, data: { list: [device(id, `device-${id}`, name)], total: 120, onlineTotal: 1, offlineTotal: 119 } };
        });
        api.listChannels.mockResolvedValue({ code: 0, data: { list: [channel] } });
        api.listDirectoryTree.mockResolvedValue({ code: 0, data: { list: [{ key: "national:area:370000", name: "山东省", type: "area", count: 1, onlineCount: 1, depth: 0, children: [] }] } });
        favoritesApi.listChannelFavoriteGroups.mockResolvedValue({ code: 0, data: { list: [] } });
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it("shows source views and nests channels below an expanded device", async () => {
        const wrapper = mountTree();
        await flushPromises();

        expect(wrapper.findAll("[role=tab]").map(node => node.text())).toEqual(["设备树", "国标目录", "自定义目录", "我的收藏"]);
        expect(wrapper.findAll("[data-test^=source-view-scroll]")).toHaveLength(2);
        expect(wrapper.find("[data-test=source-view-devices] .lucide-cctv").exists()).toBe(true);
        expect(wrapper.find("[data-test=source-view-national] .lucide-map-pin").exists()).toBe(true);
        expect(wrapper.find("[data-test=source-view-custom] .lucide-folder").exists()).toBe(true);
        expect(wrapper.find("[data-test=source-view-favorites] .lucide-star").exists()).toBe(true);
        expect(wrapper.find(".tree-title").exists()).toBe(false);
        expect(wrapper.find(".tree-head .lucide-router").exists()).toBe(false);
        expect(wrapper.find(".tree-head .device-total").exists()).toBe(false);
        expect(wrapper.get(".tree-pagination .device-total").text()).toBe("共 120 台");
        expect(wrapper.get(".tree-summary").text()).toContain("在线 1");
        expect(wrapper.get(".tree-summary").text()).toContain("离线 119");
        expect(wrapper.get("[data-test=device-filter-toggle]").attributes("aria-expanded")).toBe("false");
        expect(wrapper.find("[data-test=device-search]").exists()).toBe(false);
        await wrapper.get("[data-test=device-filter-toggle]").trigger("click");
        expect(wrapper.get("[data-test=device-filter-toggle]").attributes("aria-expanded")).toBe("true");
        expect(wrapper.find("[data-test=device-search]").exists()).toBe(true);
        expect(wrapper.text()).toContain("一号设备");
        expect(wrapper.get('[data-node-key="root:device:1"] .tree-node svg').classes()).toContain("lucide-cctv");
        expect(wrapper.get('[data-node-key="root:device:1"] .node-status-dot').classes()).toContain("online");
        expect(wrapper.get('[data-node-key="root:device:1"] .node-status-dot').attributes("title")).toBe("在线");
        expect(wrapper.find('[data-node-key="root:device:1"] .node-status').exists()).toBe(false);
        await wrapper.get('[data-node-key="root:device:1"] .twist-button').trigger("click");
        await flushPromises();

        expect(api.listChannels).toHaveBeenCalledWith(expect.objectContaining({ deviceId: "device-1" }));
        expect(wrapper.text()).toContain("东门");
        expect(wrapper.get('[data-node-key="root:device:1:channel:11"] .tree-node svg').classes()).toContain("lucide-camera");
        await wrapper.get('[data-node-key="root:device:1:channel:11"] .tree-node').trigger("click");
        expect(wrapper.emitted("select")?.[0]?.[0]).toMatchObject({ id: 11, name: "东门" });
    });

    it("keeps the selected source view visually and semantically distinct", async () => {
        const wrapper = mountTree();
        await flushPromises();

        const devicesTab = wrapper.get("[data-test=source-view-devices]");
        const nationalTab = wrapper.get("[data-test=source-view-national]");
        expect(devicesTab.classes()).toContain("active");
        expect(devicesTab.attributes("aria-selected")).toBe("true");
        expect(nationalTab.classes()).not.toContain("active");
        expect(nationalTab.attributes("aria-selected")).toBe("false");

        await nationalTab.trigger("click");
        await flushPromises();

        expect(devicesTab.classes()).not.toContain("active");
        expect(devicesTab.attributes("aria-selected")).toBe("false");
        expect(nationalTab.classes()).toContain("active");
        expect(nationalTab.attributes("aria-selected")).toBe("true");
    });

    it("opens a group dialog when favoriting a channel and shows the group in my favorites", async () => {
        favoritesApi.createChannelFavoriteGroup.mockResolvedValue({ code: 0, data: { id: 1, name: "园区重点设备", items: [{ id: 1, deviceCode: "device-1", channelCode: "channel-11", channel: { ...channel } }], availableCount: 1, unavailableCount: 0 } });
        favoritesApi.listChannelFavoriteGroups.mockResolvedValueOnce({ code: 0, data: { list: [] } }).mockResolvedValue({ code: 0, data: { list: [{ id: 1, name: "园区重点设备", items: [{ id: 1, deviceCode: "device-1", channelCode: "channel-11", channel: { ...channel } }], availableCount: 1, unavailableCount: 0 }] } });
        const wrapper = mountTree();
        await flushPromises();

        await wrapper.get('[data-node-key="root:device:1"] .twist-button').trigger("click");
        await flushPromises();
        await wrapper.get('[data-node-key="root:device:1:channel:11"] .favorite-toggle').trigger("click");
        expect(wrapper.find("[data-test=favorite-group-name]").exists()).toBe(true);
        await wrapper.get("[data-test=favorite-group-name]").setValue("园区重点设备");
        await wrapper.get("[data-test=save-favorite-group]").trigger("click");
        expect(favoritesApi.createChannelFavoriteGroup).toHaveBeenCalledWith("园区重点设备", [{ deviceCode: "device-1", channelCode: "channel-11" }]);

        await wrapper.get("[data-test=source-view-favorites]").trigger("click");
        await flushPromises();
        expect(wrapper.text()).toContain("园区重点设备");
        expect(wrapper.get('[data-node-key^="favorites:group:"] .node-status').text()).toBe("1 个通道");
        await wrapper.get('[data-node-key^="favorites:group:"] .twist-button').trigger("click");
        expect(wrapper.text()).toContain("东门");
    });

    it("keeps the existing-group tab enabled when there are no groups", async () => {
        const wrapper = mountTree();
        await flushPromises();
        await wrapper.get('[data-node-key="root:device:1"] .twist-button').trigger("click");
        await flushPromises();
        await wrapper.get('[data-node-key="root:device:1:channel:11"] .favorite-toggle').trigger("click");

        const existingTab = wrapper.get('.favorite-dialog [role="tab"]');
        expect(existingTab.attributes("disabled")).toBeUndefined();
        await existingTab.trigger("click");
        expect(existingTab.attributes("aria-selected")).toBe("true");
        expect(wrapper.text()).toContain("暂无已有组，请切换到“新建组”");
    });

    it("appends a channel to a selected existing group", async () => {
        favoritesApi.listChannelFavoriteGroups.mockResolvedValue({ code: 0, data: { list: [{ id: 1, name: "园区重点设备", items: [{ id: 12, deviceCode: "device-1", channelCode: "channel-12", channel: { ...channel, id: 12, channelId: "channel-12", name: "西门" } }], availableCount: 1, unavailableCount: 0 }] } });
        favoritesApi.appendChannelFavoriteGroup.mockResolvedValue({ code: 0, data: { requestedCount: 1, addedCount: 1, skippedCount: 0 } });
        const wrapper = mountTree();
        await flushPromises();

        await wrapper.get('[data-node-key="root:device:1"] .twist-button').trigger("click");
        await flushPromises();
        await wrapper.get('[data-node-key="root:device:1:channel:11"] .favorite-toggle').trigger("click");
        expect((wrapper.get("[data-test=favorite-group-select]").element as HTMLSelectElement).value).toBe("");
        await wrapper.get("[data-test=favorite-group-select]").setValue("1");
        await wrapper.get("[data-test=save-favorite-group]").trigger("click");
        await flushPromises();

        expect(favoritesApi.appendChannelFavoriteGroup).toHaveBeenCalledWith(1, [{ deviceCode: "device-1", channelCode: "channel-11" }]);
        expect(wrapper.emitted("favorite-saved")?.[0]).toEqual(["园区重点设备", 1, 0]);
        expect(wrapper.find("[data-test=favorite-group-select]").exists()).toBe(false);
    });

    it("does not show an append action for a channel already in favorites", async () => {
        favoritesApi.listChannelFavoriteGroups.mockResolvedValue({ code: 0, data: { list: [{ id: 1, name: "园区重点设备", items: [{ id: 1, deviceCode: "device-1", channelCode: "channel-11", channel }], availableCount: 1, unavailableCount: 0 }] } });
        const wrapper = mountTree();
        await flushPromises();

        await wrapper.get('[data-node-key="root:device:1"] .twist-button').trigger("click");
        await flushPromises();

        expect(wrapper.find('[data-node-key="root:device:1:channel:11"] .favorite-toggle').exists()).toBe(false);
    });

    it("rejects creating a group with an existing name", async () => {
        favoritesApi.listChannelFavoriteGroups.mockResolvedValue({ code: 0, data: { list: [{ id: 1, name: "园区重点设备", items: [], availableCount: 0, unavailableCount: 0 }] } });
        favoritesApi.createChannelFavoriteGroup.mockRejectedValue({ response: { data: { data: { errorCode: "CHANNEL_FAVORITE_GROUP_NAME_CONFLICT" } } } });
        const wrapper = mountTree();
        await flushPromises();

        await wrapper.get('[data-node-key="root:device:1"] .twist-button').trigger("click");
        await flushPromises();
        await wrapper.get('[data-node-key="root:device:1:channel:11"] .favorite-toggle').trigger("click");
        await wrapper.get('.favorite-dialog [role="tab"]:not([aria-selected="true"])').trigger("click");
        await wrapper.get("[data-test=favorite-group-name]").setValue("园区重点设备");
        await wrapper.get("[data-test=save-favorite-group]").trigger("click");

        expect(wrapper.text()).toContain("已存在同名收藏组，请切换“选择已有组”进行追加");
        expect(wrapper.find("[data-test=favorite-group-name]").exists()).toBe(true);
    });

    it("centers the favorite dialog over the whole page", () => {
        const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/multi-screen-playback/PlaybackSourceTree.vue"), "utf8");

        expect(source).toMatch(/\.favorite-dialog-backdrop\s*\{[^}]*position:\s*fixed;[^}]*inset:\s*0;/s);
    });

    it("emits a quick-play action for a favorite group", async () => {
        favoritesApi.listChannelFavoriteGroups.mockResolvedValue({ code: 0, data: { list: [{ id: 1, name: "园区重点设备", items: [{ id: 1, deviceCode: "device-1", channelCode: "channel-11", channel }], availableCount: 1, unavailableCount: 0 }] } });
        const wrapper = mountTree();
        await flushPromises();
        await wrapper.get("[data-test=source-view-favorites]").trigger("click");
        await flushPromises();

        const playButton = wrapper.get('[data-test^="favorite-group-play-"]');
        expect(playButton.find(".lucide-play").exists()).toBe(true);
        expect(playButton.text()).toBe("");
        await playButton.trigger("click");
        expect(wrapper.emitted("select-group")?.[0]?.[0]).toMatchObject({ name: "园区重点设备" });
    });

    it("uses a delete icon for removing a favorite group", async () => {
        favoritesApi.listChannelFavoriteGroups.mockResolvedValue({ code: 0, data: { list: [{ id: 1, name: "园区重点设备", items: [{ id: 1, deviceCode: "device-1", channelCode: "channel-11", channel }], availableCount: 1, unavailableCount: 0 }] } });
        const wrapper = mountTree();
        await flushPromises();
        await wrapper.get("[data-test=source-view-favorites]").trigger("click");
        await flushPromises();

        const groupRow = wrapper.get('[data-node-key="favorites:group:1"]');
        expect(groupRow.find(".favorite-remove .lucide-trash-2").exists()).toBe(true);
        await groupRow.get(".twist-button").trigger("click");
        await flushPromises();
        const channelRow = wrapper.get('[data-node-key="favorites:0:group:1:channel:11"]');
        expect(channelRow.find(".favorite-remove .lucide-trash-2").exists()).toBe(true);

        const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/multi-screen-playback/PlaybackSourceTree.vue"), "utf8");
        expect(source).toMatch(/\.favorite-remove\s*\{[^}]*color:\s*var\(--uvp-danger\);/s);
        expect(source).toMatch(/\.favorite-play\s*\{[^}]*width:\s*22px;[^}]*margin-right:\s*0;/s);
        expect(source).toMatch(/\.favorite-remove\s*\{[^}]*width:\s*20px;[^}]*margin-right:\s*0;/s);
        expect(source).toContain("<a-popconfirm");
        expect(source).not.toContain("window.confirm");
        expect(source).toContain("<a-select id=\"favorite-group-select\"");
        expect(source).not.toContain("<select id=\"favorite-group-select\"");
    });

    it("refreshes every ten seconds without collapsing expanded devices", async () => {
        vi.useFakeTimers();
        const wrapper = mountTree();
        await flushPromises();
        await wrapper.get('[data-node-key="root:device:1"] .twist-button').trigger("click");
        await flushPromises();

        expect(wrapper.get('[data-node-key="root:device:1"]').attributes("aria-expanded")).toBe("true");
        expect(api.listDevices).toHaveBeenCalledTimes(1);
        await vi.advanceTimersByTimeAsync(10_000);
        await flushPromises();

        expect(api.listDevices).toHaveBeenCalledTimes(2);
        expect(wrapper.get('[data-node-key="root:device:1"]').attributes("aria-expanded")).toBe("true");
        expect(wrapper.text()).toContain("东门");
        wrapper.unmount();
        await vi.advanceTimersByTimeAsync(10_000);
        expect(api.listDevices).toHaveBeenCalledTimes(2);
    });

    it("refreshes every ten seconds without collapsing an expanded favorite group", async () => {
        vi.useFakeTimers();
        favoritesApi.listChannelFavoriteGroups.mockResolvedValue({
            code: 0,
            data: {
                list: [{
                    id: 1,
                    name: "园区重点设备",
                    items: [{ id: 1, deviceCode: "device-1", channelCode: "channel-11", channel: { ...channel } }],
                    availableCount: 1,
                    unavailableCount: 0
                }]
            }
        });

        const wrapper = mountTree();
        await flushPromises();
        await wrapper.get("[data-test=source-view-favorites]").trigger("click");
        await flushPromises();

        const groupSelector = '[data-node-key="favorites:group:1"]';
        await wrapper.get(`${groupSelector} .twist-button`).trigger("click");
        await flushPromises();

        expect(wrapper.get(groupSelector).attributes("aria-expanded")).toBe("true");
        expect(wrapper.text()).toContain("东门");

        await vi.advanceTimersByTimeAsync(10_000);
        await flushPromises();

        expect(favoritesApi.listChannelFavoriteGroups).toHaveBeenCalledTimes(2);
        expect(wrapper.get(groupSelector).attributes("aria-expanded")).toBe("true");
        expect(wrapper.text()).toContain("东门");
        wrapper.unmount();
    });

    it("keeps the expanded tree when an automatic refresh fails", async () => {
        vi.useFakeTimers();
        const wrapper = mountTree();
        await flushPromises();
        await wrapper.get('[data-node-key="root:device:1"] .twist-button').trigger("click");
        await flushPromises();
        api.listDevices.mockRejectedValueOnce(new Error("temporary failure"));

        await vi.advanceTimersByTimeAsync(10_000);
        await flushPromises();

        expect(wrapper.get('[data-node-key="root:device:1"]').attributes("aria-expanded")).toBe("true");
        expect(wrapper.text()).toContain("东门");
        wrapper.unmount();
    });

    it("keeps device results when legacy status totals fail", async () => {
        api.listDevices
            .mockResolvedValueOnce({ code: 0, data: { list: [device(1, "device-1", "一号设备")], total: 1 } })
            .mockRejectedValueOnce(new Error("status unavailable"))
            .mockRejectedValueOnce(new Error("status unavailable"));

        const wrapper = mountTree();
        await flushPromises();

        expect(wrapper.text()).toContain("一号设备");
        expect(wrapper.find(".tree-error").exists()).toBe(false);
    });

    it("paginates the flat device tree with fifty devices per page", async () => {
        const wrapper = mountTree();
        await flushPromises();

        expect(api.listDevices).toHaveBeenLastCalledWith({ page: 1, pageSize: 50 });
        await wrapper.get("[data-test=device-pagination]").trigger("click");
        await flushPromises();

        expect(api.listDevices).toHaveBeenLastCalledWith({ page: 2, pageSize: 50 });
        expect(wrapper.text()).toContain("二号设备");
    });

    it("keeps the device total in the footer when pagination is unnecessary", async () => {
        api.listDevices.mockResolvedValueOnce({
            code: 0,
            data: { list: [device(1, "device-1", "一号设备")], total: 1, onlineTotal: 1, offlineTotal: 0 }
        });

        const wrapper = mountTree();
        await flushPromises();

        expect(wrapper.get(".tree-pagination .device-total").text()).toBe("共 1 台");
        expect(wrapper.find("[data-test=device-pagination]").exists()).toBe(false);
    });

    it("searches devices on the server and resets pagination", async () => {
        vi.useFakeTimers();
        const wrapper = mountTree();
        await flushPromises();
        await wrapper.get("[data-test=device-pagination]").trigger("click");
        await flushPromises();
        await wrapper.get("[data-test=device-filter-toggle]").trigger("click");

        await wrapper.get("[data-test=device-search]").setValue("  UVP-Sim  ");
        await vi.advanceTimersByTimeAsync(300);
        await flushPromises();

        expect(api.listDevices).toHaveBeenLastCalledWith({ q: "UVP-Sim", page: 1, pageSize: 50 });
        await vi.advanceTimersByTimeAsync(10_000);
        await flushPromises();
        expect(api.listDevices).toHaveBeenLastCalledWith({ q: "UVP-Sim", page: 1, pageSize: 50 });

        await wrapper.get("[data-test=clear-device-search]").trigger("click");
        await flushPromises();
        expect(api.listDevices).toHaveBeenLastCalledWith({ page: 1, pageSize: 50 });
        wrapper.unmount();
    });

    it("filters devices by status and combines it with the search keyword", async () => {
        vi.useFakeTimers();
        const wrapper = mountTree();
        await flushPromises();
        await wrapper.get("[data-test=device-pagination]").trigger("click");
        await flushPromises();
        await wrapper.get("[data-test=device-filter-toggle]").trigger("click");
        api.listDevices.mockResolvedValueOnce({
            code: 0,
            data: { list: [device(3, "device-3", "离线设备")], total: 60, onlineTotal: 2, offlineTotal: 60 }
        });
        await wrapper.get("[data-test=device-status-offline]").trigger("click");
        await flushPromises();

        expect(api.listDevices).toHaveBeenLastCalledWith({ status: "offline", page: 1, pageSize: 50 });
        expect(wrapper.get(".tree-pagination .device-total").text()).toBe("共 60 台");
        expect(wrapper.get("[data-test=device-status-offline]").attributes("aria-pressed")).toBe("true");
        expect(wrapper.get("[data-test=device-filter-toggle]").classes()).toContain("active");

        await wrapper.get("[data-test=device-search]").setValue("摄像机");
        await vi.advanceTimersByTimeAsync(300);
        await flushPromises();

        expect(api.listDevices).toHaveBeenLastCalledWith({ q: "摄像机", status: "offline", page: 1, pageSize: 50 });
        wrapper.unmount();
    });

    it("loads devices under a national directory node instead of rendering a second flat list", async () => {
        const wrapper = mountTree();
        await wrapper.get("[data-test=source-view-national]").trigger("click");
        await flushPromises();
        await wrapper.get('[data-node-key="national:area:370000"] .twist-button').trigger("click");
        await flushPromises();

        expect(api.listDevices).toHaveBeenLastCalledWith(expect.objectContaining({ directoryView: "national", directoryKey: "national:area:370000" }));
        expect(wrapper.text()).toContain("一号设备");
    });
});
