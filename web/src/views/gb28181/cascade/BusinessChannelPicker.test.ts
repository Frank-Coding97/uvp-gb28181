import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BusinessBinding, BusinessNode, CascadeChannelProjection } from "@/api/gb28181";
import type { ChannelVO } from "@/views/gb28181/device-mgmt/api";

const api = vi.hoisted(() => ({ list: vi.fn(), devices: vi.fn() }));
vi.mock("@/views/gb28181/device-mgmt/api", () => ({ listChannels: api.list, listDevices: api.devices }));

import BusinessChannelPicker from "./BusinessChannelPicker.vue";

const root: BusinessNode = {
  code: "34020000002150000001",
  name: "业务根",
  kind: "group",
  parentCode: "",
  sort: 0
};
const target: BusinessNode = {
  code: "34020000002160000002",
  name: "运营一组",
  kind: "organization",
  parentCode: root.code,
  sort: 0
};
const other: BusinessNode = {
  ...target,
  code: "34020000002160000003",
  name: "运营二组"
};

function channel(overrides: Partial<ChannelVO> = {}): ChannelVO {
  const id = overrides.id ?? 41;
  return {
    id,
    channelId: `3402000000131${String(id).padStart(7, "0")}`,
    deviceId: "34020000002000000001",
    name: `摄像机${id}`,
    alias: "",
    status: 0,
    ...overrides
  } as ChannelVO;
}

function shared(overrides: Partial<CascadeChannelProjection> = {}): CascadeChannelProjection {
  return {
    sourceDeviceId: 8,
    sourceChannelId: 41,
    publishedChannelId: "34020000001310000999",
    name: "已重编号通道",
    parentOverride: "34020000002160000999",
    ptzAllowed: true,
    active: true,
    ...overrides
  };
}

function mountPicker(props: Record<string, unknown> = {}) {
  return mount(BusinessChannelPicker, {
    props: {
      platformName: "上级平台 A",
      targetPath: "业务根 / 运营一组",
      targetCode: target.code,
      channels: [],
      bindings: [],
      nodes: [root, target],
      ...props
    }
  });
}

function page(list: ChannelVO[], total = list.length) {
  return { code: 0, message: "", data: { list, total, page: 1, pageSize: 20 } };
}

/** 范围/状态都改成了筛选行里的下拉 —— 直接驱动下拉的 change 事件。 */
async function pickFromSelect(wrapper: ReturnType<typeof mountPicker>, action: string, value: string) {
  // VTU 用选择器找组件时类型是 WrapperLike（没有 vm），运行期拿到的就是下拉组件实例
  const select = wrapper.findComponent(`[data-action='${action}']`) as unknown as {
    vm: { $emit: (event: string, value: unknown) => void };
  };
  select.vm.$emit("change", value);
  await flushPromises();
}

const switchScope = (wrapper: ReturnType<typeof mountPicker>, next: "local" | "shared") =>
  pickFromSelect(wrapper, "scope-select", next);

