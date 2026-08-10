import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ServiceConfig from "./ServiceConfig.vue";

const api = vi.hoisted(() => ({
    fetchPTZDefaultSpeedConfig: vi.fn(),
    updatePTZDefaultSpeedConfig: vi.fn(),
    fetchDefaultChannelStreamTransportConfig: vi.fn(),
    updateDefaultChannelStreamTransportConfig: vi.fn(),
    fetchDefaultPlaybackProtocolConfig: vi.fn(),
    updateDefaultPlaybackProtocolConfig: vi.fn(),
    fetchGlobalSubscriptionConfig: vi.fn(),
    updateGlobalSubscriptionConfig: vi.fn(),
    fetchDefaultChannelAudioConfig: vi.fn(),
    updateDefaultChannelAudioConfig: vi.fn(),
    fetchPositionHistoryConfig: vi.fn(),
    updatePositionHistoryConfig: vi.fn(),
    fetchSDPExtensionConfig: vi.fn(),
    updateSDPExtensionConfig: vi.fn(),
    fetchSyncChannelsOnOnlineConfig: vi.fn(),
    updateSyncChannelsOnOnlineConfig: vi.fn(),
    fetchIgnoreChannelOfflineStatusNotifyConfig: vi.fn(),
    updateIgnoreChannelOfflineStatusNotifyConfig: vi.fn(),
    fetchOnlineOnHeartbeatConfig: vi.fn(),
    updateOnlineOnHeartbeatConfig: vi.fn(),
    fetchSaveAlarmMessagesConfig: vi.fn(),
    updateSaveAlarmMessagesConfig: vi.fn(),
    fetchSIPCommandTimeoutConfig: vi.fn(),
    updateSIPCommandTimeoutConfig: vi.fn(),
    fetchPreallocationModeConfig: vi.fn(),
    updatePreallocationModeConfig: vi.fn(),
    fetchSIPLogConfig: vi.fn(),
    updateSIPLogConfig: vi.fn()
}));

const dictionaryApi = vi.hoisted(() => ({
    getDictItemsByDictCodeAPI: vi.fn()
}));

vi.mock("@/api/gb28181", () => api);
vi.mock("@/api/dictionary", () => dictionaryApi);
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

const SelectStub = {
    props: ["modelValue", "disabled", "loading", "type"],
    emits: ["update:modelValue"],
    template: `<select :disabled="disabled || loading" :value="modelValue" @change="$emit('update:modelValue', $event.target.value)"><slot /></select>`
};

const OptionStub = {
    props: ["value"],
    template: `<option :value="value"><slot /></option>`
};

const CheckboxGroupStub = {
    props: ["modelValue", "disabled"],
    emits: ["update:modelValue"],
    template: `<div data-checkbox-group>{{ modelValue.join(",") }}<slot /></div>`
};

