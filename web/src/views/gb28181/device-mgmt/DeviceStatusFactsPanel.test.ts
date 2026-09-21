import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DeviceStatusFactsPanel from "./DeviceStatusFactsPanel.vue";

/**
 * 设备状态事实面板（从播放控制台搬到设备详情抽屉）的单测。
 *
 * ⛔ 这里覆盖的是**面板自己的协议时序**：静默读缓存 / 下发后按 operation 轮询到终态 /
 *    终态后补读一次 / 换通道丢弃迟到结果。这些用例原来挂在 PlayConsoleLinked 的
 *    `[data-testid='advanced-fact-status']` 上，控制台那份入口 2026-09-20 已摘除，
 *    于是随逻辑一起搬到这里 —— 别把断言改回控制台。
 */
const api = vi.hoisted(() => ({
  getDeviceStatus: vi.fn(),
  getPtzOperation: vi.fn()
}));

vi.mock("@/api/gb28181", () => api);

const CHANNEL_ID = 1;
const OTHER_CHANNEL_ID = 2;

function option(value: number) {
  return { value, label: `通道 ${value}`, online: true };
}

function statusResponse(overrides: Record<string, unknown> = {}) {
  return {
    code: 0,
    message: "",
    data: {
      state: { recordState: "unknown", guardState: "unknown", freshness: "unknown" },
      freshness: "unknown",
      refreshOperationId: null,
      refreshError: null,
      ...overrides
    }
  };
}

function operationResponse(
  status: "queued" | "sent" | "accepted" | "rejected" | "timeout" | "unknown" | "cancelled",
  operationId: string,
  deadlineAt: string | null = null,
  errorCode: string | null = null
) {
  return {
    code: 0,
    message: "",
    data: {
      operationId,
      status,
      errorCode,
      errorMessage: errorCode,
      completedAt: ["accepted", "rejected", "timeout", "unknown", "cancelled"].includes(status) ? "2026-09-20T10:00:05Z" : null,
      deadlineAt
    }
  };
}

function mountPanel(props: Record<string, unknown> = {}) {
  return mount(DeviceStatusFactsPanel, {
    props: {
      channelId: CHANNEL_ID,
      channelOptions: [option(CHANNEL_ID)],
      active: true,
      canView: true,
      ...props
    }
  });
}

async function clickRefresh(wrapper: ReturnType<typeof mountPanel>) {
  await wrapper.get("[data-testid='fact-refresh']").trigger("click");
  await flushPromises();
}