describe("cascade business channel picker", () => {
  beforeEach(() => {
    api.list.mockReset();
    api.list.mockResolvedValue(page([channel()]));
    api.devices.mockReset();
    api.devices.mockResolvedValue({ code: 0, message: "", data: { list: [], total: 0, page: 1, pageSize: 20 } });
  });

  /**
   * 目标组织提示行（2026-10-14 补）：弹窗标题放不下完整路径，而"加进哪个节点"是最关键的信息。
   * ⛔ 判据两头都要有：有目标时要显示路径 + 编号，**没有目标时这行必须不在** ——
   *   否则「按通道共享」会顶着一句"目标组织：未分组共享"，看着像真有个目标节点。
   */
  it("shows the target organization it will add into, and hides the line for ungrouped sharing", async () => {
    const wrapper = mountPicker();
    await flushPromises();
    const line = wrapper.get("[data-testid='picker-target']");
    expect(line.text()).toContain("业务根 / 运营一组");
    expect(line.text()).toContain(target.code);
    wrapper.unmount();

    const ungrouped = mountPicker({ targetCode: "", targetPath: "未分组共享" });
    await flushPromises();
    expect(ungrouped.find("[data-testid='picker-target']").exists()).toBe(false);
    ungrouped.unmount();
  });

  it("resolves the device name from the device list and renders online in its own column", async () => {
    const visible = channel({ id: 51, alias: "东门", status: 1, deviceId: "34020000001110000001" });
    api.list.mockResolvedValue(page([visible]));
    api.devices.mockResolvedValue({
      code: 0,
      message: "",
      data: { list: [{ id: 7, deviceId: visible.deviceId, name: "东门 NVR", alias: "东门 NVR" }], total: 1 }
    });
    const wrapper = mountPicker();
    await flushPromises();
    await flushPromises();

    expect(api.devices).toHaveBeenCalledWith({ q: visible.deviceId, page: 1, pageSize: 20 });
    const deviceCell = wrapper.get(".picker-device").text();
    expect(deviceCell).toContain("东门 NVR");
    expect(deviceCell).toContain(visible.deviceId);
    expect(wrapper.get(".picker-online").text()).toContain("在线");
    expect(wrapper.find(".picker-membership .picker-badge").exists()).toBe(true);
  });

  it("loads authorized local channels by default and lets an unshared channel enter the draft", async () => {
    const visible = channel({ id: 41, alias: "前门", status: 0 });
    api.list.mockResolvedValue(page([visible]));
    const wrapper = mountPicker();
    await flushPromises();
    expect(wrapper.findComponent({ name: "Table" }).exists()).toBe(true);
    expect(wrapper.find(".picker-table-head").exists()).toBe(false);

    expect(api.list).toHaveBeenCalledWith({ q: undefined, page: 1, pageSize: 20 });
    expect(wrapper.text()).toContain("前门");
    expect(wrapper.text()).toContain(visible.channelId);
    expect(wrapper.text()).toContain(visible.deviceId);
    expect(wrapper.text()).toContain("未共享");
    expect(wrapper.text()).toContain("离线");
    expect(wrapper.find("[data-action='empty-channels']").exists()).toBe(false);

    await wrapper.get("[data-channel-id='41'] input").setValue(true);
    await wrapper.get("[data-action='apply']").trigger("click");

    expect(wrapper.emitted("apply")?.[0]?.[0]).toEqual([
      {
        sourceDeviceId: 0,
        // ⛔ 草稿行必须把设备编号带出去：共享通道表的「在线状态」列靠它回查
        //   （少了它，草稿行永远显示「未知」—— 2026-10-14 真机踩过）。
        sourceDeviceCode: visible.deviceId,
        sourceChannelId: 41,
        publishedChannelId: visible.channelId,
        name: "前门",
        parentOverride: "",
        ptzAllowed: false,
        active: true
      }
    ]);
    expect(api.list).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("uses share-only copy and emits an ungrouped projection when targetCode is empty", async () => {
    const visible = channel({ id: 41, alias: "前门" });
    api.list.mockResolvedValue(page([visible]));
    const wrapper = mountPicker({ targetCode: "", targetPath: "" });
    await flushPromises();

    expect(wrapper.get("section").attributes("aria-label")).toBe("添加共享通道");
    // 标题行已随「本地通道 / 已共享通道」行一起移除，范围改由筛选行的下拉承载
    expect(wrapper.get("[data-action='scope-select']").text()).toContain("本地通道");
    expect(wrapper.get("[data-action='apply']").text()).toBe("加入共享");

    await wrapper.get("[data-channel-id='41'] input").setValue(true);
    await wrapper.get("[data-action='apply']").trigger("click");

    expect(wrapper.emitted("apply")?.[0]?.[0]).toEqual([
      {
        sourceDeviceId: 0,
        sourceDeviceCode: visible.deviceId,
        sourceChannelId: 41,
        publishedChannelId: visible.channelId,
        name: "前门",
        parentOverride: "",
        ptzAllowed: false,
        active: true
      }
    ]);
    wrapper.unmount();
  });

  it("prevents adding an already active shared channel again when no organization is targeted", async () => {
    const projection = shared({ sourceChannelId: 41, active: true });
    const wrapper = mountPicker({ targetCode: "", targetPath: "", channels: [projection] });
    await flushPromises();

    const checkbox = wrapper.get("[data-channel-id='41'] input");
    expect(wrapper.text()).toContain("已共享");
    expect(wrapper.text()).toContain("不能重复添加");
    expect((checkbox.element as HTMLInputElement).disabled).toBe(true);
    expect((wrapper.get("[data-action='apply']").element as HTMLButtonElement).disabled).toBe(true);

    await switchScope(wrapper, "shared");
    const sharedCheckbox = wrapper.get("[data-channel-id='41'] input");
    expect((sharedCheckbox.element as HTMLInputElement).disabled).toBe(true);
    wrapper.unmount();
  });

  it("uses server search and pagination while preserving selections across pages", async () => {
    const rows = Array.from({ length: 21 }, (_, index) => channel({ id: index + 1 }));
    api.list.mockImplementation(async ({ page: pageNumber }: { page: number }) =>
      page(rows.slice((pageNumber - 1) * 20, pageNumber * 20), rows.length)
    );
    const wrapper = mountPicker();
    await flushPromises();

    await wrapper.get("[data-channel-id='1'] input").setValue(true);
    await wrapper.get("[data-action='select-page'] input").setValue(true);
    expect(wrapper.text()).toContain("已选择 20 路");
    wrapper.findComponent({ name: "Pagination" }).vm.$emit("change", 2);
    await flushPromises();
    await flushPromises();

    expect(api.list).toHaveBeenLastCalledWith({ q: undefined, status: undefined, page: 2, pageSize: 20 });
    expect(wrapper.text()).toContain("已选择 20 路");
    expect(wrapper.find("[data-channel-id='21'] input").exists()).toBe(true);
    await wrapper.get("[data-action='select-page'] input").setValue(true);
    expect(wrapper.text()).toContain("已选择 21 路");

    await wrapper.get("[data-action='search-input']").setValue("大门");
    await wrapper.get("[data-action='search']").trigger("click");
    await flushPromises();
    expect(api.list).toHaveBeenLastCalledWith({ q: "大门", status: undefined, page: 1, pageSize: 20 });
    expect(wrapper.text()).toContain("搜索条件已变更，已清除之前的选择");
    expect(wrapper.text()).toContain("已选择 0 路");
    wrapper.unmount();
  });

  it("sends the online filter to the server and keeps it alongside the keyword", async () => {
    const wrapper = mountPicker();
    await flushPromises();

    await pickFromSelect(wrapper, "status-select", "online");
    expect(api.list).toHaveBeenLastCalledWith({ q: undefined, status: "online", page: 1, pageSize: 20 });
    expect(wrapper.text()).toContain("筛选条件已变更，已清除之前的选择");

    await wrapper.get("[data-action='search-input']").setValue("大门");
    await wrapper.get("[data-action='search']").trigger("click");
    await flushPromises();
    expect(api.list).toHaveBeenLastCalledWith({ q: "大门", status: "online", page: 1, pageSize: 20 });

    await pickFromSelect(wrapper, "status-select", "");
    expect(api.list).toHaveBeenLastCalledWith({ q: "大门", status: undefined, page: 1, pageSize: 20 });
    wrapper.unmount();
  });

  it("filters shared channels by status on the client", async () => {
    const projection = shared({ sourceChannelId: 77, publishedChannelId: "12000000001310000077" });
    const wrapper = mountPicker({ channels: [projection] });
    await flushPromises();
    await switchScope(wrapper, "shared");
    expect(wrapper.find("[data-channel-id='77'] input").exists()).toBe(true);

    // 共享行的在线状态只有本地通道缓存里有；缓存取不到时状态未知，选「在线」就不该把它列出来
    await pickFromSelect(wrapper, "status-select", "online");
    expect(wrapper.find("[data-channel-id='77'] input").exists()).toBe(false);

    await pickFromSelect(wrapper, "status-select", "");
    expect(wrapper.find("[data-channel-id='77'] input").exists()).toBe(true);
    wrapper.unmount();
  });

  it("limits select-page to the visible page and skips joined and invalid-code channels", async () => {
    api.list.mockResolvedValue(
      page([channel({ id: 41 }), channel({ id: 42, channelId: "not-a-20-digit-code" }), channel({ id: 43 })])
    );
    const wrapper = mountPicker({
      bindings: [{ sourceChannelId: 43, nodeCode: target.code } satisfies BusinessBinding]
    });
    await flushPromises();

    expect(wrapper.text()).toContain("已加入本组织");
    expect(wrapper.text()).toContain("来源通道编号不是 20 位数字，不能新增共享");
    expect((wrapper.get("[data-channel-id='42'] input").element as HTMLInputElement).disabled).toBe(true);
    expect((wrapper.get("[data-channel-id='43'] input").element as HTMLInputElement).disabled).toBe(true);
    await wrapper.get("[data-action='select-page'] input").setValue(true);
    expect(wrapper.text()).toContain("已选择 1 路");

    wrapper.unmount();
  });

  it("preserves shared identity and published fields in the local shared-channel scope", async () => {
    const projection = shared({
      sourceChannelId: 77,
      sourceDeviceId: 9,
      name: "发布侧名称",
      publishedChannelId: "12000000001310000077",
      parentOverride: "12000000002160000002",
      ptzAllowed: true
    });
    const wrapper = mountPicker({ channels: [projection] });
    await flushPromises();

    await wrapper.get("[data-channel-id='41'] input").setValue(true);
    expect(wrapper.text()).toContain("已选择 1 路");
    await switchScope(wrapper, "shared");
    expect(wrapper.text()).toContain("范围已切换，已清除之前的选择");
    expect(wrapper.text()).toContain("已选择 0 路");
    expect(api.list).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain(projection.publishedChannelId);
    expect(wrapper.text()).toContain("已共享未分组");
    await wrapper.get("[data-channel-id='77'] input").setValue(true);
    await wrapper.get("[data-action='apply']").trigger("click");

    expect(wrapper.emitted("apply")?.[0]?.[0]).toEqual([{ ...projection, active: true }]);
    wrapper.unmount();
  });

  it("requires a second explicit action before moving a channel from another organization", async () => {
    const projection = shared({ sourceChannelId: 77 });
    const bindings: BusinessBinding[] = [{ sourceChannelId: 77, nodeCode: other.code }];
    const wrapper = mountPicker({ channels: [projection], bindings, nodes: [root, target, other] });
    await flushPromises();

    await switchScope(wrapper, "shared");
    await wrapper.get("[data-channel-id='77'] input").setValue(true);
    expect(wrapper.text()).toContain("原组织：业务根 / 运营二组");
    expect(wrapper.text()).toContain("移动 1 路");
    await wrapper.get("[data-action='apply']").trigger("click");
    expect(wrapper.emitted("apply")).toBeUndefined();
    expect(wrapper.text()).toContain("再次确认将 1 路通道从 业务根 / 运营二组 移出并加入目标组织");

    await wrapper.get("[data-action='apply']").trigger("click");
    expect(wrapper.emitted("apply")?.[0]?.[0]).toEqual([{ ...projection, active: true }]);
    wrapper.unmount();
  });

  it("shows a retryable error instead of presenting a failed request as an empty list", async () => {
    api.list.mockRejectedValueOnce(new Error("接口超时"));
    const wrapper = mountPicker();
    await flushPromises();

    expect(wrapper.text()).toContain("通道加载失败：接口超时");
    expect(wrapper.find("[data-action='empty-channels']").exists()).toBe(false);
    expect(wrapper.find("[data-action='retry']").exists()).toBe(true);

    api.list.mockResolvedValueOnce(page([]));
    await wrapper.get("[data-action='retry']").trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("当前账号没有可访问的本地通道");
    wrapper.unmount();
  });

  it("does not emit apply when the user cancels", async () => {
    const wrapper = mountPicker();
    await flushPromises();
    await wrapper.get("[data-action='cancel']").trigger("click");

    expect(wrapper.emitted("cancel")).toHaveLength(1);
    expect(wrapper.emitted("apply")).toBeUndefined();
    wrapper.unmount();
  });

  it("ignores a response that arrives after the picker is unmounted", async () => {
    let resolveRequest!: (result: ReturnType<typeof page>) => void;
    api.list.mockReturnValueOnce(new Promise(resolve => (resolveRequest = resolve)));
    const wrapper = mountPicker();
    wrapper.unmount();

    resolveRequest(page([channel()]));
    await flushPromises();
    expect(api.list).toHaveBeenCalledTimes(1);
  });
});
