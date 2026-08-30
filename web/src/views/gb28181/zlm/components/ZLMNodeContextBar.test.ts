import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";

import { useZLMContextStore } from "@/store/modules/zlm-context";
import ZLMNodeContextBar from "./ZLMNodeContextBar.vue";

const nodes = [
  { id: 1, name: "主节点", state: "active" as const },
  { id: 2, name: "离线节点", state: "offline" as const }
];

describe("ZLMNodeContextBar", () => {
  beforeEach(() => {
    sessionStorage.clear();
  });

  it("keeps and labels an offline query selection, then falls back after removal", async () => {
    const testPinia = createPinia();
    setActivePinia(testPinia);
    const wrapper = mount(ZLMNodeContextBar, {
      props: { nodes, queryNodeId: "2" },
      global: { plugins: [testPinia] }
    });

    expect(wrapper.text()).toContain("离线节点");
    expect(wrapper.text()).toContain("节点已离线，保留当前选择以便排查");

    await wrapper.setProps({ nodes: [nodes[0]] });
    await flushPromises();
    expect(wrapper.text()).toContain("主节点");
    expect(wrapper.text()).toContain("节点在线，运行态数据会自动刷新");
  });

  it("keeps the current node while an empty node list is still loading", async () => {
    const testPinia = createPinia();
    setActivePinia(testPinia);
    const context = useZLMContextStore();
    context.initialize(nodes, "2");

    const wrapper = mount(ZLMNodeContextBar, {
      props: { nodes: [], queryNodeId: "2", loading: true },
      global: { plugins: [testPinia] }
    });

    expect(context.selectedNodeId).toBe(2);

    await wrapper.setProps({ loading: false });
    await flushPromises();
    expect(context.selectedNodeId).toBeNull();
  });

  it("supports an explicit all-node scope and defaults to it only once", async () => {
    const testPinia = createPinia();
    setActivePinia(testPinia);
    const context = useZLMContextStore();
    const wrapper = mount(ZLMNodeContextBar, {
      props: { nodes, allowAll: true, defaultAll: true },
      global: { plugins: [testPinia] }
    });

    await flushPromises();
    expect(context.selectedNodeId).toBeNull();
    expect(wrapper.text()).toContain("全部节点");
    expect(wrapper.text()).toContain("聚合全部可见节点");

    context.selectNode(1);
    await wrapper.setProps({ nodes: [...nodes] });
    await flushPromises();
    expect(context.selectedNodeId).toBe(1);
  });
});
