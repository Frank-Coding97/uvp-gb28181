import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createDirectoryState, readDirectoryViewPreference, type DirectoryState } from "../directoryState";
import DirectoryPanel from "./DirectoryPanel.vue";

const listDirectoryTree = vi.fn();
const directoryPreferences = new Map<string, string>();
Object.defineProperty(window, "localStorage", {
  configurable: true,
  value: {
    getItem: (key: string) => directoryPreferences.get(key) ?? null,
    setItem: (key: string, value: string) => directoryPreferences.set(key, String(value))
  }
});

vi.mock("../api", () => ({
  listDirectoryTree: (...args: unknown[]) => listDirectoryTree(...args)
}));

const administrativeTree = [
  {
    key: "administrative:area:370000",
    name: "山东省",
    type: "area",
    code: "370000",
    readOnly: true,
    count: 2,
    onlineCount: 1,
    depth: 0,
    children: [
      {
        key: "administrative:area:370100",
        name: "济南市",
        type: "area",
        code: "370100",
        readOnly: true,
        count: 2,
        onlineCount: 1,
        depth: 1,
        children: [
          {
            key: "device:31",
            name: "门岗 NVR",
            type: "device",
            code: "34020000002000000031",
            readOnly: true,
            count: 0,
            onlineCount: 0,
            depth: 2,
            device: { id: 31, deviceId: "34020000002000000031", alias: "", name: "门岗 NVR", online: true }
          }
        ]
      }
    ]
  },
  {
    key: "administrative:unknown",
    name: "未知区域",
    type: "unknown",
    readOnly: true,
    count: 1,
    onlineCount: 0,
    depth: 0,
    children: []
  }
];

const departmentTree = [
  {
    key: "department:dept:10",
    name: "保卫部",
    type: "organization",
    readOnly: true,
    count: 1,
    onlineCount: 1,
    depth: 0,
    children: [
      {
        key: "device:31",
        name: "门岗 NVR",
        type: "device",
        code: "34020000002000000031",
        readOnly: true,
        count: 0,
        onlineCount: 0,
        depth: 1,
        device: { id: 31, deviceId: "34020000002000000031", alias: "", name: "门岗 NVR", online: true }
      }
    ]
  }
];

function mountPanel(state = createDirectoryState(), selectedDeviceId: number | null = null) {
  return mount(DirectoryPanel, {
    props: { modelValue: state, selectedDeviceId },
    global: {
      stubs: {
        "a-spin": { template: "<div><slot /></div>" },
        "a-tooltip": { template: "<span><slot /></span>" }
      }
    }
  });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => {
    resolve = done;
  });
  return { promise, resolve };
}

