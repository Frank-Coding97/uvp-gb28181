import { flushPromises, shallowMount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SipLogPage from "./index.vue";
import { sessionStateLabel } from "./helpers";

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
            data: { total: 3, anomaly: 1, registerFail: 1, playStuck: 0, invitePending: 0 }
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
        expect(wrapper.text().match(/实时/g)).toHaveLength(1);
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

    it("searches immediately when switching all session scope cards without narrowing stats", async () => {
        const wrapper = mountPage();
        await flushPromises();

        await wrapper.findAll(".stat-card")[1].trigger("click");
        await flushPromises();

        expect(traceApi.listTraceSessions).toHaveBeenCalledTimes(2);
        expect(traceApi.listTraceSessions).toHaveBeenLastCalledWith(expect.objectContaining({ anomaly: true }));
        expect(traceApi.fetchTraceSessionStats).toHaveBeenCalledTimes(1);

        await wrapper.findAll(".stat-card")[2].trigger("click");
        await flushPromises();
        expect(traceApi.listTraceSessions).toHaveBeenLastCalledWith(expect.objectContaining({
            diagnosisCategory: "register_failure"
        }));

        await wrapper.findAll(".stat-card")[3].trigger("click");
        await flushPromises();
        expect(traceApi.listTraceSessions).toHaveBeenLastCalledWith(expect.objectContaining({
            diagnosisCategory: "play_stuck"
        }));
        expect(traceApi.fetchTraceSessionStats).toHaveBeenCalledTimes(1);
        expect(traceApi.fetchTraceSessionStats).toHaveBeenLastCalledWith(expect.not.objectContaining({
            diagnosisCategory: expect.anything(), diagnosisCode: expect.anything()
        }));
    });

    it("shows an explicit stats error instead of turning it into zero", async () => {
        traceApi.fetchTraceSessionStats.mockRejectedValue(new Error("stats unavailable"));
        const wrapper = mountPage();
        await flushPromises();

        const values = wrapper.findAll(".stat-num").map(item => item.text());
        expect(values).toEqual(["--", "--", "--", "--"]);
    });

    it("applies a diagnosis subtype immediately within the selected category", async () => {
        const wrapper = mountPage();
        await flushPromises();
        await wrapper.findAll(".stat-card")[3].trigger("click");
        await flushPromises();

        const vm = wrapper.vm as unknown as {
            filters: { diagnosisCode: string };
            loadSessions: () => Promise<void>;
        };
        vm.filters.diagnosisCode = "media_timeout";
        await vm.loadSessions();

        expect(traceApi.listTraceSessions).toHaveBeenLastCalledWith(expect.objectContaining({
            diagnosisCategory: "play_stuck",
            diagnosisCode: "media_timeout"
        }));
    });

    it("warns when diagnosis persistence is degraded without hiding the workbench", async () => {
        traceApi.fetchTraceHealth.mockResolvedValue({
            code: 0,
            data: {
                state: "ready", queueDepth: 0, queueCapacity: 1024, dropped: 0,
                diagnosis: { state: "degraded", queueDepth: 2, queueCapacity: 64, dropped: 1, failed: 0 }
            }
        });
        const wrapper = mountPage();
        await flushPromises();

        expect(wrapper.get(".diagnosis-warning").text()).toContain("诊断数据可能不完整");
        expect(wrapper.find(".workspace").exists()).toBe(true);
    });

    it("keeps real zero stats distinguishable from a failed request", async () => {
        traceApi.fetchTraceSessionStats.mockResolvedValue({
            code: 0,
            data: { total: 0, anomaly: 0, registerFail: 0, playStuck: 0, invitePending: 0 }
        });
        const wrapper = mountPage();
        await flushPromises();

        expect(wrapper.findAll(".stat-num").map(item => item.text())).toEqual(["0", "0", "0", "0"]);
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

describe("SIP session diagnosis labels", () => {
    const baseSession = {
        day: "2026-08-14T00:00:00Z", deviceId: "device-a", callId: "call-a",
        firstAt: "2026-08-14T00:00:00Z", lastAt: "2026-08-14T00:00:01Z",
        messageCount: 2, inboundCount: 1, outboundCount: 1, methods: ["REGISTER"],
        finalStatus: 401, firstMethod: "REGISTER", requestCount: 1, finalResponseCount: 1,
        originalAvailable: true, originalExpiresAt: "2026-08-21T00:00:01Z",
        missingResponse: false, anomaly: false
    };

    it("does not call a normal 401 challenge a registration failure", () => {
        expect(sessionStateLabel(baseSession).label).toBe("认证挑战");
    });

    it("uses the backend media diagnosis wording without blaming the device", () => {
        expect(sessionStateLabel({
            ...baseSession,
            methods: ["INVITE"],
            finalStatus: 200,
            firstMethod: "INVITE",
            diagnosis: {
                category: "play_stuck", code: "media_timeout", stage: "media",
                source: "runtime", observedAt: "2026-08-14T00:00:02Z"
            }
        }).label).toBe("信令成功，媒体未就绪");
    });
});
