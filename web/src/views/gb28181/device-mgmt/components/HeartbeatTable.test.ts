import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ArcoVue from "@arco-design/web-vue";
import HeartbeatTable from "./HeartbeatTable.vue";

const traceApi = vi.hoisted(() => ({
  fetchTraceHealth: vi.fn(),
  listTraceMessages: vi.fn()
}));

vi.mock("@/api/gb28181-trace", () => traceApi);

const MINUTE = 60_000;

/** 时间一律按「距今多久」造：窗口是相对现在算的，写死绝对时间隔天就跑到窗口外了 */
function agoIso(ms: number) {
  return new Date(Date.now() - ms).toISOString();
}

function inbound(callId: string, at: string) {
  return {
    eventId: `evt-${callId}`,
    occurredAt: at,
    direction: "inbound",
    transport: "UDP",
    localAddr: "10.0.0.1:51064",
    remoteAddr: "182.33.246.128:36159",
    deviceId: "37010301021180000010",
    method: "MESSAGE",
    statusCode: 0,
    callId,
    cseq: 1,
    cseqMethod: "MESSAGE",
    businessCode: "keepalive",
    businessType: "心跳保持",
    userAgent: "MERCURY IP-Camera",
    malformed: false
  };
}

function outbound(callId: string, at: string, statusCode: number) {
  return { ...inbound(callId, at), direction: "outbound", statusCode, userAgent: "" };
}

type Params = { direction?: string; limit?: number };

/** 主查询（limit=500）一次取完；探测「最后一次心跳」（limit=1）单独给结果 */
function mockMessages(inboundItems: unknown[], outboundItems: unknown[] = [], probeItem?: unknown) {
  traceApi.listTraceMessages.mockImplementation((params: Params) => {
    if (params.limit === 1 && params.direction === "inbound") {
      return Promise.resolve({ code: 0, data: { items: probeItem ? [probeItem] : [] } });
    }
    if (params.direction === "outbound") {
      return Promise.resolve({ code: 0, data: { items: outboundItems, nextCursor: "" } });
    }
    return Promise.resolve({ code: 0, data: { items: inboundItems, nextCursor: "" } });
  });
}

/** 只替身 a-table：把列标题与每个单元格槽渲染出来，其余按真实组件走。 */
const TableStub = {
  name: "HeartbeatTableStub",
  props: ["data", "columns", "rowKey", "rowClass"],
  template: `<div class="table-stub">
    <span v-for="column in columns" :key="column.title" class="column-title">{{ column.title }}</span>
    <div v-for="record in data" :key="record.key" class="table-stub-row">
      <slot name="received" :record="record" />
      <slot name="result" :record="record" />
      <slot name="latency" :record="record" />
      <slot name="interval" :record="record" />
      <slot name="source" :record="record" />
      <slot name="agent" :record="record" />
    </div>
  </div>`
};

function mountTable(props: Record<string, unknown> = {}) {
  return mount(HeartbeatTable, {
    props: { deviceId: "37010301021180000010", ...props },
    global: { plugins: [ArcoVue], stubs: { "a-table": TableStub } }
  });
}

/** 加载链路有 3 段（inbound / outbound / 往前探），一次 flush 追不上 */
async function settle() {
  for (let round = 0; round < 6; round += 1) await flushPromises();
}