describe("DeviceStatusFactsPanel", () => {
  beforeEach(() => {
    api.getDeviceStatus.mockReset();
    api.getPtzOperation.mockReset();
    api.getDeviceStatus.mockResolvedValue(statusResponse());
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("打开这一页只读平台缓存，不发查询报文", async () => {
    api.getDeviceStatus.mockResolvedValue(
      statusResponse({
        state: { recordState: "off", guardState: "armed", freshness: "fresh" },
        freshness: "fresh"
      })
    );
    const wrapper = mountPanel();
    await flushPromises();

    // 不带 refresh：这是纯缓存读，SIP 侧不应该有任何动作。
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(1);
    expect(api.getDeviceStatus).toHaveBeenLastCalledWith(CHANNEL_ID, false);
    const text = wrapper.text();
    expect(text).toContain("设备未录制");
    expect(text).toContain("已布防");
    wrapper.unmount();
  });

  it("渲染设备自报的在线、自检、编码与时间偏差", async () => {
    api.getDeviceStatus.mockResolvedValue(
      statusResponse({
        state: { recordState: "on", guardState: "unknown", freshness: "fresh" },
        freshness: "fresh",
        deviceReport: {
          online: "online",
          selfTest: "ok",
          encode: "on",
          deviceTime: "2026-09-19T20:03:58",
          // ±1 秒是设备时间只到秒的量化误差，不当偏差报。
          clockSkewSeconds: 1,
          alarmInputCount: 0
        }
      })
    );
    const wrapper = mountPanel();
    await flushPromises();

    const text = wrapper.text();
    expect(text).toContain("在线");
    expect(text).toContain("自检正常");
    expect(text).toContain("编码中");
    expect(text).toContain("与平台一致");
    wrapper.unmount();
  });

  it("设备明确回了 0 个报警输入时显示「设备无报警输入」而不是「未知」", async () => {
    api.getDeviceStatus.mockResolvedValue(
      statusResponse({
        state: { recordState: "on", guardState: "unknown", freshness: "fresh" },
        freshness: "fresh",
        alarmResolution: {
          status: "unavailable",
          source: "",
          targetCode: "",
          state: "unknown",
          freshness: "unknown",
          candidates: []
        },
        deviceReport: { alarmInputCount: 0 }
      })
    );
    const wrapper = mountPanel();
    await flushPromises();

    expect(wrapper.text()).toContain("设备无报警输入");
    expect(wrapper.get("[data-testid='alarm-resolution-warning']").text()).toContain("设备自报没有报警输入通道");
    wrapper.unmount();
  });

  it("设备没上报那些事实时显示「未上报」而不是「已停」", async () => {
    api.getDeviceStatus.mockResolvedValue(
      statusResponse({
        state: { recordState: "on", guardState: "unknown", freshness: "fresh" },
        freshness: "fresh",
        deviceReport: {
          online: null,
          selfTest: null,
          encode: null,
          deviceTime: null,
          clockSkewSeconds: null,
          alarmInputCount: null
        }
      })
    );
    const wrapper = mountPanel();
    await flushPromises();

    const text = wrapper.text();
    expect(text).toContain("未上报");
    expect(text).not.toContain("编码已停");
    expect(text).not.toContain("自检异常");
    expect(text).not.toContain("设备无报警输入");
    wrapper.unmount();
  });

  it("向设备查询后按 operation 等到终态，再补读一次合并事实", async () => {
    vi.useFakeTimers();
    api.getDeviceStatus
      // 第 1 格给"挂载时的静默读"，下面 mockClear 只清调用记录、不动队列。
      .mockResolvedValueOnce(statusResponse())
      .mockResolvedValueOnce(statusResponse({ refreshOperationId: "device-status-op" }))
      .mockResolvedValueOnce(
        statusResponse({
          state: { recordState: "on", guardState: "on", freshness: "fresh" },
          freshness: "fresh"
        })
      );
    api.getPtzOperation
      .mockResolvedValueOnce(operationResponse("sent", "device-status-op"))
      .mockResolvedValueOnce(operationResponse("accepted", "device-status-op"));

    const wrapper = mountPanel();
    await flushPromises();
    api.getDeviceStatus.mockClear();

    await clickRefresh(wrapper);
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(1);
    expect(api.getDeviceStatus).toHaveBeenLastCalledWith(CHANNEL_ID, true);
    // 还没到 operation 终态，事实不能当作"刚问出来的"。
    expect(wrapper.text()).toContain("正在查询设备状态");

    await vi.advanceTimersByTimeAsync(1000);
    expect(api.getPtzOperation).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(2000);
    await flushPromises();
    expect(api.getPtzOperation).toHaveBeenCalledTimes(2);
    expect(api.getPtzOperation).toHaveBeenLastCalledWith(CHANNEL_ID, "device-status-op");
    // 终态后的补读是 refresh=false 的缓存读 —— 权威结论来自补读，不是下发应答。
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(2);
    expect(api.getDeviceStatus).toHaveBeenLastCalledWith(CHANNEL_ID, false);

    const text = wrapper.text();
    expect(text).toContain("设备录制中");
    expect(text).toContain("已布防");
    wrapper.unmount();
  });

  it("录像与报警两个 operation 都到终态后只补读一次", async () => {
    vi.useFakeTimers();
    api.getDeviceStatus
      .mockResolvedValueOnce(statusResponse())
      .mockResolvedValueOnce(
        statusResponse({
          refreshOperationId: "record-status-op",
          recordRefreshOperationId: "record-status-op",
          alarmRefreshOperationId: "alarm-status-op",
          refreshOperationIds: { record: "record-status-op", alarm: "alarm-status-op" }
        })
      )
      .mockResolvedValueOnce(
        statusResponse({
          state: { recordState: "on", guardState: "alarm", freshness: "fresh" },
          recordState: "on",
          guardState: "alarm",
          freshness: "fresh",
          alarmResolution: {
            status: "resolved",
            source: "direct_parent",
            targetCode: "A1",
            state: "alarm",
            freshness: "fresh",
            candidates: [{ code: "A1", name: "门磁" }]
          },
          alarmFacts: [{ targetCode: "A1", guardState: "alarm", freshness: "fresh" }]
        })
      );
    let alarmPolls = 0;
    api.getPtzOperation.mockImplementation((_channelId: number, operationId: string) => {
      if (operationId === "record-status-op") return Promise.resolve(operationResponse("accepted", operationId));
      alarmPolls += 1;
      return Promise.resolve(operationResponse(alarmPolls === 1 ? "sent" : "accepted", operationId));
    });

    const wrapper = mountPanel();
    await flushPromises();
    api.getDeviceStatus.mockClear();

    await clickRefresh(wrapper);
    await vi.advanceTimersByTimeAsync(1000);
    await flushPromises();
    expect(api.getPtzOperation).toHaveBeenCalledWith(CHANNEL_ID, "record-status-op");
    expect(api.getPtzOperation).toHaveBeenCalledWith(CHANNEL_ID, "alarm-status-op");
    // 还有一个 operation 没终态：此时不能补读（只算"点查询"那一次）。
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(2000);
    await flushPromises();
    expect(api.getPtzOperation.mock.calls.filter(([, id]) => id === "record-status-op")).toHaveLength(1);
    expect(api.getPtzOperation.mock.calls.filter(([, id]) => id === "alarm-status-op")).toHaveLength(2);
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(2);
    expect(api.getDeviceStatus).toHaveBeenLastCalledWith(CHANNEL_ID, false);

    const text = wrapper.text();
    expect(text).toContain("设备录制中");
    expect(text).toContain("ALARM 报警中");
    expect(text).toContain("A1");
    wrapper.unmount();
  });

  it("非终态 operation 以**服务端给的 deadline** 收敛，到点才补读合并事实", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-20T03:00:00.000Z"));
    // 服务端的截止时间优先于本地退避节奏：到点就该收尾，不多等一轮。
    const alarmDeadline = "2026-09-20T03:00:01.500Z";
    api.getDeviceStatus
      .mockResolvedValueOnce(statusResponse())
      .mockResolvedValueOnce(
        statusResponse({
          refreshOperationIds: { record: "record-deadline-op", alarm: "alarm-deadline-op" }
        })
      )
      .mockResolvedValueOnce(
        statusResponse({
          state: { recordState: "on", guardState: "unknown", freshness: "unknown" },
          freshness: "unknown"
        })
      );
    api.getPtzOperation.mockImplementation((_channelId: number, operationId: string) => {
      if (operationId === "record-deadline-op") return Promise.resolve(operationResponse("accepted", operationId));
      return Promise.resolve(operationResponse("sent", operationId, alarmDeadline));
    });

    const wrapper = mountPanel();
    await flushPromises();
    api.getDeviceStatus.mockClear();

    await clickRefresh(wrapper);
    await vi.advanceTimersByTimeAsync(1000);
    await flushPromises();
    // 此刻只发生"点查询"那一次；还在等 alarm operation 的服务端 deadline。
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(499);
    await flushPromises();
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(1);
    await flushPromises();
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(2);
    expect(api.getDeviceStatus).toHaveBeenLastCalledWith(CHANNEL_ID, false);
    wrapper.unmount();
  });

  it("operation 查询卡住时按截止时间收尾为未知，并补读事实", async () => {
    vi.useFakeTimers();
    api.getDeviceStatus
      .mockResolvedValueOnce(statusResponse())
      .mockResolvedValueOnce(statusResponse({ refreshOperationId: "stuck-op" }))
      .mockResolvedValueOnce(statusResponse({ refreshError: "设备状态查询超时，结果未知" }));
    // operation 一直停在非终态；断言的是"不会永远转圈"，不是某个具体错误码。
    api.getPtzOperation.mockResolvedValue(operationResponse("sent", "stuck-op"));

    const wrapper = mountPanel();
    await flushPromises();
    api.getDeviceStatus.mockClear();

    await clickRefresh(wrapper);
    await vi.advanceTimersByTimeAsync(16000);
    await flushPromises();

    expect(api.getDeviceStatus).toHaveBeenLastCalledWith(CHANNEL_ID, false);
    expect(wrapper.text()).toContain("设备状态查询超时");
    expect(wrapper.text()).not.toContain("正在查询设备状态");
    wrapper.unmount();
  });

  it("最终补读卡住时不会一直停在「正在查询设备状态」", async () => {
    vi.useFakeTimers();
    api.getDeviceStatus
      .mockResolvedValueOnce(statusResponse())
      .mockResolvedValueOnce(statusResponse({ refreshOperationId: "op-1" }))
      // 补读挂住：永不 resolve，只能靠 5s 兜底收尾。
      .mockReturnValue(new Promise(() => undefined));
    api.getPtzOperation.mockResolvedValue(operationResponse("accepted", "op-1"));

    const wrapper = mountPanel();
    await flushPromises();
    api.getDeviceStatus.mockClear();

    await clickRefresh(wrapper);
    await vi.advanceTimersByTimeAsync(1000);
    await flushPromises();
    // operation 已终态 ⇒ pending 立刻熄灭，不被挂住的补读按住（补读是尽力而为）。
    expect(wrapper.text()).not.toContain("正在查询设备状态");

    await vi.advanceTimersByTimeAsync(5000);
    await flushPromises();
    expect(wrapper.text()).toContain("最终补读超时");
    wrapper.unmount();
  });

  it("切换通道后丢弃迟到的旧 DeviceStatus", async () => {
    let resolveOld!: (value: unknown) => void;
    api.getDeviceStatus.mockReturnValueOnce(new Promise(resolve => (resolveOld = resolve))).mockResolvedValueOnce(
      statusResponse({
        state: { recordState: "off", guardState: "armed", freshness: "fresh" },
        freshness: "fresh"
      })
    );

    const wrapper = mountPanel();
    await wrapper.setProps({ channelId: OTHER_CHANNEL_ID, channelOptions: [option(OTHER_CHANNEL_ID)] });
    await flushPromises();

    resolveOld(
      statusResponse({
        state: { recordState: "on", guardState: "disarmed", freshness: "fresh" },
        freshness: "fresh"
      })
    );
    await flushPromises();

    const text = wrapper.text();
    expect(text).toContain("设备未录制");
    expect(text).toContain("已布防");
    expect(text).not.toContain("设备录制中");
    wrapper.unmount();
  });

  it("报警目标歧义时给出可操作的警告，而不是静默当作未知", async () => {
    api.getDeviceStatus.mockResolvedValue(
      statusResponse({
        state: { recordState: "unknown", guardState: "unknown", freshness: "fresh" },
        freshness: "fresh",
        alarmResolution: {
          status: "ambiguous",
          source: "catalog",
          targetCode: null,
          state: "unknown",
          freshness: "unknown",
          candidates: [
            { code: "A1", name: "门磁" },
            { code: "A2", name: "红外" }
          ]
        }
      })
    );
    const wrapper = mountPanel();
    await flushPromises();

    const warning = wrapper.get("[data-testid='alarm-resolution-warning']").text();
    expect(warning).toContain("报警目标不明确");
    expect(warning).toContain("A1、A2");
    wrapper.unmount();
  });

  it("没有通道时给出空态，查询按钮不可用", async () => {
    const wrapper = mountPanel({ channelId: null, channelOptions: [] });
    await flushPromises();

    expect(wrapper.text()).toContain("该设备下暂无通道，无法查询设备状态");
    expect(api.getDeviceStatus).not.toHaveBeenCalled();
    expect(wrapper.get("[data-testid='fact-refresh']").attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("没有权限时不读不查", async () => {
    const wrapper = mountPanel({ canView: false });
    await flushPromises();

    expect(api.getDeviceStatus).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});