const CheckboxStub = {
    props: ["value"],
    template: `<span><slot /></span>`
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
                "a-col": {
                    props: ["span"],
                    template: `<div :data-span="span"><slot /></div>`
                },
                "a-form-item": {
                    props: ["field", "label", "tooltip"],
                    template: `<label :data-field="field" :data-tooltip="tooltip">{{ label }}<slot /><slot name="extra" /></label>`
                },
                "a-button": ButtonStub,
                "a-switch": SwitchStub,
                "a-slider": SliderStub,
                "a-input-number": InputNumberStub,
                "a-radio-group": SelectStub,
                "a-radio": OptionStub,
                "a-checkbox-group": CheckboxGroupStub,
                "a-checkbox": CheckboxStub,
                "a-select": SelectStub,
                "a-option": OptionStub
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
        api.fetchDefaultChannelStreamTransportConfig.mockReset();
        api.updateDefaultChannelStreamTransportConfig.mockReset();
        api.fetchDefaultPlaybackProtocolConfig.mockReset();
        api.updateDefaultPlaybackProtocolConfig.mockReset();
        api.fetchGlobalSubscriptionConfig.mockReset();
        api.updateGlobalSubscriptionConfig.mockReset();
        api.fetchDefaultChannelAudioConfig.mockReset();
        api.updateDefaultChannelAudioConfig.mockReset();
        api.fetchSyncChannelsOnOnlineConfig.mockReset();
        api.updateSyncChannelsOnOnlineConfig.mockReset();
        api.fetchIgnoreChannelOfflineStatusNotifyConfig.mockReset();
        api.updateIgnoreChannelOfflineStatusNotifyConfig.mockReset();
        api.fetchOnlineOnHeartbeatConfig.mockReset();
        api.updateOnlineOnHeartbeatConfig.mockReset();
        api.fetchSaveAlarmMessagesConfig.mockReset();
        api.updateSaveAlarmMessagesConfig.mockReset();
        api.fetchSIPCommandTimeoutConfig.mockReset();
        api.updateSIPCommandTimeoutConfig.mockReset();
        api.fetchPreallocationModeConfig.mockReset();
        api.updatePreallocationModeConfig.mockReset();
        api.fetchSIPLogConfig.mockReset();
        api.updateSIPLogConfig.mockReset();
        dictionaryApi.getDictItemsByDictCodeAPI.mockReset();
        api.fetchPositionHistoryConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: true, retentionDays: 7 } });
        api.fetchSDPExtensionConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: false } });
        api.fetchPTZDefaultSpeedConfig.mockResolvedValue({ code: 0, message: "", data: { level: 8 } });
        api.fetchDefaultChannelStreamTransportConfig.mockResolvedValue({ code: 0, message: "", data: { transport: "TCP-Passive" } });
        api.fetchDefaultPlaybackProtocolConfig.mockResolvedValue({ code: 0, message: "", data: { protocol: "ws-flv" } });
        dictionaryApi.getDictItemsByDictCodeAPI.mockResolvedValue({
            code: 0,
            message: "",
            data: {
                list: [
                    { id: 1, name: "WebRTC（低延迟）", value: "webrtc", status: 1, dictId: 1 },
                    { id: 2, name: "WS-FLV", value: "ws-flv", status: 1, dictId: 1 },
                    { id: 3, name: "HTTP-FLV", value: "http-flv", status: 1, dictId: 1 },
                    { id: 4, name: "HLS", value: "hls", status: 1, dictId: 1 }
                ]
            }
        });
        api.fetchGlobalSubscriptionConfig.mockResolvedValue({ code: 0, message: "", data: { items: [] } });
        api.fetchDefaultChannelAudioConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: true } });
        api.updatePositionHistoryConfig.mockResolvedValue({
            code: 0,
            message: "保存成功",
            data: { enabled: false, retentionDays: 7 }
        });
        api.updateSDPExtensionConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { enabled: true } });
        api.updateGlobalSubscriptionConfig.mockResolvedValue({
            code: 0,
            message: "保存成功",
            data: { items: ["catalog", "alarm"] }
        });
        api.updateDefaultChannelAudioConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { enabled: false } });
        api.updatePTZDefaultSpeedConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { level: 10 } });
        api.updateDefaultChannelStreamTransportConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { transport: "UDP" } });
        api.updateDefaultPlaybackProtocolConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { protocol: "webrtc" } });
        api.fetchSyncChannelsOnOnlineConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: true } });
        api.updateSyncChannelsOnOnlineConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { enabled: false } });
        api.fetchIgnoreChannelOfflineStatusNotifyConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: false } });
        api.updateIgnoreChannelOfflineStatusNotifyConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { enabled: true } });
        api.fetchOnlineOnHeartbeatConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: true } });
        api.updateOnlineOnHeartbeatConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { enabled: false } });
        api.fetchSaveAlarmMessagesConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: true } });
        api.updateSaveAlarmMessagesConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { enabled: false } });
        api.fetchSIPCommandTimeoutConfig.mockResolvedValue({ code: 0, message: "", data: { timeoutSec: 10 } });
        api.updateSIPCommandTimeoutConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { timeoutSec: 30 } });
        api.fetchPreallocationModeConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: false } });
        api.updatePreallocationModeConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { enabled: true } });
        api.fetchSIPLogConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: false, applied: true } });
        api.updateSIPLogConfig.mockResolvedValue({ code: 0, message: "保存成功", data: { enabled: true, applied: true } });
    });

    it("reuses the system configuration page layout primitives", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(wrapper.find(".service-config-page").exists()).toBe(true);
        expect(wrapper.find(".uvp-system-tabs").exists()).toBe(true);
        expect(wrapper.find(".service-config-shell").exists()).toBe(false);
        expect(wrapper.findAll(".uvp-system-panel")).toHaveLength(3);
        expect(wrapper.findAll(".uvp-system-form")).toHaveLength(3);
        expect(wrapper.findAll("input[type='number']")).toHaveLength(5);
        expect(wrapper.findAll("input[type='number']").every(input => input.classes().includes("service-config-number-input"))).toBe(
            true
        );
    });

    it("moves configuration descriptions into form-item tooltips", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(wrapper.findAll("[data-tooltip]")).toHaveLength(15);
        expect(wrapper.find("[data-field='saveMobilePositionHistory']").attributes("data-tooltip")).toBe(
            "关闭后仍更新设备和通道的最新位置，不再新增轨迹点。"
        );
        expect(wrapper.find("[data-field='sipLogEnabled']").attributes("data-tooltip")).toContain(
            "保存后会热重载 SIP 服务"
        );
    });

    it("removes unsupported legacy WVP configuration placeholders", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(wrapper.text()).not.toContain("使用来源请求ip作为streamIp");
        expect(wrapper.text()).not.toContain("是否使用设备来源IP作为回复IP");
        expect(wrapper.text()).not.toContain("缺少国标ID是否给所有上级发送消息");
        expect(wrapper.text()).not.toContain("设置notify缓存队列最大长度");
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
        expect(wrapper.find("[data-field='sdpExtension']").attributes("data-tooltip")).toContain("一般设备无需开启");

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

    it("loads and saves the default stream transport for newly discovered channels", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(api.fetchDefaultChannelStreamTransportConfig).toHaveBeenCalledOnce();
        expect(wrapper.text()).toContain("新通道默认流传输模式");
        expect(wrapper.find("[data-field='defaultChannelStreamTransport']").attributes("data-tooltip")).toContain(
            "仅影响之后通过 Catalog 新发现的通道"
        );
        const transportSelect = wrapper
            .findAll("label")
            .find(label => label.text().includes("新通道默认流传输模式"))
            ?.find("select");
        expect(transportSelect?.classes()).toContain("stream-transport-select");
        expect(transportSelect?.element).toHaveProperty("value", "TCP-Passive");
        expect(transportSelect?.element).toHaveProperty("disabled", true);

        await wrapper.get("button").trigger("click");
        expect(transportSelect?.element).toHaveProperty("disabled", false);
        await transportSelect?.setValue("UDP");

        const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存"));
        await saveButton?.trigger("click");
        await flushPromises();

        expect(api.updateDefaultChannelStreamTransportConfig).toHaveBeenCalledWith("UDP");
    });

    it("loads, saves, and cancels the default playback protocol", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(api.fetchDefaultPlaybackProtocolConfig).toHaveBeenCalledOnce();
        expect(dictionaryApi.getDictItemsByDictCodeAPI).toHaveBeenCalledWith("gb28181_playback_protocol");
        const field = wrapper.find("[data-field='defaultProtocol']");
        expect(field.text()).toContain("默认播放协议");
        expect(field.find(".playback-protocol-select").exists()).toBe(true);
        expect(field.findAll("option").map(option => option.text())).toEqual([
            "WebRTC（低延迟）",
            "WS-FLV",
            "HTTP-FLV",
            "HLS"
        ]);
        const select = field.get("select");
        expect(select.element).toHaveProperty("value", "ws-flv");
        expect(select.element).toHaveProperty("disabled", true);

        await wrapper.get("button").trigger("click");
        await select.setValue("webrtc");
        const cancelButton = wrapper.findAll("button").find(button => button.text().includes("取消"));
        await cancelButton?.trigger("click");
        expect(select.element).toHaveProperty("value", "ws-flv");

        await wrapper.get("button").trigger("click");
        await select.setValue("webrtc");
        const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存"));
        await saveButton?.trigger("click");
        await flushPromises();
        expect(api.updateDefaultPlaybackProtocolConfig).toHaveBeenCalledWith("webrtc");
        expect(select.element).toHaveProperty("value", "webrtc");
    });

    it("loads and saves global subscription defaults", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(api.fetchGlobalSubscriptionConfig).toHaveBeenCalledOnce();
        expect(wrapper.text()).toContain("全局订阅项目");
        expect(wrapper.text()).toContain("目录");
        expect(wrapper.text()).toContain("报警");
        expect(wrapper.text()).toContain("位置");
        expect(wrapper.text()).toContain("PTZ 精准位置变化（2022）");
        const subscriptionField = wrapper.find("[data-field='globalSubscriptionItems']");
        expect(subscriptionField.element.parentElement?.getAttribute("data-span")).toBe("24");
        const formFields = wrapper.findAll("[data-field]").map(field => field.attributes("data-field"));
        expect(formFields.indexOf("globalSubscriptionItems")).toBeGreaterThan(formFields.indexOf("preallocationMode"));

        await wrapper.get("button").trigger("click");
        wrapper.findComponent(CheckboxGroupStub).vm.$emit("update:modelValue", ["catalog", "alarm", "ptz_precise_position"]);
        await wrapper.vm.$nextTick();
        const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存"));
        await saveButton?.trigger("click");
        await flushPromises();

        expect(api.updateGlobalSubscriptionConfig).toHaveBeenCalledWith(["catalog", "alarm", "ptz_precise_position"]);
    });

    it("loads and saves the default audio switch for newly discovered channels", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(api.fetchDefaultChannelAudioConfig).toHaveBeenCalledOnce();
        const audioLabel = wrapper.findAll("label").find(label => label.text().includes("全局通道开启音频"));
        expect(wrapper.find("[data-field='defaultChannelAudioEnabled']").attributes("data-tooltip")).toContain(
            "仅影响之后通过 Catalog 新发现的通道"
        );
        const audioSwitch = audioLabel?.find("button");

        await wrapper.get("button").trigger("click");
        await audioSwitch?.trigger("click");
        const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存"));
        await saveButton?.trigger("click");
        await flushPromises();

        expect(api.updateDefaultChannelAudioConfig).toHaveBeenCalledWith(false);
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
        expect(wrapper.find("[data-field='sipLogEnabled']").attributes("data-tooltip")).toContain("热重载 SIP 服务");
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

    it("loads and saves the ignore channel offline status notify switch", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(api.fetchIgnoreChannelOfflineStatusNotifyConfig).toHaveBeenCalledOnce();
        expect(wrapper.text()).toContain("忽略通道离线/异常通知");
        expect(wrapper.find("[data-field='ignoreChannelOfflineStatusNotify']").attributes("data-tooltip")).toContain(
            "OFF、VLOST、DEFECT"
        );
        const statusSwitch = wrapper
            .findAll("label")
            .find(label => label.text().includes("忽略通道离线/异常通知"))
            ?.find("button");
        expect(statusSwitch?.element).toHaveProperty("disabled", true);

        await wrapper.get("button").trigger("click");
        expect(statusSwitch?.element).toHaveProperty("disabled", false);
        await statusSwitch?.trigger("click");

        const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存"));
        await saveButton?.trigger("click");
        await flushPromises();

        expect(api.updateIgnoreChannelOfflineStatusNotifyConfig).toHaveBeenCalledWith(true);
    });

    it("loads and saves the online-on-heartbeat switch", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(api.fetchOnlineOnHeartbeatConfig).toHaveBeenCalledOnce();
        expect(wrapper.text()).toContain("心跳恢复设备在线状态");
        expect(wrapper.find("[data-field='onlineOnHeartbeat']").attributes("data-tooltip")).toContain(
            "关闭后仍记录最后心跳时间"
        );
        const heartbeatSwitch = wrapper
            .findAll("label")
            .find(label => label.text().includes("心跳恢复设备在线状态"))
            ?.find("button");
        expect(heartbeatSwitch?.element).toHaveProperty("disabled", true);

        await wrapper.get("button").trigger("click");
        expect(heartbeatSwitch?.element).toHaveProperty("disabled", false);
        await heartbeatSwitch?.trigger("click");

        const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存"));
        await saveButton?.trigger("click");
        await flushPromises();

        expect(api.updateOnlineOnHeartbeatConfig).toHaveBeenCalledWith(false);
    });

    it("loads and saves the alarm message storage switch", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(api.fetchSaveAlarmMessagesConfig).toHaveBeenCalledOnce();
        expect(wrapper.text()).toContain("是否存储报警消息");
        expect(wrapper.find("[data-field='saveAlarmMessages']").attributes("data-tooltip")).toContain(
            "关闭后仍接收和解析报警通知"
        );
        const alarmSwitch = wrapper
            .findAll("label")
            .find(label => label.text().includes("是否存储报警消息"))
            ?.find("button");
        expect(alarmSwitch?.element).toHaveProperty("disabled", true);

        await wrapper.get("button").trigger("click");
        expect(alarmSwitch?.element).toHaveProperty("disabled", false);
        await alarmSwitch?.trigger("click");

        const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存"));
        await saveButton?.trigger("click");
        await flushPromises();

        expect(api.updateSaveAlarmMessagesConfig).toHaveBeenCalledWith(false);
    });

    it("loads and saves the SIP command timeout and preallocation mode", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(api.fetchSIPCommandTimeoutConfig).toHaveBeenCalledOnce();
        expect(api.fetchPreallocationModeConfig).toHaveBeenCalledOnce();
        expect(wrapper.text()).toContain("SIP 命令超时时间（秒）");
        expect(wrapper.find("[data-field='preallocationMode']").attributes("data-tooltip")).toContain(
            "未知国标 ID 将被拒绝注册"
        );

        await wrapper.get("button").trigger("click");
        const timeoutInputs = wrapper.findAll("input[type='number']");
        const timeoutInput = timeoutInputs.find(input => input.element.getAttribute("value") === "10");
        expect(timeoutInput?.element).toHaveProperty("disabled", false);
        await timeoutInput?.setValue("30");

        const preallocationSwitch = wrapper
            .findAll("label")
            .find(label => label.text().includes("预分配模式"))
            ?.find("button");
        expect(preallocationSwitch?.element).toHaveProperty("disabled", false);
        await preallocationSwitch?.trigger("click");

        const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存"));
        await saveButton?.trigger("click");
        await flushPromises();

        expect(api.updateSIPCommandTimeoutConfig).toHaveBeenCalledWith(30);
        expect(api.updatePreallocationModeConfig).toHaveBeenCalledWith(true);
    });

    it("shows when the saved SIP trace switch is not applied to the runtime", async () => {
        api.fetchSIPLogConfig.mockResolvedValue({ code: 0, message: "", data: { enabled: true, applied: false } });

        const wrapper = mountPage();
        await flushPromises();

        expect(wrapper.find("[data-field='sipLogEnabled']").attributes("data-tooltip")).toContain("配置尚未应用");
    });
});
