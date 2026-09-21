import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DeviceControlPanel from "./DeviceControlPanel.vue";

/**
 * 设备控制面板（2026-09-20 从播放控制台侧栏「高级」搬到设备详情抽屉）的单测。
 *
 * ⛔ 这个面板的价值全在**收敛规则**上，改代码前先看这四条别丢：
 *    ① `POST` 返回 200 只代表报文发出去了，本地事实**只能**由设备确认（`accepted`）改写；
 *    ② 服务端没给 `deadlineAt` 是一个独立状态：补读一次就收敛为"结果未知"，不许自己造截止时间；
 *    ③ `responseRequired === false` 的单向命令 `sent` 即止：不轮询，也不许说"已成功"；
 *    ④ 录制 / 布撤防 / 复位三组各自持锁 —— 同组互斥、跨组不互斥。
 *
 * ⚠️ `a-button` 在本仓是全局 stub（`src/test/setup.ts`），**不渲染 `disabled`/`loading`**，
 *    所以"按住不放"只能靠"再点一次不会多发一条请求"这类行为断言，别去查 `disabled` 属性。
 */
const api = vi.hoisted(() => ({
  controlDevice: vi.fn(),
  getDeviceStatus: vi.fn(),
  getPtzOperation: vi.fn()
}));

vi.mock("@/api/gb28181", () => api);

const CHANNEL_ID = 1;

function option(value: number, online = true) {
  return { value, label: `通道 ${value}`, online };
}

function statusResponse(overrides: Record<string, unknown> = {}) {
  return {
    code: 0,
    message: "",
    data: {
      state: { recordState: "off", guardState: "disarmed", freshness: "fresh" },
      freshness: "fresh",
      ...overrides
    }
  };
}

function controlResponse(operationId: string, status = "queued", overrides: Record<string, unknown> = {}) {
  return {
    code: 0,
    message: "",
    data: { operationId, status, responseRequired: true, deadlineAt: null, ...overrides }
  };
}

function operationResponse(operationId: string, status: string, deadlineAt: string | null = null) {
  return {
    code: 0,
    message: "",
    data: {
      operationId,
      status,
      responseRequired: true,
      deadlineAt,
      errorCode: null,
      errorMessage: null,
      completedAt: null
    }
  };
}

function mountPanel(props: Record<string, unknown> = {}) {
  return mount(DeviceControlPanel, {
    props: {
      channelId: CHANNEL_ID,
      channelOptions: [option(CHANNEL_ID)],
      active: true,
      canControl: true,
      ...props
    }
  });
}

