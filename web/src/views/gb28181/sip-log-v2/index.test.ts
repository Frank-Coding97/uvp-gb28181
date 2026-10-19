import { nextTick } from "vue";
import { flushPromises, shallowMount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import dayjs from "dayjs";
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

vi.mock("@/api/gb28181-trace", () => traceApi);
vi.mock("vue-router", () => ({ useRoute: () => ({ query: {} }), useRouter: () => ({ push: vi.fn() }) }));
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
    expect(wrapper.findAll("a-select-stub")).toHaveLength(2);
    expect(wrapper.get("a-input-stub").attributes("placeholder")).toBe("搜索设备 ID、名称或 Call-ID");
    expect(wrapper.find(".view-switch").exists()).toBe(true);
    expect(wrapper.findAll(".view-btn").map(button => button.text())).toEqual(["表格", "终端"]);
    expect(wrapper.find(".toolbar-right .icon-btn").exists()).toBe(false);
    expect(wrapper.get(".filter-refresh-btn").text()).toContain("刷新");
    expect(wrapper.text().match(/实时/g)).toHaveLength(1);
    expect(wrapper.find(".workspace").exists()).toBe(true);
    expect(wrapper.findComponent({ name: "TableView" }).props("sessions")).toEqual([]);
    expect(wrapper.text()).toContain("全部会话");
    expect(wrapper.text()).toContain("异常会话");
    expect(traceApi.listTraceSessions).toHaveBeenCalledWith(expect.objectContaining({ limit: 200 }));
    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it("hides the page status toolbar while viewing a session detail", async () => {
    const session = {
      day: "2026-08-24T00:00:00Z",
      deviceId: "device-a",
      callId: "detail-call",
      firstAt: "2026-08-24T09:00:00Z",
      lastAt: "2026-08-24T09:00:01Z",
      messageCount: 2,
      inboundCount: 1,
      outboundCount: 1,
      methods: ["MESSAGE"],
      finalStatus: 200,
      firstMethod: "MESSAGE",
      requestCount: 1,
      finalResponseCount: 1,
      businessCode: "keepalive" as const,
      businessType: "心跳保持",
      originalAvailable: true,
      originalExpiresAt: "2026-08-31T09:00:01Z",
      missingResponse: false,
      anomaly: false
    };
    traceApi.listTraceSessions.mockResolvedValue({ code: 0, data: { items: [session] } });
    traceApi.listTraceSessionMessages.mockResolvedValue({ code: 0, data: { items: [] } });
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find(".toolbar").exists()).toBe(true);
    wrapper.findComponent({ name: "TableView" }).vm.$emit("selectSession", session);
    await flushPromises();

    expect(wrapper.find(".toolbar").exists()).toBe(false);
    expect(wrapper.findComponent({ name: "SessionDetail" }).exists()).toBe(true);
  });

  it("tracks the latest five minutes and restores that mode when reset", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-24T13:57:02+08:00"));

    const wrapper = mountPage();
    await flushPromises();

    const vm = wrapper.vm as unknown as {
      filters: { range: string[]; scope: string; diagnosisCode: string; keyword: string };
      autoTraceRange: boolean;
      traceWindowPreset: 5 | 10 | 30 | 60 | "custom";
      refresh: () => Promise<void>;
      onRangeChange: (range?: string[]) => void;
      selectTimePreset: (preset: 5 | 10 | 30 | 60) => Promise<void>;
      resetFilters: () => void;
    };
    expect(vm.autoTraceRange).toBe(true);
    expect(vm.traceWindowPreset).toBe(5);
    expect(vm.filters.range).toEqual([]);

    vi.setSystemTime(new Date("2026-08-24T13:59:02+08:00"));
    await vm.refresh();
    expect(vm.filters.range).toEqual([]);
    expect(traceApi.listTraceSessions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        from: dayjs("2026-08-24 13:54:02").toISOString(),
        to: dayjs("2026-08-24 13:59:02").toISOString()
      })
    );

    await vm.selectTimePreset(30);
    expect(vm.traceWindowPreset).toBe(30);
    expect(traceApi.listTraceSessions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        from: dayjs("2026-08-24 13:29:02").toISOString(),
        to: dayjs("2026-08-24 13:59:02").toISOString()
      })
    );

    vm.filters.range = ["2026-08-23 09:00:00", "2026-08-23 10:00:00"];
    vm.onRangeChange(vm.filters.range);
    expect(vm.autoTraceRange).toBe(false);
    expect(vm.traceWindowPreset).toBe("custom");
    vi.setSystemTime(new Date("2026-08-24T14:01:02+08:00"));
    await vm.refresh();
    expect(vm.filters.range).toEqual(["2026-08-23 09:00:00", "2026-08-23 10:00:00"]);

    vm.resetFilters();
    expect(vm.autoTraceRange).toBe(true);
    expect(vm.traceWindowPreset).toBe(5);
    expect(vm.filters.range).toEqual([]);
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
    expect(traceApi.listTraceSessions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        diagnosisCategory: "register_failure"
      })
    );

    await wrapper.findAll(".stat-card")[3].trigger("click");
    await flushPromises();
    expect(traceApi.listTraceSessions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        diagnosisCategory: "play_stuck"
      })
    );
    expect(traceApi.fetchTraceSessionStats).toHaveBeenCalledTimes(1);
    expect(traceApi.fetchTraceSessionStats).toHaveBeenLastCalledWith(
      expect.not.objectContaining({
        diagnosisCategory: expect.anything(),
        diagnosisCode: expect.anything()
      })
    );
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
      applyFilters: () => Promise<void>;
    };
    vm.filters.diagnosisCode = "media_timeout";
    await vm.applyFilters();

    expect(traceApi.listTraceSessions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        diagnosisCategory: "play_stuck",
        diagnosisCode: "media_timeout"
      })
    );
  });

  it("warns when diagnosis persistence is degraded without hiding the workbench", async () => {
    traceApi.fetchTraceHealth.mockResolvedValue({
      code: 0,
      data: {
        state: "ready",
        queueDepth: 0,
        queueCapacity: 1024,
        dropped: 0,
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

  it("keeps the initial load visible, but leaves existing rows and stats interactive during live refresh", async () => {
    vi.useFakeTimers();
    const initialSessions = deferred<{ code: number; data: { items: any[] } }>();
    const initialStats = deferred<{ code: number; data: any }>();
    traceApi.listTraceSessions.mockReturnValueOnce(initialSessions.promise);
    traceApi.fetchTraceSessionStats.mockReturnValueOnce(initialStats.promise);

    const wrapper = mountPage();
    await flushPromises();
    expect((wrapper.vm as unknown as { sessionLoading: boolean }).sessionLoading).toBe(true);
    initialSessions.resolve({ code: 0, data: { items: [testSession("initial-call")] } });
    initialStats.resolve({ code: 0, data: { total: 7, anomaly: 2, registerFail: 1, playStuck: 0, invitePending: 0 } });
    await flushPromises();

    const nextSessions = deferred<{ code: number; data: { items: any[] } }>();
    const nextStats = deferred<{ code: number; data: any }>();
    traceApi.listTraceSessions.mockReturnValueOnce(nextSessions.promise);
    traceApi.fetchTraceSessionStats.mockReturnValueOnce(nextStats.promise);
    FakeEventSource.instances[0].emit("message");
    await vi.advanceTimersByTimeAsync(500);

    expect(wrapper.findComponent({ name: "TableView" }).props("loading")).toBe(false);
    expect(wrapper.findComponent({ name: "TableView" }).props("sessions")).toEqual([testSession("initial-call")]);
    expect(wrapper.findAll(".stat-num").map(item => item.text())).toEqual(["7", "2", "1", "0"]);

    wrapper.findComponent({ name: "TableView" }).vm.$emit("selectSession", testSession("initial-call"));
    await flushPromises();
    expect(wrapper.findComponent({ name: "SessionDetail" }).props("session").callId).toBe("initial-call");
    expect(FakeEventSource.instances[0].close).toHaveBeenCalledOnce();

    nextSessions.resolve({ code: 0, data: { items: [testSession("updated-call")] } });
    nextStats.resolve({ code: 0, data: { total: 8, anomaly: 3, registerFail: 1, playStuck: 1, invitePending: 0 } });
    await flushPromises();
    expect(wrapper.findComponent({ name: "SessionDetail" }).props("session").callId).toBe("initial-call");
    wrapper.findComponent({ name: "SessionDetail" }).vm.$emit("back");
    await flushPromises();
    expect(wrapper.findComponent({ name: "TableView" }).props("sessions")).toEqual([testSession("updated-call")]);
    expect(FakeEventSource.instances).toHaveLength(2);
    wrapper.unmount();
  });

  it("serializes live refresh batches and coalesces events received while a batch is running", async () => {
    vi.useFakeTimers();
    const wrapper = mountPage();
    await flushPromises();

    const firstSessions = deferred<{ code: number; data: { items: any[] } }>();
    const firstStats = deferred<{ code: number; data: any }>();
    traceApi.listTraceSessions.mockReturnValueOnce(firstSessions.promise);
    traceApi.fetchTraceSessionStats.mockReturnValueOnce(firstStats.promise);
    FakeEventSource.instances[0].emit("message");
    FakeEventSource.instances[0].emit("message");
    FakeEventSource.instances[0].emit("message");
    await vi.advanceTimersByTimeAsync(500);
    expect(traceApi.listTraceSessions).toHaveBeenCalledTimes(2);

    const queuedSessions = deferred<{ code: number; data: { items: any[] } }>();
    const queuedStats = deferred<{ code: number; data: any }>();
    traceApi.listTraceSessions.mockReturnValueOnce(queuedSessions.promise);
    traceApi.fetchTraceSessionStats.mockReturnValueOnce(queuedStats.promise);
    FakeEventSource.instances[0].emit("message");
    await vi.advanceTimersByTimeAsync(500);
    FakeEventSource.instances[0].emit("message");
    FakeEventSource.instances[0].emit("message");
    await vi.advanceTimersByTimeAsync(500);
    expect(traceApi.listTraceSessions).toHaveBeenCalledTimes(2);

    firstSessions.resolve({ code: 0, data: { items: [] } });
    firstStats.resolve({ code: 0, data: { total: 1, anomaly: 0, registerFail: 0, playStuck: 0, invitePending: 0 } });
    await flushPromises();
    expect(traceApi.listTraceSessions).toHaveBeenCalledTimes(3);

    queuedSessions.resolve({ code: 0, data: { items: [] } });
    queuedStats.resolve({ code: 0, data: { total: 2, anomaly: 0, registerFail: 0, playStuck: 0, invitePending: 0 } });
    await flushPromises();
    wrapper.unmount();
  });

  it("applies business type to both session and stats queries and keeps the table responsive while changing filters", async () => {
    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.findAll("a-select-stub").map(select => select.attributes("placeholder"))).toContain("全部业务类型");

    const nextSessions = deferred<{ code: number; data: { items: any[] } }>();
    const nextStats = deferred<{ code: number; data: any }>();
    traceApi.listTraceSessions.mockReturnValueOnce(nextSessions.promise);
    traceApi.fetchTraceSessionStats.mockReturnValueOnce(nextStats.promise);
    const vm = wrapper.vm as unknown as {
      filters: { businessCode: string };
      search: () => Promise<void>;
    };
    vm.filters.businessCode = "unknown";
    const search = vm.search();
    await nextTick();

    expect(wrapper.findComponent({ name: "TableView" }).props("loading")).toBe(true);
    expect(traceApi.listTraceSessions).toHaveBeenLastCalledWith(expect.objectContaining({ businessCode: "unknown" }));
    expect(traceApi.fetchTraceSessionStats).toHaveBeenLastCalledWith(expect.objectContaining({ businessCode: "unknown" }));
    nextSessions.resolve({ code: 0, data: { items: [] } });
    nextStats.resolve({ code: 0, data: { total: 4, anomaly: 0, registerFail: 0, playStuck: 0, invitePending: 0 } });
    await search;
    wrapper.unmount();
  });

  it("does not let an older query overwrite a newer filter result, and pauses live updates inside session detail", async () => {
    const wrapper = mountPage();
    await flushPromises();
    const oldSessions = deferred<{ code: number; data: { items: any[] } }>();
    const oldStats = deferred<{ code: number; data: any }>();
    traceApi.listTraceSessions.mockReturnValueOnce(oldSessions.promise);
    traceApi.fetchTraceSessionStats.mockReturnValueOnce(oldStats.promise);

    const vm = wrapper.vm as unknown as {
      filters: { businessCode: string };
      search: () => Promise<void>;
    };
    vm.filters.businessCode = "register";
    const olderSearch = vm.search();
    expect(traceApi.listTraceSessions).toHaveBeenLastCalledWith(expect.objectContaining({ businessCode: "register" }));

    vm.filters.businessCode = "unknown";
    const newerSearch = vm.search();
    await nextTick();
    expect(wrapper.findComponent({ name: "TableView" }).props("loading")).toBe(true);
    const newestSessions = deferred<{ code: number; data: { items: any[] } }>();
    const newestStats = deferred<{ code: number; data: any }>();
    traceApi.listTraceSessions.mockReturnValueOnce(newestSessions.promise);
    traceApi.fetchTraceSessionStats.mockReturnValueOnce(newestStats.promise);
    oldSessions.resolve({ code: 0, data: { items: [testSession("stale-call")] } });
    oldStats.resolve({ code: 0, data: { total: 99, anomaly: 0, registerFail: 0, playStuck: 0, invitePending: 0 } });
    await flushPromises();
    expect(traceApi.listTraceSessions).toHaveBeenLastCalledWith(expect.objectContaining({ businessCode: "unknown" }));
    newestSessions.resolve({ code: 0, data: { items: [testSession("current-call")] } });
    newestStats.resolve({ code: 0, data: { total: 3, anomaly: 0, registerFail: 0, playStuck: 0, invitePending: 0 } });
    await Promise.all([olderSearch, newerSearch]);

    const table = wrapper.findComponent({ name: "TableView" });
    table.vm.$emit("selectSession", testSession("current-call"));
    await flushPromises();
    expect(wrapper.findComponent({ name: "SessionDetail" }).exists()).toBe(true);
    expect(FakeEventSource.instances[0].close).toHaveBeenCalledOnce();
    expect(wrapper.findComponent({ name: "SessionDetail" }).props("session").callId).toBe("current-call");

    wrapper.findComponent({ name: "SessionDetail" }).vm.$emit("back");
    await flushPromises();
    expect(wrapper.findComponent({ name: "TableView" }).exists()).toBe(true);
    expect(FakeEventSource.instances).toHaveLength(2);
    wrapper.unmount();
  });

  it("opens a transport event without inventing a SIP Call-ID", async () => {
    const session = { ...testSession(""), eventId: "transport-a", businessCode: "transport_keepalive" };
    traceApi.listTraceSessions.mockResolvedValue({ code: 0, data: { items: [session] } });
    traceApi.getTraceMessage.mockResolvedValue({ code: 0, data: { eventId: "transport-a", callId: "", payload: "\r\n" } });
    const wrapper = mountPage();
    await flushPromises();
    wrapper.findComponent({ name: "TableView" }).vm.$emit("selectSession", session);
    await flushPromises();
    expect(wrapper.findComponent({ name: "SessionDetail" }).exists()).toBe(true);
    expect(traceApi.getTraceMessage).toHaveBeenCalledWith("transport-a");
    expect(traceApi.listTraceSessionMessages).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("does not write state after unmount while a live batch is still pending", async () => {
    vi.useFakeTimers();
    const wrapper = mountPage();
    await flushPromises();
    const pendingSessions = deferred<{ code: number; data: { items: any[] } }>();
    const pendingStats = deferred<{ code: number; data: any }>();
    traceApi.listTraceSessions.mockReturnValueOnce(pendingSessions.promise);
    traceApi.fetchTraceSessionStats.mockReturnValueOnce(pendingStats.promise);
    FakeEventSource.instances[0].emit("message");
    await vi.advanceTimersByTimeAsync(500);
    const vm = wrapper.vm as unknown as { sessions: any[]; stats: any };
    const previousSessions = vm.sessions;
    const previousStats = vm.stats;
    wrapper.unmount();

    pendingSessions.resolve({ code: 0, data: { items: [testSession("after-unmount")] } });
    pendingStats.resolve({ code: 0, data: { total: 100, anomaly: 0, registerFail: 0, playStuck: 0, invitePending: 0 } });
    await flushPromises();
    expect(vm.sessions).toBe(previousSessions);
    expect(vm.stats).toBe(previousStats);
  });
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function testSession(callId: string) {
  return {
    day: "2026-08-24T00:00:00Z",
    deviceId: "device-a",
    callId,
    firstAt: "2026-08-24T09:00:00Z",
    lastAt: "2026-08-24T09:00:01Z",
    messageCount: 2,
    inboundCount: 1,
    outboundCount: 1,
    methods: ["REGISTER"],
    finalStatus: 200,
    firstMethod: "REGISTER",
    requestCount: 1,
    finalResponseCount: 1,
    businessCode: "register" as const,
    businessType: "注册",
    originalAvailable: true,
    originalExpiresAt: "2026-08-31T09:00:01Z",
    missingResponse: false,
    anomaly: false
  };
}

describe("SIP session diagnosis labels", () => {
  const baseSession = {
    day: "2026-08-14T00:00:00Z",
    deviceId: "device-a",
    callId: "call-a",
    firstAt: "2026-08-14T00:00:00Z",
    lastAt: "2026-08-14T00:00:01Z",
    messageCount: 2,
    inboundCount: 1,
    outboundCount: 1,
    methods: ["REGISTER"],
    finalStatus: 401,
    firstMethod: "REGISTER",
    requestCount: 1,
    finalResponseCount: 1,
    businessCode: "register" as const,
    businessType: "注册",
    originalAvailable: true,
    originalExpiresAt: "2026-08-21T00:00:01Z",
    missingResponse: false,
    anomaly: false
  };

  it("does not call a normal 401 challenge a registration failure", () => {
    expect(sessionStateLabel(baseSession).label).toBe("认证挑战");
  });

  it("uses the backend media diagnosis wording without blaming the device", () => {
    expect(
      sessionStateLabel({
        ...baseSession,
        methods: ["INVITE"],
        finalStatus: 200,
        firstMethod: "INVITE",
        diagnosis: {
          category: "play_stuck",
          code: "media_timeout",
          stage: "media",
          source: "runtime",
          observedAt: "2026-08-14T00:00:02Z"
        }
      }).label
    ).toBe("信令成功，媒体未就绪");
  });
});
