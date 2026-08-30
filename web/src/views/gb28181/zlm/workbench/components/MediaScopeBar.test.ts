import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import MediaScopeBar from "./MediaScopeBar.vue";

const nodes = [
  { id: 1, name: "主节点", state: "active" as const },
  { id: 2, name: "备节点", state: "offline" as const }
];

describe("MediaScopeBar", () => {
  it("offers all nodes only when the workspace supports an aggregate scope", async () => {
    const wrapper = mount(MediaScopeBar, { props: { modelValue: "all", nodes, allowAll: true } });

    expect(wrapper.get("select[aria-label='节点范围']").text()).toContain("全部节点");
    expect(wrapper.text()).toContain("备节点 · 离线");
    await wrapper.get("select").setValue("2");
    expect(wrapper.emitted("update:modelValue")?.[0]).toEqual([2]);
  });

  it("does not silently select a node for node-required operations", () => {
    const wrapper = mount(MediaScopeBar, {
      props: { modelValue: "all", nodes, allowAll: false, requiresNode: true }
    });

    expect(wrapper.get("select").text()).not.toContain("全部节点");
    expect(wrapper.get("[role='status']").text()).toContain("请先明确选择节点");
    expect(wrapper.emitted("update:modelValue")).toBeUndefined();
  });

  it("keeps the previous scope visible when refresh fails", async () => {
    const wrapper = mount(MediaScopeBar, {
      props: { modelValue: 1, nodes, errorText: "节点目录刷新失败", stale: true }
    });

    expect(wrapper.get("select").element.value).toBe("1");
    expect(wrapper.get("[role='alert']").text()).toContain("仍显示上次成功目录");
    await wrapper.get("button[aria-label='刷新节点目录']").trigger("click");
    expect(wrapper.emitted("refresh")).toHaveLength(1);
  });
});