describe("DeviceControlPanel", () => {
  beforeEach(() => {
    api.controlDevice.mockReset();
    api.getDeviceStatus.mockReset();
    api.getPtzOperation.mockReset();
    api.getDeviceStatus.mockResolvedValue(statusResponse());
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("打开这一页只读缓存事实，按钮正反由事实决定，一个 SIP 查询都不发", async () => {
    const wrapper = mountPanel();
    await flushPromises();

    expect(api.getDeviceStatus).toHaveBeenCalledWith(CHANNEL_ID, false);
    expect(api.getDeviceStatus).not.toHaveBeenCalledWith(CHANNEL_ID, true);
    expect(wrapper.get("[data-testid='control-record-fact']").text()).toBe("设备未录制");
    expect(wrapper.get("[data-testid='control-guard-fact']").text()).toBe("已撤防");
    expect(wrapper.get("[data-testid='control-record-toggle']").text()).toContain("开始设备端录制");
    expect(wrapper.get("[data-testid='control-guard-toggle']").text()).toContain("布防");
    // 事实已知 ⇒ 不摆第二个"请求停止"按钮，避免操作员在两条路之间犹豫
    expect(wrapper.find("[data-testid='control-record-stop']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='control-guard-reset']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("事实缺失时保持未知，正反两个动作都摆出来由操作员选", async () => {
    api.getDeviceStatus.mockResolvedValue({
      code: 0,
      message: "",
      data: { state: { freshness: "unknown" }, freshness: "unknown" }
    });
    const wrapper = mountPanel();
    await flushPromises();

    expect(wrapper.get("[data-testid='control-record-fact']").text()).toBe("录制状态未知");
    expect(wrapper.get("[data-testid='control-guard-fact']").text()).toBe("布防状态未知");
    expect(wrapper.get("[data-testid='control-record-toggle']").text()).toContain("开始设备端录制");
    expect(wrapper.get("[data-testid='control-record-stop']").text()).toContain("请求停止设备录制");
    expect(wrapper.get("[data-testid='control-guard-toggle']").text()).toContain("布防");
    expect(wrapper.get("[data-testid='control-guard-reset']").text()).toContain("请求撤防");
    wrapper.unmount();
  });

  it("HTTP 200 不算成功：报文受理后只进「等待设备应答」，本地事实不动", async () => {
    let acceptRequest!: (value: unknown) => void;
    api.controlDevice.mockReturnValue(new Promise(resolve => (acceptRequest = resolve)));
    api.getPtzOperation.mockReturnValue(new Promise(() => undefined));
    const wrapper = mountPanel();
    await flushPromises();

    await wrapper.get("[data-testid='control-record-toggle']").trigger("click");
    await flushPromises();

    expect(api.controlDevice).toHaveBeenCalledWith(CHANNEL_ID, expect.objectContaining({ action: "record_start" }));
    // ① 请求在途：只说"正在发送"
    expect(wrapper.get("[data-testid='control-record-status']").text()).toBe("正在发送");
    expect(wrapper.get("[data-testid='control-record-fact']").text()).toBe("设备未录制");

    // ② 服务端受理并记了 operation：进入等待，仍然不是成功
    acceptRequest(controlResponse("record-on"));
    await flushPromises();
    expect(wrapper.get("[data-testid='control-record-status']").text()).toBe("等待设备应答");
    // 关键：200 没让它把事实改成"设备录制中"
    expect(wrapper.get("[data-testid='control-record-fact']").text()).toBe("设备未录制");
    expect(wrapper.get("[data-testid='control-record-toggle']").text()).toContain("开始设备端录制");
    wrapper.unmount();
  });

  it("设备确认 accepted 后才改事实，随后按钮变成反向动作", async () => {
    vi.useFakeTimers();
    api.controlDevice.mockResolvedValueOnce(controlResponse("record-on")).mockResolvedValueOnce(controlResponse("record-off"));
    api.getPtzOperation
      .mockResolvedValueOnce(operationResponse("record-on", "accepted"))
      .mockResolvedValueOnce(operationResponse("record-off", "accepted"));

    const wrapper = mountPanel();
    await flushPromises();

    await wrapper.get("[data-testid='control-record-toggle']").trigger("click");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(1000);
    await flushPromises();

    expect(wrapper.get("[data-testid='control-record-fact']").text()).toBe("设备录制中");
    expect(wrapper.get("[data-testid='control-record-toggle']").text()).toContain("停止设备端录制");
    expect(wrapper.get("[data-testid='control-record-status']").text()).toBe("设备已确认");

    await wrapper.get("[data-testid='control-record-toggle']").trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenLastCalledWith(CHANNEL_ID, expect.objectContaining({ action: "record_stop" }));
    await vi.advanceTimersByTimeAsync(1000);
    await flushPromises();
    expect(wrapper.get("[data-testid='control-record-fact']").text()).toBe("设备未录制");
    wrapper.unmount();
  });

  it("录制与布撤防各自持锁：同组互斥、跨组不互斥", async () => {
    api.getDeviceStatus.mockResolvedValue({
      code: 0,
      message: "",
      data: { state: { freshness: "unknown" }, freshness: "unknown" }
    });
    api.controlDevice.mockReturnValue(new Promise(() => undefined));
    const wrapper = mountPanel();
    await flushPromises();

    await wrapper.get("[data-testid='control-record-toggle']").trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenCalledTimes(1);

    // 同组：正反动作共享一把锁，按住期间不会把反向命令也发出去
    await wrapper.get("[data-testid='control-record-stop']").trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenCalledTimes(1);

    // 跨组：布防不受录制影响
    await wrapper.get("[data-testid='control-guard-toggle']").trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenCalledTimes(2);
    expect(api.controlDevice).toHaveBeenLastCalledWith(CHANNEL_ID, expect.objectContaining({ action: "guard_set" }));
    wrapper.unmount();
  });

  it("单向命令（responseRequired=false）sent 即止：不轮询、不说成功", async () => {
    vi.useFakeTimers();
    api.controlDevice.mockResolvedValueOnce(controlResponse("one-way", "sent", { responseRequired: false }));
    const wrapper = mountPanel();
    await flushPromises();

    await wrapper.get("[data-testid='control-record-toggle']").trigger("click");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(30000);
    await flushPromises();

    expect(api.getPtzOperation).not.toHaveBeenCalled();
    expect(wrapper.get("[data-testid='control-record-status']").text()).toBe("已下发（该命令无需设备回执）");
    expect(wrapper.get("[data-testid='control-record-fact']").text()).toBe("设备未录制");
    wrapper.unmount();
  });

  it("服务端没给截止时间时只补读一次就收敛为未知，且不与普通提示同色", async () => {
    vi.useFakeTimers();
    api.controlDevice.mockResolvedValueOnce(controlResponse("no-deadline", "queued", { deadlineAt: null }));
    api.getPtzOperation.mockResolvedValue(operationResponse("no-deadline", "sent", null));
    const wrapper = mountPanel();
    await flushPromises();

    await wrapper.get("[data-testid='control-record-toggle']").trigger("click");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(1000);
    await flushPromises();

    expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
    const status = wrapper.get("[data-testid='control-record-status']");
    expect(status.text()).toContain("服务端未返回操作截止时间，补读一次后结果未知");
    // 「结果未知」不能被画成一句普通提示，否则会被读成"正在做"
    expect(status.attributes("data-level")).toBe("warn");
    expect(wrapper.get("[data-testid='control-record-fact']").text()).toBe("设备未录制");

    await vi.advanceTimersByTimeAsync(30000);
    await flushPromises();
    expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("到服务端截止时间仍未终态时只做一次截止补读并收敛", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-25T03:00:00.000Z"));
    const deadline = "2026-07-25T03:00:01.500Z";
    api.controlDevice.mockResolvedValueOnce(controlResponse("expired", "queued", { deadlineAt: deadline }));
    api.getPtzOperation.mockResolvedValue(operationResponse("expired", "sent", deadline));
    const wrapper = mountPanel();
    await flushPromises();

    await wrapper.get("[data-testid='control-record-toggle']").trigger("click");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(1000);
    await flushPromises();
    expect(api.getPtzOperation).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(500);
    await flushPromises();
    expect(api.getPtzOperation).toHaveBeenCalledTimes(2);
    expect(wrapper.get("[data-testid='control-record-status']").text()).toContain("操作超过服务端截止时间，结果未知");

    await vi.advanceTimersByTimeAsync(30000);
    await flushPromises();
    expect(api.getPtzOperation).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("换通道后在途应答作废，不改写新通道的事实", async () => {
    vi.useFakeTimers();
    let resolveOld!: (value: unknown) => void;
    api.controlDevice.mockResolvedValueOnce(controlResponse("old-op"));
    api.getPtzOperation.mockReturnValue(new Promise(resolve => (resolveOld = resolve)));

    const wrapper = mountPanel();
    await flushPromises();
    await wrapper.get("[data-testid='control-record-toggle']").trigger("click");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(1000);
    expect(api.getPtzOperation).toHaveBeenCalledWith(CHANNEL_ID, "old-op");

    await wrapper.setProps({ channelId: 2, channelOptions: [option(2)] });
    await flushPromises();

    resolveOld(operationResponse("old-op", "accepted"));
    await flushPromises();
    await vi.advanceTimersByTimeAsync(30000);
    await flushPromises();

    // 新通道的缓存事实是"未录制"；迟到的 accepted 若生效会把它改成"录制中"
    expect(wrapper.get("[data-testid='control-record-fact']").text()).toBe("设备未录制");
    expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("卸载后不再轮询", async () => {
    vi.useFakeTimers();
    api.controlDevice.mockResolvedValueOnce(controlResponse("op-1", "queued", { deadlineAt: null }));
    api.getPtzOperation.mockResolvedValue(operationResponse("op-1", "sent", null));
    const wrapper = mountPanel();
    await flushPromises();

    await wrapper.get("[data-testid='control-record-toggle']").trigger("click");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(1000);
    await flushPromises();
    expect(api.getPtzOperation).toHaveBeenCalledTimes(1);

    wrapper.unmount();
    await vi.advanceTimersByTimeAsync(30000);
    await flushPromises();
    expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
  });

  it("面板不替设备下结论：不读能力上报，点一下就把命令发出去", async () => {
    api.controlDevice.mockResolvedValueOnce(controlResponse("record-on"));
    api.getPtzOperation.mockResolvedValueOnce(operationResponse("record-on", "accepted"));
    const wrapper = mountPanel();
    await flushPromises();

    await wrapper.get("[data-testid='control-record-toggle']").trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenCalledWith(CHANNEL_ID, expect.objectContaining({ action: "record_start" }));
    // 设备自报"不支持"时仍要能试 —— 结论由设备应答给，不由平台替它下
    expect(Object.keys(api)).not.toContain("getControlCapabilities");
    wrapper.unmount();
  });

  it("报警复位单独成组下发，并展示服务端解析出的报警目标", async () => {
    api.getDeviceStatus.mockResolvedValue(
      statusResponse({
        alarmResolution: {
          status: "resolved",
          targetCode: "0411212755",
          state: "disarmed",
          freshness: "fresh",
          candidates: []
        }
      })
    );
    api.controlDevice.mockResolvedValueOnce(controlResponse("alarm-op"));
    api.getPtzOperation.mockResolvedValueOnce(operationResponse("alarm-op", "accepted"));

    const wrapper = mountPanel();
    await flushPromises();
    expect(wrapper.get("[data-testid='control-alarm-target']").text()).toBe("报警目标 0411212755");

    await wrapper.get("[data-testid='control-alarm-reset']").trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenCalledWith(CHANNEL_ID, expect.objectContaining({ action: "alarm_reset" }));
    wrapper.unmount();
  });

  it("报警目标不明确时先讲清会被拒绝，再摆按钮", async () => {
    api.getDeviceStatus.mockResolvedValue(
      statusResponse({
        alarmResolution: {
          status: "ambiguous",
          targetCode: null,
          state: "unknown",
          freshness: "fresh",
          candidates: [{ code: "0411212755", name: "门磁" }]
        }
      })
    );
    const wrapper = mountPanel();
    await flushPromises();

    const hint = wrapper.get("[data-testid='control-alarm-target']").text();
    expect(hint).toContain("报警目标不明确");
    expect(hint).toContain("0411212755");
    expect(hint).toContain("会被服务端拒绝");
    wrapper.unmount();
  });

  it("没有设备控制权限时给出原因，且点按钮不会下发任何命令", async () => {
    const wrapper = mountPanel({ canControl: false });
    await flushPromises();

    expect(wrapper.get("[data-testid='control-blocked']").text()).toBe("当前账号没有设备控制权限");
    await wrapper.get("[data-testid='control-record-toggle']").trigger("click");
    await wrapper.get("[data-testid='control-alarm-reset']").trigger("click");
    await flushPromises();
    expect(api.controlDevice).not.toHaveBeenCalled();
    expect(api.getDeviceStatus).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("该设备下没有通道时给出空态并拦下下发", async () => {
    const wrapper = mountPanel({ channelId: null, channelOptions: [] });
    await flushPromises();

    expect(wrapper.get("[data-testid='control-blocked']").text()).toBe("该设备下暂无通道，无法下发控制命令");
    await wrapper.get("[data-testid='control-alarm-reset']").trigger("click");
    await flushPromises();
    expect(api.controlDevice).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("本页不显示时不读事实（抽屉没打开就别打接口）", async () => {
    const wrapper = mountPanel({ active: false });
    await flushPromises();

    expect(api.getDeviceStatus).not.toHaveBeenCalled();
    await wrapper.setProps({ active: true });
    await flushPromises();
    expect(api.getDeviceStatus).toHaveBeenCalledWith(CHANNEL_ID, false);
    wrapper.unmount();
  });
});

/**
 * ⛔ 防回归：**不许自己造截止时间**。
 *
 * 2026-09-20 从控制台搬过来时，初版把 `parseDeadline(operation.deadlineAt)` 写成了
 * `... ?? Date.now() + DEFAULT_DEADLINE_MS`。后果很隐蔽：
 * 服务端**没给** `deadlineAt` 时，上下文里就不再是 `null`，
 * 于是"服务端未返回操作截止时间，补读一次后结果未知"这条收敛路径**永远走不到** ——
 * 界面会把一个纯粹的未知说成"超过截止时间"，两种完全不同的故障被合并成一句话。
 * 这里钉住"原样落库、不做兜底"这一条。
 */
describe("DeviceControlPanel 的截止时间契约（源码级）", () => {
  const PANEL = resolve(process.cwd(), "src/views/gb28181/device-mgmt/DeviceControlPanel.vue");
  const source = readFileSync(PANEL, "utf8");

  it("deadlineMs 直接落 parseDeadline 的结果，不叠加自造的默认值", () => {
    expect(source).not.toMatch(/deadlineMs:\s*parseDeadline\([^)]*\)\s*\?\?/);
    expect(source).toMatch(/deadlineMs:\s*parseDeadline\(/);
  });

  it("「服务端没给截止时间」与「超过截止时间」是两句不同的话", () => {
    expect(source).toContain("服务端未返回操作截止时间，补读一次后结果未知");
    expect(source).toContain("操作超过服务端截止时间，结果未知");
  });
});
