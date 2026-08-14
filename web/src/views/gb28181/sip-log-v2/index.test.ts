import { flushPromises, shallowMount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SipLogPage from "./index.vue";

const traceApi = vi.hoisted(() => ({
    buildTraceStreamUrl: vi.fn(() => "/api/gb28181/sip-traces/stream"),
    fetchTraceHealth: vi.fn(),
    fetchTraceSessionStats: vi.fn(),
    getTraceMessage: vi.fn(),
    listTraceSessionMessages: vi.fn(),
    listTraceSessions: vi.fn()
}));
const listDevices = vi.hoisted(() => vi.fn());

vi.mock("@/api/gb28181-trace", () => traceApi);
vi.mock("@/views/gb28181/device-mgmt/api", () => ({ listDevices }));
vi.mock("vue-router", () => ({ useRoute: () => ({ query: {} }) }));
vi.mock("@arco-design/web-vue", () => ({ Message: { warning: vi.fn(), error: vi.fn() } }));

class FakeEventSource {
    static instances: FakeEventSource[] = [];
    listeners = new Map<string, () => void>();
    close = vi.fn();

    constructor(public readonly url: string) {
        FakeEventSource.instances.push(this);
    }

    addEventListener(name: string, listener: () => void) {
        this.listeners.set(name, listener);
    }

    emit(name: string) {
        this.listeners.get(name)?.();
    }
}

function mountPage() {
    return shallowMount(SipLogPage, {
        global: {
            stubs: {
                "a-tooltip": { template: "<div><slot /></div>" },
                "s-layout-search": { template: "<div><slot name='fields' /><slot name='actions' /></div>" },
                "a-range-picker": true,
                "a-select": true,
                "a-option": true,
                "a-input": true,
                "a-button": { template: "<button><slot name='icon' /><slot /></button>" },
                "icon-search": true,
                "icon-refresh": true
            }
        }
    });
}

describe("SIP log workbench storage replacement regression", () => {
    beforeEach(() => {
        FakeEventSource.instances = [];
        vi.stubGlobal("EventSource", FakeEventSource);
        listDevices.mockReset().mockResolvedValue({ code: 0, data: { list: [] } });
        traceApi.fetchTraceHealth.mockReset().mockResolvedValue({
            code: 0,
            data: { state: "ready", queueDepth: 0, queueCapacity: 1024, dropped: 0 }
        });
        traceApi.fetchTraceSessionStats.mockReset().mockResolvedValue({
            code: 0,
            data: { total: 3, anomaly: 1, registerFail: 1, invitePending: 0 }
        });
        traceApi.listTraceSessions.mockReset().mockResolvedValue({ code: 0, data: { items: [] } });
        traceApi.getTraceMessage.mockReset();
        traceApi.listTraceSessionMessages.mockReset();
        traceApi.buildTraceStreamUrl.mockClear();
    });

    afterEach(() => {
        vi.useRealTimers();
        vi.unstubAllGlobals();
    });

    it("keeps the existing toolbar, stats, filters, view switch and empty table structure", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(wrapper.get(".page-title").text()).toBe("SIP 日志");
        expect(wrapper.get(".head-badge").text()).toContain("采集正常");
        expect(wrapper.find(".health-icon-ready").exists()).toBe(true);
        expect(wrapper.find(".health-icon-degraded").exists()).toBe(false);
        expect(wrapper.find(".health-icon-disabled").exists()).toBe(false);
        expect(wrapper.find(".toolbar").exists()).toBe(true);
        expect(wrapper.find(".stat-band").exists()).toBe(true);
        expect(wrapper.find(".sip-log-search").exists()).toBe(true);
        expect(wrapper.find(".view-switch").exists()).toBe(true);
        expect(wrapper.findAll(".view-btn").map(button => button.text())).toEqual(["表格", "终端"]);
        expect(wrapper.find(".workspace").exists()).toBe(true);
        expect(wrapper.findComponent({ name: "TableView" }).props("sessions")).toEqual([]);
        expect(wrapper.text()).toContain("全部会话");
        expect(wrapper.text()).toContain("异常会话");
        expect(traceApi.listTraceSessions).toHaveBeenCalledWith(expect.objectContaining({ limit: 200 }));
        expect(FakeEventSource.instances).toHaveLength(1);
    });

    it("shows the existing degraded state when the health API is unavailable", async () => {
        traceApi.fetchTraceHealth.mockRejectedValue(new Error("database unavailable"));
        const wrapper = mountPage();
        await flushPromises();

        expect(wrapper.get(".head-badge").text()).toContain("存储降级");
        expect(wrapper.find(".health-icon-ready").exists()).toBe(false);
        expect(wrapper.find(".health-icon-degraded").exists()).toBe(true);
        expect(wrapper.find(".stat-band").exists()).toBe(true);
        expect(wrapper.find(".workspace").exists()).toBe(true);
    });

    it("uses a disabled icon when trace collection is not enabled", async () => {
        traceApi.fetchTraceHealth.mockResolvedValue({
            code: 0,
            data: { state: "disabled", queueDepth: 0, queueCapacity: 0, dropped: 0 }
        });
        const wrapper = mountPage();
        await flushPromises();

        expect(wrapper.get(".head-badge").text()).toContain("未启用");
        expect(wrapper.find(".health-icon-ready").exists()).toBe(false);
        expect(wrapper.find(".health-icon-degraded").exists()).toBe(false);
        expect(wrapper.find(".health-icon-disabled").exists()).toBe(true);
    });

    it("searches immediately when switching the session scope cards", async () => {
        const wrapper = mountPage();
        await flushPromises();

        await wrapper.findAll(".stat-card")[1].trigger("click");
        await flushPromises();

        expect(traceApi.listTraceSessions).toHaveBeenCalledTimes(2);
        expect(traceApi.listTraceSessions).toHaveBeenLastCalledWith(expect.objectContaining({ anomaly: true }));
        expect(traceApi.fetchTraceSessionStats).toHaveBeenCalledTimes(2);
    });

    it("uses the existing SSE message event to refresh sessions and stats", async () => {
        vi.useFakeTimers();
        const wrapper = mountPage();
        await flushPromises();
        expect(traceApi.listTraceSessions).toHaveBeenCalledTimes(1);

        FakeEventSource.instances[0].emit("message");
        await vi.advanceTimersByTimeAsync(500);
        await flushPromises();

        expect(traceApi.listTraceSessions).toHaveBeenCalledTimes(2);
        expect(traceApi.fetchTraceSessionStats).toHaveBeenCalledTimes(2);
        wrapper.unmount();
        expect(FakeEventSource.instances[0].close).toHaveBeenCalledOnce();
    });
});
