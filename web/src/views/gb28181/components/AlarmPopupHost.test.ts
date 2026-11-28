import { mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import { ALARM_POPUP_LEAVE_MS, ALARM_POPUP_TTL_MS, MAX_ALARM_POPUPS } from "./alarmPopupQueue";
import type { AlarmPopupEvent } from "@/api/gb28181-alarm-stream";
import AlarmPopupHost from "./AlarmPopupHost.vue";

const buildAlarmStreamUrl = vi.hoisted(() => vi.fn(() => "/api/gb28181/alarms?stream=1"));
vi.mock("@/api/gb28181-alarm-stream", () => ({ buildAlarmStreamUrl }));

/**
 * jsdom 不实现 EventSource，用最小替身驱动组件：
 * 保留 addEventListener / close / readyState，另加 emit 供测试推帧。
 */
class FakeEventSource {
  static CLOSED = 2;
  static OPEN = 1;
  static CONNECTING = 0;
  static instances: FakeEventSource[] = [];

  readyState = FakeEventSource.CONNECTING;
  onerror: ((event: unknown) => void) | null = null;
  closed = false;
  private listeners = new Map<string, Array<(event: { data: string }) => void>>();

  constructor(public url: string) {
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: (event: { data: string }) => void) {
    const bucket = this.listeners.get(type) ?? [];
    bucket.push(listener);
    this.listeners.set(type, bucket);
  }

  close() {
    this.closed = true;
    this.readyState = FakeEventSource.CLOSED;
  }

  emit(type: string, payload: unknown) {
    for (const listener of this.listeners.get(type) ?? []) {
      listener({ data: JSON.stringify(payload) });
    }
  }

  emitRaw(type: string, data: string) {
    for (const listener of this.listeners.get(type) ?? []) {
      listener({ data });
    }
  }
}

function alarmEvent(overrides: Partial<AlarmPopupEvent> = {}): AlarmPopupEvent {
  return {
    alarmId: 1,
    deviceCode: "34020000001320000001",
    deviceName: "东门摄像机",
    priority: 1,
    method: 5,
    alarmType: 6,
    description: "入侵检测报警",
    receivedAt: "2026-11-17T11:00:00+08:00",
    ...overrides
  };
}

async function mountHost() {
  const wrapper = mount(AlarmPopupHost);
  await nextTick();
  const source = FakeEventSource.instances.at(-1);
  if (!source) throw new Error("组件没有建立 SSE 连接");
  return { wrapper, source };
}

describe("AlarmPopupHost 设备告警实时浮层", () => {
  beforeEach(() => {
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("挂载即连接告警流，并在收到报警后渲染卡片", async () => {
    const { wrapper, source } = await mountHost();
    expect(buildAlarmStreamUrl).toHaveBeenCalled();
    expect(wrapper.find('[data-testid="alarm-popup"]').exists()).toBe(false);

    source.emit("alarm", alarmEvent({ alarmId: 7, deviceAlias: "北门枪机" }));
    await nextTick();

    const card = wrapper.get('[data-testid="alarm-popup"]');
    expect(card.text()).toContain("入侵检测报警");
    // 名称回退链：别名优先于设备自上报名称
    expect(card.text()).toContain("北门枪机");
    expect(wrapper.classes()).toContain("uvp-alarm-popups");
  });

  it("卡片上直接显示设备 ID 与通道 ID", async () => {
    const { wrapper, source } = await mountHost();
    source.emit("alarm", alarmEvent({ alarmId: 13, channelCode: "34020000001320000002", channelName: "三号通道" }));
    await nextTick();

    const card = wrapper.get('[data-testid="alarm-popup"]');
    const ids = card.findAll(".uvp-alarm-popup__id");
    expect(ids).toHaveLength(2);
    expect(ids[0].text()).toContain("设备 ID");
    expect(ids[0].text()).toContain("34020000001320000001");
    expect(ids[1].text()).toContain("通道 ID");
    expect(ids[1].text()).toContain("34020000001320000002");
    // 长编码截断时靠 title 看全
    expect(ids[1].get(".uvp-alarm-popup__id-value").attributes("title")).toBe("34020000001320000002");
  });

  it("耐性：没有通道信息时通道 ID 显示占位而不是留空", async () => {
    const { wrapper, source } = await mountHost();
    source.emit("alarm", alarmEvent({ alarmId: 17, sourceCode: "34020000001320000009" }));
    await nextTick();

    const ids = wrapper.get('[data-testid="alarm-popup"]').findAll(".uvp-alarm-popup__id");
    expect(ids[1].text()).toContain("—");
    expect(ids[2].text()).toContain("34020000001320000009");
  });

  it("停留数秒后自动消失（先离场再移除）", async () => {
    const { wrapper, source } = await mountHost();
    source.emit("alarm", alarmEvent({ alarmId: 9 }));
    await nextTick();
    expect(wrapper.findAll('[data-testid="alarm-popup"]')).toHaveLength(1);

    vi.advanceTimersByTime(ALARM_POPUP_TTL_MS - 1);
    await nextTick();
    expect(wrapper.findAll('[data-testid="alarm-popup"]')).toHaveLength(1);

    vi.advanceTimersByTime(1);
    await nextTick();
    expect(wrapper.get('[data-testid="alarm-popup"]').classes()).toContain("is-leaving");

    vi.advanceTimersByTime(ALARM_POPUP_LEAVE_MS);
    await nextTick();
    expect(wrapper.findAll('[data-testid="alarm-popup"]')).toHaveLength(0);
  });

  it("同一条报警重复推送不会弹两次", async () => {
    const { wrapper, source } = await mountHost();
    source.emit("alarm", alarmEvent({ alarmId: 11 }));
    source.emit("alarm", alarmEvent({ alarmId: 11 }));
    await nextTick();
    expect(wrapper.findAll('[data-testid="alarm-popup"]')).toHaveLength(1);
  });

  it("堆叠上限内挤掉最旧的一张", async () => {
    const { wrapper, source } = await mountHost();
    for (let index = 1; index <= MAX_ALARM_POPUPS + 2; index += 1) {
      source.emit("alarm", alarmEvent({ alarmId: index, deviceName: `设备${index}` }));
    }
    await nextTick();
    const cards = wrapper.findAll('[data-testid="alarm-popup"]');
    expect(cards).toHaveLength(MAX_ALARM_POPUPS);
    // 最旧的两条已经被挤出
    expect(cards[0].text()).toContain("设备3");
  });

  it("点关闭按钮立即进入离场，动画结束后移除", async () => {
    const { wrapper, source } = await mountHost();
    source.emit("alarm", alarmEvent({ alarmId: 21 }));
    await nextTick();

    await wrapper.get(".uvp-alarm-popup__close").trigger("click");
    expect(wrapper.get('[data-testid="alarm-popup"]').classes()).toContain("is-leaving");

    vi.advanceTimersByTime(ALARM_POPUP_LEAVE_MS);
    await nextTick();
    expect(wrapper.findAll('[data-testid="alarm-popup"]')).toHaveLength(0);
  });

  it("脏帧被忽略且不影响后续推送", async () => {
    const { wrapper, source } = await mountHost();
    source.emitRaw("alarm", "{不是 JSON");
    source.emit("alarm", alarmEvent({ alarmId: 31 }));
    await nextTick();
    expect(wrapper.findAll('[data-testid="alarm-popup"]')).toHaveLength(1);
  });

  it("卸载时关闭连接并清空浮层", async () => {
    const { wrapper, source } = await mountHost();
    source.emit("alarm", alarmEvent({ alarmId: 41 }));
    await nextTick();
    wrapper.unmount();
    expect(source.closed).toBe(true);
  });
});