describe("DirectoryPanel", () => {
  beforeEach(() => {
    listDirectoryTree.mockReset();
    directoryPreferences.clear();
    listDirectoryTree.mockImplementation((view: string) =>
      Promise.resolve({
        code: 0,
        data: { list: view === "department" ? departmentTree : view === "business" ? [] : administrativeTree }
      })
    );
  });

  it("selects a populated business group and renders channel status and source", async () => {
    const channel = {
      key: "business:catalog:3",
      name: "摄像头",
      type: "channel",
      count: 1,
      onlineCount: 0,
      depth: 1,
      readOnly: true
    };
    const group = {
      key: "business:catalog:2",
      name: "校园",
      type: "biz_group",
      count: 1,
      onlineCount: 0,
      depth: 0,
      readOnly: true,
      meta: { sourceName: "下级平台" },
      children: [channel]
    };
    listDirectoryTree.mockResolvedValue({ code: 0, data: { list: [{ ...group, key: "empty", count: 0, children: [] }, group] } });
    const wrapper = mountPanel(createDirectoryState("business"));
    await flushPromises();
    const updates = wrapper.emitted("update:modelValue")!;
    const state = updates[updates.length - 1][0] as DirectoryState;
    expect(state.selectedKey.business).toBe(group.key);
    await wrapper.setProps({ modelValue: state });
    expect(wrapper.emitted("select")![0][0]).toEqual(group);
    expect(wrapper.text()).toContain("下级平台");
    const leaf = wrapper.get('[data-node-key="business:catalog:3"]');
    expect(leaf.find(".channel-node-icon").exists()).toBe(true);
    expect(leaf.find(".node-count").exists()).toBe(false);
    expect(leaf.get(".channel-status").text()).toBe("离线");
    await (wrapper.vm as any).sync();
    await flushPromises();
    expect(wrapper.emitted("select")).toHaveLength(1);
  });

  it("shows three directory tabs in the agreed order and defaults to administrative", async () => {
    const wrapper = mountPanel();
    await flushPromises();

    expect(listDirectoryTree).toHaveBeenCalledWith("administrative", expect.objectContaining({ sync: 1 }));
    expect(wrapper.findAll(".directory-switch [role='tab']").map(tab => tab.attributes("data-view"))).toEqual([
      "administrative",
      "department",
      "business"
    ]);
    expect(wrapper.get("[data-view='administrative']").classes()).toContain("active");
    expect(wrapper.text()).toContain("行政区划");
    expect(wrapper.text()).toContain("部门目录");
    expect(wrapper.text()).toContain("业务分组");
    expect(wrapper.text()).not.toContain("综合目录");
    expect(wrapper.text()).not.toContain("自定义分组");
    expect(wrapper.find("[data-action='create-root']").exists()).toBe(false);
    expect(wrapper.find("[data-action='rename']").exists()).toBe(false);
    await wrapper.get("[data-view='department']").trigger("click");
    expect(readDirectoryViewPreference()).toBe("department");
    wrapper.unmount();
  });

  it("renders readable administrative names and selectable device leaves", async () => {
    const state = createDirectoryState();
    state.expandedKeys.administrative = ["administrative:area:370000", "administrative:area:370100"];
    const wrapper = mountPanel(state, 31);
    await flushPromises();

    expect(wrapper.text()).toContain("山东省");
    expect(wrapper.text()).toContain("未知区域");
    expect(wrapper.text()).not.toContain("370000");
    expect(wrapper.get('[data-node-key="administrative:area:370000"]').attributes("title")).toContain("370000");
    const device = wrapper.get('[data-node-key="device:31"]');
    expect(device.attributes("aria-selected")).toBe("true");
    expect(device.text()).toContain("门岗 NVR");
    await device.get(".directory-node").trigger("click");
    expect(wrapper.emitted("deviceSelect")?.[0]?.[0]).toMatchObject({ id: 31, deviceId: "34020000002000000031" });
    expect(wrapper.emitted("select")).toBeUndefined();
    wrapper.unmount();
  });

  it("selects a department node whose subtree contains its assigned devices", async () => {
    const state = createDirectoryState("department");
    state.expandedKeys.department = ["department:dept:10"];
    const wrapper = mountPanel(state);
    await flushPromises();

    expect(listDirectoryTree).toHaveBeenCalledWith("department", expect.objectContaining({ sync: 1 }));
    const department = wrapper.get('[data-node-key="department:dept:10"]');
    expect(department.text()).toContain("保卫部");
    await department.get(".directory-node").trigger("click");
    expect(wrapper.emitted("select")?.[0]?.[0]).toMatchObject({ key: "department:dept:10", name: "保卫部" });
    const emitted = wrapper.emitted("update:modelValue")?.at(-1)?.[0] as DirectoryState;
    expect(emitted.selectedKey.department).toBe("department:dept:10");
    expect(wrapper.get('[data-node-key="device:31"]').text()).toContain("门岗 NVR");
    wrapper.unmount();
  });

  it("merges background deltas without changing selection or expansion", async () => {
    const state = createDirectoryState();
    state.expandedKeys.administrative = ["administrative:area:370000", "administrative:area:370100"];
    listDirectoryTree.mockResolvedValueOnce({ code: 0, data: { reset: true, revision: "v1", list: administrativeTree } });
    const wrapper = mountPanel(state, 31);
    await flushPromises();
    const device = administrativeTree[0].children![0].children![0];
    listDirectoryTree.mockResolvedValueOnce({
      code: 0,
      data: {
        reset: false,
        revision: "v2",
        roots: administrativeTree.map(node => node.key),
        removed: [],
        upserts: [{ node: { ...device, device: { ...device.device, online: false } }, children: [] }]
      }
    });
    const updatesBefore = wrapper.emitted("update:modelValue")?.length || 0;
    await (wrapper.vm as any).sync();
    await flushPromises();

    expect(listDirectoryTree).toHaveBeenLastCalledWith("administrative", expect.objectContaining({ since: "v1", sync: 1 }));
    expect(wrapper.get('[data-node-key="device:31"] .device-node-status').text()).toBe("离线");
    expect(wrapper.get('[data-node-key="device:31"]').attributes("aria-selected")).toBe("true");
    expect(wrapper.emitted("update:modelValue")?.length || 0).toBe(updatesBefore);
    wrapper.unmount();
  });

  it("invalidates in-flight deltas when the linked resource filters change", async () => {
    vi.useFakeTimers();
    const old = deferred<any>();
    listDirectoryTree.mockReturnValueOnce(old.promise);
    const wrapper = mountPanel();
    await wrapper.setProps({ resourceFilter: { assetKind: "channel", q: "Gate", status: "online" } });
    listDirectoryTree.mockResolvedValueOnce({ code: 0, data: { reset: true, revision: "filtered", list: [] } });
    await vi.advanceTimersByTimeAsync(300);
    await flushPromises();
    old.resolve({ code: 0, data: { list: administrativeTree, revision: "old", reset: true } });
    await flushPromises();

    expect(wrapper.text()).not.toContain("山东省");
    expect(wrapper.text()).toContain("暂无匹配");
    expect(listDirectoryTree).toHaveBeenLastCalledWith(
      "administrative",
      expect.objectContaining({ assetKind: "channel", q: "Gate", status: "online", since: undefined })
    );
    wrapper.unmount();
    vi.useRealTimers();
  });

  it("keeps the latest department tree when an older refresh finishes last", async () => {
    const first = deferred<any>();
    const second = deferred<any>();
    listDirectoryTree.mockReset();
    listDirectoryTree.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const wrapper = mountPanel(createDirectoryState("department"));
    const refresh = (wrapper.vm as any).refresh("department");

    second.resolve({ code: 0, data: { list: [{ ...departmentTree[0], name: "最新部门" }] } });
    await flushPromises();
    expect(wrapper.text()).toContain("最新部门");
    first.resolve({ code: 0, data: { list: [{ ...departmentTree[0], name: "过期部门" }] } });
    await refresh;
    await flushPromises();

    expect(wrapper.text()).toContain("最新部门");
    expect(wrapper.text()).not.toContain("过期部门");
    wrapper.unmount();
  });
});
