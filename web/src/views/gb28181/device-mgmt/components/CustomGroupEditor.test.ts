import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DirectoryNode } from "../api";
import CustomGroupEditor from "./CustomGroupEditor.vue";

const createCustomGroup = vi.fn();
const renameCustomGroup = vi.fn();
const moveCustomGroup = vi.fn();
const deleteCustomGroup = vi.fn();

vi.mock("../api", () => ({
    createCustomGroup: (...args: unknown[]) => createCustomGroup(...args),
    renameCustomGroup: (...args: unknown[]) => renameCustomGroup(...args),
    moveCustomGroup: (...args: unknown[]) => moveCustomGroup(...args),
    deleteCustomGroup: (...args: unknown[]) => deleteCustomGroup(...args)
}));

const child: DirectoryNode = {
    key: "custom:group:13", name: "子分组", type: "group", readOnly: false,
    count: 1, onlineCount: 1, depth: 1, children: []
};
const root: DirectoryNode = {
    key: "custom:group:12", name: "一号园区", type: "group", readOnly: false,
    count: 2, onlineCount: 1, depth: 0, children: [child]
};
const peer: DirectoryNode = {
    key: "custom:group:20", name: "二号园区", type: "group", readOnly: false,
    count: 0, onlineCount: 0, depth: 0, children: []
};
const tree = [root, peer];

function mountEditor(props: Record<string, unknown>) {
    return mount(CustomGroupEditor, {
        props: { visible: true, canManage: true, mode: "create", node: null, tree, ...props },
        global: {
            stubs: {
                "a-modal": { props: ["visible"], template: "<div v-if='visible'><slot /><slot name='footer' /></div>" },
                "a-form": { template: "<form @submit.prevent><slot /></form>" },
                "a-form-item": { props: ["help"], template: "<label><slot /><span>{{ help }}</span></label>" },
                "a-input": {
                    props: ["modelValue"],
                    emits: ["update:modelValue"],
                    template: "<input :value='modelValue' @input='$emit(\"update:modelValue\", $event.target.value)' />"
                },
                "a-alert": { template: "<div><slot /></div>" }
            }
        }
    });
}

describe("CustomGroupEditor", () => {
    beforeEach(() => {
        createCustomGroup.mockReset();
        renameCustomGroup.mockReset();
        moveCustomGroup.mockReset();
        deleteCustomGroup.mockReset();
    });

    it("blocks blank and overlong names before calling the API", async () => {
        const wrapper = mountEditor({ mode: "create", node: null });
        await wrapper.get("[data-testid='group-submit']").trigger("click");
        expect(wrapper.text()).toContain("请输入 1-64 个字符的分组名称");
        expect(createCustomGroup).not.toHaveBeenCalled();

        await wrapper.get("[data-testid='group-name']").setValue("a".repeat(65));
        await wrapper.get("[data-testid='group-submit']").trigger("click");
        expect(createCustomGroup).not.toHaveBeenCalled();
    });

    it("creates a child with the exact parent id and emits the created group", async () => {
        createCustomGroup.mockResolvedValue({ code: 0, data: { id: 30, parentId: 12, name: "门岗" } });
        const wrapper = mountEditor({ mode: "create", node: root });
        await wrapper.get("[data-testid='group-name']").setValue("  门岗  ");
        await wrapper.get("[data-testid='group-submit']").trigger("click");
        await flushPromises();

        expect(createCustomGroup).toHaveBeenCalledWith({ name: "门岗", parentId: 12 });
        expect(wrapper.emitted("saved")?.[0]?.[0]).toMatchObject({ action: "create", group: { id: 30 } });
    });

    it("keeps the input and shows a specific same-level conflict", async () => {
        createCustomGroup.mockRejectedValue({ response: { data: { data: { errorCode: "GROUP_NAME_CONFLICT" } } } });
        const wrapper = mountEditor({ mode: "create", node: null });
        await wrapper.get("[data-testid='group-name']").setValue("一号园区");
        await wrapper.get("[data-testid='group-submit']").trigger("click");
        await flushPromises();

        expect(wrapper.get<HTMLInputElement>("[data-testid='group-name']").element.value).toBe("一号园区");
        expect(wrapper.text()).toContain("同级分组名称已存在");
    });

    it("excludes the moving group and its descendants from parent targets", async () => {
        moveCustomGroup.mockResolvedValue({ code: 0, data: { id: 12, parentId: 20 } });
        const wrapper = mountEditor({ mode: "move", node: root });
        const options = wrapper.findAll("[data-testid='move-target'] option").map(option => option.attributes("value"));
        expect(options).toContain("20");
        expect(options).not.toContain("12");
        expect(options).not.toContain("13");

        await wrapper.get("[data-testid='move-target']").setValue("20");
        await wrapper.get("[data-testid='group-submit']").trigger("click");
        await flushPromises();
        expect(moveCustomGroup).toHaveBeenCalledWith(12, 20);
    });

    it("warns that deleting a group never deletes devices and maps child-group errors", async () => {
        deleteCustomGroup.mockRejectedValue({ response: { data: { data: { errorCode: "GROUP_HAS_CHILDREN" } } } });
        const wrapper = mountEditor({ mode: "delete", node: root });
        expect(wrapper.text()).toContain("不会删除设备");
        await wrapper.get("[data-testid='group-submit']").trigger("click");
        await flushPromises();
        expect(wrapper.text()).toContain("请先处理子分组");
    });

    it("renders nothing when management permission is absent", () => {
        const wrapper = mountEditor({ canManage: false });
        expect(wrapper.html()).toBe("<!--v-if-->");
    });
});
