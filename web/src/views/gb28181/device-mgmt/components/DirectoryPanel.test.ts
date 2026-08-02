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

function mountPanel(canManage = true, state = createDirectoryState()) {
    return mount(DirectoryPanel, {
        props: { modelValue: state, canManage },
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

function deferred<T>() {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((done) => { resolve = done; });
    return { promise, resolve };
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

    it("keeps the latest custom tree when an older refresh finishes last", async () => {
        const first = deferred<any>();
        const second = deferred<any>();
        listDirectoryTree.mockReset();
        listDirectoryTree.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
        const wrapper = mountPanel(true, { ...createDirectoryState(), view: "custom" });
        const refresh = (wrapper.vm as any).refresh("custom");

        second.resolve({ code: 0, data: { list: [{ ...customTree[0], name: "最新分组" }] } });
        await flushPromises();
        expect(wrapper.text()).toContain("最新分组");

        first.resolve({ code: 0, data: { list: [{ ...customTree[0], name: "过期分组" }] } });
        await refresh;
        await flushPromises();
        expect(wrapper.text()).toContain("最新分组");
        expect(wrapper.text()).not.toContain("过期分组");
    });

    it("does not switch back when the previous view request finishes late", async () => {
        const national = deferred<any>();
        const custom = deferred<any>();
        listDirectoryTree.mockReset();
        listDirectoryTree.mockReturnValueOnce(national.promise).mockReturnValueOnce(custom.promise);
        const wrapper = mountPanel();
        await wrapper.get("[data-view='custom']").trigger("click");
        const switchedState = wrapper.emitted("update:modelValue")?.at(-1)?.[0] as DirectoryState;
        await wrapper.setProps({ modelValue: switchedState });

        custom.resolve({ code: 0, data: { list: customTree } });
        await flushPromises();
        const customState = wrapper.emitted("update:modelValue")?.at(-1)?.[0] as DirectoryState;
        await wrapper.setProps({ modelValue: customState });
        national.resolve({ code: 0, data: { list: nationalTree } });
        await flushPromises();

        const latestState = wrapper.emitted("update:modelValue")?.at(-1)?.[0] as DirectoryState;
        expect(latestState.view).toBe("custom");
        expect(wrapper.get("[data-view='custom']").classes()).toContain("active");
    });
});
