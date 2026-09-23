import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AddToGroupDialog from "./AddToGroupDialog.vue";

const listDirectoryTree = vi.fn();
const addDevicesToGroup = vi.fn();

vi.mock("../api", () => ({
    listDirectoryTree: (...args: unknown[]) => listDirectoryTree(...args),
    addDevicesToGroup: (...args: unknown[]) => addDevicesToGroup(...args)
}));

const tree = [
    {
        key: "custom:group:12", name: "一号园区", type: "group", readOnly: false,
        count: 2, onlineCount: 1, depth: 0,
        children: [{
            key: "custom:group:13", name: "门岗", type: "group", readOnly: false,
            count: 1, onlineCount: 1, depth: 1, children: []
        }]
    },
    { key: "custom:ungrouped", name: "未分组设备", type: "ungrouped", readOnly: true, count: 1, onlineCount: 0, depth: 0, children: [] }
];

function mountDialog() {
    return mount(AddToGroupDialog, {
        props: { visible: true, deviceIds: [7, 8] },
        global: {
            stubs: {
                "a-modal": { props: ["visible"], template: "<div v-if='visible'><slot /><slot name='footer' /></div>" },
                "a-alert": { template: "<div><slot /></div>" }
            }
        }
    });
}

describe("AddToGroupDialog", () => {
    beforeEach(() => {
        listDirectoryTree.mockReset();
        addDevicesToGroup.mockReset();
        listDirectoryTree.mockResolvedValue({ code: 0, data: { list: tree } });
    });

    it("loads only writable groups and excludes the ungrouped virtual node", async () => {
        const wrapper = mountDialog();
        await flushPromises();
        expect(listDirectoryTree).toHaveBeenCalledWith("custom");
        expect(wrapper.text()).toContain("一号园区");
        expect(wrapper.text()).toContain("门岗");
        expect(wrapper.text()).not.toContain("未分组设备");
    });

    it("submits selected devices and emits exact idempotent counts", async () => {
        addDevicesToGroup.mockResolvedValue({ code: 0, data: { requestedCount: 2, addedCount: 1, skippedCount: 1 } });
        const wrapper = mountDialog();
        await flushPromises();
        await wrapper.get("[data-testid='group-target']").setValue("13");
        await wrapper.get("[data-testid='add-to-group-submit']").trigger("click");
        await flushPromises();

        expect(addDevicesToGroup).toHaveBeenCalledWith(13, [7, 8]);
        expect(wrapper.emitted("saved")?.[0]?.[0]).toEqual({ groupId: 13, requestedCount: 2, addedCount: 1, skippedCount: 1 });
        expect(wrapper.emitted("update:visible")?.at(-1)?.[0]).toBe(false);
    });

    it("keeps the dialog and device selection available after an API failure", async () => {
        addDevicesToGroup.mockRejectedValue(new Error("network down"));
        const wrapper = mountDialog();
        await flushPromises();
        await wrapper.get("[data-testid='group-target']").setValue("12");
        await wrapper.get("[data-testid='add-to-group-submit']").trigger("click");
        await flushPromises();

        expect(wrapper.text()).toContain("network down");
        expect(wrapper.emitted("saved")).toBeUndefined();
        expect(wrapper.emitted("update:visible")).toBeUndefined();
        expect(wrapper.props("deviceIds")).toEqual([7, 8]);
    });
});
