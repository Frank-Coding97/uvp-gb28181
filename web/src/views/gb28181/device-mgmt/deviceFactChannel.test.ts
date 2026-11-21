import { describe, expect, it } from "vitest";
import { buildFactChannelOptions, factChannelLabel, pickDefaultFactChannel } from "./deviceFactChannel";
import type { ChannelVO } from "./api";

function channel(overrides: Partial<ChannelVO> = {}): ChannelVO {
  return {
    id: 1,
    channelId: "34020000001320000001",
    deviceId: "34020000002000000001",
    name: "园区北门",
    alias: "",
    manufacturer: "",
    model: "",
    owner: "",
    civilCode: "",
    parentId: "",
    ptzType: 1,
    status: 1,
    ...overrides
  } as ChannelVO;
}

describe("deviceFactChannel", () => {
  it("通道名优先用别名，再退回设备上报名，最后退回编码", () => {
    expect(factChannelLabel(channel({ alias: "北门-球机", name: "CH1" }))).toBe("北门-球机 · 34020000001320000001");
    expect(factChannelLabel(channel({ alias: "", name: "CH1" }))).toBe("CH1 · 34020000001320000001");
    expect(factChannelLabel(channel({ alias: "", name: "", channelId: "" }))).toBe("通道 #1");
  });

  it("过滤掉没有有效数据库 ID 的通道", () => {
    const options = buildFactChannelOptions([
      channel({ id: 3, alias: "三号" }),
      channel({ id: 0, alias: "脏数据" }),
      channel({ id: Number.NaN, alias: "脏数据" })
    ]);
    expect(options.map(item => item.value)).toEqual([3]);
    expect(buildFactChannelOptions(null)).toEqual([]);
  });

  it("离线通道照样进选项，只是标出来", () => {
    const options = buildFactChannelOptions([channel({ id: 5, status: 0 })]);
    expect(options[0]).toMatchObject({ value: 5, online: false });
  });

  it("默认落到第一个在线通道，全离线时才退回第一个", () => {
    const options = buildFactChannelOptions([
      channel({ id: 1, status: 0 }),
      channel({ id: 2, status: 1 }),
      channel({ id: 3, status: 1 })
    ]);
    expect(pickDefaultFactChannel(options, null)).toBe(2);

    const offline = buildFactChannelOptions([channel({ id: 1, status: 0 }), channel({ id: 2, status: 0 })]);
    expect(pickDefaultFactChannel(offline, null)).toBe(1);
    expect(pickDefaultFactChannel([], null)).toBeNull();
  });

  it("已选通道还在列表里就保持不变，免得刷新通道列表时把用户的选择顶掉", () => {
    const options = buildFactChannelOptions([channel({ id: 1, status: 1 }), channel({ id: 2, status: 1 })]);
    expect(pickDefaultFactChannel(options, 2)).toBe(2);
    // 选中的通道已经不在设备下了（例如换了设备）→ 重新挑一个在线的。
    expect(pickDefaultFactChannel(options, 99)).toBe(1);
  });
});
