import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PlaybackSourceTree from "./PlaybackSourceTree.vue";

const api = vi.hoisted(() => ({
    listDevices: vi.fn(),
    listChannels: vi.fn(),
    listDirectoryTree: vi.fn()
}));

vi.mock("../device-mgmt/api", () => api);

const device = (id: number, deviceId: string, name: string) => ({
    id, deviceId, name, alias: "", status: 1, channelCount: 1, channelOnlineCount: 1,
    onlineRate: 1, transport: "UDP", manufacturer: "", model: "", firmware: "", ip: "", port: 5060,
    online: true, createdAt: "", updatedAt: ""
});

const channel = { id: 11, channelId: "channel-11", deviceId: "device-1", name: "东门", alias: "", status: 1, audioEnabled: false };

function mountTree() {
    return mount(PlaybackSourceTree, {
        props: { usedChannelIds: [] },
        global: { stubs: { "a-spin": { template: "<div><slot /></div>" } } }
    });
}

describe("PlaybackSourceTree", () => {
    beforeEach(() => {
        api.listDevices.mockReset();
        api.listChannels.mockReset();
        api.listDirectoryTree.mockReset();
        api.listDevices.mockImplementation(async (params: { status?: string }) => {
            if (params.status === "online") return { code: 0, data: { list: [], total: 1 } };
            if (params.status === "offline") return { code: 0, data: { list: [], total: 2 } };
            return { code: 0, data: { list: [device(1, "device-1", "一号设备")], total: 1 } };
        });
        api.listChannels.mockResolvedValue({ code: 0, data: { list: [channel] } });
        api.listDirectoryTree.mockResolvedValue({ code: 0, data: { list: [{ key: "national:area:370000", name: "山东省", type: "area", count: 1, onlineCount: 1, depth: 0, children: [] }] } });
    });

    it("shows three source views and nests channels below an expanded device", async () => {
        const wrapper = mountTree();
        await flushPromises();

        expect(wrapper.findAll("[role=tab]").map(node => node.text())).toEqual(["直接展示", "国标目录", "自定义目录"]);
        expect(wrapper.get(".device-total").text()).toBe("共 3 台");
        expect(wrapper.get(".tree-summary").text()).toContain("在线 1");
        expect(wrapper.get(".tree-summary").text()).toContain("离线 2");
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
