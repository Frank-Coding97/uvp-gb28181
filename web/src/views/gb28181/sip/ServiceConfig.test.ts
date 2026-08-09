import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ServiceConfig from "./ServiceConfig.vue";

const api = vi.hoisted(() => ({
    fetchPTZDefaultSpeedConfig: vi.fn(),
    updatePTZDefaultSpeedConfig: vi.fn(),
    fetchPositionHistoryConfig: vi.fn(),
    updatePositionHistoryConfig: vi.fn(),
    fetchSDPExtensionConfig: vi.fn(),
    updateSDPExtensionConfig: vi.fn(),
    fetchSyncChannelsOnOnlineConfig: vi.fn(),
    updateSyncChannelsOnOnlineConfig: vi.fn(),
    fetchSIPLogConfig: vi.fn(),
    updateSIPLogConfig: vi.fn()
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

const SliderStub = {
    props: ["modelValue", "min", "max", "step", "disabled"],
    emits: ["update:modelValue"],
    template: `<input type="range" :min="min" :max="max" :step="step" :disabled="disabled" :value="modelValue" @input="$emit('update:modelValue', Number($event.target.value))" />`
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
                "a-slider": SliderStub,
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
        api.fetchPTZDefaultSpeedConfig.mockReset();
        api.updatePTZDefaultSpeedConfig.mockReset();
        api.fetchSyncChannelsOnOnlineConfig.mockReset();
        api.updateSyncChannelsOnOnlineConfig.mockReset();
        api.fetchSIPLogConfig.mockReset();
        api.updateSIPLogConfig.mockReset();
        api.fetchPositionHistoryConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: true, retentionDays: 7 } });
        api.fetchSDPExtensionConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: false } });
        api.fetchPTZDefaultSpeedConfig.mockResolvedValue({ code: 0, message: "", data: { level: 8 } });
        api.updatePositionHistoryConfig.mockResolvedValue({
            code: 0,
            message: "保存成功",
            data: { enabled: false, retentionDays: 7 }
        });
        api.updateSDPExtensionConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { enabled: true } });
        api.updatePTZDefaultSpeedConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { level: 10 } });
        api.fetchSyncChannelsOnOnlineConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: true } });
        api.updateSyncChannelsOnOnlineConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { enabled: false } });
        api.fetchSIPLogConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: false, applied: true } });
        api.updateSIPLogConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { enabled: true, applied: true } });
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

    it("presents and saves the PTZ default speed as a 1-10 level slider", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(wrapper.text()).toContain("云台默认速度");
        expect(wrapper.text()).toContain("8 档");
        const speedSlider = wrapper.get("input[type='range']");
        expect(speedSlider.attributes()).toMatchObject({ min: "1", max: "10", step: "1" });
        expect(speedSlider.element).toHaveProperty("value", "8");
        expect(speedSlider.element).toHaveProperty("disabled", true);

        await wrapper.get("button").trigger("click");
        expect(speedSlider.element).toHaveProperty("disabled", false);
        await speedSlider.setValue("10");
        expect(wrapper.text()).toContain("10 档");
        const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存"));
        await saveButton?.trigger("click");
        await flushPromises();

        expect(api.updatePTZDefaultSpeedConfig).toHaveBeenCalledWith(10);
    });

    it("loads and saves the device-online channel synchronization switch", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(api.fetchSyncChannelsOnOnlineConfig).toHaveBeenCalledOnce();
        expect(wrapper.text()).toContain("设备上线时同步通道");
        const syncSwitch = wrapper
            .findAll("label")
            .find(label => label.text().includes("设备上线时同步通道"))
            ?.find("button");
        expect(syncSwitch?.element).toHaveProperty("disabled", true);

        await wrapper.get("button").trigger("click");
        expect(syncSwitch?.element).toHaveProperty("disabled", false);
        await syncSwitch?.trigger("click");

        const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存"));
        await saveButton?.trigger("click");
        await flushPromises();

        expect(api.updateSyncChannelsOnOnlineConfig).toHaveBeenCalledWith(false);
    });

    it("loads and saves the SIP trace switch with immediate-apply feedback", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(api.fetchSIPLogConfig).toHaveBeenCalledOnce();
        expect(wrapper.text()).toContain("是否开启 SIP 日志");
        expect(wrapper.text()).toContain("热重载 SIP 服务");
        const sipLogSwitch = wrapper
            .findAll("label")
            .find(label => label.text().includes("是否开启 SIP 日志"))
            ?.find("button");
        expect(sipLogSwitch?.element).toHaveProperty("disabled", true);

        await wrapper.get("button").trigger("click");
        expect(sipLogSwitch?.element).toHaveProperty("disabled", false);
        await sipLogSwitch?.trigger("click");

        const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存"));
        await saveButton?.trigger("click");
        await flushPromises();

        expect(api.updateSIPLogConfig).toHaveBeenCalledWith(true);
    });

    it("shows when the saved SIP trace switch is not applied to the runtime", async () => {
        api.fetchSIPLogConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: true, applied: false } });

        const wrapper = mountPage();
        await flushPromises();

        expect(wrapper.text()).toContain("配置尚未应用");
    });
});
