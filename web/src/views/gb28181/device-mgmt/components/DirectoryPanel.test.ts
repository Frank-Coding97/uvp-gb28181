import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createDirectoryState, type DirectoryState } from "../directoryState";
import DirectoryPanel from "./DirectoryPanel.vue";

const listDirectoryTree = vi.fn();

vi.mock("../api", () => ({
    listDirectoryTree: (...args: unknown[]) => listDirectoryTree(...args)
}));

const nationalTree = [
    {
        key: "national:area:370000",
        name: "山东省",
        type: "area",
        code: "370000",
        readOnly: true,
        count: 2,
        onlineCount: 1,
        depth: 0,
        children: [
            {
                key: "national:area:370100",
                name: "济南市",
                type: "area",
                code: "370100",
                readOnly: true,
                count: 2,
                onlineCount: 1,
                depth: 1,
                children: []
            }
        ]
    },
    {
        key: "national:unknown",
        name: "未知区域",
        type: "unknown",
        readOnly: true,
        count: 1,
        onlineCount: 0,
        depth: 0,
        children: []
    }
];

const customTree = [
    {
        key: "custom:group:12",
        name: "一号园区",
        type: "group",
        readOnly: false,
        count: 2,
        onlineCount: 1,
        depth: 0,
        children: []
    },
    {
        key: "custom:ungrouped",
        name: "未分组设备",
        type: "ungrouped",
        readOnly: true,
        count: 1,
        onlineCount: 0,
        depth: 0,
        children: []
    }
];

function mountPanel(canManage = true) {
    return mount(DirectoryPanel, {
        props: { modelValue: createDirectoryState(), canManage },
        global: {
            stubs: {
                "a-spin": { template: "<div><slot /></div>" },
                "a-dropdown": { template: "<div><slot /><slot name='content' /></div>" },
                "a-menu": { template: "<div><slot /></div>" },
                "a-menu-item": { template: "<button><slot /></button>" },
                "a-tooltip": { template: "<span><slot /></span>" }
            }
        }
    });
}

describe("DirectoryPanel", () => {
    beforeEach(() => {
        listDirectoryTree.mockReset();
        listDirectoryTree.mockImplementation((view: string) => Promise.resolve({
            code: 0,
            data: { list: view === "national" ? nationalTree : customTree }
        }));
    });

    it("defaults to readable national names and keeps codes out of visible labels", async () => {
        const wrapper = mountPanel();
        await flushPromises();

        expect(listDirectoryTree).toHaveBeenCalledWith("national");
        expect(wrapper.text()).toContain("山东省");
        expect(wrapper.text()).toContain("未知区域");
        expect(wrapper.text()).not.toContain("370000");
        expect(wrapper.get('[data-node-key="national:area:370000"]').attributes("title")).toContain("370000");
        expect(wrapper.find("[data-action='rename']").exists()).toBe(false);
    });

    it("switches to custom groups, filters with its own keyword and exposes actions only on writable groups", async () => {
        const wrapper = mountPanel();
        await flushPromises();

        await wrapper.get("[data-view='custom']").trigger("click");
        const switchedState = wrapper.emitted("update:modelValue")?.at(-1)?.[0] as DirectoryState;
        await wrapper.setProps({ modelValue: switchedState });
        await flushPromises();
        expect(listDirectoryTree).toHaveBeenCalledWith("custom");
        expect(wrapper.text()).toContain("一号园区");
        expect(wrapper.findAll("[data-action='rename']")).toHaveLength(1);

        await wrapper.get("[data-testid='directory-search']").setValue("未分组");
        const searchedState = wrapper.emitted("update:modelValue")?.at(-1)?.[0] as DirectoryState;
        await wrapper.setProps({ modelValue: searchedState });
        expect(wrapper.text()).not.toContain("一号园区");
        expect(wrapper.text()).toContain("未分组设备");
        expect(wrapper.find("[data-action='rename']").exists()).toBe(false);
        expect(wrapper.emitted("update:modelValue")).toBeTruthy();
    });

    it("does not expose group management without permission", async () => {
        const wrapper = mountPanel(false);
        await flushPromises();
        await wrapper.get("[data-view='custom']").trigger("click");
        const switchedState = wrapper.emitted("update:modelValue")?.at(-1)?.[0] as DirectoryState;
        await wrapper.setProps({ modelValue: switchedState });
        await flushPromises();
        expect(wrapper.find("[data-action='create-root']").exists()).toBe(false);
        expect(wrapper.find("[data-action='rename']").exists()).toBe(false);
    });
});