describe("心跳记录表格", () => {
  beforeEach(() => {
    traceApi.fetchTraceHealth.mockReset().mockResolvedValue({ code: 0, data: { state: "ready" } });
    traceApi.listTraceMessages.mockReset();
  });

  it("列出心跳与平台应答，异常与正常分得开", async () => {
    mockMessages(
      [inbound("a", agoIso(1 * MINUTE)), inbound("b", agoIso(2 * MINUTE))],
      [outbound("a", agoIso(1 * MINUTE - 120), 200)]
    );

    const wrapper = mountTable();
    await settle();

    const rows = wrapper.findComponent({ name: "HeartbeatTableStub" }).props("data") as { kind: string }[];
    expect(rows.map(row => row.kind)).toEqual(["ok", "no_response"]);

    const text = wrapper.text();
    expect(text).toContain("收到时间");
    expect(text).toContain("平台判定");
    expect(text).toContain("正常接收");
    // b 那次没有应答 ⇒ 必须显示「无应答」，不能被当成正常
    expect(text).toContain("无应答");
    expect(text).toContain("182.33.246.128:36159");
    expect(text).toContain("MERCURY IP-Camera");
  });

  it("24 小时窗口画出 144 格条带，并把最后一条心跳单独描出来", async () => {
    mockMessages(
      [inbound("a", agoIso(1 * MINUTE)), inbound("b", agoIso(2 * MINUTE))],
      [outbound("a", agoIso(1 * MINUTE - 120), 200), outbound("b", agoIso(2 * MINUTE - 120), 200)]
    );

    const wrapper = mountTable();
    await settle();

    // 24 小时 / 10 分钟 = 144 格
    expect(wrapper.findAll(".heartbeat-cell")).toHaveLength(144);
    // 最后一条心跳所在的那一格必须能被单独认出来，而不是混在一片色块里
    expect(wrapper.findAll(".heartbeat-cell[data-last='true']")).toHaveLength(1);
    expect(wrapper.text()).toContain("心跳连续性");
    expect(wrapper.text()).toContain("点任意一格放大到该时段");
  });

  it("设备五小时前就停了：直接说出最后一次心跳的时间与停顿时长", async () => {
    mockMessages([inbound("a", agoIso(5 * 60 * MINUTE))], [outbound("a", agoIso(5 * 60 * MINUTE - 120), 200)]);

    const wrapper = mountTable();
    await settle();

    const text = wrapper.text();
    expect(text).toContain("最后一条心跳在");
    // 距今应当接近 5 小时；不断言精确到分，避免测试跟着执行时刻抖动
    expect(text).toMatch(/距今已 4 小时 5[0-9] 分|距今已 5 小时/);
  });

  it("窗口内一条心跳都没有时，往前找出最后一次心跳并给入口", async () => {
    mockMessages([], [], inbound("old", agoIso(30 * 60 * MINUTE)));

    const wrapper = mountTable();
    await settle();

    const text = wrapper.text();
    expect(text).toContain("一条心跳都没有");
    expect(text).toContain("再往前找，最后一次心跳在");
    expect(wrapper.text()).toContain("查看那一刻");
  });

  it("点某一格就把时间范围收窄到那一段，粒度细化到分钟", async () => {
    mockMessages(
      [inbound("a", agoIso(1 * MINUTE)), inbound("b", agoIso(2 * MINUTE))],
      [outbound("a", agoIso(1 * MINUTE - 120), 200), outbound("b", agoIso(2 * MINUTE - 120), 200)]
    );

    const wrapper = mountTable();
    await settle();
    const before = traceApi.listTraceMessages.mock.calls.length;

    await wrapper.findAll(".heartbeat-cell")[100].trigger("click");
    await settle();

    // 又发了一轮请求，而且范围被收窄成那一格（外加半分钟余量）
    expect(traceApi.listTraceMessages.mock.calls.length).toBeGreaterThan(before);
    const params = traceApi.listTraceMessages.mock.calls.at(-1)?.[0] as { from: string; to: string; limit?: number };
    const span = new Date(params.to).getTime() - new Date(params.from).getTime();
    expect(span).toBeLessThanOrEqual(11 * MINUTE);
    // 收窄后条带跟着变成分钟级，并且给出返回原窗口的入口
    expect(wrapper.text()).toContain("返回近 24 小时");
  });

  it("报文记录未启用时给明确说明，而不是一张空表", async () => {
    traceApi.fetchTraceHealth.mockResolvedValue({ code: 0, data: { state: "disabled" } });
    const wrapper = mountTable();
    await flushPromises();
    expect(wrapper.text()).toContain("未启用 SIP 报文记录");
    expect(traceApi.listTraceMessages).not.toHaveBeenCalled();
  });

  it("报文收到了却没计入心跳时给出对账提示", async () => {
    mockMessages([inbound("a", agoIso(1 * MINUTE))], [outbound("a", agoIso(1 * MINUTE - 120), 200)]);

    const wrapper = mountTable({ accountedKeepaliveAt: agoIso(30 * MINUTE) });
    await settle();
    expect(wrapper.text()).toContain("没有计入心跳");
  });

  it("应答查询失败时报错而不是静默显示无应答", async () => {
    traceApi.listTraceMessages.mockImplementation((params: Params) =>
      Promise.resolve(
        params.direction === "outbound"
          ? { code: 500, message: "heartbeat responses failed", data: { items: [] } }
          : { code: 0, data: { items: [inbound("a", agoIso(1 * MINUTE))], nextCursor: "" } }
      )
    );

    const wrapper = mountTable();
    await settle();
    expect(wrapper.text()).toContain("heartbeat responses failed");
  });
});
