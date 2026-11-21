import { describe, expect, it } from "vitest";
import type { AlarmPopupEvent } from "@/api/gb28181-alarm-stream";
import {
  MAX_ALARM_POPUPS,
  alarmPopupDetail,
  alarmPopupEntityName,
  alarmPopupIdentifiers,
  alarmPopupKey,
  alarmPopupPriority,
  alarmPopupSubtitle,
  alarmPopupTitle,
  enqueueAlarmPopup,
  formatAlarmPopupTime,
  markAlarmPopupLeaving,
  removeAlarmPopup,
  type AlarmPopupItem
} from "./alarmPopupQueue";

function event(overrides: Partial<AlarmPopupEvent> = {}): AlarmPopupEvent {
  return {
    deviceCode: "34020000001320000001",
    deviceName: "东门摄像机",
    receivedAt: "2026-11-17T11:23:05+08:00",
    ...overrides
  };
}

function item(key: string, overrides: Partial<AlarmPopupEvent> = {}): AlarmPopupItem {
  return { key, event: event(overrides), leaving: false };
}

describe("alarmPopupQueue", () => {
  it("同一条告警入队两次只保留一张", () => {
    const first = enqueueAlarmPopup([], item("a"));
    expect(enqueueAlarmPopup(first, item("a"))).toBe(first);
  });

  it("超过上限挤掉最旧的，保留最新的顺序", () => {
    let items: AlarmPopupItem[] = [];
    for (let index = 1; index <= MAX_ALARM_POPUPS + 2; index += 1) {
      items = enqueueAlarmPopup(items, item(`k${index}`));
    }
    expect(items).toHaveLength(MAX_ALARM_POPUPS);
    expect(items.map(entry => entry.key)).toEqual(["k3", "k4", "k5", "k6"]);
  });

  it("标记离场与移除都不改变其它条目", () => {
    const items = [item("a"), item("b")];
    const leaving = markAlarmPopupLeaving(items, "a");
    expect(leaving[0].leaving).toBe(true);
    expect(leaving[1].leaving).toBe(false);
    expect(items[0].leaving).toBe(false);
    expect(removeAlarmPopup(items, "a").map(entry => entry.key)).toEqual(["b"]);
  });

  it("key 优先用 alarmId；不落库时用设备+时间+序号兜底", () => {
    expect(alarmPopupKey(event({ alarmId: 12 }), 1)).toBe("alarm-12");
    const key = alarmPopupKey(event({ receivedAt: "2026-11-17T11:23:05+08:00" }), 3);
    expect(key).not.toContain("alarm-");
    expect(key).toContain("34020000001320000001");
    expect(key).toContain("3");
    // 同一毫秒的两条不会撞
    expect(alarmPopupKey(event(), 4)).not.toBe(alarmPopupKey(event(), 5));
  });

  it("名称回退链是别名 → 名称 → 编码，且逐级 trim", () => {
    expect(alarmPopupEntityName({ alias: " 北门枪机 ", name: "东门摄像机", code: "3402" })).toBe("北门枪机");
    expect(alarmPopupEntityName({ alias: "   ", name: "东门摄像机", code: "3402" })).toBe("东门摄像机");
    expect(alarmPopupEntityName({ alias: "", name: "", code: "3402" })).toBe("3402");
    expect(alarmPopupEntityName({})).toBe("");
  });

  it("标题优先用报警类型文案，其次描述", () => {
    expect(alarmPopupTitle(event({ method: 5, alarmType: 6, description: "入侵" }))).toBe("入侵检测报警");
    expect(alarmPopupTitle(event({ method: 9, alarmType: 1, description: " 视频丢失 " }))).toBe("视频丢失");
    expect(alarmPopupTitle(event({ method: 9, alarmType: 1 }))).toBe("设备报警");
  });

  it("副标题拼设备与通道，缺一就只显示一个", () => {
    expect(alarmPopupSubtitle(event({ channelAlias: " 三号通道 " }))).toBe("东门摄像机 · 三号通道");
    expect(alarmPopupSubtitle(event())).toBe("东门摄像机");
  });

  it("详情与标题重复时不重复显示", () => {
    expect(alarmPopupDetail(event({ method: 5, alarmType: 6, description: "入侵检测报警" }))).toBe("");
    expect(alarmPopupDetail(event({ method: 5, alarmType: 6, description: "南侧围墙" }))).toBe("南侧围墙");
  });

  it("级别只认 1-4，其它一律 0", () => {
    expect(alarmPopupPriority(event({ priority: 1 }))).toBe(1);
    expect(alarmPopupPriority(event({ priority: 4 }))).toBe(4);
    expect(alarmPopupPriority(event({ priority: 9 }))).toBe(0);
    expect(alarmPopupPriority(event())).toBe(0);
  });

  it("卡片带设备 ID 与通道 ID，缺通道时补来源编码", () => {
    expect(alarmPopupIdentifiers(event({ channelCode: "34020000001320000002" }))).toEqual([
      { label: "设备 ID", value: "34020000001320000001" },
      { label: "通道 ID", value: "34020000001320000002" }
    ]);

    // 报警 SourceID 没匹配到通道表：通道 ID 给占位，来源编码单独列出（排查用）
    expect(alarmPopupIdentifiers(event({ channelCode: "  ", sourceCode: "34020000001320000009" }))).toEqual([
      { label: "设备 ID", value: "34020000001320000001" },
      { label: "通道 ID", value: "—" },
      { label: "来源编码", value: "34020000001320000009" }
    ]);

    // 来源编码与设备编码相同（设备级报警）时不重复出一行
    expect(alarmPopupIdentifiers(event({ sourceCode: "34020000001320000001" }))).toHaveLength(2);
    // 设备编码缺失也要有占位，卡片高度不能因为某条异常数据而跳动
    expect(alarmPopupIdentifiers(event({ deviceCode: "" }))[0]).toEqual({ label: "设备 ID", value: "—" });
  });

  it("时间格式化失败返回空串，不显示 Invalid Date", () => {
    expect(formatAlarmPopupTime("not-a-time")).toBe("");
    expect(formatAlarmPopupTime("2026-11-17T11:23:05+08:00")).toMatch(/^\d{2}:\d{2}:\d{2}$/);
  });
});
