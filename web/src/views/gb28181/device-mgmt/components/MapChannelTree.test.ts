import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import MapChannelTree from "./MapChannelTree.vue";

const getMapChannelTree = vi.fn();
const listDirectoryTree = vi.fn();

vi.mock("../api", () => ({
  getMapChannelTree: (...args: unknown[]) => getMapChannelTree(...args),
  listDirectoryTree: (...args: unknown[]) => listDirectoryTree(...args)
}));

const channelTree = {
  devices: [
    {
      deviceId: "34020000002000000001",
      name: "执法仪原始名",
      alias: "巡逻一组",
      online: true,
      channels: [
        {
          id: 11,
          channelId: "37011200001310000001",
          deviceId: "34020000002000000001",
          name: "通道 一",
          alias: "",
          status: 1,
          longitude: 117.05,
          latitude: 36.685,
          positionSource: "mobile"
        },
        {
          id: 12,
          channelId: "37011200001310000002",
          deviceId: "34020000002000000001",
          name: "通道 二",
          alias: "西门",
          status: 0,
          longitude: 117.06,
          latitude: 36.686,
          positionSource: "manual"
        }
      ]
    },
    {
      deviceId: "34020000002000000002",
      name: "执法仪 B",
      alias: "",
      online: false,
      channels: [
        {
          id: 21,
          channelId: "37011200001310000009",
          deviceId: "34020000002000000002",
          name: "单兵通道",
          alias: "",
          status: 0,
          longitude: 117.1,
          latitude: 36.7,
          positionSource: "catalog"
        }
      ]
    }
  ],
  total: 3,
  truncated: false,
  noCoordCount: 2
};

const departmentTree = [
  { key: "department:dept:10", name: "保卫部", type: "department", readOnly: true, count: 1, onlineCount: 1, depth: 0 },
  { key: "department:dept:20", name: "巡特警", type: "department", readOnly: true, count: 2, onlineCount: 1, depth: 0 }
];
const businessTree = [
  { key: "business:catalog:1", name: "一组", type: "biz_group", readOnly: true, count: 1, onlineCount: 0, depth: 0 }
];

function mountTree(props: Record<string, unknown> = {}) {
  return mount(MapChannelTree, {
    props: { scopeValue: "", status: "", ...props },
    global: {
      stubs: {
        "a-spin": { template: "<div><slot /></div>" },
        "a-tooltip": { props: ["content"], template: '<span :data-tooltip="content"><slot /></span>' },
        // 范围下拉已经换成 Arco：桩成真 <select> 才能继续用 setValue 驱动"选一项"，
        // 同时把 data-testid 等 attrs 透到 <select> 上（组件里它们挂在 a-select 上）。
        "a-select": {
          inheritAttrs: false,
          props: ["modelValue"],
          emits: ["update:modelValue", "change"],
          template: `<select v-bind="$attrs" :value="modelValue" @change="$emit('change', $event.target.value); $emit('update:modelValue', $event.target.value)"><slot /></select>`
        },
        "a-option": { props: ["value"], template: '<option :value="value"><slot /></option>' },
        "a-optgroup": { props: ["label"], template: '<optgroup :label="label"><slot /></optgroup>' }
      }
    }
  });
}

