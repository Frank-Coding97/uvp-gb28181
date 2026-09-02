import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import MediaScopeBar from "./MediaScopeBar.vue";

const nodes = [
  { id: 1, name: "主节点", state: "active" as const },
  { id: 2, name: "备节点", state: "offline" as const }
];

describe("MediaScopeBar", () => {
  it("shows the selected node without redundant range labels", () => {
    const wrapper = mount(MediaScopeBar, { props: { modelValue: 1, nodes, allowAll: false } });

    expect(wrapper.text()).not.toContain("数据范围");
    expect(wrapper.text()).not.toContain("仅查看所选节点");
    expect(wrapper.text()).toContain("当前节点");
    expect(wrapper.get("[aria-label='节点范围']").attributes("model-value")).toBe("1");
  });

  it("uses a right-aligned borderless node toolbar above the page content", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/components/MediaScopeBar.vue"), "utf8");
    expect(source).toContain("<a-select");
    expect(source).toContain("<a-option");
    expect(source).not.toContain("<select");
    expect(source).not.toContain("<Server");
    expect(source).toMatch(/\.media-scope-bar\s*\{[^}]*justify-content:\s*flex-end/);
    expect(source).toMatch(/\.media-scope-bar\s*\{[^}]*border:\s*0/);
    expect(source).toMatch(/\.media-scope-bar__field\s*\{[^}]*box-shadow:/);
    expect(source).toMatch(/\.media-scope-bar__notice\s*\{[^}]*flex:\s*none/);
  });

  it("offers all nodes only when the workspace supports an aggregate scope", async () => {
    const wrapper = mount(MediaScopeBar, { props: { modelValue: "all", nodes, allowAll: true } });

    expect(wrapper.get("select[aria-label='节点范围']").text()).toContain("全部节点");
    expect(wrapper.text()).toContain("备节点 · 离线");
    await wrapper.get("[aria-label='节点范围']").setValue("2");
    expect(wrapper.emitted("update:modelValue")?.[0]).toEqual([2]);
  });

  it("does not silently select a node for node-required operations", () => {
    const wrapper = mount(MediaScopeBar, {
      props: { modelValue: "all", nodes, allowAll: false, requiresNode: true }
    });

    expect(wrapper.text()).not.toContain("全部节点");
    expect(wrapper.get("[role='status']").text()).toContain("请先明确选择节点");
    expect(wrapper.emitted("update:modelValue")).toBeUndefined();
  });

  it("keeps the previous scope visible when refresh fails", async () => {
    const wrapper = mount(MediaScopeBar, {
      props: { modelValue: 1, nodes, errorText: "节点目录刷新失败", stale: true }
    });

    expect(wrapper.get("[aria-label='节点范围']").attributes("model-value")).toBe("1");
    expect(wrapper.get("[role='alert']").text()).toContain("仍显示上次成功目录");
    await wrapper.get("button[aria-label='刷新节点目录']").trigger("click");
    expect(wrapper.emitted("refresh")).toHaveLength(1);
  });
});
