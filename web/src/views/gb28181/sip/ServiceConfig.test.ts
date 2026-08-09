import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ServiceConfig from "./ServiceConfig.vue";

const api = vi.hoisted(() => ({
    fetchPositionHistoryConfig: vi.fn(),
    updatePositionHistoryConfig: vi.fn(),
    fetchSDPExtensionConfig: vi.fn(),
    updateSDPExtensionConfig: vi.fn()
}));

vi.mock("@/api/gb28181", () => api);
vi.mock("@arco-design/web-vue", () => ({
    Message: { success: vi.fn(), error: vi.fn() }
}));

const ButtonStub = {
    props: ["disabled", "loading"],
    emits: ["click"],
    template: `<button :disabled="disabled || loading" @click="$emit('click')"><slot name="icon" /><slot /></button>`
};

const SwitchStub = {
    props: ["modelValue", "disabled", "loading"],
    emits: ["update:modelValue"],
    template: `<button :disabled="disabled || loading" @click="$emit('update:modelValue', !modelValue)">{{ modelValue }}</button>`
};

const InputNumberStub = {
    props: ["modelValue", "disabled"],
    emits: ["update:modelValue"],
    template: `<input type="number" :disabled="disabled" :value="modelValue" @input="$emit('update:modelValue', Number($event.target.value))" />`
};

function mountPage() {
    return mount(ServiceConfig, {
        global: {
            stubs: {
                "a-tabs": { template: `<div><slot name="extra" /><slot /></div>` },
                "a-tab-pane": { template: `<section><slot /></section>` },
                "a-card": { template: `<div><slot /></div>` },
                "a-form": { template: `<form><slot /></form>` },
                "a-row": { template: `<div><slot /></div>` },
                "a-col": { template: `<div><slot /></div>` },
                "a-form-item": {
                    props: ["label"],
                    template: `<label>{{ label }}<slot /><slot name="extra" /></label>`
                },
                "a-button": ButtonStub,
                "a-switch": SwitchStub,
                "a-input-number": InputNumberStub,
                "a-select": { template: `<select disabled><slot /></select>` },
                "a-option": { template: `<option><slot /></option>` }
            }
        }
    });
}

describe("ServiceConfig edit mode", () => {
    beforeEach(() => {
        api.fetchPositionHistoryConfig.mockReset();
        api.updatePositionHistoryConfig.mockReset();
        api.fetchSDPExtensionConfig.mockReset();
        api.updateSDPExtensionConfig.mockReset();
        api.fetchPositionHistoryConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: true, retentionDays: 7 } });
        api.fetchSDPExtensionConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: false } });
        api.updatePositionHistoryConfig.mockResolvedValue({
            code: 0,
            message: "保存成功",
            data: { enabled: false, retentionDays: 7 }
        });
        api.updateSDPExtensionConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { enabled: true } });
    });

    it("reuses the system configuration page layout primitives", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(wrapper.find(".uvp-system-tabs").exists()).toBe(true);
        expect(wrapper.findAll(".uvp-system-panel")).toHaveLength(3);
        expect(wrapper.findAll(".uvp-system-form")).toHaveLength(3);
    });

    it("keeps controls read-only until edit and only persists on save", async () => {
        const wrapper = mountPage();
        await flushPromises();

        const switchButton = wrapper.findAll("button").find(button => button.text() === "true");
        expect(switchButton?.element).toHaveProperty("disabled", true);
        expect(api.updatePositionHistoryConfig).not.toHaveBeenCalled();

        await wrapper.get("button").trigger("click");
        expect(switchButton?.element).toHaveProperty("disabled", false);
        await switchButton?.trigger("click");
        expect(api.updatePositionHistoryConfig).not.toHaveBeenCalled();

        const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存"));
        await saveButton?.trigger("click");
        await flushPromises();

        expect(api.updatePositionHistoryConfig).toHaveBeenCalledOnce();
        expect(api.updatePositionHistoryConfig).toHaveBeenCalledWith({ enabled: false, retentionDays: 7 });
        expect(wrapper.text()).toContain("编辑");
    });

    it("edits and persists the retention days from the migrated position history settings", async () => {
        api.updatePositionHistoryConfig.mockResolvedValue({
            code: 0,
            message: "保存成功",
            data: { enabled: true, retentionDays: 30 }
        });
        const wrapper = mountPage();
        await flushPromises();

        await wrapper.get("button").trigger("click");
        const retentionDaysInput = wrapper.find("input[type='number']");
        expect(retentionDaysInput.element).toHaveProperty("disabled", false);
        await retentionDaysInput.setValue("30");

        const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存"));
        await saveButton?.trigger("click");
        await flushPromises();

        expect(api.updatePositionHistoryConfig).toHaveBeenCalledWith({ enabled: true, retentionDays: 30 });
    });

    it("falls back to seven retention days for an older backend response", async () => {
        api.fetchPositionHistoryConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: true } });
        const wrapper = mountPage();
        await flushPromises();

        await wrapper.get("button").trigger("click");

        expect(wrapper.find("input[type='number']").element).toHaveProperty("value", "7");
    });

    it("restores the saved value when editing is cancelled", async () => {
        const wrapper = mountPage();
        await flushPromises();

        await wrapper.get("button").trigger("click");
        const switchButton = wrapper.findAll("button").find(button => button.text() === "true");
        await switchButton?.trigger("click");
        expect(wrapper.text()).toContain("false");

        const cancelButton = wrapper.findAll("button").find(button => button.text().includes("取消"));
        await cancelButton?.trigger("click");

        expect(api.updatePositionHistoryConfig).not.toHaveBeenCalled();
        expect(wrapper.text()).toContain("true");
        expect(wrapper.text()).toContain("编辑");
    });

    it("loads, edits and saves the SDP compatibility mode only after explicit save", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(api.fetchSDPExtensionConfig).toHaveBeenCalledOnce();
        expect(wrapper.text()).toContain("扩展 SDP 兼容模式");
        expect(wrapper.text()).toContain("一般设备无需开启");

        const falseSwitch = wrapper.findAll("button").find(button => button.text() === "false");
        expect(falseSwitch?.element).toHaveProperty("disabled", true);

        await wrapper.get("button").trigger("click");
        expect(falseSwitch?.element).toHaveProperty("disabled", false);
        await falseSwitch?.trigger("click");
        expect(api.updateSDPExtensionConfig).not.toHaveBeenCalled();

        const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存"));
        await saveButton?.trigger("click");
        await flushPromises();

        expect(api.updateSDPExtensionConfig).toHaveBeenCalledWith(true);
    });
});