describe("MapChannelTree", () => {
  beforeEach(() => {
    getMapChannelTree.mockReset().mockResolvedValue({ code: 0, data: structuredClone(channelTree) });
    listDirectoryTree
      .mockReset()
      .mockImplementation((view: string) =>
        Promise.resolve({ code: 0, data: { list: view === "department" ? departmentTree : businessTree } })
      );
  });

  it("只渲染后端给的可定位通道，并报出未设置位置的数量", async () => {
    const wrapper = mountTree();
    await flushPromises();

    // 设备名走 alias 优先，编码作为副标题
    expect(wrapper.text()).toContain("巡逻一组");
    expect(wrapper.text()).toContain("34020000002000000001");
    expect(wrapper.text()).toContain("2 点");
    // 通道默认展开：点位目录折叠起来等于多点一次
    expect(wrapper.text()).toContain("通道 一");
    expect(wrapper.text()).toContain("西门");
    expect(wrapper.text()).toContain("另有 2 个通道未设置位置");
    expect(wrapper.text()).not.toContain("点位过多");

    // 无坐标的通道根本不进树（后端保证），前端只负责把数量提示出来
    // 没有范围时不能凭空带上 directoryView：后端会把它当成一次目录筛选
    expect(getMapChannelTree).toHaveBeenCalledWith({ q: undefined, status: undefined });
    wrapper.unmount();
  });

  it("点通道触发 locate，点设备触发 focus", async () => {
    const wrapper = mountTree();
    await flushPromises();

    const rows = wrapper.findAll(".mct-channel-row");
    expect(rows).toHaveLength(3);
    await rows[0].get(".directory-node").trigger("click");
    expect(wrapper.emitted("locate")?.[0]?.[0]).toMatchObject({
      id: 11,
      deviceId: "34020000002000000001",
      longitude: 117.05,
      latitude: 36.685
    });
    expect(wrapper.emitted("focus")).toBeUndefined();

    await wrapper.findAll(".directory-row-device")[1].get(".directory-device-node").trigger("click");
    expect(wrapper.emitted("focus")?.[0]?.[0]).toMatchObject({ deviceId: "34020000002000000002" });
    wrapper.unmount();
  });

  it("收起设备后不再显示它的通道", async () => {
    const wrapper = mountTree();
    await flushPromises();
    expect(wrapper.findAll(".mct-channel-row")).toHaveLength(3);

    await wrapper.findAll(".directory-row-device")[0].get(".twist-button").trigger("click");
    expect(wrapper.findAll(".mct-channel-row")).toHaveLength(1);
    wrapper.unmount();
  });

  it("搜索走后端并做防抖，搜索时强制展开被收起过的设备", async () => {
    const wrapper = mountTree();
    await flushPromises();
    getMapChannelTree.mockClear();

    await wrapper.get("[data-testid='map-channel-search']").setValue("西");
    await wrapper.get("[data-testid='map-channel-search']").setValue("西门");
    await new Promise(resolve => setTimeout(resolve, 100));
    expect(getMapChannelTree).not.toHaveBeenCalled();

    await new Promise(resolve => setTimeout(resolve, 260));
    await flushPromises();
    expect(getMapChannelTree).toHaveBeenCalledTimes(1);
    expect(getMapChannelTree).toHaveBeenCalledWith(expect.objectContaining({ q: "西门" }));
    // 关键词与顶部搜索框共用一份：输入就要同步出去，否则地图和左栏会各筛各的
    expect(wrapper.emitted("update:keyword")?.at(-1)?.[0]).toBe("西门");

    // 搜到的东西不能被"之前收起的设备"藏起来
    await wrapper.findAll(".directory-row-device")[0].get(".twist-button").trigger("click");
    expect(wrapper.findAll(".mct-channel-row")).toHaveLength(3);
    await wrapper.get("[data-testid='map-channel-search']").setValue("西门子");
    await new Promise(resolve => setTimeout(resolve, 320));
    await flushPromises();
    expect(wrapper.findAll(".mct-channel-row")).toHaveLength(3);
    wrapper.unmount();
  });

  it("范围与状态变化会带进查询，行政区划这类下拉表达不了的范围补一个选项", async () => {
    const wrapper = mountTree({ scopeValue: "department|department:dept:10" });
    await flushPromises();
    expect(getMapChannelTree).toHaveBeenCalledWith(
      expect.objectContaining({ directoryView: "department", directoryKey: "department:dept:10" })
    );

    getMapChannelTree.mockClear();
    await wrapper.setProps({ status: "online" });
    await flushPromises();
    expect(getMapChannelTree).toHaveBeenCalledWith(expect.objectContaining({ status: "online" }));

    // 行政区划生效时下拉里没有该项 ⇒ 用 scopeLabel 补一个，不能显示成"全部通道"
    const wrapper2 = mountTree({ scopeValue: "administrative|administrative:area:370100", scopeLabel: "行政区划 / 济南市" });
    await flushPromises();
    expect(wrapper2.get("[data-testid='map-scope-select']").text()).toContain("行政区划 / 济南市");
    // 行政区划同样要透传给后端，否则地图被收窄、左栏还列着全国通道
    expect(getMapChannelTree).toHaveBeenCalledWith(
      expect.objectContaining({ directoryView: "administrative", directoryKey: "administrative:area:370100" })
    );
    wrapper.unmount();
    wrapper2.unmount();
  });

  it("切换范围下拉会带上节点本身，清空则回 null", async () => {
    const wrapper = mountTree();
    await flushPromises();

    await wrapper.get("[data-testid='map-scope-select']").setValue("department|department:dept:10");
    expect(wrapper.emitted("scopeChange")?.[0]?.[0]).toMatchObject({
      view: "department",
      node: { key: "department:dept:10", name: "保卫部" }
    });

    await wrapper.get("[data-testid='map-scope-select']").setValue("");
    expect(wrapper.emitted("scopeChange")?.[1]?.[0]).toBeNull();
    wrapper.unmount();
  });

  it("截断时明确提示，空态区分“搜索无结果”和“还没采点”", async () => {
    getMapChannelTree.mockResolvedValueOnce({
      code: 0,
      data: { devices: [], total: 1000, truncated: true, noCoordCount: 0 }
    });
    const wrapper = mountTree();
    await flushPromises();
    expect(wrapper.text()).toContain("点位过多，仅显示前 1000 个");
    expect(wrapper.text()).toContain("还没有设置位置信息的通道");

    getMapChannelTree.mockResolvedValueOnce({ code: 0, data: { devices: [], total: 0, truncated: false, noCoordCount: 0 } });
    await wrapper.get("[data-testid='map-channel-search']").setValue("没有这个");
    await new Promise(resolve => setTimeout(resolve, 320));
    await flushPromises();
    expect(wrapper.text()).toContain("没有匹配的可定位通道");
    wrapper.unmount();
  });
});
