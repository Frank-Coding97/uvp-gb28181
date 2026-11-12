import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ArcoVue from "@arco-design/web-vue";
import HeartbeatTable from "./HeartbeatTable.vue";

const traceApi = vi.hoisted(() => ({
  fetchTraceHealth: vi.fn(),
  listTraceMessages: vi.fn()
}));

vi.mock("@/api/gb28181-trace", () => traceApi);

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

describe("心跳记录表格", () => {
  beforeEach(() => {
    traceApi.fetchTraceHealth.mockReset().mockResolvedValue({ code: 0, data: { state: "ready" } });
    traceApi.listTraceMessages.mockReset();
  });

  it("列出心跳与平台应答，异常与正常分得开", async () => {
    traceApi.listTraceMessages.mockImplementation((params: { direction?: string }) => {
      if (params.direction === "outbound") {
        return Promise.resolve({ code: 0, data: { items: [outbound("a", "2026-11-12T10:01:58.080Z", 200)] } });
      }
      return Promise.resolve({
        code: 0,
        data: {
          items: [inbound("a", "2026-11-12T10:01:58.000Z"), inbound("b", "2026-11-12T10:00:58.000Z")],
          nextCursor: "cursor-2"
        }
      });
    });

    const wrapper = mountTable();
    await flushPromises();
    await flushPromises();

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
    expect(text).toContain("加载更早");
  });

  it("报文记录未启用时给明确说明，而不是一张空表", async () => {
    traceApi.fetchTraceHealth.mockResolvedValue({ code: 0, data: { state: "disabled" } });
    const wrapper = mountTable();
    await flushPromises();
    expect(wrapper.text()).toContain("未启用 SIP 报文记录");
    expect(traceApi.listTraceMessages).not.toHaveBeenCalled();
  });

  it("报文收到了却没计入心跳时给出对账提示", async () => {
    traceApi.listTraceMessages.mockImplementation((params: { direction?: string }) =>
      Promise.resolve(
        params.direction === "outbound"
          ? { code: 0, data: { items: [outbound("a", "2026-11-12T10:01:58.080Z", 200)] } }
          : { code: 0, data: { items: [inbound("a", "2026-11-12T10:01:58.000Z")] } }
      )
    );

    const wrapper = mountTable({ accountedKeepaliveAt: "2026-11-12T09:50:00.000Z" });
    await flushPromises();
    await flushPromises();
    expect(wrapper.text()).toContain("没有计入心跳");
  });

  it("应答查询失败时报错而不是静默显示无应答", async () => {
    traceApi.listTraceMessages.mockImplementation((params: { direction?: string }) =>
      Promise.resolve(
        params.direction === "outbound"
          ? { code: 500, message: "heartbeat responses failed", data: { items: [] } }
          : { code: 0, data: { items: [inbound("a", "2026-11-12T10:01:58.000Z")] } }
      )
    );

    const wrapper = mountTable();
    await flushPromises();
    await flushPromises();
    expect(wrapper.text()).toContain("heartbeat responses failed");
  });
});
